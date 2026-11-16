package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/compiler"
	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/lexer"
	"github.com/OL486/minilang/internal/parser"
	"github.com/OL486/minilang/internal/token"
	"github.com/OL486/minilang/internal/treewalk"
	"github.com/OL486/minilang/internal/vm"
)

// session 持有跨行存活的状态：REPL 里 VM 和解释器必须复用，
// 否则上一行 let 的全局变量下一行就看不见了。
type session struct {
	opt    options
	vm     *vm.VM
	interp *treewalk.Interp
}

func newSession(o options) *session {
	s := &session{opt: o}
	if o.engine == "tree" {
		// tree 引擎没有字节码，--trace 无从跟踪：明确提示而不是静默忽略，
		// 程序仍正常执行。
		if o.trace {
			fmt.Fprintln(o.errOut, "--trace 仅支持 VM 引擎：tree 引擎没有字节码，无法逐指令跟踪（程序仍会正常执行）")
		}
		s.interp = treewalk.New(o.out)
	} else {
		s.vm = vm.NewVM(o.out)
		if o.gcOff {
			s.vm.SetGCEnabled(false)
		}
		if o.trace {
			s.vm.SetTrace(true)
		}
	}
	return s
}

// parseSource 只做词法 + 语法（S1–S2），返回语句列表。
// dumpTokens 由调用方决定：runSource 按 --dump-tokens 打印，
// bench 不打印 token 流（以前也不打印）。
func (s *session) parseSource(src string, dumpTokens bool) ([]ast.Stmt, bool) {
	// ---- S1: 词法 ----
	tokens, lexErrs := lexer.New(src).Scan()
	if dumpTokens && s.opt.dumpTokens {
		printTokens(s.opt.out, tokens)
	}
	if len(lexErrs) > 0 {
		fmt.Fprintln(s.opt.errOut, diag.RenderAll(src, lexErrs))
		return nil, false
	}

	// ---- S2: 语法 ----
	stmts, parseErrs := parser.New(tokens).Parse()
	if s.opt.dumpAST {
		fmt.Fprint(s.opt.out, ast.Dump(stmts))
	}
	if len(parseErrs) > 0 {
		fmt.Fprintln(s.opt.errOut, diag.RenderAll(src, parseErrs))
		return nil, false
	}
	return stmts, true
}

// compile 把语句编译成字节码函数（仅 VM 引擎）。
// 反汇编在这里打印一次：runSource 和 bench 都走这里，不会重复。
// src 只用来渲染错误时的源码片段。
func (s *session) compile(stmts []ast.Stmt, name, src string) (*vm.ObjFunction, bool) {
	fn, compErrs := compiler.CompileProgram(s.vm, stmts)
	if len(compErrs) > 0 {
		fmt.Fprintln(s.opt.errOut, diag.RenderAll(src, compErrs))
		return nil, false
	}
	if s.opt.dumpBytecode {
		fmt.Fprint(s.opt.out, fn.Chunk.Disassemble(name))
	}
	return fn, true
}

// runTree 用 tree-walking 解释器执行已解析的语句。
func (s *session) runTree(stmts []ast.Stmt) error {
	return s.interp.Run(stmts)
}

// runCompiled 执行一个已编译好的脚本函数（仅 VM 引擎）。
func (s *session) runCompiled(fn *vm.ObjFunction) (vm.Value, error) {
	return s.vm.RunScript(fn)
}

// runParsed 执行已解析的语句：tree 直接跑，VM 先编译再跑。
// src 只用来渲染错误时的源码片段。
func (s *session) runParsed(stmts []ast.Stmt, name, src string) bool {
	// ---- S3/S4: tree-walking ----
	if s.opt.engine == "tree" {
		if err := s.runTree(stmts); err != nil {
			reportRuntime(s.opt.errOut, src, err)
			return false
		}
		return true
	}

	// ---- S6: 编译 ----
	fn, ok := s.compile(stmts, name, src)
	if !ok {
		return false
	}

	// ---- S7/S8: 执行 ----
	if _, err := s.runCompiled(fn); err != nil {
		reportRuntime(s.opt.errOut, src, err)
		return false
	}
	if s.opt.stats {
		printStats(s.opt.out, s.vm)
	}
	return true
}

