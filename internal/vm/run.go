package vm

import (
	"fmt"

	"github.com/OL486/minilang/internal/diag"
)

// ---------- 执行 ----------

// RunScript 执行一个已编译好的脚本函数。
func (vm *VM) RunScript(fn *ObjFunction) (Value, error) {
	vm.sp = 0
	vm.frameCount = 0
	vm.openUpvalues = nil

	cl := vm.newClosure(fn)
	vm.push(ObjValue(cl)) // 脚本闭包占用槽位 0
	if err := vm.callClosure(cl, 0, 0); err != nil {
		vm.sp = 0
		return NilValue(), err
	}
	return vm.Run()
}

// Run 是分派循环。
func (vm *VM) Run() (result Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(stackOverflow); !ok {
				panic(r)
			}
			result = NilValue()
			err = diag.New(diag.StageRuntime, vm.curLine, 0,
				"VM 栈溢出：操作数栈超过 %d 个槽位（常见原因：无限递归、或表达式嵌套过深）", StackMax)
		}
	}()

	var f *callFrame
	for {
		// 每次迭代重新取当前帧：OP_CALL / OP_RETURN 会改变 frameCount，
		// 缓存的指针就失效了。这一行的开销换掉了整整一类「帧错乱」bug。
		f = &vm.frames[vm.frameCount-1]
		chunk := f.closure.Fn.Chunk
		op := Op(chunk.Code[f.ip])
		f.ip++
		line := chunk.LineAt(f.ip - 1)
		vm.curLine = line

		switch op {
		case OpConstant:
			idx := int(chunk.Code[f.ip])
			f.ip++
			vm.push(chunk.Constants[idx])

		case OpNil:
			vm.push(NilValue())
		case OpTrue:
			vm.push(BoolValue(true))
		case OpFalse:
			vm.push(BoolValue(false))
		case OpPop:
			vm.pop()

		case OpGetLocal:
			slot := int(chunk.Code[f.ip])
			f.ip++
			vm.push(vm.stack[f.slotBase+slot])
		case OpSetLocal:
			// 赋值不弹栈：赋值表达式的值是右值，它要留在栈顶给外层表达式用。
			// 如果这里写成 pop 再 push，`x = y = 1;` 的中间结果就没了。
			slot := int(chunk.Code[f.ip])
			f.ip++
			vm.stack[f.slotBase+slot] = vm.peek(0)

		case OpGetGlobal:
			name := chunk.Constants[chunk.Code[f.ip]].AsString()
			f.ip++
			v, ok := vm.globals[name.Chars]
			if !ok {
				return NilValue(), diag.New(diag.StageRuntime, line, 0, "未定义的变量 '%s'", name.Chars)
			}
			vm.push(v)
		case OpDefineGlobal:
			name := chunk.Constants[chunk.Code[f.ip]].AsString()
			f.ip++
			vm.globals[name.Chars] = vm.pop()
		case OpSetGlobal:
			name := chunk.Constants[chunk.Code[f.ip]].AsString()
			f.ip++
			if _, ok := vm.globals[name.Chars]; !ok {
				return NilValue(), diag.New(diag.StageRuntime, line, 0,
					"不能给未定义的变量 '%s' 赋值（minilang 没有隐式全局变量）", name.Chars)
			}
			vm.globals[name.Chars] = vm.peek(0)

		case OpGetUpvalue:
			idx := int(chunk.Code[f.ip])
			f.ip++
			vm.push(*f.closure.Upvalues[idx].Location)
		case OpSetUpvalue:
			idx := int(chunk.Code[f.ip])
			f.ip++
			*f.closure.Upvalues[idx].Location = vm.peek(0)

		case OpEqual:
			b := vm.pop()
			a := vm.pop()
			vm.push(BoolValue(valuesEqual(a, b)))
		case OpGreater:
			if err := vm.compareAndPush(OpGreater, line); err != nil {
				return NilValue(), err
			}
		case OpGreaterEqual:
			if err := vm.compareAndPush(OpGreaterEqual, line); err != nil {
				return NilValue(), err
			}
		case OpLess:
			if err := vm.compareAndPush(OpLess, line); err != nil {
				return NilValue(), err
			}
		case OpLessEqual:
			if err := vm.compareAndPush(OpLessEqual, line); err != nil {
				return NilValue(), err
			}
		case OpAdd:
			if err := vm.add(line); err != nil {
				return NilValue(), err
			}
		case OpSubtract:
			if err := vm.arith(OpSubtract, line); err != nil {
				return NilValue(), err
			}
		case OpMultiply:
			if err := vm.arith(OpMultiply, line); err != nil {
				return NilValue(), err
			}
		case OpDivide:
			if err := vm.arith(OpDivide, line); err != nil {
				return NilValue(), err
			}
		case OpModulo:
			if err := vm.arith(OpModulo, line); err != nil {
				return NilValue(), err
			}
		case OpNot:
			vm.push(BoolValue(!vm.pop().Truthy()))
		case OpNegate:
			v := vm.pop()
			if !v.IsNumber() {
				return NilValue(), diag.New(diag.StageRuntime, line, 0,
					"一元 - 只能作用于 number，实际是 %s", v.TypeName())
			}
			vm.push(NumberValue(-v.AsNumber()))

		case OpPrint:
			fmt.Fprintln(vm.out, Format(vm.pop()))

		case OpJump:
			jump := int(chunk.Code[f.ip])<<8 | int(chunk.Code[f.ip+1])
			f.ip += 2 + jump
		case OpJumpIfFalse:
			jump := int(chunk.Code[f.ip])<<8 | int(chunk.Code[f.ip+1])
			f.ip += 2
			if !vm.peek(0).Truthy() {
				f.ip += jump
			}
		case OpLoop:
			jump := int(chunk.Code[f.ip])<<8 | int(chunk.Code[f.ip+1])
			f.ip += 2 - jump

		case OpCall:
			argc := int(chunk.Code[f.ip])
			f.ip++
			if err := vm.callValue(vm.peek(argc), argc, line); err != nil {
				return NilValue(), err
			}

		case OpClosure:
			idx := int(chunk.Code[f.ip])
			f.ip++
			fn := chunk.Constants[idx].AsObject().(*ObjFunction)
			cl := vm.newClosure(fn)
			// 先入栈再填 upvalue：填的过程中会分配 ObjUpvalue，
			// 可能触发 GC。此时闭包必须已经是一个根，否则会被回收。
			vm.push(ObjValue(cl))
			for i := 0; i < fn.UpvalueCount; i++ {
				isLocal := chunk.Code[f.ip] == 1
				f.ip++
				index := int(chunk.Code[f.ip])
				f.ip++
				if isLocal {
					cl.Upvalues[i] = vm.captureUpvalue(f.slotBase + index)
				} else {
					cl.Upvalues[i] = f.closure.Upvalues[index]
				}
			}

		case OpCloseUpvalue:
			vm.closeUpvalues(vm.sp - 1)
			vm.pop()

		case OpReturn:
			result := vm.pop()
			vm.closeUpvalues(f.slotBase)
			vm.frameCount--
			if vm.frameCount == 0 {
				vm.pop() // 弹出脚本闭包
				return result, nil
			}
			vm.sp = f.slotBase
			vm.push(result)

		default:
			return NilValue(), diag.New(diag.StageRuntime, line, 0, "内部错误：未知操作码 %d", byte(op))
		}
	}
}
