package vm

// ---------- 堆分配 ----------

// allocate 把一个新对象挂到对象堆上。
//
// 关键顺序：先判断是否需要 GC，再挂链。
// 如果先挂链再 GC，新对象还没有任何根指向它，会被立刻回收 ——
// 这个 bug 的表现是「偶发地把刚创建的字符串打成 nil」，
// 而且只在内存压力大时复现，极难定位。
func (vm *VM) allocate(h *objHeader, typ ObjType, self Object, size int) {
	if vm.gcEnabled && vm.bytesAllocated+size > vm.nextGC {
		vm.collectGarbage()
	}
	h.typ = typ
	h.marked = false
	h.self = self
	h.size = size
	h.next = vm.objects
	vm.objects = h
	vm.bytesAllocated += size
	vm.objectsCreated++
}

// InternString 把字符串放进驻留表，返回唯一实例。
//
// 驻留解决的是「字符串作为值要按内容比较，作为对象要按身份比较」这个矛盾：
// 每次创建字符串都查表，内容相同就复用同一个对象，省内存、省重复分配。
// 注意：== 本身按内容比较（见 valuesEqual：toAny 把字符串对象转成 Go string
// 再比较），不依赖指针相等；即使去掉驻留，== 语义仍正确。
//
// 代价：每次拼接都要做一次 map 查找 + 可能的分配。
// 这也是 S5→S10 对比表里 VM 在字符串密集场景下不一定更快的原因之一。
func (vm *VM) InternString(s string) *ObjString {
	if o, ok := vm.intern[s]; ok {
		return o
	}
	o := &ObjString{Chars: s}
	vm.allocate(&o.objHeader, ObjStringType, o, len(s)+40)
	// 这里不能有分配操作：allocate 已经做过 GC，此时 o 还没进表，
	// 一旦再触发一次 GC 就会被回收。所以紧接着插入。
	vm.intern[s] = o
	return o
}

// NewFunction 创建一个函数对象。调用方必须马上把它挂到根上（PushRoot 或入常量池）。
func (vm *VM) NewFunction(name string, arity int) *ObjFunction {
	fn := &ObjFunction{Name: name, Arity: arity, Chunk: NewChunk()}
	vm.allocate(&fn.objHeader, ObjFunctionType, fn, 64)
	return fn
}

func (vm *VM) newClosure(fn *ObjFunction) *ObjClosure {
	cl := &ObjClosure{Fn: fn, Upvalues: make([]*ObjUpvalue, fn.UpvalueCount)}
	vm.allocate(&cl.objHeader, ObjClosureType, cl, 40+8*fn.UpvalueCount)
	return cl
}

func (vm *VM) newUpvalue(slot int) *ObjUpvalue {
	uv := &ObjUpvalue{Location: &vm.stack[slot], Slot: slot}
	vm.allocate(&uv.objHeader, ObjUpvalueType, uv, 48)
	return uv
}

func (vm *VM) defineNative(name string, arity int, fn func(vm *VM, args []Value) (Value, error)) {
	nat := &ObjNative{Name: name, Arity: arity, Fn: fn}
	vm.allocate(&nat.objHeader, ObjNativeType, nat, 56)
	vm.globals[name] = ObjValue(nat)
}

// ---------- 根集合（供编译器使用） ----------

func (vm *VM) PushRoot(v Value) { vm.roots = append(vm.roots, v) }

func (vm *VM) PopRoot() {
	if n := len(vm.roots); n > 0 {
		vm.roots = vm.roots[:n-1]
	}
}

// ClearRoots 在编译结束后强制清空临时根。
// 编译中途报错会 panic，root 栈可能不平衡；不清空的话这些对象永远不会被回收。
func (vm *VM) ClearRoots() { vm.roots = vm.roots[:0] }
