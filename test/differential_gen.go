// 差分 fuzz 的随机程序生成器：按 docs/00-spec.md 生成合法程序，
// 再由 TestDifferentialFuzz 拿去跑双引擎逐字节对比。
//
// 设计要点（都是为了让生成的程序「大概率合法、一定终止、输出确定」）：
//   - 维护作用域栈，变量先声明后使用，同名只在内层块里遮蔽；
//   - 跟踪每个变量与函数的类型，只生成类型合法的表达式
//     （跨类型 == 是合法的，会返回 false，可以出现）；
//   - while 一律用带计数器的有界形式，递归只用 fib 模板且参数很小；
//   - 不用 clock()（输出不确定），不用输入（语言没有）；
//   - 每个程序用固定种子（baseSeed + 序号），失败时凭序号即可复现。
package test

import (
	"fmt"
	"math/rand"
	"strings"
)

// fnSig 是生成器登记的函数签名：参数类型与返回类型，
// 以及声明时所在的作用域链（函数声明是块作用域的，
// 只有在该链是当前作用域链前缀的位置才能调用它）。
// bounded 表示该函数（经传递）可能调用递归模板 fib：
// 调用它时 number 参数必须用小字面量，否则 fib 的指数级
// 递归会让程序跑不完（例如 f1(81)）。
type fnSig struct {
	params  []string
	ret     string
	scopes  []int
	bounded bool
}

// diffGen 是单个程序的生成器状态。
type diffGen struct {
	r           *rand.Rand
	scopes      []map[string]string // 名字 -> 类型（"num"/"str"/"bool"/"fn"），最内层在最后
	scopeIDs    []int               // 与 scopes 平行的作用域编号栈
	nextScopeID int
	fns         map[string]fnSig // 已声明的函数名 -> 签名
	locked      map[string]bool  // 循环计数器：体内不许赋值
	counter     int              //  fresh 名字计数器
	blockDepth  int              // 当前块嵌套深度：只在 0 时才生成函数声明
	called      map[string]bool  // 当前函数体内调用过的函数（生成 bounded 用）
}

func newDiffGen(seed int64) *diffGen {
	return &diffGen{
		r:           rand.New(rand.NewSource(seed)),
		scopes:      []map[string]string{{}},
		scopeIDs:    []int{0},
		nextScopeID: 1,
		fns:         map[string]fnSig{},
		locked:      map[string]bool{},
	}
}

// fnVisible 判断一个在 scopes 链处声明的函数在当前位置是否可见：
// 声明时的作用域链必须是当前作用域链的前缀。
func (g *diffGen) fnVisible(declScopes []int) bool {
	if len(declScopes) > len(g.scopeIDs) {
		return false
	}
	for i, id := range declScopes {
		if g.scopeIDs[i] != id {
			return false
		}
	}
	return true
}

// ---------- 作用域 ----------

func (g *diffGen) pushScope() {
	g.scopes = append(g.scopes, map[string]string{})
	g.scopeIDs = append(g.scopeIDs, g.nextScopeID)
	g.nextScopeID++
}

func (g *diffGen) popScope() {
	g.scopes = g.scopes[:len(g.scopes)-1]
	g.scopeIDs = g.scopeIDs[:len(g.scopeIDs)-1]
}

func (g *diffGen) declare(name, typ string) { g.scopes[len(g.scopes)-1][name] = typ }

// lookup 从内向外找变量，返回类型。
func (g *diffGen) lookup(name string) (string, bool) {
	for i := len(g.scopes) - 1; i >= 0; i-- {
		if t, ok := g.scopes[i][name]; ok {
			return t, true
		}
	}
	return "", false
}

// varsOfType 返回当前所有可见的、指定类型的变量名。
// 注意遮蔽：同名变量以最内层绑定的类型为准——外层同类型、
// 内层不同类型的同名变量不能返回，否则运行时的实际类型
// 会与生成器以为的类型不一致。
func (g *diffGen) varsOfType(typ string) []string {
	var out []string
	seen := map[string]bool{}
	for i := len(g.scopes) - 1; i >= 0; i-- {
		for name, t := range g.scopes[i] {
			if seen[name] {
				continue
			}
			seen[name] = true
			if t == typ {
				out = append(out, name)
			}
		}
	}
	return out
}

