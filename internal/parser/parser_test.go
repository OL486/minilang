package parser

import (
	"strings"
	"testing"

	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/lexer"
	"github.com/OL486/minilang/internal/semantics"
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

func TestParseNestingDepthLimit(t *testing.T) {
	// 边界换算（每层嵌套计 1，顶层表达式本身占 1，最内层内容还要再占 1）：
	// 括号/一元链/代码块/嵌套调用/else-if 链 N 层 → 最深 depth = N+1，
	// 所以 999 层通过、1000 层报错。
	parseAll := func(src string) ([]ast.Stmt, []*diag.Error) {
		t.Helper()
		tokens, lexErrs := lexer.New(src).Scan()
		if len(lexErrs) > 0 {
			t.Fatalf("lex errors: %v", lexErrs)
		}
		return New(tokens).Parse()
	}
	hasTooDeep := func(errs []*diag.Error) bool {
		for _, e := range errs {
			if strings.Contains(e.Msg, "嵌套太深") {
				return true
			}
		}
		return false
	}

	parens := func(n int) string {
		return "print " + strings.Repeat("(", n) + "1" + strings.Repeat(")", n) + ";"
	}
	if _, errs := parseAll(parens(999)); len(errs) > 0 {
		t.Errorf("999 层括号应通过，得到错误 %v", errs)
	}
	if _, errs := parseAll(parens(1000)); !hasTooDeep(errs) {
		t.Errorf("1000 层括号应报「嵌套太深」，得到 %v", errs)
	}
	if _, errs := parseAll(parens(5000)); !hasTooDeep(errs) {
		t.Errorf("5000 层括号应报「嵌套太深」，得到 %v", errs)
	}

	unary := func(n int) string {
		return "print " + strings.Repeat("!", n) + "x;"
	}
	if _, errs := parseAll(unary(999)); len(errs) > 0 {
		t.Errorf("999 个一元 ! 应通过，得到错误 %v", errs)
	}
	if _, errs := parseAll(unary(1000)); !hasTooDeep(errs) {
		t.Errorf("1000 个一元 ! 应报「嵌套太深」，得到 %v", errs)
	}

	blocks := func(n int) string {
		return strings.Repeat("{", n) + "print 1;" + strings.Repeat("}", n)
	}
	if _, errs := parseAll(blocks(999)); len(errs) > 0 {
		t.Errorf("999 层代码块应通过，得到错误 %v", errs)
	}
	if _, errs := parseAll(blocks(1000)); !hasTooDeep(errs) {
		t.Errorf("1000 层代码块应报「嵌套太深」，得到 %v", errs)
	}

	calls := func(n int) string {
		return "print " + strings.Repeat("f(", n) + "1" + strings.Repeat(")", n) + ";"
	}
	if _, errs := parseAll(calls(999)); len(errs) > 0 {
		t.Errorf("999 层嵌套调用应通过，得到错误 %v", errs)
	}
	if _, errs := parseAll(calls(1000)); !hasTooDeep(errs) {
		t.Errorf("1000 层嵌套调用应报「嵌套太深」，得到 %v", errs)
	}

	// else-if 链：ifStatement 直接递归，同样受上限约束。
	// 注意第 k 个 if 的 then 块里 print 语句的 expression() 还要再占 1 层，
	// 所以 N 个 else-if 的最深 depth = N+2，998 层通过、999 层报错。
	elseIfs := func(n int) string {
		var b strings.Builder
		b.WriteString("if (true) { print 1; }")
		for i := 1; i < n; i++ {
			b.WriteString(" else if (true) { print 1; }")
		}
		b.WriteString(" else { print 1; }")
		return b.String()
	}
	if _, errs := parseAll(elseIfs(998)); len(errs) > 0 {
		t.Errorf("998 层 else-if 链应通过，得到错误 %v", errs)
	}
	if _, errs := parseAll(elseIfs(999)); !hasTooDeep(errs) {
		t.Errorf("999 层 else-if 链应报「嵌套太深」，得到 %v", errs)
	}

	// 超限报错带位置：语法阶段、第 1 行、有列号。
	_, errs := parseAll(parens(5000))
	if len(errs) == 0 || errs[0].Stage != diag.StageParse || errs[0].Line != 1 || errs[0].Col <= 0 {
		t.Errorf("超限错误应为语法阶段第 1 行带列号，得到 %v", errs)
	}

	// 报错后继续解析：超限语句后面的语句不受影响。
	stmts, errs := parseAll(parens(5000) + " print 42;")
	if !hasTooDeep(errs) {
		t.Fatalf("应报「嵌套太深」，得到 %v", errs)
	}
	if len(stmts) != 1 {
		t.Fatalf("超限后应继续解析出 1 条语句，得到 %d 条", len(stmts))
	}
	if _, ok := stmts[0].(*ast.PrintStmt); !ok {
		t.Errorf("后续语句应为 PrintStmt，得到 %T", stmts[0])
	}

	// 正常嵌套不受影响：几层括号 + 嵌套 if/while/fn。
	normal := `fn f(n) { if (n > 0) { while (n > 0) { print (n + (1 * (2 - 1))); n = n - 1; } } return n; }`
	if _, errs := parseAll(normal); len(errs) > 0 {
		t.Errorf("正常嵌套应通过，得到错误 %v", errs)
	}

	// 上限值本身与 semantics.MaxNestingDepth 一致，避免测试与实现各写一个数。
	if semantics.MaxNestingDepth != 1000 {
		t.Errorf("MaxNestingDepth = %d, want 1000", semantics.MaxNestingDepth)
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
