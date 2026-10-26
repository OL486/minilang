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
	"github.com/OL486/minilang/internal/engine"
	"github.com/OL486/minilang/internal/token"
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
	fn         *engine.ObjFunction
	locals     []local
	upvalues   []upvalueDesc
	scopeDepth int
}

// Compiler 是字节码编译器。
type Compiler struct {
	vm   *engine.VM
	fn   *fnState
	errs []*diag.Error
}

// compileAbort 用于从深层递归直接退出编译。
// 编译错误之后继续生成字节码没有意义（槽位表可能已经乱了），
// 立刻中止比「尽量多报几个错」更安全。
type compileAbort struct{}

// CompileProgram 编译整个程序，返回脚本函数。
func CompileProgram(vm *engine.VM, stmts []ast.Stmt) (fn *engine.ObjFunction, errs []*diag.Error) {
	c := &Compiler{vm: vm}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(compileAbort); !ok {
				panic(r)
			}
			fn = nil
			errs = c.errs
			vm.ClearRoots()
		}
	}()

	script := vm.NewFunction("", 0)
	// 立刻上根：编译过程中会创建大量字符串常量，随时可能触发 GC。
	vm.PushRoot(engine.ObjValue(script))
	// 槽位 0 固定给脚本闭包自身。运行期 slotBase 指向它，
	// 所以脚本的局部变量从槽位 1 开始 —— 与函数完全一致，不需要特例。
	c.fn = &fnState{fn: script, locals: []local{{name: "", depth: 0}}}
	for _, s := range stmts {
		c.stmt(s)
	}
	c.emitReturn(lastLine(stmts))
	vm.PopRoot()

	if len(c.errs) > 0 {
		vm.ClearRoots()
		return nil, c.errs
	}
	return script, nil
}

// ---------- 发射原语 ----------

func (c *Compiler) chunk() *engine.Chunk { return c.fn.fn.Chunk }

func (c *Compiler) emit(op engine.Op, line int) {
	c.chunk().Write(byte(op), line)
}

func (c *Compiler) emitByte(b byte, line int) {
	c.chunk().Write(b, line)
}

func (c *Compiler) emitShort(v int, line int) {
	c.chunk().Write(byte(v>>8), line)
	c.chunk().Write(byte(v), line)
}

func (c *Compiler) errorAt(line int, format string, args ...any) {
	c.errs = append(c.errs, diag.New(diag.StageCompile, line, 0, format, args...))
	panic(compileAbort{})
}

