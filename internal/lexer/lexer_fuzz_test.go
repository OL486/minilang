// lexer 的 fuzz 测试：专抓扫描器在任意输入下的 panic/crash。
//
// 词法器是流水线的第一站，任何输入（包括随机字节、未闭合字符串、
// 非法 UTF-8）都必须被它干净地处理成 token 流或词法错误，绝不能 panic。
// fuzz 运行时会把变异输入写进 testdata/fuzz/FuzzLexer/ 作为语料库，
// 平时 `go test` 只跑下面的种子语料，不会启动 fuzz 循环。
package lexer

import (
	"testing"

	"github.com/OL486/minilang/internal/token"
)

// FuzzLexer 对任意字节输入调用扫描器，并断言两条不变量：
//  1. 不 panic（fuzz 框架会自动把 panic 记为失败）；
//  2. 返回的 token 流始终以 EOF 结尾。
func FuzzLexer(f *testing.F) {
	seeds := []string{
		// 正常程序片段。
		`let x = 1; print x;`,
		`fn add(a, b) { return a + b; } print add(2, 3);`,
		`if (x >= 10) { print "大"; } else { print "小"; }`,
		`while (i < 5) { i = i + 1; }`,
		`print "a\tb\nc"; print 3.14; print true; print nil;`,
		`// 只有注释`,
		``,
		// 边界输入：未闭合字符串、非法转义、数字双小数点。
		`let x = "未闭合`,
		`print "bad\qescape";`,
		`print 1.2.3;`,
		`let x = 1 $ 2;`,
		// 特殊字符与非 ASCII：中文注释、emoji、控制字符。
		`// 中文注释 print 1;`,
		`print "你好世界";`,
		"print \"\x00\x01\x02\";",
		// 畸形 token 组合。
		`;;;;`,
		`let let let;`,
		`fn fn() {}`,
		`print (((((1)))));`,
		"let x = 1;\r\nprint x;\r\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		tokens, _ := New(string(data)).Scan()
		if len(tokens) == 0 {
			t.Fatalf("Scan(%q) 返回了空 token 流", data)
		}
		if last := tokens[len(tokens)-1]; last.Type != token.EOF {
			t.Fatalf("Scan(%q) 的 token 流没有以 EOF 结尾，最后一个是 %v", data, last.Type)
		}
	})
}
