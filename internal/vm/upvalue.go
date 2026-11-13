package vm

// ---------- upvalue ----------

// captureUpvalue 捕获一个栈槽位，返回指向它的 upvalue。
//
// 链表的顺序是整个 GC 里最容易写错的地方：必须按槽位降序（栈顶方向在前）。
// 因为 closeUpvalues 是从栈顶往下扫，遇到第一个「槽位低于边界」的就该停。
// 如果链表是无序的，closeUpvalues 会在扫到一半时提前停止，
// 导致一部分 upvalue 留在 open 状态、指向已经被复用的栈槽 ——
// 表现为「闭包里的变量莫名变成了别人的值」。
func (vm *VM) captureUpvalue(slot int) *ObjUpvalue {
	var prev *ObjUpvalue
	cur := vm.openUpvalues
	for cur != nil && cur.Slot > slot {
		prev = cur
		cur = cur.Next
	}
	if cur != nil && cur.Slot == slot {
		return cur
	}
	uv := vm.newUpvalue(slot)
	uv.Next = cur
	if prev == nil {
		vm.openUpvalues = uv
	} else {
		prev.Next = uv
	}
	return uv
}

// closeUpvalues 关闭所有「槽位下标 >= last」的 open upvalue。
// 把栈上的值拷到堆上的 Closed 字段，然后把 Location 指向 Closed。
func (vm *VM) closeUpvalues(last int) {
	for vm.openUpvalues != nil && vm.openUpvalues.Slot >= last {
		uv := vm.openUpvalues
		uv.Closed = *uv.Location
		uv.Location = &uv.Closed
		vm.openUpvalues = uv.Next
	}
}
