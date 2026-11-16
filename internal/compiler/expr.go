package compiler

import (
	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/token"
	"github.com/OL486/minilang/internal/vm"
)

// ---------- 表达式 ----------

func (c *Compiler) expr(e ast.Expr) {
	switch n := e.(type) {
	case *ast.Literal:
		c.literal(n)

	case *ast.Variable:
		if slot := c.resolveLocal(c.fn, n.Name); slot >= 0 {
			c.emit(vm.OpGetLocal, n.P)
			c.emitByte(byte(slot), n.P)
			return
		}
		if uv := c.resolveUpvalue(c.fn, n.Name); uv >= 0 {
			c.emit(vm.OpGetUpvalue, n.P)
			c.emitByte(byte(uv), n.P)
			return
		}
		idx := c.identifierConstant(n.Name, n.P)
		c.emit(vm.OpGetGlobal, n.P)
		c.emitByte(byte(idx), n.P)

	case *ast.Assign:
		c.expr(n.Value)
		if slot := c.resolveLocal(c.fn, n.Name); slot >= 0 {
			c.emit(vm.OpSetLocal, n.P)
			c.emitByte(byte(slot), n.P)
			return
		}
		if uv := c.resolveUpvalue(c.fn, n.Name); uv >= 0 {
			c.emit(vm.OpSetUpvalue, n.P)
			c.emitByte(byte(uv), n.P)
			return
		}
		idx := c.identifierConstant(n.Name, n.P)
		c.emit(vm.OpSetGlobal, n.P)
		c.emitByte(byte(idx), n.P)

	case *ast.Grouping:
		c.expr(n.Inner)

	case *ast.Unary:
		c.expr(n.Right)
		switch n.Op {
		case token.Bang:
			c.emit(vm.OpNot, n.P)
		case token.Minus:
			c.emit(vm.OpNegate, n.P)
		}

	case *ast.Binary:
		c.expr(n.Left)
		c.expr(n.Right)
		pos := n.P
		switch n.Op {
		case token.Plus:
			c.emit(vm.OpAdd, pos)
		case token.Minus:
			c.emit(vm.OpSubtract, pos)
		case token.Star:
			c.emit(vm.OpMultiply, pos)
		case token.Slash:
			c.emit(vm.OpDivide, pos)
		case token.Percent:
			c.emit(vm.OpModulo, pos)
		case token.Eq:
			c.emit(vm.OpEqual, pos)
		case token.Ne:
			// 没有 OP_NOT_EQUAL：复用 OP_EQUAL + OP_NOT。
			// 多一个 opcode 就要多一条分派分支，而「取反」本身已经存在。
			c.emit(vm.OpEqual, pos)
			c.emit(vm.OpNot, pos)
		case token.Gt:
			c.emit(vm.OpGreater, pos)
		case token.Ge:
			c.emit(vm.OpGreaterEqual, pos)
		case token.Lt:
			c.emit(vm.OpLess, pos)
		case token.Le:
			c.emit(vm.OpLessEqual, pos)
		}

	case *ast.Logical:
		c.logical(n)

	case *ast.Call:
		c.expr(n.Callee)
		for _, a := range n.Args {
			c.expr(a)
		}
		if len(n.Args) > 255 {
			c.errorAt(n.P, "调用实参超过 255 个")
		}
		c.emit(vm.OpCall, n.P)
		c.emitByte(byte(len(n.Args)), n.P)

	default:
		c.errorAt(e.Pos(), "内部错误：未知表达式类型 %T", e)
	}
}

func (c *Compiler) literal(n *ast.Literal) {
	switch v := n.Value.(type) {
	case nil:
		c.emit(vm.OpNil, n.P)
	case bool:
		if v {
			c.emit(vm.OpTrue, n.P)
		} else {
			c.emit(vm.OpFalse, n.P)
		}
	case float64:
		idx := c.addConstant(vm.NumberValue(v), n.P)
		c.emit(vm.OpConstant, n.P)
		c.emitByte(byte(idx), n.P)
	case string:
		// 字符串常量也走驻留表：同一内容复用同一对象，省内存。
		// == 本身按内容比较（见 vm.valuesEqual），不依赖指针相等。
		s := c.alloc.InternString(v)
		idx := c.addConstant(vm.ObjValue(s), n.P)
		c.emit(vm.OpConstant, n.P)
		c.emitByte(byte(idx), n.P)
	default:
		c.errorAt(n.P, "内部错误：未知字面量类型 %T", n.Value)
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
	pos := n.P
	c.expr(n.Left)

	if n.Op == token.AndAnd {
		endJump := c.emitJump(vm.OpJumpIfFalse, pos)
		c.emit(vm.OpPop, pos)
		c.expr(n.Right)
		c.patchJump(endJump)
		return
	}

	elseJump := c.emitJump(vm.OpJumpIfFalse, pos)
	endJump := c.emitJump(vm.OpJump, pos)
	c.patchJump(elseJump)
	c.emit(vm.OpPop, pos)
	c.expr(n.Right)
	c.patchJump(endJump)
}
