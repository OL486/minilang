// Command minilang 是 minilang 语言的命令行入口。
//
//	minilang run <file.ml>        执行文件
//	minilang repl                 交互式解释器
//	minilang bench                跑 fib(30) 基准测试
//
// 三个调试开关（--dump-tokens / --dump-ast / --dump-bytecode）可以任意组合，
// 用来逐段检查流水线。它们输出到 stdout，执行结果也输出到 stdout，
// 报错输出到 stderr —— 这样 `minilang run x.ml > out.txt` 拿到的是干净的程序输出。
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/compiler"
	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/engine"
	"github.com/OL486/minilang/internal/lexer"
	"github.com/OL486/minilang/internal/parser"
	"github.com/OL486/minilang/internal/token"
	"github.com/OL486/minilang/internal/tree"
)

const version = "0.1.0"

type options struct {
	dumpTokens   bool
	dumpAST      bool
	dumpBytecode bool
	stats        bool
	engine       string
	gcOff        bool
	out          io.Writer
	errOut       io.Writer
}

func defaultOptions() options {
	return options{engine: "vm", out: os.Stdout, errOut: os.Stderr}
}

func addFlags(fs *flag.FlagSet, o *options) {
	fs.BoolVar(&o.dumpTokens, "dump-tokens", false, "打印词法分析后的 token 流（类型/字面量/行号）")
	fs.BoolVar(&o.dumpAST, "dump-ast", false, "打印解析后的 AST（缩进树形）")
	fs.BoolVar(&o.dumpBytecode, "dump-bytecode", false, "打印反汇编结果（偏移/opcode/操作数/常量池）")
	fs.BoolVar(&o.stats, "stats", false, "执行结束后打印对象堆与 GC 统计")
	fs.BoolVar(&o.gcOff, "gc-off", false, "关闭 GC（仅用于对比，长时间运行会吃满内存）")
	fs.StringVar(&o.engine, "engine", "vm", "执行引擎: vm | tree")
}

func main() {
	args := os.Args[1:]

	// 先把子命令摘出来，剩下的才交给 flag 解析。
	cmd := "run"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "repl", "bench", "run":
			cmd = args[0]
			args = args[1:]
		case "help", "--help", "-h":
			usage()
			return
		case "version", "--version", "-v":
			fmt.Printf("minilang %s\n", version)
			return
		}
	}

	o := defaultOptions()
	fs := flag.NewFlagSet("minilang", flag.ExitOnError)
	addFlags(fs, &o)
	// 自己先把参数分成「开关」和「位置参数」两组。
	// 标准库 flag 遇到第一个非开关参数就停止解析，
	// 于是 `minilang run a.ml --dump-ast` 里的 --dump-ast 会被静默忽略 ——
	// 这个坑不修的话，验收时最常用的写法恰好不生效。
	positional := parseArgs(fs, args)
	checkEngine(&o)

	switch cmd {
	case "repl":
		runREPL(o)
	case "bench":
		runBench(o)
	default:
		if len(positional) < 1 {
			usage()
			os.Exit(2)
		}
		os.Exit(runFile(positional[0], o))
	}
}

// parseArgs 把参数重排成「所有开关在前、位置参数在后」再交给 flag。
// 只有 -engine 需要带值，且支持 -engine=vm 与 -engine vm 两种写法。
func parseArgs(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		if !strings.Contains(a, "=") && (a == "-engine" || a == "--engine") && i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	_ = fs.Parse(flags)
	return positional
}

func checkEngine(o *options) {
	if o.engine != "vm" && o.engine != "tree" {
		fmt.Fprintf(os.Stderr, "未知引擎 %q，可选 vm | tree\n", o.engine)
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stdout, `minilang %s — 一门带字节码 VM 的小语言

用法:
  minilang run <file.ml> [flags]   执行脚本
  minilang <file.ml> [flags]       同上
  minilang repl [flags]            交互式解释器
  minilang bench [flags]           fib(30) 基准测试

调试开关:
  --dump-tokens      打印 token 流
  --dump-ast         打印语法树
  --dump-bytecode    打印反汇编
  --engine=vm|tree   选择执行引擎（默认 vm）
  --stats            执行后打印对象堆统计
  --gc-off           关闭 GC（对比用）

REPL 命令:
  :help  显示帮助    :quit  退出
  :gc    立即回收    :stats 查看堆统计
`, version)
}

