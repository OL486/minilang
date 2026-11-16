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

// caretAligned 断言 ^ 画在显示行中标记字符的正下方。
// 标记字符在源码行里唯一，错误列就指向它。
// 按 rune 比下标：列号本身按字符算，byte 下标会在多字节字符前算错。
func caretAligned(t *testing.T, rendered, marker string) {
	t.Helper()
	lines := strings.Split(rendered, "\n")
	if len(lines) < 3 {
		t.Fatalf("Render 缺少源码行或 ^ 行: %q", rendered)
	}
	runeIndex := func(s, sub string) int {
		for i, r := range []rune(s) {
			if string(r) == sub {
				return i
			}
		}
		return -1
	}
	displayed, caretLine := lines[1], lines[2]
	mark := runeIndex(displayed, marker)
	caret := runeIndex(caretLine, "^")
	if mark < 0 || caret < 0 || mark != caret {
		t.Errorf("^ 未对准标记字符：显示行 %q（标记在 %d），^ 行 %q（^ 在 %d）", displayed, mark, caretLine, caret)
	}
}

func TestTruncateLine(t *testing.T) {
	short := "let x = 1;"
	if got, off := truncateLine(short, 5); got != short || off != 4 {
		t.Errorf("短行应原样返回：got %q, off %d", got, off)
	}
	// 正好 80 字符不截断。
	exact := strings.Repeat("a", 80)
	if got, _ := truncateLine(exact, 40); got != exact {
		t.Errorf("80 字符行不应截断：got %q", got)
	}
	// 81 字符、错误列在行首附近：只有尾部省略号。
	long := strings.Repeat("a", 81)
	got, off := truncateLine(long, 3)
	if strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("行首附近应只有尾部省略号：got %q", got)
	}
	if off != 2 {
		t.Errorf("行首附近 offset 应为 col-1=2，got %d", off)
	}
	// 错误列在中间（200 字符行，第 101 列）：两侧都有省略号，
	// 显示行共 80 字符 + 2 个省略号，offset 为窗口内 40 + 左省略号 1。
	long = strings.Repeat("a", 200)
	got, off = truncateLine(long, 101)
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("中间应两侧都有省略号：got %q", got)
	}
	if n := len([]rune(got)); n != 82 {
		t.Errorf("截断后显示行应为 82 rune，got %d", n)
	}
	if off != 41 {
		t.Errorf("中间 offset 应为 41（窗口内 40 + 左省略号 1），got %d", off)
	}
	// 错误列在行尾附近（第 199 列）：只有首部省略号。
	got, off = truncateLine(long, 199)
	if !strings.HasPrefix(got, "…") || strings.HasSuffix(got, "…") {
		t.Errorf("行尾附近应只有首部省略号：got %q", got)
	}
	if off != 79 {
		t.Errorf("行尾附近 offset 应为 79，got %d", off)
	}
	// 列号非法（<= 0）：按行首处理，取行首窗口。
	got, off = truncateLine(long, 0)
	if strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") || off != 0 {
		t.Errorf("col<=0 应取行首窗口：got %q, off %d", got, off)
	}
	// 列号超出行的：夹到行尾字符。
	got, off = truncateLine(long, 999)
	if !strings.HasPrefix(got, "…") || strings.HasSuffix(got, "…") || off != 80 {
		t.Errorf("超出行尾应夹到最后一个字符：got %q, off %d", got, off)
	}
}

func TestRenderTruncatesLongLine(t *testing.T) {
	// 错误列在行中：标记字符 @ 在第 101 列，两侧各 100 个字符。
	src := strings.Repeat("a", 100) + "@" + strings.Repeat("b", 100) + "\n"
	e := New(StageParse, 1, 101, "x")
	rendered := Render(src, e)
	lines := strings.Split(rendered, "\n")
	if n := len([]rune(lines[1])); n > len("   1 | ")+82 {
		t.Errorf("截断后源码行过长（%d rune）: %q", n, lines[1])
	}
	if !strings.Contains(rendered, "…") {
		t.Errorf("长行应有省略号: %q", rendered)
	}
	caretAligned(t, rendered, "@")
	// 错误列在行尾附近：窗口滑到行尾，只有首部省略号。
	src = strings.Repeat("a", 198) + "@\n"
	e = New(StageParse, 1, 199, "x")
	rendered = Render(src, e)
	if !strings.Contains(rendered, "…") || strings.HasSuffix(strings.Split(rendered, "\n")[1], "…") {
		t.Errorf("行尾附近应只有首部省略号: %q", rendered)
	}
	caretAligned(t, rendered, "@")
	// 错误列在行首附近：只有尾部省略号。
	src = "@" + strings.Repeat("a", 199) + "\n"
	e = New(StageParse, 1, 1, "x")
	rendered = Render(src, e)
	displayed := strings.Split(rendered, "\n")[1]
	if strings.HasPrefix(displayed, "…") || !strings.HasSuffix(displayed, "…") {
		t.Errorf("行首附近应只有尾部省略号: %q", rendered)
	}
	caretAligned(t, rendered, "@")
}

func TestRenderLongLineWithoutCol(t *testing.T) {
	// Col <= 0（VM 运行时错误）也截断，只是不画 ^。
	src := strings.Repeat("a", 500) + "\n"
	e := New(StageRuntime, 1, 0, "x")
	rendered := Render(src, e)
	if strings.Contains(rendered, "^") {
		t.Errorf("Col<=0 不应画 ^: %q", rendered)
	}
	lines := strings.Split(rendered, "\n")
	if n := len([]rune(lines[1])); n > len("   1 | ")+82 {
		t.Errorf("Col<=0 的长行也应截断（%d rune）: %q", n, lines[1])
	}
	if !strings.Contains(rendered, "…") {
		t.Errorf("Col<=0 的长行应有省略号: %q", rendered)
	}
}

func TestRenderShortLineUnchanged(t *testing.T) {
	// 短行行为与截断前完全一致：逐字比对整块输出。
	src := "let x = 1;\nlet y = ;\n"
	e := New(StageParse, 2, 9, "表达式不能以 ';' 开头")
	want := "语法错误 (第 2 行, 第 9 列): 表达式不能以 ';' 开头\n   2 | let y = ;\n     |         ^"
	if got := Render(src, e); got != want {
		t.Errorf("短行 Render 行为变了：\n got %q\nwant %q", got, want)
	}
	// 多字节字符前的 ^ 仍对准：列号按字符算。
	src = "let 变量 = ;\n"
	e = New(StageParse, 1, 10, "x")
	rendered := Render(src, e)
	caretAligned(t, rendered, ";")
}
