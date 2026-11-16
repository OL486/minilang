# Changelog

本文件记录 minilang 每个版本值得注意的改动，格式遵循
[Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

规则：**只有已合进 `main` 的改动才能记进 Unreleased / 版本小节**。
在途的 PR 先记在下面的「在途」小节并写清未合，等合并后再上移。
（notes-mcp 曾把未合的 `list_notes` 写进 Unreleased 造成误报，本仓库不要重蹈覆辙。）

## [Unreleased]

### 在途（尚未合进 main，合并后再记入上面）

- 文档漂移收敛、treewalk `interp.go` 拆分、`chunk.go` 反汇编拆分
  （PR #28，Closes #15；`internal/treewalk/format.go` 的 `ast` import 已补）。
- 执行步数预算：VM 指令计数 + treewalk 步数计数，死循环撞限报运行时错误，
  不再只能 Ctrl-C 中断；`docs/00-spec.md` §6 限额表加一行（PR #30，Closes #29）。
- gofmt 漂移修复：4 个文件 import 排序 + `fmt-check` 在 gofmt 不在 PATH 时报错
  （PR #32，Closes #31）。
- 诊断渲染截断长行：2M 字符单行报错从约 4 MB 降到 251 字节
  （PR #34，Closes #33）。

## [0.1.0] - 2026-10-27

首个版本。2026-10-26 首个提交即为 0.1.0 的完整实现；
tag `v0.1.0` 指向 2026-10-27 的提交，本文件补记至今已合的全部内容。

### Added

- 完整语言实现：词法分析 → Pratt 解析 → tree-walking 解释器 → 字节码编译器 →
  栈式虚拟机 → 标记-清除 GC。5 种类型（number / string / bool / nil / function），
  无用户自定义类型、无数组、无哈希表（刻意取舍，见 `docs/00-spec.md`）。
- 闭包与 upvalue：`fn` 定义函数，`makeCounter` 两个计数器互不干扰；
  局部变量、常量池、upvalue 单函数上限各 256，调用深度上限 1000（双引擎共用
  `semantics.MaxCallDepth`），语法嵌套深度上限 1000（`semantics.MaxNestingDepth`，
  PR #18，Closes #17）。
- 双引擎：同一份 AST 可走 treewalk 或编译成字节码跑 VM，
  `test/e2e_test.go` 强制两个引擎输出逐字节相同；`--engine=tree` / `--engine=vm` 切换。
- CLI：`run` / `repl` / `bench` / `version` / `help`；三个调试开关
  `--dump-tokens` / `--dump-ast` / `--dump-bytecode` 可任意组合；
  `--stats`（对象堆与 GC 统计）、`--gc-off`（关闭 GC 作对照组）；
  REPL 命令 `:help` `:quit` `:gc` `:stats`。
- 示例：`examples/` 5 个（基础、控制流、闭包、递归、GC 压力）与
  `examples/errors/` 5 个错误示例。
- 文档：`docs/00-spec.md`（语言规范，实现的唯一依据）、`docs/01-pipeline.md`、
  `docs/02-pitfalls.md`（5 个真实踩到的陷阱）、`docs/03-benchmark.md`、
  `docs/04-compiler-vm-decoupling.md`；README 含阶段 → 文件映射（S0–S10）。
- 发布打包：`tools/pack/main.go` 纯 Go 实现，交叉编译 6 个平台
  （windows / linux / darwin × amd64 / arm64），打 tag 即出 Release；
  `make release` 与 CI 用同一份打包逻辑（PR #24，Closes #23）。
- 双引擎差分 fuzz 与 lexer / parser fuzz（PR #20，Closes #19）；
  VM 执行层直接单元测试，`internal/vm` 覆盖率 24.8% → 95.7%
  （PR #22，Closes #21）。
- 编译器与 VM 解耦：`CompileProgram` 只依赖 `Allocator` 窄接口
  （`internal/compiler/alloc.go`），不再依赖活的 `*vm.VM`
  （PR #27，Closes #13）。

### Changed

- 结构重构（PR #4，Closes #3）：抽取双引擎共享语义 `internal/semantics`、
  瘦身入口、补单元测试。
- 包改名（PR #10）：`internal/tree` → `treewalk`，`internal/engine` → `vm`；
  CLI 的 `--engine=tree` 取值刻意不变。
- 大文件拆分（PR #12，Closes #11）：`internal/vm/vm.go`（619 行）→ 7 个文件，
  `internal/compiler/compiler.go`（596 行）→ 5 个文件；公开 API 不变。
- 优化收敛（PR #6）：注释与文档收敛、死代码清理、bench 与 Makefile 修复。
- 清理（PR #8，Closes #7）：删除死包 `internal/numfmt/`、孤儿脚本
  `scripts/push-to-github.cmd`、根目录 `ISSUE.md`（归档副本在
  `docs/archive/ISSUE.md`）。
- 基准数字按新口径重测同步（PR #26，Closes #25）：`fib(30)` tree 2471 ms、
  VM 377 ms，加速比 6.6×（2026-11-16，Linux x86_64 / Go 1.23.4 共享虚拟机，
  5 次 + 预热取中位数）。

### Fixed

- 极深嵌套括号曾导致 fatal stack overflow 崩溃，改为干净的语法错误
  （PR #18，Closes #17）。
- 差分 fuzz 发现并修复：只有 `!` 的输入曾让解析器无限递归直至 fatal stack overflow，
  `parsePrefix` 加 EOF 守卫（PR #20，Closes #19）。

### 已知限制

- 执行无步数 / 时间预算：`while (true) {}` 只能 Ctrl-C 中断
  （在途 PR #30 修复中）。
- 诊断渲染不截断长行：超长单行报错会输出整行源码
  （在途 PR #34 修复中）。
- VM 运行时错误只有行号、没有列号；两个引擎对同一程序报的位置不对称
  （待做）。
- REPL 是裸 `bufio.Scanner`：无行编辑、无历史、无 `:load`、无 `:engine`
  （待做）。
- 无 `check` / `fmt` 子命令（待做）；无上手教程；除 `print` 外唯一的内置
  `clock` 没有任何示例用到（待做）。
- 语言特性冻结：不加类、数组、哈希表、lambda、`break` / `continue`
  （见 `docs/00-spec.md`）。