func (g *diffGen) freshVar() string {
	g.counter++
	return fmt.Sprintf("v%d", g.counter)
}

func (g *diffGen) freshFn() string {
	g.counter++
	return fmt.Sprintf("f%d", g.counter)
}

// ---------- 字面量 ----------

var diffStrings = []string{`"a"`, `"b"`, `"hi"`, `""`, `"a\tb"`, `"x y"`}

func (g *diffGen) numLit() string {
	switch g.r.Intn(6) {
	case 0:
		return "0"
	case 1:
		return "0.5"
	case 2:
		return "2.5"
	case 3:
		return "3.14"
	default:
		return fmt.Sprintf("%d", g.r.Intn(10))
	}
}

func (g *diffGen) strLit() string { return diffStrings[g.r.Intn(len(diffStrings))] }

func (g *diffGen) boolLit() string {
	if g.r.Intn(2) == 0 {
		return "true"
	}
	return "false"
}

func (g *diffGen) literal(typ string) string {
	switch typ {
	case "num":
		return g.numLit()
	case "str":
		return g.strLit()
	case "bool":
		return g.boolLit()
	case "fn":
		// 函数值没有字面量，退回一个当前可见的已声明函数名。
		for name, sig := range g.fns {
			if g.fnVisible(sig.scopes) {
				return name
			}
		}
		return "nil"
	default:
		return "nil"
	}
}

// ---------- 表达式 ----------

// expr 生成一个期望类型为 want 的表达式，depth 控制递归深度。
// want 为 "any" 时任意类型都行（只用于 print 与 == 的操作数）。
func (g *diffGen) expr(depth int, want string) string {
	if want == "any" {
		want = []string{"num", "str", "bool"}[g.r.Intn(3)]
	}
	if depth <= 0 {
		if vs := g.varsOfType(want); len(vs) > 0 && g.r.Intn(2) == 0 {
			return vs[g.r.Intn(len(vs))]
		}
		return g.literal(want)
	}

	switch g.r.Intn(10) {
	case 0, 1:
		return g.literal(want)
	case 2:
		if vs := g.varsOfType(want); len(vs) > 0 {
			return vs[g.r.Intn(len(vs))]
		}
		return g.literal(want)
	case 3:
		// 一元运算：操作数加括号，否则优先级会改变语义——
		// 例如 ! 作用在 `a / b` 上时，不加括号会变成 `(!a) / b`。
		if want == "num" {
			return "-(" + g.expr(depth-1, "num") + ")"
		}
		if want == "bool" {
			return "!(" + g.expr(depth-1, "any") + ")"
		}
		return g.expr(depth-1, want)
	case 4, 5:
		// 二元运算（按期望类型选合法的运算符）。
		return g.binary(depth, want)
	case 6:
		// 函数调用：只挑返回类型匹配的已声明函数。
		if call, ok := g.call(depth, want); ok {
			return call
		}
		return g.literal(want)
	default:
		return "(" + g.expr(depth-1, want) + ")"
	}
}

// binary 生成期望类型为 want 的二元表达式。
// 结果整体加括号：作为其他表达式的操作数时，优先级不会改变语义
// （例如 `a + b` 放进 `!(...)` 或 `*` 的操作数位置时）。
func (g *diffGen) binary(depth int, want string) string {
	switch want {
	case "num":
		op := []string{"+", "-", "*", "/", "%"}[g.r.Intn(5)]
		return fmt.Sprintf("(%s %s %s)", g.expr(depth-1, "num"), op, g.expr(depth-1, "num"))
	case "str":
		// 字符串只有 + 是合法的。
		return fmt.Sprintf("(%s + %s)", g.expr(depth-1, "str"), g.expr(depth-1, "str"))
	case "bool":
		switch g.r.Intn(4) {
		case 0:
			// 相等：允许跨类型（返回 false），但多用同类型。
			t := []string{"num", "str", "bool"}[g.r.Intn(3)]
			op := "=="
			if g.r.Intn(2) == 0 {
				op = "!="
			}
			return fmt.Sprintf("(%s %s %s)", g.expr(depth-1, t), op, g.expr(depth-1, t))
		case 1:
			// 大小比较：只接受同类型 number 或 string。
			t := "num"
			if g.r.Intn(2) == 0 {
				t = "str"
			}
			op := []string{"<", "<=", ">", ">="}[g.r.Intn(4)]
			return fmt.Sprintf("(%s %s %s)", g.expr(depth-1, t), op, g.expr(depth-1, t))
		default:
			// 短路：操作数本身是 bool 时结果才是 bool。
			op := "&&"
			if g.r.Intn(2) == 0 {
				op = "||"
			}
			return fmt.Sprintf("(%s %s %s)", g.expr(depth-1, "bool"), op, g.expr(depth-1, "bool"))
		}
	default:
		return g.literal(want)
	}
}

