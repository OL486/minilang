// Package ast 定义 minilang 的语法树。
//
// 为什么表达式和语句分成两个接口：语句可以有副作用、不产生值（`let`、`while`），
// 表达式一定产生值。分开之后，`if` 的条件位置在类型上就只接受 Expr，
// 把 `if (let x = 1)` 这种错误挡在编译期，而不是等到运行时才炸。
package ast

import (
	"fmt"
	"strings"

	"github.com/OL486/minilang/internal/token"
)

// Pos 是源码位置，从 1 开始。每个节点都带，因为运行时错误也要能指回源码。
type Pos struct {
	Line int
	Col  int
}

// Node 是所有语法树节点的公共接口。
type Node interface {
	Pos() Pos
}

// Expr 是表达式：一定求出一个值。
type Expr interface {
	Node
	exprNode()
}

// Stmt 是语句：执行，不一定产生值。
type Stmt interface {
	Node
	stmtNode()
}

// ---------- 表达式 ----------

// Literal 是字面量。Value 的 Go 类型：float64 / string / bool / nil。
type Literal struct {
	P     Pos
	Value any
}

// Variable 是变量引用。
type Variable struct {
	P    Pos
	Name string
}

// Assign 是赋值表达式 `x = v`。它本身有值（v），所以是 Expr 不是 Stmt。
// 这一点很重要：`print x = 1;` 在语义上说得通，而且 S6 编译时
// 赋值「留下一个值」与 let 的「把值留在栈槽里」正好复用同一段代码。
type Assign struct {
	P     Pos
	Name  string
	Value Expr
}

// Unary 是一元运算 `!x` `-x`。
type Unary struct {
	P     Pos
	Op    token.Type
	Right Expr
}

// Binary 是二元运算。Op 直接存 token 类型，省一张中间枚举表。
type Binary struct {
	P     Pos
	Left  Expr
	Op    token.Type
	Right Expr
}

// Logical 是 && 和 ||。它必须是独立节点，不能塞进 Binary：
// 逻辑运算是短路求值，不能先算两边再算结果 —— 那会改变副作用顺序，
// 也会让 `f() || true` 白白调用 f。
type Logical struct {
	P     Pos
	Left  Expr
	Op    token.Type
	Right Expr
}

// Grouping 是括号。保留节点而不是解析时直接丢弃，
// 是为了 --dump-ast 能忠实反映源码结构 —— 否则你没法确认
// 「括号真的生效了」还是「碰巧优先级对了」。
type Grouping struct {
	P     Pos
	Inner Expr
}

// Call 是函数调用。
type Call struct {
	P      Pos
	Callee Expr
	Args   []Expr
}

func (e *Literal) Pos() Pos  { return e.P }
func (e *Variable) Pos() Pos { return e.P }
func (e *Assign) Pos() Pos   { return e.P }
func (e *Unary) Pos() Pos    { return e.P }
func (e *Binary) Pos() Pos   { return e.P }
func (e *Logical) Pos() Pos  { return e.P }
func (e *Grouping) Pos() Pos { return e.P }
func (e *Call) Pos() Pos     { return e.P }

func (*Literal) exprNode()  {}
func (*Variable) exprNode() {}
func (*Assign) exprNode()   {}
func (*Unary) exprNode()    {}
func (*Binary) exprNode()   {}
func (*Logical) exprNode()  {}
func (*Grouping) exprNode() {}
func (*Call) exprNode()     {}

// ---------- 语句 ----------

// LetStmt 是变量声明。minilang 只有可变的 let，没有 const。
type LetStmt struct {
	P    Pos
	Name string
	Init Expr
}

// FnDecl 是具名函数声明。Params 只存名字 —— 参数没有默认值也没有类型。
type FnDecl struct {
	P      Pos
	Name   string
	Params []string
	Body   []Stmt
}

// IfStmt 的 Then/Else 都是语句列表，而不是单个语句：
// 这样 `else if` 就自然变成「Else 里放一个 IfStmt」，不需要额外的语法规则。
type IfStmt struct {
	P    Pos
	Cond Expr
	Then []Stmt
	Else []Stmt
}

// WhileStmt 是唯一的循环。
type WhileStmt struct {
	P    Pos
	Cond Expr
	Body []Stmt
}

// ReturnStmt 的 Value 可以为 nil（`return;`）。
type ReturnStmt struct {
	P     Pos
	Value Expr
}

// PrintStmt 是内置语句 `print expr;`。做成语句而不是函数，
// 是因为它不产生值，做函数会让 `print` 变成一等公民，进而出现 `print(print(1))` 这种没意义的写法。
type PrintStmt struct {
	P     Pos
	Value Expr
}

// BlockStmt 是 `{ ... }`，引入新作用域。
type BlockStmt struct {
	P     Pos
	Stmts []Stmt
}

// ExprStmt 是「表达式加分号」。赋值语句走这条路。
type ExprStmt struct {
	P Pos
	X Expr
}

func (s *LetStmt) Pos() Pos    { return s.P }
func (s *FnDecl) Pos() Pos     { return s.P }
func (s *IfStmt) Pos() Pos     { return s.P }
func (s *WhileStmt) Pos() Pos  { return s.P }
func (s *ReturnStmt) Pos() Pos { return s.P }
func (s *PrintStmt) Pos() Pos  { return s.P }
func (s *BlockStmt) Pos() Pos  { return s.P }
func (s *ExprStmt) Pos() Pos   { return s.P }

func (*LetStmt) stmtNode()    {}
func (*FnDecl) stmtNode()     {}
func (*IfStmt) stmtNode()     {}
func (*WhileStmt) stmtNode()  {}
func (*ReturnStmt) stmtNode() {}
func (*PrintStmt) stmtNode()  {}
func (*BlockStmt) stmtNode()  {}
func (*ExprStmt) stmtNode()   {}

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
		fmt.Fprintf(b, "Unary(%s)\n", opText(n.Op))
		dumpExpr(b, n.Right, depth+1)
	case *Binary:
		fmt.Fprintf(b, "Binary(%s)\n", opText(n.Op))
		dumpExpr(b, n.Left, depth+1)
		dumpExpr(b, n.Right, depth+1)
	case *Logical:
		fmt.Fprintf(b, "Logical(%s)\n", opText(n.Op))
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

func opText(t token.Type) string {
	switch t {
	case token.Plus:
		return "+"
	case token.Minus:
		return "-"
	case token.Star:
		return "*"
	case token.Slash:
		return "/"
	case token.Percent:
		return "%"
	case token.Bang:
		return "!"
	case token.Eq:
		return "=="
	case token.Ne:
		return "!="
	case token.Lt:
		return "<"
	case token.Le:
		return "<="
	case token.Gt:
		return ">"
	case token.Ge:
		return ">="
	case token.AndAnd:
		return "&&"
	case token.OrOr:
		return "||"
	}
	return t.String()
}
