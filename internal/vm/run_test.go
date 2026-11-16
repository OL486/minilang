// Package vm 的分派循环（run.go）直接测试。
//
// 与 test/e2e_test.go 的区别：这里不走「源码 → 编译」整条流水线，
// 而是直接手拼字节码（hand-built chunk）或对 VM 内部方法做白盒调用，
// 目的是覆盖那些 e2e 黄金用例碰不到的错误路径：
// 未知操作码、栈溢出恢复、未定义变量、非法调用、跳转与循环指令本身。
package vm

import (
	"bytes"
	"strings"
	"testing"
)

// newScriptFn 返回一个空的脚本函数，字节码由调用方手拼。
func newScriptFn(vm *VM) *ObjFunction {
	return vm.NewFunction("", 0)
}

// constNum 往常量池里加一个数字，返回下标。
func constNum(fn *ObjFunction, n float64) int {
	return fn.Chunk.AddConstant(NumberValue(n))
}

// constStr 往常量池里加一个字符串（走驻留表），返回下标。
func constStr(vm *VM, fn *ObjFunction, s string) int {
	return fn.Chunk.AddConstant(ObjValue(vm.InternString(s)))
}

// emit 写一条无操作数的指令；emitByte 写一个操作数/立即数字节。
func emit(fn *ObjFunction, op Op)      { fn.Chunk.Write(byte(op), 1) }
func emitByte(fn *ObjFunction, b byte) { fn.Chunk.Write(b, 1) }

// emitJump 写一条 16 位跳转指令，返回两个操作数字节的位置，
// 调用方填好目标偏移后用 patchJump 回填。
func emitJump(fn *ObjFunction, op Op) int {
	emit(fn, op)
	pos := len(fn.Chunk.Code)
	emitByte(fn, 0)
	emitByte(fn, 0)
	return pos
}

// patchJump 把从 pos 开始的两个字节回填成「跳到 target」的偏移量。
// 语义与 run.go 一致：OpJump/OpJumpIfFalse 的目标是 pos+2+jump，
// OpLoop 的目标是 pos+2-jump。
func patchJump(fn *ObjFunction, pos int, target int, op Op) {
	var jump int
	if op == OpLoop {
		jump = pos + 2 - target
	} else {
		jump = target - (pos + 2)
	}
	fn.Chunk.Code[pos] = byte(jump >> 8)
	fn.Chunk.Code[pos+1] = byte(jump)
}

func TestRunScriptHandBuiltChunk(t *testing.T) {
	var out bytes.Buffer
	vm := NewVM(&out)
	fn := newScriptFn(vm)
	c0 := constNum(fn, 1)
	c1 := constNum(fn, 2)
	emit(fn, OpConstant)
	emitByte(fn, byte(c0))
	emit(fn, OpConstant)
	emitByte(fn, byte(c1))
	emit(fn, OpAdd)
	emit(fn, OpPrint)
	emit(fn, OpNil)
	emit(fn, OpReturn)

	result, err := vm.RunScript(fn)
	if err != nil {
		t.Fatalf("RunScript 报错: %v", err)
	}
	if out.String() != "3\n" {
		t.Fatalf("输出 = %q, want %q", out.String(), "3\n")
	}
	if !result.IsNil() {
		t.Fatalf("返回值 = %v, want nil", result)
	}
}

func TestRunScriptReturnsValue(t *testing.T) {
	var out bytes.Buffer
	vm := NewVM(&out)
	fn := newScriptFn(vm)
	c := constNum(fn, 42)
	emit(fn, OpConstant)
	emitByte(fn, byte(c))
	emit(fn, OpReturn)

	result, err := vm.RunScript(fn)
	if err != nil {
		t.Fatalf("RunScript 报错: %v", err)
	}
	if !result.IsNumber() || result.AsNumber() != 42 {
		t.Fatalf("返回值 = %v, want 42", result)
	}
}

func TestRunJumpAndJumpIfFalse(t *testing.T) {
	// 布局：cond; OpJumpIfFalse -> L1; print "wrong"; L1: print "right"; return
	build := func(vm *VM, cond Op) *ObjFunction {
		fn := newScriptFn(vm)
		wrong := constStr(vm, fn, "wrong")
		right := constStr(vm, fn, "right")
		emit(fn, cond)
		jumpPos := emitJump(fn, OpJumpIfFalse)
		emit(fn, OpConstant)
		emitByte(fn, byte(wrong))
		emit(fn, OpPrint)
		target := len(fn.Chunk.Code)
		patchJump(fn, jumpPos, target, OpJumpIfFalse)
		emit(fn, OpConstant)
		emitByte(fn, byte(right))
		emit(fn, OpPrint)
		emit(fn, OpNil)
		emit(fn, OpReturn)
		return fn
	}

	// 条件为假：跳过 "wrong"，只打印 "right"。
	var out bytes.Buffer
	vm := NewVM(&out)
	if _, err := vm.RunScript(build(vm, OpFalse)); err != nil {
		t.Fatalf("RunScript(false) 报错: %v", err)
	}
	if out.String() != "right\n" {
		t.Fatalf("条件为假时输出 = %q, want %q", out.String(), "right\n")
	}

	// 条件为真：不跳，两个都打印。
	out.Reset()
	vm2 := NewVM(&out)
	if _, err := vm2.RunScript(build(vm2, OpTrue)); err != nil {
		t.Fatalf("RunScript(true) 报错: %v", err)
	}
	if out.String() != "wrong\nright\n" {
		t.Fatalf("条件为真时输出 = %q, want %q", out.String(), "wrong\nright\n")
	}
}

