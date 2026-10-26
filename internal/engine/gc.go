package engine

// 标记-清除垃圾回收。
//
// 为什么要自己写：Go 的 GC 能回收「Go 看不见引用的对象」，
// 但它不知道 VM 栈上哪个槽位是活跃的（我们复用了栈位，sp 之后的内容是脏数据），
// 也不知道 upvalue 的 open/close 语义。如果完全依赖 Go GC，
// 唯一安全的做法是永远不丢弃任何对象 —— 那就是内存泄漏。
// 自己实现 mark-sweep，换来的是「对象生命周期由语言语义决定」，
// 而这正是 VM 相比 tree-walking 能控制内存的前提。
//
// 代价也必须说清楚：Go 不会真的把内存还给操作系统，
// 我们只是在对象堆链表上「摘链」，让 Go 那边的对象失去最后引用后被它自己回收。
// 换句话说：本实现管的是「对象何时变成垃圾」，不管「内存何时还给 OS」。

// collectGarbage 是一次完整的标记-清除。
func (vm *VM) collectGarbage() {
	if !vm.gcEnabled {
		return
	}
	vm.gcRuns++
	vm.gray = vm.gray[:0]

	vm.markRoots()
	vm.traceReferences()
	vm.sweep()

	// 阈值翻倍（下限 1 MiB）：回收太频繁会让 GC 本身成为热点，
	// 回收太少则内存峰值过高。翻倍是一种不需要调参的折中。
	vm.nextGC = vm.bytesAllocated * 2
	if vm.nextGC < 1<<20 {
		vm.nextGC = 1 << 20
	}
}

// markRoots 标记所有根。
//
// 根集合必须完整，漏一个就会出现「对象还在栈上却被回收」。
// 本项目一共有 5 类根，逐条列在这里，新增运行时状态时必须回来加：
//  1. VM 操作数栈 [0, sp) —— 注意是 sp 而不是 cap，sp 之后是脏数据；
//  2. 调用帧里的闭包 —— 帧可能还没执行到第一条指令；
//  3. 全局变量表；
//  4. open upvalue 链表 —— 它们指向栈，但对象本身也要活；
//  5. 编译期临时根 —— 编译器手里的函数对象不在 VM 栈上。
//
// 唯一不需要出现在这里的是「当前正在编译的常量池」：
// 常量池属于某个 ObjFunction，而那个函数已经被第 5 类根保住了。
func (vm *VM) markRoots() {
	for i := 0; i < vm.sp; i++ {
		vm.markValue(vm.stack[i])
	}
	for i := 0; i < vm.frameCount; i++ {
		vm.markObject(vm.frames[i].closure)
	}
	for _, v := range vm.globals {
		vm.markValue(v)
	}
	for uv := vm.openUpvalues; uv != nil; uv = uv.Next {
		vm.markObject(uv)
	}
	for _, v := range vm.roots {
		vm.markValue(v)
	}
}

func (vm *VM) markValue(v Value) {
	if v.kind != KindObject || v.obj == nil {
		return
	}
	vm.markObject(v.obj)
}

func (vm *VM) markObject(o Object) {
	if o == nil {
		return
	}
	h := o.header()
	if h == nil || h.marked {
		return
	}
	h.marked = true
	vm.gray = append(vm.gray, o)
}

// traceReferences 用显式的灰色栈做迭代式标记。
//
// 不用递归：一条 100 层的闭包链会让递归深度跟着对象图走，
// 而对象图的深度是不受语言层控制的（用户可以写无限递归生成闭包）。
func (vm *VM) traceReferences() {
	for len(vm.gray) > 0 {
		o := vm.gray[len(vm.gray)-1]
		vm.gray = vm.gray[:len(vm.gray)-1]
		vm.blacken(o)
	}
}

func (vm *VM) blacken(o Object) {
	switch x := o.(type) {
	case *ObjString:
		// 字符串没有子引用。它的 Chars 是 Go string，不受我们的 GC 管。
	case *ObjFunction:
		for _, c := range x.Chunk.Constants {
			vm.markValue(c)
		}
	case *ObjClosure:
		vm.markObject(x.Fn)
		for _, uv := range x.Upvalues {
			if uv == nil {
				continue
			}
			vm.markObject(uv)
		}
	case *ObjUpvalue:
		// open 时 Location 指向栈，栈本身是根；closed 时值在 Closed 里，必须标记。
		vm.markValue(x.Closed)
	case *ObjNative:
		// 原生函数是 Go 闭包，不持有 minilang 对象。
	}
}

// sweep 遍历对象堆，回收未标记的对象。
func (vm *VM) sweep() {
	var prev *objHeader
	h := vm.objects
	for h != nil {
		next := h.next
		if h.marked {
			h.marked = false // 为下一轮复位
			prev = h
		} else {
			if prev == nil {
				vm.objects = next
			} else {
				prev.next = next
			}
			vm.bytesAllocated -= h.size
			// 字符串还要从驻留表里摘掉，否则表里会留下指向已回收对象的指针，
			// 下次 InternString 查到它就会返回一个「已死」的对象。
			if s, ok := h.self.(*ObjString); ok {
				delete(vm.intern, s.Chars)
			}
		}
		h = next
	}
}
