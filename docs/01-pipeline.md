# 流水线走读：三个 dump 怎么用

这三个开关是排错能力的全部基础。没有它们，任何 bug 都只能靠「改一行、跑一次、猜一次」。
下面用同一个程序走完三站，所有输出都是真实运行结果（`go run ./cmd/minilang run p.ml --dump-xxx`）。

被测程序 `p.ml`：

```minilang
fn add(a, b) { return a + b; }
let x = 1 + 2 * 3;
if (x > 5) { print add(x, 1); } else { print "small"; }
```

---

## 第一站：`--dump-tokens`

```
#     TYPE         LEXEME                 LITERAL            POS
0     FN           "fn"                   -                  1:1
1     IDENT        "add"                  -                  1:4
2     LPAREN       "("                    -                  1:7
...
```

完整表在终端里是 30 多行。**看什么**：

1. **分类对不对**。`add` 是 `IDENT` 而不是关键字；`1` 是 `NUMBER` 且 `LITERAL` 列是 `1`（已转成 float64）。
2. **`POS` 是 1 基的行:列，按 rune 计数**。中文注释不会把后面的列号算歪。
3. **这里看不到优先级**。`1 + 2 * 3` 的 token 流是 `NUMBER PLUS NUMBER STAR NUMBER`，
   `*` 和 `+` 完全平等 —— 谁先算由 parser 的优先级表决定。
   如果在 token 阶段就想「看到」优先级，说明层次搞错了。

**这一站能抓到什么 bug**：字符串转义、数字解析、关键字表遗漏、行列号漂移。
抓不到：任何关于结构的问题。

---

## 第二站：`--dump-ast`

```
Fn(add) params=[a, b]
  Body:
    Return
      Binary(+)
        Var(a)
        Var(b)
Let(x)
  Init:
    Binary(+)
      Literal(1)
      Binary(*)
        Literal(2)
        Literal(3)
If
  Cond:
    Binary(>)
      Var(x)
      Literal(5)
  Then:
    Print
      Call
        Callee:
          Var(add)
        Args:
          Var(x)
          Literal(1)
  Else:
    Print
      Literal(string "small")
```

**看什么**：`Binary(+)` 的右子节点是 `Binary(*)`，不是反过来。
这就是 Pratt 解析器验收的全部内容 —— `*` 必须是 `+` 的子节点。

**为什么把 `Group`（括号节点）保留在树里**：如果解析时把括号直接丢掉，
`(1+2)*3` 和 `1+2*3` 的树会不一样（因为优先级不同），但 `(1+2)` 和 `1+2` 的树会一样。
保留 `Group` 节点，你才能确认「括号真的被解析了」，而不是碰巧优先级对了。
代价是编译器要多处理一种节点（直接透传，两行代码）。

**这一站能抓到什么 bug**：结合性写反（`1-2-3` 变成 `1-(2-3)`）、`else` 挂错 `if`、
调用与二元的优先级错乱（`f(1)+2` 解析成 `f(1+2)`）。抓不到：任何关于语义的问题。

---

## 第三站：`--dump-bytecode`

```
== p.ml ==
0000    1 OP_CLOSURE              0 'add' upvalues=0
0002    | OP_DEFINE_GLOBAL        1 'add'
0004    2 OP_CONSTANT             2 '1'
0006    | OP_CONSTANT             3 '2'
0008    | OP_CONSTANT             4 '3'
0010    | OP_MULTIPLY
0011    | OP_ADD
0012    | OP_DEFINE_GLOBAL        5 'x'
0014    3 OP_GET_GLOBAL           6 'x'
0016    | OP_CONSTANT             7 '5'
0018    | OP_GREATER
0019    | OP_JUMP_IF_FALSE       13 -> 0035
0022    | OP_POP
0023    | OP_GET_GLOBAL           8 'add'
0025    | OP_GET_GLOBAL           9 'x'
0027    | OP_CONSTANT            10 '1'
0029    | OP_CALL                 2
0031    | OP_PRINT
0032    | OP_JUMP                 4 -> 0039
0035    | OP_POP
0036    | OP_CONSTANT            11 'small'
0038    | OP_PRINT
0039    | OP_NIL
0040    | OP_RETURN

  == add ==
  0000    1 OP_GET_LOCAL            1
  0002    | OP_GET_LOCAL            2
  0004    | OP_ADD
  0005    | OP_RETURN
  0006    | OP_NIL
  0007    | OP_RETURN
```