func TestRunJump(t *testing.T) {
	var out bytes.Buffer
	vm := NewVM(&out)
	fn := newScriptFn(vm)
	skipped := constStr(vm, fn, "skipped")
	landed := constStr(vm, fn, "landed")
	jumpPos := emitJump(fn, OpJump)
	emit(fn, OpConstant)
	emitByte(fn, byte(skipped))
	emit(fn, OpPrint)
	target := len(fn.Chunk.Code)
	patchJump(fn, jumpPos, target, OpJump)
	emit(fn, OpConstant)
	emitByte(fn, byte(landed))
	emit(fn, OpPrint)
	emit(fn, OpNil)
	emit(fn, OpReturn)

	if _, err := vm.RunScript(fn); err != nil {
		t.Fatalf("RunScript 报错: %v", err)
	}
	if out.String() != "landed\n" {
		t.Fatalf("输出 = %q, want %q", out.String(), "landed\n")
	}
}

func TestRunLoopOp(t *testing.T) {
	// 手拼等价于 `let i = 0; while (i < 3) { print i; i = i + 1; }` 的字节码。
	// 脚本帧的槽位 0 是脚本闭包自身，局部变量 i 用槽位 1。
	// 注意：声明局部变量后不能 OpPop——槽位 1 既是局部变量的家，
	// 也是 sp=1 时第一个临时槽；一旦 pop，下一条压栈指令就会覆盖它。
	// 编译器的做法同样是让局部变量一直占着栈槽，直到作用域退出。
	var out bytes.Buffer
	vm := NewVM(&out)
	fn := newScriptFn(vm)
	c0 := constNum(fn, 0)
	c3 := constNum(fn, 3)
	c1 := constNum(fn, 1)

	emit(fn, OpConstant)
	emitByte(fn, byte(c0))
	emit(fn, OpSetLocal)
	emitByte(fn, 1)

	loopStart := len(fn.Chunk.Code)
	emit(fn, OpGetLocal)
	emitByte(fn, 1)
	emit(fn, OpConstant)
	emitByte(fn, byte(c3))
	emit(fn, OpLess)
	jumpPos := emitJump(fn, OpJumpIfFalse)
	emit(fn, OpPop)
	emit(fn, OpGetLocal)
	emitByte(fn, 1)
	emit(fn, OpPrint)
	emit(fn, OpGetLocal)
	emitByte(fn, 1)
	emit(fn, OpConstant)
	emitByte(fn, byte(c1))
	emit(fn, OpAdd)
	emit(fn, OpSetLocal)
	emitByte(fn, 1)
	emit(fn, OpPop)
	loopPos := emitJump(fn, OpLoop)
	patchJump(fn, loopPos, loopStart, OpLoop)
	end := len(fn.Chunk.Code)
	patchJump(fn, jumpPos, end, OpJumpIfFalse)
	emit(fn, OpNil)
	emit(fn, OpReturn)

	if _, err := vm.RunScript(fn); err != nil {
		t.Fatalf("RunScript 报错: %v", err)
	}
	if out.String() != "0\n1\n2\n" {
		t.Fatalf("输出 = %q, want %q", out.String(), "0\n1\n2\n")
	}
}

func TestRunUndefinedVariableErrors(t *testing.T) {
	// 读未定义的全局变量。
	var out bytes.Buffer
	vm := NewVM(&out)
	fn := newScriptFn(vm)
	name := constStr(vm, fn, "nope")
	emit(fn, OpGetGlobal)
	emitByte(fn, byte(name))
	emit(fn, OpReturn)
	_, err := vm.RunScript(fn)
	if err == nil || !strings.Contains(err.Error(), "未定义的变量 'nope'") {
		t.Fatalf("读未定义全局应报错未定义，实际 err = %v", err)
	}

	// 给未定义的全局变量赋值。
	vm2 := NewVM(&out)
	fn2 := newScriptFn(vm2)
	c := constNum(fn2, 1)
	name2 := constStr(vm2, fn2, "ghost")
	emit(fn2, OpConstant)
	emitByte(fn2, byte(c))
	emit(fn2, OpSetGlobal)
	emitByte(fn2, byte(name2))
	emit(fn2, OpReturn)
	_, err = vm2.RunScript(fn2)
	if err == nil || !strings.Contains(err.Error(), "不能给未定义的变量 'ghost' 赋值") {
		t.Fatalf("给未定义全局赋值应报错，实际 err = %v", err)
	}
}

