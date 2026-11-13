package tree

import (
	"fmt"
	"io"
	"time"

	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/semantics"
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
		if semantics.Truthy(cond) {
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
			if !semantics.Truthy(cond) {
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
			return !semantics.Truthy(v), nil
		case token.Minus:
			f, ok := v.(float64)
			if !ok {
				return nil, diag.New(diag.StageRuntime, n.P.Line, n.P.Col, "一元 - 只能作用于 number，实际是 %s", semantics.TypeName(v))
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
			if semantics.Truthy(l) {
				return l, nil
			}
		} else {
			if !semantics.Truthy(l) {
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
	return nil, diag.New(diag.StageRuntime, p.Line, p.Col, "%s 不是函数，不能调用", semantics.TypeName(callee))
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
//
// 真值、类型名、==、二元运算、运算符文本的唯一实现在 internal/semantics，
// 这里只剩薄适配：二元运算的 diag 错误包装，以及函数值的打印分支
// （tree 的函数类型与 VM 的不同，原始值部分由 semantics.FormatPrimitive 负责）。

func binaryOp(op token.Type, l, r any, p ast.Pos) (any, error) {
	if !semantics.IsBinaryOperator(op) {
		return nil, diag.New(diag.StageRuntime, p.Line, p.Col, "内部错误：未知二元运算符 %s", op)
	}
	if v, ok := semantics.BinaryOp(op, l, r); ok {
		return v, nil
	}
	return nil, mismatch(op, l, r, p)
}

func mismatch(op token.Type, l, r any, p ast.Pos) error {
	return diag.New(diag.StageRuntime, p.Line, p.Col,
		"运算符 %s 不支持 %s 与 %s", token.OpText(op), semantics.TypeName(l), semantics.TypeName(r))
}

// FormatValue 把内部值渲染成 print 的输出。
// 与 VM 侧的 engine.Format 必须逐字节一致，否则两个引擎的 diff 回归会失效。
// 原始值部分由 semantics.FormatPrimitive 实现，这里只补函数值分支。
func FormatValue(v any) string {
	if s, ok := semantics.FormatPrimitive(v); ok {
		return s
	}
	switch x := v.(type) {
	case *Function:
		return semantics.FormatFunc(x.Decl.Name, false)
	case *NativeFn:
		return semantics.FormatFunc(x.Name, true)
	}
	return fmt.Sprintf("%v", v)
}