func (c *Compiler) addConstant(v engine.Value, line int) int {
	idx := c.chunk().AddConstant(v)
	if idx < 0 {
		c.errorAt(line, "函数 %s 的常量池超过 %d 项（单字节操作数上限）", displayFnName(c.fn.fn.Name), maxConstants)
	}
	return idx
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

func (c *Compiler) identifierConstant(name string, line int) int {
	s := c.vm.InternString(name)
	return c.addConstant(engine.ObjValue(s), line)
}

// emitJump 发射一个 16 位占位跳转，返回操作数位置。
//
// 为什么必须先发 0xff 0xff 再回填：编译到 if/while 时，跳转目标还不存在。
// 只有等分支体编译完，才知道目标偏移。回填（patch）是这个模式的名字。
// 另一种写法是「先记录位置，最后统一重排」—— 那需要两遍编译，得不偿失。
func (c *Compiler) emitJump(op engine.Op, line int) int {
	c.emit(op, line)
	c.emitByte(0xff, line)
	c.emitByte(0xff, line)
	return len(c.chunk().Code) - 2
}

// patchJump 回填跳转偏移。
//
// 偏移量是「相对操作数之后那个字节」算的：jump = 当前位置 - 操作数位置 - 2。
// 少减那个 2 是最常见的 off-by-one：所有 if 分支都会多执行两条指令，
// 表现为「else 分支总被跳过一条语句」，而且只在多分支时才明显。
func (c *Compiler) patchJump(offset int) {
	jump := len(c.chunk().Code) - offset - 2
	if jump > 0xffff {
		c.errorAt(c.chunk().LineAt(offset), "跳转距离超过 65535 字节（函数体过大）")
	}
	c.chunk().Code[offset] = byte(jump >> 8)
	c.chunk().Code[offset+1] = byte(jump)
}

func (c *Compiler) emitLoop(loopStart, line int) {
	c.emit(engine.OpLoop, line)
	// +2 是操作数自身的长度：VM 在读完操作数后 ip 已经指向下一条指令，
	// 要减掉「opcode + 2 字节操作数」才能回到循环开头。
	offset := len(c.chunk().Code) - loopStart + 2
	if offset > 0xffff {
		c.errorAt(line, "循环体超过 65535 字节")
	}
	c.emitShort(offset, line)
}

func (c *Compiler) emitReturn(line int) {
	c.emit(engine.OpNil, line)
	c.emit(engine.OpReturn, line)
}

// ---------- 作用域 ----------

func (c *Compiler) beginScope() { c.fn.scopeDepth++ }

func (c *Compiler) endScope(line int) {
	c.fn.scopeDepth--
	for len(c.fn.locals) > 0 && c.fn.locals[len(c.fn.locals)-1].depth > c.fn.scopeDepth {
		if c.fn.locals[len(c.fn.locals)-1].captured {
			// 被闭包捕获过的局部变量不能简单 POP：必须先把值搬到堆上，
			// 否则栈槽被后续代码复用时，闭包看到的就是别人的值。
			c.emit(engine.OpCloseUpvalue, line)
		} else {
			c.emit(engine.OpPop, line)
		}
		c.fn.locals = c.fn.locals[:len(c.fn.locals)-1]
	}
}

func (c *Compiler) declareLocal(name string, line int) {
	for i := len(c.fn.locals) - 1; i >= 0; i-- {
		if c.fn.locals[i].depth < c.fn.scopeDepth {
			break
		}
		if c.fn.locals[i].name == name {
			c.errorAt(line, "同一作用域内重复声明变量 '%s'", name)
		}
	}
	if len(c.fn.locals) >= maxLocals {
		c.errorAt(line, "局部变量超过 %d 个", maxLocals)
	}
	c.fn.locals = append(c.fn.locals, local{name: name, depth: c.fn.scopeDepth})
}

// resolveLocal 在当前函数里找变量，返回栈槽位（= locals 下标）。
//
// 为什么用绝对槽位而不是「相对栈顶偏移」：
// 相对偏移的写法在赋值时特别容易差一 —— 编译期算出「栈顶往下第 2 个」，
// 运行期 OP_SET_LOCAL 执行时栈顶可能已经因为压入右值而上移了一格，
// 结果写到了隔壁变量上。这类 bug 只在「赋值表达式的值被用到」时出现，
// 比如 `x = y = 1` 或者 `print x = 1`，非常难复现。
// 绝对槽位 + slotBase 的组合让「槽位」与「栈顶」彻底解耦：
// 不管栈顶在哪，第 n 个局部变量永远是 stack[slotBase+n]。
func (c *Compiler) resolveLocal(fs *fnState, name string) int {
	for i := len(fs.locals) - 1; i >= 0; i-- {
		if fs.locals[i].name == name {
			return i
		}
	}
	return -1
}

// resolveUpvalue 沿函数嵌套链向外找变量。
//
// 递归是关键：如果当前函数没找到，先看外层函数的局部变量（捕获它），
// 再看外层函数自己的 upvalue（传递捕获）。
// 只有一层的话，三层嵌套闭包的最内层就拿不到最外层的变量。
func (c *Compiler) resolveUpvalue(fs *fnState, name string) int {
	if fs.enclosing == nil {
		return -1
	}
	if idx := c.resolveLocal(fs.enclosing, name); idx >= 0 {
		fs.enclosing.locals[idx].captured = true
		return c.addUpvalue(fs, uint8(idx), true)
	}
	if idx := c.resolveUpvalue(fs.enclosing, name); idx >= 0 {
		return c.addUpvalue(fs, uint8(idx), false)
	}
	return -1
}

func (c *Compiler) addUpvalue(fs *fnState, index uint8, isLocal bool) int {
	for i, uv := range fs.upvalues {
		if uv.index == index && uv.isLocal == isLocal {
			return i
		}
	}
	if len(fs.upvalues) >= maxUpvalues {
		c.errorAt(0, "upvalue 超过 %d 个", maxUpvalues)
	}
	fs.upvalues = append(fs.upvalues, upvalueDesc{index: index, isLocal: isLocal})
	return len(fs.upvalues) - 1
}

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
			c.emit(engine.OpDefineGlobal, n.P.Line)
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
		c.emit(engine.OpPrint, n.P.Line)

	case *ast.ExprStmt:
		c.expr(n.X)
		c.emit(engine.OpPop, n.P.Line)

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
			c.emit(engine.OpNil, n.P.Line)
		} else {
			c.expr(n.Value)
		}
		c.emit(engine.OpReturn, n.P.Line)

	default:
		c.errorAt(s.Pos().Line, "内部错误：未知语句类型 %T", s)
	}
}

