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
	"github.com/OL486/minilang/internal/engine"
	"github.com/OL486/minilang/internal/lexer"
	"github.com/OL486/minilang/internal/parser"
	"github.com/OL486/minilang/internal/token"
	"github.com/OL486/minilang/internal/tree"
)

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
func (s *session) compile(stmts []ast.Stmt, name, src string) (*engine.ObjFunction, bool) {
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
func (s *session) runCompiled(fn *engine.ObjFunction) (engine.Value, error) {
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
