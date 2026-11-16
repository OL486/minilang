# 05 — 10 分钟上手 minilang

这篇是给第一次接触 minilang 的人的最短路径：装好、跑起来、写几行、
看清它在内部做了什么。读完应该在 10 分钟内。

语言的完整规则在 [`docs/00-spec.md`](00-spec.md)，
流水线的详细说明在 [`docs/01-pipeline.md`](01-pipeline.md)，
这里只讲「够用」的部分。下面每条命令都实际跑过，输出是真实输出
（机器不同时，计时类数字会不一样）。

---

## 1. 安装并确认能跑

三种方式，任选一种（详细步骤见 README 的「安装」一节）：

```bash
# ① 预编译包：不用装 Go，从 Releases 下载对应平台的压缩包，解开即用
# ② go install：
go install github.com/OL486/minilang/cmd/minilang@latest
# ③ 从源码构建：
git clone https://github.com/OL486/minilang.git
cd minilang
make build
```

确认版本，然后跑第一个示例：

```bash
$ ./minilang version
minilang 0.1.0

$ ./minilang run examples/01-basics.ml
5
9
-4
true
hello, minilang
0 是真值
空串也是真值
true
false
true
false
true
nil
1
3.5
```

看到输出就说明装好了。`run` 可以省略，`./minilang examples/01-basics.ml` 效果一样。

---

## 2. 第一个程序

新建 `hello.ml`：

```minilang
print "你好，minilang";
let name = "Nero";
print "你好，" + name;
```

跑它：

```bash
$ ./minilang run hello.ml
你好，minilang
你好，Nero
```

两个要点：

- 语句以 `;` 结尾；
- `let` 声明变量，`print` 是语句（不是函数，不能 `let p = print;`）。

---

## 3. 语言速览

### 变量与运算符

```minilang
let a = 1 + 2 * 3;
print a;            // 7：* 优先于 +
print 7 / 2;        // 3.5：除法永远是浮点除法
print 7 % 3;        // 1
print "ab" + "cd";  // abcd：+ 可以拼接字符串
print 1 == 1.0;     // true
print 1 == "1";     // false：不同类型 == 返回 false，不报错
```

### 真值：只有 `false` 和 `nil` 是假

```minilang
if (0) { print "0 是真值"; }     // 打印
if ("") { print "空串也是真值"; } // 打印
print nil || "默认值";            // 默认值：|| 返回操作数本身，可当默认值用
```

`&&` / `||` 会短路：右边不会被求值，所以 `false && undefined_thing;`
不会报「未定义变量」。

### 控制流：`if` / `while`，没有 `for` / `break`

```minilang
let i = 0;
let sum = 0;
while (i < 5) {
  sum = sum + i;
  i = i + 1;
}
print sum;  // 10

fn grade(score) {
  if (score >= 90) {
    return "A";
  } else if (score >= 60) {
    return "B";
  } else {
    return "C";
  }
}
print grade(95);  // A
```

`return` 只能出现在函数体内，写在顶层是运行时错误。

### 函数与闭包

```minilang
fn fib(n) {
  if (n < 2) { return n; }
  return fib(n - 1) + fib(n - 2);
}
print fib(10);  // 55

fn makeCounter() {
  let count = 0;
  fn inc() { count = count + 1; return count; }
  return inc;
}
let c = makeCounter();
print c();  // 1
print c();  // 2
```

函数是一等公民：可以存进变量、当参数传、当返回值。
闭包捕获的是**变量**而不是值 —— 两个 `makeCounter()` 的计数器互不干扰，
但共享同一个捕获变量的两个闭包，一个改了另一个能看见。
完整例子见 `examples/03-closures.ml`。

---

## 4. 计时：唯一的内置函数 `clock`

除 `print` 外，语言里只有一个内置：`clock()`，返回当前 Unix 毫秒时间戳：

```bash
$ ./minilang run examples/06-clock-timing.ml
75025
true
true
```

这个示例用 `clock()` 给 `fib(25)` 计时（见 `examples/06-clock-timing.ml`）。
注意它只打印比较结果，不打印原始毫秒数 —— 两个引擎是先后两次运行，
原始毫秒数必然不同，只有比较结果才是确定性的，
`make examples` 才会对两个引擎的输出做 diff 校验。
想看真实耗时，把 `elapsed` 直接 `print` 出来就行。

---

## 5. 两个引擎：同一份代码，两种执行方式

minilang 有两个执行引擎：

- `vm`（默认）：源码 → 字节码 → 栈式虚拟机；
- `tree`：直接遍历语法树解释执行，是 VM 的语义参照物。

```bash
$ ./minilang run examples/03-closures.ml --engine=tree
```

两个引擎的输出必须逐字节相同 —— 这条约束由 `test/e2e_test.go` 强制。
自己验证：

```bash
$ diff <(./minilang run examples/03-closures.ml) \
        <(./minilang run examples/03-closures.ml --engine=tree) \
  && echo "两个引擎输出相同"
两个引擎输出相同
```