// call 生成一个返回类型为 want 的函数调用，ok 表示是否找到了匹配的函数。
// 只挑当前可见（作用域链是前缀）的函数，避免块外调用块内声明的函数。
func (g *diffGen) call(depth int, want string) (string, bool) {
	var names []string
	for name, sig := range g.fns {
		if sig.ret == want && g.fnVisible(sig.scopes) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "", false
	}
	name := names[g.r.Intn(len(names))]
	sig := g.fns[name]
	if g.called != nil {
		g.called[name] = true
	}
	args := make([]string, len(sig.params))
	for i, p := range sig.params {
		// 可能（经传递）调用 fib 的函数：number 参数只传小字面量，
		// 否则参数值可能一路流进 fib 的递归，造成指数级耗时。
		if sig.bounded && p == "num" {
			args[i] = fmt.Sprintf("%d", g.r.Intn(11))
			continue
		}
		args[i] = g.expr(depth-1, p)
	}
	return fmt.Sprintf("%s(%s)", name, strings.Join(args, ", ")), true
}

// ---------- 语句 ----------

// blockStmts 生成 n 条块内语句：块内不生成函数声明——
// 函数声明是块作用域的，在块里声明、块外调用会对双引擎都报错，
// 生成器要避免这种“合法但无意义”的噪音。
func (g *diffGen) blockStmts(n int, retType string) string {
	g.blockDepth++
	defer func() { g.blockDepth-- }()
	return g.stmts(n, retType)
}

// stmts 生成 n 条语句；retType 为 "" 表示顶层（不许 return），
// 否则表示在返回该类型的函数体内（可以提前 return）。
func (g *diffGen) stmts(n int, retType string) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(g.stmt(retType))
	}
	return b.String()
}

