package compiler

import (
	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/vm"
)

// ---------- 语句 ----------

func (c *Compiler) stmt(s ast.Stmt) {
	switch n := s.(type) {
	case *ast.LetStmt:
		c.expr(n.Init)
		if c.fn.scopeDepth == 0 {
			// 顶层 let 编译成全局变量，而不是脚本函数的局部变量。
			// 这样 REPL 里连续输入的两行才能互相看见 —— 每行是独立编译的脚本函数，
			// 局部槽位在脚本结束后就没了。
			idx := c.identifierConstant(n.Name, n.P.Line)
			c.emit(vm.OpDefineGlobal, n.P.Line)
			c.emitByte(byte(idx), n.P.Line)
		} else {
			// 局部变量：初始化表达式的值已经在栈顶，那个位置就是它的槽位。
			// 不需要额外的「存」指令 —— 这是栈式 VM 最省事的一点。
			c.declareLocal(n.Name, n.P.Line)
		}

	case *ast.FnDecl:
		c.fnDecl(n)

	case *ast.PrintStmt:
		c.expr(n.Value)
		c.emit(vm.OpPrint, n.P.Line)

	case *ast.ExprStmt:
		c.expr(n.X)
		c.emit(vm.OpPop, n.P.Line)

	case *ast.BlockStmt:
		c.beginScope()
		for _, st := range n.Stmts {
			c.stmt(st)
		}
		c.endScope(n.P.Line)

	case *ast.IfStmt:
		c.ifStmt(n)

	case *ast.WhileStmt:
		c.whileStmt(n)

	case *ast.ReturnStmt:
		if n.Value == nil {
			c.emit(vm.OpNil, n.P.Line)
		} else {
			c.expr(n.Value)
		}
		c.emit(vm.OpReturn, n.P.Line)

	default:
		c.errorAt(s.Pos().Line, "内部错误：未知语句类型 %T", s)
	}
}

func (c *Compiler) ifStmt(n *ast.IfStmt) {
	line := n.P.Line
	c.expr(n.Cond)
	thenJump := c.emitJump(vm.OpJumpIfFalse, line)

	// 条件值还在栈上，then 分支进来第一件事就是把它弹掉。
	// OP_JUMP_IF_FALSE 故意不弹栈：它既要「保留条件值给 then 用」，
	// 又要「跳到 else 时让 else 去弹」—— 两边各弹一次，指令数最少。
	c.emit(vm.OpPop, line)
	c.beginScope()
	for _, st := range n.Then {
		c.stmt(st)
	}
	c.endScope(line)

	elseJump := c.emitJump(vm.OpJump, line)
	c.patchJump(thenJump)

	c.emit(vm.OpPop, line)
	if len(n.Else) > 0 {
		c.beginScope()
		for _, st := range n.Else {
			c.stmt(st)
		}
		c.endScope(line)
	}
	c.patchJump(elseJump)
}

func (c *Compiler) whileStmt(n *ast.WhileStmt) {
	line := n.P.Line
	loopStart := len(c.chunk().Code)

	c.expr(n.Cond)
	exitJump := c.emitJump(vm.OpJumpIfFalse, line)
	c.emit(vm.OpPop, line)

	c.beginScope()
	for _, st := range n.Body {
		c.stmt(st)
	}
	c.endScope(line)

	c.emitLoop(loopStart, line)
	c.patchJump(exitJump)
	c.emit(vm.OpPop, line) // 条件为假时把条件值弹掉
}

func (c *Compiler) fnDecl(n *ast.FnDecl) {
	isLocal := c.fn.scopeDepth > 0
	if isLocal {
		// 先声明槽位再编译函数体，这样函数体里的递归调用
		// `fib(n-1)` 能解析到自己的槽位。反过来写就会报「未定义变量 fib」。
		c.declareLocal(n.Name, n.P.Line)
	}

	fn, upvals := c.compileFunction(n)

	// 常量必须在 PopRoot 之前加进去：compileFunction 已经把 fn 挂到根上，
	// 此刻把 fn 放进当前函数的常量池，之后就由常量池（间接由当前函数）保活。
	idx := c.addConstant(vm.ObjValue(fn), n.P.Line)
	c.vm.PopRoot()

	c.emit(vm.OpClosure, n.P.Line)
	c.emitByte(byte(idx), n.P.Line)
	for _, uv := range upvals {
		if uv.isLocal {
			c.emitByte(1, n.P.Line)
		} else {
			c.emitByte(0, n.P.Line)
		}
		c.emitByte(uv.index, n.P.Line)
	}

	if !isLocal {
		nameIdx := c.identifierConstant(n.Name, n.P.Line)
		c.emit(vm.OpDefineGlobal, n.P.Line)
		c.emitByte(byte(nameIdx), n.P.Line)
	}
	// 局部函数不需要额外指令：OP_CLOSURE 已经把闭包压在预留的槽位上。
}

// compileFunction 编译一个函数体，返回函数对象与它的 upvalue 描述表。
//
// 约定：本函数把 fn 压入临时根栈，由调用方负责 PopRoot。
// 这样「编译完 → 放进常量池」之间不会被 GC 打断。
func (c *Compiler) compileFunction(n *ast.FnDecl) (*vm.ObjFunction, []upvalueDesc) {
	fn := c.vm.NewFunction(n.Name, len(n.Params))
	c.vm.PushRoot(vm.ObjValue(fn))

	outer := c.fn
	inner := &fnState{
		enclosing:  outer,
		fn:         fn,
		locals:     []local{{name: "", depth: 0}},
		scopeDepth: 1, // 函数体天然是一个作用域，参数与函数体同级
	}
	c.fn = inner
	for _, p := range n.Params {
		c.declareLocal(p, n.P.Line)
	}
	for _, st := range n.Body {
		c.stmt(st)
	}
	c.emitReturn(n.P.Line)

	fn.UpvalueCount = len(inner.upvalues)
	c.fn = outer
	return fn, inner.upvalues
}
