# Contributing

欢迎给 minilang 提 PR。先读这份指南：它只写本仓库真正执行的规则，
不写放之四海皆准的空话。

## 开发环境

- Go 1.22+（`go.mod` 为准）。不需要 `make` 之外的任何工具，
  Windows 上没有 `make` 时见 README 的等价命令。
- 零外部依赖，只用 Go 标准库。不要引入第三方库。

## 提交前必过的一道关

```bash
make check      # gofmt 检查 + go vet + go test，全部要绿
make examples   # 跑全部示例，并校验两个引擎输出逐字节一致
```

`make check` 是提交 PR 的门槛，不是建议。CI 的 check job 会再跑一次，
本地先绿了再推，省得来回。

## 双引擎一致性：这条规则高于一切

同一份源码、同一份 AST，可以走 treewalk 解释器，也可以编译成字节码跑 VM。
**两个引擎的输出必须逐字节相同**，这条约束由 `test/e2e_test.go` 强制。

- 改语义时，两边一起改，只改一边就是 bug。
- 定位语义漂移：`diff <(minilang run x.ml) <(minilang run x.ml --engine=tree)`。
- treewalk 是 VM 的语义参照物，不是备胎；不要以“VM 更快”为由放宽它。
- 新增语言行为（如果真的要加）必须补差分测试：同一份程序在两个引擎上跑，
  输出、退出码、报错文本都要一致。

## 资源上限：改数字先改文档

调用深度、嵌套深度、常量池、局部变量、upvalue 等上限集中在
`internal/semantics`（如 `MaxCallDepth = 1000`、`MaxNestingDepth = 1000`），
双引擎共用。改任何一个上限：

1. 先改 `docs/00-spec.md` §6 的限额表（规范是实现的唯一依据，
   代码与文档冲突时先改文档再改代码）；
2. 再改代码与测试；
3. 顺手更新 CHANGELOG。

## 文档同步

- 改行为必改文档：`docs/00-spec.md`（语义）、`docs/01-pipeline.md`（流水线）、
  `docs/02-pitfalls.md`（踩到的新坑）、`docs/03-benchmark.md`（性能数字）。
- 基准数字不要手写：用 `minilang bench` 实测后按 `docs/03-benchmark.md`
  注明的机器与口径（次数、预热、取中位数）写进去。
- 不要在文档里写精确的 Go 源码行数：每合一个 PR 就过期一次，
  行数只活在 `bench` 输出里（`countLOC`）。
- 有用户可见的改动就更新 `CHANGELOG.md`（只记已合进 `main` 的内容，
  在途 PR 记在「在途」小节并写清未合）。**合併 PR 後立刻把該行從「在途」
  上移到對應分組，別攢到發版前再補**——Unreleased 必須隨時和 `main` 一致，
  本文件曾因只記「在途」不記已合而整段失真。

## 代码风格

- `gofmt` 全过：`make check` 里的 `fmt-check` 会拦，没过就 `go fmt ./...`。
- 注释写“为什么”，不写“是什么”：`docs/02-pitfalls.md` 里的五个陷阱，
  代码里都有长注释讲清前因后果，新踩的坑照这个标准写。
- 保持小文件：`vm.go` / `compiler.go` 这种 600 行大文件已经被拆过，
  新文件超过 300 行先想想能不能拆。

## PR 流程

1. 先开 issue 说清要改什么（bug 贴最小复现，见 Issue 模板），
   大改动先在 issue 里对齐方案再动手。
2. 一个 PR 只做一件事；分支名用 `fix/...`、`test/...`、`docs/...`、
   `refactor/...`、`ci/...` 开头。
3. PR 描述写清：改了什么、为什么、怎么验证的（贴 `make check` 与
   `make examples` 的结果）。模板里有检查清单，逐项打勾。
4. 合并由维护者决定；CI 红了先在本地复现修好再推。

## Issue 流程

- Bug：用 bug 报告模板，贴最小可复现的 `.ml`、跑的命令、用的引擎
  （`--engine=tree` 还是默认 VM）、期望与实际输出。
- 功能请求：先想想它是不是语言特性——spec 已冻结类、数组、哈希表、
  lambda、`break` / `continue`，这类提案大概率不会接受；
  工具链、文档、测试类的提案欢迎。
