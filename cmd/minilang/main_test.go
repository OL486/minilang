// Package main 的直接测试。
//
// 以前 cmd/minilang 没有任何测试文件，覆盖率 0% ——
// main() 的子命令分派、flag 解析、runFile 的退出码、REPL 都是空白。
// main() 本身调用 os.Exit，没法在进程内直接测：
// 这里用子进程重跑测试二进制（TestMain 里用环境变量拦截），
// 与 Go 标准库里测 os.Exit 的惯用写法一致。
package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OL486/minilang/internal/token"
	"github.com/OL486/minilang/internal/vm"
)

func TestMain(m *testing.M) {
	// 子进程模式：直接跑 main()，不跑测试。
	// 环境变量由 TestMainDispatch 设置；main() 自己会 os.Exit。
	if os.Getenv("MINILANG_TEST_MAIN") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

// runMainSubprocess 把当前测试二进制当 minilang 跑一次，
// 返回退出码与合并后的 stdout+stderr。
func runMainSubprocess(t *testing.T, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "MINILANG_TEST_MAIN=1")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("子进程执行失败: %v", err)
		}
	}
	return code, buf.String()
}

func writeTempML(t *testing.T, src string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "prog.ml")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatalf("写临时文件失败: %v", err)
	}
	return p
}

// ---------- main() 子命令分派（子进程） ----------

func TestMainDispatch(t *testing.T) {
	good := writeTempML(t, "print 40 + 2;\n")

	cases := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"version", []string{"version"}, 0, "minilang 0.1.0"},
		{"--version", []string{"--version"}, 0, "minilang 0.1.0"},
		{"-v", []string{"-v"}, 0, "minilang 0.1.0"},
		{"help", []string{"help"}, 0, "用法:"},
		{"--help", []string{"--help"}, 0, "用法:"},
		{"no-args", nil, 2, "用法:"},
		{"run-good", []string{"run", good}, 0, "42"},
		{"bare-file", []string{good}, 0, "42"},
		{"run-tree", []string{"run", good, "--engine=tree"}, 0, "42"},
		{"run-flag-first", []string{"--engine=tree", good}, 0, "42"},
		{"run-dump-ast", []string{"run", good, "--dump-ast"}, 0, "42"},
		{"run-missing", []string{"run", filepath.Join(t.TempDir(), "nope.ml")}, 1, "无法读取文件"},
		{"bare-missing", []string{"no-such-file.ml"}, 1, "无法读取文件"},
		{"unknown-engine", []string{"run", good, "--engine=bogus"}, 2, "未知引擎"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, out := runMainSubprocess(t, tc.args...)
			if code != tc.code {
				t.Fatalf("args=%v 退出码 = %d, want %d（输出 %q）", tc.args, code, tc.code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("args=%v 输出应含 %q，实际 %q", tc.args, tc.want, out)
			}
		})
	}
}

func TestMainDispatchREPL(t *testing.T) {
	// stdin 是空设备时 REPL 立刻退出，退出码 0。
	code, out := runMainSubprocess(t, "repl")
	if code != 0 {
		t.Fatalf("repl 空 stdin 退出码 = %d, want 0（输出 %q）", code, out)
	}
	if !strings.Contains(out, "minilang 0.1.0") {
		t.Fatalf("repl 应打印 banner，实际 %q", out)
	}
}

func TestMainDispatchSyntaxError(t *testing.T) {
	bad := writeTempML(t, "print (;\n")
	code, out := runMainSubprocess(t, "run", bad)
	if code != 1 {
		t.Fatalf("语法错误退出码 = %d, want 1（输出 %q）", code, out)
	}
}

func TestMainDispatchRuntimeError(t *testing.T) {
	bad := writeTempML(t, "print undefined_var;\n")
	code, out := runMainSubprocess(t, "run", bad)
	if code != 1 {
		t.Fatalf("运行时错误退出码 = %d, want 1（输出 %q）", code, out)
	}
	if !strings.Contains(out, "未定义的变量") {
		t.Fatalf("运行时错误输出应含「未定义的变量」，实际 %q", out)
	}
}

// ---------- parseArgs ----------

func newTestFlagSet(o *options) *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addFlags(fs, o)
	return fs
}

