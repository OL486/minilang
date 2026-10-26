package tree

import (
	"fmt"
	"io"
	"math"
	"time"

	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/numfmt"
	"github.com/OL486/minilang/internal/token"
)

// maxCallDepth 是调用深度上限。
// 不设上限时，一个写错的递归会让 Go 的栈一路长到 OOM 或者打出一屏
// "stack overflow" 的 goroutine dump —— 那个 dump 里没有任何 minilang 的信息。
// 自己设限，就能报出「第几行调用太深」，这才是有用的信息。
const maxCallDepth = 1000

// returnSignal 用 panic 实现 return 的非局部跳转。
//
// 为什么不用 error 逐层返回：`while` 里的 `return` 要穿过 body → if → while
// 好几层循环才能回到函数调用点，每层都要写 `if err != nil` 判断。
// 用 panic 一次跳到底，代价是必须保证 recover 只在函数调用边界做 ——
// 如果中途某处误 recover，return 会被静默吞掉，函数返回 nil。
type returnSignal struct {
	value any
	pos   ast.Pos
}

// Interp 是 tree-walking 解释器。
type Interp struct {
	globals  *Env
	out      io.Writer
	depth    int
	maxDepth int
}

// New 创建解释器，并注册内置函数。
func New(out io.Writer) *Interp {
	in := &Interp{
		globals:  NewEnv(nil),
		out:      out,
		maxDepth: maxCallDepth,
	}
	in.globals.Define("clock", &NativeFn{
		Name:  "clock",
		Arity: 0,
		Fn: func([]any) (any, error) {
			// 返回毫秒时间戳的浮点数，与 minilang 的 number 类型一致。
			return float64(time.Now().UnixMilli()), nil
		},
	})
	return in
}

// Globals 暴露全局作用域，供 REPL 与测试使用。
func (in *Interp) Globals() *Env { return in.globals }

// Run 在全局作用域执行一组语句。
func (in *Interp) Run(stmts []ast.Stmt) (err error) {
	defer func() {
		if r := recover(); r != nil {
			rs, ok := r.(returnSignal)
			if !ok {
				panic(r)
			}
			err = diag.New(diag.StageRuntime, rs.pos.Line, rs.pos.Col, "return 只能在函数内部使用")
		}
	}()
	return in.execBlock(stmts, in.globals)
}

func (in *Interp) execBlock(stmts []ast.Stmt, env *Env) error {
	for _, s := range stmts {
		if err := in.execStmt(s, env); err != nil {
			return err
		}
	}
	return nil
}

func (in *Interp) execStmt(s ast.Stmt, env *Env) error {
	switch n := s.(type) {
	case *ast.LetStmt:
		v, err := in.eval(n.Init, env)
		if err != nil {
			return err
		}
		env.Define(n.Name, v)
		return nil

	case *ast.FnDecl:
		// 先定义再执行 body：这样递归函数在函数体内能查到自己的名字。
		// 如果反过来（先建闭包再 define），递归调用会在定义处查不到自己，
		// 报「未定义变量 f」—— 这是最经典的 tree-walking 闭包 bug。
		env.Define(n.Name, &Function{Decl: n, Closure: env})
		return nil

	case *ast.PrintStmt:
		v, err := in.eval(n.Value, env)
		if err != nil {
			return err
		}
		fmt.Fprintln(in.out, FormatValue(v))
		return nil

	case *ast.ExprStmt:
		_, err := in.eval(n.X, env)
		return err

	case *ast.BlockStmt:
		// 块引入新作用域。用同一个 parent 链，不复制环境：
		// 复制会让闭包捕获到一份快照，而不是活的作用域。
		return in.execBlock(n.Stmts, NewEnv(env))

	case *ast.IfStmt:
		cond, err := in.eval(n.Cond, env)
		if err != nil {
			return err
		}
		if truthy(cond) {
			return in.execBlock(n.Then, NewEnv(env))
		}
		if len(n.Else) > 0 {
			return in.execBlock(n.Else, NewEnv(env))
		}
		return nil

	case *ast.WhileStmt:
		for {
			cond, err := in.eval(n.Cond, env)
			if err != nil {
				return err
			}
			if !truthy(cond) {
				return nil
			}
			if err := in.execBlock(n.Body, NewEnv(env)); err != nil {
				return err
			}
		}

	case *ast.ReturnStmt:
		var v any
		if n.Value != nil {
			var err error
			v, err = in.eval(n.Value, env)
			if err != nil {
				return err
			}
		}
		panic(returnSignal{value: v, pos: n.P})

	default:
		return diag.New(diag.StageRuntime, s.Pos().Line, s.Pos().Col, "内部错误：未知语句类型 %T", s)
	}
}

