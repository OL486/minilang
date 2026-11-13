package treewalk

import (
	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/semantics"
	"github.com/OL486/minilang/internal/token"
)

// ---------- 语义辅助 ----------
//
// 真值、类型名、==、二元运算、运算符文本的唯一实现在 internal/semantics，
// 这里只剩薄适配：二元运算的 diag 错误包装，以及函数值的打印分支
// （treewalk 的函数类型与 VM 的不同，原始值部分由 semantics.FormatPrimitive 负责）。

func binaryOp(op token.Type, l, r any, p ast.Pos) (any, error) {
	if !semantics.IsBinaryOperator(op) {
		return nil, diag.New(diag.StageRuntime, p.Line, p.Col, "内部错误：未知二元运算符 %s", op)
	}
	if v, ok := semantics.BinaryOp(op, l, r); ok {
		return v, nil
	}
	return nil, mismatch(op, l, r, p)
}

func mismatch(op token.Type, l, r any, p ast.Pos) error {
	return diag.New(diag.StageRuntime, p.Line, p.Col,
		"运算符 %s 不支持 %s 与 %s", token.OpText(op), semantics.TypeName(l), semantics.TypeName(r))
}

// FormatValue 把内部值渲染成 print 的输出。
// 与 VM 侧的 vm.Format 必须逐字节一致，否则两个引擎的 diff 回归会失效。
// 原始值部分由 semantics.FormatPrimitive 实现，这里只补函数值分支。
func FormatValue(v any) string {
	if s, ok := semantics.FormatPrimitive(v); ok {
		return s
	}
	switch x := v.(type) {
	case *Function:
		return semantics.FormatFunc(x.Decl.Name, false)
	case *NativeFn:
		return semantics.FormatFunc(x.Name, true)
	}
	return "<unknown>"
}
