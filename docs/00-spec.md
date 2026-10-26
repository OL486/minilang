# minilang 语言规范（S0 产出）

这份文档是实现的唯一依据。代码与文档冲突时，先改文档再改代码 —— 反过来会让
「读代码猜语义」变成这个项目的日常。

版本：v0.1.0

---

## 1. 类型系统

只有 5 种类型，没有用户自定义类型、没有数组、没有哈希表。

| 类型 | 内部表示 | 字面量 |
|---|---|---|
| `number` | IEEE 754 float64 | `1` `3.14` `0.5` |
| `string` | 不可变 UTF-8 字符串 | `"hello"` |
| `bool` | true / false | `true` `false` |
| `nil` | 空值 | `nil` |
| `function` | 用户函数或内置函数 | 无字面量，由 `fn` 产生 |

没有独立的整数类型：`1` 和 `1.0` 是同一个值。这是刻意的取舍 ——
一旦区分整数和浮点，`/` 就要定义两套语义（整除还是浮点除），
而本项目的目标是理解执行模型，不是数值语义。

---

## 2. 词法

### 2.1 空白与注释

空白：空格、制表符、回车、换行。全部忽略，但换行会推进行号。

注释：`//` 到行尾。**没有块注释** —— 块注释会让「行号 → 源码片段」的映射
在报错时出现歧义（一行代码可能落在注释中间），而位置准确是第一优先级。

### 2.2 标识符

```
ident := (letter | "_") (letter | digit | "_")*
letter := "a".."z" | "A".."Z"
```

标识符只支持 ASCII。中文不能做变量名（中文只能出现在字符串和注释里）。

### 2.3 数字

```
number := digit+ ("." digit+)?
```

不支持指数形式（`1e9`）、十六进制、前导负号（`-1` 是一元运算）。
`1.2.3` 是词法错误，报错位置指向第二个小数点。

### 2.4 字符串

```
string := '"' char* '"'
char   := 除换行和未转义双引号外的任意字符
```

转义序列：`\\` `\"` `\n` `\t` `\r`。未知转义（如 `\q`）是词法错误。

字符串**不能跨行**。未闭合时报错位置指向开引号。

### 2.5 关键字

```
let  fn  if  else  while  return  print  true  false  nil
```

这 11 个是保留字，不能作为变量名。注意 `print` 是语句关键字，不是函数。

---

## 3. 文法（BNF）

```
program      := declaration* EOF
declaration  := letDecl | fnDecl | statement

letDecl      := "let" IDENT "=" expression ";"
fnDecl       := "fn" IDENT "(" paramList? ")" block
paramList    := IDENT ("," IDENT)*

statement    := ifStmt | whileStmt | returnStmt | printStmt | block | exprStmt
ifStmt       := "if" "(" expression ")" block ("else" (ifStmt | block))?
whileStmt    := "while" "(" expression ")" block
returnStmt   := "return" expression? ";"
printStmt    := "print" expression ";"
block        := "{" declaration* "}"
exprStmt     := expression ";"

expression   := assignment
assignment   := logicalOr ("=" assignment)?
logicalOr    := logicalAnd ("||" logicalAnd)*
logicalAnd   := equality ("&&" equality)*
equality     := comparison (("==" | "!=") comparison)*
comparison   := term (("<" | "<=" | ">" | ">=") term)*
term         := factor (("+" | "-") factor)*
factor       := unary (("*" | "/" | "%") unary)*
unary        := ("!" | "-") unary | call
call         := primary ("(" argList? ")")*
argList      := expression ("," expression)*
primary      := NUMBER | STRING | "true" | "false" | "nil"
              | IDENT | "(" expression ")"
```

注意两处强制的语法约束（不满足就是语法错误，不做容错）：

- `if` / `while` 的条件**必须**用括号包裹。`if x > 1 {` 是错误。
- 每条语句**必须**以分号结束。`print x` 换行不会自动补分号。

`else if` 不是独立文法，它等价于 `else` 后面跟一个 `ifStmt`，
所以 `else` 的右侧既接受 `block` 也接受 `ifStmt`。

---

## 4. 运算符优先级与结合性

数值越大结合越紧。这张表同时是 `internal/parser` 里 `binaryPrec` 的实现依据。