// ---------- 会话 ----------

// session 持有跨行存活的状态：REPL 里 VM 和解释器必须复用，
// 否则上一行 let 的全局变量下一行就看不见了。
type session struct {
	opt    options
	vm     *engine.VM
	interp *tree.Interp
}

func newSession(o options) *session {
	s := &session{opt: o}
	if o.engine == "tree" {
		s.interp = tree.New(o.out)
	} else {
		s.vm = engine.NewVM(o.out)
		if o.gcOff {
			s.vm.SetGCEnabled(false)
		}
	}
	return s
}

// runSource 跑一段源码，返回是否全部成功。
func (s *session) runSource(src, name string) bool {
	// ---- S1: 词法 ----
	tokens, lexErrs := lexer.New(src).Scan()
	if s.opt.dumpTokens {
		printTokens(s.opt.out, tokens)
	}
	if len(lexErrs) > 0 {
		fmt.Fprintln(s.opt.errOut, diag.RenderAll(src, lexErrs))
		return false
	}

	// ---- S2: 语法 ----
	stmts, parseErrs := parser.New(tokens).Parse()
	if s.opt.dumpAST {
		fmt.Fprint(s.opt.out, ast.Dump(stmts))
	}
	if len(parseErrs) > 0 {
		fmt.Fprintln(s.opt.errOut, diag.RenderAll(src, parseErrs))
		return false
	}
	if len(stmts) == 0 {
		return true
	}

	// ---- S3/S4: tree-walking ----
	if s.opt.engine == "tree" {
		if err := s.interp.Run(stmts); err != nil {
			reportRuntime(s.opt.errOut, src, err)
			return false
		}
		return true
	}

	// ---- S6: 编译 ----
	fn, compErrs := compiler.CompileProgram(s.vm, stmts)
	if len(compErrs) > 0 {
		fmt.Fprintln(s.opt.errOut, diag.RenderAll(src, compErrs))
		return false
	}
	if s.opt.dumpBytecode {
		fmt.Fprint(s.opt.out, fn.Chunk.Disassemble(name))
	}

	// ---- S7/S8: 执行 ----
	if _, err := s.vm.RunScript(fn); err != nil {
		reportRuntime(s.opt.errOut, src, err)
		return false
	}
	if s.opt.stats {
		printStats(s.opt.out, s.vm)
	}
	return true
}

func reportRuntime(w io.Writer, src string, err error) {
	if de, ok := err.(*diag.Error); ok {
		fmt.Fprintln(w, diag.Render(src, de))
		return
	}
	fmt.Fprintln(w, err)
}

func printTokens(w io.Writer, tokens []token.Token) {
	fmt.Fprintf(w, "%-5s %-12s %-22s %-18s %s\n", "#", "TYPE", "LEXEME", "LITERAL", "POS")
	for i, t := range tokens {
		lex := t.Lexeme
		if t.Type == token.EOF {
			lex = "<eof>"
		}
		lit := "-"
		switch v := t.Literal.(type) {
		case nil:
		case string:
			lit = strconv.Quote(v)
		case float64:
			lit = strconv.FormatFloat(v, 'g', -1, 64)
		default:
			lit = fmt.Sprint(v)
		}
		fmt.Fprintf(w, "%-5d %-12s %-22s %-18s %d:%d\n", i, t.Type, strconv.Quote(lex), lit, t.Line, t.Col)
	}
}

