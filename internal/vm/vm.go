package vm

import (
	"io"
	"time"

	"github.com/OL486/minilang/internal/semantics"
)

const (
	// StackMax 是操作数栈的槽位上限。
	// 预先一次性分配、之后永不 realloc —— 这不是性能考虑，是正确性要求：
	// ObjUpvalue.Location 直接指向栈内某个槽位的地址，
	// 如果栈扩容时搬了家，所有 open upvalue 都会指向已经失效的内存。
	StackMax = 1024 * 32

	// FramesMax 是调用帧上限，防止无限递归把 Go 的栈打爆。
	// 与 tree 侧共用 semantics.MaxCallDepth：以前 VM 是 1024、tree 是 1000，
	// 同一份深递归在 1001–1024 层时两个引擎行为不一致。
	FramesMax = semantics.MaxCallDepth
)

// callFrame 是一个调用帧。
//
// slotBase 是这一帧的「槽位 0」在 VM 栈中的绝对下标。
// 有了它，局部变量的访问就是 stack[slotBase+slot]，
// 不需要像 tree-walking 那样沿作用域链查 map。
type callFrame struct {
	closure  *ObjClosure
	ip       int
	slotBase int
}

// VM 是栈式虚拟机。
type VM struct {
	stack      []Value
	sp         int
	frames     []callFrame
	frameCount int

	globals      map[string]Value
	openUpvalues *ObjUpvalue

	// 对象堆：一条单向链表，GC 的遍历基础。
	objects *objHeader
	// intern 是字符串驻留表。key 是内容，value 是唯一的那个对象。
	intern map[string]*ObjString

	// roots 是「编译期临时根」。编译器手里的 ObjFunction 不在 VM 栈上，
	// 如果 GC 在这时跑，正在编译的函数会被当成垃圾回收掉。
	roots []Value

	gray []Object

	out io.Writer

	bytesAllocated int
	nextGC         int
	gcEnabled      bool
	gcRuns         int
	objectsCreated int
	curLine        int
}

// NewVM 创建虚拟机并注册内置函数。
func NewVM(out io.Writer) *VM {
	vm := &VM{
		stack:     make([]Value, StackMax),
		frames:    make([]callFrame, FramesMax),
		globals:   make(map[string]Value),
		intern:    make(map[string]*ObjString),
		out:       out,
		nextGC:    1 << 20, // 1 MiB 后第一次回收
		gcEnabled: true,
	}
	vm.defineNative("clock", 0, func(_ *VM, _ []Value) (Value, error) {
		return NumberValue(float64(time.Now().UnixMilli())), nil
	})
	return vm
}

// ---------- 统计 ----------

// Stats 是一次运行结束后的堆状态快照。
type Stats struct {
	Objects      int
	Bytes        int
	GCRuns       int
	ObjectsTotal int
}

func (vm *VM) Stats() Stats {
	n := 0
	for h := vm.objects; h != nil; h = h.next {
		n++
	}
	return Stats{
		Objects:      n,
		Bytes:        vm.bytesAllocated,
		GCRuns:       vm.gcRuns,
		ObjectsTotal: vm.objectsCreated,
	}
}

// Global 读取一个全局变量，供测试与基准脚本取回结果。
func (vm *VM) Global(name string) (Value, bool) {
	v, ok := vm.globals[name]
	return v, ok
}

// CollectGarbage 暴露给外部（压力测试、REPL 的 :gc 命令）。
func (vm *VM) CollectGarbage() { vm.collectGarbage() }

// SetGCEnabled 关闭 GC 只用于对比测试（证明 GC 确实在工作）。
func (vm *VM) SetGCEnabled(on bool) { vm.gcEnabled = on }