func (c *Compiler) ifStmt(n *ast.IfStmt) {
	line := n.P.Line
	c.expr(n.Cond)
	thenJump := c.emitJump(engine.OpJumpIfFalse, line)

	// 条件值还在栈上，then 分支进来第一件事就是把它弹掉。
	// OP_JUMP_IF_FALSE 故意不弹栈：它既要「保留条件值给 then 用」，
	// 又要「跳到 else 时让 else 去弹」—— 两边各弹一次，指令数最少。
	c.emit(engine.OpPop, line)
	c.beginScope()
	for _, st := range n.Then {
		c.stmt(st)
	}
	c.endScope(line)

	elseJump := c.emitJump(engine.OpJump, line)
	c.patchJump(thenJump)

	c.emit(engine.OpPop, line)
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
	exitJump := c.emitJump(engine.OpJumpIfFalse, line)
	c.emit(engine.OpPop, line)

	c.beginScope()
	for _, st := range n.Body {
		c.stmt(st)
	}
	c.endScope(line)

	c.emitLoop(loopStart, line)
	c.patchJump(exitJump)
	c.emit(engine.OpPop, line) // 条件为假时把条件值弹掉
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
	idx := c.addConstant(engine.ObjValue(fn), n.P.Line)
	c.vm.PopRoot()

	c.emit(engine.OpClosure, n.P.Line)
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
		c.emit(engine.OpDefineGlobal, n.P.Line)
		c.emitByte(byte(nameIdx), n.P.Line)
	}
	// 局部函数不需要额外指令：OP_CLOSURE 已经把闭包压在预留的槽位上。
}

