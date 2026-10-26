package engine

import "github.com/OL486/minilang/internal/numfmt"

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

// Truthy 与 tree 包保持一致：只有 nil 和 false 是假。
func (v Value) Truthy() bool {
	switch v.kind {
	case KindNil:
		return false
	case KindBool:
		return v.num != 0
	}
	return true
}

// TypeName 用于错误信息。
func (v Value) TypeName() string {
	switch v.kind {
	case KindNil:
		return "nil"
	case KindBool:
		return "bool"
	case KindNumber:
		return "number"
	case KindObject:
		switch v.obj.(type) {
		case *ObjString:
			return "string"
		case *ObjClosure, *ObjNative, *ObjFunction:
			return "function"
		}
	}
	return "object"
}

// Format 把值渲染成 print 的输出。必须与 tree.FormatValue 逐字节一致。
func Format(v Value) string {
	switch v.kind {
	case KindNil:
		return "nil"
	case KindBool:
		if v.num != 0 {
			return "true"
		}
		return "false"
	case KindNumber:
		return numfmt.Number(v.num)
	case KindObject:
		switch o := v.obj.(type) {
		case *ObjString:
			return o.Chars
		case *ObjClosure:
			return "<fn " + displayName(o.Fn.Name) + ">"
		case *ObjFunction:
			return "<fn " + displayName(o.Name) + ">"
		case *ObjNative:
			return "<native fn " + o.Name + ">"
		case *ObjUpvalue:
			return "<upvalue>"
		}
	}
	return "<unknown>"
}

func displayName(n string) string {
	if n == "" {
		return "script"
	}
	return n
}

// valuesEqual 是 == 的语义。
//
// 字符串在这里只比较指针 —— 这是合法的，因为所有字符串都经过驻留（intern），
// 内容相同的字符串一定是同一个对象。详见 vm.internString 的注释。
// 如果哪天去掉了驻留，这一行就会变成「内容相同却判不相等」的隐蔽 bug，
// 所以驻留不是性能优化，是 == 语义的前提。
func valuesEqual(a, b Value) bool {
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case KindNil:
		return true
	case KindBool, KindNumber:
		return a.num == b.num
	case KindObject:
		return a.obj == b.obj
	}
	return false
}
