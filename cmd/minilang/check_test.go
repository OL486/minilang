// Package main 的 check 子命令测试。
//
// 覆盖：合法程序通过、语法/编译错误退出 1 且报错格式与 run 一致、
// check 不执行程序（死循环立即返回、print 无输出）、
// --engine 对 check 不适用（恒走 VM 编译）、文件不存在、空文件。
package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTempML 复用 main_test.go 里的同名 helper（行为相同：写临时 .ml 文件）。
// 注意：check_test.go 不能再定义自己的 writeTempML，否则与 main_test.go 冲突。

func checkOpts() (options, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return options{engine: "vm", out: &out, errOut: &errOut}, &out, &errOut
}

func TestCheckValidProgram(t *testing.T) {
	path := writeTempML(t, "let x = 1;\nprint x + 1;\n")
	o, out, errOut := checkOpts()
	if code := checkFile(path, o); code != 0 {
		t.Fatalf("合法程序 check 应退出 0，实际 %d，stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), ": OK") {
		t.Fatalf("通过时应打印一行通过信息，实际 stdout: %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("通过时 stderr 应为空，实际: %q", errOut.String())
	}
}

func TestCheckEmptyFile(t *testing.T) {
	path := writeTempML(t, "// 只有注释\n")
	o, out, _ := checkOpts()
	if code := checkFile(path, o); code != 0 {
		t.Fatalf("空程序 check 应退出 0，实际 %d", code)
	}
	if !strings.Contains(out.String(), ": OK") {
		t.Fatalf("空程序通过时也应打印通过信息，实际 stdout: %q", out.String())
	}
}

func TestCheckMissingFile(t *testing.T) {
	o, _, errOut := checkOpts()
	missing := filepath.Join(t.TempDir(), "no-such-file.ml")
	if code := checkFile(missing, o); code != 1 {
		t.Fatalf("不存在的文件 check 应退出 1，实际 %d", code)
	}
	if !strings.Contains(errOut.String(), "无法读取文件") {
		t.Fatalf("应报无法读取文件，实际 stderr: %q", errOut.String())
	}
}

func TestCheckSyntaxError(t *testing.T) {
	// 缺少分号：与 examples/errors/02-missing-semicolon.ml 同类错误。
	path := writeTempML(t, "let x = 1\nlet y = 2;\n")
	o, _, errOut := checkOpts()
	if code := checkFile(path, o); code != 1 {
		t.Fatalf("语法错误 check 应退出 1，实际 %d", code)
	}
	if errOut.Len() == 0 {
		t.Fatal("语法错误时 stderr 应有诊断输出")
	}
	// 报错格式必须与 run 一致：同一文件跑 runFile 应输出逐字节相同的诊断。
	ro, _, rerrOut := checkOpts()
	if code := runFile(path, ro); code != 1 {
		t.Fatalf("对照：run 同一坏文件应退出 1，实际 %d", code)
	}
	if errOut.String() != rerrOut.String() {
		t.Fatalf("check 与 run 的诊断输出不一致：\ncheck:\n%s\nrun:\n%s", errOut.String(), rerrOut.String())
	}
}

func TestCheckCompileError(t *testing.T) {
	// 同一作用域重复声明：能解析、VM 编译失败（examples/errors/05-duplicate-local.ml）。
	src := "fn f() {\n  let a = 1;\n  let a = 2;\n}\n"
	path := writeTempML(t, src)
	o, _, errOut := checkOpts()
	if code := checkFile(path, o); code != 1 {
		t.Fatalf("编译错误 check 应退出 1，实际 %d", code)
	}
	if !strings.Contains(errOut.String(), "重复声明") {
		t.Fatalf("应报重复声明，实际 stderr: %q", errOut.String())
	}
	ro, _, rerrOut := checkOpts()
	if code := runFile(path, ro); code != 1 {
		t.Fatalf("对照：run 同一坏文件应退出 1，实际 %d", code)
	}
	if errOut.String() != rerrOut.String() {
		t.Fatalf("check 与 run 的诊断输出不一致：\ncheck:\n%s\nrun:\n%s", errOut.String(), rerrOut.String())
	}
}

func TestCheckDoesNotExecute(t *testing.T) {
	t.Run("死循环立即返回", func(t *testing.T) {
		path := writeTempML(t, "while (true) { print 1; }\n")
		o, _, _ := checkOpts()
		// 若 check 真去执行，测试会卡住：用带超时的 goroutine 守住，
		// 超时则直接失败而不是拖垮整个测试套件。
		done := make(chan int, 1)
		go func() { done <- checkFile(path, o) }()
		select {
		case code := <-done:
			if code != 0 {
				t.Fatalf("死循环程序语法合法，check 应退出 0，实际 %d", code)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("check 在 5 秒内未返回：它执行了死循环程序")
		}
	})

	t.Run("print 无输出", func(t *testing.T) {
		path := writeTempML(t, "print \"EXECUTED\";\n")
		o, out, _ := checkOpts()
		if code := checkFile(path, o); code != 0 {
			t.Fatalf("合法程序 check 应退出 0，实际 %d", code)
		}
		if strings.Contains(out.String(), "EXECUTED") {
			t.Fatalf("check 不应执行程序，stdout 里不应出现程序输出，实际: %q", out.String())
		}
	})
}

func TestCheckIgnoresEngine(t *testing.T) {
	// --engine 对 check 不适用：恒走 VM 编译。
	// 同一作用域重复声明是 VM 编译期错误，treewalk 运行时才暴露——
	// 即使传 --engine=tree，check 也必须报出来。
	dup := "fn f() {\n  let a = 1;\n  let a = 2;\n}\n"
	path := writeTempML(t, dup)
	o, _, errOut := checkOpts()
	o.engine = "tree"
	if code := checkFile(path, o); code != 1 {
		t.Fatalf("--engine=tree 时重复声明仍应被 check 抓住，实际退出 %d", code)
	}
	if !strings.Contains(errOut.String(), "重复声明") {
		t.Fatalf("应报重复声明，实际 stderr: %q", errOut.String())
	}

	// 合法程序在 --engine=tree 下同样通过。
	valid := writeTempML(t, "let x = 1;\nprint x;\n")
	vo, _, _ := checkOpts()
	vo.engine = "tree"
	if code := checkFile(valid, vo); code != 0 {
		t.Fatalf("--engine=tree 时合法程序 check 应退出 0，实际 %d", code)
	}
}
