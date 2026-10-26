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
	// 字节码 VM 只记录行号（chunk 的 Lines 表是按字节存行号，没有列信息）。
	// 这种情况下不要硬凑一个「第 0 列」出来。
	if e.Col <= 0 {
		return fmt.Sprintf("%s错误 (第 %d 行): %s", e.Stage, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s错误 (第 %d 行, 第 %d 列): %s", e.Stage, e.Line, e.Col, e.Msg)
}

// New 构造一条诊断。
func New(stage Stage, line, col int, format string, args ...any) *Error {
	return &Error{Stage: stage, Line: line, Col: col, Msg: fmt.Sprintf(format, args...)}
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
	text := lines[e.Line-1]
	// 行号右对齐到 4 位，方便多行报错时竖着看。
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%4d | %s\n", e.Line, text))
	// Col <= 0 表示「只知道行、不知道列」——字节码 VM 运行时错误就是这种情况，
	// 因为 chunk 只记录了每字节的行号。这时不要画 ^，画在第一列反而误导。
	if e.Col <= 0 {
		return strings.TrimRight(b.String(), "\n")
	}
	// 制表符在终端里占的宽度不固定，这里统一按 4 空格算，保证 ^ 大致落在正确列。
	pad := 0
	for i, r := range text {
		if i+1 >= e.Col {
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
