// Package main 的 --trace 开关测试：
//   - VM 引擎下 --trace 接通跟踪；
//   - tree 引擎下 --trace 给出明确提示而不是静默忽略，程序仍正常执行；
//   - --trace 能被 flag 解析正常识别。
package main

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

func TestTraceFlagParsed(t *testing.T) {
	o := defaultOptions()
	fs := flag.NewFlagSet("minilang", flag.ContinueOnError)
	fs.SetOutput(&bytes.Buffer{})
	addFlags(fs, &o)
	positional := parseArgs(fs, []string{"x.ml", "--trace"})
	if !o.trace {
		t.Fatal("--trace 开关未被解析出来")
	}
	if len(positional) != 1 || positional[0] != "x.ml" {
		t.Fatalf("位置参数 = %v, want [x.ml]", positional)
	}
}

func TestTraceWithVMEngineRuns(t *testing.T) {
	var out, errOut bytes.Buffer
	o := defaultOptions()
	o.engine = "vm"
	o.trace = true
	o.out = &out
	o.errOut = &errOut
	s := newSession(o)
	if !s.runSource("print 1 + 2;", "t.ml") {
		t.Fatalf("runSource 失败，errOut = %q", errOut.String())
	}
	got := out.String()
	if !strings.Contains(got, "3\n") {
		t.Fatalf("程序输出缺失，got = %q", got)
	}
	if !strings.Contains(got, "OP_ADD") || !strings.Contains(got, "栈顶: 3") {
		t.Fatalf("VM 引擎下 --trace 应输出跟踪行，got = %q", got)
	}
	if errOut.Len() != 0 {
		t.Fatalf("VM 引擎下不应有警告，errOut = %q", errOut.String())
	}
}

func TestTraceWithTreeEngineWarns(t *testing.T) {
	var out, errOut bytes.Buffer
	o := defaultOptions()
	o.engine = "tree"
	o.trace = true
	o.out = &out
	o.errOut = &errOut
	s := newSession(o)
	// 提示是一次性的，在建 session 时就发出。
	if got := errOut.String(); !strings.Contains(got, "--trace") || !strings.Contains(got, "tree") {
		t.Fatalf("tree 引擎下应提示 --trace 不支持，errOut = %q", got)
	}
	// 程序仍正常执行，只是没有跟踪输出。
	if !s.runSource("print 1 + 2;", "t.ml") {
		t.Fatal("runSource 失败")
	}
	if got := out.String(); got != "3\n" {
		t.Fatalf("tree 引擎下输出应只有程序结果，got = %q, want %q", got, "3\n")
	}
}
