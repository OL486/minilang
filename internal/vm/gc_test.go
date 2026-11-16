// Package vm 的垃圾回收（gc.go）与堆分配（alloc.go）直接测试。
//
// 重点是 gc.go 头注释里列出的 5 类根是否完整、
// 驻留表是否在回收时正确摘掉死字符串、以及 nextGC 的翻倍规则。
// 测试与 VM 同包，可以直接读写 vm.intern、vm.nextGC 等内部状态。
package vm

import (
	"io"
	"strings"
	"testing"

	"github.com/OL486/minilang/internal/ast"
)

func TestInternStringDedup(t *testing.T) {
	vm := NewVM(io.Discard)
	a := vm.InternString("hello")
	b := vm.InternString("hello")
	if a != b {
		t.Fatalf("同一内容应返回同一对象，得到两个不同指针")
	}
	if c := vm.InternString("world"); c == a {
		t.Fatalf("不同内容不应返回同一对象")
	}
}

func TestGCRemovesDeadInternedString(t *testing.T) {
	vm := NewVM(io.Discard)
	dead := vm.InternString("dead")
	vm.CollectGarbage()
	if _, ok := vm.intern["dead"]; ok {
		t.Fatalf("无根字符串回收后仍留在驻留表里")
	}
	// 重新驻留同一内容必须分配新对象，不能复活已死的对象。
	again := vm.InternString("dead")
	if again == dead {
		t.Fatalf("重新驻留返回了已回收的对象")
	}
}

func TestGCStackIsRoot(t *testing.T) {
	vm := NewVM(io.Discard)
	s := vm.InternString("keep")
	vm.push(ObjValue(s))
	vm.CollectGarbage()
	if got := vm.InternString("keep"); got != s {
		t.Fatalf("栈上的字符串被 GC 回收了")
	}
	// 弹栈后不再是根，下一次回收必须摘掉。
	vm.pop()
	vm.CollectGarbage()
	if _, ok := vm.intern["keep"]; ok {
		t.Fatalf("弹栈后的字符串回收后仍留在驻留表里")
	}
}

func TestGCTempRoots(t *testing.T) {
	vm := NewVM(io.Discard)
	s := vm.InternString("temp")
	vm.PushRoot(ObjValue(s))
	vm.CollectGarbage()
	if _, ok := vm.intern["temp"]; !ok {
		t.Fatalf("临时根保护的字符串被 GC 回收了")
	}
	vm.PopRoot()
	vm.CollectGarbage()
	if _, ok := vm.intern["temp"]; ok {
		t.Fatalf("PopRoot 后的字符串回收后仍留在驻留表里")
	}
	// ClearRoots 必须能兜底不平衡的 root 栈（编译中途报错时）。
	vm.PushRoot(ObjValue(vm.InternString("a")))
	vm.PushRoot(ObjValue(vm.InternString("b")))
	vm.ClearRoots()
	if len(vm.roots) != 0 {
		t.Fatalf("ClearRoots 后 roots 长度 = %d, want 0", len(vm.roots))
	}
}

func TestGCThresholdDoubling(t *testing.T) {
	vm := NewVM(io.Discard)
	// 空堆回收后阈值取下限 1 MiB。
	vm.collectGarbage()
	if vm.nextGC != 1<<20 {
		t.Fatalf("空堆回收后 nextGC = %d, want %d", vm.nextGC, 1<<20)
	}
	// 堆里留一个 2 MiB 的有根字符串：回收后阈值应翻倍，而不是回落到下限。
	big := vm.InternString(strings.Repeat("x", 2<<20))
	vm.PushRoot(ObjValue(big))
	vm.collectGarbage()
	if vm.nextGC != vm.bytesAllocated*2 {
		t.Fatalf("nextGC = %d, want bytesAllocated*2 = %d", vm.nextGC, vm.bytesAllocated*2)
	}
	if vm.nextGC <= 1<<20 {
		t.Fatalf("大堆回收后 nextGC = %d, 应大于 1 MiB", vm.nextGC)
	}
	if got := vm.InternString(strings.Repeat("x", 2<<20)); got != big {
		t.Fatalf("有根的大字符串被 GC 回收了")
	}
}

