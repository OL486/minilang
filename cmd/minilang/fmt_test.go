// Package main 的 fmt 子命令测试。
//
// 覆盖：合法程序格式化输出、语法错误退出 1 且诊断与 run 一致、
// 空文件、stdin（fmt -）、不存在的文件、超限文件（10 MiB 上限）、
// 注释丢失、6 个示例的幂等性与格式化前后双引擎语义等价。
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempML 复用 main_test.go 里的同名 helper（行为相同：写临时 .ml 文件）。
// 注意：fmt_test.go 不能再定义自己的 writeTempML，否则与 main_test.go 冲突。

func fmtOpts() (options, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return options{engine: "vm", out: &out, errOut: &errOut}, &out, &errOut
}

func TestFmtValidProgram(t *testing.T) {
	path := writeTempML(t, "let    x=1+2;\nif(x>0){print x;}\n")
	o, out, errOut := fmtOpts()
	if code := fmtFile(path, o); code != 0 {
		t.Fatalf("合法程序 fmt 应退出 0，实际 %d，stderr: %s", code, errOut.String())
	}
	want := "let x = 1 + 2;\nif (x > 0) {\n  print x;\n}\n"
	if out.String() != want {
		t.Fatalf("fmt 输出不符：\n实际:\n%s\n期望:\n%s", out.String(), want)
	}
	if errOut.Len() != 0 {
		t.Fatalf("通过时 stderr 应为空，实际: %q", errOut.String())
	}
}

