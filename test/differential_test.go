// 差分 fuzz：用固定种子生成 N 个随机程序，双引擎逐字节对比。
//
// 这是 TestBothEnginesAgree（15 个手写黄金用例）的补充：手写用例只能覆盖
// 想得到的组合，闭包 × 遮蔽 × 短路 × 赋值留值 × 递归的交叉组合要靠
// 随机生成。生成器见 differential_gen.go。
//
// 对比规则：
//   - 解析失败的程序跳过（生成器 bug 的信号，见最后的解析成功率门禁）；
//   - 两个引擎都报错算通过（只要求“都报错”，不比错误文本——
//     VM 的运行时错误没有列号，tree 的有，文本本就不一致）；
//   - 一个报错、另一个成功，或输出逐字节不同，算失败，
//     失败信息里打印完整程序原文（凭种子与序号可复现，见下）。
//
// 可复现性：程序 i 用种子 differentialSeed+i 生成，失败信息里会给出
// 序号；也可以在测试里临时把 differentialPrograms 改成 1、
// 把循环起点改成该序号，单独重跑那一个程序。
package test

import (
	"testing"

	"github.com/OL486/minilang/internal/lexer"
	"github.com/OL486/minilang/internal/parser"
)

// differentialSeed 是差分测试的基准种子，程序 i 用 seed+i 生成。
const differentialSeed = 20261116

// differentialPrograms 是每轮生成的程序数。300 个程序在
// 本机跑完约 1 秒以内；想加强覆盖可以调大，但别让 `go test` 变慢。
const differentialPrograms = 300

func TestDifferentialFuzz(t *testing.T) {
	var parsed, bothErr, compared int
	for i := 0; i < differentialPrograms; i++ {
		src := newDiffGen(differentialSeed + int64(i)).program()

		// 先不借助 t.Fatalf 的 parseSource，手工走一遍词法 + 语法，
		// 以便统计解析成功率（生成器质量的门禁）。
		tokens, lexErrs := lexer.New(src).Scan()
		if len(lexErrs) > 0 {
			t.Logf("程序 #%d 词法失败（生成器问题）: %v\n程序:\n%s", i, lexErrs[0], src)
			continue
		}
		if _, parseErrs := parser.New(tokens).Parse(); len(parseErrs) > 0 {
			t.Logf("程序 #%d 解析失败（生成器问题）: %v\n程序:\n%s", i, parseErrs[0], src)
			continue
		}
		parsed++

		gotVM, errVM := runVM(t, src)
		gotTree, errTree := runTree(t, src)
		if (errVM == nil) != (errTree == nil) {
			t.Fatalf("程序 #%d：两个引擎报错不一致\nVM err: %v\ntree err: %v\n程序:\n%s",
				i, errVM, errTree, src)
		}
		if errVM != nil {
			bothErr++
			continue
		}
		compared++
		if gotVM != gotTree {
			t.Fatalf("程序 #%d：两个引擎输出不一致\nVM:   %q\ntree: %q\n程序:\n%s",
				i, gotVM, gotTree, src)
		}
	}

	// 生成器质量门禁：解析成功率低于 90% 说明生成器本身有 bug，
	// 而不是引擎有问题，这时测试应当红，让人先修生成器。
	if parsed*10 < differentialPrograms*9 {
		t.Fatalf("解析成功率过低：%d/%d，生成器可能有 bug，先修生成器再看引擎",
			parsed, differentialPrograms)
	}
	t.Logf("差分测试：%d 个程序，解析成功 %d，双引擎均报错 %d，输出对比 %d，全部一致",
		differentialPrograms, parsed, bothErr, compared)
}
