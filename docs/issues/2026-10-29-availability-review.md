# [可用性审查] `bench --gc-off` 静默无效 / 单位换算边界 / LOC 统计链路脆弱

> 本文就是 issue 正文，可整段粘贴到 GitHub。
> 对应分支：`improve/loc-and-walkdir`。全部改动已附回归测试。

## 背景

对 `OL486/minilang` 做了一次全局运行可用性审查，覆盖：构建、静态检查、测试、
五个示例在**两个引擎**上的一致性、GC 压力、CLI 参数真实生效情况。

结论：**可用，且质量高于一般教学项目**。`go build` / `go vet` / `go test ./...` 全绿，
5 个示例 + 5 个错误示例全部正常，tree 与 VM 两个引擎输出逐字节一致，
`fib(30)` 中位数 160–260 ms（随机器负载波动），40 万对象压力测试后存活对象 < 300。

审查中发现 1 个真实 bug、2 处健壮性问题、1 处测试空白，均已处理。

---

## 问题 1（真 bug）：`bench --gc-off` 被静默忽略

**文件**：`cmd/minilang/main.go` → `runBench`

**复现**：

```bash
minilang bench --gc-off      # 参数被接受，输出看起来完全正常
```

**原因**：`--gc-off` 只在 `newSession()` 里被读取（`run` / `repl` 走这条路径）。
`runBench` 是直接 `engine.NewVM(io.Discard)`，之后**没有**调用 `SetGCEnabled(false)`，
于是开关在 bench 路径上完全失效。

**为什么这个 bug 值得单列**：它不报错、不崩溃、输出格式正确——只是对照实验没做成。
`ISSUE.md` 里给的 GC 对照命令用的是 `run`（那条路径是好的），所以文档也没暴露它。
这类"参数被接受但没生效"的问题，比崩溃更难发现。

**修复**：

```go
vm := engine.NewVM(io.Discard)
if o.gcOff {
    vm.SetGCEnabled(false)
}
```

**说明**：`fib(30)` 的对象分配量不大，GC 次数通常为 0，所以在这个基准上开不开 GC
耗时差异不明显。开关现在确实生效了，但要观测到差异需要用 `05-gc-stress.ml` 那类场景。

---

## 问题 2：`humanBytes` 在 1024 边界上单位不进位

**文件**：`cmd/minilang/main.go` → `humanBytes`

**复现**：

| 输入 | 修复前 | 修复后 |
|---|---|---|
| 1024 | `1024 B` | `1.00 KiB` |
| 1 MiB | `1024.00 KiB` | `1.00 MiB` |

**原因**：判断条件写的是 `n > 1<<10` 而不是 `>=`，恰好等于阈值时掉到下一级单位。
不影响正确性，但 `bench` 的内存报告会出现"1024.00 KiB"这种读起来别扭的数字。

**修复**：改用 `>=`。

---

## 问题 3：LOC 统计链路在项目目录外直接归零

**文件**：`cmd/minilang/main.go` → `countLOC` / `moduleRoot`

**原因**：

1. `moduleRoot()` 只从 `os.Getwd()` 向上找 `go.mod`。`go install` 之后在任意目录运行
   `minilang bench`，找不到 `go.mod` 就返回 `(0, 0)`，输出变成
   `Go 源码行数: 0 行（非空非注释 0 行）`——看起来像统计功能坏了。
2. `countLOC` 用的是 `filepath.Walk`，该 API 自 Go 1.16 起已被 `filepath.WalkDir` 取代，
   后者避免对每个文件额外 `os.Lstat`。

**修复**：

1. `moduleRoot()` 增加回退：cwd 找不到时，再从 `os.Executable()` 所在目录向上找。
   覆盖"在项目目录里编译、在别处运行该二进制"的场景（`go install` 到 `$GOBIN` 的极端情况
   仍无法定位源码，这是固有限制）。
2. `filepath.Walk` → `filepath.WalkDir`（`go.mod` 要求 1.22，可用）。
3. `locTotal == 0` 时输出改为 `（无法统计：不在项目目录中运行）`，而不是显示 0。

---

## 问题 4：`cmd/minilang` 此前没有任何测试

**原因**：`go test ./...` 输出里 `cmd/minilang [no test files]`。而恰恰是这个包里放着
`needsMoreInput`（REPL 续行判定）、`medianDuration`、`signedBytes`、`humanBytes`
这些**纯函数**，它们最容易测，也最容易在重构时被改坏。

**修复**：新增 `cmd/minilang/main_test.go`，5 个测试函数、14 个断言点：