func (g *diffGen) stmt(retType string) string {
	roll := g.r.Intn(10)
	switch {
	case roll < 3:
		// let 声明：先生成初始值表达式，再登记名字——
		// 否则初始值里会引用到正在声明的变量本身。
		typ := []string{"num", "str", "bool"}[g.r.Intn(3)]
		name := g.freshVar()
		init := g.expr(3, typ)
		g.declare(name, typ)
		return fmt.Sprintf("let %s = %s;\n", name, init)
	case roll == 3:
		// 赋值（含赋值留值链）：挑一个未锁定的同类型变量。
		if s := g.assignStmt(); s != "" {
			return s
		}
		return fmt.Sprintf("print %s;\n", g.expr(3, "any"))
	case roll == 4:
		return fmt.Sprintf("print %s;\n", g.expr(3, "any"))
	case roll == 5:
		// if / else，else 后面有时再跟一个 if（else if）。
		// 每个分支的 { } 都是新作用域：生成分支体时 push，结束时 pop，
		// 否则分支里声明的变量会在分支外还被生成器认为可见。
		cond := g.expr(2, "bool")
		g.pushScope()
		then := g.blockStmts(1+g.r.Intn(3), retType)
		g.popScope()
		s := fmt.Sprintf("if (%s) {\n%s}\n", cond, then)
		if g.r.Intn(2) == 0 {
			if g.r.Intn(2) == 0 {
				g.pushScope()
				els := g.blockStmts(1+g.r.Intn(2), retType)
				g.popScope()
				s += fmt.Sprintf("else {\n%s}\n", els)
			} else {
				g.pushScope()
				els := g.blockStmts(1+g.r.Intn(2), retType)
				g.popScope()
				s += fmt.Sprintf("else if (%s) {\n%s}\n", g.expr(2, "bool"), els)
			}
		}
		return s
	case roll == 6:
		// 有界 while：计数器锁定，体内不许赋值给它，保证终止。
		// 计数器声明在循环外的当前作用域，循环体是新作用域。
		ctr := g.freshVar()
		limit := 1 + g.r.Intn(5)
		g.declare(ctr, "num")
		g.locked[ctr] = true
		g.pushScope()
		body := g.blockStmts(1+g.r.Intn(3), retType)
		g.popScope()
		delete(g.locked, ctr)
		return fmt.Sprintf("let %s = 0;\nwhile (%s < %d) {\n%s%s = %s + 1;\n}\n",
			ctr, ctr, limit, body, ctr, ctr)
	case roll == 7:
		// 块：内层遮蔽外层同名变量（只在块作用域里）。
		g.pushScope()
		var inner strings.Builder
		if outer := g.outerVarNames(); len(outer) > 0 && g.r.Intn(2) == 0 {
			name := outer[g.r.Intn(len(outer))]
			typ := []string{"num", "str", "bool"}[g.r.Intn(3)]
			// 先算初始值再登记：否则初始值会引用到内层自己的名字，
			// 而不是要遮蔽的外层变量。
			init := g.expr(3, typ)
			g.declare(name, typ)
			inner.WriteString(fmt.Sprintf("let %s = %s;\n", name, init))
		}
		inner.WriteString(g.blockStmts(1+g.r.Intn(3), retType))
		g.popScope()
		return fmt.Sprintf("{\n%s}\n", inner.String())
	case roll == 8 && retType != "":
		// 提前 return（只在函数体内）。
		return fmt.Sprintf("return %s;\n", g.expr(3, retType))
	default:
		// 嵌套函数声明（闭包：体内可引用外层变量）。
		// 只在块深度 0 时生成，见 blockStmts 的注释。
		if g.blockDepth == 0 {
			if s := g.fnDecl(1); s != "" {
				return s
			}
		}
		return fmt.Sprintf("print %s;\n", g.expr(3, "any"))
	}
}

// assignStmt 生成一条赋值语句，含偶尔的赋值链；没有可赋值的变量时返回 ""。
func (g *diffGen) assignStmt() string {
	var cands []string
	for name := range g.scopes[len(g.scopes)-1] {
		if !g.locked[name] {
			cands = append(cands, name)
		}
	}
	// 也允许给外层变量赋值（闭包共享捕获变量就靠这个）。
	for i := len(g.scopes) - 2; i >= 0; i-- {
		for name := range g.scopes[i] {
			if !g.locked[name] {
				cands = append(cands, name)
			}
		}
	}
	if len(cands) == 0 {
		return ""
	}
	name := cands[g.r.Intn(len(cands))]
	typ, _ := g.lookup(name)
	if g.r.Intn(4) == 0 {
		// 赋值链：x = y = v，要求 y 与 x 同类型。
		if same := g.varsOfType(typ); len(same) > 1 {
			y := same[g.r.Intn(len(same))]
			if y != name && !g.locked[y] {
				return fmt.Sprintf("%s = %s = %s;\n", name, y, g.expr(2, typ))
			}
		}
	}
	return fmt.Sprintf("%s = %s;\n", name, g.expr(2, typ))
}

