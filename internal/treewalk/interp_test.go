// Package treewalk 的直接测试。
//
// treewalk 是 VM 的语义参照物：两个引擎输出一致的约束由 test/e2e_test.go
// 在流水线层面保证，这里补的是解释器本身的直接覆盖 —— 求值、作用域、
// 闭包、错误路径。以前这个包没有任何测试文件，覆盖率 0%。
package treewalk

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/lexer"
	"github.com/OL486/minilang/internal/parser"
	"github.com/OL486/minilang/internal/semantics"
)

// runSrc 走「词法 → 语法 → treewalk 执行」完整路径，返回 print 的输出。
func runSrc(t *testing.T, src string) (string, error) {
	t.Helper()
	tokens, lexErrs := lexer.New(src).Scan()
	if len(lexErrs) > 0 {
		return "", lexErrs[0]
	}
	stmts, parseErrs := parser.New(tokens).Parse()
	if len(parseErrs) > 0 {
		return "", parseErrs[0]
	}
	var out bytes.Buffer
	if err := New(&out).Run(stmts); err != nil {
		return out.String(), err
	}
	return out.String(), nil
}

func mustRun(t *testing.T, src string) string {
	t.Helper()
	out, err := runSrc(t, src)
	if err != nil {
		t.Fatalf("执行 %q 应成功，实际 err = %v", src, err)
	}
	return out
}

