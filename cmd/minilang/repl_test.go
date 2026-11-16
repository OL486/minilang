package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runREPLInput 把一段输入喂给 REPL，返回 stdout 与 stderr。
// histPath 为 "" 时禁用历史；传 t.TempDir() 下的路径可隔离真实主目录。
func runREPLInput(t *testing.T, engine, input, histPath string) (string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	o := defaultOptions()
	o.engine = engine
	o.out = &out
	o.errOut = &errOut
	runREPLIO(o, strings.NewReader(input), &out, histPath)
	return out.String(), errOut.String()
}

func TestREPLGCTreeHint(t *testing.T) {
	out, _ := runREPLInput(t, "tree", ":gc\n:quit\n", "")
	if !strings.Contains(out, "tree 引擎没有自己的对象堆") {
		t.Fatalf(":gc 在 tree 下没有提示: %q", out)
	}
}

func TestREPLGCVM(t *testing.T) {
	out, _ := runREPLInput(t, "vm", ":gc\n:quit\n", "")
	if !strings.Contains(out, "GC: 对象") {
		t.Fatalf(":gc 在 vm 下没有报 GC 统计: %q", out)
	}
}

func TestREPLLineTooLong(t *testing.T) {
	// 一行 5 MiB，超过 scanner 4 MiB 上限：REPL 应提示后退出，而不是静默退出。
	out, _ := runREPLInput(t, "vm", strings.Repeat("x", 5*1024*1024)+"\n:quit\n", "")
	if !strings.Contains(out, "4 MiB") {
		t.Fatalf("超长行没有提示 4 MiB 上限: %q", out[:min(200, len(out))])
	}
}

func TestHistoryAddDedupAndTrim(t *testing.T) {
	h := &history{}
	h.add("")
	h.add("   ")
	h.add("print 1;")
	h.add("print 1;") // 连续重复不记
	h.add("print 2;")
	if len(h.lines) != 2 || h.lines[0] != "print 1;" || h.lines[1] != "print 2;" {
		t.Fatalf("add 去重/空行错误: %v", h.lines)
	}
	for i := 0; i < maxHistoryLines+10; i++ {
		h.add(strings.Repeat("x", 3) + string(rune('a'+i%26)) + strings.Repeat("y", 3))
	}
	if len(h.lines) != maxHistoryLines {
		t.Fatalf("上限应为 %d，实际 %d", maxHistoryLines, len(h.lines))
	}
}

func TestLoadHistoryMissingAndEmptyPath(t *testing.T) {
	if got := loadHistory(""); len(got.lines) != 0 {
		t.Fatalf("空路径应返回空历史: %v", got.lines)
	}
	if got := loadHistory(filepath.Join(t.TempDir(), "no-such-file")); len(got.lines) != 0 {
		t.Fatalf("不存在的文件应返回空历史: %v", got.lines)
	}
	h := &history{}
	h.save("")                                             // 不应 panic
	h.save(filepath.Join(t.TempDir(), "no-such-dir", "h")) // 写失败静默跳过
}

func TestREPLHistorySavedAndReloaded(t *testing.T) {
	dir := t.TempDir()
	histPath := filepath.Join(dir, ".minilang_history")

	out, _ := runREPLInput(t, "vm", "print 1;\n:quit\n", histPath)
	if !strings.Contains(out, "1") {
		t.Fatalf("print 1; 应输出 1，实际 stdout: %q", out)
	}

	data, err := os.ReadFile(histPath)
	if err != nil {
		t.Fatalf("历史文件应已写出: %v", err)
	}
	if info, _ := os.Stat(histPath); info.Mode().Perm() != 0600 {
		t.Fatalf("历史文件权限应为 0600，实际 %o", info.Mode().Perm())
	}
	if !strings.Contains(string(data), "print 1;") {
		t.Fatalf("历史文件应含 print 1;，实际: %q", data)
	}

	// 第二次进 REPL：旧历史应被读回，:history 能看到它。
	out2, _ := runREPLInput(t, "vm", ":history\n:quit\n", histPath)
	if !strings.Contains(out2, "print 1;") {
		t.Fatalf(":history 应列出上次的输入，实际 stdout: %q", out2)
	}
}