func TestGCDisabled(t *testing.T) {
	vm := NewVM(io.Discard)
	vm.SetGCEnabled(false)
	for i := 0; i < 100; i++ {
		vm.InternString(strings.Repeat("s", i))
	}
	if vm.gcRuns != 0 {
		t.Fatalf("GC 关闭时 gcRuns = %d, want 0", vm.gcRuns)
	}
	vm.CollectGarbage()
	if vm.gcRuns != 0 {
		t.Fatalf("GC 关闭时 CollectGarbage 不应计数，gcRuns = %d", vm.gcRuns)
	}
	vm.SetGCEnabled(true)
	vm.CollectGarbage()
	if vm.gcRuns != 1 {
		t.Fatalf("重新打开 GC 后 gcRuns = %d, want 1", vm.gcRuns)
	}
}

func TestAllocateTriggersGC(t *testing.T) {
	vm := NewVM(io.Discard)
	vm.nextGC = 1 // 压到最小：下一次分配就必须触发回收
	vm.InternString("trigger")
	if vm.gcRuns < 1 {
		t.Fatalf("超过 nextGC 的分配没有触发 GC，gcRuns = %d", vm.gcRuns)
	}
	if vm.nextGC < 1<<20 {
		t.Fatalf("触发回收后 nextGC = %d, 应至少回到 1 MiB 下限", vm.nextGC)
	}
}

func TestAddStringConcatUnderGC(t *testing.T) {
	// add() 注释里写明的坑：拼接结果必须在两个操作数还在栈上时驻留，
	// 否则一次恰好触发的 GC 会把结果当垃圾回收。
	// 这里把 nextGC 压到 0，强制每一次驻留都触发 GC。
	vm := NewVM(io.Discard)
	for i := 0; i < 50; i++ {
		vm.nextGC = 0
		a := vm.InternString("hello ")
		b := vm.InternString("world")
		vm.push(ObjValue(a))
		vm.push(ObjValue(b))
		if err := vm.add(ast.Pos{Line: 1, Col: 1}); err != nil {
			t.Fatalf("第 %d 次 add 报错: %v", i, err)
		}
		got := vm.peek(0).AsString()
		if got == nil || got.Chars != "hello world" {
			t.Fatalf("第 %d 次拼接结果 = %v, want \"hello world\"", i, got)
		}
		vm.pop()
		vm.CollectGarbage()
	}
}

func TestGCMarksSharedObjectTwice(t *testing.T) {
	// 同一个对象同时出现在栈上和 globals 里：第二次标记必须走
	// markObject 的「已标记直接返回」分支，而不是重复进灰色栈。
	vm := NewVM(io.Discard)
	s := vm.InternString("shared")
	vm.push(ObjValue(s))
	vm.push(ObjValue(s))
	vm.globals["g"] = ObjValue(s)
	vm.CollectGarbage()
	if got := vm.InternString("shared"); got != s {
		t.Fatalf("多根共享的字符串被 GC 回收了")
	}
	if vm.gcRuns != 1 {
		t.Fatalf("gcRuns = %d, want 1", vm.gcRuns)
	}
}

func TestBlackenNilUpvalueSlot(t *testing.T) {
	// OP_CLOSURE 先把闭包压栈再逐个填 upvalue：填第一个时如果触发 GC，
	// 闭包的 Upvalues[1..] 还是 nil。blacken 必须跳过 nil 槽位，
	// 而不是对着 nil 解引用。
	vm := NewVM(io.Discard)
	fn := vm.NewFunction("f", 0)
	fn.UpvalueCount = 2
	cl := vm.newClosure(fn)
	cl.Upvalues[0] = vm.newUpvalue(0)
	// Upvalues[1] 保持 nil，模拟「正在填充中」的闭包。
	vm.markObject(cl)
	vm.traceReferences() // 不应 panic
	// 闭包本身仍在堆上且未被误回收。
	if vm.Stats().Objects < 2 {
		t.Fatalf("标记后堆对象数 = %d, 闭包与函数应都在", vm.Stats().Objects)
	}
}
