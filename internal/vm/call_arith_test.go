// Package vm 的调用（call.go）、upvalue（upvalue.go）、
// 算术（arith.go）与栈（stack.go）直接测试。
package vm

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// ---------- upvalue ----------

func TestCaptureUpvalueSameSlot(t *testing.T) {
	vm := NewVM(io.Discard)
	for i := 0; i < 4; i++ {
		vm.push(NumberValue(float64(i * 10)))
	}
	uv1 := vm.captureUpvalue(1)
	uv2 := vm.captureUpvalue(1)
	if uv1 != uv2 {
		t.Fatalf("捕获同一槽位应返回同一 upvalue，得到两个不同对象")
	}
	uv3 := vm.captureUpvalue(3)
	if uv3 == uv1 {
		t.Fatalf("捕获不同槽位不应返回同一 upvalue")
	}
	// 链表必须按槽位降序：链表头应是最大的槽位 3。
	if vm.openUpvalues == nil || vm.openUpvalues.Slot != 3 {
		t.Fatalf("open upvalue 链表头槽位 = %v, want 3", vm.openUpvalues)
	}
	if uv1.Location == nil || (*uv1.Location).AsNumber() != 10 {
		t.Fatalf("捕获的 upvalue 应指向栈槽的值 10，实际 %v", uv1.Location)
	}
}

func TestCloseUpvaluesPartial(t *testing.T) {
	vm := NewVM(io.Discard)
	for i := 0; i < 4; i++ {
		vm.push(NumberValue(float64(i)))
	}
	uv1 := vm.captureUpvalue(1)
	uv2 := vm.captureUpvalue(2)
	uv3 := vm.captureUpvalue(3)

	vm.closeUpvalues(2)
	// 槽位 3、2 已关闭，槽位 1 仍 open。
	if vm.openUpvalues != uv1 {
		t.Fatalf("closeUpvalues(2) 后链表头应是槽位 1 的 upvalue")
	}
	if uv2.Location != &uv2.Closed || uv2.Closed.AsNumber() != 2 {
		t.Fatalf("槽位 2 的 upvalue 关闭后 Closed 应为 2，实际 %v", uv2.Closed)
	}
	if uv3.Location != &uv3.Closed || uv3.Closed.AsNumber() != 3 {
		t.Fatalf("槽位 3 的 upvalue 关闭后 Closed 应为 3，实际 %v", uv3.Closed)
	}
	// 已关闭的值是拷贝：改栈不影响它。
	vm.stack[3] = NumberValue(99)
	if uv3.Closed.AsNumber() != 3 {
		t.Fatalf("改栈后已关闭的 upvalue 变成了 %v, want 3", uv3.Closed)
	}
	// 仍 open 的值还 alias 栈：改栈立刻可见。
	vm.stack[1] = NumberValue(11)
	if uv1.Location == nil || (*uv1.Location).AsNumber() != 11 {
		t.Fatalf("仍 open 的 upvalue 应跟随栈槽变化，实际 %v", uv1.Location)
	}
}

// ---------- 调用 ----------

func TestCallClosureFrameLimitDirect(t *testing.T) {
	vm := NewVM(io.Discard)
	fn := vm.NewFunction("f", 0)
	cl := vm.newClosure(fn)
	vm.frameCount = FramesMax
	err := vm.callClosure(cl, 0, 1)
	if err == nil || !strings.Contains(err.Error(), "调用栈溢出") {
		t.Fatalf("帧满时 callClosure 应报错调用栈溢出，实际 err = %v", err)
	}
	vm.frameCount = 0
	if err := vm.callClosure(cl, 0, 1); err != nil {
		t.Fatalf("空闲时 callClosure 不应报错，实际 err = %v", err)
	}
	if vm.frameCount != 1 {
		t.Fatalf("callClosure 后 frameCount = %d, want 1", vm.frameCount)
	}
}

func TestCallValueErrors(t *testing.T) {
	vm := NewVM(io.Discard)
	// 调用非函数。
	if err := vm.callValue(NumberValue(1), 0, 1); err == nil ||
		!strings.Contains(err.Error(), "不是函数，不能调用") {
		t.Fatalf("调用 number 应报错不是函数，实际 err = %v", err)
	}
	// 内置函数参数个数不对。
	clock, ok := vm.globals["clock"]
	if !ok {
		t.Fatalf("NewVM 应注册 clock")
	}
	if err := vm.callValue(clock, 1, 1); err == nil ||
		!strings.Contains(err.Error(), "内置函数 clock 需要 0 个参数") {
		t.Fatalf("clock(1) 应报错参数个数，实际 err = %v", err)
	}
	// 闭包参数个数不对。
	fn := vm.NewFunction("f", 2)
	cl := vm.newClosure(fn)
	if err := vm.callValue(ObjValue(cl), 1, 1); err == nil ||
		!strings.Contains(err.Error(), "函数 f 需要 2 个参数，传入了 1 个") {
		t.Fatalf("闭包参数个数不对时应报错，实际 err = %v", err)
	}
}

// ---------- 栈 ----------

