// 执行步数预算的测试。
//
// 正常程序不受影响、死循环干净报错、预算按次清零，三个行为都在这里覆盖。
// runOnVM / runSource 两个辅助函数在 vm_compiled_test.go 里。
package vm_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/OL486/minilang/internal/semantics"
	"github.com/OL486/minilang/internal/vm"
)

// fib30Source 与 cmd/minilang/bench.go 的 benchSource 是同一份程序，
// 是仓库里最重的正常程序：它的步数决定了预算下限。
const fib30Source = `fn fib(n) {
  if (n < 2) { return n; }
  return fib(n-1) + fib(n-2);
}
let benchResult = fib(30);
`

func TestExecBudgetInfiniteLoop(t *testing.T) {
	start := time.Now()
	_, err := runSource(t, "while (true) { }")
	if err == nil {
		t.Fatal("while (true) {} 没有报错，预算没有生效")
	}
	if !strings.Contains(err.Error(), "执行步数超过") {
		t.Fatalf("死循环报错不是预算错误：%v", err)
	}
	// 120 秒的上限只证明「会停下而不是 hang」：本机实测约 1 秒，
	// 但 -race 下实测约 40 秒（检测器把每步开销放大了数十倍），
	// 再加共享虚拟机波动，上限要留足余量。
	if d := time.Since(start); d > 120*time.Second {
		t.Fatalf("死循环 %v 才报错，太慢（本机实测约 1 秒，-race 下约 40 秒）", d)
	}
}

func TestExecBudgetNormalProgramUnaffected(t *testing.T) {
	var out bytes.Buffer
	v := vm.NewVM(&out)
	if err := runOnVM(t, v, fib30Source); err != nil {
		t.Fatalf("fib(30) 不应撞上限：%v", err)
	}
	if v.Steps() >= semantics.MaxExecSteps {
		t.Fatalf("fib(30) 步数 %d 已达上限 %d", v.Steps(), semantics.MaxExecSteps)
	}
	t.Logf("fib(30) VM 步数 %d，上限 %d", v.Steps(), semantics.MaxExecSteps)
}

func TestExecBudgetResetsBetweenRuns(t *testing.T) {
	// 预算按次清零：同一台 VM 上一次死循环撞限后，
	// 下一个正常程序仍能跑；REPL 复用同一台 VM，全靠这一点。
	var out bytes.Buffer
	v := vm.NewVM(&out)
	if err := runOnVM(t, v, "while (true) { }"); err == nil {
		t.Fatal("死循环没有报错")
	}
	if err := runOnVM(t, v, "print 1 + 2;"); err != nil {
		t.Fatalf("撞限后正常程序跑不起来：%v", err)
	}
	if v.Steps() >= semantics.MaxExecSteps {
		t.Fatalf("小段程序步数 %d 异常", v.Steps())
	}
}
