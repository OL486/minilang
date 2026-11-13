// Package compiler 把语法树编译成字节码。
//
// 与 Crafting Interpreters 的 clox 最大的结构差异：clox 是「token → 字节码」单遍扫描，
// 这里复用 S2 产出的 AST，做「AST → 字节码」。
// 多花一次 AST 的内存，换来三件事：
//  1. --dump-ast 和 --dump-bytecode 看到的是同一棵树，不会出现「AST 说 A，字节码按 B 生成」；
//  2. 语法错误只在 parser 里报一次，编译器只需要关心语义错误；
//  3. 变量名解析（局部/upvalue/全局）可以在树上做，不需要在扫描 token 时维护局部变量栈。
//
// 局部变量的槽位采用「绝对下标」而不是「相对栈顶偏移」，理由见 resolveLocal 的注释。
package compiler

import (
	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/vm"
)

const (
	maxConstants = 256
	maxLocals    = 256
	maxUpvalues  = 256
)

// local 是一个编译期的局部变量记录。
// 它在 locals 切片里的下标就是运行期的栈槽位 —— 这两个东西必须严格对应，
// 对不上的后果就是「赋值给了隔壁变量」。
type local struct {
	name     string
	depth    int  // 声明时的作用域深度
	captured bool // 是否被内层函数捕获（决定退出作用域时发 POP 还是 CLOSE_UPVALUE）
}

// upvalueDesc 描述「如何从外层拿到一个变量」，会被写进 OP_CLOSURE 的指令流。
type upvalueDesc struct {
	index   uint8
	isLocal bool // true = 捕获外层函数的局部槽位；false = 捕获外层的 upvalue
}

// fnState 是一个函数（或脚本）的编译状态。
type fnState struct {
	enclosing  *fnState
	fn         *vm.ObjFunction
	locals     []local
	upvalues   []upvalueDesc
	scopeDepth int
}

// Compiler 是字节码编译器。
type Compiler struct {
	vm   *vm.VM
	fn   *fnState
	errs []*diag.Error
}

// compileAbort 用于从深层递归直接退出编译。
// 编译错误之后继续生成字节码没有意义（槽位表可能已经乱了），
// 立刻中止比「尽量多报几个错」更安全。
type compileAbort struct{}

// CompileProgram 编译整个程序，返回脚本函数。
func CompileProgram(v *vm.VM, stmts []ast.Stmt) (fn *vm.ObjFunction, errs []*diag.Error) {
	c := &Compiler{vm: v}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(compileAbort); !ok {
				panic(r)
			}
			fn = nil
			errs = c.errs
			v.ClearRoots()
		}
	}()

	script := v.NewFunction("", 0)
	// 立刻上根：编译过程中会创建大量字符串常量，随时可能触发 GC。
	v.PushRoot(vm.ObjValue(script))
	// 槽位 0 固定给脚本闭包自身。运行期 slotBase 指向它，
	// 所以脚本的局部变量从槽位 1 开始 —— 与函数完全一致，不需要特例。
	c.fn = &fnState{fn: script, locals: []local{{name: "", depth: 0}}}
	for _, s := range stmts {
		c.stmt(s)
	}
	c.emitReturn(lastLine(stmts))
	v.PopRoot()

	if len(c.errs) > 0 {
		v.ClearRoots()
		return nil, c.errs
	}
	return script, nil
}

// lastLine 用于给「函数结尾的隐式 return」分配一个行号。
// 全用 0 的话，--dump-bytecode 里最后两条指令会显示第 0 行，
// 一旦在这里触发运行时错误（比如栈溢出），报出来的行号就是错的。
func lastLine(stmts []ast.Stmt) int {
	if len(stmts) == 0 {
		return 1
	}
	return stmts[len(stmts)-1].Pos().Line
}

func displayFnName(n string) string {
	if n == "" {
		return "<script>"
	}
	return n
}
