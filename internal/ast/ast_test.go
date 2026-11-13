package ast_test

import (
	"strings"
	"testing"

	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/lexer"
	"github.com/OL486/minilang/internal/parser"
)

func dump(t *testing.T, src string) string {
	t.Helper()
	tokens, lexErrs := lexer.New(src).Scan()
	if len(lexErrs) > 0 {
		t.Fatalf("lex errors: %v", lexErrs)
	}
	stmts, parseErrs := parser.New(tokens).Parse()
	if len(parseErrs) > 0 {
		t.Fatalf("parse errors: %v", parseErrs)
	}
	return ast.Dump(stmts)
}

func TestDump(t *testing.T) {
	out := dump(t, "let n = 1 + 2 * 3;")
	for _, want := range []string{"Let(n)", "Binary(+)", "Binary(*)", "Literal(1)"} {
		if !strings.Contains(out, want) {
			t.Errorf("Dump missing %q:\n%s", want, out)
		}
	}
	// 逻辑与一元运算符也走同一份运算符文本表（以前 treewalk 侧缺 !、&&、||）。
	out = dump(t, "print !a && b || c;")
	for _, want := range []string{"Unary(!)", "Logical(&&)", "Logical(||)"} {
		if !strings.Contains(out, want) {
			t.Errorf("Dump missing %q:\n%s", want, out)
		}
	}
	out = dump(t, "fn add(a, b) { return a + b; }")
	for _, want := range []string{"Fn(add) params=[a, b]", "Return", "Binary(+)"} {
		if !strings.Contains(out, want) {
			t.Errorf("Dump missing %q:\n%s", want, out)
		}
	}
	out = dump(t, "if (x) { print 1; } else { print 2; }")
	for _, want := range []string{"If", "Then:", "Else:", "Print"} {
		if !strings.Contains(out, want) {
			t.Errorf("Dump missing %q:\n%s", want, out)
		}
	}
	// 空程序打印空字符串。
	if got := ast.Dump(nil); got != "" {
		t.Errorf("Dump(nil) = %q, want empty", got)
	}
}
