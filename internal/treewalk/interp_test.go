// Package treewalk 的测试：目前只有执行步数预算相关的测试；
// 求值、遮蔽、闭包捕获的直接测试在 P1-9 的待办里，不在这里展开。
package treewalk

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/lexer"
	"github.com/OL486/minilang/internal/parser"
	"github.com/OL486/minilang/internal/semantics"
)

// fib30Source 与 cmd/minilang/bench.go 的 benchSource 是同一份程序，
// 是仓库里最重的正常程序：它的步数决定了预算下限。
const fib30Source = `fn fib(n) {
  if (n < 2) { return n; }
  return fib(n-1) + fib(n-2);
}
let benchResult = fib(30);
`

func parseOnly(t *testing.T, src string) []ast.Stmt {
	t.Helper()
	tokens, lexErrs := lexer.New(src).Scan()
	if len(lexErrs) > 0 {
		t.Fatalf("lex: %v", lexErrs[0])
	}
	stmts, parseErrs := parser.New(tokens).Parse()
	if len(parseErrs) > 0 {
		t.Fatalf("parse: %v", parseErrs[0])
	}
	return stmts
}

// runTree 在全新解释器上解析并执行一段源码。
func runTree(t *testing.T, src string) (*Interp, error) {
	t.Helper()
	var out bytes.Buffer
	in := New(&out)
	return in, in.Run(parseOnly(t, src))
}

func TestExecBudgetInfiniteLoop(t *testing.T) {
	start := time.Now()
	_, err := runTree(t, "while (true) { }")
	if err == nil {
		t.Fatal("while (true) {} 没有报错，预算没有生效")
	}
	if !strings.Contains(err.Error(), "执行步数超过") {
		t.Fatalf("死循环报错不是预算错误：%v", err)
	}
	// treewalk 每步都是一次表达式求值，比 VM 指令慢一个量级：
	// 本机实测约 10–15 秒，-race 下约 50 秒。300 秒的上限只证明
	// 「会停下而不是 hang」，再加共享虚拟机波动，上限要留足余量。
	if d := time.Since(start); d > 300*time.Second {
		t.Fatalf("死循环 %v 才报错，太慢（本机实测约 10–15 秒，-race 下约 50 秒）", d)
	}
}

func TestExecBudgetNormalProgramUnaffected(t *testing.T) {
	in, err := runTree(t, fib30Source)
	if err != nil {
		t.Fatalf("fib(30) 不应撞上限：%v", err)
	}
	if in.Steps() >= semantics.MaxExecSteps {
		t.Fatalf("fib(30) 步数 %d 已达上限 %d", in.Steps(), semantics.MaxExecSteps)
	}
	t.Logf("fib(30) treewalk 步数 %d，上限 %d", in.Steps(), semantics.MaxExecSteps)
}

func TestExecBudgetResetsBetweenRuns(t *testing.T) {
	// 预算按次清零：同一个解释器上一次死循环撞限后，
	// 下一个正常程序仍能跑；REPL 复用同一个解释器，全靠这一点。
	var out bytes.Buffer
	in := New(&out)
	if err := in.Run(parseOnly(t, "while (true) { }")); err == nil {
		t.Fatal("死循环没有报错")
	}
	if err := in.Run(parseOnly(t, "print 1 + 2;")); err != nil {
		t.Fatalf("撞限后正常程序跑不起来：%v", err)
	}
	if in.Steps() >= semantics.MaxExecSteps {
		t.Fatalf("小段程序步数 %d 异常", in.Steps())
	}
}
