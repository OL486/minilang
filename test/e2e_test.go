// Package test 是端到端测试。
//
// 这里最重要的一条规则：每个用例都必须同时跑 tree 引擎和 VM 引擎，
// 并要求两边输出逐字节相同。
// 这条规则的价值不在「测试通过」，而在于它把「两个实现语义漂移」
// 变成了一个立刻会红的失败 —— 手写两次解释器，语义漂移是必然的，
// 唯一能控制它的办法是让它可检测。
package test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/OL486/minilang/internal/compiler"
	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/engine"
	"github.com/OL486/minilang/internal/lexer"
	"github.com/OL486/minilang/internal/parser"
	"github.com/OL486/minilang/internal/tree"
)

// runVM 走完整流水线：词法 → 语法 → 编译 → 执行。
func runVM(t *testing.T, src string) (string, error) {
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
	vm := engine.NewVM(&out)
	fn, compErrs := compiler.CompileProgram(vm, stmts)
	if len(compErrs) > 0 {
		return "", compErrs[0]
	}
	if _, err := vm.RunScript(fn); err != nil {
		return out.String(), err
	}
	return out.String(), nil
}

func runTree(t *testing.T, src string) (string, error) {
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
	in := tree.New(&out)
	if err := in.Run(stmts); err != nil {
		return out.String(), err
	}
	return out.String(), nil
}

type goldenCase struct {
	name string
	src  string
	want string
}

func TestBothEnginesAgree(t *testing.T) {
	cases := []goldenCase{
		{
			name: "算术优先级",
			src:  "print 1 + 2 * 3;\nprint (1 + 2) * 3;\nprint 7 % 3;\nprint 7 / 2;\nprint -3 + 1;\n",
			want: "7\n9\n1\n3.5\n-2\n",
		},
		{
			name: "字符串与真值",
			src:  "print \"a\" + \"b\";\nprint nil;\nprint true;\nprint 1 == 1.0;\nprint \"x\" == \"x\";\nprint 1 == \"1\";\n",
			want: "ab\nnil\ntrue\ntrue\ntrue\nfalse\n",
		},
		{
			name: "while 循环",
			src:  "let i = 0;\nwhile (i < 5) { print i; i = i + 1; }\n",
			want: "0\n1\n2\n3\n4\n",
		},
		{
			name: "if else if",
			src: `fn grade(n) {
  if (n >= 90) { return "A"; }
  else if (n >= 60) { return "B"; }
  else { return "C"; }
}
print grade(95); print grade(70); print grade(10);
`,
			want: "A\nB\nC\n",
		},
		{
			name: "闭包互不干扰",
			src: `fn makeCounter() {
  let count = 0;
  fn inc() { count = count + 1; return count; }
  return inc;
}
let c1 = makeCounter();
let c2 = makeCounter();
print c1(); print c1(); print c2(); print c1();
`,
			want: "1\n2\n1\n3\n",
		},
		{
			name: "三级嵌套闭包",
			src: `fn outer(a) {
  fn middle(b) {
    fn inner(c) { return a + b + c; }
    return inner;
  }
  return middle;
}
print outer(1)(2)(3);
`,
			want: "6\n",
		},
		{
			name: "块作用域遮蔽",
			src: `let x = 1;
{
  let x = 2;
  print x;
  {
    let x = 3;
    print x;
  }
  print x;
}
print x;
`,
			want: "2\n3\n2\n1\n",
		},
		{
			name: "短路求值",
			src: `fn side() { print "SIDE"; return true; }
print false && side();
print true || side();
print true && side();
print nil || "默认值";
print 1 && 2;
`,
			want: "false\ntrue\nSIDE\ntrue\n默认值\n2\n",
		},
		{
			name: "赋值表达式留值",
			src:  "let x = 0;\nlet y = 0;\nx = y = 7;\nprint x; print y;\nprint x = 9;\nprint x;\n",
			want: "7\n7\n9\n9\n",
		},
		{
			name: "递归",
			src: `fn fib(n) { if (n < 2) { return n; } return fib(n-1) + fib(n-2); }
print fib(15);
`,
			want: "610\n",
		},
		{
			name: "局部函数递归",
			src: `fn fact(n) {
  fn helper(n, acc) {
    if (n < 2) { return acc; }
    return helper(n - 1, acc * n);
  }
  return helper(n, 1);
}
print fact(6);
`,
			want: "720\n",
		},
		{
			name: "while 内 return",
			src: `fn firstOver(limit) {
  let i = 0;
  while (true) {
    if (i * i > limit) { return i; }
    i = i + 1;
  }
}
print firstOver(50);
`,
			want: "8\n",
		},
		{
			name: "转义字符",
			src:  "print \"a\\tb\";\nprint \"q\\\"q\";\nprint \"back\\\\slash\";\n",
			want: "a\tb\nq\"q\nback\\slash\n",
		},
		{
			name: "函数是值",
			src: `fn add(a, b) { return a + b; }
let f = add;
print f(2, 3);
fn apply(g, x) { return g(x); }
fn twice(n) { return n * 2; }
print apply(twice, 21);
print add;
`,
			want: "5\n42\n<fn add>\n",
		},
		{
			name: "注释不影响解析",
			src:  "// 一行注释\nlet x = 1; // 行尾注释\nprint x;\n",
			want: "1\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotVM, errVM := runVM(t, c.src)
			if errVM != nil {
				t.Fatalf("VM 引擎报错: %v", errVM)
			}
			gotTree, errTree := runTree(t, c.src)
			if errTree != nil {
				t.Fatalf("tree 引擎报错: %v", errTree)
			}
			if gotVM != gotTree {
				t.Fatalf("两个引擎输出不一致\nVM:   %q\ntree: %q", gotVM, gotTree)
			}
			if gotVM != c.want {
				t.Fatalf("输出不符\n实际: %q\n期望: %q", gotVM, c.want)
			}
		})
	}
}

