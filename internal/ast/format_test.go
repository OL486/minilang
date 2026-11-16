// Package ast_test 的 Format 测试：规范格式输出。
//
// 覆盖：全部语句与表达式类型、else-if 链、空体、括号保留、
// 数字归一化（1.0 → 1）、字符串重引号（中文原样保留）、
// 注释丢失（词法阶段已丢，fmt 不恢复）、幂等性。
//
// 用外部测试包：内部测试包 import parser 会与 ast 形成循环
// （parser import ast），而 Format 是导出 API，外部包足够测。
package ast_test

import (
	"testing"

	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/lexer"
	"github.com/OL486/minilang/internal/parser"
)

// parseForFormat 是测试专用的搭建：词法 → 语法，失败直接报测试错误。
func parseForFormat(t *testing.T, src string) []ast.Stmt {
	t.Helper()
	tokens, lexErrs := lexer.New(src).Scan()
	if len(lexErrs) > 0 {
		t.Fatalf("测试输入本身词法错误: %v", lexErrs[0])
	}
	stmts, parseErrs := parser.New(tokens).Parse()
	if len(parseErrs) > 0 {
		t.Fatalf("测试输入本身语法错误: %v", parseErrs[0])
	}
	return stmts
}

func TestFormatStatements(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "let 与 print",
			in:   "let    n=1+2*3;\nprint n;\n",
			want: "let n = 1 + 2 * 3;\nprint n;\n",
		},
		{
			name: "空程序",
			in:   "",
			want: "",
		},
		{
			name: "只有注释的程序是空程序",
			in:   "// 只有注释\n",
			want: "",
		},
		{
			name: "注释丢失",
			in:   "// 头注释\nlet x = 1; // 行尾注释\nprint x;\n",
			want: "let x = 1;\nprint x;\n",
		},
		{
			name: "if else-if else",
			in:   "if(a>=90){return \"A\";}else if(a>=60){return \"B\";}else{return \"C\";}",
			want: "if (a >= 90) {\n  return \"A\";\n} else if (a >= 60) {\n  return \"B\";\n} else {\n  return \"C\";\n}\n",
		},
		{
			name: "while 与嵌套块",
			in:   "while(i<5){print i;i=i+1;}",
			want: "while (i < 5) {\n  print i;\n  i = i + 1;\n}\n",
		},
		{
			name: "fn 与空体",
			in:   "fn f(){} fn g(a,b){return a+b;}",
			want: "fn f() {}\nfn g(a, b) {\n  return a + b;\n}\n",
		},
		{
			name: "闭包嵌套缩进",
			in:   "fn outer(a){fn inner(c){return a+c;}return inner;}",
			want: "fn outer(a) {\n  fn inner(c) {\n    return a + c;\n  }\n  return inner;\n}\n",
		},
		{
			name: "裸块",
			in:   "{let x=1;{print x;}}",
			want: "{\n  let x = 1;\n  {\n    print x;\n  }\n}\n",
		},
		{
			name: "空块",
			in:   "{}",
			want: "{}\n",
		},
		{
			name: "return 无值",
			in:   "fn f(){return;}",
			want: "fn f() {\n  return;\n}\n",
		},
		{
			name: "括号原样保留",
			in:   "print (1+2)*3; print a+(b+c); print ((x));",
			want: "print (1 + 2) * 3;\nprint a + (b + c);\nprint ((x));\n",
		},
		{
			name: "一元与逻辑",
			in:   "print -5+1; print !false; print false&&x||true;",
			want: "print -5 + 1;\nprint !false;\nprint false && x || true;\n",
		},
		{
			name: "调用与赋值",
			in:   "print f(2,3); x=x+1; print makeCounter();",
			want: "print f(2, 3);\nx = x + 1;\nprint makeCounter();\n",
		},
		{
			name: "字面量",
			in:   "print nil; print true; print 7%3; print 7/2;",
			want: "print nil;\nprint true;\nprint 7 % 3;\nprint 7 / 2;\n",
		},
		{
			name: "数字归一化",
			in:   "print 1.0; print 2.50; print 0.5;",
			want: "print 1;\nprint 2.5;\nprint 0.5;\n",
		},
		{
			name: "字符串重引号",
			in:   "print \"a\\nb\"; print \"0 是真值\";",
			want: "print \"a\\nb\";\nprint \"0 是真值\";\n",
		},
		{
			name: "单行块展开",
			in:   "fn fib(n){if(n<2){return n;}return fib(n-1)+fib(n-2);}",
			want: "fn fib(n) {\n  if (n < 2) {\n    return n;\n  }\n  return fib(n - 1) + fib(n - 2);\n}\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ast.Format(parseForFormat(t, c.in))
			if got != c.want {
				t.Fatalf("Format 输出不符：\n输入: %q\n实际:\n%s\n期望:\n%s", c.in, got, c.want)
			}
		})
	}
}

// TestFormatIdempotent：格式化结果再解析、再格式化，必须逐字节相同。
// 每个用例都跑两遍：第二遍的输入是第一遍的输出。
func TestFormatIdempotent(t *testing.T) {
	inputs := []string{
		"let n = 1 + 2 * 3 - 4 / 2 % 3;\nprint (1 + 2) * 3;\n",
		"fn grade(score) {\n  if (score >= 90) {\n    return \"A\";\n  } else if (score >= 60) {\n    return \"B\";\n  } else {\n    return \"C\";\n  }\n}\n",
		"fn f() {}\nwhile (true) {\n  if (n * n > limit) {\n    return n;\n  }\n  n = n + 1;\n}\n",
		"print \"中文\"; print 1.0; print -5; let x = a && b || c == d;\n",
		"{\n  let x = \"middle\";\n  {\n    print x;\n  }\n}\n",
	}
	for i, in := range inputs {
		once := ast.Format(parseForFormat(t, in))
		twice := ast.Format(parseForFormat(t, once))
		if once != twice {
			t.Fatalf("用例 %d 不幂等：\n一次:\n%s\n两次:\n%s", i, once, twice)
		}
	}
}
