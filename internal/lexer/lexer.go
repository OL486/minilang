// Package lexer 把源码切成 token 流。
package lexer

import (
	"strconv"
	"strings"

	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/token"
)

// Lexer 是单遍扫描器：从左到右吃掉 rune，绝不回退。
//
// 为什么用 []rune 而不是 []byte：列号要按「字符」算。
// 如果按 byte 走，"let 变量 = 1;" 里的中文注释会让列号偏移，
// 报错时 ^ 指的位置就不对了 —— 而位置准确正是本项目的第一要求。
type Lexer struct {
	src  []rune
	cur  int // 下一个待扫描字符的下标
	line int // cur 所在行，从 1 开始
	col  int // cur 所在列，从 1 开始

	startLine int // 当前 token 的起始位置
	startCol  int
	start     int // 当前 token 的起始下标

	tokens []token.Token
	errs   []*diag.Error
}

// New 创建扫描器。
func New(src string) *Lexer {
	return &Lexer{
		src:  []rune(src),
		line: 1,
		col:  1,
	}
}

// Scan 扫描全部 token。返回值里始终以 EOF 结尾，且 EOF 的 Line/Col 指向源码末尾。
// 即使中途出错也继续扫，这样一次能报出所有词法错误，而不是改一个跑一次。
func (l *Lexer) Scan() ([]token.Token, []*diag.Error) {
	for !l.atEnd() {
		l.start = l.cur
		l.startLine = l.line
		l.startCol = l.col
		l.scanToken()
	}
	l.tokens = append(l.tokens, token.Token{
		Type:   token.EOF,
		Lexeme: "",
		Line:   l.line,
		Col:    l.col,
	})
	return l.tokens, l.errs
}

func (l *Lexer) atEnd() bool { return l.cur >= len(l.src) }

// peek 看当前字符，不消费。到末尾返回 0（NUL 不会出现在合法源码里）。
func (l *Lexer) peek() rune {
	if l.atEnd() {
		return 0
	}
	return l.src[l.cur]
}

// peekNext 再看一个字符，用于区分 `=` 与 `==`、`!` 与 `!=`。
func (l *Lexer) peekNext() rune {
	if l.cur+1 >= len(l.src) {
		return 0
	}
	return l.src[l.cur+1]
}

// advance 消费一个字符并推进行列号。
func (l *Lexer) advance() rune {
	r := l.src[l.cur]
	l.cur++
	if r == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return r
}

// match 是 advance 的条件版：只有当前字符等于 want 才消费。
func (l *Lexer) match(want rune) bool {
	if l.atEnd() || l.src[l.cur] != want {
		return false
	}
	l.advance()
	return true
}

func (l *Lexer) add(t token.Type) {
	l.tokens = append(l.tokens, token.Token{
		Type:   t,
		Lexeme: string(l.src[l.start:l.cur]),
		Line:   l.startLine,
		Col:    l.startCol,
	})
}

func (l *Lexer) addLiteral(t token.Type, lit any) {
	l.tokens = append(l.tokens, token.Token{
		Type:    t,
		Lexeme:  string(l.src[l.start:l.cur]),
		Literal: lit,
		Line:    l.startLine,
		Col:     l.startCol,
	})
}

func (l *Lexer) errorf(line, col int, format string, args ...any) {
	l.errs = append(l.errs, diag.New(diag.StageLex, line, col, format, args...))
}

