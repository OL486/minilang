# [项目] minilang：从零实现一门小语言（词法 → Pratt → 字节码 VM → 标记-清除 GC）

> 这份文件就是 issue 正文，可以整段粘贴到 GitHub。
> 代码已在本地完成并全部通过 `go build` / `go vet` / `go test`，下方「交付清单」即当前实际状态。

---

## 背景

要打通一组编译原理概念：**词法分析、递归下降与 Pratt 解析、作用域链、调用帧、闭包与 upvalue、字节码、栈式虚拟机、垃圾回收**。

选择「从零实现一门自己定义的小语言」而不是复刻现成实现，因为：

- 复刻会退化成「照着抄一遍」，抄完记不住为什么；
- 自定义语法会逼着你在每个设计点上做决定（槽位怎么编址、跳转怎么回填、upvalue 什么时候关），
  这些决定才是要学的东西；
- 两个引擎（tree-walking + 字节码 VM）跑同一份 AST，可以互相验证语义，把「理解偏差」变成可检测的失败。

## 目标

1. 一条完整可运行的流水线：`源码 → Token → AST → 字节码 → VM 执行`
2. **每一步都能看见中间产物**：`--dump-tokens` / `--dump-ast` / `--dump-bytecode`
3. **有性能基线**：先测 tree-walking 的 `fib(30)`，再用字节码 VM 复测，给出对比
4. **错误信息带位置**：行号 + 列号 + 源码片段 + `^` 指针，不允许「unexpected token」式报错
5. **自己实现标记-清除 GC**，不依赖宿主语言的 GC 做对象生命周期决策

## 范围

**包含**：`number` / `string` / `bool` / `nil` / `function` 五种类型；`let` / `if-else` / `while` / `fn` / `return` / `print`；算术、比较、短路逻辑；一等函数与闭包；`clock()`。

**不包含**（刻意排除，留作扩展练习）：类、对象、数组、哈希表、匿名函数、`break`/`continue`、尾调用优化、异常。

---

## 交付清单

- [x] **S0** 语言规范：BNF 文法、运算符优先级与结合性表、关键字、内置函数签名、错误约定、目录结构 → `docs/00-spec.md`
- [x] **S1** 词法分析器 + REPL 骨架 + `--dump-tokens`
- [x] **S2** AST + 递归下降解析器（表达式用 Pratt / precedence climbing）+ `--dump-ast`
- [x] **S3** tree-walking 解释器：环境链、`let`、赋值、`if`、`while`、`print`
- [x] **S4** 函数、调用帧、闭包（tree-walking 版）
- [x] **S5** 性能基线：`fib(30)` 三次取中位数，记录耗时/内存/行数 → `docs/03-benchmark.md`
- [x] **S6** 字节码编译器 + 反汇编器 + `--dump-bytecode`
- [x] **S7** 栈式 VM 执行循环：算术、跳转回填、局部变量槽位
- [x] **S8** VM 里的函数、调用帧、闭包 upvalue（open/close）
- [x] **S9** 标记-清除 GC（自管对象堆）+ 压力测试验收
- [x] **S10** 收尾：复测 `fib(30)`，完整对比表 + 「字节码换来了什么」结论
- [x] 端到端测试：双引擎一致性、错误信息、GC 存活对象数
- [x] 5 个可运行示例 + 5 个错误示例

---

## 阶段计划与验收

| 阶段 | 验收命令 | 通过标准 |
|---|---|---|
| S1 | `minilang run x.ml --dump-tokens` | 数字/标识符/关键字/运算符分类正确；`let x = "未闭合` 报带行号的未闭合字符串 |
| S2 | `minilang run x.ml --dump-ast` | `1 + 2 * 3` 的 AST 里 `*` 是 `+` 的子节点 |
| S3 | `minilang run examples/02-control-flow.ml` | `while` 输出 0–4 |
| S4 | `minilang run examples/03-closures.ml` | 两个计数器互不干扰（1,2,1,3） |
| S5 | `minilang bench --engine=tree` | 得到一组数字（不是代码） |
| S6 | `minilang run x.ml --dump-bytecode` | `1 + 2 * 3` 反汇编出 `CONST 1 / CONST 2 / CONST 3 / MULTIPLY / ADD` |
| S7 | `minilang run examples/02-control-flow.ml` | 与 S3 输出完全一致 |
| S8 | `minilang run examples/03-closures.ml` | 与 S4 输出完全一致 |
| S9 | `minilang run examples/05-gc-stress.ml --stats` | 40 万对象创建后存活 <1000，不 OOM |
| S10 | `minilang bench` vs `minilang bench --engine=tree` | 给出对比表与归因 |