// TestErrors 检查错误信息必须带位置。
// 这里断言的不是「报错了」，而是「报错里有行号」—— 后者才是本项目的要求。
func TestErrors(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantSub string
	}{
		{"未闭合字符串", "let x = \"未闭合\n", "字符串未闭合"},
		{"缺少分号", "let x = 1\nlet y = 2;\n", "分号"},
		{"缺少右括号", "print (1 + 2;\n", "右括号"},
		{"数字双小数点", "print 1.2.3;\n", "第二个小数点"},
		{"非法字符", "let x = 1 $ 2;\n", "无法识别的字符"},
		{"if 缺括号", "if true { print 1; }\n", "括号"},
		{"重复声明", "fn f() { let a = 1; let a = 2; }\n", "重复声明"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runVM(t, c.src)
			if err == nil {
				t.Fatalf("期望报错，但执行成功了")
			}
			msg := err.Error()
			if !strings.Contains(msg, c.wantSub) {
				t.Fatalf("错误信息缺少关键字 %q，实际: %s", c.wantSub, msg)
			}
			if !strings.Contains(msg, "行") {
				t.Fatalf("错误信息没有行号: %s", msg)
			}
		})
	}
}

// TestRuntimeErrors 检查两个引擎在同样的错误上给出同样的判断。
func TestRuntimeErrors(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantSub string
	}{
		{"类型不匹配的加法", "print 1 + \"a\";\n", "不支持"},
		{"未定义变量", "print nope;\n", "未定义的变量"},
		{"给未定义变量赋值", "nope = 1;\n", "未定义的变量"},
		{"调用非函数", "let x = 1; x();\n", "不是函数"},
		{"参数个数不符", "fn f(a) { return a; } f();\n", "需要 1 个参数"},
		{"一元负号作用于字符串", "print -\"a\";\n", "一元 -"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, errVM := runVM(t, c.src)
			if errVM == nil {
				t.Fatalf("VM 引擎期望报错，但成功了")
			}
			if !strings.Contains(errVM.Error(), c.wantSub) {
				t.Fatalf("VM 错误信息缺少 %q: %s", c.wantSub, errVM.Error())
			}
			_, errTree := runTree(t, c.src)
			if errTree == nil {
				t.Fatalf("tree 引擎期望报错，但成功了")
			}
			if !strings.Contains(errTree.Error(), c.wantSub) {
				t.Fatalf("tree 错误信息缺少 %q: %s", c.wantSub, errTree.Error())
			}
		})
	}
}

