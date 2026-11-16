// Package vm 的 --trace 逐指令跟踪（run.go 的 traceInstruction）测试。
//
// trace 的输出格式与 --dump-bytecode 的反汇编保持一致
// （%04d + 左对齐 20 列 opcode 名），外加「栈顶: <执行前栈顶的值>」。
// 注意栈顶是执行前的值：OP_ADD 这一行看到的是两个操作数里上面的那个，
// 相加后的结果要到下一行（OP_PRINT）的栈顶才能看到。
package vm

import (
	"bytes"
	"strings"
	"testing"
)

// traceChunk 拼一段「1 + 2，打印」的字节码，与 TestRunScriptHandBuiltChunk 相同，
// 只是调用方可以决定开不开 trace。
func traceChunk(vm *VM) *ObjFunction {
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
	return fn
}

func TestTraceEmitsInstructionLines(t *testing.T) {
	var out bytes.Buffer
	vm := NewVM(&out)
	vm.SetTrace(true)

	if _, err := vm.RunScript(traceChunk(vm)); err != nil {
		t.Fatalf("RunScript 报错: %v", err)
	}
	got := out.String()

	// 程序输出仍在：trace 行与 print 结果混在同一路输出里（都走 vm.out，
	// 与 --dump-bytecode 的惯例一致）。
	if !strings.Contains(got, "3\n") {
		t.Fatalf("程序输出缺失，got = %q", got)
	}
	// 每条指令一行，pc 从 0000 开始连续编号。
	for _, op := range []string{"OP_CONSTANT", "OP_ADD", "OP_PRINT", "OP_RETURN"} {
		if !strings.Contains(got, op) {
			t.Fatalf("trace 缺 %s 行，got = %q", op, got)
		}
	}
	if !strings.Contains(got, "0000 OP_CONSTANT") {
		t.Fatalf("第一行应为 pc 0000 的 OP_CONSTANT，got = %q", got)
	}
	// 栈顶是执行前的值：OP_ADD 行的栈顶是第二个操作数 2，
	// 相加后的 3 出现在下一条指令（OP_PRINT）行的栈顶。
	lines := strings.Split(got, "\n")
	var addTop, printTop string
	for _, l := range lines {
		if strings.Contains(l, "OP_ADD") {
			addTop = l
		}
		if strings.Contains(l, "OP_PRINT") {
			printTop = l
		}
	}
	if !strings.Contains(addTop, "栈顶: 2") {
		t.Fatalf("OP_ADD 行的栈顶应为 2（执行前），got = %q", addTop)
	}
	if !strings.Contains(printTop, "栈顶: 3") {
		t.Fatalf("OP_PRINT 行的栈顶应为 3（1+2 的结果），got = %q", printTop)
	}
}

func TestTraceDisabledEmitsNothing(t *testing.T) {
	var out bytes.Buffer
	vm := NewVM(&out)
	// 默认就是关闭；显式再关一次，确认 SetTrace(false) 同样干净。
	vm.SetTrace(false)

	if _, err := vm.RunScript(traceChunk(vm)); err != nil {
		t.Fatalf("RunScript 报错: %v", err)
	}
	if got := out.String(); got != "3\n" {
		t.Fatalf("trace 关闭时输出应只有程序结果，got = %q, want %q", got, "3\n")
	}
}
