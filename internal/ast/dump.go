package ast

import (
	"fmt"
	"strings"

	"github.com/OL486/minilang/internal/token"
)

// ---------- 树形打印（--dump-ast） ----------

// Dump 把语法树渲染成缩进树。
//
// 格式约定：每个节点一行，子节点缩进两格。
// 二元运算打印成 `Binary(+)` 再挂两个子节点，而不是 `(+)` 直接挂三个 ——
// 因为你要一眼看出「谁是左、谁是右」，而不是靠位置猜。
func Dump(stmts []Stmt) string {
	var b strings.Builder
	for _, s := range stmts {
		dumpStmt(&b, s, 0)
	}
	return b.String()
}

func indent(b *strings.Builder, depth int) {
	b.WriteString(strings.Repeat("  ", depth))
}

func dumpStmt(b *strings.Builder, s Stmt, depth int) {
	indent(b, depth)
	switch n := s.(type) {
	case *LetStmt:
		fmt.Fprintf(b, "Let(%s)\n", n.Name)
		indent(b, depth+1)
		b.WriteString("Init:\n")
		dumpExpr(b, n.Init, depth+2)
	case *FnDecl:
		fmt.Fprintf(b, "Fn(%s) params=[%s]\n", n.Name, strings.Join(n.Params, ", "))
		indent(b, depth+1)
		b.WriteString("Body:\n")
		for _, st := range n.Body {
			dumpStmt(b, st, depth+2)
		}
	case *IfStmt:
		b.WriteString("If\n")
		indent(b, depth+1)
		b.WriteString("Cond:\n")
		dumpExpr(b, n.Cond, depth+2)
		indent(b, depth+1)
		b.WriteString("Then:\n")
		for _, st := range n.Then {
			dumpStmt(b, st, depth+2)
		}
		if len(n.Else) > 0 {
			indent(b, depth+1)
			b.WriteString("Else:\n")
			for _, st := range n.Else {
				dumpStmt(b, st, depth+2)
			}
		}
	case *WhileStmt:
		b.WriteString("While\n")
		indent(b, depth+1)
		b.WriteString("Cond:\n")
		dumpExpr(b, n.Cond, depth+2)
		indent(b, depth+1)
		b.WriteString("Body:\n")
		for _, st := range n.Body {
			dumpStmt(b, st, depth+2)
		}
	case *ReturnStmt:
		b.WriteString("Return\n")
		if n.Value != nil {
			dumpExpr(b, n.Value, depth+1)
		}
	case *PrintStmt:
		b.WriteString("Print\n")
		dumpExpr(b, n.Value, depth+1)
	case *BlockStmt:
		b.WriteString("Block\n")
		for _, st := range n.Stmts {
			dumpStmt(b, st, depth+1)
		}
	case *ExprStmt:
		b.WriteString("ExprStmt\n")
		dumpExpr(b, n.X, depth+1)
	default:
		fmt.Fprintf(b, "<unknown stmt %T>\n", s)
	}
}

func dumpExpr(b *strings.Builder, e Expr, depth int) {
	indent(b, depth)
	switch n := e.(type) {
	case *Literal:
		switch v := n.Value.(type) {
		case string:
			fmt.Fprintf(b, "Literal(string %q)\n", v)
		case nil:
			b.WriteString("Literal(nil)\n")
		default:
			fmt.Fprintf(b, "Literal(%v)\n", v)
		}
	case *Variable:
		fmt.Fprintf(b, "Var(%s)\n", n.Name)
	case *Assign:
		fmt.Fprintf(b, "Assign(%s)\n", n.Name)
		dumpExpr(b, n.Value, depth+1)
	case *Unary:
		fmt.Fprintf(b, "Unary(%s)\n", token.OpText(n.Op))
		dumpExpr(b, n.Right, depth+1)
	case *Binary:
		fmt.Fprintf(b, "Binary(%s)\n", token.OpText(n.Op))
		dumpExpr(b, n.Left, depth+1)
		dumpExpr(b, n.Right, depth+1)
	case *Logical:
		fmt.Fprintf(b, "Logical(%s)\n", token.OpText(n.Op))
		dumpExpr(b, n.Left, depth+1)
		dumpExpr(b, n.Right, depth+1)
	case *Grouping:
		b.WriteString("Group\n")
		dumpExpr(b, n.Inner, depth+1)
	case *Call:
		b.WriteString("Call\n")
		indent(b, depth+1)
		b.WriteString("Callee:\n")
		dumpExpr(b, n.Callee, depth+2)
		if len(n.Args) > 0 {
			indent(b, depth+1)
			b.WriteString("Args:\n")
			for _, a := range n.Args {
				dumpExpr(b, a, depth+2)
			}
		}
	default:
		fmt.Fprintf(b, "<unknown expr %T>\n", e)
	}
}
