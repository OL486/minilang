package semantics

import (
	"math"
	"testing"

	"github.com/OL486/minilang/internal/token"
)

func TestNumber(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{1, "1"},
		{-2, "-2"},
		{0, "0"},
		{0.1, "0.1"},
		{3.5, "3.5"},
		{1000000, "1000000"},
		{1e15, "1000000000000000"},
		{1e16, "1e+16"},
		{math.NaN(), "nan"},
		{math.Inf(1), "+inf"},
		{math.Inf(-1), "-inf"},
	}
	for _, c := range cases {
		if got := Number(c.in); got != c.want {
			t.Errorf("Number(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{2048, "2.00 KiB"},
		{2 << 20, "2.00 MiB"},
	}
	for _, c := range cases {
		if got := HumanBytes(c.in); got != c.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := SignedBytes(-2048); got != "-2.00 KiB" {
		t.Errorf("SignedBytes(-2048) = %q, want %q", got, "-2.00 KiB")
	}
	if got := SignedBytes(512); got != "+512 B" {
		t.Errorf("SignedBytes(512) = %q, want %q", got, "+512 B")
	}
}

func TestTruthy(t *testing.T) {
	cases := []struct {
		in   any
		want bool
	}{
		{nil, false},
		{false, false},
		{true, true},
		{float64(0), true},
		{float64(1), true},
		{"", true},
		{"x", true},
	}
	for _, c := range cases {
		if got := Truthy(c.in); got != c.want {
			t.Errorf("Truthy(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestTypeName(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, "nil"},
		{true, "bool"},
		{float64(1), "number"},
		{"s", "string"},
	}
	for _, c := range cases {
		if got := TypeName(c.in); got != c.want {
			t.Errorf("TypeName(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEqual(t *testing.T) {
	cases := []struct {
		l, r any
		want bool
	}{
		{nil, nil, true},
		{nil, float64(0), false},
		{true, true, true},
		{true, false, false},
		{float64(1), float64(1), true},
		{float64(1), float64(1.0), true},
		{float64(1), "1", false},
		{"x", "x", true},
		{"x", "y", false},
	}
	for _, c := range cases {
		if got := Equal(c.l, c.r); got != c.want {
			t.Errorf("Equal(%v, %v) = %v, want %v", c.l, c.r, got, c.want)
		}
	}
	// 函数值按身份比较（minilang 的函数值是指针类型，可比较）。
	p := &struct{ name string }{"f"}
	if !Equal(p, p) {
		t.Error("Equal(p, p) should be true")
	}
	if Equal(p, &struct{ name string }{"f"}) {
		t.Error("Equal(p, q) with different pointers should be false")
	}
}

func TestFormatPrimitive(t *testing.T) {
	cases := []struct {
		in   any
		want string
		ok   bool
	}{
		{nil, "nil", true},
		{true, "true", true},
		{false, "false", true},
		{float64(1), "1", true},
		{float64(0.5), "0.5", true},
		{"hi", "hi", true},
		{struct{}{}, "", false},
	}
	for _, c := range cases {
		got, ok := FormatPrimitive(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("FormatPrimitive(%v) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
	if got := FormatFunc("add", false); got != "<fn add>" {
		t.Errorf("FormatFunc(add) = %q", got)
	}
	if got := FormatFunc("", false); got != "<fn script>" {
		t.Errorf("FormatFunc(\"\") = %q", got)
	}
	if got := FormatFunc("clock", true); got != "<native fn clock>" {
		t.Errorf("FormatFunc(clock, native) = %q", got)
	}
}

func TestOpText(t *testing.T) {
	// token.OpText 是全仓库唯一一份运算符文本表，
	// 这里重点回归以前 tree 侧缺失的三项。
	if got := token.OpText(token.Bang); got != "!" {
		t.Errorf("OpText(Bang) = %q, want %q", got, "!")
	}
	if got := token.OpText(token.AndAnd); got != "&&" {
		t.Errorf("OpText(AndAnd) = %q, want %q", got, "&&")
	}
	if got := token.OpText(token.OrOr); got != "||" {
		t.Errorf("OpText(OrOr) = %q, want %q", got, "||")
	}
	if got := token.OpText(token.Plus); got != "+" {
		t.Errorf("OpText(Plus) = %q, want %q", got, "+")
	}
	if got := token.OpText(token.Ge); got != ">=" {
		t.Errorf("OpText(Ge) = %q, want %q", got, ">=")
	}
}

func TestBinaryOp(t *testing.T) {
	cases := []struct {
		op   token.Type
		l, r any
		want any
		ok   bool
	}{
		{token.Plus, float64(1), float64(2), float64(3), true},
		{token.Plus, "a", "b", "ab", true},
		{token.Plus, float64(1), "a", nil, false},
		{token.Minus, float64(5), float64(2), float64(3), true},
		{token.Star, float64(2), float64(3), float64(6), true},
		{token.Slash, float64(7), float64(2), float64(3.5), true},
		{token.Percent, float64(7), float64(3), float64(1), true},
		{token.Minus, "a", "b", nil, false},
		{token.Eq, float64(1), float64(1), true, true},
		{token.Eq, float64(1), "1", false, true},
		{token.Ne, float64(1), float64(2), true, true},
		{token.Lt, float64(1), float64(2), true, true},
		{token.Le, float64(2), float64(2), true, true},
		{token.Gt, "b", "a", true, true},
		{token.Ge, "a", "b", false, true},
		{token.Lt, float64(1), "a", false, false},
		{token.Lt, "a", float64(1), false, false},
	}
	for _, c := range cases {
		got, ok := BinaryOp(c.op, c.l, c.r)
		if got != c.want || ok != c.ok {
			t.Errorf("BinaryOp(%v, %v, %v) = (%v, %v), want (%v, %v)",
				c.op, c.l, c.r, got, ok, c.want, c.ok)
		}
	}
	if IsBinaryOperator(token.Bang) {
		t.Error("IsBinaryOperator(Bang) should be false")
	}
	if !IsBinaryOperator(token.Plus) || !IsBinaryOperator(token.Eq) {
		t.Error("IsBinaryOperator should be true for + and ==")
	}
}

func TestArithmeticAndCompare(t *testing.T) {
	if got, ok := Arithmetic(token.Slash, 1, 0); !ok || !math.IsInf(got, 1) {
		t.Errorf("Arithmetic(/, 1, 0) = (%v, %v), want (+Inf, true)", got, ok)
	}
	if _, ok := Arithmetic(token.Plus, 1, 2); ok {
		t.Error("Arithmetic(+) should be false: + is handled by Add, not Arithmetic")
	}
	if got, ok := Compare(token.Lt, float64(1), float64(2)); !ok || !got {
		t.Errorf("Compare(<, 1, 2) = (%v, %v), want (true, true)", got, ok)
	}
	if _, ok := Compare(token.Lt, float64(1), "a"); ok {
		t.Error("Compare(<, number, string) should be false")
	}
}