四列的含义：**字节偏移**、**源码行号**（同一行不重复打印，用 `|` 占位）、
**opcode**、**操作数**。常量类指令的操作数后面带 `'值'`，是常量池里那一项的内容。

### 逐段读

**`1 + 2 * 3` → `CONST 1, CONST 2, CONST 3, MULTIPLY, ADD`**

栈式字节码是**后缀序**。中缀表达式 `1 + (2 * 3)` 转后缀就是 `1 2 3 * +`。
`MULTIPLY` 弹出栈顶两个值压回一个，`ADD` 同理。如果反汇编出来是
`1 2 + 3 *`，说明优先级表反了。

**`OP_JUMP_IF_FALSE 13 -> 0035`**

偏移量 13，目标 35。验算：`19 + 3 + 13 = 35`（指令起点 + 指令长度 + 偏移）。
这条跳转跳过的是 then 分支（`0022`–`0032`）。

`OP_JUMP_IF_FALSE` **不弹栈**：条件值留在栈上，由分支自己 `OP_POP`。
`0022` 和 `0035` 各有一个 `OP_POP`，分别是 then 和 else 的入口。
这样每条路径都恰好弹一次，不需要在跳转指令里做「跳的时候弹、不跳的时候也弹」的分支。

**`OP_JUMP 4 -> 0039`**

then 分支执行完要跳过 else。`32 + 3 + 4 = 39`。

**为什么是回填而不是两遍编译**

编译到 `OP_JUMP_IF_FALSE` 这一条时，else 的起始位置还不知道（then 分支还没编译）。
所以先写两个 `0xff` 占位，等编译到目标位置再回头改。核心就两个函数：

```go
// internal/compiler/compiler.go
func (c *Compiler) emitJump(op engine.Op, line int) int {
	c.emit(op, line)
	c.emitByte(0xff, line)   // 占位：高字节
	c.emitByte(0xff, line)   // 占位：低字节
	return len(c.chunk().Code) - 2   // 返回操作数的位置
}

func (c *Compiler) patchJump(offset int) {
	jump := len(c.chunk().Code) - offset - 2
	c.chunk().Code[offset] = byte(jump >> 8)
	c.chunk().Code[offset+1] = byte(jump)
}
```

那个 `- 2` 是唯一容易错的地方。偏移是相对**操作数之后**那个字节算的，
不是相对指令开头。少减 2 的表现是「每个跳转都多走两条指令」，
在 `if` 里通常还能跑（多执行的恰好是下一条指令），在 `while` 里就会变成死循环或多跳一轮。

**`OP_CLOSURE 0 'add' upvalues=0`**

`add` 不捕获任何外层变量，所以 `upvalues=0`，后面没有附加字节。
对比 `makeCounter` 的例子（`examples/03-closures.ml`）：

```
0002    3 OP_CLOSURE              1 'inc' upvalues=1
0004    |                        local[1]
```

`local[1]` 表示「捕获当前帧的第 1 号栈槽」（槽位 0 是函数自身，所以 1 就是 `count`）。
这两个字节会被 `OP_CLOSURE` 的运行时逻辑读走，用来构造 upvalue。

**嵌套函数会递归打印**，缩进两格。只打顶层 chunk 的话，闭包 bug 完全没法看。

**`add` 里的 `OP_GET_LOCAL 1` / `OP_GET_LOCAL 2`**

参数 `a`、`b` 分别落在槽位 1、2（槽位 0 是函数自己）。
这就是 VM 比 tree-walking 快的主要原因：取变量是一次数组下标，
而 tree-walking 要沿 `map` 链查名字。

---

## 三个开关的组合用法

| 症状 | 先看哪一站 |
|---|---|
| 报错位置不对、字符串内容怪 | `--dump-tokens` |
| 优先级/结合性不对、`else` 挂错 | `--dump-ast` |
| 结果算错但 AST 是对的、死循环、变量串值 | `--dump-bytecode` |
| 两个引擎结果不一致 | 先各跑一遍对比输出，再 `--dump-ast` 确认树相同，问题一定在编译或 VM |

一次全开也行，输出是按流水线顺序拼起来的，不会交错：

```bash
minilang run examples/03-closures.ml --dump-tokens --dump-ast --dump-bytecode
```