func TestParseArgs(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		want  []string
		check func(*options) bool
	}{
		{
			"flags-after-positional", []string{"a.ml", "--dump-ast"}, []string{"a.ml"},
			func(o *options) bool { return o.dumpAST },
		},
		{
			"engine-space-form", []string{"-engine", "tree", "a.ml"}, []string{"a.ml"},
			func(o *options) bool { return o.engine == "tree" },
		},
		{
			"engine-eq-form", []string{"--engine=vm", "a.ml"}, []string{"a.ml"},
			func(o *options) bool { return o.engine == "vm" },
		},
		{
			"dash-is-positional", []string{"-", "a.ml"}, []string{"-", "a.ml"},
			func(o *options) bool { return true },
		},
		{
			"multiple-flags", []string{"--stats", "--gc-off", "--dump-tokens", "a.ml"}, []string{"a.ml"},
			func(o *options) bool { return o.stats && o.gcOff && o.dumpTokens },
		},
		{
			"no-positional", []string{"--dump-bytecode"}, nil,
			func(o *options) bool { return o.dumpBytecode },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := defaultOptions()
			pos := parseArgs(newTestFlagSet(&o), tc.args)
			if len(pos) != len(tc.want) {
				t.Fatalf("parseArgs(%v) 位置参数 = %v, want %v", tc.args, pos, tc.want)
			}
			for i := range pos {
				if pos[i] != tc.want[i] {
					t.Fatalf("parseArgs(%v) 位置参数 = %v, want %v", tc.args, pos, tc.want)
				}
			}
			if !tc.check(&o) {
				t.Fatalf("parseArgs(%v) 开关解析不对，options = %+v", tc.args, o)
			}
		})
	}
}

// ---------- checkEngine / usage ----------

func TestCheckEngineValid(t *testing.T) {
	// 合法引擎不退出；非法引擎会 os.Exit(2)，只在子进程里测（见 TestMainDispatch）。
	for _, e := range []string{"vm", "tree"} {
		o := options{engine: e}
		checkEngine(&o)
	}
}

func TestUsage(t *testing.T) {
	out := captureStdout(t, usage)
	for _, want := range []string{"minilang 0.1.0", "用法:", "minilang run", "minilang repl", "minilang bench", "--dump-tokens", "--engine=vm|tree", ":help", ":quit"} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage 输出应含 %q，实际:\n%s", want, out)
		}
	}
}

// ---------- runFile / session ----------

func testOptions() (options, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	o := defaultOptions()
	o.out = &out
	o.errOut = &errOut
	return o, &out, &errOut
}

