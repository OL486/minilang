package ast

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/OL486/minilang/internal/semantics"
	"github.com/OL486/minilang/internal/token"
)

// ---------- 规范格式打印（minilang fmt） ----------

// Format 把语句列表渲染成规范格式的源码。
//
// 与 Dump 的分工：Dump 是调试用的缩进树（看结构），Format 是可重新解析的
// 源码（看文本）。两者共用「按节点类型 switch + 递归」的遍历结构。
//
// 规范格式（与 examples/*.ml 的风格一致）：
//   - 两格缩进；每个语句一行，以 ; 结尾（块语句除外）
//   - `fn f(a, b) {`、`if (c) {`、`while (c) {`，左花括号前一个空格
//   - `} else if (c) {`、`else {` 与前一个 } 同行
//   - 空函数体 / 空块写成 `fn f() {}`（一行），非空块展开多行
//   - 不插空行、不保留注释：AST 里没有这两类信息，词法阶段就丢了注释
//   - 用户写的括号原样保留（Grouping 节点直接打印），不重新推导最小括号
//   - 数字按 strconv.FormatFloat(v, 'f', -1, 64) 打印：1.0 写成 1，
//     且永不出现指数形式，保证输出一定能重新解析
//   - 字符串按 strconv.Quote 重新加引号：可打印 Unicode（包括中文）原样保留
//
// 输出满足：Format(Parse(Format(s))) == Format(s)（幂等）；
// 空程序输出空字符串（不带换行）。
func Format(stmts []Stmt) string {
	var b strings.Builder
	f := &formatter{b: &b}
	for _, s := range stmts {
		f.stmt(s, 0)
	}
	return b.String()
}

type formatter struct {
	b *strings.Builder
}

// stmt 打印一条语句（含末尾换行）。depth 是当前缩进层级。
func (f *formatter) stmt(s Stmt, depth int) {
	// 与 Dump 同样的防御：parser 保证解析出的 AST 深度 ≤ MaxNestingDepth，
	// 这里是对手写 AST 的防御，否则万层嵌套的手写节点会耗尽栈。
	// 截断行写成注释，输出仍可解析（只是那之后的内容丢了）。
	if depth > semantics.MaxNestingDepth {
		indent(f.b, depth)
		f.b.WriteString("// …嵌套太深，已截断\n")
		return
	}
	indent(f.b, depth)
	switch n := s.(type) {
	case *LetStmt:
		fmt.Fprintf(f.b, "let %s = ", n.Name)
		f.expr(n.Init, depth)
		f.b.WriteString(";\n")
	case *FnDecl:
		fmt.Fprintf(f.b, "fn %s(%s)", n.Name, strings.Join(n.Params, ", "))
		f.block(n.Body, depth)
		f.b.WriteString("\n")
	case *IfStmt:
		f.ifStmt(n, depth)
		f.b.WriteString("\n")
	case *WhileStmt:
		f.b.WriteString("while (")
		f.expr(n.Cond, depth)
		f.b.WriteString(")")
		f.block(n.Body, depth)
		f.b.WriteString("\n")
	case *ReturnStmt:
		if n.Value == nil {
			f.b.WriteString("return;\n")
		} else {
			f.b.WriteString("return ")
			f.expr(n.Value, depth)
			f.b.WriteString(";\n")
		}
	case *PrintStmt:
		f.b.WriteString("print ")
		f.expr(n.Value, depth)
		f.b.WriteString(";\n")
	case *BlockStmt:
		if len(n.Stmts) == 0 {
			f.b.WriteString("{}\n")
			return
		}
		f.b.WriteString("{\n")
		for _, st := range n.Stmts {
			f.stmt(st, depth+1)
		}
		indent(f.b, depth)
		f.b.WriteString("}\n")
	case *ExprStmt:
		f.expr(n.X, depth)
		f.b.WriteString(";\n")
	default:
		fmt.Fprintf(f.b, "<unknown stmt %T>\n", s)
	}
}

// ifStmt 打印 if 语句，不带前导缩进、不带末尾换行，
// 这样 `} else if (c) {` 可以复用它（else-if 在 AST 里就是
// Else 中的单个 IfStmt，见 parser.ifStatement）。
func (f *formatter) ifStmt(n *IfStmt, depth int) {
	f.b.WriteString("if (")
	f.expr(n.Cond, depth)
	f.b.WriteString(")")
	f.block(n.Then, depth)
	if len(n.Else) == 1 {
		if elif, ok := n.Else[0].(*IfStmt); ok {
			f.b.WriteString(" else ")
			f.ifStmt(elif, depth)
			return
		}
	}
	if len(n.Else) > 0 {
		f.b.WriteString(" else")
		f.block(n.Else, depth)
	}
}

// block 打印「 { … }」部分：紧跟在 fn/if/while 头后面。
// 空体写成一行 ` {}`，非空体展开多行，右花括号与头同缩进。
func (f *formatter) block(stmts []Stmt, depth int) {
	if len(stmts) == 0 {
		f.b.WriteString(" {}")
		return
	}
	f.b.WriteString(" {\n")
	for _, s := range stmts {
		f.stmt(s, depth+1)
	}
	indent(f.b, depth)
	f.b.WriteString("}")
}

// expr 打印表达式（行内，不带缩进与换行）。
func (f *formatter) expr(e Expr, depth int) {
	if depth > semantics.MaxNestingDepth {
		// 与 stmt 同样的防御；行内只能写一个占位表达式，取 nil
		// （输出仍可解析；parser 保证真实输入走不到这里）。
		f.b.WriteString("nil")
		return
	}
	switch n := e.(type) {
	case *Literal:
		switch v := n.Value.(type) {
		case string:
			f.b.WriteString(strconv.Quote(v))
		case float64:
			// 'f' 保证永不出现指数形式：`1e20` 这类写法在源码里本就不合法，
			// 输出里一旦出现 `e`，重新解析就会失败。
			f.b.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
		case bool:
			if v {
				f.b.WriteString("true")
			} else {
				f.b.WriteString("false")
			}
		case nil:
			f.b.WriteString("nil")
		default:
			fmt.Fprintf(f.b, "%v", v)
		}
	case *Variable:
		f.b.WriteString(n.Name)
	case *Assign:
		fmt.Fprintf(f.b, "%s = ", n.Name)
		f.expr(n.Value, depth+1)
	case *Unary:
		f.b.WriteString(token.OpText(n.Op))
		f.expr(n.Right, depth+1)
	case *Binary:
		f.expr(n.Left, depth+1)
		fmt.Fprintf(f.b, " %s ", token.OpText(n.Op))
		f.expr(n.Right, depth+1)
	case *Logical:
		f.expr(n.Left, depth+1)
		fmt.Fprintf(f.b, " %s ", token.OpText(n.Op))
		f.expr(n.Right, depth+1)
	case *Grouping:
		f.b.WriteString("(")
		f.expr(n.Inner, depth+1)
		f.b.WriteString(")")
	case *Call:
		f.expr(n.Callee, depth+1)
		f.b.WriteString("(")
		for i, a := range n.Args {
			if i > 0 {
				f.b.WriteString(", ")
			}
			f.expr(a, depth+1)
		}
		f.b.WriteString(")")
	default:
		fmt.Fprintf(f.b, "<unknown expr %T>", e)
	}
}