func TestREPLHistoryCapEnforced(t *testing.T) {
	dir := t.TempDir()
	histPath := filepath.Join(dir, ".minilang_history")

	var sb strings.Builder
	for i := 0; i < maxHistoryLines+20; i++ {
		sb.WriteString("let v")
		sb.WriteString(strings.Repeat("x", i+1))
		sb.WriteString(" = ")
		sb.WriteString(strings.Repeat("1", i%9+1))
		sb.WriteString(";\n")
	}
	sb.WriteString(":quit\n")
	runREPLInput(t, "vm", sb.String(), histPath)

	data, err := os.ReadFile(histPath)
	if err != nil {
		t.Fatalf("历史文件应已写出: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != maxHistoryLines {
		t.Fatalf("输入 %d 行后历史文件应正好 %d 行，实际 %d", maxHistoryLines+20, maxHistoryLines, len(lines))
	}
}

func TestREPLLoadFile(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hello.ml")
	if err := os.WriteFile(script, []byte("print 42;\nlet x = 3;\nprint x + 1;\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out, errOut := runREPLInput(t, "vm", ":load "+script+"\n:quit\n", "")
	if !strings.Contains(out, "42") || !strings.Contains(out, "4") {
		t.Fatalf(":load 应执行文件内容，stdout: %q stderr: %q", out, errOut)
	}

	// :load 之后 REPL 里的变量应存活（同一 session）。
	out, _ = runREPLInput(t, "vm", ":load "+script+"\nprint x;\n:quit\n", "")
	if !strings.Contains(out, "3") {
		t.Fatalf(":load 定义的变量应在 REPL 里存活，stdout: %q", out)
	}
}

func TestREPLLoadErrorsDoNotExit(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "no-such.ml")

	out, errOut := runREPLInput(t, "vm", ":load "+missing+"\nprint 7;\n:quit\n", "")
	if !strings.Contains(errOut, "无法读取文件") {
		t.Fatalf("缺失文件应报无法读取，stderr: %q", errOut)
	}
	if !strings.Contains(out, "7") {
		t.Fatalf(":load 失败后 REPL 应继续，stdout: %q", out)
	}

	// 无参数与语法错误同样不退出。
	out, _ = runREPLInput(t, "vm", ":load\nprint 8;\n:quit\n", "")
	if !strings.Contains(out, "用法: :load") || !strings.Contains(out, "8") {
		t.Fatalf(":load 无参数应提示用法且不退出，stdout: %q", out)
	}

	bad := filepath.Join(dir, "bad.ml")
	if err := os.WriteFile(bad, []byte("let = ;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out, errOut = runREPLInput(t, "vm", ":load "+bad+"\nprint 9;\n:quit\n", "")
	if errOut == "" {
		t.Fatalf("语法错误的文件应输出诊断，stderr 为空")
	}
	if !strings.Contains(out, "9") {
		t.Fatalf("语法错误后 REPL 应继续，stdout: %q", out)
	}
}

func TestREPLEngineSwitch(t *testing.T) {
	out, _ := runREPLInput(t, "vm", ":engine\n:engine tree\nprint 1;\n:engine bogus\n:engine vm\nprint 2;\n:quit\n", "")
	for _, want := range []string{
		"当前引擎: vm",
		"已切换到 tree 引擎",
		"未知引擎",
		"已切换到 vm 引擎",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("应包含 %q，实际 stdout: %q", want, out)
		}
	}
	// 两段 print 都应正常输出（提示符 "> " 后紧跟输出行）。
	if !strings.Contains(out, "> 1\n") || !strings.Contains(out, "> 2\n") {
		t.Fatalf("切换前后 print 都应输出，实际 stdout: %q", out)
	}

	// :engine 无参数应打印当前引擎，不报错。
	out, _ = runREPLInput(t, "vm", ":engine\n:quit\n", "")
	if !strings.Contains(out, "当前引擎: vm") {
		t.Fatalf(":engine 无参数应打印当前引擎，stdout: %q", out)
	}
}

func TestREPLEngineSwitchResetsSession(t *testing.T) {
	// vm 里定义的变量，切换到 tree 后应不存在（新会话）。
	_, errOut := runREPLInput(t, "vm", "let y = 9;\n:engine tree\nprint y;\n:quit\n", "")
	if errOut == "" {
		t.Fatalf("切换引擎后旧变量应不可见，stderr 为空")
	}
	if strings.Contains(errOut, "9") {
		t.Fatalf("切换后 print y 不应输出 9，stderr: %q", errOut)
	}
}

func TestREPLHelpListsNewCommands(t *testing.T) {
	out, _ := runREPLInput(t, "vm", ":help\n:quit\n", "")
	for _, want := range []string{":load", ":engine", ":history"} {
		if !strings.Contains(out, want) {
			t.Fatalf(":help 应列出 %s，实际 stdout: %q", want, out)
		}
	}
}
