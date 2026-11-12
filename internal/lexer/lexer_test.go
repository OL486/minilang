package lexer

import (
	"strings"
	"testing"

	"github.com/OL486/minilang/internal/token"
)

func scanTypes(t *testing.T, src string) []token.Type {
	t.Helper()
	tokens, errs := New(src).Scan()
	if len(errs) > 0 {
		t.Fatalf("Scan(%q) unexpected errors: %v", src, errs)
	}
	types := make([]token.Type, 0, len(tokens))
	for _, tok := range tokens {
		types = append(types, tok.Type)
	}
	return types
}

func TestScanTokens(t *testing.T) {
	got := scanTypes(t, "let x = 1 + 2.5;")
	want := []token.Type{
		token.Let, token.Ident, token.Assign, token.Number,
		token.Plus, token.Number, token.Semicolon, token.EOF,
	}
	if len(got) != len(want) {
		t.Fatalf("token count = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestScanKeywordsAndOperators(t *testing.T) {
	got := scanTypes(t, "fn if else while return print true false nil && || == != < <= > >= !")
	want := []token.Type{
		token.Fn, token.If, token.Else, token.While, token.Return,
		token.Print, token.True, token.False, token.Nil,
		token.AndAnd, token.OrOr, token.Eq, token.Ne,
		token.Lt, token.Le, token.Gt, token.Ge, token.Bang, token.EOF,
	}
	if len(got) != len(want) {
		t.Fatalf("token count = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestScanLiterals(t *testing.T) {
	tokens, errs := New("print 42; print 0.5; print \"a\\tb\";").Scan()
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if got := tokens[1].Literal; got != float64(42) {
		t.Errorf("number literal = %v (%T), want 42", got, got)
	}
	if got := tokens[4].Literal; got != 0.5 {
		t.Errorf("number literal = %v, want 0.5", got)
	}
	if got := tokens[7].Literal; got != "a\tb" {
		t.Errorf("string literal = %q, want %q", got, "a\tb")
	}
}

func TestScanPositions(t *testing.T) {
	tokens, errs := New("let x = 1;\nlet y = 2;").Scan()
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// 第二行的 let 在第 2 行第 1 列（token 顺序：let x = 1 ; let …）。
	if tokens[5].Line != 2 || tokens[5].Col != 1 {
		t.Errorf("second let at %d:%d, want 2:1", tokens[5].Line, tokens[5].Col)
	}
	// EOF 指向源码末尾。
	last := tokens[len(tokens)-1]
	if last.Type != token.EOF {
		t.Fatalf("last token = %v, want EOF", last.Type)
	}
}

func TestScanCommentsIgnored(t *testing.T) {
	got := scanTypes(t, "// 注释\nlet x = 1; // 行尾注释\n")
	want := []token.Type{token.Let, token.Ident, token.Assign, token.Number, token.Semicolon, token.EOF}
	if len(got) != len(want) {
		t.Fatalf("token count = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestScanErrors(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantSub string
	}{
		{"未闭合字符串", "let x = \"未闭合\n", "字符串未闭合"},
		{"数字双小数点", "print 1.2.3;\n", "第二个小数点"},
		{"非法字符", "let x = 1 $ 2;\n", "无法识别的字符"},
		{"单个 &", "print true & true;\n", "不支持的字符 '&'"},
		{"单个 |", "print true | true;\n", "不支持的字符 '|'"},
		{"未知转义", "print \"\\q\";\n", "未知的转义序列"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, errs := New(c.src).Scan()
			if len(errs) == 0 {
				t.Fatalf("Scan(%q) expected an error", c.src)
			}
			found := false
			for _, e := range errs {
				if strings.Contains(e.Msg, c.wantSub) {
					found = true
				}
			}
			if !found {
				t.Errorf("errors %v do not contain %q", errs, c.wantSub)
			}
		})
	}
}