// TestGCRecoversGarbage 是 S9 的验收：大量创建临时对象后，
// 存活对象数必须回到很小的量级。
//
// 这条测试能抓到的最典型的 bug 是「GC 根集合漏了一项」：
// 漏掉栈的话，所有对象都能活下来，存活数会等于创建数。
func TestGCRecoversGarbage(t *testing.T) {
	src := `fn make(n) { fn get() { return n; } return get; }
let i = 0;
let last = nil;
while (i < 50000) { last = make(i); i = i + 1; }
print last();
`
	tokens, lexErrs := lexer.New(src).Scan()
	if len(lexErrs) > 0 {
		t.Fatalf("词法错误: %v", lexErrs[0])
	}
	stmts, parseErrs := parser.New(tokens).Parse()
	if len(parseErrs) > 0 {
		t.Fatalf("语法错误: %v", parseErrs[0])
	}
	var out bytes.Buffer
	vm := engine.NewVM(&out)
	fn, compErrs := compiler.CompileProgram(vm, stmts)
	if len(compErrs) > 0 {
		t.Fatalf("编译错误: %v", compErrs[0])
	}
	if _, err := vm.RunScript(fn); err != nil {
		t.Fatalf("执行错误: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "49999" {
		t.Fatalf("结果不对: %q", got)
	}
	// 先强制回收一次再统计。
	// 不能直接看运行结束时的存活数：GC 是「攒够阈值才跑」，
	// 最后一批分配之后可能还没到下一次阈值，
	// 那些对象里既有垃圾也有活对象，统计结果没有意义。
	vm.CollectGarbage()
	st := vm.Stats()
	if st.ObjectsTotal < 100000 {
		t.Fatalf("累计创建对象太少（%d），压力不够，测试没有意义", st.ObjectsTotal)
	}
	if st.Objects > 2000 {
		t.Fatalf("GC 后存活对象 %d 个（累计创建 %d），说明有对象没被回收，检查根集合",
			st.Objects, st.ObjectsTotal)
	}
	if st.GCRuns == 0 {
		t.Fatalf("一次 GC 都没触发，检查 nextGC 阈值")
	}
}

// TestUpvalueClosedAfterScope 验证离开作用域后 upvalue 被搬到堆上。
// 如果 closeUpvalues 没执行，栈槽被复用后闭包会读到别人的值。
func TestUpvalueClosedAfterScope(t *testing.T) {
	src := `fn make() {
  let a = 1;
  fn get() { return a; }
  let b = 99;
  return get;
}
let f = make();
print f();
`
	want := "1\n"
	gotVM, err := runVM(t, src)
	if err != nil {
		t.Fatalf("VM 报错: %v", err)
	}
	if gotVM != want {
		t.Fatalf("VM 输出 %q，期望 %q", gotVM, want)
	}
	gotTree, err := runTree(t, src)
	if err != nil {
		t.Fatalf("tree 报错: %v", err)
	}
	if gotTree != want {
		t.Fatalf("tree 输出 %q，期望 %q", gotTree, want)
	}
}

// TestDiagnosticRender 检查源码片段的渲染。
func TestDiagnosticRender(t *testing.T) {
	src := "let x = 1;\nlet y = ;\n"
	e := diag.New(diag.StageParse, 2, 9, "表达式不能以 ';' 开头")
	rendered := diag.Render(src, e)
	if !strings.Contains(rendered, "2 | let y = ;") {
		t.Fatalf("没有渲染出源码行: %s", rendered)
	}
	if !strings.Contains(rendered, "^") {
		t.Fatalf("没有渲染出位置指针: %s", rendered)
	}
}