// outerVarNames 返回当前最内层以外所有可见的变量名（用于块内遮蔽）。
func (g *diffGen) outerVarNames() []string {
	var out []string
	seen := map[string]bool{}
	for i := 0; i < len(g.scopes)-1; i++ {
		for name := range g.scopes[i] {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// ---------- 函数 ----------

// fnDecl 生成一个函数声明并登记签名；depth 控制嵌套函数深度。
// 返回 "" 表示本次不生成（例如嵌套已达上限）。
func (g *diffGen) fnDecl(depth int) string {
	if depth > 2 {
		return ""
	}
	name := g.freshFn()
	nparams := g.r.Intn(3)
	params := make([]string, nparams)
	sig := fnSig{ret: []string{"num", "str", "bool"}[g.r.Intn(3)]}
	g.pushScope()
	for i := 0; i < nparams; i++ {
		p := fmt.Sprintf("p%d", i)
		pt := []string{"num", "str", "bool"}[g.r.Intn(3)]
		params[i] = p
		sig.params = append(sig.params, pt)
		g.declare(p, pt)
	}
	// 函数体是新的块深度 0：体内顶层可以再声明嵌套函数（闭包）。
	savedDepth := g.blockDepth
	savedCalled := g.called
	g.blockDepth = 0
	g.called = map[string]bool{}
	body := g.stmts(1+g.r.Intn(3), sig.ret)
	// 嵌套函数：体内可以再声明一个闭包，捕获外层变量。
	nested := ""
	if depth < 2 && g.r.Intn(3) == 0 {
		nested = g.fnDecl(depth + 1)
	}
	ret := fmt.Sprintf("return %s;\n", g.expr(3, sig.ret))
	// bounded 经传递：如果体内调用了可能调用 fib 的函数，本函数也一样。
	for callee := range g.called {
		if g.fns[callee].bounded {
			sig.bounded = true
		}
	}
	g.blockDepth = savedDepth
	g.called = savedCalled
	g.popScope()
	sig.scopes = append([]int(nil), g.scopeIDs...)
	g.fns[name] = sig
	return fmt.Sprintf("fn %s(%s) {\n%s%s%s}\n", name, strings.Join(params, ", "), body, nested, ret)
}

// ---------- 程序 ----------

// program 生成一整个程序：先放几个固定模板的辅助函数
// （递归、闭包工厂、带副作用的短路探针），再接随机主语句。
func (g *diffGen) program() string {
	var b strings.Builder

	// 递归模板：fib，小参数调用，保证终止；bounded=true，
	// 调用它的函数也要跟着 bounded（见 fnSig 的注释）。
	fib := g.freshFn()
	g.fns[fib] = fnSig{params: []string{"num"}, ret: "num", scopes: append([]int(nil), g.scopeIDs...), bounded: true}
	fmt.Fprintf(&b, "fn %s(n) { if (n < 2) { return n; } return %s(n - 1) + %s(n - 2); }\n", fib, fib, fib)

	// 闭包工厂模板：返回的闭包共享同一个捕获变量。
	mk := g.freshFn()
	g.fns[mk] = fnSig{params: []string{}, ret: "fn", scopes: append([]int(nil), g.scopeIDs...)}
	fmt.Fprintf(&b, "fn %s() { let count = 0; fn inc() { count = count + 1; return count; } return inc; }\n", mk)

	// 短路探针：被调用时会打印 SIDE，用来验证 && 与 || 是否真的短路。
	side := g.freshFn()
	g.fns[side] = fnSig{params: []string{}, ret: "bool", scopes: append([]int(nil), g.scopeIDs...)}
	fmt.Fprintf(&b, "fn %s() { print \"SIDE\"; return true; }\n", side)

	// 主语句：随机生成，中间穿插对模板函数的调用。
	n := 5 + g.r.Intn(8)
	for i := 0; i < n; i++ {
		switch g.r.Intn(6) {
		case 0:
			fmt.Fprintf(&b, "print %s(%d);\n", fib, 5+g.r.Intn(6))
		case 1:
			// 两个独立的计数器闭包，验证互不干扰。
			c1, c2 := g.freshVar(), g.freshVar()
			g.declare(c1, "fn")
			g.declare(c2, "fn")
			fmt.Fprintf(&b, "let %s = %s();\nlet %s = %s();\n", c1, mk, c2, mk)
			fmt.Fprintf(&b, "print %s(); print %s(); print %s();\n", c1, c1, c2)
		case 2:
			// 短路探针：前两行不应打印 SIDE，第三行应该打印。
			fmt.Fprintf(&b, "print false && %s();\nprint true || %s();\nprint true && %s();\n", side, side, side)
		default:
			b.WriteString(g.stmt(""))
		}
	}
	return b.String()
}
