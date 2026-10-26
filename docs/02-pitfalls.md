# 五个已知陷阱：本实现怎么处理，以及换写法会出什么 bug

这一篇只讲「为什么必须这样写」。代码里的注释是简版，这里是完整版。

---

## 陷阱 1：局部变量的槽位

### 两种设计

**相对偏移**：`OP_SET_LOCAL n` 表示「从栈顶往下第 n 个」。
**绝对槽位**：`OP_SET_LOCAL n` 表示「本帧的槽位 0 加 n」，而槽位 0 在帧里的位置由
`callFrame.slotBase` 记录。

**本实现用绝对槽位**。`vm.go` 里的读法是：

```go
case OpSetLocal:
    slot := int(chunk.Code[f.ip])
    f.ip++
    vm.stack[f.slotBase+slot] = vm.peek(0)
```

### 为什么相对偏移容易差一位

赋值表达式的值要留在栈上（`x = y = 1` 需要中间结果）。所以编译 `x = 1` 时的指令序列是：

```
<编译右值>       ; 栈顶 +1
OP_SET_LOCAL n   ; 把栈顶写到第 n 个变量
```

如果 `n` 是相对偏移，编译期算出的 `n` 必须包含「右值已经压栈」这个事实。
编译期在**编译左值时**还不知道右值会压几个槽（可能是 `f(g(h()))`，压了好几个）。
于是要么在编译完右值之后再算偏移（可行，但每个赋值点都要记住「右值深度」），
要么固定减 1（右值恰好占一个槽时才对）。

一旦算错，写入的是**隔壁槽位**：变量 `a` 被赋成了 `b` 的值，或者干脆覆盖了一个临时值。
这类 bug 的特征是：

- 只有「赋值表达式的值被用到」时才发作，`x = 1;` 单独一句看不出来；
- 只在某个变量恰好排在另一个变量前面时发作，改个声明顺序就消失。

绝对槽位没有这个问题：`x` 是第几个局部变量，编译期就定死了，
和运行时栈顶在哪、右值压了几个槽完全无关。代价是 VM 每次访问局部变量要多加一次
`slotBase`（一次整数加法，可以忽略）。

### 真正会咬人的地方

绝对槽位的前提是**编译期的 locals 下标与运行期的栈布局严格一致**。
本实现里这个一致性由三处约定维持，改任何一处都要同时检查另外两处：

1. `declareLocal` 把变量 append 到 `fnState.locals`，**下标即槽位**；
2. `let` 语句不发射任何「存」指令 —— 初始化表达式的值本来就在栈顶，
   那个位置就是新变量的槽位；
3. 退出作用域时 `endScope` 按 locals 逆序发射 `OP_POP`（或 `OP_CLOSE_UPVALUE`），
   把槽位弹掉。

第 2 条最容易被破坏。如果给 `let` 加上「先 POP 再 SET」或者「先 SET 再 PUSH」，
槽位就会整体偏移一格，而且**只在同一个作用域里有多个局部变量时**才表现为错值。

---

## 陷阱 2：跳转指令的回填

`if` / `while` 编译时目标地址还不知道。本实现先发两个 `0xff` 占位，编译到目标再回填。

`if` 的形状（`internal/compiler/compiler.go` 的 `ifStmt`）：

```
<cond>
OP_JUMP_IF_FALSE  -> else 或 end
OP_POP                       ; then 入口弹掉条件值
<then 体>
OP_JUMP           -> end     ; 跳过 else
else:
OP_POP                       ; else 入口弹掉条件值
<else 体>
end:
```

`while` 的形状（`whileStmt`）：

```
loop_start:                  ; 记录这里的字节偏移
<cond>
OP_JUMP_IF_FALSE -> exit
OP_POP
<body>
OP_LOOP          -> loop_start
exit:
OP_POP                       ; 条件为假时弹掉条件值
```

`OP_LOOP` 是**后向**跳转，偏移是「往回退多少」，所以计算方式与 `OP_JUMP` 不同：

```go
func (c *Compiler) emitLoop(loopStart, line int) {
	c.emit(engine.OpLoop, line)
	// +2 是操作数自身的长度：VM 读完操作数后 ip 已指向下一条指令，
	// 要减掉「opcode + 2 字节操作数」才能回到循环开头。
	offset := len(c.chunk().Code) - loopStart + 2
	c.emitShort(offset, line)
}
```

对照 VM 侧：

```go
case OpJump:        f.ip += 2 + jump    // 前向
case OpJumpIfFalse: f.ip += 2; if !truthy { f.ip += jump }
case OpLoop:        f.ip += 2 - jump    // 后向
```

### 三个必错的点

