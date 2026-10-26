// Package parser 把 token 流变成语法树。
//
// 语句部分用递归下降（每种语句一条函数），表达式部分用 Pratt（precedence climbing）。
// 为什么表达式不能也用递归下降：`+` `*` `==` `&&` 有 6 个优先级，
// 递归下降要为每个优先级写一条函数（parseTerm→parseFactor→parseUnary…），
// 加一个运算符就要改三条函数。Pratt 把「优先级」变成一张表 + 一个循环，
// 加运算符只改表。
package parser

import (
	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/token"
)

// 优先级表。数值越大结合越紧。
//
// 为什么赋值不在这张表里：赋值是右结合，且左值必须是变量，
// 塞进循环里反而要写特例。它在 expression() 里单独处理，只有一层。
const (
	precNone     = 0
	precOr       = 1 // ||
	precAnd      = 2 // &&
	precEquality = 3 // == !=
	precCompare  = 4 // < <= > >=
	precTerm     = 5 // + -
	precFactor   = 6 // * / %
	precUnary    = 7 // ! -
	precCall     = 8 // ()
)

func binaryPrec(t token.Type) int {
	switch t {
	case token.OrOr:
		return precOr
	case token.AndAnd:
		return precAnd
	case token.Eq, token.Ne:
		return precEquality
	case token.Lt, token.Le, token.Gt, token.Ge:
		return precCompare
	case token.Plus, token.Minus:
		return precTerm
	case token.Star, token.Slash, token.Percent:
		return precFactor
	}
	return precNone
}

// maxErrors 限制单次解析报错数量。
// 不加限制时，一个漏掉的 `}` 会让后面每个 token 都报一次错，输出几千行，真正的原因被埋掉。
const maxErrors = 20

type Parser struct {
	tokens []token.Token
	cur    int
	errs   []*diag.Error
}

// New 创建解析器。tokens 必须以 EOF 结尾（lexer 保证）。
func New(tokens []token.Token) *Parser {
	return &Parser{tokens: tokens}
}

// abort 用于把「语法错误」从深层递归直接抛回语句边界。
// 用 panic 而不是逐层返回 error：递归下降的每一层都要检查 error，
// 代码会被 `if err != nil { return nil, err }` 淹没，真正的逻辑看不见。
type abort struct{}

// Parse 解析整个程序。
//
// 出错后不中止：跳到下一个语句边界继续解析，这样一次能报出多个语法错误。
func (p *Parser) Parse() ([]ast.Stmt, []*diag.Error) {
	var stmts []ast.Stmt
	for !p.atEnd() {
		s := p.safeStatement()
		if s != nil {
			stmts = append(stmts, s)
		}
	}
	return stmts, p.errs
}

func (p *Parser) safeStatement() (s ast.Stmt) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(abort); !ok {
				panic(r)
			}
			s = nil
			p.synchronize()
		}
	}()
	return p.statement()
}

// ---------- token 游标 ----------

func (p *Parser) peek() token.Token { return p.tokens[p.cur] }

func (p *Parser) previous() token.Token { return p.tokens[p.cur-1] }

func (p *Parser) atEnd() bool { return p.peek().Type == token.EOF }

func (p *Parser) advance() token.Token {
	if !p.atEnd() {
		p.cur++
	}
	return p.previous()
}

func (p *Parser) check(t token.Type) bool { return p.peek().Type == t }

func (p *Parser) match(types ...token.Type) bool {
	for _, t := range types {
		if p.check(t) {
			p.advance()
			return true
		}
	}
	return false
}

func (p *Parser) consume(t token.Type, what string) token.Token {
	if p.check(t) {
		return p.advance()
	}
	p.failHere("期望 %s，实际得到 %s", what, describe(p.peek()))
	return token.Token{}
}

// ---------- 错误 ----------

func (p *Parser) failHere(format string, args ...any) {
	p.failAt(p.peek(), format, args...)
}

func (p *Parser) failAt(t token.Token, format string, args ...any) {
	if len(p.errs) >= maxErrors {
		panic(abort{})
	}
	p.errs = append(p.errs, diag.New(diag.StageParse, t.Line, t.Col, format, args...))
	panic(abort{})
}

