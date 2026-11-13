package engine

import (
	"io"
	"testing"
)

func TestToAny(t *testing.T) {
	vm := NewVM(io.Discard)

	cases := []struct {
		in   Value
		want any
	}{
		{NilValue(), nil},
		{BoolValue(true), true},
		{BoolValue(false), false},
		{NumberValue(1.5), float64(1.5)},
		{ObjValue(vm.InternString("hi")), "hi"},
	}
	for _, c := range cases {
		if got := c.in.toAny(); got != c.want {
			t.Errorf("toAny(%v) = %v (%T), want %v", c.in, got, got, c.want)
		}
	}

	// 函数/闭包对象保持指针身份，不转成别的类型。
	fn := vm.NewFunction("f", 0)
	if got := ObjValue(fn).toAny(); got != any(fn) {
		t.Errorf("toAny(ObjFunction) = %v, want the function object itself", got)
	}
}

func TestValuesEqual(t *testing.T) {
	vm := NewVM(io.Discard)
	sx := vm.InternString("x")
	sy := vm.InternString("y")

	cases := []struct {
		a, b Value
		want bool
	}{
		{NumberValue(1), NumberValue(1), true},
		{NumberValue(1), NumberValue(2), false},
		{NumberValue(1), ObjValue(sx), false}, // 跨类型一律 false
		{BoolValue(true), BoolValue(true), true},
		{BoolValue(true), BoolValue(false), false},
		{NilValue(), NilValue(), true},
		{NilValue(), NumberValue(0), false},
		{ObjValue(sx), ObjValue(vm.InternString("x")), true}, // 同内容字符串相等
		{ObjValue(sx), ObjValue(sy), false},
	}
	for _, c := range cases {
		if got := valuesEqual(c.a, c.b); got != c.want {
			t.Errorf("valuesEqual(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestFormat(t *testing.T) {
	vm := NewVM(io.Discard)

	cases := []struct {
		in   Value
		want string
	}{
		{NilValue(), "nil"},
		{BoolValue(true), "true"},
		{BoolValue(false), "false"},
		{NumberValue(2), "2"},
		{NumberValue(0.5), "0.5"},
		{ObjValue(vm.InternString("hi")), "hi"},
		{ObjValue(vm.NewFunction("add", 2)), "<fn add>"},
		{ObjValue(vm.NewFunction("", 0)), "<fn script>"},
		{ObjValue(&ObjNative{Name: "clock", Arity: 0}), "<native fn clock>"},
	}
	for _, c := range cases {
		if got := Format(c.in); got != c.want {
			t.Errorf("Format(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValueTypeName(t *testing.T) {
	vm := NewVM(io.Discard)
	fn := vm.NewFunction("f", 0)

	cases := []struct {
		in   Value
		want string
	}{
		{NilValue(), "nil"},
		{BoolValue(true), "bool"},
		{NumberValue(1), "number"},
		{ObjValue(vm.InternString("s")), "string"},
		{ObjValue(fn), "function"},
	}
	for _, c := range cases {
		if got := c.in.TypeName(); got != c.want {
			t.Errorf("TypeName(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValueAccessors(t *testing.T) {
	if v := NumberValue(3); !v.IsNumber() || v.AsNumber() != 3 {
		t.Errorf("NumberValue accessors broken: %v", v)
	}
	if v := BoolValue(true); !v.IsBool() || !v.AsBool() {
		t.Errorf("BoolValue accessors broken: %v", v)
	}
	if v := NilValue(); !v.IsNil() {
		t.Errorf("NilValue should be nil")
	}
	vm := NewVM(io.Discard)
	s := vm.InternString("x")
	if v := ObjValue(s); !v.IsString() || v.AsString() != s {
		t.Errorf("ObjValue(string) accessors broken")
	}
	if v := NumberValue(1); v.IsString() || v.AsString() != nil {
		t.Errorf("NumberValue should not be a string")
	}
}
