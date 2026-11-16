// Package vm_test 是 VM 执行层的黑盒测试：走「源码 → 词法 → 语法 → 编译 →
// 执行」整条流水线，验证 run/call/upvalue/arith/gc 在真实程序上的行为。
//
// 必须用外部测试包（package vm_test）：internal/vm 的白盒测试（run_test.go、
// gc_test.go、call_arith_test.go）与 VM 同包，而 compiler 又依赖 vm，
// 白盒测试文件里 import compiler 会造成 import cycle。
// 这里只用 VM 的公开 API（NewVM、RunScript、CollectGarbage、Global）。
package vm_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/OL486/minilang/internal/compiler"
	"github.com/OL486/minilang/internal/lexer"
	"github.com/OL486/minilang/internal/parser"
	"github.com/OL486/minilang/internal/vm"
)

// runOnVM 在给定的 VM 上编译并执行一段源码，返回执行期的错误。
// 调用方自己持有 out 缓冲区（NewVM 时传入），以便多次运行间 Reset。
func runOnVM(t *testing.T, v *vm.VM, src string) error {
	t.Helper()
	tokens, lexErrs := lexer.New(src).Scan()
	if len(lexErrs) > 0 {
		return lexErrs[0]
	}
	stmts, parseErrs := parser.New(tokens).Parse()
	if len(parseErrs) > 0 {
		return parseErrs[0]
	}
	fn, compErrs := compiler.CompileProgram(v, stmts)
	if len(compErrs) > 0 {
		return compErrs[0]
	}
	_, err := v.RunScript(fn)
	return err
}

// runSource 在全新 VM 上编译并执行一段源码，返回打印输出与错误。
func runSource(t *testing.T, src string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	v := vm.NewVM(&out)
	err := runOnVM(t, v, src)
	return out.String(), err
}

func TestTwoClosuresShareUpvalue(t *testing.T) {
	// 两个闭包共享同一个 upvalue：一个写，另一个立刻能读到。
	// 如果捕获时拷了值而不是共享存储，read() 永远读不到 bump() 的修改。
	out, err := runSource(t, `
let read = nil;
fn makePair() {
  let v = 0;
  fn bump() { v = v + 10; return v; }
  fn get() { return v; }
  read = get;
  return bump;
}
let bump = makePair();
print bump();
print read();
print bump();
print read();
`)
	if err != nil {
		t.Fatalf("运行失败: %v", err)
	}
	if out != "10\n10\n20\n20\n" {
		t.Fatalf("输出 = %q, want %q", out, "10\n10\n20\n20\n")
	}
}

func TestCounterIndependence(t *testing.T) {
	// 两个计数器必须互不干扰：c2 的第一次调用不能读到 c1 的计数。
	out, err := runSource(t, `
fn makeCounter() {
  let count = 0;
  fn inc() { count = count + 1; return count; }
  return inc;
}
let c1 = makeCounter();
let c2 = makeCounter();
print c1();
print c1();
print c2();
print c1();
`)
	if err != nil {
		t.Fatalf("运行失败: %v", err)
	}
	if out != "1\n2\n1\n3\n" {
		t.Fatalf("输出 = %q, want %q", out, "1\n2\n1\n3\n")
	}
}

func TestCallDepthLimit(t *testing.T) {
	// 递归 2000 层必然撞上 1000 层调用帧上限，错误必须是干净的
	// diag 错误，而不是 Go 栈崩溃。
	_, err := runSource(t, `
fn f(n) { if (n == 0) { return 0; } return f(n - 1); }
print f(2000);
`)
	if err == nil || !strings.Contains(err.Error(), "调用栈溢出") {
		t.Fatalf("深递归应报错调用栈溢出，实际 err = %v", err)
	}
}

func TestCallArityMismatch(t *testing.T) {
	_, err := runSource(t, `
fn f(a, b) { return a + b; }
print f(1);
`)
	if err == nil || !strings.Contains(err.Error(), "函数 f 需要 2 个参数，传入了 1 个") {
		t.Fatalf("实参不足应报错参数个数，实际 err = %v", err)
	}

	_, err = runSource(t, `print clock(1);`)
	if err == nil || !strings.Contains(err.Error(), "内置函数 clock 需要 0 个参数") {
		t.Fatalf("内置函数实参过多应报错，实际 err = %v", err)
	}
}

func TestArithDivideModulo(t *testing.T) {
	out, err := runSource(t, `
print 7 / 2;
print 7 % 3;
print (0 - 7) % 3;
print 1 / 0;
print (0 - 1) / 0;
print 0 / 0;
`)
	if err != nil {
		t.Fatalf("运行失败: %v", err)
	}
	want := "3.5\n1\n-1\n+inf\n-inf\nnan\n"
	if out != want {
		t.Fatalf("输出 = %q, want %q", out, want)
	}
}

