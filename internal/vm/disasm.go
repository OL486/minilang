package vm

import (
	"fmt"
	"strings"
)

// ---------- 反汇编 ----------
//
// 反汇编必须递归到嵌套函数。只打印顶层 chunk 的话，
// 闭包相关的 bug 完全没法看 —— 而闭包恰恰是 S8 唯一需要调试的东西。
// 这里按「常量池里出现 ObjFunction」递归，嵌套层级用缩进表示。

// Disassemble 输出整段字节码（含所有嵌套函数）。
func (c *Chunk) Disassemble(name string) string {
	var b strings.Builder
	c.dump(&b, name, 0)
	return b.String()
}

func (c *Chunk) dump(b *strings.Builder, name string, depth int) {
	pad := strings.Repeat("  ", depth)
	if name == "" {
		name = "<script>"
	}
	fmt.Fprintf(b, "%s== %s ==\n", pad, name)
	for offset := 0; offset < len(c.Code); {
		offset = c.disassembleInstruction(b, offset, pad)
	}
	for _, k := range c.Constants {
		if fn, ok := k.AsObject().(*ObjFunction); ok && fn != nil {
			b.WriteString("\n")
			fn.Chunk.dump(b, displayName(fn.Name), depth+1)
		}
	}
}

func (c *Chunk) disassembleInstruction(b *strings.Builder, offset int, pad string) int {
	fmt.Fprintf(b, "%s%04d ", pad, offset)
	line := c.LineAt(offset)
	// 同一行连续指令不重复打印行号，用 | 占位，
	// 纵向扫读时更容易看出「这一行源码编译成了哪几条指令」。
	if offset > 0 && line == c.LineAt(offset-1) {
		b.WriteString("   | ")
	} else {
		fmt.Fprintf(b, "%4d ", line)
	}

	op := Op(c.Code[offset])
	switch op {
	case OpConstant, OpGetGlobal, OpSetGlobal, OpDefineGlobal, OpClosure:
		return c.constantInstruction(b, op, offset, pad)
	case OpGetLocal, OpSetLocal, OpGetUpvalue, OpSetUpvalue, OpCall:
		return c.byteInstruction(b, op, offset)
	case OpJump, OpJumpIfFalse, OpLoop:
		return c.jumpInstruction(b, op, offset)
	case OpNil, OpTrue, OpFalse, OpPop, OpEqual, OpGreater, OpGreaterEqual,
		OpLess, OpLessEqual, OpAdd, OpSubtract, OpMultiply, OpDivide, OpModulo,
		OpNot, OpNegate, OpPrint, OpCloseUpvalue, OpReturn:
		b.WriteString(op.String())
		b.WriteString("\n")
		return offset + 1
	default:
		fmt.Fprintf(b, "未知操作码 %d\n", byte(op))
		return offset + 1
	}
}

func (c *Chunk) constantInstruction(b *strings.Builder, op Op, offset int, pad string) int {
	idx := int(c.Code[offset+1])
	name := op.String()
	if op == OpClosure {
		// OP_CLOSURE 后面跟着 N 对 (isLocal, index)，N 由被闭包化的函数的 UpvalueCount 决定。
		// 所以这里必须先取常量拿到函数对象，才知道这条指令有多长。
		// 反汇编器必须能正确算出指令长度，否则从这一条开始全部错位。
		fn, ok := c.Constants[idx].AsObject().(*ObjFunction)
		if !ok {
			fmt.Fprintf(b, "%-20s %4d <非法函数常量>\n", name, idx)
			return offset + 2
		}
		fmt.Fprintf(b, "%-20s %4d '%s' upvalues=%d\n", name, idx, displayName(fn.Name), fn.UpvalueCount)
		next := offset + 2
		for i := 0; i < fn.UpvalueCount; i++ {
			isLocal := c.Code[next]
			index := c.Code[next+1]
			kind := "upvalue"
			if isLocal == 1 {
				kind = "local"
			}
			fmt.Fprintf(b, "%s%04d    | %-20s   %s[%d]\n", pad, next, "", kind, index)
			next += 2
		}
		return next
	}
	fmt.Fprintf(b, "%-20s %4d '%s'\n", name, idx, Format(c.Constants[idx]))
	return offset + 2
}

func (c *Chunk) byteInstruction(b *strings.Builder, op Op, offset int) int {
	v := c.Code[offset+1]
	fmt.Fprintf(b, "%-20s %4d\n", op.String(), v)
	return offset + 2
}

func (c *Chunk) jumpInstruction(b *strings.Builder, op Op, offset int) int {
	jump := int(c.Code[offset+1])<<8 | int(c.Code[offset+2])
	var target int
	if op == OpLoop {
		target = offset + 3 - jump
	} else {
		target = offset + 3 + jump
	}
	fmt.Fprintf(b, "%-20s %4d -> %04d\n", op.String(), jump, target)
	return offset + 3
}