func (l *Lexer) scanToken() {
	r := l.advance()
	switch r {
	case ' ', '\t', '\r', '\n':
		return

	case '(':
		l.add(token.LParen)
	case ')':
		l.add(token.RParen)
	case '{':
		l.add(token.LBrace)
	case '}':
		l.add(token.RBrace)
	case ';':
		l.add(token.Semicolon)
	case ',':
		l.add(token.Comma)
	case '+':
		l.add(token.Plus)
	case '-':
		l.add(token.Minus)
	case '*':
		l.add(token.Star)
	case '%':
		l.add(token.Percent)

	case '/':
		if l.match('/') {
			// 行注释：吃到换行为止，但不要把换行本身吃掉 ——
			// 下一轮循环会走空白分支去更新行号。
			for !l.atEnd() && l.peek() != '\n' {
				l.advance()
			}
		} else {
			l.add(token.Slash)
		}

	case '!':
		if l.match('=') {
			l.add(token.Ne)
		} else {
			l.add(token.Bang)
		}
	case '=':
		if l.match('=') {
			l.add(token.Eq)
		} else {
			l.add(token.Assign)
		}
	case '<':
		if l.match('=') {
			l.add(token.Le)
		} else {
			l.add(token.Lt)
		}
	case '>':
		if l.match('=') {
			l.add(token.Ge)
		} else {
			l.add(token.Gt)
		}
	case '&':
		if l.match('&') {
			l.add(token.AndAnd)
		} else {
			l.errorf(l.startLine, l.startCol, "不支持的字符 '&'，逻辑与请写 &&")
		}
	case '|':
		if l.match('|') {
			l.add(token.OrOr)
		} else {
			l.errorf(l.startLine, l.startCol, "不支持的字符 '|'，逻辑或请写 ||")
		}

	case '"':
		l.scanString()

	default:
		switch {
		case isDigit(r):
			l.scanNumber()
		case isAlpha(r):
			l.scanIdent()
		default:
			l.errorf(l.startLine, l.startCol, "无法识别的字符 %q", string(r))
		}
	}
}

// scanString 处理字符串字面量，支持 \\ \" \n \t \r 四种转义。
//
// 为什么要在这里做转义而不是留给运行时：字符串的值必须在词法阶段就定下来，
// 因为常量池里存的是最终值。如果拖到运行时，"a\nb" 和 "a\\nb" 会在
// 两个引擎里走出不同的结果。
func (l *Lexer) scanString() {
	var sb strings.Builder
	for {
		if l.atEnd() {
			l.errorf(l.startLine, l.startCol, "字符串未闭合：缺少结尾的双引号")
			break
		}
		r := l.peek()
		if r == '\n' {
			// 不做跨行字符串。跨行字符串会让「行号」这个概念在报错时失效，
			// 对教学项目来说不值得。
			l.errorf(l.startLine, l.startCol, "字符串未闭合：字符串不能跨行，第 %d 行就已换行", l.line)
			break
		}
		l.advance()
		if r == '"' {
			l.addLiteral(token.String, sb.String())
			return
		}
		if r != '\\' {
			sb.WriteRune(r)
			continue
		}
		if l.atEnd() {
			l.errorf(l.startLine, l.startCol, "字符串未闭合：反斜杠后没有字符")
			break
		}
		e := l.advance()
		switch e {
		case 'n':
			sb.WriteRune('\n')
		case 't':
			sb.WriteRune('\t')
		case 'r':
			sb.WriteRune('\r')
		case '\\':
			sb.WriteRune('\\')
		case '"':
			sb.WriteRune('"')
		default:
			l.errorf(l.line, l.col-2, "未知的转义序列 \\%c，支持 \\\\ \\\" \\n \\t \\r", e)
			sb.WriteRune(e)
		}
	}
	// 出错时补一个占位 token，让 parser 不至于因为缺 token 报一串级联错误。
	l.addLiteral(token.String, sb.String())
}

// scanNumber 扫描十进制数：digits [ '.' digits ]。
//
// 这里主动检查「第二个小数点」，因为 1.2.3 如果交给 strconv.ParseFloat 会报
// 一个「invalid syntax」并丢掉具体位置，而我们需要的是「第 1 行第 3 列多余的小数点」。
func (l *Lexer) scanNumber() {
	for isDigit(l.peek()) {
		l.advance()
	}
	if l.peek() == '.' && isDigit(l.peekNext()) {
		l.advance() // 吃掉 '.'
		for isDigit(l.peek()) {
			l.advance()
		}
		if l.peek() == '.' {
			l.errorf(l.line, l.col, "数字字面量中出现了第二个小数点")
		}
	}
	text := string(l.src[l.start:l.cur])
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		l.errorf(l.startLine, l.startCol, "数字字面量 %q 无法解析: %v", text, err)
		return
	}
	l.addLiteral(token.Number, v)
}

func (l *Lexer) scanIdent() {
	for isAlphaNumeric(l.peek()) {
		l.advance()
	}
	text := string(l.src[l.start:l.cur])
	l.add(token.LookupIdent(text))
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func isAlpha(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
}

func isAlphaNumeric(r rune) bool { return isAlpha(r) || isDigit(r) }
