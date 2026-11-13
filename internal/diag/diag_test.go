package diag

import (
	"strings"
	"testing"
)

func TestErrorString(t *testing.T) {
	e := New(StageParse, 2, 9, "表达式不能以 %s 开头", "';'")
	if got := e.Error(); got != "语法错误 (第 2 行, 第 9 列): 表达式不能以 ';' 开头" {
		t.Errorf("Error() = %q", got)
	}
	// 只有行号、没有列时不画第 0 列。
	e = New(StageRuntime, 4, 0, "未定义的变量 '%s'", "x")
	if got := e.Error(); got != "运行时错误 (第 4 行): 未定义的变量 'x'" {
		t.Errorf("Error() = %q", got)
	}
	// 没有位置时只有阶段和消息。
	e = New(StageLex, 0, 0, "oops")
	if got := e.Error(); got != "词法错误: oops" {
		t.Errorf("Error() = %q", got)
	}
}

func TestRender(t *testing.T) {
	src := "let x = 1;\nlet y = ;\n"
	e := New(StageParse, 2, 9, "表达式不能以 ';' 开头")
	rendered := Render(src, e)
	if !strings.Contains(rendered, "2 | let y = ;") {
		t.Errorf("Render missing source line: %s", rendered)
	}
	if !strings.Contains(rendered, "^") {
		t.Errorf("Render missing caret: %s", rendered)
	}
	// 行号超出源码时不 panic，只返回错误本身。
	e = New(StageParse, 99, 1, "x")
	if got := Render(src, e); !strings.Contains(got, "第 99 行") {
		t.Errorf("Render out-of-range = %q", got)
	}
}

func TestRenderAllDedupes(t *testing.T) {
	src := "let x = 1;\n"
	errs := []*Error{
		New(StageParse, 1, 1, "same"),
		New(StageParse, 1, 1, "same"),
		New(StageParse, 1, 1, "different"),
	}
	rendered := RenderAll(src, errs)
	if strings.Count(rendered, "same") != 1 {
		t.Errorf("RenderAll should dedupe, got: %s", rendered)
	}
	if !strings.Contains(rendered, "different") {
		t.Errorf("RenderAll dropped a distinct error: %s", rendered)
	}
}
