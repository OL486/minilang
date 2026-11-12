package token

import "testing"

func TestLookupIdent(t *testing.T) {
	if got := LookupIdent("let"); got != Let {
		t.Errorf("LookupIdent(let) = %v, want LET", got)
	}
	if got := LookupIdent("fn"); got != Fn {
		t.Errorf("LookupIdent(fn) = %v, want FN", got)
	}
	if got := LookupIdent("myVar"); got != Ident {
		t.Errorf("LookupIdent(myVar) = %v, want IDENT", got)
	}
	if got := LookupIdent("print"); got != Print {
		t.Errorf("LookupIdent(print) = %v, want PRINT", got)
	}
}

func TestTypeString(t *testing.T) {
	if got := Plus.String(); got != "PLUS" {
		t.Errorf("Plus.String() = %q, want %q", got, "PLUS")
	}
	if got := EOF.String(); got != "EOF" {
		t.Errorf("EOF.String() = %q, want %q", got, "EOF")
	}
}

func TestOpText(t *testing.T) {
	cases := map[Type]string{
		Plus: "+", Minus: "-", Star: "*", Slash: "/", Percent: "%",
		Bang: "!", Eq: "==", Ne: "!=", Lt: "<", Le: "<=",
		Gt: ">", Ge: ">=", AndAnd: "&&", OrOr: "||",
	}
	for typ, want := range cases {
		if got := OpText(typ); got != want {
			t.Errorf("OpText(%v) = %q, want %q", typ, got, want)
		}
	}
	// 非运算符类型回退到类型名。
	if got := OpText(Ident); got != "IDENT" {
		t.Errorf("OpText(Ident) = %q, want %q", got, "IDENT")
	}
}