func TestArithCrossTypeErrors(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{`print "a" - 1;`, "运算符 - 不支持"},
		{`print true + 1;`, "运算符 + 不支持"},
		{`print "a" + 1;`, "运算符 + 不支持"},
		{`print 1 < "a";`, "运算符 < 不支持"},
		{`print 1 > "a";`, "运算符 > 不支持"},
		{`print 1 >= "a";`, "运算符 >= 不支持"},
		{`print 1 <= "a";`, "运算符 <= 不支持"},
		{`print "a" / 1;`, "运算符 / 不支持"},
		{`print "a" % 1;`, "运算符 % 不支持"},
		{`print nil * 2;`, "运算符 * 不支持"},
	}
	for _, c := range cases {
		_, err := runSource(t, c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("runSource(%s) 应报错 %q，实际 err = %v", c.src, c.want, err)
		}
	}
}

func TestCompareStringsAndEquality(t *testing.T) {
	out, err := runSource(t, `
print "abc" < "abd";
print "b" > "a";
print "a" <= "a";
print 1 == "1";
print nil == nil;
print "x" == "x";
`)
	if err != nil {
		t.Fatalf("运行失败: %v", err)
	}
	want := "true\ntrue\ntrue\nfalse\ntrue\ntrue\n"
	if out != want {
		t.Fatalf("输出 = %q, want %q", out, want)
	}
}

func TestAddStringConcat(t *testing.T) {
	out, err := runSource(t, `
print "ab" + "cd" + "ef";
let s = "";
let i = 0;
while (i < 200) { s = s + "x"; i = i + 1; }
print s;
`)
	if err != nil {
		t.Fatalf("运行失败: %v", err)
	}
	want := "abcdef\n" + strings.Repeat("x", 200) + "\n"
	if out != want {
		t.Fatalf("拼接输出长度 = %d, want %d", len(out), len(want))
	}
}

func TestChainedAssignment(t *testing.T) {
	// OpSetLocal 不弹栈：赋值表达式的值是右值，要留在栈顶给外层用。
	// 如果写成 pop 再 push，`x = y = 7` 的中间结果就没了。
	out, err := runSource(t, `
let x = 0;
let y = 0;
x = y = 7;
print x;
print y;
print x = 9;
print x;
`)
	if err != nil {
		t.Fatalf("运行失败: %v", err)
	}
	if out != "7\n7\n9\n9\n" {
		t.Fatalf("输出 = %q, want %q", out, "7\n7\n9\n9\n")
	}
}

func TestNegateAndNot(t *testing.T) {
	out, err := runSource(t, `
print -5;
print !nil;
print !0;
print !(1 == 2);
`)
	if err != nil {
		t.Fatalf("运行失败: %v", err)
	}
	if out != "-5\ntrue\nfalse\ntrue\n" {
		t.Fatalf("输出 = %q, want %q", out, "-5\ntrue\nfalse\ntrue\n")
	}
}

func TestGCFunctionConstantsSurvive(t *testing.T) {
	// 函数对象在 globals 里是根；它的常量池（字符串）靠 blacken(ObjFunction) 保住。
	// 如果标记漏了常量，回收后调用 greet 会拿到已死字符串。
	var out bytes.Buffer
	v := vm.NewVM(&out)
	if err := runOnVM(t, v, `fn greet() { return "hello"; }`); err != nil {
		t.Fatalf("定义 greet 失败: %v", err)
	}
	v.CollectGarbage()
	v.CollectGarbage()
	out.Reset()
	if err := runOnVM(t, v, `print greet();`); err != nil {
		t.Fatalf("回收后调用 greet 失败: %v", err)
	}
	if out.String() != "hello\n" {
		t.Fatalf("回收后 greet() 输出 = %q, want %q", out.String(), "hello\n")
	}
}

func TestGCClosedUpvalueSurvives(t *testing.T) {
	// 闭包返回后，捕获的变量以 closed upvalue 形态活在堆上；
	// 它 Closed 里装的字符串必须靠 blacken(ObjUpvalue) 保住。
	// 如果标记漏了 Closed，回收后调用问候函数会拿到已死字符串。
	var out bytes.Buffer
	v := vm.NewVM(&out)
	if err := runOnVM(t, v, `
fn makeGreeter() {
  let name = "world";
  fn greet() { return "hi " + name; }
  return greet;
}
let g = makeGreeter();
`); err != nil {
		t.Fatalf("定义 greeter 失败: %v", err)
	}
	v.CollectGarbage()
	v.CollectGarbage()
	out.Reset()
	if err := runOnVM(t, v, `print g();`); err != nil {
		t.Fatalf("回收后调用 g() 失败: %v", err)
	}
	if out.String() != "hi world\n" {
		t.Fatalf("回收后 g() 输出 = %q, want %q", out.String(), "hi world\n")
	}
}