func printStats(w io.Writer, vm *engine.VM) {
	st := vm.Stats()
	fmt.Fprintf(w, "-- stats: 存活对象 %d, 堆 %d B, GC 次数 %d, 累计创建对象 %d\n",
		st.Objects, st.Bytes, st.GCRuns, st.ObjectsTotal)
}

// ---------- 文件执行 ----------

func runFile(path string, o options) int {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(o.errOut, "无法读取文件 %s: %v\n", path, err)
		return 1
	}
	s := newSession(o)
	if !s.runSource(string(data), filepath.Base(path)) {
		return 1
	}
	return 0
}

// ---------- REPL ----------

func runREPL(o options) {
	s := newSession(o)
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	fmt.Printf("minilang %s  [engine=%s]  :help 查看命令，:quit 退出\n", version, o.engine)
	var pending strings.Builder

	for {
		if pending.Len() == 0 {
			fmt.Print("> ")
		} else {
			fmt.Print("| ")
		}
		if !in.Scan() {
			fmt.Println()
			return
		}
		line := in.Text()

		if pending.Len() == 0 {
			cmd := strings.TrimSpace(line)
			switch cmd {
			case ":quit", ":q", "exit":
				return
			case ":help", ":h":
				fmt.Print("  :help   帮助\n  :quit   退出\n  :gc     立即执行一次标记-清除\n  :stats  查看对象堆统计\n")
				continue
			case ":gc":
				if s.vm != nil {
					before := s.vm.Stats()
					s.vm.CollectGarbage()
					after := s.vm.Stats()
					fmt.Printf("  GC: 对象 %d -> %d, 堆 %d B -> %d B\n", before.Objects, after.Objects, before.Bytes, after.Bytes)
				}
				continue
			case ":stats":
				if s.vm != nil {
					printStats(os.Stdout, s.vm)
				} else {
					fmt.Println("  tree 引擎没有自己的对象堆")
				}
				continue
			case "":
				continue
			}
		}

		pending.WriteString(line)
		pending.WriteString("\n")

		// 括号/花括号没配平就继续读下一行。
		// 这是 REPL 里最容易被忽略的体验细节：没有它，
		// 输入 `if (x) {` 回车就会立刻报一个莫名其妙的语法错误。
		if needsMoreInput(pending.String()) {
			continue
		}
		src := pending.String()
		pending.Reset()
		s.runSource(src, "<repl>")
	}
}

// needsMoreInput 判断是否需要继续读取。
// 只统计字符串字面量之外的括号 —— 否则 `print "}";` 会被误判。
func needsMoreInput(src string) bool {
	depth := 0
	inString := false
	escaped := false
	for _, r := range src {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '(', '{':
			depth++
		case ')', '}':
			depth--
		}
	}
	return depth > 0 || inString
}

// ---------- 基准测试 ----------

// benchSource 是被测程序。用 let 接收结果而不是 print，
// 避免 I/O 时间混进测量区间。
const benchSource = `fn fib(n) {
  if (n < 2) { return n; }
  return fib(n-1) + fib(n-2);
}
let benchResult = fib(30);
`

