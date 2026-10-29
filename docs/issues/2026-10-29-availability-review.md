# 可用性审查：4 处修复

对 minilang 做了一轮可用性检查：构建、静态检查、测试、五个示例在两个引擎上的一致性、
GC 压力、CLI 参数是否真的生效。

整体可用。`go build` / `go vet` / `go test ./...` 全绿，5 个示例和 5 个错误示例都正常，
tree 与 VM 输出逐字节一致，`fib(30)` 中位数 160–260 ms（随机器负载波动），
40 万对象压力测试后存活对象不到 300。

发现 1 个功能 bug、2 处健壮性问题、1 处测试空白，改动在 `improve/loc-and-walkdir` 分支。

## 1. bench --gc-off 被静默忽略

`--gc-off` 只在 `newSession()` 里被读取，`run` 和 `repl` 走这条路径。`runBench` 直接
`engine.NewVM(io.Discard)`，之后没有调用 `SetGCEnabled(false)`，开关在 bench 路径上不生效。

```bash
minilang bench --gc-off      # 参数被接受，输出看起来正常
```

不报错、不崩溃、输出格式也对，只是对照实验没做成。`ISSUE.md` 里给的 GC 对照命令用的是 `run`
（那条路径是好的），所以文档也没暴露。

```go
vm := engine.NewVM(io.Discard)
if o.gcOff {
    vm.SetGCEnabled(false)
}
```

需要说明的是 `fib(30)` 的对象分配量不大，GC 次数通常为 0，在这个基准上看不出开关前后的耗时差异。
开关现在确实生效了，但要看差异得用 `05-gc-stress.ml` 那类场景。

## 2. humanBytes 在 1024 边界不进位

判断条件写的是 `n > 1<<10` 而不是 `>=`，等于阈值时掉到下一级单位：

| 输入 | 修复前 | 修复后 |
|---|---|---|
| 1024 | `1024 B` | `1.00 KiB` |
| 1 MiB | `1024.00 KiB` | `1.00 MiB` |

不影响正确性，只是 `bench` 的内存报告会出现「1024.00 KiB」这种数字。改用 `>=`。

## 3. LOC 统计在项目目录外归零

两个问题叠在一起：

`moduleRoot()` 只从 `os.Getwd()` 向上找 `go.mod`。`go install` 之后在别的目录跑 `minilang bench`，
找不到就返回 `(0, 0)`，输出变成 `Go 源码行数: 0 行（非空非注释 0 行）`，看起来像统计坏了。

`countLOC` 用的是 `filepath.Walk`，这个 API 从 Go 1.16 起被 `filepath.WalkDir` 取代，
后者不会对每个文件多做一次 `os.Lstat`。

改法：`moduleRoot()` 在 cwd 找不到时再从 `os.Executable()` 所在目录向上找，覆盖「在项目里编译、
在别处运行该二进制」的情况（`go install` 到 `$GOBIN` 之后仍然定位不到源码，这是固有限制）；
`Walk` 换成 `WalkDir`；`locTotal == 0` 时输出「（无法统计：不在项目目录中运行）」而不是显示 0。

## 4. cmd/minilang 没有任何测试

`go test ./...` 里 `cmd/minilang [no test files]`。这个包放着 `needsMoreInput`（REPL 续行判定）、
`medianDuration`、`signedBytes`、`humanBytes` 几个纯函数，是最容易测、也最容易在重构时改坏的。

新增 `cmd/minilang/main_test.go`，5 个测试函数、14 个断言：

- `TestCountLOCFindsModule`：能定位到模块，且 `code <= total`
- `TestNeedsMoreInput`：7 种输入，含未闭合括号/花括号、字符串里的 `}`、转义引号、未闭合字符串
- `TestSignedBytesHandlesNegative`：负差值带 `-`，防止 uint64 下溢成 1.7e19
- `TestMedianDuration`：乱序输入取排序后的中位数
- `TestHumanBytes`：512 B / 1 KiB / 1 MiB / 2 KiB 四个边界

`needsMoreInput` 那组值得留着：它决定 REPL 里输入 `if (x) {` 回车后是继续等下一行还是立刻报语法错，
改坏了功能测试不容易发现，但用户一用就会碰到。

## 验证

```bash
go build ./... && go vet ./... && go test ./...

for f in examples/*.ml; do
  diff <(go run ./cmd/minilang run "$f") \
       <(go run ./cmd/minilang run "$f" --engine=tree) && echo "OK $f"
done

go run ./cmd/minilang bench
go run ./cmd/minilang bench --engine=tree
go run ./cmd/minilang run examples/05-gc-stress.ml --stats
go run ./cmd/minilang run examples/05-gc-stress.ml --stats --gc-off
```

Windows 上没有 make 时，`Makefile` 里各目标的等价命令就是上面这些。

实测结果：`go test ./...` 全绿（含新增的 `cmd/minilang`），`gofmt -l` 无输出，
5 个示例双引擎输出一致，`bench` 中位数 181–192 ms，LOC 4327 行（非空非注释 3733 行），
`bench --gc-off` 可正常执行。CI 在 ubuntu / windows / macos 三个平台全绿。

## 后续可做

- `bench` 不响应 `--stats`，GC 对照实验只能看耗时和内存。把 `printStats` 接进 bench 收尾，输出对象堆和 GC 次数。
- `Chunk.Lines` 现在是 `[]int`，VM 运行时错误只有行号没有列号（`diag` 里 `Col <= 0` 就不画 `^`）。换成 `[]struct{line, col}` 之后位置精度能和词法/语法错误对齐，代价是 chunk 内存翻倍。
- `break` / `continue`：`ISSUE.md` 已列。要点是维护一个循环上下文栈，`break` 跳到出口、`continue` 跳到 `loopStart`，并在 `endScope` 时正确关闭 upvalue。
- 数组 + 下标：需要新的 `ObjArray`、GC 的标记分支、以及下标表达式的优先级（`a[0]` 与函数调用同级）。做完可以把 `05-gc-stress.ml` 改成数组压力测试，GC 覆盖更真实。
- `addConstant` 超限时报的是行号，但常量池属于函数。建议带上函数名（`displayFnName` 已有），并提示拆小函数。
- REPL 遇到未闭合字符串会继续等下一行，但 lexer 不允许字符串跨行，用户最后拿到的是「字符串不能跨行」。可以在 REPL 层直接提示补齐引号。

## 变更文件

| 文件 | 变更 |
|---|---|
| `cmd/minilang/main.go` | bench 响应 `--gc-off`；`humanBytes` 边界；`Walk`→`WalkDir`；`moduleRoot` 回退；LOC 输出兜底 |
| `cmd/minilang/main_test.go` | 新增，5 个测试函数 / 14 个断言 |

CLI 参数、内部包 API 都没有变。