func TestGCClosedCounterKeepsCounting(t *testing.T) {
	// 同一个模式的数字版：closed upvalue 里的数值在多次回收后仍能累加。
	var out bytes.Buffer
	v := vm.NewVM(&out)
	if err := runOnVM(t, v, `
fn makeCounter() {
  let count = 0;
  fn inc() { count = count + 1; return count; }
  return inc;
}
let c = makeCounter();
`); err != nil {
		t.Fatalf("定义 counter 失败: %v", err)
	}
	v.CollectGarbage()
	out.Reset()
	if err := runOnVM(t, v, `print c(); print c();`); err != nil {
		t.Fatalf("回收后调用 c() 失败: %v", err)
	}
	if out.String() != "1\n2\n" {
		t.Fatalf("回收后 c() 输出 = %q, want %q", out.String(), "1\n2\n")
	}
}

func TestNestedClosureCapturesUpvalue(t *testing.T) {
	// 三级嵌套：最内层捕获的 a、b 里，b 来自中间层闭包的 upvalue
	//（OP_CLOSURE 的 else 分支：从外层闭包继承，而不是从栈上抓）。
	// 如果 else 分支写错，inner 读到的 b 会是错值或崩溃。
	out, err := runSource(t, `
fn outer(a) {
  fn middle(b) {
    fn inner(c) {
      return a + b + c;
    }
    return inner;
  }
  return middle;
}
print outer(1)(2)(3);
`)
	if err != nil {
		t.Fatalf("运行失败: %v", err)
	}
	if out != "6\n" {
		t.Fatalf("输出 = %q, want %q", out, "6\n")
	}
}

func TestBlockScopedCapturedLocal(t *testing.T) {
	// 块作用域里被捕获的局部变量：块退出时编译器发 OP_CLOSE_UPVALUE，
	// 把值从栈搬到堆上。如果这条指令漏了或关错了槽位，
	// 块结束后闭包读到的就是别人的值。
	out, err := runSource(t, `
fn make() {
  let f = nil;
  if (true) {
    let x = 42;
    fn get() { return x; }
    f = get;
  }
  return f;
}
print make()();
`)
	if err != nil {
		t.Fatalf("运行失败: %v", err)
	}
	if out != "42\n" {
		t.Fatalf("输出 = %q, want %q", out, "42\n")
	}
}

func TestNativeCallSuccess(t *testing.T) {
	// 内置函数调用成功路径：clock() 应返回当前毫秒数（> 0）。
	out, err := runSource(t, `print clock() > 0;`)
	if err != nil {
		t.Fatalf("运行失败: %v", err)
	}
	if out != "true\n" {
		t.Fatalf("输出 = %q, want %q", out, "true\n")
	}
}

func TestGCMarksFramesAndOpenUpvalues(t *testing.T) {
	// 在函数执行中途触发 GC：循环拼接累计分配超过 1 MiB，必然在 make()
	// 执行期间触发至少一次回收。此时调用帧里的闭包（第 2 类根）
	// 和仍 open 的 upvalue（第 4 类根）都必须被标记；
	// 如果标记漏了，执行中的函数或它捕获的变量会被回收，
	// 症状是程序在 GC 后立刻行为错乱，而不是在 GC 那一刻崩溃。
	var out bytes.Buffer
	v := vm.NewVM(&out)
	if err := runOnVM(t, v, `
fn make() {
  let x = "alive";
  fn get() { return x; }
  let s = "";
  let i = 0;
  while (i < 2000) { s = s + "x"; i = i + 1; }
  return get;
}
let g = make();
`); err != nil {
		t.Fatalf("定义 make 失败: %v", err)
	}
	if v.Stats().GCRuns < 1 {
		t.Fatalf("执行期间应至少触发一次 GC，GCRuns = %d", v.Stats().GCRuns)
	}
	out.Reset()
	if err := runOnVM(t, v, `print g();`); err != nil {
		t.Fatalf("回收后调用 g() 失败: %v", err)
	}
	if out.String() != "alive\n" {
		t.Fatalf("回收后 g() 输出 = %q, want %q", out.String(), "alive\n")
	}
}
