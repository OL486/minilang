package engine

import "github.com/OL486/minilang/internal/semantics"

// Kind 是值的运行时标签。
//
// 为什么不用 Go 的 interface{} 直接当值：VM 的栈是 []Value，
// 每个 Value 只有 3 个机器字。用 interface{} 的话每次取值都要做一次
// 动态类型断言（有缓存查找开销），而且 GC 要扫的指针变多。
// 显式标签让「number 不进 GC 根集合」这个优化成立 —— 见 gc.go。
type Kind uint8

const (
	KindNil Kind = iota
	KindBool
	KindNumber
	KindObject
)

// Value 是 VM 栈上一个槽位的内容。
// 复用 num 字段存 bool（0/1），省一个字段。
type Value struct {
	kind Kind
	num  float64
	obj  Object
}

func NilValue() Value             { return Value{kind: KindNil} }
func NumberValue(f float64) Value { return Value{kind: KindNumber, num: f} }

func BoolValue(b bool) Value {
	v := Value{kind: KindBool}
	if b {
		v.num = 1
	}
	return v
}

func ObjValue(o Object) Value { return Value{kind: KindObject, obj: o} }

func (v Value) IsNil() bool       { return v.kind == KindNil }
func (v Value) IsBool() bool      { return v.kind == KindBool }
func (v Value) IsNumber() bool    { return v.kind == KindNumber }
func (v Value) IsObject() bool    { return v.kind == KindObject }
func (v Value) AsBool() bool      { return v.num != 0 }
func (v Value) AsNumber() float64 { return v.num }
func (v Value) AsObject() Object  { return v.obj }

// AsString 返回字符串对象；不是字符串时返回 nil。
// 故意不返回 error：调用点都是「先 IsString 再取」，
// 多一个 error 返回值只会让每个调用点多三行 if。
func (v Value) AsString() *ObjString {
	if v.kind != KindObject {
		return nil
	}
	s, _ := v.obj.(*ObjString)
	return s
}

func (v Value) AsClosure() *ObjClosure {
	if v.kind != KindObject {
		return nil
	}
	c, _ := v.obj.(*ObjClosure)
	return c
}

func (v Value) AsNative() *ObjNative {
	if v.kind != KindObject {
		return nil
	}
	n, _ := v.obj.(*ObjNative)
	return n
}

func (v Value) IsString() bool  { return v.AsString() != nil }
func (v Value) IsClosure() bool { return v.AsClosure() != nil }
func (v Value) IsNative() bool  { return v.AsNative() != nil }
func (v Value) IsCallable() bool {
	return v.IsClosure() || v.IsNative()
}

// toAny 把 VM 值转成 Go 原生值，供 semantics 的共享实现使用。
// 字符串对象转成 Go string 时内容相等即相等；
// 函数/闭包对象保持指针身份，与原来 valuesEqual 的指针比较一致。
func (v Value) toAny() any {
	switch v.kind {
	case KindNil:
		return nil
	case KindBool:
		return v.num != 0
	case KindNumber:
		return v.num
	case KindObject:
		if s := v.AsString(); s != nil {
			return s.Chars
		}
		return v.obj
	}
	return nil
}

// Truthy 与 tree 包保持一致：只有 nil 和 false 是假。
// 实现在 semantics.Truthy，只留这一行薄适配。
func (v Value) Truthy() bool { return semantics.Truthy(v.toAny()) }

// TypeName 用于错误信息。原始值部分用 semantics.TypeName，
// 对象部分按对象类型区分（字符串/函数/其他对象）。
func (v Value) TypeName() string {
	if v.kind == KindObject {
		switch v.obj.(type) {
		case *ObjString:
			return "string"
		case *ObjClosure, *ObjNative, *ObjFunction:
			return "function"
		}
		return "object"
	}
	return semantics.TypeName(v.toAny())
}

// Format 把值渲染成 print 的输出。必须与 tree.FormatValue 逐字节一致。
// 原始值部分由 semantics.FormatPrimitive 实现，这里只补对象分支。
func Format(v Value) string {
	if s, ok := semantics.FormatPrimitive(v.toAny()); ok {
		return s
	}
	switch o := v.obj.(type) {
	case *ObjClosure:
		return semantics.FormatFunc(displayName(o.Fn.Name), false)
	case *ObjFunction:
		return semantics.FormatFunc(displayName(o.Name), false)
	case *ObjNative:
		return semantics.FormatFunc(o.Name, true)
	case *ObjUpvalue:
		return "<upvalue>"
	}
	return "<unknown>"
}

func displayName(n string) string {
	if n == "" {
		return "script"
	}
	return n
}

// valuesEqual 是 == 的语义，委托给 semantics.Equal。
//
// 注意：toAny 把字符串对象转成 Go string 再比较内容，而不是比较指针。
// 在驻留（intern）的前提下两者结果完全一致；即使哪天去掉驻留，
// 内容比较仍能保持 == 语义正确，不会变成「内容相同却判不相等」的隐蔽 bug。
func valuesEqual(a, b Value) bool {
	return semantics.Equal(a.toAny(), b.toAny())
}