如果哪天两个引擎输出不一致，先用 `--engine=tree` 的结果当正确答案，
再去 VM 里找 bug —— 这就是保留 tree 引擎的原因。

---

## 6. 看清流水线：三个 dump 开关

程序内部走「词法 → 语法 → 编译 → 执行」四步，三个开关可以逐段检查。
拿这个两行程序当例子：

```minilang
let x = 1;
print x + 2;
```

```bash
$ ./minilang run /tmp/tut-dump.ml --dump-tokens
#     TYPE         LEXEME                 LITERAL            POS
0     LET          "let"                  -                  1:1
1     IDENT        "x"                    -                  1:5
2     ASSIGN       "="                    -                  1:7
3     NUMBER       "1"                    1                  1:9
4     SEMICOLON    ";"                    -                  1:10
5     PRINT        "print"                -                  2:1
6     IDENT        "x"                    -                  2:7
7     PLUS         "+"                    -                  2:9
8     NUMBER       "2"                    2                  2:11
9     SEMICOLON    ";"                    -                  2:12
10    EOF          "<eof>"                -                  3:1
3

$ ./minilang run /tmp/tut-dump.ml --dump-ast
Let(x)
  Init:
    Literal(1)
Print
  Binary(+)
    Var(x)
    Literal(2)
3

$ ./minilang run /tmp/tut-dump.ml --dump-bytecode
== tut-dump.ml ==
0000    1 OP_CONSTANT             0 '1'
0002    | OP_DEFINE_GLOBAL        1 'x'
0004    2 OP_GET_GLOBAL           2 'x'
0006    | OP_CONSTANT             3 '2'
0008    | OP_ADD
0009    | OP_PRINT
0010    | OP_NIL
0011    | OP_RETURN
3
```

三个开关可以任意组合。它们输出到 stdout，报错输出到 stderr，
所以 `minilang run x.ml > out.txt` 拿到的是干净的程序输出。
三个 dump 怎么读，见 [`docs/01-pipeline.md`](01-pipeline.md)。

---

## 7. REPL：交互式解释器

```bash
$ ./minilang repl
minilang 0.1.0  [engine=vm]  :help 查看命令，:quit 退出
> let x = 6;
> print x * 7;
42
> :stats
-- stats: 存活对象 6, 堆 305 B, GC 次数 0, 累计创建对象 6
> :quit
```

- 多行输入：括号/花括号没配平会显示 `| ` 续行提示，配平后一次执行；
- 上一行 `let` 的变量下一行能看见（会话复用同一个 VM/解释器）；
- 命令只有四个：`:help`、`:quit`、`:gc`（立即回收）、`:stats`（堆统计）。

---

## 8. 看性能：`--stats` 与 `bench`

```bash
$ ./minilang run examples/04-recursion.ml --stats
55
6765
3628800
2.43290200817664e+18
5050
3
2
1
-- stats: 存活对象 31, 堆 1582 B, GC 次数 0, 累计创建对象 31
```

`bench` 跑 `fib(30)` 五次取中位数（VM 引擎，另加 `--engine=tree` 可看另一边）：

```bash
$ ./minilang bench
minilang 基准测试  engine=vm  fib(30)
  fib(30) = 832040
  五次耗时: 588.4 ms, 584.0 ms, 546.0 ms, 568.8 ms, 613.9 ms
  中位数:   584.0 ms
  VM 对象堆: 运行前 5 对象 278 B -> 运行后 17 对象 758 B（GC 0 -> 0 次，累计创建 17 对象）
  ……
```

数字因机器而异，别跟文档里的数字较真 ——
完整数据与测量口径见 [`docs/03-benchmark.md`](03-benchmark.md)。
想对比两个引擎的加速比，跑 `make bench`（两个引擎各跑一次）。

---

## 9. 常见坑（先知道这五条）

完整分析在 [`docs/02-pitfalls.md`](02-pitfalls.md)，这里只列结论：

1. `print` 是语句不是函数，不能赋值、不能当参数传；
2. `0` 和 `""` 都是真值，只有 `false` 和 `nil` 是假；
3. 没有 `break` / `continue`，循环里想提前出去用 `return`（写在函数里）；
4. `clock` 是普通全局变量，可以被 `let clock = 1;` 覆盖（允许，不保护）；
5. 数字没有整数类型，`7 / 2` 是 `3.5`，不是 `3`；很大的整数会按科学计数法打印
   （如 `fact(20)` 输出 `2.43290200817664e+18`）。

---

## 10. 下一步

- 照着 `examples/` 把 6 个示例都跑一遍，再自己改着玩；
- 按 README 末尾的扩展练习给语言加功能（数组、`break`、字符串内置函数……），
  每道题都有难度分级；
- 想理解实现：按 [`docs/01-pipeline.md`](01-pipeline.md) 的顺序读源码，
  `internal/lexer` → `internal/parser` → `internal/treewalk` → `internal/compiler` → `internal/vm`；
- 想确认改动没弄坏东西：`make check`（格式 + vet + 测试），改示例后跑 `make examples`。