---

## 关键结果

### 性能

| 指标 | tree-walking（S5） | 字节码 VM（S10） | 变化 |
|---|---|---|---|
| `fib(30)` 中位数 | **1402 ms** | **161 ms** | **8.7×**（多次测量 8.4–8.7） |
| 单次波动 | ±10%（第 3 次 +25%） | ±3% | 更稳 |
| 每层调用分配 | 1 个 `map` + N 个闭包 | 0 | — |
| Go 堆（回收后） | ~400 KiB | ~1.38 MiB（含 1 MiB 预分配栈） | VM 常驻更高 |

### GC

| 指标 | GC 开启 | GC 关闭（对照） |
|---|---|---|
| 累计创建对象 | 403,015 | 403,015 |
| 运行后存活对象 | **288** | **403,015** |
| 自管对象堆 | **0.75 MiB** | **22.7 MiB** |
| GC 次数 | 22 | 0 |

### 规模

- Go 源码 3970 行（非空非注释 3426 行）
- 零外部依赖，`go.mod` 只有 `module minilang` + `go 1.22`

---

## 三条工程要求的落实情况

### 1. 中间产物可见

三个开关可以任意组合，输出按流水线顺序拼接：

```bash
minilang run examples/03-closures.ml --dump-tokens --dump-ast --dump-bytecode
```

反汇编会**递归到嵌套函数**（缩进表示层级），因为闭包 bug 只能在这一层看到：

```
== p.ml ==
0002    | OP_CLOSURE              1 'inc' upvalues=1
0004    |                        local[1]

  == inc ==
    0000    3 OP_GET_UPVALUE          0
```

### 2. 性能基线

基线是 `docs/03-benchmark.md` 的第 1 节，**在 VM 存在之前测的**（用 `--engine=tree`）。
没有它，S7 之后的 8.7 倍就只是一个数字，不是证据。

### 3. 错误信息带位置

```
词法错误 (第 2 行, 第 11 列): 字符串未闭合：字符串不能跨行，第 2 行就已换行
   2 | let msg = "没有结尾;
     |           ^
```

四个阶段各有标签（词法/语法/编译/运行时），`test/e2e_test.go` 的 `TestErrors`
断言「消息里必须含行号」，而不只是「报错了」。

字节码 VM 的运行时错误只有行号没有列号 —— 因为 `Chunk.Lines` 是每字节一个行号。
这是刻意的取舍，写在 `docs/00-spec.md` 第 7 节。

---

## 五个陷阱的处理

详细分析见 `docs/02-pitfalls.md`，这里只列结论：

| # | 陷阱 | 本实现的选择 |
|---|---|---|
| 1 | 局部变量槽位 | **绝对槽位**（`slotBase + slot`），不用相对栈顶偏移。相对偏移在「赋值表达式的值被用到」时会写到隔壁变量 |
| 2 | 跳转回填 | 发 `0xff 0xff` 占位，编译到目标再 patch。偏移相对**操作数之后**那个字节算（`len - offset - 2`） |
| 3 | upvalue open/close | 栈上时 open（`Location` 指栈），离开作用域时 close（值拷到 `Closed`）。链表**按槽位降序**，因为 close 是「扫到第一个越界就停」 |
| 4 | GC 根集合 | 5 类根：VM 栈 `[0,sp)`、调用帧闭包、全局表、open upvalue 链表、编译期临时根。**先 GC 再挂链**，否则新对象当场被回收 |
| 5 | 字符串驻留 | `map[string]*ObjString`。驻留是 `==` 语义的**前提**不是优化 —— 去掉它 `valuesEqual` 会静默错 |