func runBench(o options) {
	src := benchSource

	tokens, lexErrs := lexer.New(src).Scan()
	if len(lexErrs) > 0 {
		fmt.Fprintln(o.errOut, diag.RenderAll(src, lexErrs))
		os.Exit(1)
	}
	stmts, parseErrs := parser.New(tokens).Parse()
	if len(parseErrs) > 0 {
		fmt.Fprintln(o.errOut, diag.RenderAll(src, parseErrs))
		os.Exit(1)
	}
	if o.dumpAST {
		fmt.Fprint(o.out, ast.Dump(stmts))
	}

	fmt.Printf("minilang 基准测试  engine=%s  fib(30)\n", o.engine)

	var times []time.Duration
	var result string
	var heapBefore, heapAfter uint64

	if o.engine == "tree" {
		interp := tree.New(io.Discard)
		heapBefore = readHeap()
		for i := 0; i < 3; i++ {
			start := time.Now()
			if err := interp.Run(stmts); err != nil {
				fmt.Fprintln(o.errOut, "执行失败:", err)
				os.Exit(1)
			}
			times = append(times, time.Since(start))
		}
		heapAfter = readHeap()
		if v, ok := interp.Globals().Lookup("benchResult"); ok {
			result = tree.FormatValue(v)
		}
	} else {
		vm := engine.NewVM(io.Discard)
		fn, compErrs := compiler.CompileProgram(vm, stmts)
		if len(compErrs) > 0 {
			fmt.Fprintln(o.errOut, diag.RenderAll(src, compErrs))
			os.Exit(1)
		}
		if o.dumpBytecode {
			fmt.Fprint(o.out, fn.Chunk.Disassemble("<bench>"))
		}
		heapBefore = readHeap()
		for i := 0; i < 3; i++ {
			start := time.Now()
			if _, err := vm.RunScript(fn); err != nil {
				fmt.Fprintln(o.errOut, "执行失败:", err)
				os.Exit(1)
			}
			times = append(times, time.Since(start))
		}
		heapAfter = readHeap()
		if v, ok := vm.Global("benchResult"); ok {
			result = engine.Format(v)
		}
	}

	median := medianDuration(times)
	fmt.Printf("  fib(30) = %s\n", result)
	fmt.Printf("  三次耗时: %s, %s, %s\n", fmtDur(times[0]), fmtDur(times[1]), fmtDur(times[2]))
	fmt.Printf("  中位数:   %s\n", fmtDur(median))
	fmt.Printf("  Go 堆: 运行前 %s -> 运行后 %s（差 %s）\n",
		humanBytes(heapBefore), humanBytes(heapAfter), signedBytes(int64(heapAfter)-int64(heapBefore)))
	fmt.Printf("  Go 源码行数: %d 行（非空非注释 %d 行）\n", locTotal, locCode)
}

func medianDuration(ds []time.Duration) time.Duration {
	cp := make([]time.Duration, len(ds))
	copy(cp, ds)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	return cp[len(cp)/2]
}

func fmtDur(d time.Duration) string {
	return fmt.Sprintf("%.1f ms", float64(d.Nanoseconds())/1e6)
}

func humanBytes(n uint64) string {
	if n > 1<<20 {
		return fmt.Sprintf("%.2f MiB", float64(n)/float64(1<<20))
	}
	if n > 1<<10 {
		return fmt.Sprintf("%.2f KiB", float64(n)/float64(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// signedBytes 处理「运行后比运行前更小」的情况。
// 直接用 uint64 相减会下溢成 1.7e19，这个数会以「17592186044415 MiB」的形式
// 出现在报告里 —— 一眼就能看出是算错了，但很容易被当成 GC 把内存还回去了。
func signedBytes(n int64) string {
	if n < 0 {
		return "-" + humanBytes(uint64(-n))
	}
	return "+" + humanBytes(uint64(n))
}

func readHeap() uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// ---------- 代码行数统计 ----------

var locTotal, locCode int

func init() {
	locTotal, locCode = countLOC()
}

// countLOC 统计当前模块的 Go 代码行数（不含 _test.go）。
// 用运行期统计而不是外部脚本，是为了让 S10 的对比表在任何机器上都能复现。
func countLOC() (total, code int) {
	root, err := moduleRoot()
	if err != nil {
		return 0, 0
	}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+"node_modules"+string(filepath.Separator)) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		inBlock := false
		for _, line := range strings.Split(string(data), "\n") {
			t := strings.TrimSpace(line)
			if t == "" {
				continue
			}
			total++
			if inBlock {
				if strings.Contains(t, "*/") {
					inBlock = false
				}
				continue
			}
			if strings.HasPrefix(t, "//") {
				continue
			}
			if strings.HasPrefix(t, "/*") {
				if !strings.Contains(t, "*/") {
					inBlock = true
				}
				continue
			}
			code++
		}
		return nil
	})
	return total, code
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("找不到 go.mod")
		}
		dir = parent
	}
}