1. **`patchJump` 里的 `- 2`**。偏移相对操作数之后那个字节，不是相对指令开头。
   少减 2 → 每次跳转都多走两条指令。
2. **`OP_JUMP_IF_FALSE` 弹不弹栈**。本实现不弹，由分支入口弹。
   如果改成「跳的时候弹、不跳的时候不弹」，then 分支入口就不需要 `OP_POP` 了，
   但 else 分支入口还需要 —— 两个分支的栈状态不再对称，
   后续任何「在 if 之后读栈顶」的代码都会读到脏值。
3. **`endScope` 与跳转的先后顺序**。then 分支里的局部变量必须在 `OP_JUMP` **之前**弹掉，
   否则跳出 then 时栈上多留了几个槽位，`end` 处的栈深度与 `else` 分支不一致，
   而 VM 不做栈深度校验，错误会一路传染到下一次函数调用。

---

## 陷阱 3：upvalue 的 open / close

这是整个项目最难的一段。

### 问题

```minilang
fn makeCounter() {
  let count = 0;
  fn inc() { count = count + 1; return count; }
  return inc;
}
```

`inc` 里的 `count` 是 `makeCounter` 的局部变量，住在**栈槽**上。
但 `makeCounter` 返回后，它的栈帧就没了，栈槽会被后续调用复用。
所以 `inc` 不能直接保存「槽位 1 的地址」——那个地址之后会变成别人的变量。

### 解法：open → close

- **open**：变量还在栈上时，`ObjUpvalue.Location` 直接指向那个栈槽。
  读写都作用在栈上，所以 `inc` 和 `makeCounter` 看到的是**同一个存储**。
- **close**：变量所在作用域即将退出（`endScope` / `OP_RETURN`）时，
  把值**拷贝**到 `ObjUpvalue.Closed` 字段，然后把 `Location` 改指向 `Closed`。
  从此这个 upvalue 自己持有值，不再依赖栈。

### 分步走一遍

```
执行 makeCounter()：
  栈: [makeCounter闭包, count=0, inc闭包]
                              ^槽位1

编译 inc 时知道要捕获 local[1]。
OP_CLOSURE 执行：captureUpvalue(slotBase+1)
  -> 新建 uv{Slot:1, Location:&stack[1]}
  -> openUpvalues = uv

执行 return inc;  ->  OP_RETURN:
  closeUpvalues(slotBase)
     uv.Slot(1) >= slotBase(0)  -> 关闭
     uv.Closed = *uv.Location   // 0 被拷到堆上
     uv.Location = &uv.Closed
     openUpvalues = nil
  sp = slotBase; push(inc闭包)

之后调用 c1()：
  OP_GET_UPVALUE 0 -> *uv.Location -> 0
  ... 自增 ... -> *uv.Location = 1   // 写到堆上的 Closed，栈槽已被复用也无所谓
```

### 链表必须按槽位降序

`closeUpvalues(last)` 是「关掉所有槽位 ≥ last 的 upvalue」。它从链表头开始扫，
遇到第一个 `Slot < last` 就停：

```go
for vm.openUpvalues != nil && vm.openUpvalues.Slot >= last {
    uv := vm.openUpvalues
    uv.Closed = *uv.Location
    uv.Location = &uv.Closed
    vm.openUpvalues = uv.Next
}
```

「遇到第一个就停」只有在链表**从大到小**排列时才正确。如果链表无序，
扫到中间就会提前停止，后面本该关闭的 upvalue 留在 open 状态，
继续指向已经被复用的栈槽 —— 表现是「闭包里的变量莫名变成了别人的值」，
而且只在**同一个作用域捕获了多个变量**时出现（单个变量时链表只有一个节点，无序也有序）。

插入时维持顺序：

```go
for cur != nil && cur.Slot > slot {   // 找到第一个不大于 slot 的节点
    prev = cur
    cur = cur.Next
}
if cur != nil && cur.Slot == slot {
    return cur                         // 同一槽位复用同一个 upvalue
}
```

### 为什么用 `Slot int` 而不是比较指针

C 版本的实现通常直接比较 `Location` 指针大小（`upvalue->location > last`）。
**Go 不允许对指针做 `<` `>` 比较**（只允许 `==` `!=`），所以本实现额外存了 `Slot int`。
这不是「等价替换」那么简单：`Slot` 只在 open 状态有效，close 之后它就是脏数据 ——
如果哪天有人拿一个 closed upvalue 的 `Slot` 去比较，会得到一个看似合理的错误结果。
所以 `closeUpvalues` 和 `captureUpvalue` 是唯一允许读 `Slot` 的两个函数。

### 同一槽位必须共享 upvalue