func mustErr(t *testing.T, src, want string) {
	t.Helper()
	_, err := runSrc(t, src)
	if err == nil {
		t.Fatalf("执行 %q 应报错（含 %q），实际成功", src, want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("执行 %q 报错应含 %q，实际 err = %v", src, want, err)
	}
}

// ---------- 字面量与运算符 ----------

func TestInterpPrintLiterals(t *testing.T) {
	out := mustRun(t, `
print 42;
print 3.5;
print "hello";
print true;
print false;
print nil;
print 1 + 2 * 3;
print (1 + 2) * 3;
print -5 + 1;
print !false;
print 7 % 3;
print 7 / 2;
print 1 == 1.0;
print 1 == "1";
print "abc" < "abd";
`)
	want := "42\n3.5\nhello\ntrue\nfalse\nnil\n7\n9\n-4\ntrue\n1\n3.5\ntrue\nfalse\ntrue\n"
	if out != want {
		t.Fatalf("字面量与运算符输出 = %q, want %q", out, want)
	}
}

func TestInterpTruthiness(t *testing.T) {
	// 只有 false 和 nil 是假；0 和空串都是真。
	out := mustRun(t, `
if (0) { print "0 是真值"; }
if ("") { print "空串也是真值"; }
if (false) { print "不该出现"; } else { print "false 是假值"; }
if (nil) { print "不该出现"; } else { print "nil 是假值"; }
`)
	want := "0 是真值\n空串也是真值\nfalse 是假值\nnil 是假值\n"
	if out != want {
		t.Fatalf("真值输出 = %q, want %q", out, want)
	}
}

func TestInterpShortCircuit(t *testing.T) {
	// 短路：右边不会被求值，所以不会报「未定义变量」。
	// 逻辑运算返回操作数本身而不是 true/false。
	out := mustRun(t, `
print false && undefined_thing;
print true || undefined_thing;
print nil || "x";
print 1 && 2;
`)
	want := "false\ntrue\nx\n2\n"
	if out != want {
		t.Fatalf("短路求值输出 = %q, want %q", out, want)
	}
}

// ---------- 变量与作用域 ----------

func TestInterpVariables(t *testing.T) {
	out := mustRun(t, `
let x = 1;
print x;
x = x + 10;
print x;
let y = x * 2;
print y;
`)
	if out != "1\n11\n22\n" {
		t.Fatalf("变量输出 = %q, want %q", out, "1\n11\n22\n")
	}
}

func TestInterpBlockShadowing(t *testing.T) {
	// 块引入新作用域，内层同名变量遮蔽外层，退出块后外层不受影响。
	out := mustRun(t, `
let x = "outer";
{
  let x = "middle";
  print x;
  {
    let x = "inner";
    print x;
  }
  print x;
}
print x;
`)
	if out != "middle\ninner\nmiddle\nouter\n" {
		t.Fatalf("遮蔽输出 = %q, want %q", out, "middle\ninner\nmiddle\nouter\n")
	}
}

func TestInterpAssignFindsNearest(t *testing.T) {
	// 赋值改的是最近的那层声明，不是全局。
	out := mustRun(t, `
let x = 1;
{
  let x = 2;
  x = 3;
  print x;
}
print x;
`)
	if out != "3\n1\n" {
		t.Fatalf("就近赋值输出 = %q, want %q", out, "3\n1\n")
	}
}

func TestInterpUndefinedVariable(t *testing.T) {
	mustErr(t, `print y;`, "未定义的变量 'y'")
}

func TestInterpAssignUndefined(t *testing.T) {
	// minilang 没有隐式全局变量。
	mustErr(t, `x = 1;`, "不能给未定义的变量 'x' 赋值")
}

// ---------- 控制流 ----------

func TestInterpControlFlow(t *testing.T) {
	out := mustRun(t, `
let i = 0;
let sum = 0;
while (i < 5) {
  sum = sum + i;
  i = i + 1;
}
print sum;

fn grade(score) {
  if (score >= 90) {
    return "A";
  } else if (score >= 60) {
    return "B";
  } else {
    return "C";
  }
}
print grade(95);
print grade(72);
print grade(30);

fn firstSquareOver(limit) {
  let n = 0;
  while (true) {
    if (n * n > limit) {
      return n;
    }
    n = n + 1;
  }
}
print firstSquareOver(50);
`)
	if out != "10\nA\nB\nC\n8\n" {
		t.Fatalf("控制流输出 = %q, want %q", out, "10\nA\nB\nC\n8\n")
	}
}

func TestInterpWhileCondError(t *testing.T) {
	mustErr(t, `while (undefined) { }`, "未定义的变量")
}

// ---------- 函数 ----------

func TestInterpFunctions(t *testing.T) {
	out := mustRun(t, `
fn fib(n) {
  if (n < 2) { return n; }
  return fib(n - 1) + fib(n - 2);
}
print fib(10);
print fib(20);

fn fact(n) {
  fn helper(n, acc) {
    if (n < 2) { return acc; }
    return helper(n - 1, acc * n);
  }
  return helper(n, 1);
}
print fact(10);

// 没有 return 的函数返回 nil
fn noop() { }
print noop();

// 函数是一等公民：存进变量、当参数传
fn add(a, b) { return a + b; }
let f = add;
print f(2, 3);
fn apply(g, x) { return g(x); }
fn twice(n) { return n * 2; }
print apply(twice, 21);

// 打印函数值
print add;
print clock;
`)
	want := "55\n6765\n3628800\nnil\n5\n42\n<fn add>\n<native fn clock>\n"
	if out != want {
		t.Fatalf("函数输出 = %q, want %q", out, want)
	}
}

func TestInterpArityMismatch(t *testing.T) {
	mustErr(t, `fn f(a) { return a; } print f(1, 2);`, "函数 f 需要 1 个参数，传入了 2 个")
	mustErr(t, `fn f(a, b) { return a; } print f(1);`, "函数 f 需要 2 个参数，传入了 1 个")
}

func TestInterpCallNonFunction(t *testing.T) {
	mustErr(t, `let x = 1; print x();`, "不是函数，不能调用")
	mustErr(t, `print "s"();`, "不是函数，不能调用")
}

func TestInterpReturnAtTopLevel(t *testing.T) {
	mustErr(t, `return 1;`, "return 只能在函数内部使用")
}

func TestInterpCallDepthOverflow(t *testing.T) {
	mustErr(t, `
fn f(n) { return f(n + 1); }
print f(0);
`, "调用栈溢出")
}

// ---------- 闭包 ----------

func TestInterpClosures(t *testing.T) {
	// 两个计数器必须互不干扰；闭包捕获的是变量而不是值。
	out := mustRun(t, `
fn makeCounter() {
  let count = 0;
  fn inc() {
    count = count + 1;
    return count;
  }
  return inc;
}
let c1 = makeCounter();
let c2 = makeCounter();
print c1();
print c1();
print c2();
print c1();

// 共享同一个捕获变量：一个改了另一个能看见
fn makePair() {
  let v = 0;
  fn bump() { v = v + 10; return v; }
  fn read() { return v; }
  return bump;
}
let bump = makePair();
print bump();
print bump();

// 三级嵌套：最内层同时捕获三层外的变量
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
	want := "1\n2\n1\n3\n10\n20\n6\n"
	if out != want {
		t.Fatalf("闭包输出 = %q, want %q", out, want)
	}
}

func TestInterpClosureCapturesVariableNotValue(t *testing.T) {
	// 闭包返回后，外层变量的后续修改对闭包可见 ——
	// 捕获的是变量本身，不是定义那一刻的值。
	out := mustRun(t, `
let x = 1;
fn get() { return x; }
x = 2;
print get();
`)
	if out != "2\n" {
		t.Fatalf("捕获变量输出 = %q, want %q", out, "2\n")
	}
}

// ---------- 内置 clock ----------

func TestInterpClock(t *testing.T) {
	// clock() 返回毫秒时间戳，是 number 类型。
	out := mustRun(t, `
let t1 = clock();
let t2 = clock();
print t2 >= t1;
print t1 > 0;
`)
	if out != "true\ntrue\n" {
		t.Fatalf("clock 输出 = %q, want %q", out, "true\ntrue\n")
	}
}

func TestInterpClockArity(t *testing.T) {
	mustErr(t, `print clock(1);`, "内置函数 clock 需要 0 个参数")
}

func TestInterpClockCanBeShadowed(t *testing.T) {
	// spec 5.9：clock 是全局变量，可以被用户覆盖（允许，不保护）。
	out := mustRun(t, `
let clock = 1;
print clock;
`)
	if out != "1\n" {
		t.Fatalf("覆盖 clock 输出 = %q, want %q", out, "1\n")
	}
}

// ---------- 运行时错误 ----------

func TestInterpUnaryMinusOnString(t *testing.T) {
	mustErr(t, `print -"a";`, "一元 - 只能作用于 number")
}

func TestInterpBinaryTypeMismatch(t *testing.T) {
	mustErr(t, `print "a" - 1;`, "不支持 string 与 number")
	mustErr(t, `print 1 < "a";`, "不支持 number 与 string")
}

func TestInterpGlobals(t *testing.T) {
	// New 注册 clock；Globals 暴露全局作用域，供 REPL 与测试使用。
	in := New(io.Discard)
	v, ok := in.Globals().Lookup("clock")
	if !ok {
		t.Fatalf("全局作用域应注册 clock")
	}
	nf, ok := v.(*NativeFn)
	if !ok || nf.Name != "clock" || nf.Arity != 0 {
		t.Fatalf("clock 应是 0 参数内置函数，实际 %#v", v)
	}
	// 同一个解释器两次 Run 共享全局变量。
	if _, err := runSrcOn(t, in, `let shared = 7;`); err != nil {
		t.Fatalf("第一次 Run 应成功，实际 err = %v", err)
	}
	v, ok = in.Globals().Lookup("shared")
	if !ok || v != float64(7) {
		t.Fatalf("第二次 Run 应能看到第一次定义的 shared，实际 ok=%v v=%v", ok, v)
	}
}

// runSrcOn 在已有的解释器上执行源码（复用其全局作用域）。
func runSrcOn(t *testing.T, in *Interp, src string) (string, error) {
	t.Helper()
	tokens, lexErrs := lexer.New(src).Scan()
	if len(lexErrs) > 0 {
		return "", lexErrs[0]
	}
	stmts, parseErrs := parser.New(tokens).Parse()
	if len(parseErrs) > 0 {
		return "", parseErrs[0]
	}
	var out bytes.Buffer
	// 注意：Run 写的是解释器创建时绑定的 out，这里只关心 err。
	if err := in.Run(stmts); err != nil {
		return out.String(), err
	}
	return out.String(), nil
}

// ---------- Env 直接测试 ----------

func TestEnvLookupAndAssign(t *testing.T) {
	root := NewEnv(nil)
	root.Define("a", float64(1))
	child := NewEnv(root)
	child.Define("b", float64(2))

	// 子作用域能沿链找到父级的变量。
	if v, ok := child.Lookup("a"); !ok || v != float64(1) {
		t.Fatalf("子作用域 Lookup(a) = %v, %v, want 1, true", v, ok)
	}
	// 父级看不到子级的变量。
	if _, ok := root.Lookup("b"); ok {
		t.Fatalf("父作用域不应看到子级的 b")
	}
	// 查不到返回 false。
	if _, ok := child.Lookup("missing"); ok {
		t.Fatalf("Lookup(missing) 应返回 false")
	}
	// Assign 改最近的那层声明。
	if !child.Assign("a", float64(9)) {
		t.Fatalf("Assign 已声明的 a 应返回 true")
	}
	if v, _ := root.Lookup("a"); v != float64(9) {
		t.Fatalf("Assign 后父级 a = %v, want 9", v)
	}
	// Assign 未声明的变量返回 false，不会隐式创建。
	if child.Assign("nope", float64(0)) {
		t.Fatalf("Assign 未声明的变量应返回 false")
	}
	if _, ok := root.Lookup("nope"); ok {
		t.Fatalf("Assign 不应隐式创建全局变量")
	}
	// 同作用域重复 Define 是覆盖（REPL 里重新 let 同名变量是正常操作）。
	root.Define("a", float64(5))
	if v, _ := root.Lookup("a"); v != float64(5) {
		t.Fatalf("重复 Define 应覆盖，实际 a = %v", v)
	}
}

// ---------- FormatValue ----------

func TestFormatValue(t *testing.T) {
	if got := FormatValue(nil); got != "nil" {
		t.Fatalf("FormatValue(nil) = %q, want nil", got)
	}
	if got := FormatValue(true); got != "true" {
		t.Fatalf("FormatValue(true) = %q, want true", got)
	}
	if got := FormatValue(float64(42)); got != "42" {
		t.Fatalf("FormatValue(42) = %q, want 42", got)
	}
	if got := FormatValue("s"); got != "s" {
		t.Fatalf("FormatValue(s) = %q, want s", got)
	}
	if got := FormatValue(123); got != "<unknown>" {
		t.Fatalf("FormatValue(123 int) = %q, want <unknown>", got)
	}
}

// fib30Source 与 cmd/minilang/bench.go 的 benchSource 是同一份程序，
// 是仓库里最重的正常程序：它的步数决定了预算下限。
const fib30Source = `fn fib(n) {
  if (n < 2) { return n; }
  return fib(n-1) + fib(n-2);
}
let benchResult = fib(30);
`

func parseOnly(t *testing.T, src string) []ast.Stmt {
	t.Helper()
	tokens, lexErrs := lexer.New(src).Scan()
	if len(lexErrs) > 0 {
		t.Fatalf("lex: %v", lexErrs[0])
	}
	stmts, parseErrs := parser.New(tokens).Parse()
	if len(parseErrs) > 0 {
		t.Fatalf("parse: %v", parseErrs[0])
	}
	return stmts
}

// runTree 在全新解释器上解析并执行一段源码。
func runTree(t *testing.T, src string) (*Interp, error) {
	t.Helper()
	var out bytes.Buffer
	in := New(&out)
	return in, in.Run(parseOnly(t, src))
}

func TestExecBudgetInfiniteLoop(t *testing.T) {
	start := time.Now()
	_, err := runTree(t, "while (true) { }")
	if err == nil {
		t.Fatal("while (true) {} 没有报错，预算没有生效")
	}
	if !strings.Contains(err.Error(), "执行步数超过") {
		t.Fatalf("死循环报错不是预算错误：%v", err)
	}
	// treewalk 每步都是一次表达式求值，比 VM 指令慢一个量级：
	// 本机实测约 10–15 秒，-race 下约 50 秒。300 秒的上限只证明
	// 「会停下而不是 hang」，再加共享虚拟机波动，上限要留足余量。
	if d := time.Since(start); d > 300*time.Second {
		t.Fatalf("死循环 %v 才报错，太慢（本机实测约 10–15 秒，-race 下约 50 秒）", d)
	}
}

func TestExecBudgetNormalProgramUnaffected(t *testing.T) {
	in, err := runTree(t, fib30Source)
	if err != nil {
		t.Fatalf("fib(30) 不应撞上限：%v", err)
	}
	if in.Steps() >= semantics.MaxExecSteps {
		t.Fatalf("fib(30) 步数 %d 已达上限 %d", in.Steps(), semantics.MaxExecSteps)
	}
	t.Logf("fib(30) treewalk 步数 %d，上限 %d", in.Steps(), semantics.MaxExecSteps)
}

func TestExecBudgetResetsBetweenRuns(t *testing.T) {
	// 预算按次清零：同一个解释器上一次死循环撞限后，
	// 下一个正常程序仍能跑；REPL 复用同一个解释器，全靠这一点。
	var out bytes.Buffer
	in := New(&out)
	if err := in.Run(parseOnly(t, "while (true) { }")); err == nil {
		t.Fatal("死循环没有报错")
	}
	if err := in.Run(parseOnly(t, "print 1 + 2;")); err != nil {
		t.Fatalf("撞限后正常程序跑不起来：%v", err)
	}
	if in.Steps() >= semantics.MaxExecSteps {
		t.Fatalf("小段程序步数 %d 异常", in.Steps())
	}
}
