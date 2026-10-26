# minilang

一门从零实现的小语言：词法分析 → Pratt 解析 → tree-walking 解释器 → 字节码编译器 → 栈式虚拟机 → 标记-清除 GC。

零外部依赖，只用 Go 标准库。`go build ./...`、`go vet ./...`、`go test ./...` 全绿。

```bash
go build -o minilang.exe ./cmd/minilang
./minilang.exe run examples/03-closures.ml
```

```minilang
fn makeCounter() {
  let count = 0;
  fn inc() { count = count + 1; return count; }
  return inc;
}

let c1 = makeCounter();
let c2 = makeCounter();
print c1();   // 1
print c1();   // 2
print c2();   // 1   <- 两个计数器互不干扰
print c1();   // 3
```

---

## 为什么有两个引擎

同一份源码、同一份 AST，可以走两条完全不同的执行路径：

```
源码 ──lexer──> []Token ──parser──> []ast.Stmt ──┬── tree.Interp  （直接遍历 AST）
                                                └── compiler ──> 字节码 ──> engine.VM
```

`--engine=tree` / `--engine=vm` 切换。**两个引擎的输出必须逐字节相同** ——
这条约束由 `test/e2e_test.go` 强制，是控制「手写两次解释器导致语义漂移」的唯一手段。

保留 tree 引擎不是为了怀旧：它是 VM 的**语义参照物**。
VM 里任何一处语义写错，都可以用 `diff <(minilang run x.ml) <(minilang run x.ml --engine=tree)` 定位。

---

## 安装

**① 预编译包（不需要装 Go）**