`captureUpvalue` 里的 `if cur.Slot == slot { return cur }` 不能省。
两个内层函数捕获同一个变量时，必须拿到**同一个** `ObjUpvalue`，
否则一个改了另一个看不到 —— 语义上就是「闭包捕获变量」退化成「闭包捕获值」。

---

## 陷阱 4：GC 的根集合

标记-清除最容易出的 bug 是「对象还在用却被回收了」。根集合漏一项就会偶发崩溃。

### 本实现的 5 类根（`internal/engine/gc.go` 的 `markRoots`）

| # | 根 | 为什么必须在这里 |
|---|---|---|
| 1 | VM 栈 `[0, sp)` | 活跃的临时值和局部变量。**只扫到 sp**，sp 之后是脏数据 |
| 2 | 调用帧里的闭包 | 帧刚建立、还没执行第一条指令时，闭包只在帧里 |
| 3 | 全局变量表 | 顶层 `let` |
| 4 | open upvalue 链表 | 它们指向栈，但 upvalue 对象本身也要活 |
| 5 | 编译期临时根 `roots` | 编译器手里的 `ObjFunction` 不在 VM 栈上 |

**「当前正在编译的常量池」不在这个列表里，但它被间接保住了**：
常量池属于某个 `ObjFunction`，而那个函数被第 5 类根按住。
编译嵌套函数时，外层函数已经在根上，内层函数在编译期间被临时压入根栈，
编译完立刻放进外层函数的常量池 —— 于是它从「临时根」平滑过渡到「常量池里的引用」。

### 一个反直觉的顺序约束

```go
func (vm *VM) allocate(h *objHeader, typ ObjType, self Object, size int) {
	if vm.gcEnabled && vm.bytesAllocated+size > vm.nextGC {
		vm.collectGarbage()      // 先回收
	}
	h.typ = typ
	h.marked = false
	...
	h.next = vm.objects           // 再挂链
	vm.objects = h
}
```

**必须先 GC 再挂链**。反过来（先挂链再 GC）的话，新对象已经进入对象堆，
但还没有任何根指向它 —— 这一轮 GC 会立刻把它扫掉。
表现是「偶发地把刚创建的字符串打成 nil」，只在内存压力刚好卡在阈值上时复现。

对应的推论：**任何 `allocate` 之后，必须马上让新对象可被根到达**。
`InternString` 里是紧接着插入驻留表，`OP_CLOSURE` 里是紧接着 `push`，
编译器里是紧接着 `PushRoot`。这三处都不能插入其他分配操作。

### 实际踩到的坑：typed nil

本实现在开发过程中真的崩过一次，堆栈指向 `(*ObjUpvalue).header` 的 nil 解引用。

原因是 `OP_CLOSURE` 的执行顺序：

```go
cl := vm.newClosure(fn)     // 分配，此时 Upvalues[0..n] 全是 nil
vm.push(ObjValue(cl))       // 先入栈（成为根）
for i := 0; i < fn.UpvalueCount; i++ {
    cl.Upvalues[i] = vm.captureUpvalue(...)   // 每个 capture 都可能触发 GC
}
```

填第一个 upvalue 时如果触发 GC，`Upvalues[1..]` 还是 nil。
`markObject(uv)` 收到的是一个 `*ObjUpvalue(nil)` ——
**装进 `Object` 接口之后，它不等于 nil 接口**，`o == nil` 判断为 false，
接着 `o.header()` 就崩了。

修法是让 `header()` 容忍 nil 接收者，同时在 `blacken` 里显式跳过 nil。
这类问题不会在单元测试里稳定复现，只在大对象图 + 恰好触发 GC 时出现 ——
所以「压力测试 + 断言存活对象数」这条验收不是形式主义。

### 根集合漏项的检测方法

不要靠「跑起来没崩」。跑这段：

```bash
minilang run examples/05-gc-stress.ml --stats
```

输出里 `累计创建对象` 应该是 40 万量级，`存活对象` 应该是几百。
如果存活数 ≈ 累计创建数，就是根集合漏了（或者 `marked` 没复位）。
如果存活数是几十万但小于创建数，多半是 `endScope` 没发 `OP_POP`。

---

## 陷阱 5：字符串驻留（interning）

### 矛盾在哪

`==` 需要按**内容**比较字符串（`"ab" == "ab"` 必须为 true）。
但对象身份（`*ObjString` 指针）是按**地址**比较的。
如果 `==` 走内容比较，每次比较都要 O(n) 扫一遍；如果走指针比较，
两个内容相同但分别创建的字符串会判不相等。

### 解法

创建字符串时查驻留表，内容相同就复用同一个对象：

