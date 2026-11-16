# Changelog

本文件记录 minilang 每个版本值得注意的改动，格式遵循
[Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

规则：**只有已合进 `main` 的改动才能记进 Unreleased / 版本小节**。
在途的 PR 可先记在 Unreleased 下的「在途」小节并写清未合，等合并后再上移
（Unreleased 当前没有在途条目时，不留空小节）。
（notes-mcp 曾把未合的 `list_notes` 写进 Unreleased 造成误报，本仓库不要重蹈覆辙。）

## [Unreleased]

### Added

- `check` 子命令：只做词法 / 语法 / 编译检查，不执行；纯运行时错误放过
  （PR #55，Closes #37）。
- `--trace` 逐指令调试器：VM 下打印 pc / opcode / 栈顶，tree 引擎下明确提示
  仅支持 VM（PR #43，Closes #42）。
- REPL 持久历史（`~/.minilang_history`，0600，上限 500 行）+ `:load` +
  `:engine` / `:history`（PR #49，Closes #45）。
- 上手教程（`docs/05-tutorial.md`）与 `clock` 计时示例
  `examples/06-clock-timing.ml`；cmd / treewalk 直接测试，覆盖率分别 0% →
  84.1% / 89.2%（PR #40，Closes #39）。
- CHANGELOG / CONTRIBUTING / Issue 与 PR 模板（PR #36，Closes #35）。

### Changed

- 文档漂移收敛、treewalk `interp.go` 拆分、`chunk.go` 反汇编拆分
  （PR #28，Closes #15）。
- P2 打包：删过期行数、死配置、REPL 毛刺，`run -` 读 stdin，`runFile` 10 MiB
  上限（PR #48，Closes #46）。
- README 加 CI / Release / Go / License 徽章（PR #44，Closes #41）。

### Fixed

- 执行步数预算：VM 指令计数 + treewalk 步数计数，死循环撞限报运行时错误，
  不再只能 Ctrl-C 中断；上限 `semantics.MaxExecSteps = 100M`
  （PR #56，Closes #29；原 PR #30 被替代，未合）。
- 诊断渲染截断长行：2M 字符单行报错从约 4 MB 降到 251 字节
  （PR #34，Closes #33）。
- gofmt 漂移修复：4 个文件 import 排序 + `fmt-check` 在 gofmt 不在 PATH 时报错
  （PR #32，Closes #31）。
- VM 运行时错误带列号：`Chunk.Lines` → `Positions []ast.Pos`，编译错误也带列号；
  预算报错同样带列号（PR #57，Closes #51；原 PR #52 / #53 被替代，未合）。

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

### 已知限制（当前 Unreleased 仍未解决的）

- 无 `fmt` 子命令（`check` 已有，见 Unreleased）。
- 语言特性冻结：不加类、数组、哈希表、lambda、`break` / `continue`
  （见 `docs/00-spec.md`）。
