package compiler

import "github.com/OL486/minilang/internal/vm"

// Allocator 是编译器对「堆」的全部需求 —— 编译器真正用到的 VM 能力只有这 5 个方法，
// 而不是整个 *vm.VM（见 docs/04-compiler-vm-decoupling.md 的清单）。
//
//   - NewFunction：创建函数对象（脚本与内层函数），编译器只填它的 Chunk。
//   - InternString：字符串常量与变量名驻留，同一内容复用同一对象。
//   - PushRoot / PopRoot / ClearRoots：编译期临时根。编译中创建的函数对象不在 VM 栈上，
//     分配随时可能触发 GC；不挂根，正在编译的函数会被当成垃圾回收掉。
//     成功编译结束时根栈应为空；编译报错（compileAbort）时由 CompileProgram 的
//     defer 调 ClearRoots 强制清空。
//
// *vm.VM 完整实现这个接口，所以生产调用方（cmd/minilang、test/e2e）
// 一行代码都不用改；编译器单元测试则可以用一个最小的 fake（见 compiler_test.go
// 的 fakeAllocator），不需要启动完整 VM。
type Allocator interface {
	NewFunction(name string, arity int) *vm.ObjFunction
	InternString(s string) *vm.ObjString
	PushRoot(v vm.Value)
	PopRoot()
	ClearRoots()
}