func TestRunFile(t *testing.T) {
	good := writeTempML(t, "print 40 + 2;\n")

	o, out, _ := testOptions()
	if code := runFile(good, o); code != 0 {
		t.Fatalf("runFile 好文件退出码 = %d, want 0", code)
	}
	if out.String() != "42\n" {
		t.Fatalf("runFile 输出 = %q, want %q", out.String(), "42\n")
	}

	o, out, _ = testOptions()
	o.engine = "tree"
	if code := runFile(good, o); code != 0 {
		t.Fatalf("runFile tree 引擎退出码 = %d, want 0", code)
	}
	if out.String() != "42\n" {
		t.Fatalf("runFile tree 输出 = %q, want %q", out.String(), "42\n")
	}

	o, _, errOut := testOptions()
	if code := runFile(filepath.Join(t.TempDir(), "nope.ml"), o); code != 1 {
		t.Fatalf("runFile 缺失文件退出码 = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "无法读取文件") {
		t.Fatalf("runFile 缺失文件应报「无法读取文件」，实际 %q", errOut.String())
	}

	o, _, errOut = testOptions()
	if code := runFile(writeTempML(t, "print (;\n"), o); code != 1 {
		t.Fatalf("runFile 语法错误退出码 = %d, want 1", code)
	}
	if errOut.Len() == 0 {
		t.Fatalf("runFile 语法错误应向 errOut 写诊断")
	}

	o, _, errOut = testOptions()
	if code := runFile(writeTempML(t, "print undefined_var;\n"), o); code != 1 {
		t.Fatalf("runFile 运行时错误退出码 = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "未定义的变量") {
		t.Fatalf("runFile 运行时错误应报「未定义的变量」，实际 %q", errOut.String())
	}
}

func TestSessionRunSource(t *testing.T) {
	for _, engine := range []string{"vm", "tree"} {
		o, out, _ := testOptions()
		o.engine = engine
		s := newSession(o)
		if !s.runSource("print 1 + 2;\n", "test") {
			t.Fatalf("runSource(%s) 应成功", engine)
		}
		if out.String() != "3\n" {
			t.Fatalf("runSource(%s) 输出 = %q, want %q", engine, out.String(), "3\n")
		}
	}

	// 空源码直接成功，不执行。
	o, out, _ := testOptions()
	if !newSession(o).runSource("", "test") {
		t.Fatalf("空源码 runSource 应成功")
	}
	if out.Len() != 0 {
		t.Fatalf("空源码不应有输出，实际 %q", out.String())
	}

	// 词法错误返回 false。
	o, _, errOut := testOptions()
	if newSession(o).runSource("print \"abc;\n", "test") {
		t.Fatalf("词法错误 runSource 应返回 false")
	}
	if errOut.Len() == 0 {
		t.Fatalf("词法错误应向 errOut 写诊断")
	}

	// 运行时错误返回 false。
	o, _, errOut = testOptions()
	if newSession(o).runSource("print undefined_var;\n", "test") {
		t.Fatalf("运行时错误 runSource 应返回 false")
	}
	if !strings.Contains(errOut.String(), "未定义的变量") {
		t.Fatalf("运行时错误应报「未定义的变量」，实际 %q", errOut.String())
	}
}

func TestSessionDumpFlags(t *testing.T) {
	o, out, _ := testOptions()
	o.dumpTokens = true
	o.dumpAST = true
	o.dumpBytecode = true
	if !newSession(o).runSource("let x = 1;\nprint x;\n", "test") {
		t.Fatalf("dump 开关全开时 runSource 应成功")
	}
	got := out.String()
	// token 表头 + AST dump + 反汇编三段都应出现。
	for _, want := range []string{"TYPE", "LEXEME", "Let(x)", "Var(x)", "OP_", "1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("dump 输出应含 %q，实际:\n%s", want, got)
		}
	}
}

func TestSessionStatsFlag(t *testing.T) {
	o, out, _ := testOptions()
	o.stats = true
	if !newSession(o).runSource("print 1;\n", "test") {
		t.Fatalf("stats 开关 runSource 应成功")
	}
	if !strings.Contains(out.String(), "-- stats:") {
		t.Fatalf("stats 输出应含「-- stats:」，实际 %q", out.String())
	}
	// tree 引擎没有 --stats 输出（统计只属于 VM）。
	o, out, _ = testOptions()
	o.engine = "tree"
	o.stats = true
	if !newSession(o).runSource("print 1;\n", "test") {
		t.Fatalf("tree stats runSource 应成功")
	}
	if strings.Contains(out.String(), "-- stats:") {
		t.Fatalf("tree 引擎不应打印 -- stats，实际 %q", out.String())
	}
}

func TestNewSession(t *testing.T) {
	o, _, _ := testOptions()
	o.engine = "tree"
	s := newSession(o)
	if s.interp == nil {
		t.Fatalf("tree 引擎应创建解释器")
	}
	if s.vm != nil {
		t.Fatalf("tree 引擎不应创建 VM")
	}

	o, _, _ = testOptions()
	o.gcOff = true
	s = newSession(o)
	if s.vm == nil {
		t.Fatalf("vm 引擎应创建 VM")
	}
	if s.interp != nil {
		t.Fatalf("vm 引擎不应创建解释器")
	}
}

// ---------- printTokens / printStats ----------

func TestPrintTokens(t *testing.T) {
	var buf bytes.Buffer
	printTokens(&buf, []token.Token{
		{Type: token.Let, Lexeme: "let", Line: 1, Col: 1},
		{Type: token.Number, Lexeme: "42", Literal: float64(42), Line: 1, Col: 5},
		{Type: token.String, Lexeme: `"s"`, Literal: "s", Line: 1, Col: 8},
		{Type: token.True, Lexeme: "true", Literal: true, Line: 1, Col: 11},
		{Type: token.EOF, Lexeme: "", Line: 2, Col: 1},
	})
	got := buf.String()
	for _, want := range []string{"TYPE", "LEXEME", "<eof>", `"42"`, `"s"`, "1:1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("printTokens 输出应含 %q，实际:\n%s", want, got)
		}
	}
}

func TestPrintStats(t *testing.T) {
	var buf bytes.Buffer
	printStats(&buf, vm.NewVM(io.Discard))
	if !strings.Contains(buf.String(), "-- stats:") {
		t.Fatalf("printStats 输出应含「-- stats:」，实际 %q", buf.String())
	}
}

// ---------- needsMoreInput ----------

