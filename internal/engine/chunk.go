package engine

import (
	"fmt"
	"strings"
)

// Op 是字节码操作码。
//
// 为什么全部用单字节：分派循环里 `switch Op(code[ip])` 是最热的代码，
// 单字节 opcode 让指令流紧凑、缓存友好。代价是每个函数最多 256 个常量、
// 256 个局部变量、256 个 upvalue —— 这些上限在 S6 的编译器里显式检查并报错，
// 而不是静默溢出成错乱的指令。
type Op byte

const (
	OpConstant Op = iota // 常量池索引
	OpNil
	OpTrue
	OpFalse
	OpPop

	OpGetLocal // 局部变量槽位（绝对下标，见 vm.go 的说明）
	OpSetLocal
	OpGetGlobal // 全局名在常量池里的索引
	OpSetGlobal
	OpDefineGlobal
	OpGetUpvalue // upvalue 下标
	OpSetUpvalue

	OpEqual
	OpGreater
	OpGreaterEqual
	OpLess
	OpLessEqual
	OpAdd
	OpSubtract
	OpMultiply
	OpDivide
	OpModulo
	OpNot
	OpNegate

	OpPrint

	OpJump        // 16 位前向偏移
	OpJumpIfFalse // 16 位前向偏移，条件为假时跳，不弹栈
	OpLoop        // 16 位后向偏移

	OpCall // 实参个数
	OpClosure
	OpCloseUpvalue
	OpReturn
)

var opNames = [...]string{
	OpConstant:     "OP_CONSTANT",
	OpNil:          "OP_NIL",
	OpTrue:         "OP_TRUE",
	OpFalse:        "OP_FALSE",
	OpPop:          "OP_POP",
	OpGetLocal:     "OP_GET_LOCAL",
	OpSetLocal:     "OP_SET_LOCAL",
	OpGetGlobal:    "OP_GET_GLOBAL",
	OpSetGlobal:    "OP_SET_GLOBAL",
	OpDefineGlobal: "OP_DEFINE_GLOBAL",
	OpGetUpvalue:   "OP_GET_UPVALUE",
	OpSetUpvalue:   "OP_SET_UPVALUE",
	OpEqual:        "OP_EQUAL",
	OpGreater:      "OP_GREATER",
	OpGreaterEqual: "OP_GREATER_EQUAL",
	OpLess:         "OP_LESS",
	OpLessEqual:    "OP_LESS_EQUAL",
	OpAdd:          "OP_ADD",
	OpSubtract:     "OP_SUBTRACT",
	OpMultiply:     "OP_MULTIPLY",
	OpDivide:       "OP_DIVIDE",
	OpModulo:       "OP_MODULO",
	OpNot:          "OP_NOT",
	OpNegate:       "OP_NEGATE",
	OpPrint:        "OP_PRINT",
	OpJump:         "OP_JUMP",
	OpJumpIfFalse:  "OP_JUMP_IF_FALSE",
	OpLoop:         "OP_LOOP",
	OpCall:         "OP_CALL",
	OpClosure:      "OP_CLOSURE",
	OpCloseUpvalue: "OP_CLOSE_UPVALUE",
	OpReturn:       "OP_RETURN",
}

func (o Op) String() string {
	if int(o) < len(opNames) && opNames[o] != "" {
		return opNames[o]
	}
	return fmt.Sprintf("OP_?(%d)", byte(o))
}

// Chunk 是一段字节码及其附属数据。
//
// Lines 与 Code 等长，逐字节记录源码行号。
// 为什么不用「行号表 + 区间」的压缩格式：调试器要的是 O(1) 查任意偏移的行号，
// 一个字节的冗余换一次数组下标，对教学项目是划算的。
type Chunk struct {
	Code      []byte
	Constants []Value
	Lines     []int
}

func NewChunk() *Chunk {
	return &Chunk{}
}

func (c *Chunk) Write(b byte, line int) {
	c.Code = append(c.Code, b)
	c.Lines = append(c.Lines, line)
}

// AddConstant 返回新常量在池中的下标。超过 255 时返回 -1，
// 由编译器负责报错 —— 这里不 panic，因为「常量太多」是用户代码的问题，不是内部 bug。
func (c *Chunk) AddConstant(v Value) int {
	if len(c.Constants) > 255 {
		return -1
	}
	c.Constants = append(c.Constants, v)
	return len(c.Constants) - 1
}

func (c *Chunk) LineAt(offset int) int {
	if offset < 0 || offset >= len(c.Lines) {
		return 0
	}
	return c.Lines[offset]
}

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