| 用例 | 覆盖点 |
|---|---|
| `TestCountLOCFindsModule` | LOC 统计能定位到模块，且 `code <= total` |
| `TestNeedsMoreInput` | 7 种输入：未闭合括号/花括号、字符串里的 `}`、转义引号、未闭合字符串 |
| `TestSignedBytesHandlesNegative` | 负差值必须带 `-`，防止 uint64 下溢成 1.7e19 |
| `TestMedianDuration` | 乱序输入取排序后的中位数 |
| `TestHumanBytes` | 512 B / 1 KiB / 1 MiB / 2 KiB 四个边界 |

`needsMoreInput` 的用例尤其值得留：它决定 REPL 里输入 `if (x) {` 回车后是继续等下一行
还是立刻报语法错，属于"改坏了很难在测试里发现、但用户一用就烦"的那类逻辑。

---

## 验证方式（可直接执行）

```bash
go build ./... && go vet ./... && go test ./...

# 双引擎一致性（5 个示例逐字节 diff）
for f in examples/*.ml; do
  diff <(go run ./cmd/minilang run "$f") \
       <(go run ./cmd/minilang run "$f" --engine=tree) && echo "OK $f"
done

# 基准与 GC
go run ./cmd/minilang bench
go run ./cmd/minilang bench --engine=tree
go run ./cmd/minilang run examples/05-gc-stress.ml --stats
go run ./cmd/minilang run examples/05-gc-stress.ml --stats --gc-off
```

Windows 上没有 `make` 时，`Makefile` 里各目标的等价命令就是上面这些。

本次审查实测结果：

- `go test ./...` 全绿（含新增 `cmd/minilang` 包），`gofmt -l` 无输出
- 5 个示例在两个引擎上输出逐字节一致
- `bench` 中位数 181–192 ms，LOC 统计 4327 行（非空非注释 3733 行）
- `bench --gc-off` 可正常执行
- CI（`ci.yml`）在 **ubuntu / windows / macos 三个平台全绿**：`check` + 三个 `smoke` job 全部 success

---

## 拓展建议（按性价比排序）

### 1. `bench` 支持 `--stats`（低成本）

`runBench` 不响应 `--stats`，所以上面那条"GC 开关是否生效"的验证只能靠耗时/内存间接看。
建议把 `printStats` 接进 bench 收尾，输出对象堆与 GC 次数——这样 GC 对照实验才有硬指标。

### 2. `Chunk.Lines` 换成 `[]struct{line, col}`（中成本，收益明显）

目前 VM 运行时错误只有行号没有列号（`diag` 里 `Col <= 0` 就不画 `^`）。
把 `Lines []int` 换成结构体切片后，VM 运行时错误也能画出精确的 `^` 指针，
和词法/语法错误的位置精度对齐。代价是 chunk 内存翻倍，对教学项目可接受。

### 3. `break` / `continue`（中成本）

`ISSUE.md` 已列为后续任务。实现要点是跳转回填的嵌套管理：`break` 跳到循环出口，
`continue` 跳到 `loopStart`，都需要维护一个"当前循环上下文栈"，
并在 `endScope` 时正确关闭 upvalue。这也是把"跳转回填"这个知识点讲透的最好练习。

### 4. 数组 + 下标（中成本）

需要新堆对象 `ObjArray`、新的 GC 标记分支（遍历元素）、以及下标表达式的解析优先级
（`a[0]` 应该和函数调用同级）。做完之后 `05-gc-stress.ml` 可以改成数组压力测试，
GC 覆盖会更真实。

### 5. 常量池 256 上限的报错位置（低成本）

`addConstant` 超限时报的是 `line`，但常量池属于**函数**而不是某一行。
建议在报错里带上函数名（`displayFnName` 已有），并提示"把大函数拆小"这一具体改法。

### 6. REPL 未闭合字符串的处理（低成本）

`needsMoreInput` 遇到未闭合字符串会继续等待下一行，但 lexer 又不允许字符串跨行，
用户最终拿到的是"字符串不能跨行"——中间多绕了一步。可以在 REPL 层直接提示
"当前行字符串未闭合，字符串不支持跨行，请补齐引号"。

---

## 变更范围

| 文件 | 变更 |
|---|---|
| `cmd/minilang/main.go` | `bench` 响应 `--gc-off`；`humanBytes` 边界；`Walk`→`WalkDir`；`moduleRoot` 回退；LOC 输出兜底 |
| `cmd/minilang/main_test.go` | 新增，5 个测试函数 / 14 个断言点 |

无破坏性变更：CLI 参数、输出格式（除上述两处修正）、内部包 API 均保持不变。
