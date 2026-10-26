package engine

import (
	"fmt"
	"io"
	"math"
	"time"

	"github.com/OL486/minilang/internal/diag"
)

const (
	// StackMax 是操作数栈的槽位上限。
	// 预先一次性分配、之后永不 realloc —— 这不是性能考虑，是正确性要求：
	// ObjUpvalue.Location 直接指向栈内某个槽位的地址，
	// 如果栈扩容时搬了家，所有 open upvalue 都会指向已经失效的内存。
	StackMax = 1024 * 32

	// FramesMax 是调用帧上限，防止无限递归把 Go 的栈打爆。
	FramesMax = 1024
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
// 每次创建字符串都查表，内容相同就复用同一个对象。
// 于是「内容相等」和「指针相等」变成同一件事，
// == 可以只比较指针（见 valuesEqual），拼接出来的 "ab" 也能和字面量 "ab" 判等。
//
// 代价：每次拼接都要做一次 map 查找 + 可能的分配。
// 这也是 S5→S10 对比表里 VM 在字符串密集场景下不一定更快的原因之一。
func (vm *VM) InternString(s string) *ObjString {
	if o, ok := vm.intern[s]; ok {
		return o
	}
	o := &ObjString{Chars: s, Hash: hashString(s)}
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

// ---------- 算术 ----------

func (vm *VM) arith(op Op, line int) error {
	b := vm.pop()
	a := vm.pop()
	if !a.IsNumber() || !b.IsNumber() {
		return diag.New(diag.StageRuntime, line, 0,
			"运算符 %s 不支持 %s 与 %s", opSymbol(op), a.TypeName(), b.TypeName())
	}
	x, y := a.AsNumber(), b.AsNumber()
	var r float64
	switch op {
	case OpSubtract:
		r = x - y
	case OpMultiply:
		r = x * y
	case OpDivide:
		r = x / y
	default:
		r = math.Mod(x, y)
	}
	vm.push(NumberValue(r))
	return nil
}

func (vm *VM) add(line int) error {
	b := vm.peek(0)
	a := vm.peek(1)
	if a.IsNumber() && b.IsNumber() {
		vm.pop()
		vm.pop()
		vm.push(NumberValue(a.AsNumber() + b.AsNumber()))
		return nil
	}
	if a.IsString() && b.IsString() {
		// 注意：两个操作数在 internString 期间都还在栈上，
		// 所以即使这里触发 GC，它们也是根。先 pop 再分配就会踩到
		// 「拼接结果被回收」的坑（只在 GC 恰好触发时出现）。
		s := vm.InternString(a.AsString().Chars + b.AsString().Chars)
		vm.pop()
		vm.pop()
		vm.push(ObjValue(s))
		return nil
	}
	return diag.New(diag.StageRuntime, line, 0,
		"运算符 + 不支持 %s 与 %s", a.TypeName(), b.TypeName())
}

func (vm *VM) compareAndPush(op Op, line int) error {
	b := vm.pop()
	a := vm.pop()
	var res bool
	switch {
	case a.IsNumber() && b.IsNumber():
		x, y := a.AsNumber(), b.AsNumber()
		switch op {
		case OpGreater:
			res = x > y
		case OpGreaterEqual:
			res = x >= y
		case OpLess:
			res = x < y
		default:
			res = x <= y
		}
	case a.IsString() && b.IsString():
		x, y := a.AsString().Chars, b.AsString().Chars
		switch op {
		case OpGreater:
			res = x > y
		case OpGreaterEqual:
			res = x >= y
		case OpLess:
			res = x < y
		default:
			res = x <= y
		}
	default:
		return diag.New(diag.StageRuntime, line, 0,
			"运算符 %s 不支持 %s 与 %s", opSymbol(op), a.TypeName(), b.TypeName())
	}
	vm.push(BoolValue(res))
	return nil
}

func opSymbol(op Op) string {
	switch op {
	case OpAdd:
		return "+"
	case OpSubtract:
		return "-"
	case OpMultiply:
		return "*"
	case OpDivide:
		return "/"
	case OpModulo:
		return "%"
	case OpGreater:
		return ">"
	case OpGreaterEqual:
		return ">="
	case OpLess:
		return "<"
	case OpLessEqual:
		return "<="
	}
	return op.String()
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