func (in *Interp) eval(e ast.Expr, env *Env) (any, error) {
	switch n := e.(type) {
	case *ast.Literal:
		return n.Value, nil

	case *ast.Variable:
		v, ok := env.Lookup(n.Name)
		if !ok {
			return nil, diag.New(diag.StageRuntime, n.P.Line, n.P.Col, "未定义的变量 '%s'", n.Name)
		}
		return v, nil

	case *ast.Assign:
		v, err := in.eval(n.Value, env)
		if err != nil {
			return nil, err
		}
		if !env.Assign(n.Name, v) {
			return nil, diag.New(diag.StageRuntime, n.P.Line, n.P.Col, "不能给未定义的变量 '%s' 赋值（minilang 没有隐式全局变量）", n.Name)
		}
		return v, nil

	case *ast.Grouping:
		return in.eval(n.Inner, env)

	case *ast.Unary:
		v, err := in.eval(n.Right, env)
		if err != nil {
			return nil, err
		}
		switch n.Op {
		case token.Bang:
			return !truthy(v), nil
		case token.Minus:
			f, ok := v.(float64)
			if !ok {
				return nil, diag.New(diag.StageRuntime, n.P.Line, n.P.Col, "一元 - 只能作用于 number，实际是 %s", typeName(v))
			}
			return -f, nil
		}
		return nil, diag.New(diag.StageRuntime, n.P.Line, n.P.Col, "内部错误：未知一元运算符 %s", n.Op)

	case *ast.Logical:
		// 短路：左边已经决定结果时，右边根本不求值。
		// 这里返回的是操作数本身而不是 true/false —— 与 Go/Python 一致。
		// 如果改成返回 bool，`nil || "x"` 就无法用来写默认值了。
		l, err := in.eval(n.Left, env)
		if err != nil {
			return nil, err
		}
		if n.Op == token.OrOr {
			if truthy(l) {
				return l, nil
			}
		} else {
			if !truthy(l) {
				return l, nil
			}
		}
		return in.eval(n.Right, env)

	case *ast.Binary:
		l, err := in.eval(n.Left, env)
		if err != nil {
			return nil, err
		}
		r, err := in.eval(n.Right, env)
		if err != nil {
			return nil, err
		}
		return binaryOp(n.Op, l, r, n.P)

	case *ast.Call:
		callee, err := in.eval(n.Callee, env)
		if err != nil {
			return nil, err
		}
		args := make([]any, len(n.Args))
		for i, a := range n.Args {
			v, err := in.eval(a, env)
			if err != nil {
				return nil, err
			}
			args[i] = v
		}
		return in.callValue(callee, args, n.P)
	}
	return nil, diag.New(diag.StageRuntime, e.Pos().Line, e.Pos().Col, "内部错误：未知表达式类型 %T", e)
}

func (in *Interp) callValue(callee any, args []any, p ast.Pos) (any, error) {
	switch fn := callee.(type) {
	case *Function:
		if len(args) != len(fn.Decl.Params) {
			return nil, diag.New(diag.StageRuntime, p.Line, p.Col,
				"函数 %s 需要 %d 个参数，传入了 %d 个", fn.Decl.Name, len(fn.Decl.Params), len(args))
		}
		return in.callUser(fn, args)

	case *NativeFn:
		if len(args) != fn.Arity {
			return nil, diag.New(diag.StageRuntime, p.Line, p.Col,
				"内置函数 %s 需要 %d 个参数，传入了 %d 个", fn.Name, fn.Arity, len(args))
		}
		v, err := fn.Fn(args)
		if err != nil {
			return nil, diag.New(diag.StageRuntime, p.Line, p.Col, "内置函数 %s 出错: %v", fn.Name, err)
		}
		return v, nil
	}
	return nil, diag.New(diag.StageRuntime, p.Line, p.Col, "%s 不是函数，不能调用", typeName(callee))
}

func (in *Interp) callUser(fn *Function, args []any) (result any, err error) {
	if in.depth >= in.maxDepth {
		return nil, diag.New(diag.StageRuntime, fn.Decl.P.Line, fn.Decl.P.Col,
			"调用栈溢出：嵌套调用超过 %d 层", in.maxDepth)
	}
	in.depth++
	defer func() { in.depth-- }()

	// 新环境挂在函数定义时的闭包上，而不是调用者的环境上。
	// 挂错这一处，闭包就退化成动态作用域：
	// makeCounter 返回的 inc 会去读调用者的 count，两个计数器就串了。
	env := NewEnv(fn.Closure)
	for i, name := range fn.Decl.Params {
		env.Define(name, args[i])
	}

	defer func() {
		if r := recover(); r != nil {
			rs, ok := r.(returnSignal)
			if !ok {
				panic(r)
			}
			result = rs.value
			err = nil
		}
	}()
	if e := in.execBlock(fn.Decl.Body, env); e != nil {
		return nil, e
	}
	return nil, nil
}