| 优先级 | 运算符 | 结合性 | 说明 |
|---|---|---|---|
| 1 | `=` | 右 | 赋值表达式，左值必须是变量名 |
| 2 | `\|\|` | 左 | 短路 |
| 3 | `&&` | 左 | 短路 |
| 4 | `==` `!=` | 左 | 允许跨类型（返回 false） |
| 5 | `<` `<=` `>` `>=` | 左 | 要求同类型且为 number 或 string |
| 6 | `+` `-` | 左 | `+` 同时用于 number 加法和 string 拼接 |
| 7 | `*` `/` `%` | 左 | 仅 number |
| 8 | `!` `-`（一元） | 右 | |
| 9 | `()` 调用 | 左 | 最高优先级 |

`=` 是表达式而不是语句，所以 `print x = 1;` 合法，`x = y = 7;` 从右往左结合。

---

## 5. 语义规则

### 5.1 真值（truthiness）

**只有 `false` 和 `nil` 是假**，其余一切都是真 —— 包括 `0`、`0.0`、`""`。

理由：minilang 没有静态类型。如果 `0` 是假，`if (n)` 在 `n` 为 0 时会静默走
else 分支，而这种错误在运行时毫无提示。把规则压缩到两条，是最可预测的选择。

### 5.2 相等 `==` / `!=`

| 左 | 右 | 结果 |
|---|---|---|
| 任意 | 任意（类型不同） | `false`（不报错） |
| `nil` | `nil` | `true` |
| `bool` | `bool` | 值相等 |
| `number` | `number` | 值相等 |
| `string` | `string` | 内容相等（靠驻留实现指针比较） |
| `function` | 同一个函数对象 | `true` |

跨类型比较不报错，因为 `x == nil` 是常用写法，报错会让它写不出来。

### 5.3 大小比较

只接受 `number` vs `number`（数值比较）和 `string` vs `string`（按字节序的字典序）。
其余组合是运行时错误。

### 5.4 算术

| 运算 | 允许类型 | 备注 |
|---|---|---|
| `+` | number+number 或 string+string | 混合类型报错，不做隐式转换 |
| `-` `*` `/` `%` | number+number | |
| 一元 `-` | number | |
| 一元 `!` | 任意 | 返回 `!truthy(x)` |

`/` 和 `%` **不做除零检查**：`1/0` 得到 `+inf`，`0/0` 得到 `nan`，遵循 IEEE 754。
打印出来是 `+inf` / `-inf` / `nan`。

### 5.5 逻辑运算

`&&` 和 `||` 短路，并且**返回操作数本身**，不返回 bool：

```
nil || "默认值"   // => "默认值"
1 && 2           // => 2
false && f()     // => false，f() 不会被调用
```

### 5.6 作用域

- 词法作用域，`{ }` 引入新作用域。
- 内层可以遮蔽外层同名变量。
- **同一作用域内重复声明同名变量是错误**（VM 引擎在编译期报，tree 引擎在运行期报）。
- 顶层 `let` 是全局变量；函数体内的 `let` 是局部变量。
- 变量必须先声明后使用；给未声明的变量赋值是运行时错误（没有隐式全局）。

### 5.7 函数与闭包

- 函数是一等公民：可以存进变量、当参数传、当返回值、作为调用目标。
- 只有具名函数声明，**没有匿名函数 / lambda**（列入扩展练习）。
- 参数按值传递；参数个数必须严格匹配，不匹配是运行时错误。
- 闭包按**引用**捕获变量（不是快照）：多个闭包共享同一个捕获变量时，
  一个改了另一个能看见。
- 没有默认参数、没有可变参数、没有 `this`。

### 5.8 控制流

- `if` 条件为假且没有 `else` 时，什么都不做。
- `while` 是唯一的循环，没有 `break` / `continue`（列入扩展练习）。
- `return` 只能出现在函数体内；在顶层出现是运行时错误。
- 函数执行到末尾没有 `return` 时，返回 `nil`。

### 5.9 内置

| 名字 | 签名 | 说明 |
|---|---|---|
| `print` | 语句 `print expr;` | 输出一行。**不是函数**，不能 `let p = print;` |
| `clock` | `fn clock() -> number` | 当前 Unix 毫秒时间戳 |

只有这两个。`clock` 是全局变量，可以被用户 `let clock = 1;` 覆盖（允许，不保护）。

---