func TestRunCallNonFunction(t *testing.T) {
	var out bytes.Buffer
	vm := NewVM(&out)
	fn := newScriptFn(vm)
	c := constNum(fn, 42)
	emit(fn, OpConstant)
	emitByte(fn, byte(c))
	emit(fn, OpCall)
	emitByte(fn, 0)
	emit(fn, OpReturn)

	_, err := vm.RunScript(fn)
	if err == nil || !strings.Contains(err.Error(), "不是函数，不能调用") {
		t.Fatalf("调用 number 应报错不是函数，实际 err = %v", err)
	}
}

func TestRunStackOverflowRecovery(t *testing.T) {
	var out bytes.Buffer
	vm := NewVM(&out)
	fn := newScriptFn(vm)
	c := constNum(fn, 1)
	// 压入超过 StackMax 个值：push 会 panic(stackOverflow)，Run 必须把它
	// 恢复成干净的运行时错误，而不是让进程崩溃。
	for i := 0; i < StackMax+10; i++ {
		emit(fn, OpConstant)
		emitByte(fn, byte(c))
	}
	emit(fn, OpReturn)

	_, err := vm.RunScript(fn)
	if err == nil || !strings.Contains(err.Error(), "VM 栈溢出") {
		t.Fatalf("栈溢出应恢复成运行时错误，实际 err = %v", err)
	}

	// 恢复后 VM 仍可用：同一台 VM 再跑一段正常程序。
	fn2 := newScriptFn(vm)
	c2 := constNum(fn2, 7)
	emit(fn2, OpConstant)
	emitByte(fn2, byte(c2))
	emit(fn2, OpPrint)
	emit(fn2, OpNil)
	emit(fn2, OpReturn)
	if _, err := vm.RunScript(fn2); err != nil {
		t.Fatalf("栈溢出恢复后再次运行应成功，实际 err = %v", err)
	}
	if out.String() != "7\n" {
		t.Fatalf("恢复后输出 = %q, want %q", out.String(), "7\n")
	}
}

func TestRunUnknownOpcode(t *testing.T) {
	var out bytes.Buffer
	vm := NewVM(&out)
	fn := newScriptFn(vm)
	c := constNum(fn, 1)
	emit(fn, OpConstant)
	emitByte(fn, byte(c))
	emit(fn, Op(0xFF)) // 不存在的操作码
	emit(fn, OpReturn)

	_, err := vm.RunScript(fn)
	if err == nil || !strings.Contains(err.Error(), "未知操作码") {
		t.Fatalf("未知操作码应报错，实际 err = %v", err)
	}
}

func TestRunNegateNonNumber(t *testing.T) {
	var out bytes.Buffer
	vm := NewVM(&out)
	fn := newScriptFn(vm)
	s := constStr(vm, fn, "x")
	emit(fn, OpConstant)
	emitByte(fn, byte(s))
	emit(fn, OpNegate)
	emit(fn, OpReturn)

	_, err := vm.RunScript(fn)
	if err == nil || !strings.Contains(err.Error(), "一元 - 只能作用于 number") {
		t.Fatalf("对 string 取负应报错，实际 err = %v", err)
	}
}

func TestRunScriptCallClosureError(t *testing.T) {
	// RunScript 里 callClosure 失败时（这里用一个 Arity 为 1 的“脚本函数”
	// 触发参数个数错误；帧满分支同理，但 RunScript 开头会把
	// frameCount 清零，所以帧满只能从 Run 内部撞上，见 TestCallDepthLimit），
	// 必须返回干净的错误，并且把 sp 复位、VM 仍可用。
	var out bytes.Buffer
	vm := NewVM(&out)
	fn := vm.NewFunction("", 1) // 脚本函数本应是 0 参数
	emit(fn, OpNil)
	emit(fn, OpReturn)
	_, err := vm.RunScript(fn)
	if err == nil || !strings.Contains(err.Error(), "需要 1 个参数") {
		t.Fatalf("RunScript 应报参数个数错误，实际 err = %v", err)
	}
	if vm.sp != 0 {
		t.Fatalf("RunScript 报错后 sp = %d, want 0", vm.sp)
	}
	// 报错后 VM 仍可用。
	fn2 := newScriptFn(vm)
	c := constNum(fn2, 7)
	emit(fn2, OpConstant)
	emitByte(fn2, byte(c))
	emit(fn2, OpPrint)
	emit(fn2, OpNil)
	emit(fn2, OpReturn)
	if _, err := vm.RunScript(fn2); err != nil {
		t.Fatalf("恢复后 RunScript 应成功，实际 err = %v", err)
	}
	if out.String() != "7\n" {
		t.Fatalf("恢复后输出 = %q, want %q", out.String(), "7\n")
	}
}
