package vm

import (
	"fmt"
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