```go
func (vm *VM) InternString(s string) *ObjString {
	if o, ok := vm.intern[s]; ok {
		return o
	}
	o := &ObjString{Chars: s, Hash: hashString(s)}
	vm.allocate(&o.objHeader, ObjStringType, o, len(s)+40)
	vm.intern[s] = o
	return o
}
```

于是「内容相等」和「指针相等」变成同一件事，`==` 只比较指针：

```go
case KindObject:
    return a.obj == b.obj     // 字符串：靠驻留保证正确
```

**驻留不是性能优化，是 `==` 语义的前提。** 如果哪天去掉它，
这一行会静默地变成「内容相同却判不相等」—— 没有崩溃，没有报错，只是逻辑错了。
所以改这一行的时候必须同时改 `valuesEqual`。

### 用 `map[string]*ObjString` 而不是哈希桶

Crafting Interpreters 的 clox 用「哈希表 + 桶链表 + 开放寻址」实现，因为 C 没有现成的 map。
Go 有，所以本实现直接用 `map[string]*ObjString`：

- key 是 Go string（按内容哈希），天然完成「按内容查找」；
- value 是唯一实例；
- `ObjString.Hash` 仍然存着 FNV-1a 值，为将来需要按 hash 分桶或做缓存留位置，
  当前实现里它只被写入、不被读取 —— 这是有意的「预留但不使用」，
  而不是「写了忘了用」。

### 驻留与 GC 的交互

驻留表**不是根**。字符串只要没有别的地方引用，就该被回收。
所以 `sweep` 里回收字符串时必须同时把它从表里摘掉：

```go
if s, ok := h.self.(*ObjString); ok {
    delete(vm.intern, s.Chars)
}
```

漏掉这一步，表里会留下指向已回收对象的指针，
下次 `InternString` 查到它就会返回一个**已死**的对象 ——
表现为「字符串内容变成乱码或空」，而且只在 GC 之后才出现。

这也是 `objHeader` 里那个看起来多余的 `self Object` 字段的用途：
`markRoots`/`sweep` 遍历的是 `*objHeader`，但要删表需要 `*ObjString`。
没有 `self` 就只能用 `unsafe.Pointer` 强转（因为 `ObjString` 的第一个字段是 `objHeader`）。
多存一个指针（8 字节/对象）换掉一处 unsafe，对这个项目是划算的。

### 代价

每次字符串拼接都要做一次 map 查找 + 可能的分配。
`a + b + c` 会创建两个中间字符串（都是驻留的，都可能触发 GC）。
这也是 S10 结论里「VM 不是所有场景都快」的原因之一：
`fib` 这种纯数值计算，VM 快 8 倍；字符串密集的循环，两者差距会小得多。

---

## 附：实现中真实踩到的另一个坑

`parsePrecedence` 里处理调用的分支：

```go
if t == token.LParen && precCall >= min {
    p.advance()          // 这一行一开始漏了
    left = p.finishCall(left)
    continue
}
```

漏了 `p.advance()` 的后果不是「调用解析失败」，而是 `finishCall` 看到的是 `(` 本身，
把它当成**分组括号**去解析，于是 `f()` 报出「表达式不能以 ')' 开头」。
报错位置在 `)` 上，指向的错误原因却是 `(` 没被消费 —— 位置对了，归因错了。

这类 bug 说明一件事：**报错位置准确 ≠ 报错信息有用**。
位置只告诉你「在哪炸的」，信息质量取决于你写这段代码时有没有想清楚「谁消费了哪个 token」。

---

## 不确定的地方（按实际认知标注，不编造）

以下三处我没有把握给出「标准答案」，只说明本实现的取舍：

1. **`OP_JUMP_IF_FALSE` 该不该弹栈**。本实现选「不弹、由分支弹」，理由是两条路径
   各弹一次、指令数最少。另一种做法是「跳转时弹、不跳时也弹」，会让 `if` 变成
   4 条指令而非 3 条，但「条件值不会残留到分支体」这个不变式更简单。
   两种都能跑通，没有对错，只有取舍。

2. **绝对槽位 vs 相对偏移**。本实现选绝对槽位。相对偏移在某些指令布局下
   能省掉 `slotBase` 加法，但我没有实测过两者在本项目规模下的性能差异，
   所以不能说「绝对槽位更快」—— 只能说「绝对槽位更不容易写错」。

3. **8.7 倍的加速里，多少来自「消掉名字查找」，多少来自「消掉 Go 的 interface 装箱」**。
   tree 引擎用 `any` 存值，每次算术都要类型断言 + 可能装箱；
   VM 用 24 字节的 `Value` 结构体，无装箱。这两个因素混在一起，
   我没有做过分离实验，所以 S10 的归因只能说到「名字查找 + 装箱」这个量级，
   再细分需要单独做实验（例如给 tree 引擎换成 tagged value 再测一遍）。
