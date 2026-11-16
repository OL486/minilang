// Package vm_test 的 --trace 黑盒测试：走「源码 → 编译 → 执行」整条流水线，
// 验证跟踪在跨函数调用（帧切换）时仍能逐行输出，且不影响程序结果。
package vm_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/OL486/minilang/internal/vm"
)

func TestTraceAcrossFunctionCall(t *testing.T) {
	var out bytes.Buffer
	v := vm.NewVM(&out)
	v.SetTrace(true)

	err := runOnVM(t, v, `
fn inc(x) { return x + 1; }
print inc(41);
`)
	if err != nil {
		t.Fatalf("运行失败: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, "42\n") {
		t.Fatalf("程序输出缺失，got = %q", got)
	}
	// 调用与返回都被跟踪到：OP_CALL 在脚本帧，OP_RETURN 在 inc 帧与脚本帧各一次。
	if !strings.Contains(got, "OP_CALL") {
		t.Fatalf("trace 缺 OP_CALL 行，got = %q", got)
	}
	if strings.Count(got, "OP_RETURN") < 2 {
		t.Fatalf("trace 应有至少两行 OP_RETURN（inc 帧 + 脚本帧），got = %q", got)
	}
	// 至少有一行栈顶是函数调用的结果 42（OP_PRINT 执行前的栈顶）。
	if !strings.Contains(got, "栈顶: 42") {
		t.Fatalf("trace 应有栈顶为 42 的行，got = %q", got)
	}
}
