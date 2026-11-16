// parser 的 fuzz 测试：专抓解析器在任意 token 流下的 panic/crash。
//
// 解析器是递归下降 + Pratt，输入里的括号、块、调用都可以任意嵌套。
// fuzz 目标是「解析器永不 panic」——任何输入要么解析成功，要么返回
// 带位置的语法错误。注意这里不直接断言错误内容，只断言不崩溃。
//
// 已知限制：main 分支的 parser 没有嵌套深度上限，200 万层括号会触发
// Go 运行时 fatal stack overflow（不是可恢复的 panic，fuzz 框架也
// 救不了）。该问题由 PR #18（parser 嵌套深度上限 1000）修复，
// 在它合进来之前，fuzz 目标先跳过括号嵌套超过 200 层的输入，
// 只测深度上限以内的解析健壮性；#18 合并后可放宽这个 guard。
// 平时 `go test` 只跑下面的种子语料，不会启动 fuzz 循环。
package parser

import (
	"testing"

	"github.com/OL486/minilang/internal/lexer"
)

// maxFuzzNestingDepth 是 fuzz 目标接受的括号嵌套上限。
// 超过它的一律跳过，见文件头注释（PR #18 未合并前的已知 crash）。
const maxFuzzNestingDepth = 200

// maxFuzzInputBytes 是 fuzz 目标接受的输入长度上限，
// 避免超大输入拖慢 fuzz 循环；长输入由 e2e 示例覆盖。
const maxFuzzInputBytes = 8192

// nestingDepth 返回 src 里圆括号与花括号的最大嵌套深度。
// 只做词法层面的粗略计数（字符串与注释里的括号也会被计入，
// 这只会让 guard 更保守，不会漏掉真正的深嵌套）。
func nestingDepth(src string) int {
	depth, max := 0, 0
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '(', '{':
			depth++
			if depth > max {
				max = depth
			}
		case ')', '}':
			depth--
			if depth < 0 {
				depth = 0
			}
		}
	}
	return max
}

// FuzzParser 对任意输入先词法、再语法，断言解析器不 panic。
// 即使词法已经报错，也把拿到的 token 流喂给解析器：
// 词法错误只应影响错误列表，不应让解析器在残缺的 token 流上崩溃。
func FuzzParser(f *testing.F) {
	seeds := []string{
		// 每种语句与表达式各一段。
		`let x = 1; print x;`,
		`fn add(a, b) { return a + b; } print add(2, 3);`,
		`if (x > 1) { print 1; } else if (x > 0) { print 0; } else { print -1; }`,
		`while (i < 3) { i = i + 1; }`,
		`return 1;`,
		`print 1 + 2 * 3 == 7 && true || false;`,
		`print -x; print !flag; print f(1, 2)(3);`,
		`let x = 0; x = y = 7; print x = 9;`,
		`{ let x = 1; { let x = 2; print x; } print x; }`,
		`fn outer(a) { fn inner(b) { return a + b; } return inner; } print outer(1)(2);`,
		`print nil || "默认值"; print 1 && 2;`,
		`print "a" + "b"; print "x" == "x"; print 1 == "1";`,
		// 错误输入：缺分号、缺括号、表达式开头非法、空程序。
		`let x = 1`,
		`print (1 + 2;`,
		`let y = ;`,
		`if true { print 1; }`,
		``,
		`// 只有注释`,
		`;;;;`,
		// 中等嵌套：50 层括号（在 guard 内，练递归路径）。
		`print ((((((((((((((((((((((((((((((((((((((((((((((((((1))))))))))))))))))))))))))))))))))))))))))))))))));`,
		// 字符串与注释里的括号不应干扰解析。
		`print "(((("; // )))){{
`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// 注意：这里用 return 而不用 t.Skip —— fuzz 目标函数里
		// t.Skip 的行为与普通测试不同，直接 return 即表示
		// “该输入不在本目标的测试范围”，框架会继续下一个输入。
		if len(data) > maxFuzzInputBytes {
			return
		}
		if nestingDepth(string(data)) > maxFuzzNestingDepth {
			return
		}
		tokens, _ := lexer.New(string(data)).Scan()
		_, _ = New(tokens).Parse()
	})
}
