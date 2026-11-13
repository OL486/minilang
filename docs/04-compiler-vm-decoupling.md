# 编译器与 VM 解耦

## 问题

`CompileProgram` 要求一个活的 `*vm.VM`，编译器无法单独单元测试：
`internal/compiler/compiler_test.go` 里每个测试都要 `vm.NewVM(io.Discard)`，
连只测跳转回填、变量解析的测试也不例外。VM 的执行循环、栈、GC
与编译完全无关，却成了编译测试的前置条件。

## 清单：编译器实际用到的 VM 能力

对 `internal/compiler` 做 grep + 阅读，编译器对 `*vm.VM` 的**运行期**依赖
只有 5 个方法（其余 `vm.ObjFunction` / `vm.Chunk` / `vm.Op` / `vm.Value`
只是数据类型，不是活的 VM）：

| 方法 | 用处 | 调用点 |
| --- | --- | --- |
| `NewFunction(name, arity)` | 创建脚本与内层函数对象 | `CompileProgram`、 `compileFunction` |
| `InternString(s)` | 字符串常量与变量名驻留 | `identifierConstant`、 `literal`（string） |
| `PushRoot(v)` | 编译期临时根：正在编译的函数挂根，防 GC | `CompileProgram`、 `compileFunction` |
| `PopRoot()` | 编译完一个函数后出根 | `CompileProgram`、 `fnDecl` |
| `ClearRoots()` | 编译报错时强制清空根（根栈可能不平衡） | `CompileProgram`（defer + 错误路径） |

三件事本质上是同一件事：**编译期对象分配与 GC 安全**。分配可能触发 GC，
而编译中的函数对象不在 VM 栈上，必须有临时根保活（`vm.go` 的注释写明了
这个顺序坑；GC 标记时会扫 `vm.roots`，见 `gc.go:61`）。

## 方案比较

### A. 窄接口（已选）

在 `internal/compiler/alloc.go` 定义 `Allocator` 接口，只含上面 5 个方法；
`CompileProgram(a Allocator, stmts)`，`Compiler` 存 `alloc Allocator`；
`*vm.VM` 完整实现该接口。

- 改动面：编译器包内 2 个文件 + 测试；生产调用方（`cmd/minilang/session.go`、
  `test/e2e_test.go`）**一行不用改**（传 `*vm.VM` 照常编译）。
- API 破坏：`CompileProgram` 参数类型由 `*vm.VM` 变为 `Allocator`，
  对调用方源码兼容（` *vm.VM` 满足接口）；语义上反而放宽了。
- 测试收益：`fakeAllocator`（约 30 行，见 `compiler_test.go`）即可编译
  小程序并校验字节码/常量/驻留，编译器测试不再启动 VM。

### B. 把堆抽成独立组件（不选）

把 `objects` 链表、`intern` 表、`roots`、`bytesAllocated` 等从 `VM`
抽成 `Heap`，VM 内嵌 `*Heap`，编译器拿 `*Heap`。

- 收益与 A 相同，但要动 `allocate` / `collectGarbage` 的归属，
  GC 代码路径全部要过一遍，违反「不动 GC」；且编译器测试仍需一个真实
  `Heap`（带 GC），并不比 fake 更简单。留待以后 VM 本身需要拆分时再做。

### C. 把对象模型（Chunk/Value/Object）移出 vm 包（不选）

让编译器彻底不 import `internal/vm`。但 `Chunk` / `Value` / `ObjFunction`
是编译产物的数据定义，移包会波及 VM、treewalk、cmd、tools 全仓，
改动面数倍于收益，且对「编译器能否单独测试」没有额外帮助。
本轮不做。

## 新签名与调用方改动

```go
// internal/compiler/alloc.go
type Allocator interface {
	NewFunction(name string, arity int) *vm.ObjFunction
	InternString(s string) *vm.ObjString
	PushRoot(v vm.Value)
	PopRoot()
	ClearRoots()
}

// internal/compiler/compiler.go
func CompileProgram(a Allocator, stmts []ast.Stmt) (fn *vm.ObjFunction, errs []*diag.Error)
```

- `cmd/minilang/session.go:71`、`test/e2e_test.go:48,326`：不改，传 `*vm.VM`。
- `internal/compiler/compiler_test.go`：`newTestCompiler` 改用 `fakeAllocator`；
  旧的两个 `CompileProgram` 测试改传 fake；新增 3 个测试（见下）。

## 编译器如何单独测试

`fakeAllocator`（测试文件内）：`NewFunction` 返回带空 `Chunk` 的普通
函数对象（不挂堆、不参与 GC）；`InternString` 用 map 驻留，同一内容返回
同一对象；根操作只维护一个切片。

新增测试（均不启动 VM）：

- `TestCompileProgramFakeAllocatorRootsEmpty`：编译 `let x = 42; print x;`，
  校验字节码含 `OP_DEFINE_GLOBAL`，且成功后临时根为空。
- `TestCompileProgramFakeAllocatorInternsStrings`：两个 `"hi"` 字面量占
  2 个常量槽（`AddConstant` 不去重），但指向同一个 `*ObjString`。
- `TestCompileProgramErrorClearsRoots`：同一作用域重复声明变量应报错，
  返回 nil 函数，且报错后临时根被清空。

## 明确不做

- 不改字节码格式、不改语义、不动 GC、不重构 VM 执行循环。
- 不动 `vm.go` / `gc.go` / `object.go` 的任何实现（只读了它们来列清单）。
- 不抽 `Heap` 组件（方案 B），不移对象模型出包（方案 C）。