func TestStackPushPopPeekAndOverflow(t *testing.T) {
	vm := NewVM(io.Discard)
	vm.push(NumberValue(1))
	vm.push(NumberValue(2))
	vm.push(NumberValue(3))
	if vm.peek(0).AsNumber() != 3 || vm.peek(2).AsNumber() != 1 {
		t.Fatalf("peek 取值不对")
	}
	if got := vm.pop(); got.AsNumber() != 3 || vm.sp != 2 {
		t.Fatalf("pop 后栈顶不对，sp = %d", vm.sp)
	}

	// 超过 StackMax 的 push 必须 panic(stackOverflow)，
	// 由 Run 的 recover 转成干净错误；这里直接验证 panic 的类型。
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatalf("push 超过 StackMax 应 panic")
			}
			if _, ok := r.(stackOverflow); !ok {
				t.Fatalf("panic 应为 stackOverflow 类型，实际 %T", r)
			}
		}()
		for i := 0; i < StackMax+10; i++ {
			vm.push(NilValue())
		}
	}()
}

func TestCaptureUpvalueMiddleInsert(t *testing.T) {
	// 先抓槽位 3、再抓槽位 1、最后抓槽位 2：2 必须插进链表中间，
	// 链表全程保持降序（3 → 2 → 1），否则 closeUpvalues 会提前停。
	vm := NewVM(io.Discard)
	for i := 0; i < 4; i++ {
		vm.push(NumberValue(float64(i)))
	}
	vm.captureUpvalue(3)
	vm.captureUpvalue(1)
	vm.captureUpvalue(2)
	var slots []int
	for uv := vm.openUpvalues; uv != nil; uv = uv.Next {
		slots = append(slots, uv.Slot)
	}
	want := []int{3, 2, 1}
	if len(slots) != len(want) {
		t.Fatalf("open upvalue 链表 = %v, want %v", slots, want)
	}
	for i := range want {
		if slots[i] != want[i] {
			t.Fatalf("open upvalue 链表 = %v, want %v", slots, want)
		}
	}
}

func TestArithUnknownOp(t *testing.T) {
	// arith() 收到没有数值语义的操作码时，应报内部错误而不是静默算错。
	// OpAdd 有 token 映射（Plus），但 semantics.Arithmetic 不处理加法，
	// 正好走到 ok == false 的分支。
	vm := NewVM(io.Discard)
	vm.push(NumberValue(1))
	vm.push(NumberValue(2))
	err := vm.arith(OpAdd, 1)
	if err == nil || !strings.Contains(err.Error(), "内部错误：未知操作码") {
		t.Fatalf("arith(OpAdd) 应报未知操作码，实际 err = %v", err)
	}
}

func TestOpTokenMapping(t *testing.T) {
	if _, ok := opToken(OpPrint); ok {
		t.Fatalf("opToken(OpPrint) 应返回 ok=false")
	}
	if got := arithToken(OpPrint); got != 0 {
		t.Fatalf("arithToken(OpPrint) = %v, want 0", got)
	}
	if got := compareToken(OpPrint); got != 0 {
		t.Fatalf("compareToken(OpPrint) = %v, want 0", got)
	}
	if got := opSymbol(OpPrint); got != "OP_PRINT" {
		t.Fatalf("opSymbol(OpPrint) = %q, want %q", got, "OP_PRINT")
	}
	if got := opSymbol(OpAdd); got != "+" {
		t.Fatalf("opSymbol(OpAdd) = %q, want %q", got, "+")
	}
}