// describe 生成人类可读的 token 描述。
// 直接打印 token.Type 的枚举名（比如 SEMICOLON）对新手不友好，
// 所以这里额外带上源码原文。
func describe(t token.Token) string {
	if t.Type == token.EOF {
		return "文件结束"
	}
	if t.Lexeme != "" {
		return "'" + t.Lexeme + "'"
	}
	return t.Type.String()
}

// synchronize 在报错后跳过 token，直到看起来像一条新语句的开头。
// 这一步决定了「一次报多个错」是不是有用：跳得不准会把正确的代码也吞掉。
func (p *Parser) synchronize() {
	if p.atEnd() {
		return
	}
	p.advance()
	for !p.atEnd() {
		if p.previous().Type == token.Semicolon {
			return
		}
		switch p.peek().Type {
		case token.Fn, token.Let, token.If, token.While, token.Return, token.Print, token.RBrace:
			return
		}
		p.advance()
	}
}

// ---------- 语句 ----------

func (p *Parser) statement() ast.Stmt {
	switch {
	case p.match(token.Let):
		return p.letStatement(p.previous())
	case p.match(token.Fn):
		return p.fnDecl(p.previous())
	case p.match(token.If):
		return p.ifStatement(p.previous())
	case p.match(token.While):
		return p.whileStatement(p.previous())
	case p.match(token.Return):
		return p.returnStatement(p.previous())
	case p.match(token.Print):
		return p.printStatement(p.previous())
	case p.match(token.LBrace):
		return p.block(p.previous())
	default:
		return p.exprStatement()
	}
}

func pos(t token.Token) ast.Pos { return ast.Pos{Line: t.Line, Col: t.Col} }

func (p *Parser) letStatement(kw token.Token) ast.Stmt {
	name := p.consume(token.Ident, "变量名")
	if name.Type != token.Ident {
		// consume 已经 panic，这里只是让静态分析器安心。
		return nil
	}
	p.consume(token.Assign, "变量声明必须初始化，形如 let x = 1")
	init := p.expression()
	p.consume(token.Semicolon, "语句末尾的分号")
	return &ast.LetStmt{P: pos(kw), Name: name.Lexeme, Init: init}
}

func (p *Parser) fnDecl(kw token.Token) ast.Stmt {
	name := p.consume(token.Ident, "函数名")
	var params []string
	p.consume(token.LParen, "参数列表的左括号")
	if !p.check(token.RParen) {
		for {
			pt := p.consume(token.Ident, "参数名")
			params = append(params, pt.Lexeme)
			if !p.match(token.Comma) {
				break
			}
		}
	}
	p.consume(token.RParen, "参数列表的右括号")
	p.consume(token.LBrace, "函数体的左花括号")
	body := p.blockBody()
	return &ast.FnDecl{P: pos(kw), Name: name.Lexeme, Params: params, Body: body}
}

// block 处理 `{ ... }`，左花括号已经被消费。
func (p *Parser) block(_ token.Token) ast.Stmt {
	stmts := p.blockBody()
	return &ast.BlockStmt{P: pos(p.previous()), Stmts: stmts}
}

// blockBody 读到匹配的右花括号为止。左花括号已被消费。
func (p *Parser) blockBody() []ast.Stmt {
	var stmts []ast.Stmt
	for !p.check(token.RBrace) && !p.atEnd() {
		s := p.safeStatement()
		if s != nil {
			stmts = append(stmts, s)
		}
	}
	p.consume(token.RBrace, "代码块的右花括号")
	return stmts
}

func (p *Parser) ifStatement(kw token.Token) ast.Stmt {
	p.consume(token.LParen, "if 条件必须用括号包裹，形如 if (x < 1)")
	cond := p.expression()
	p.consume(token.RParen, "if 条件的右括号")
	p.consume(token.LBrace, "if 分支的左花括号")
	then := p.blockBody()

	var elseBody []ast.Stmt
	if p.match(token.Else) {
		if p.match(token.If) {
			// `else if` 直接复用 ifStatement，再包一层单元素切片。
			// 好处是嵌套任意层 if/else if 都不需要新语法，AST 也保持统一。
			elseBody = []ast.Stmt{p.ifStatement(p.previous())}
		} else {
			p.consume(token.LBrace, "else 分支的左花括号")
			elseBody = p.blockBody()
		}
	}
	return &ast.IfStmt{P: pos(kw), Cond: cond, Then: then, Else: elseBody}
}