从 [Releases](https://github.com/OL486/minilang/releases) 下载对应平台的压缩包，解开即用：

```bash
# Linux / macOS
tar -xzf minilang_0.1.0_linux_amd64.tar.gz
cd minilang_0.1.0_linux_amd64
./minilang run examples/03-closures.ml

# Windows：解压 minilang_0.1.0_windows_amd64.zip 后
minilang.exe run examples\03-closures.ml
```

包内自带 `examples/` 和 `docs/`，解开就能跑示例，不用回仓库翻文件。
`dist/checksums.txt` 可校验完整性：`sha256sum -c checksums.txt`。

**② go install**

```bash
go install github.com/OL486/minilang/cmd/minilang@latest
```

**③ 从源码构建**

```bash
git clone https://github.com/OL486/minilang.git
cd minilang
make build     # 等价于 go build -trimpath -ldflags "-s -w" -o minilang ./cmd/minilang
```

### 自己出发布包

```bash
make check      # gofmt + vet + test，提交前跑这个
make examples   # 跑全部示例，并校验两个引擎输出逐字节一致
make release    # 交叉编译 6 个平台 -> dist/*.zip|tar.gz + checksums.txt
```

`make release` 调用 `tools/pack/main.go`（纯 Go 实现，不依赖 zip / tar / sha256sum 这些在 Windows 上不齐的命令），
与 `.github/workflows/release.yml` 在打 tag 时执行的是**同一份打包逻辑**，
所以本地出的包和 Release 上的包内容一致。

---

## 用法

```bash
# 执行脚本
minilang run examples/01-basics.ml
minilang examples/01-basics.ml              # run 可以省略

# 交互式
minilang repl

# 基准测试
minilang bench                  # 字节码 VM
minilang bench --engine=tree    # tree-walking（S5 基线）
```

### 三个调试开关（可以任意组合）

```bash
minilang run x.ml --dump-tokens      # token 流：类型 / 字面量 / 行号
minilang run x.ml --dump-ast         # 语法树：缩进树形
minilang run x.ml --dump-bytecode    # 反汇编：偏移 / 行号 / opcode / 操作数 / 常量池
```

这三个开关是排错能力的全部基础，用法见 [`docs/01-pipeline.md`](docs/01-pipeline.md)。

### 其他

```bash
minilang run x.ml --stats            # 执行后打印对象堆与 GC 统计
minilang run x.ml --gc-off           # 关闭 GC（对照组，长跑会吃满内存）
```

REPL 命令：`:help` `:quit` `:gc` `:stats`

---

## 阶段 → 文件映射

| 阶段 | 内容 | 主要文件 | 验收 |
|---|---|---|---|
| S0 | 语言规范 | [`docs/00-spec.md`](docs/00-spec.md) | BNF + 优先级表 + 错误约定 |
| S1 | 词法分析 + REPL | `internal/lexer/` `internal/token/` `internal/diag/` | `--dump-tokens` |
| S2 | AST + Pratt 解析 | `internal/ast/` `internal/parser/` | `--dump-ast`，`*` 是 `+` 的子节点 |
| S3 | 变量 / 作用域 / 控制流 | `internal/tree/env.go` `interp.go` | `while (i<5) { print i; i=i+1; }` |
| S4 | 函数 / 调用帧 / 闭包 | `internal/tree/interp.go` | `makeCounter` 两个计数器互不干扰 |
| S5 | 性能基线 | `cmd/minilang/main.go` (`bench`) | [`docs/03-benchmark.md`](docs/03-benchmark.md) |
| S6 | 字节码编译器 + 反汇编 | `internal/compiler/` `internal/engine/chunk.go` | `--dump-bytecode` |
| S7 | VM 执行循环 + 跳转 + 局部变量 | `internal/engine/vm.go` | while 循环结果与 S3 一致 |
| S8 | VM 里的函数 / 帧 / upvalue | `internal/engine/vm.go` `object.go` | `makeCounter` 在 VM 上一致 |
| S9 | 标记-清除 GC | `internal/engine/gc.go` | `examples/05-gc-stress.ml --stats` |
| S10 | 收尾对比 | [`docs/03-benchmark.md`](docs/03-benchmark.md) | 8.7× 加速 |

---

## 验收命令（一次跑完）

```bash
go build ./... && go vet ./... && go test ./...

# 双引擎一致性：所有示例在两条路径上输出必须相同
for f in examples/*.ml; do
  diff <(./minilang.exe run "$f") <(./minilang.exe run "$f" --engine=tree) && echo "OK $f"
done

# 错误信息带位置
./minilang.exe run examples/errors/01-unterminated-string.ml

# 性能对比
./minilang.exe bench --engine=tree
./minilang.exe bench

# GC 验收
./minilang.exe run examples/05-gc-stress.ml --stats
./minilang.exe run examples/05-gc-stress.ml --stats --gc-off
```

---

## 关键数字

| 指标 | tree-walking | 字节码 VM |
|---|---|---|
| `fib(30)` 中位数 | 1402 ms | **161 ms** |
| 加速比 | — | **8.7×**（多次测量 8.4–8.7） |
| 40 万对象压力测试后存活 | — | **288**（关闭 GC 时 403,015） |

完整数据和测量口径见 [`docs/03-benchmark.md`](docs/03-benchmark.md)。

---

## 设计上最值得看的五处

这五处对应 [`docs/02-pitfalls.md`](docs/02-pitfalls.md) 里的五个陷阱，代码里都有长注释：

1. **局部变量用绝对槽位，不用相对栈顶偏移** —— `compiler.go` 的 `resolveLocal`。
   相对偏移在「赋值表达式的值被用到」时会写到隔壁变量上。
2. **跳转回填的 `- 2`** —— `compiler.go` 的 `patchJump` / `emitLoop`。
   偏移相对操作数之后那个字节算，不是相对指令开头。
3. **upvalue 的 open / close，链表按槽位降序** —— `vm.go` 的 `captureUpvalue` / `closeUpvalues`。
   降序是「扫到第一个越界就停」这个前提。
4. **GC 的 5 类根，以及「先 GC 再挂链」的分配顺序** —— `gc.go` 的 `markRoots`、`vm.go` 的 `allocate`。
5. **字符串驻留是 `==` 语义的前提，不是性能优化** —— `vm.go` 的 `InternString`、`value.go` 的 `valuesEqual`。

---

## 已知限制（刻意的取舍）

- 没有类 / 对象 / 数组 / 哈希表（按规范排除）
- 没有匿名函数（lambda）、没有 `break` / `continue`
- 没有尾调用优化，深递归会撞调用帧上限（VM 1024，tree 1000）
- 单函数常量 / 局部变量 / upvalue 上限各 256（单字节操作数）
- 字符串不可跨行，转义只支持 `\\ \" \n \t \r`
- 只有 ASCII 标识符
- GC 不做分代、不做增量、不做压缩；`--gc-off` 只用于对比

## 建议的扩展练习（按难度排序）

1. `break` / `continue`（要处理嵌套循环的跳转回填）
2. 匿名函数 `fn (x) { ... }`（编译器要支持表达式位置的闭包）
3. 数组 `[1, 2, 3]` + 下标（需要新的堆对象 + GC 标记子元素）
4. 字符串格式化 / `number` 转 `string`（会让 GC 压力测试变得更有意思）
5. 把 `Chunk.Lines` 换成 `[]struct{line, col}`，让 VM 的运行时错误也带列号
6. 分代 GC 或增量 GC（对照当前实现，看 STW 时间怎么变化）

---

## 文档

- [`docs/00-spec.md`](docs/00-spec.md) —— 语言规范：BNF、优先级、语义、错误约定、目录结构
- [`docs/01-pipeline.md`](docs/01-pipeline.md) —— 三个 dump 怎么读，附真实输出
- [`docs/02-pitfalls.md`](docs/02-pitfalls.md) —— 五个陷阱的完整分析 + 实现中真实踩到的两个坑
- [`docs/03-benchmark.md`](docs/03-benchmark.md) —— 基线、对比表、GC 验收、结论

## 环境

Go 1.22+（开发用 1.26.1）。Windows / Linux / macOS 均可，无 CGO，无外部依赖。
