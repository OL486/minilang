// Package diag 统一处理「带位置的诊断信息」。
//
// 为什么单独抽一个包：词法、语法、运行时三个阶段的错误长得必须一样。
// 如果每个阶段各自拼字符串，最后会出现三种格式的错误信息，
// 而排错时你恰恰最不想读的就是风格不一致的报错。
package diag

import (
	"fmt"
	"strings"
)

// Stage 是错误发生的流水线阶段。区分它不是为了好看：
// 同一个现象（比如「变量未定义」）在词法阶段根本不可能出现，
// 标明阶段能让你立刻知道该去看哪个文件。
type Stage string

const (
	StageLex     Stage = "词法"
	StageParse   Stage = "语法"
	StageCompile Stage = "编译"
	StageRuntime Stage = "运行时"
)

// Error 是一条诊断。Line/Col 从 1 开始。
type Error struct {
	Stage Stage
	Line  int
	Col   int
	Msg   string
}

func (e *Error) Error() string {
	if e.Line <= 0 {
		return fmt.Sprintf("%s错误: %s", e.Stage, e.Msg)
	}
	// Col <= 0 表示「只知道行、不知道列」——比如某些与具体源码点无关的
	// 内部错误（位置是零值）。这时不要画 ^，画在第一列反而误导。
	// 注意：字节码 VM 的运行时错误现在也带列号（chunk 逐字节记录 ast.Pos），
	// 不再走这条分支。
	if e.Col <= 0 {
		return fmt.Sprintf("%s错误 (第 %d 行): %s", e.Stage, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s错误 (第 %d 行, 第 %d 列): %s", e.Stage, e.Line, e.Col, e.Msg)
}

// New 构造一条诊断。
func New(stage Stage, line, col int, format string, args ...any) *Error {
	return &Error{Stage: stage, Line: line, Col: col, Msg: fmt.Sprintf(format, args...)}
}

// maxLineWidth 是渲染时单行源码最多显示的字符数（rune）。
// 超过这个长度的行只显示以错误列为中心的窗口，两侧省略处用 … 标示。
// 没有它，2M 字符的单行输入会让一次报错输出达到 MB 级（终端刷屏、日志爆炸）。
const maxLineWidth = 80

// truncateLine 把超长行截断为以错误列为中心的窗口。
//
// 返回显示文本与错误列在显示文本中的 rune 下标（从 0 开始），
// 调用方据此重算 ^ 的位置。行不超长时原样返回，下标为 col-1。
// col <= 0（只知道行、不知道列）时按行首处理：取行首窗口。
func truncateLine(text string, col int) (string, int) {
	runes := []rune(text)
	if len(runes) <= maxLineWidth {
		return text, col - 1
	}
	// 错误列的 rune 下标（从 0 开始），列号非法时夹到行内。
	errIdx := col - 1
	if errIdx < 0 {
		errIdx = 0
	}
	if errIdx >= len(runes) {
		errIdx = len(runes) - 1
	}
	// 窗口以错误列为中心；贴边时窗口顺势滑到行首或行尾，保证总显示 maxLineWidth 个字符。
	half := maxLineWidth / 2
	start := errIdx - half
	if start < 0 {
		start = 0
	}
	end := start + maxLineWidth
	if end > len(runes) {
		end = len(runes)
		start = end - maxLineWidth
	}
	var b strings.Builder
	if start > 0 {
		b.WriteRune('…')
	}
	b.WriteString(string(runes[start:end]))
	if end < len(runes) {
		b.WriteRune('…')
	}
	// 错误列在显示文本中的 rune 下标：窗口内偏移 + 左侧省略号占 1 位。
	offset := errIdx - start
	if start > 0 {
		offset++
	}
	return b.String(), offset
}

// Render 把诊断渲染成「报错 + 源码片段 + ^ 指针」的块。
//
// 这是本项目「不是玩具」的分界线：没有源码片段的报错，
// 你只能靠行号去数，数错一次就浪费十分钟。
func Render(src string, e *Error) string {
	var b strings.Builder
	b.WriteString(e.Error())
	if e.Line <= 0 {
		return b.String()
	}
	lines := strings.Split(src, "\n")
	if e.Line > len(lines) {
		return b.String()
	}
	// 超长行先截断再显示，否则一次报错能输出 MB 级文本；
	// caret 是截断后错误列在显示文本中的 rune 下标。
	text, caret := truncateLine(lines[e.Line-1], e.Col)
	// 行号右对齐到 4 位，方便多行报错时竖着看。
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%4d | %s\n", e.Line, text))
	// Col <= 0 表示「只知道行、不知道列」——见 Error() 的注释。
	// 这时不要画 ^，画在第一列反而误导。
	if e.Col <= 0 {
		return strings.TrimRight(b.String(), "\n")
	}
	// 制表符在终端里占的宽度不固定，这里统一按 4 空格算，保证 ^ 大致落在正确列。
	// 按 rune 计数：列号本身就是按字符算的（lexer 用 []rune 扫描），
	// 按 byte 数会在多字节字符前把 ^ 画偏。
	pad := 0
	for n, r := range []rune(text) {
		if n >= caret {
			break
		}
		if r == '\t' {
			pad += 4
		} else {
			pad++
		}
	}
	b.WriteString("     | ")
	b.WriteString(strings.Repeat(" ", pad))
	b.WriteString("^")
	return b.String()
}

// RenderAll 渲染一批诊断，去重后按顺序拼接。
func RenderAll(src string, errs []*Error) string {
	seen := make(map[string]bool, len(errs))
	var parts []string
	for _, e := range errs {
		k := e.Error()
		if seen[k] {
			continue
		}
		seen[k] = true
		parts = append(parts, Render(src, e))
	}
	return strings.Join(parts, "\n")
}