## 6. 资源上限

超过上限一律报错，不静默截断。

| 上限 | 值 | 触发时的行为 |
|---|---|---|
| 单函数常量池 | 256 | 编译错误 |
| 单函数局部变量 | 256 | 编译错误 |
| 单函数 upvalue | 256 | 编译错误 |
| 调用实参个数 | 255 | 编译错误 |
| 跳转距离 | 65535 字节 | 编译错误 |
| 操作数栈 | 32768 槽（VM） | 运行时错误 |
| 调用帧 | 1024（VM）/ 1000（tree） | 运行时错误 |
| 单次报错数量 | 20 条 | 之后停止解析 |

前 5 条来自「单字节操作数」这个设计决定，见 `docs/01-pipeline.md`。

---

## 7. 错误信息约定

错误信息由三部分组成，**不允许出现「unexpected token」这类没有位置的报错**：

```
<阶段>错误 (第 <行> 行, 第 <列> 列): <具体描述>
  <行号> | <源码行>
       |    ^
```

阶段标签有四种，对应流水线的四个位置：

| 阶段 | 前缀 | 产生位置 | 有列号 | 例子 |
|---|---|---|---|---|
| 词法 | `词法错误` | `internal/lexer` | 是 | 字符串未闭合、非法字符、数字里有两个小数点 |
| 语法 | `语法错误` | `internal/parser` | 是 | 缺分号、缺右括号、表达式开头非法 |
| 编译 | `编译错误` | `internal/compiler` | 否 | 重复声明、常量池溢出 |
| 运行时 | `运行时错误` | `engine` / `tree` | VM 无、tree 有 | 类型不匹配、未定义变量、栈溢出 |

为什么字节码 VM 的运行时错误没有列号：`Chunk.Lines` 是「每字节一个行号」，
只记录行不记录列。要支持列号就得把 `Lines` 换成 `[]struct{line,col}`，
每个字节多 4 字节 —— 本项目的取舍是「VM 侧只给行号」，因为到了字节码层面，
列号的意义已经不大（一条指令可能对应多个源码位置）。

**没有数字错误码**。消息本身就是契约，测试断言的是消息里的关键字
（见 `test/e2e_test.go` 的 `TestErrors`）。加数字码会让「改文案」变成破坏性变更，
而这条流水线上还没有需要机器识别的错误分类需求。

---

## 8. 目录结构

```
minilang/
├── go.mod                          module minilang（零外部依赖）
├── cmd/minilang/main.go            CLI：run / repl / bench，三个 dump 开关
├── internal/
│   ├── token/token.go              Token 类型与关键字表
│   ├── diag/diag.go                带位置的诊断 + 源码片段渲染
│   ├── numfmt/numfmt.go            数字格式化（两个引擎共用，保证输出一致）
│   ├── lexer/lexer.go              S1 扫描器
│   ├── ast/ast.go                  S2 语法树 + 树形打印（--dump-ast）
│   ├── parser/parser.go            S2 递归下降 + Pratt
│   ├── tree/                        S3–S4 tree-walking 解释器
│   │   ├── env.go                  环境链
│   │   └── interp.go               求值器
│   ├── engine/                      S6–S9 字节码运行时
│   │   ├── value.go                Value 表示 + 相等语义 + 格式化
│   │   ├── object.go               堆对象（string/function/closure/upvalue/native）
│   │   ├── chunk.go                Op 定义 + Chunk + 反汇编（--dump-bytecode）
│   │   ├── vm.go                   分派循环、调用帧、upvalue 捕获/关闭
│   │   └── gc.go                   标记-清除
│   └── compiler/compiler.go        S6 AST → 字节码
├── test/e2e_test.go                双引擎一致性 + 错误信息 + GC 验收
├── examples/                       可运行示例（含 errors/ 子目录）
└── docs/                           本目录
```

数据流：

```
源码 ──lexer──> []Token ──parser──> []ast.Stmt ──compiler──> *ObjFunction ──vm──> 结果
                   │                    │                          │
              --dump-tokens        --dump-ast                --dump-bytecode
                   │                    │                          │
                   └──────────── 也可以直接交给 tree 引擎 ───────────┘
                                 (--engine=tree)
```

两个引擎共享同一份 Token 和 AST，只在最后一步分叉。这是双引擎对比测试能成立的前提。
