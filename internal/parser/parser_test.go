package parser

import (
	"strings"
	"testing"

	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/lexer"
	"github.com/OL486/minilang/internal/token"
)

func parse(t *testing.T, src string) []ast.Stmt {
	t.Helper()
	tokens, lexErrs := lexer.New(src).Scan()
	if len(lexErrs) > 0 {
		t.Fatalf("Parse(%q) lex errors: %v", src, lexErrs)
	}
	stmts, parseErrs := New(tokens).Parse()
	if len(parseErrs) > 0 {
		t.Fatalf("Parse(%q) parse errors: %v", src, parseErrs)
	}
	return stmts
}

func TestParsePrecedence(t *testing.T) {
	// 1 + 2 * 3 解析成 Binary(+, 1, Binary(*, 2, 3))，而不是 (1+2)*3。
	stmts := parse(t, "print 1 + 2 * 3;")
	printStmt, ok := stmts[0].(*ast.PrintStmt)
	if !ok {
		t.Fatalf("stmt[0] = %T, want *ast.PrintStmt", stmts[0])
	}
	outer, ok := printStmt.Value.(*ast.Binary)
	if !ok || outer.Op != token.Plus {
		t.Fatalf("print value = %T, want Binary(+)", printStmt.Value)
	}
	inner, ok := outer.Right.(*ast.Binary)
	if !ok || inner.Op != token.Star {
		t.Fatalf("right operand = %T, want Binary(*)", outer.Right)
	}
	// (1 + 2) * 3：括号改变结合。
	stmts = parse(t, "print (1 + 2) * 3;")
	printStmt = stmts[0].(*ast.PrintStmt)
	outer, ok = printStmt.Value.(*ast.Binary)
	if !ok || outer.Op != token.Star {
		t.Fatalf("print value = %T, want Binary(*)", printStmt.Value)
	}
	if _, ok := outer.Left.(*ast.Grouping); !ok {
		t.Fatalf("left operand = %T, want *ast.Grouping", outer.Left)
	}
	// 减法左结合：1 - 2 - 3 解析成 (1-2)-3。
	stmts = parse(t, "print 1 - 2 - 3;")
	outer = stmts[0].(*ast.PrintStmt).Value.(*ast.Binary)
	left, ok := outer.Left.(*ast.Binary)
	if !ok || left.Op != token.Minus {
		t.Fatalf("1-2-3 left = %T, want Binary(-)", outer.Left)
	}
}

func TestParseLogicalIsSeparateNode(t *testing.T) {
	stmts := parse(t, "print true && false || true;")
	printStmt := stmts[0].(*ast.PrintStmt)
	// || 优先级低于 &&：顶层是 Logical(||)，左操作数是 Logical(&&)。

	outer, ok := printStmt.Value.(*ast.Logical)
	if !ok || outer.Op != token.OrOr {
		t.Fatalf("print value = %T, want Logical(||)", printStmt.Value)
	}
	if _, ok := outer.Left.(*ast.Logical); !ok {
		t.Fatalf("left operand = %T, want *ast.Logical", outer.Left)
	}
}

func TestParseStatements(t *testing.T) {
	stmts := parse(t, "let x = 1;")
	let, ok := stmts[0].(*ast.LetStmt)
	if !ok || let.Name != "x" {
		t.Fatalf("stmt[0] = %T, want LetStmt x", stmts[0])
	}

	stmts = parse(t, "fn add(a, b) { return a + b; }")
	fn, ok := stmts[0].(*ast.FnDecl)
	if !ok || fn.Name != "add" || len(fn.Params) != 2 {
		t.Fatalf("stmt[0] = %T, want FnDecl add(a, b)", stmts[0])
	}

	stmts = parse(t, "if (x) { print 1; } else { print 2; }")
	ifStmt, ok := stmts[0].(*ast.IfStmt)
	if !ok || len(ifStmt.Then) != 1 || len(ifStmt.Else) != 1 {
		t.Fatalf("stmt[0] = %T, want IfStmt with then/else", stmts[0])
	}

	stmts = parse(t, "while (true) { print 1; }")
	if _, ok := stmts[0].(*ast.WhileStmt); !ok {
		t.Fatalf("stmt[0] = %T, want *ast.WhileStmt", stmts[0])
	}

	stmts = parse(t, "print -x; print !y;")
	unary := stmts[0].(*ast.PrintStmt).Value.(*ast.Unary)
	if unary.Op != token.Minus {
		t.Errorf("unary op = %v, want -", unary.Op)
	}
	unary = stmts[1].(*ast.PrintStmt).Value.(*ast.Unary)
	if unary.Op != token.Bang {
		t.Errorf("unary op = %v, want !", unary.Op)
	}
}

func TestParseAssignmentIsRightAssociative(t *testing.T) {
	// x = y = 7 解析成 x = (y = 7)。
	stmts := parse(t, "x = y = 7;")
	exprStmt, ok := stmts[0].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("stmt[0] = %T, want *ast.ExprStmt", stmts[0])
	}
	assign, ok := exprStmt.X.(*ast.Assign)
	if !ok || assign.Name != "x" {
		t.Fatalf("expr = %T, want Assign(x)", exprStmt.X)
	}
	inner, ok := assign.Value.(*ast.Assign)
	if !ok || inner.Name != "y" {
		t.Fatalf("assign value = %T, want Assign(y)", assign.Value)
	}
}

func TestParseCall(t *testing.T) {
	stmts := parse(t, "print f(1, 2);")
	call, ok := stmts[0].(*ast.PrintStmt).Value.(*ast.Call)
	if !ok {
		t.Fatalf("print value = %T, want *ast.Call", stmts[0].(*ast.PrintStmt).Value)
	}
	if len(call.Args) != 2 {
		t.Errorf("call args = %d, want 2", len(call.Args))
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantSub string
	}{
		{"缺少分号", "let x = 1\n", "分号"},
		{"缺少右括号", "print (1 + 2;\n", "右括号"},
		{"if 缺括号", "if true { print 1; }\n", "括号"},
		{"表达式不能以分号开头", "let y = ;\n", "表达式不能以"},
		{"赋值左侧不是变量", "1 = 2;\n", "赋值号左侧必须是变量名"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tokens, lexErrs := lexer.New(c.src).Scan()
			if len(lexErrs) > 0 {
				t.Fatalf("unexpected lex errors: %v", lexErrs)
			}
			_, parseErrs := New(tokens).Parse()
			if len(parseErrs) == 0 {
				t.Fatalf("Parse(%q) expected an error", c.src)
			}
			found := false
			for _, e := range parseErrs {
				if strings.Contains(e.Msg, c.wantSub) {
					found = true
				}
			}
			if !found {
				t.Errorf("errors %v do not contain %q", parseErrs, c.wantSub)
			}
		})
	}
}
