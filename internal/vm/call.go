package vm

import (
	"github.com/OL486/minilang/internal/diag"
)

// ---------- 调用 ----------

func (vm *VM) callValue(callee Value, argc int, line int) error {
	if cl := callee.AsClosure(); cl != nil {
		return vm.callClosure(cl, argc, line)
	}
	if nat := callee.AsNative(); nat != nil {
		if argc != nat.Arity {
			return diag.New(diag.StageRuntime, line, 0,
				"内置函数 %s 需要 %d 个参数，传入了 %d 个", nat.Name, nat.Arity, argc)
		}
		args := make([]Value, argc)
		for i := 0; i < argc; i++ {
			args[i] = vm.stack[vm.sp-argc+i]
		}
		res, err := nat.Fn(vm, args)
		if err != nil {
			return diag.New(diag.StageRuntime, line, 0, "内置函数 %s 出错: %v", nat.Name, err)
		}
		vm.sp -= argc + 1
		vm.push(res)
		return nil
	}
	return diag.New(diag.StageRuntime, line, 0, "%s 不是函数，不能调用", callee.TypeName())
}

func (vm *VM) callClosure(cl *ObjClosure, argc int, line int) error {
	if argc != cl.Fn.Arity {
		return diag.New(diag.StageRuntime, line, 0,
			"函数 %s 需要 %d 个参数，传入了 %d 个", displayName(cl.Fn.Name), cl.Fn.Arity, argc)
	}
	if vm.frameCount >= FramesMax {
		return diag.New(diag.StageRuntime, line, 0, "调用栈溢出：调用帧超过 %d 层", FramesMax)
	}
	vm.frames[vm.frameCount] = callFrame{
		closure:  cl,
		ip:       0,
		slotBase: vm.sp - argc - 1, // 槽位 0 是函数自身
	}
	vm.frameCount++
	return nil
}
