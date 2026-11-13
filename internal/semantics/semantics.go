// Package semantics 是 tree-walking 解释器与字节码 VM 共用的语义核心。
//
// 两个引擎以前各写了一份真值、类型名、==、打印、二元运算和运算符文本，
// 而且已经漂移（例如 tree 侧的 opText 没有 !、&&、||）。
// 这里只留一份实现，tree 和 engine 只做薄适配：
//   - tree 的值是 Go 的 any（nil/bool/float64/string/函数），直接调这里的函数；
//   - engine 的值是带标签的 Value，先转成 any 再调这里的函数，
//     字符串比较仍靠驻留后的指针相等，与原来完全一致。
//
// 本包是纯语义，不依赖 diag、ast、tree、engine，因此不会形成 import 环。
package semantics

import (
	"fmt"
	"math"
	"strconv"

	"github.com/OL486/minilang/internal/token"
)

// Number 把 float64 渲染成人能读的字符串。
//
// 规则：整数值不带小数点（1 而不是 1），其余用最短往返表示（0.1 而不是 0.100000）。
// 为什么不用 %v：Go 的 %v 对 float64 用的是 %g，
// 1e6 会打成 1e+06，而 minilang 里 1000000 应该原样打印。
func Number(f float64) string {
	if math.IsNaN(f) {
		return "nan"
	}
	if math.IsInf(f, 1) {
		return "+inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e16 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// HumanBytes 把字节数格式化成人类可读的字符串。
// cmd/minilang 的 bench 输出与 tools/pack 的打包清单共用这一份实现。
func HumanBytes(n uint64) string {
	if n > 1<<20 {
		return fmt.Sprintf("%.2f MiB", float64(n)/float64(1<<20))
	}
	if n > 1<<10 {
		return fmt.Sprintf("%.2f KiB", float64(n)/float64(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// SignedBytes 处理「运行后比运行前更小」的情况。
// 直接用 uint64 相减会下溢成 1.7e19，这个数会以「17592186044415 MiB」的形式
// 出现在报告里 —— 一眼就能看出是算错了，但很容易被当成 GC 把内存还回去了。
func SignedBytes(n int64) string {
	if n < 0 {
		return "-" + HumanBytes(uint64(-n))
	}
	return "+" + HumanBytes(uint64(n))
}

// Truthy 定义真值：只有 false 和 nil 是假，其余（含 0 和 ""）都是真。
//
// 为什么 0 是真：如果 0 是假，`if (n) {...}` 在 n 为 0 时会静默走 else 分支，
// 而 minilang 没有类型系统来提醒你 n 是 number 还是 bool。
// 只把 false/nil 当假，规则最少、最容易预测。
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	}
	return true
}

// TypeName 返回值在错误信息里的类型名。
// 未知类型按 Go 类型名打印，与原来 tree 侧的行为一致。
func TypeName(v any) string {
	switch v.(type) {
	case nil:
		return "nil"
	case bool:
		return "bool"
	case float64:
		return "number"
	case string:
		return "string"
	}
	return fmt.Sprintf("%T", v)
}

// Equal 是 == 的语义：不同类型之间比较一律 false，不报错。
//
// 为什么跨类型比较不报错：`nil == f()` 是很常见的写法，
// 如果 number 与 nil 比较就报错，这段代码没法写。
// 而 `<` 这类大小比较必须同类型，所以那边会报错。
// 函数值按身份比较：同一个函数对象才相等。
func Equal(l, r any) bool {
	switch a := l.(type) {
	case nil:
		return r == nil
	case bool:
		b, ok := r.(bool)
		return ok && a == b
	case float64:
		b, ok := r.(float64)
		return ok && a == b
	case string:
		b, ok := r.(string)
		return ok && a == b
	}
	// 其余类型（函数对象等）按身份比较。
	return l == r
}

// FormatPrimitive 把原始值渲染成 print 的输出。
// 函数值不在这里处理：tree 与 engine 的函数类型不同，
// 各自在薄适配层里补上函数分支，原始值部分只留这一份。
func FormatPrimitive(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "nil", true
	case bool:
		if x {
			return "true", true
		}
		return "false", true
	case float64:
		return Number(x), true
	case string:
		return x, true
	}
	return "", false
}

// FormatFunc 渲染函数值。name 为空时（脚本函数）用 "script"。
func FormatFunc(name string, native bool) string {
	if name == "" {
		name = "script"
	}
	if native {
		return "<native fn " + name + ">"
	}
	return "<fn " + name + ">"
}

// IsBinaryOperator 判断一个 token 类型是不是二元运算符。
func IsBinaryOperator(op token.Type) bool {
	switch op {
	case token.Plus, token.Minus, token.Star, token.Slash, token.Percent,
		token.Eq, token.Ne, token.Lt, token.Le, token.Gt, token.Ge:
		return true
	}
	return false
}

// BinaryOp 计算二元运算。ok 为 false 表示类型不匹配（由调用方报诊断错误）。
// 未知运算符不在这里处理：调用方先用 IsBinaryOperator 检查。
func BinaryOp(op token.Type, l, r any) (any, bool) {
	switch op {
	case token.Plus:
		return Add(l, r)
	case token.Minus, token.Star, token.Slash, token.Percent:
		a, ok1 := l.(float64)
		b, ok2 := r.(float64)
		if !ok1 || !ok2 {
			return nil, false
		}
		v, ok := Arithmetic(op, a, b)
		return v, ok
	case token.Eq:
		return Equal(l, r), true
	case token.Ne:
		return !Equal(l, r), true
	case token.Lt, token.Le, token.Gt, token.Ge:
		return Compare(op, l, r)
	}
	return nil, false
}

// Add 是 + 的语义：number 相加或 string 拼接。
func Add(l, r any) (any, bool) {
	if a, ok := l.(float64); ok {
		if b, ok := r.(float64); ok {
			return a + b, true
		}
	}
	if a, ok := l.(string); ok {
		if b, ok := r.(string); ok {
			return a + b, true
		}
	}
	return nil, false
}

// Arithmetic 是 -、*、/、% 的语义，只接受 number。
// 不检查除零：float64 的 1/0 是 +Inf，NaN 也是合法结果，
// IEEE 754 已经定义好了这两种情况。
func Arithmetic(op token.Type, a, b float64) (float64, bool) {
	switch op {
	case token.Minus:
		return a - b, true
	case token.Star:
		return a * b, true
	case token.Slash:
		return a / b, true
	case token.Percent:
		return math.Mod(a, b), true
	}
	return 0, false
}

// Compare 是 <、<=、>、>= 的语义，只接受同类型 number 或 string。
func Compare(op token.Type, l, r any) (bool, bool) {
	if a, ok := l.(float64); ok {
		if b, ok := r.(float64); ok {
			return CompareNumbers(op, a, b), true
		}
	}
	if a, ok := l.(string); ok {
		if b, ok := r.(string); ok {
			return CompareStrings(op, a, b), true
		}
	}
	return false, false
}

// CompareNumbers 比较两个 number。
func CompareNumbers(op token.Type, a, b float64) bool {
	switch op {
	case token.Lt:
		return a < b
	case token.Le:
		return a <= b
	case token.Gt:
		return a > b
	default:
		return a >= b
	}
}

// CompareStrings 比较两个 string。
func CompareStrings(op token.Type, a, b string) bool {
	switch op {
	case token.Lt:
		return a < b
	case token.Le:
		return a <= b
	case token.Gt:
		return a > b
	default:
		return a >= b
	}
}