### 需要指出的两点偏差

1. **陷阱 1 的描述对应「相对偏移」那一派设计**。本实现用绝对槽位，所以那个 off-by-one
   在本项目里不存在；但相对偏移确实更容易差一位，原因写在 `docs/02-pitfalls.md`。
2. **Go 不允许指针大小比较**，所以「按栈槽降序」这件事用 `ObjUpvalue.Slot int` 表达，
   而不是像 C 版本那样比较 `Location` 指针。

---

## 实现中真实踩到的两个坑

记在这里，因为它们比「理论上的陷阱」更有价值：

1. **`parsePrecedence` 里调用分支漏了 `p.advance()`**。
   `finishCall` 看到的是 `(` 本身，把它当分组括号解析，
   于是 `f()` 报出「表达式不能以 ')' 开头」——**位置对了，归因错了**。

2. **GC 标记 nil upvalue 崩溃**。`OP_CLOSURE` 先把闭包压栈再逐个填 upvalue，
   填第一个时若触发 GC，`Upvalues[1..]` 还是 nil。
   `*ObjUpvalue(nil)` 装进 `Object` 接口后**不等于 nil 接口**，
   `o == nil` 判 false，接着 `o.header()` 就崩了。
   修法：`header()` 容忍 nil 接收者 + `blacken` 显式跳过 nil。

---

## 如何验证

```bash
go build ./... && go vet ./... && go test ./...

# 双引擎一致性
for f in examples/*.ml; do
  diff <(go run ./cmd/minilang run "$f") \
       <(go run ./cmd/minilang run "$f" --engine=tree) && echo "OK $f"
done

# 基准
go run ./cmd/minilang bench --engine=tree
go run ./cmd/minilang bench

# GC
go run ./cmd/minilang run examples/05-gc-stress.ml --stats
go run ./cmd/minilang run examples/05-gc-stress.ml --stats --gc-off
```

当前状态：以上全部通过。`go test ./...` 覆盖 15 个双引擎一致性用例、
7 个编译期错误用例、6 个运行时错误用例、1 个 GC 存活对象用例、1 个 upvalue close 用例。

---

## 已知限制

- 单函数常量/局部变量/upvalue 上限各 256（单字节操作数），超限是编译错误而不是静默截断
- 没有尾调用优化：VM 调用帧上限 1024，tree 引擎 1000，超限报「调用栈溢出」
- 字符串不可跨行；转义只支持 `\\ \" \n \t \r`
- 标识符只支持 ASCII
- GC 是朴素 STW 标记-清除，不分代不增量不压缩
- 用 Go 承载，所以「回收」是在对象堆链表上摘链，内存真正还给 OS 仍由 Go 的 GC 决定
  —— 本实现管的是**对象何时变成垃圾**，不是**内存何时归还**

## 后续任务（可选）

- [ ] `break` / `continue`（嵌套循环的跳转回填）
- [ ] 匿名函数（表达式位置的闭包）
- [ ] 数组 + 下标（新堆对象 + GC 标记子元素）
- [ ] `Chunk.Lines` 换成 `[]struct{line,col}`，让 VM 运行时错误也带列号
- [ ] 分代/增量 GC，对照当前实现看 STW 变化

---

## 目录

```
minilang/
├── cmd/minilang/main.go        CLI（run / repl / bench，三个 dump 开关）
├── internal/token/             Token 类型与关键字表
├── internal/diag/              带位置的诊断 + 源码片段渲染
├── internal/numfmt/            数字格式化（两引擎共用）
├── internal/lexer/             S1 扫描器
├── internal/ast/               S2 语法树 + 树形打印
├── internal/parser/            S2 递归下降 + Pratt
├── internal/tree/              S3–S4 tree-walking 解释器
├── internal/engine/            S6–S9 字节码运行时（chunk/vm/gc/value/object）
├── internal/compiler/          S6 AST → 字节码
├── test/e2e_test.go            双引擎一致性 + 错误 + GC 验收
├── examples/                   5 个示例 + errors/ 下 5 个错误示例
└── docs/                       00-spec / 01-pipeline / 02-pitfalls / 03-benchmark
```