func TestFmtEmptyFile(t *testing.T) {
	path := writeTempML(t, "// 只有注释\n")
	o, out, _ := fmtOpts()
	if code := fmtFile(path, o); code != 0 {
		t.Fatalf("空程序 fmt 应退出 0，实际 %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("空程序 fmt 应输出空，实际: %q", out.String())
	}
}

func TestFmtMissingFile(t *testing.T) {
	o, _, errOut := fmtOpts()
	missing := filepath.Join(t.TempDir(), "no-such-file.ml")
	if code := fmtFile(missing, o); code != 1 {
		t.Fatalf("不存在的文件 fmt 应退出 1，实际 %d", code)
	}
	if !strings.Contains(errOut.String(), "无法读取文件") {
		t.Fatalf("应报无法读取文件，实际 stderr: %q", errOut.String())
	}
}

func TestFmtSyntaxError(t *testing.T) {
	// 缺少分号：与 examples/errors/02-missing-semicolon.ml 同类错误。
	path := writeTempML(t, "let x = 1\nlet y = 2;\n")
	o, _, errOut := fmtOpts()
	if code := fmtFile(path, o); code != 1 {
		t.Fatalf("语法错误 fmt 应退出 1，实际 %d", code)
	}
	if errOut.Len() == 0 {
		t.Fatal("语法错误时 stderr 应有诊断输出")
	}
	// 报错格式必须与 run 一致：同一文件跑 runFile 应输出逐字节相同的诊断。
	ro, _, rerrOut := fmtOpts()
	if code := runFile(path, ro); code != 1 {
		t.Fatalf("对照：run 同一坏文件应退出 1，实际 %d", code)
	}
	if errOut.String() != rerrOut.String() {
		t.Fatalf("fmt 与 run 的诊断输出不一致：\nfmt:\n%s\nrun:\n%s", errOut.String(), rerrOut.String())
	}
}

func TestFmtStdin(t *testing.T) {
	o, out, _ := fmtOpts()
	o.stdin = strings.NewReader("let x=1;\nprint x;\n")
	if code := fmtFile("-", o); code != 0 {
		t.Fatalf("fmt - 应退出 0，实际 %d", code)
	}
	if want := "let x = 1;\nprint x;\n"; out.String() != want {
		t.Fatalf("fmt - 输出不符：\n实际:\n%s\n期望:\n%s", out.String(), want)
	}
}

func TestFmtOversizeFile(t *testing.T) {
	// 11 MiB 注释：readSource 的 10 MiB 上限应在解析前拦住。
	path := filepath.Join(t.TempDir(), "huge.ml")
	blob := strings.Repeat("// "+strings.Repeat("x", 100)+"\n", 110*1024)
	if err := os.WriteFile(path, []byte(blob), 0o600); err != nil {
		t.Fatal(err)
	}
	o, _, errOut := fmtOpts()
	if code := fmtFile(path, o); code != 1 {
		t.Fatalf("超限文件 fmt 应退出 1，实际 %d", code)
	}
	if !strings.Contains(errOut.String(), "10 MiB") {
		t.Fatalf("应报 10 MiB 上限，实际 stderr: %q", errOut.String())
	}
}

func TestFmtDropsComments(t *testing.T) {
	// 注释在词法阶段已被丢弃，AST 里没有注释信息，fmt 无法恢复：
	// 输出里不应出现任何注释文本。
	path := writeTempML(t, "// 头注释\nlet x = 1; // 行尾注释\nprint x; // 再见\n")
	o, out, _ := fmtOpts()
	if code := fmtFile(path, o); code != 0 {
		t.Fatalf("合法程序 fmt 应退出 0，实际 %d", code)
	}
	if strings.Contains(out.String(), "注释") || strings.Contains(out.String(), "再见") {
		t.Fatalf("fmt 输出不应含注释，实际: %q", out.String())
	}
	if want := "let x = 1;\nprint x;\n"; out.String() != want {
		t.Fatalf("fmt 输出不符：\n实际:\n%s\n期望:\n%s", out.String(), want)
	}
}

// TestFmtExamplesIdempotentAndEquivalent：6 个示例的格式化必须幂等，
// 且格式化前后的程序在双引擎下输出相同（即 fmt 不改变语义）。
//
// 06-clock-timing.ml 打印计时数字，每次运行都不同，只断言退出码，
// 不比输出；其余 5 个示例逐字节比对。
func TestFmtExamplesIdempotentAndEquivalent(t *testing.T) {
	examples := []string{
		"01-basics.ml",
		"02-control-flow.ml",
		"03-closures.ml",
		"04-recursion.ml",
		"05-gc-stress.ml",
		"06-clock-timing.ml",
	}
	for _, name := range examples {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", "examples", name)

			// 第一遍格式化。
			o1, out1, _ := fmtOpts()
			if code := fmtFile(path, o1); code != 0 {
				t.Fatalf("fmt %s 应退出 0，实际 %d", name, code)
			}
			formatted := out1.String()

			// 第二遍格式化（对第一遍的输出再 fmt）：必须逐字节相同。
			o2, out2, _ := fmtOpts()
			o2.stdin = strings.NewReader(formatted)
			if code := fmtFile("-", o2); code != 0 {
				t.Fatalf("fmt 第二遍应退出 0，实际 %d", code)
			}
			if out2.String() != formatted {
				t.Fatalf("%s 不幂等：\n一遍:\n%s\n两遍:\n%s", name, formatted, out2.String())
			}

			// 格式化前后语义等价：原文件与格式化后文件在双引擎下跑，
			// 退出码相同；除 06 外输出也逐字节相同。
			formattedPath := writeTempML(t, formatted)
			for _, engine := range []string{"vm", "tree"} {
				origOpts, origOut, _ := fmtOpts()
				origOpts.engine = engine
				origCode := runFile(path, origOpts)

				fmtOpts2, fmtOut, _ := fmtOpts()
				fmtOpts2.engine = engine
				fmtCode := runFile(formattedPath, fmtOpts2)

				if origCode != fmtCode {
					t.Fatalf("%s (%s 引擎)：格式化前后退出码不同：原 %d，格式化后 %d",
						name, engine, origCode, fmtCode)
				}
				if name != "06-clock-timing.ml" && origOut.String() != fmtOut.String() {
					t.Fatalf("%s (%s 引擎)：格式化前后输出不同：\n原:\n%s\n格式化后:\n%s",
						name, engine, origOut.String(), fmtOut.String())
				}
			}
		})
	}
}
