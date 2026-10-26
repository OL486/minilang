// Package token 定义 minilang 的词法单元。
//
// 设计取舍：Token 用「Type + Lexeme + Literal」三段式，而不是只存 Type。
// 原因有两个，都是排错时才会感谢自己的那种：
//  1. Lexeme 保留源码原文，--dump-tokens 和错误信息可以直接复用，不需要回头再切一遍源码；
//  2. Literal 存的是「已经解析好的值」（数字已转 float64、字符串已去引号并处理转义），
//     parser 拿到的就是可用值。如果让 parser 自己去 parse 数字字符串，那么
//     "1.2.3" 这类错误会在两个地方各报一次，且报的位置不一致。
package token

import "fmt"

// Type 是词法单元类型。用 uint8 而不是 string：
// 比较是整数比较（快），且不会因为拼写错误在编译期之外的地方静默失败。
type Type uint8

const (
	EOF Type = iota

	// 字面量与标识符
	Ident
	Number
	String

	// 关键字
	Let
	Fn
	If
	Else
	While
	Return
	Print
	True
	False
	Nil

	// 分隔符
	LParen
	RParen
	LBrace
	RBrace
	Semicolon
	Comma

	// 运算符
	Plus
	Minus
	Star
	Slash
	Percent
	Bang
	Assign
	Eq
	Ne
	Lt
	Le
	Gt
	Ge
	AndAnd
	OrOr
)

var typeNames = [...]string{
	EOF:       "EOF",
	Ident:     "IDENT",
	Number:    "NUMBER",
	String:    "STRING",
	Let:       "LET",
	Fn:        "FN",
	If:        "IF",
	Else:      "ELSE",
	While:     "WHILE",
	Return:    "RETURN",
	Print:     "PRINT",
	True:      "TRUE",
	False:     "FALSE",
	Nil:       "NIL",
	LParen:    "LPAREN",
	RParen:    "RPAREN",
	LBrace:    "LBRACE",
	RBrace:    "RBRACE",
	Semicolon: "SEMICOLON",
	Comma:     "COMMA",
	Plus:      "PLUS",
	Minus:     "MINUS",
	Star:      "STAR",
	Slash:     "SLASH",
	Percent:   "PERCENT",
	Bang:      "BANG",
	Assign:    "ASSIGN",
	Eq:        "EQ",
	Ne:        "NE",
	Lt:        "LT",
	Le:        "LE",
	Gt:        "GT",
	Ge:        "GE",
	AndAnd:    "AND_AND",
	OrOr:      "OR_OR",
}

func (t Type) String() string {
	if int(t) < len(typeNames) && typeNames[t] != "" {
		return typeNames[t]
	}
	return fmt.Sprintf("Type(%d)", uint8(t))
}

// Token 是词法单元。Line/Col 从 1 开始，列号按 rune 计数（中文注释不会把列号算歪）。
type Token struct {
	Type    Type
	Lexeme  string
	Literal any // Number -> float64, String -> string, 其余为 nil
	Line    int
	Col     int
}

// Keywords 是保留字表。放在包级 var 而不是函数里，避免每次扫描新建 map。
var Keywords = map[string]Type{
	"let":    Let,
	"fn":     Fn,
	"if":     If,
	"else":   Else,
	"while":  While,
	"return": Return,
	"print":  Print,
	"true":   True,
	"false":  False,
	"nil":    Nil,
}

// LookupIdent 判断一个标识符文本是不是关键字。
func LookupIdent(s string) Type {
	if t, ok := Keywords[s]; ok {
		return t
	}
	return Ident
}
