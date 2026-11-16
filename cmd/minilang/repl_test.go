package main

import (
	"bytes"
	"strings"
	"testing"
)

func runREPLInput(t *testing.T, engine, input string) string {
	t.Helper()
	var buf bytes.Buffer
	o := defaultOptions()
	o.engine = engine
	o.out = &buf
	o.errOut = &buf
	runREPLIO(o, strings.NewReader(input), &buf)
	return buf.String()
}

func TestREPLGCTreeHint(t *testing.T) {
	out := runREPLInput(t, "tree", ":gc\n:quit\n")
	if !strings.Contains(out, "tree 引擎没有自己的对象堆") {
		t.Fatalf(":gc 在 tree 下没有提示: %q", out)
	}
}

func TestREPLGCVM(t *testing.T) {
	out := runREPLInput(t, "vm", ":gc\n:quit\n")
	if !strings.Contains(out, "GC: 对象") {
		t.Fatalf(":gc 在 vm 下没有报 GC 统计: %q", out)
	}
}

func TestREPLLineTooLong(t *testing.T) {
	// 一行 5 MiB，超过 scanner 4 MiB 上限：REPL 应提示后退出，而不是静默退出。
	out := runREPLInput(t, "vm", strings.Repeat("x", 5*1024*1024)+"\n:quit\n")
	if !strings.Contains(out, "4 MiB") {
		t.Fatalf("超长行没有提示 4 MiB 上限: %q", out[:min(200, len(out))])
	}
}