// ---------- 语义辅助 ----------

// truthy 定义真值：只有 false 和 nil 是假，其余（含 0 和 ""）都是真。
//
// 为什么 0 是真：如果 0 是假，`if (n) {...}` 在 n 为 0 时会静默走 else 分支，
// 而 minilang 没有类型系统来提醒你 n 是 number 还是 bool。
// 只把 false/nil 当假，规则最少、最容易预测。
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	}
	return true
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "nil"
	case bool:
		return "bool"
	case float64:
		return "number"
	case string:
		return "string"
	case *Function, *NativeFn:
		return "function"
	}
	return fmt.Sprintf("%T", v)
}

func binaryOp(op token.Type, l, r any, p ast.Pos) (any, error) {
	switch op {
	case token.Plus:
		if a, ok := l.(float64); ok {
			if b, ok := r.(float64); ok {
				return a + b, nil
			}
		}
		if a, ok := l.(string); ok {
			if b, ok := r.(string); ok {
				return a + b, nil
			}
		}
		return nil, mismatch(op, l, r, p)

	case token.Minus, token.Star, token.Slash, token.Percent:
		a, ok1 := l.(float64)
		b, ok2 := r.(float64)
		if !ok1 || !ok2 {
			return nil, mismatch(op, l, r, p)
		}
		switch op {
		case token.Minus:
			return a - b, nil
		case token.Star:
			return a * b, nil
		case token.Slash:
			// 不检查除零：float64 的 1/0 是 +Inf，NaN 也是合法结果。
			// 加一个「除零错误」会让 `1/0` 和 `0/0` 走不同路径，
			// 而 IEEE 754 已经定义好了这两种情况。
			return a / b, nil
		default:
			return math.Mod(a, b), nil
		}

	case token.Eq:
		return equalValues(l, r), nil
	case token.Ne:
		return !equalValues(l, r), nil

	case token.Lt, token.Le, token.Gt, token.Ge:
		if a, ok := l.(float64); ok {
			if b, ok := r.(float64); ok {
				return compareNumbers(op, a, b), nil
			}
		}
		if a, ok := l.(string); ok {
			if b, ok := r.(string); ok {
				return compareStrings(op, a, b), nil
			}
		}
		return nil, mismatch(op, l, r, p)
	}
	return nil, diag.New(diag.StageRuntime, p.Line, p.Col, "内部错误：未知二元运算符 %s", op)
}

func compareNumbers(op token.Type, a, b float64) bool {
	switch op {
	case token.Lt:
		return a < b
	case token.Le:
		return a <= b
	case token.Gt:
		return a > b
	default:
		return a >= b
	}
}

func compareStrings(op token.Type, a, b string) bool {
	switch op {
	case token.Lt:
		return a < b
	case token.Le:
		return a <= b
	case token.Gt:
		return a > b
	default:
		return a >= b
	}
}

func mismatch(op token.Type, l, r any, p ast.Pos) error {
	return diag.New(diag.StageRuntime, p.Line, p.Col,
		"运算符 %s 不支持 %s 与 %s", opText(op), typeName(l), typeName(r))
}

func opText(t token.Type) string {
	switch t {
	case token.Plus:
		return "+"
	case token.Minus:
		return "-"
	case token.Star:
		return "*"
	case token.Slash:
		return "/"
	case token.Percent:
		return "%"
	case token.Eq:
		return "=="
	case token.Ne:
		return "!="
	case token.Lt:
		return "<"
	case token.Le:
		return "<="
	case token.Gt:
		return ">"
	case token.Ge:
		return ">="
	}
	return t.String()
}

// equalValues 是 == 的语义：不同类型之间比较一律 false，不报错。
//
// 为什么跨类型比较不报错：`nil == f()` 是很常见的写法，
// 如果 number 与 nil 比较就报错，这段代码没法写。
// 而 `<` 这类大小比较必须同类型，所以那边会报错。
func equalValues(l, r any) bool {
	switch a := l.(type) {
	case nil:
		return r == nil
	case bool:
		b, ok := r.(bool)
		return ok && a == b
	case float64:
		b, ok := r.(float64)
		return ok && a == b
	case string:
		b, ok := r.(string)
		return ok && a == b
	}
	// 函数值按身份比较：同一个函数对象才相等。
	return l == r
}

// FormatValue 把内部值渲染成 print 的输出。
// 与 VM 侧的 engine.Format 必须逐字节一致，否则两个引擎的 diff 回归会失效。
func FormatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return numfmt.Number(x)
	case string:
		return x
	case *Function:
		return "<fn " + x.Decl.Name + ">"
	case *NativeFn:
		return "<native fn " + x.Name + ">"
	}
	return fmt.Sprintf("%v", v)
}
