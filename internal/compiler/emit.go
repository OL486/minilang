package compiler

import (
	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/vm"
)

// ---------- 发射原语 ----------

func (c *Compiler) chunk() *vm.Chunk { return c.fn.fn.Chunk }

func (c *Compiler) emit(op vm.Op, line int) {
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

func (c *Compiler) addConstant(v vm.Value, line int) int {
	idx := c.chunk().AddConstant(v)
	if idx < 0 {
		c.errorAt(line, "函数 %s 的常量池超过 %d 项（单字节操作数上限）", displayFnName(c.fn.fn.Name), maxConstants)
	}
	return idx
}

func (c *Compiler) identifierConstant(name string, line int) int {
	s := c.vm.InternString(name)
	return c.addConstant(vm.ObjValue(s), line)
}

// emitJump 发射一个 16 位占位跳转，返回操作数位置。
//
// 为什么必须先发 0xff 0xff 再回填：编译到 if/while 时，跳转目标还不存在。
// 只有等分支体编译完，才知道目标偏移。回填（patch）是这个模式的名字。
// 另一种写法是「先记录位置，最后统一重排」—— 那需要两遍编译，得不偿失。
func (c *Compiler) emitJump(op vm.Op, line int) int {
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
	c.emit(vm.OpLoop, line)
	// +2 是操作数自身的长度：VM 在读完操作数后 ip 已经指向下一条指令，
	// 要减掉「opcode + 2 字节操作数」才能回到循环开头。
	offset := len(c.chunk().Code) - loopStart + 2
	if offset > 0xffff {
		c.errorAt(line, "循环体超过 65535 字节")
	}
	c.emitShort(offset, line)
}

func (c *Compiler) emitReturn(line int) {
	c.emit(vm.OpNil, line)
	c.emit(vm.OpReturn, line)
}
