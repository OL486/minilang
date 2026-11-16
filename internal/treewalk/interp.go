package treewalk

import (
	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/semantics"
	"io"
	"time"
)

// 调用深度上限与 VM 侧共用 semantics.MaxCallDepth（见 B2 的说明）。
// 不设上限时，一个写错的递归会让 Go 的栈一路长到 OOM 或者打出一屏
// "stack overflow" 的 goroutine dump —— 那个 dump 里没有任何 minilang 的信息。
// 自己设限，就能报出「第几行调用太深」，这才是有用的信息。

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
	// steps 是本次运行已求值的表达式数；超过 semantics.MaxExecSteps 时
	// eval() 报运行时错误。每次 Run() 开始时清零。
	// 每个 while 迭代至少求值一次条件表达式，所以任何无限循环都会撞上限。
	steps int
}

// New 创建解释器，并注册内置函数。
func New(out io.Writer) *Interp {
	in := &Interp{
		globals:  NewEnv(nil),
		out:      out,
		maxDepth: semantics.MaxCallDepth,
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

// Steps 返回本次运行已求值的表达式步数。
func (in *Interp) Steps() int { return in.steps }

// Run 在全局作用域执行一组语句。
func (in *Interp) Run(stmts []ast.Stmt) (err error) {
	in.steps = 0
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