// compileFunction 编译一个函数体，返回函数对象与它的 upvalue 描述表。
//
// 约定：本函数把 fn 压入临时根栈，由调用方负责 PopRoot。
// 这样「编译完 → 放进常量池」之间不会被 GC 打断。
func (c *Compiler) compileFunction(n *ast.FnDecl) (*engine.ObjFunction, []upvalueDesc) {
	fn := c.vm.NewFunction(n.Name, len(n.Params))
	c.vm.PushRoot(engine.ObjValue(fn))

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

// ---------- 表达式 ----------

func (c *Compiler) expr(e ast.Expr) {
	switch n := e.(type) {
	case *ast.Literal:
		c.literal(n)

	case *ast.Variable:
		if slot := c.resolveLocal(c.fn, n.Name); slot >= 0 {
			c.emit(engine.OpGetLocal, n.P.Line)
			c.emitByte(byte(slot), n.P.Line)
			return
		}
		if uv := c.resolveUpvalue(c.fn, n.Name); uv >= 0 {
			c.emit(engine.OpGetUpvalue, n.P.Line)
			c.emitByte(byte(uv), n.P.Line)
			return
		}
		idx := c.identifierConstant(n.Name, n.P.Line)
		c.emit(engine.OpGetGlobal, n.P.Line)
		c.emitByte(byte(idx), n.P.Line)

	case *ast.Assign:
		c.expr(n.Value)
		if slot := c.resolveLocal(c.fn, n.Name); slot >= 0 {
			c.emit(engine.OpSetLocal, n.P.Line)
			c.emitByte(byte(slot), n.P.Line)
			return
		}
		if uv := c.resolveUpvalue(c.fn, n.Name); uv >= 0 {
			c.emit(engine.OpSetUpvalue, n.P.Line)
			c.emitByte(byte(uv), n.P.Line)
			return
		}
		idx := c.identifierConstant(n.Name, n.P.Line)
		c.emit(engine.OpSetGlobal, n.P.Line)
		c.emitByte(byte(idx), n.P.Line)

	case *ast.Grouping:
		c.expr(n.Inner)

	case *ast.Unary:
		c.expr(n.Right)
		switch n.Op {
		case token.Bang:
			c.emit(engine.OpNot, n.P.Line)
		case token.Minus:
			c.emit(engine.OpNegate, n.P.Line)
		}

	case *ast.Binary:
		c.expr(n.Left)
		c.expr(n.Right)
		line := n.P.Line
		switch n.Op {
		case token.Plus:
			c.emit(engine.OpAdd, line)
		case token.Minus:
			c.emit(engine.OpSubtract, line)
		case token.Star:
			c.emit(engine.OpMultiply, line)
		case token.Slash:
			c.emit(engine.OpDivide, line)
		case token.Percent:
			c.emit(engine.OpModulo, line)
		case token.Eq:
			c.emit(engine.OpEqual, line)
		case token.Ne:
			// 没有 OP_NOT_EQUAL：复用 OP_EQUAL + OP_NOT。
			// 多一个 opcode 就要多一条分派分支，而「取反」本身已经存在。
			c.emit(engine.OpEqual, line)
			c.emit(engine.OpNot, line)
		case token.Gt:
			c.emit(engine.OpGreater, line)
		case token.Ge:
			c.emit(engine.OpGreaterEqual, line)
		case token.Lt:
			c.emit(engine.OpLess, line)
		case token.Le:
			c.emit(engine.OpLessEqual, line)
		}

	case *ast.Logical:
		c.logical(n)

	case *ast.Call:
		c.expr(n.Callee)
		for _, a := range n.Args {
			c.expr(a)
		}
		if len(n.Args) > 255 {
			c.errorAt(n.P.Line, "调用实参超过 255 个")
		}
		c.emit(engine.OpCall, n.P.Line)
		c.emitByte(byte(len(n.Args)), n.P.Line)

	default:
		c.errorAt(e.Pos().Line, "内部错误：未知表达式类型 %T", e)
	}
}

func (c *Compiler) literal(n *ast.Literal) {
	switch v := n.Value.(type) {
	case nil:
		c.emit(engine.OpNil, n.P.Line)
	case bool:
		if v {
			c.emit(engine.OpTrue, n.P.Line)
		} else {
			c.emit(engine.OpFalse, n.P.Line)
		}
	case float64:
		idx := c.addConstant(engine.NumberValue(v), n.P.Line)
		c.emit(engine.OpConstant, n.P.Line)
		c.emitByte(byte(idx), n.P.Line)
	case string:
		// 字符串常量也走驻留表：这样运行期比较两个同内容字符串时指针相等。
		s := c.vm.InternString(v)
		idx := c.addConstant(engine.ObjValue(s), n.P.Line)
		c.emit(engine.OpConstant, n.P.Line)
		c.emitByte(byte(idx), n.P.Line)
	default:
		c.errorAt(n.P.Line, "内部错误：未知字面量类型 %T", n.Value)
	}
}

// logical 编译 && 和 ||。
//
// 关键点：右边必须「有条件地」求值，所以不能用 Binary 那套「先算两边再算结果」。
// && 的字节码形状：
//
//	<left>
//	OP_JUMP_IF_FALSE end   // 左边假 → 结果就是左边，直接跳出
//	OP_POP                 // 左边真 → 丢掉它，右边才是结果
//	<right>
//	end:
//
// || 多一条无条件跳转：左边真时要把左边的值原样留下。
func (c *Compiler) logical(n *ast.Logical) {
	line := n.P.Line
	c.expr(n.Left)

	if n.Op == token.AndAnd {
		endJump := c.emitJump(engine.OpJumpIfFalse, line)
		c.emit(engine.OpPop, line)
		c.expr(n.Right)
		c.patchJump(endJump)
		return
	}

	elseJump := c.emitJump(engine.OpJumpIfFalse, line)
	endJump := c.emitJump(engine.OpJump, line)
	c.patchJump(elseJump)
	c.emit(engine.OpPop, line)
	c.expr(n.Right)
	c.patchJump(endJump)
}