func TestObjTypeStringAndNilHeaders(t *testing.T) {
	cases := []struct {
		typ  ObjType
		want string
	}{
		{ObjStringType, "string"},
		{ObjFunctionType, "function"},
		{ObjClosureType, "closure"},
		{ObjUpvalueType, "upvalue"},
		{ObjNativeType, "native"},
		{ObjType(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.typ.String(); got != c.want {
			t.Errorf("ObjType(%d).String() = %q, want %q", byte(c.typ), got, c.want)
		}
	}

	// nil 接收者的 header() 必须返回 nil 而不是 panic：
	// GC 标记时可能遇到还没填好的槽位（见 Object 接口注释）。
	var s *ObjString
	if s.header() != nil {
		t.Errorf("(*ObjString)(nil).header() 应为 nil")
	}
	var f *ObjFunction
	if f.header() != nil {
		t.Errorf("(*ObjFunction)(nil).header() 应为 nil")
	}
	var u *ObjUpvalue
	if u.header() != nil {
		t.Errorf("(*ObjUpvalue)(nil).header() 应为 nil")
	}
	var cl *ObjClosure
	if cl.header() != nil {
		t.Errorf("(*ObjClosure)(nil).header() 应为 nil")
	}
	var n *ObjNative
	if n.header() != nil {
		t.Errorf("(*ObjNative)(nil).header() 应为 nil")
	}
}

// fakeObject 是一个测试专用的 Object 实现：不属于 5 种真实对象类型，
// 用来覆盖 Format / TypeName 里「未知对象」的默认分支。
type fakeObject struct{ objHeader }

func (f *fakeObject) header() *objHeader { return &f.objHeader }

func TestValueAccessorsExtra(t *testing.T) {
	vm := NewVM(io.Discard)
	fn := vm.NewFunction("f", 0)
	cl := vm.newClosure(fn)
	nat := vm.globals["clock"]

	// 字符串是对象，但不是可调用对象。
	if v := ObjValue(vm.InternString("s")); !v.IsObject() || v.IsClosure() || v.IsNative() || v.IsCallable() {
		t.Errorf("字符串应 IsObject 且不是闭包/内置函数/可调用对象，实际 %v", v)
	}
	if v := ObjValue(cl); !v.IsClosure() || !v.IsCallable() {
		t.Errorf("闭包应 IsClosure 且 IsCallable，实际 %v", v)
	}
	if v := nat; !v.IsNative() || !v.IsCallable() {
		t.Errorf("内置函数应 IsNative 且 IsCallable，实际 %v", v)
	}
	if v := NumberValue(1); v.IsClosure() || v.IsNative() || v.IsCallable() {
		t.Errorf("数字不应是闭包/内置函数/可调用对象")
	}
	if got := Format(ObjValue(cl)); got != "<fn f>" {
		t.Errorf("Format(闭包) = %q, want %q", got, "<fn f>")
	}
	uv := &ObjUpvalue{}
	if got := Format(ObjValue(uv)); got != "<upvalue>" {
		t.Errorf("Format(upvalue) = %q, want %q", got, "<upvalue>")
	}
	if got := ObjValue(uv).TypeName(); got != "object" {
		t.Errorf("TypeName(upvalue) = %q, want %q", got, "object")
	}
	if got := Format(ObjValue(&fakeObject{})); got != "<unknown>" {
		t.Errorf("Format(未知对象) = %q, want %q", got, "<unknown>")
	}
	if got := ObjValue(&fakeObject{}).TypeName(); got != "object" {
		t.Errorf("TypeName(未知对象) = %q, want %q", got, "object")
	}
	// 非法 kind 的 toAny 应返回 nil，而不是 panic。
	if got := (Value{kind: Kind(99)}).toAny(); got != nil {
		t.Errorf("toAny(非法 kind) = %v, want nil", got)
	}
}

func TestVMStatsAndGlobal(t *testing.T) {
	vm := NewVM(io.Discard)
	vm.InternString("a")
	vm.InternString("b")
	st := vm.Stats()
	if st.Objects < 3 { // clock + 两个字符串
		t.Fatalf("Stats().Objects = %d, want >= 3", st.Objects)
	}
	if st.ObjectsTotal < 3 {
		t.Fatalf("Stats().ObjectsTotal = %d, want >= 3", st.ObjectsTotal)
	}
	if st.Bytes <= 0 {
		t.Fatalf("Stats().Bytes = %d, want > 0", st.Bytes)
	}
	if v, ok := vm.Global("clock"); !ok || !v.IsNative() {
		t.Fatalf("Global(\"clock\") 应找到内置函数，实际 ok=%v", ok)
	}
	if _, ok := vm.Global("nope"); ok {
		t.Fatalf("Global(\"nope\") 不应找到")
	}
}

func TestCallNativeWithArgsAndError(t *testing.T) {
	// 内置函数成功路径（带实参拷贝）与出错包装路径。
	// clock 是 0 参数，覆盖不了 args 拷贝循环，必须自定义内置函数。
	vm := NewVM(io.Discard)
	vm.defineNative("echo", 1, func(_ *VM, args []Value) (Value, error) {
		return args[0], nil
	})
	vm.defineNative("boom", 0, func(_ *VM, _ []Value) (Value, error) {
		return NilValue(), errors.New("boom")
	})

	// echo(42)：栈上依次是 callee、实参；调用后栈顶应是返回值。
	vm.push(vm.globals["echo"])
	vm.push(NumberValue(42))
	if err := vm.callValue(vm.peek(1), 1, 1); err != nil {
		t.Fatalf("echo(42) 不应报错，实际 err = %v", err)
	}
	if got := vm.peek(0); !got.IsNumber() || got.AsNumber() != 42 {
		t.Fatalf("echo(42) 的栈顶 = %v, want 42", got)
	}
	if vm.sp != 1 {
		t.Fatalf("echo 调用后 sp = %d, want 1（callee+实参被返回值替换）", vm.sp)
	}

	// boom()：内置函数返回 error 应被包装成 diag 错误。
	vm.push(vm.globals["boom"])
	if err := vm.callValue(vm.peek(0), 0, 1); err == nil ||
		!strings.Contains(err.Error(), "内置函数 boom 出错") {
		t.Fatalf("boom() 应报内置函数出错，实际 err = %v", err)
	}
}

func TestMarkObjectNil(t *testing.T) {
	// markObject(nil) 必须静默返回：blacken 时可能遇到 nil upvalue 槽位
	// 以外的 nil 对象，不能 panic。
	vm := NewVM(io.Discard)
	vm.markObject(nil) // 不应 panic
	var s *ObjString
	vm.markObject(s) // nil *ObjString 装进接口也不应 panic（header() 容忍 nil）
}
