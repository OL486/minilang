package vm

// ---------- 栈 ----------

type stackOverflow struct{}

func (vm *VM) push(v Value) {
	if vm.sp >= StackMax {
		panic(stackOverflow{})
	}
	vm.stack[vm.sp] = v
	vm.sp++
}

func (vm *VM) pop() Value {
	vm.sp--
	return vm.stack[vm.sp]
}

// peek 返回从栈顶往下第 distance 个值，不弹栈。distance=0 就是栈顶。
func (vm *VM) peek(distance int) Value {
	return vm.stack[vm.sp-1-distance]
}