func (p *Parser) whileStatement(kw token.Token) ast.Stmt {
	p.consume(token.LParen, "while 条件必须用括号包裹，形如 while (x < 10)")
	cond := p.expression()
	p.consume(token.RParen, "while 条件的右括号")
	p.consume(token.LBrace, "while 循环体的左花括号")
	body := p.blockBody()
	return &ast.WhileStmt{P: pos(kw), Cond: cond, Body: body}
}

func (p *Parser) returnStatement(kw token.Token) ast.Stmt {
	var value ast.Expr
	if !p.check(token.Semicolon) {
		value = p.expression()
	}
	p.consume(token.Semicolon, "return 语句末尾的分号")
	return &ast.ReturnStmt{P: pos(kw), Value: value}
}

func (p *Parser) printStatement(kw token.Token) ast.Stmt {
	value := p.expression()
	p.consume(token.Semicolon, "print 语句末尾的分号")
	return &ast.PrintStmt{P: pos(kw), Value: value}
}

func (p *Parser) exprStatement() ast.Stmt {
	start := p.peek()
	e := p.expression()
	p.consume(token.Semicolon, "语句末尾的分号")
	return &ast.ExprStmt{P: pos(start), X: e}
}

// ---------- 表达式 ----------

// expression 处理赋值（最低优先级、右结合），其余交给 Pratt。
func (p *Parser) expression() ast.Expr {
	e := p.parsePrecedence(precOr)
	if p.check(token.Assign) {
		eq := p.advance()
		value := p.expression() // 右结合：a = b = 1 解析成 a = (b = 1)
		v, ok := e.(*ast.Variable)
		if !ok {
			p.failAt(eq, "赋值号左侧必须是变量名，这里是一个 %T", e)
		}
		return &ast.Assign{P: v.P, Name: v.Name, Value: value}
	}
	return e
}

// parsePrecedence 是 Pratt 的核心。
//
// 循环条件 `pr < min` 是关键：调用方用 pr+1 表示「只吃比我更紧的运算符」，
// 于是左结合自然成立（a - b - c 解析成 (a-b)-c）。
// 如果这里写成 pr <= min，减法会变成右结合，1-2-3 会算成 1-(2-3)=2。
func (p *Parser) parsePrecedence(min int) ast.Expr {
	left := p.parsePrefix()
	for {
		t := p.peek().Type
		if t == token.LParen && precCall >= min {
			p.advance() // 吃掉 '('，finishCall 假定它已被消费
			left = p.finishCall(left)
			continue
		}
		pr := binaryPrec(t)
		if pr == 0 || pr < min {
			break
		}
		op := p.advance()
		right := p.parsePrecedence(pr + 1)
		if t == token.AndAnd || t == token.OrOr {
			left = &ast.Logical{P: left.Pos(), Left: left, Op: op.Type, Right: right}
		} else {
			left = &ast.Binary{P: left.Pos(), Left: left, Op: op.Type, Right: right}
		}
	}
	return left
}

func (p *Parser) parsePrefix() ast.Expr {
	t := p.advance()
	switch t.Type {
	case token.Number, token.String:
		return &ast.Literal{P: pos(t), Value: t.Literal}
	case token.True:
		return &ast.Literal{P: pos(t), Value: true}
	case token.False:
		return &ast.Literal{P: pos(t), Value: false}
	case token.Nil:
		return &ast.Literal{P: pos(t), Value: nil}
	case token.Ident:
		return &ast.Variable{P: pos(t), Name: t.Lexeme}
	case token.LParen:
		inner := p.expression()
		p.consume(token.RParen, "括号的右括号")
		return &ast.Grouping{P: pos(t), Inner: inner}
	case token.Bang, token.Minus:
		right := p.parsePrecedence(precUnary)
		return &ast.Unary{P: pos(t), Op: t.Type, Right: right}
	}
	p.failAt(t, "表达式不能以 %s 开头", describe(t))
	return nil
}

// finishCall 解析调用实参，左括号已被消费。
// 调用是最高优先级，且左结合：(f(1))(2) 会解析成对 f(1) 的返回值再调用。
func (p *Parser) finishCall(callee ast.Expr) ast.Expr {
	var args []ast.Expr
	if !p.check(token.RParen) {
		for {
			args = append(args, p.expression())
			if !p.match(token.Comma) {
				break
			}
		}
	}
	p.consume(token.RParen, "调用实参列表的右括号")
	return &ast.Call{P: callee.Pos(), Callee: callee, Args: args}
}
