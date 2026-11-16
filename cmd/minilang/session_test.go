package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testOptions(out, errOut *bytes.Buffer) options {
	o := defaultOptions()
	o.out = out
	o.errOut = errOut
	return o
}

func TestRunFileExecutesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.ml")
	if err := os.WriteFile(path, []byte("print (1 + 2) * 3;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := runFile(path, testOptions(&out, &errOut)); code != 0 {
		t.Fatalf("runFile = %d, errOut = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "9") {
		t.Fatalf("输出里没有 9: %q", out.String())
	}
}

func TestRunFileMissing(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runFile(filepath.Join(t.TempDir(), "no-such-file.ml"), testOptions(&out, &errOut)); code != 1 {
		t.Fatalf("runFile = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "无法读取文件") {
		t.Fatalf("errOut 没有报无法读取文件: %q", errOut.String())
	}
}

func TestRunFileTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.ml")
	// 10 MiB 上限 + 1 字节；内容不重要，大小检查在解析之前。
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", maxSourceBytes+1)), 0644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := runFile(path, testOptions(&out, &errOut)); code != 1 {
		t.Fatalf("runFile = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "10 MiB") {
		t.Fatalf("errOut 没有报 10 MiB 上限: %q", errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("超限文件不应有任何输出，得到 %q", out.String())
	}
}

func TestRunFileStdin(t *testing.T) {
	var out, errOut bytes.Buffer
	o := testOptions(&out, &errOut)
	o.stdin = strings.NewReader("print 40 + 2;\n")
	if code := runFile("-", o); code != 0 {
		t.Fatalf("runFile(-) = %d, errOut = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "42") {
		t.Fatalf("输出里没有 42: %q", out.String())
	}
}

func TestRunFileStdinTooLarge(t *testing.T) {
	var out, errOut bytes.Buffer
	o := testOptions(&out, &errOut)
	o.stdin = strings.NewReader(strings.Repeat(" ", maxSourceBytes+1))
	if code := runFile("-", o); code != 1 {
		t.Fatalf("runFile(-) = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "10 MiB") {
		t.Fatalf("errOut 没有报 10 MiB 上限: %q", errOut.String())
	}
}