// runSource 跑一段源码，返回是否全部成功。
func (s *session) runSource(src, name string) bool {
	stmts, ok := s.parseSource(src, true)
	if !ok {
		return false
	}
	if len(stmts) == 0 {
		return true
	}
	return s.runParsed(stmts, name, src)
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

func printStats(w io.Writer, vm *vm.VM) {
	st := vm.Stats()
	fmt.Fprintf(w, "-- stats: 存活对象 %d, 堆 %d B, GC 次数 %d, 累计创建对象 %d\n",
		st.Objects, st.Bytes, st.GCRuns, st.ObjectsTotal)
}

// ---------- 文件执行 ----------

// maxSourceBytes 是单个源码的上限：runFile 读文件、run - 读 stdin 都受它约束，
// 避免一个超大输入被整块读进内存。超限时 readSource 报干净错误，不执行。
const maxSourceBytes = 10 * 1024 * 1024

// readSource 读一段源码。path 为 "-" 时从 stdin 读（`minilang run -`）。
// 无论哪条路，超过 maxSourceBytes 都报错，不返回半截内容。
func readSource(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(io.LimitReader(stdin, maxSourceBytes+1))
		if err != nil {
			return nil, fmt.Errorf("读取 stdin 失败: %w", err)
		}
		if len(data) > maxSourceBytes {
			return nil, fmt.Errorf("源码超过 %d MiB 上限", maxSourceBytes/(1024*1024))
		}
		return data, nil
	}
	if st, err := os.Stat(path); err == nil && st.Size() > maxSourceBytes {
		return nil, fmt.Errorf("源码超过 %d MiB 上限", maxSourceBytes/(1024*1024))
	}
	return os.ReadFile(path)
}

func runFile(path string, o options) int {
	data, err := readSource(path, o.stdin)
	if err != nil {
		fmt.Fprintf(o.errOut, "无法读取文件 %s: %v\n", path, err)
		return 1
	}
	name := filepath.Base(path)
	if path == "-" {
		name = "<stdin>"
	}
	s := newSession(o)
	if !s.runSource(string(data), name) {
		return 1
	}
	return 0
}

// checkFile 只做词法 + 语法 + 编译检查，不执行任何代码。
//
// 只编译一次、恒走 VM 编译，理由：treewalk 直接解释 AST，没有独立的
// 编译阶段，所以不存在「tree 编译」；而 VM 编译是最严的一层静态检查
// （同一作用域重复声明、局部变量超 256、常量池超限、实参超 255），
// 能抓到 treewalk 要到运行时才暴露、甚至不暴露的问题。
// 因此 --engine 对 check 不适用：无论传 vm 还是 tree，check 都一样。
func checkFile(path string, o options) int {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(o.errOut, "无法读取文件 %s: %v\n", path, err)
		return 1
	}
	o.engine = "vm" // check 恒用 VM 编译器（见上）；newSession 据此建 VM
	s := newSession(o)
	stmts, ok := s.parseSource(string(data), true)
	if !ok {
		return 1
	}
	if len(stmts) == 0 {
		fmt.Fprintf(o.out, "%s: OK\n", path)
		return 0
	}
	if _, ok := s.compile(stmts, filepath.Base(path), string(data)); !ok {
		return 1
	}
	fmt.Fprintf(o.out, "%s: OK\n", path)
	return 0
}

// fmtFile 按规范格式打印源码到 stdout，不改原文件、不执行代码。
//
// 只做词法 + 语法（S1–S2），不编译：格式化的对象是「能解析的源码」
// 而不是「能编译的程序」，重复声明这类编译错误不影响打印。
// 输入走 readSource：与 run 一致的 10 MiB 上限，`-` 从 stdin 读。
// --dump-* 开关在这里强制关闭，否则它们的输出会混进 stdout、
// 污染格式化结果（fmt 的 stdout 就是交付物）。
func fmtFile(path string, o options) int {
	data, err := readSource(path, o.stdin)
	if err != nil {
		fmt.Fprintf(o.errOut, "无法读取文件 %s: %v\n", path, err)
		return 1
	}
	o.dumpTokens = false
	o.dumpAST = false
	o.dumpBytecode = false
	o.trace = false
	s := newSession(o)
	stmts, ok := s.parseSource(string(data), false)
	if !ok {
		return 1
	}
	fmt.Fprint(o.out, ast.Format(stmts))
	return 0
}
