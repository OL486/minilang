// Package numfmt 保证 tree-walking 解释器和字节码 VM 打印同一个数时字符完全一致。
//
// 这不是洁癖：S5 的基线是拿 tree-walking 跑 fib(30)，
// 如果两个引擎的输出格式不同，你没法用 diff 做回归对比，
// 每次都要人眼核对，回归测试就废了。
//
// 实现已移到 internal/semantics（与真值、==、打印等语义放在一起），
// 这里只保留兼容的薄转发，避免已有的 import 被迫改动。
package numfmt

import (
	"github.com/OL486/minilang/internal/semantics"
)

// Number 把 float64 渲染成人能读的字符串。
//
// 规则：整数值不带小数点（1 而不是 1），其余用最短往返表示（0.1 而不是 0.100000）。
// 为什么不用 %v：Go 的 %v 对 float64 用的是 %g，
// 1e6 会打成 1e+06，而 minilang 里 1000000 应该原样打印。
func Number(f float64) string {
	return semantics.Number(f)
}