func TestNeedsMoreInput(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"", false},
		{"print 1;\n", false},
		{"if (true) {\n", true},
		{"fn f() {\n  return 7;\n}\n", false},
		{"fn f() {\n", true},
		{`print "}";` + "\n", false},    // 字符串里的花括号不算
		{`print "{";` + "\n", false},    // 同理
		{"let s = \"abc;\n", true},      // 未闭合字符串
		{`print "a\"b";` + "\n", false}, // 转义引号不闭合字符串
		{"print (1 + 2;\n", true},       // 未闭合括号
		{"while (true) {\n  print 1;\n}\n", false},
	}
	for _, tc := range cases {
		if got := needsMoreInput(tc.src); got != tc.want {
			t.Fatalf("needsMoreInput(%q) = %v, want %v", tc.src, got, tc.want)
		}
	}
}

// ---------- REPL ----------

// captureStdout 把 fn 执行期间写到 os.Stdout 的内容抓回来。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("读管道失败: %v", err)
	}
	return buf.String()
}

// runREPLWithInput 用给定的输入喂 REPL，返回 stdout。
func runREPLWithInput(t *testing.T, o options, input string) string {
	t.Helper()
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatalf("写管道失败: %v", err)
	}
	w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()
	return captureStdout(t, func() { runREPL(o) })
}

func TestREPLBasic(t *testing.T) {
	o, progOut, _ := testOptions()
	stdout := runREPLWithInput(t, o, "print 1 + 2;\n:quit\n")
	if !strings.Contains(stdout, "minilang 0.1.0") {
		t.Fatalf("REPL 应打印 banner，实际 %q", stdout)
	}
	// 程序输出走 session 的 out（这里是 progOut），提示符才走 os.Stdout。
	if progOut.String() != "3\n" {
		t.Fatalf("REPL 执行 print 1 + 2 应输出 3，实际 %q", progOut.String())
	}
}

func TestREPLHelpAndCommands(t *testing.T) {
	o, _, _ := testOptions()
	out := runREPLWithInput(t, o, ":help\n:stats\n:gc\n:quit\n")
	for _, want := range []string{":help", ":quit", ":gc", ":stats", "-- stats:", "GC:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("REPL :help/:stats/:gc 输出应含 %q，实际 %q", want, out)
		}
	}
}

func TestREPLTreeEngineCommands(t *testing.T) {
	// tree 引擎没有自己的对象堆，:stats 应明确说明；:gc 是静默空操作。
	o, _, _ := testOptions()
	o.engine = "tree"
	out := runREPLWithInput(t, o, ":stats\n:gc\n:quit\n")
	if !strings.Contains(out, "tree 引擎没有自己的对象堆") {
		t.Fatalf("tree :stats 应说明没有对象堆，实际 %q", out)
	}
}

func TestREPLMultiline(t *testing.T) {
	// 括号没配平就继续读下一行，配平后一次执行。
	o, progOut, _ := testOptions()
	stdout := runREPLWithInput(t, o, "fn f() {\n  return 7;\n}\nprint f();\n:quit\n")
	if !strings.Contains(stdout, "| ") {
		t.Fatalf("REPL 多行输入时应打印续行提示符，实际 %q", stdout)
	}
	if progOut.String() != "7\n" {
		t.Fatalf("REPL 多行函数定义后应输出 7，实际 %q", progOut.String())
	}
}

func TestREPLStatePersists(t *testing.T) {
	// 上一行 let 的变量下一行能看见：会话复用同一解释器/VM。
	o, progOut, _ := testOptions()
	runREPLWithInput(t, o, "let x = 5;\nprint x * 2;\n:quit\n")
	if progOut.String() != "10\n" {
		t.Fatalf("REPL 跨行应共享变量，实际 %q", progOut.String())
	}
}

func TestREPLEOFExits(t *testing.T) {
	// stdin 立刻 EOF 时 REPL 直接退出。
	o, _, _ := testOptions()
	out := runREPLWithInput(t, o, "")
	if !strings.Contains(out, "minilang 0.1.0") {
		t.Fatalf("REPL 空输入仍应打印 banner，实际 %q", out)
	}
}

func TestREPLEmptyLinesSkipped(t *testing.T) {
	o, progOut, _ := testOptions()
	runREPLWithInput(t, o, "\n\nprint 9;\n:q\n")
	if progOut.String() != "9\n" {
		t.Fatalf("REPL 空行后应正常执行，实际 %q", progOut.String())
	}
}

// ---------- bench ----------

func TestRunBenchVM(t *testing.T) {
	// bench 只跑 VM 引擎：tree 引擎 fib(30) 要十几秒，不适合放进单元测试。
	o, _, _ := testOptions()
	out := captureStdout(t, func() { runBench(o) })
	for _, want := range []string{"fib(30) = 832040", "中位数:", "Go 源码行数:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("bench 输出应含 %q，实际:\n%s", want, out)
		}
	}
}
