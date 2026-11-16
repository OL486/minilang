package compiler

import (
	"strings"
	"testing"

	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/lexer"
	"github.com/OL486/minilang/internal/parser"
	"github.com/OL486/minilang/internal/vm"
)

// fakeAllocator 是 Allocator 的最小实现：只做编译器真正需要的三件事，
// 不启动完整 VM、不触发 GC、不注册内置函数。
//
//   - NewFunction：返回一个带空 Chunk 的普通函数对象（不挂堆、不参与 GC）。
//   - InternString：用 map 做驻留，同一内容返回同一对象，与真实 VM 的约定一致。
//   - PushRoot / PopRoot / ClearRoots：只维护一个切片，编译结束时应为空。
type fakeAllocator struct {
	intern map[string]*vm.ObjString
	roots  []vm.Value
}

func newFakeAllocator() *fakeAllocator {
	return &fakeAllocator{intern: make(map[string]*vm.ObjString)}
}

func (f *fakeAllocator) NewFunction(name string, arity int) *vm.ObjFunction {
	return &vm.ObjFunction{Name: name, Arity: arity, Chunk: vm.NewChunk()}
}

func (f *fakeAllocator) InternString(s string) *vm.ObjString {
	if o, ok := f.intern[s]; ok {
		return o
	}
	o := &vm.ObjString{Chars: s}
	f.intern[s] = o
	return o
}

func (f *fakeAllocator) PushRoot(v vm.Value) { f.roots = append(f.roots, v) }

func (f *fakeAllocator) PopRoot() {
	if n := len(f.roots); n > 0 {
		f.roots = f.roots[:n-1]
	}
}

func (f *fakeAllocator) ClearRoots() { f.roots = f.roots[:0] }

// newTestCompiler 搭一个最小的编译器：一个空脚本函数，槽位 0 预留给脚本闭包自身。
// 不需要真实 VM —— 这正是解耦要证明的。
func newTestCompiler() (*Compiler, *fakeAllocator) {
	alloc := newFakeAllocator()
	fn := alloc.NewFunction("", 0)
	c := &Compiler{alloc: alloc, fn: &fnState{fn: fn, locals: []local{{name: "", depth: 0}}}}
	return c, alloc
}

// TestPatchJump 验证跳转回填的偏移量是「相对操作数之后那个字节」：
// jump = 当前位置 - 操作数位置 - 2，少减 2 会让所有 if 分支多执行两条指令。
func TestPatchJump(t *testing.T) {
	c, _ := newTestCompiler()
	offset := c.emitJump(vm.OpJump, ast.Pos{Line: 1, Col: 1})
	c.emit(vm.OpNil, ast.Pos{Line: 1, Col: 1}) // 1 条填充指令
	c.patchJump(offset)

	jump := int(c.chunk().Code[offset])<<8 | int(c.chunk().Code[offset+1])
	// 填充后 Code 长度为 3（jump）+ 1（nil）= 4，jump 应为 4 - offset - 2。
	want := len(c.chunk().Code) - offset - 2
	if jump != want {
		t.Fatalf("patchJump offset = %d, want %d (code len %d, offset %d)",
			jump, want, len(c.chunk().Code), offset)
	}
	if want != 1 {
		t.Fatalf("本用例的 jump 应为 1（只跳过 1 条 OpNil），实际 %d", want)
	}
}

// TestResolveLocalAbsoluteSlot 验证局部变量用绝对槽位：
// locals 下标就是栈槽位，槽位 0 是脚本闭包自身，第一个声明的变量在槽位 1。
func TestResolveLocalAbsoluteSlot(t *testing.T) {
	c, _ := newTestCompiler()
	c.declareLocal("x", ast.Pos{Line: 1, Col: 1})
	c.declareLocal("y", ast.Pos{Line: 1, Col: 1})

	if got := c.resolveLocal(c.fn, "x"); got != 1 {
		t.Fatalf("resolveLocal(x) = %d, want 1", got)
	}
	if got := c.resolveLocal(c.fn, "y"); got != 2 {
		t.Fatalf("resolveLocal(y) = %d, want 2", got)
	}
	if got := c.resolveLocal(c.fn, "nope"); got != -1 {
		t.Fatalf("resolveLocal(nope) = %d, want -1", got)
	}
}

// TestResolveLocalScope 验证内层作用域的变量在退出后不再可见。
func TestResolveLocalScope(t *testing.T) {
	c, _ := newTestCompiler()
	c.beginScope()
	c.declareLocal("inner", ast.Pos{Line: 1, Col: 1})
	if got := c.resolveLocal(c.fn, "inner"); got < 0 {
		t.Fatalf("作用域内 resolveLocal(inner) = %d, want >= 0", got)
	}
	c.endScope(ast.Pos{Line: 1, Col: 1})
	if got := c.resolveLocal(c.fn, "inner"); got != -1 {
		t.Fatalf("退出作用域后 resolveLocal(inner) = %d, want -1", got)
	}
}

// TestCompileProgramSimple 编译 `let x = 42; print x;`，
// 顶层 let 应编译成全局变量定义，print 应发射 OP_PRINT。
func TestCompileProgramSimple(t *testing.T) {
	alloc := newFakeAllocator()
	stmts := []ast.Stmt{
		&ast.LetStmt{P: ast.Pos{Line: 1}, Name: "x", Init: &ast.Literal{P: ast.Pos{Line: 1}, Value: float64(42)}},
		&ast.PrintStmt{P: ast.Pos{Line: 2}, Value: &ast.Variable{P: ast.Pos{Line: 2}, Name: "x"}},
	}
	fn, errs := CompileProgram(alloc, stmts)
	if len(errs) > 0 {
		t.Fatalf("CompileProgram 报错: %v", errs[0])
	}
	if fn == nil {
		t.Fatal("CompileProgram 返回了 nil 函数")
	}
	if len(fn.Chunk.Code) == 0 {
		t.Fatal("编译出的字节码为空")
	}
	out := fn.Chunk.Disassemble("test")
	if !strings.Contains(out, "OP_DEFINE_GLOBAL") {
		t.Fatalf("顶层 let 应编译成 OP_DEFINE_GLOBAL:\n%s", out)
	}
	if !strings.Contains(out, "OP_PRINT") {
		t.Fatalf("print 应编译成 OP_PRINT:\n%s", out)
	}
}

// TestCompileProgramClosure 编译一个捕获外层变量的闭包，
// 检查 OP_CLOSURE 发射正确且 upvalue 描述无误。
func TestCompileProgramClosure(t *testing.T) {
	src := `fn makeCounter() {
  let count = 0;
  fn inc() { count = count + 1; return count; }
  return inc;
}
print makeCounter()();
`
	tokens, lexErrs := lexer.New(src).Scan()
	if len(lexErrs) > 0 {
		t.Fatalf("词法错误: %v", lexErrs[0])
	}
	stmts, parseErrs := parser.New(tokens).Parse()
	if len(parseErrs) > 0 {
		t.Fatalf("语法错误: %v", parseErrs[0])
	}
	alloc := newFakeAllocator()
	fn, compErrs := CompileProgram(alloc, stmts)
	if len(compErrs) > 0 {
		t.Fatalf("编译错误: %v", compErrs[0])
	}
	out := fn.Chunk.Disassemble("test")
	if !strings.Contains(out, "OP_CLOSURE") {
		t.Fatalf("闭包应编译成 OP_CLOSURE:\n%s", out)
	}
	if !strings.Contains(out, "upvalues=1") {
		t.Fatalf("inc 应捕获 1 个 upvalue（count）:\n%s", out)
	}
}

// TestCompileProgramFakeAllocatorRootsEmpty：只用 fakeAllocator 编译一段小程序，
// 成功后临时根栈必须为空 —— 证明编译器不再依赖真实 VM 也能完整编译。
func TestCompileProgramFakeAllocatorRootsEmpty(t *testing.T) {
	alloc := newFakeAllocator()
	stmts := []ast.Stmt{
		&ast.LetStmt{P: ast.Pos{Line: 1}, Name: "x", Init: &ast.Literal{P: ast.Pos{Line: 1}, Value: float64(42)}},
		&ast.PrintStmt{P: ast.Pos{Line: 2}, Value: &ast.Variable{P: ast.Pos{Line: 2}, Name: "x"}},
	}
	fn, errs := CompileProgram(alloc, stmts)
	if len(errs) > 0 {
		t.Fatalf("CompileProgram 报错: %v", errs[0])
	}
	if fn == nil {
		t.Fatal("CompileProgram 返回了 nil 函数")
	}
	if len(alloc.roots) != 0 {
		t.Fatalf("编译成功后临时根应为空，实际 %d 个", len(alloc.roots))
	}
	if !strings.Contains(fn.Chunk.Disassemble("test"), "OP_DEFINE_GLOBAL") {
		t.Fatal("顶层 let 应编译成 OP_DEFINE_GLOBAL")
	}
}

// TestCompileProgramFakeAllocatorInternsStrings：同一字符串字面量出现两次，
// 常量池里的两个常量应指向同一个 *ObjString —— 证明 fake 履行了驻留约定，
// 编译器对驻留的依赖只体现在 Allocator 接口上，不依赖真实 VM 的 intern 表。
func TestCompileProgramFakeAllocatorInternsStrings(t *testing.T) {
	alloc := newFakeAllocator()
	lit := func() *ast.Literal {
		return &ast.Literal{P: ast.Pos{Line: 1}, Value: "hi"}
	}
	stmts := []ast.Stmt{
		&ast.PrintStmt{P: ast.Pos{Line: 1}, Value: lit()},
		&ast.PrintStmt{P: ast.Pos{Line: 2}, Value: lit()},
	}
	fn, errs := CompileProgram(alloc, stmts)
	if len(errs) > 0 {
		t.Fatalf("CompileProgram 报错: %v", errs[0])
	}
	// AddConstant 不去重：两个字面量占 2 个常量槽，但它们应指向同一个 *ObjString。
	if len(fn.Chunk.Constants) != 2 {
		t.Fatalf("两个字符串字面量应占 2 个常量，实际 %d 个", len(fn.Chunk.Constants))
	}
	s0 := fn.Chunk.Constants[0].AsString()
	s1 := fn.Chunk.Constants[1].AsString()
	if s0 == nil || s1 == nil || s0.Chars != "hi" || s1.Chars != "hi" {
		t.Fatalf("常量应为字符串 \"hi\"，实际 %v / %v", fn.Chunk.Constants[0], fn.Chunk.Constants[1])
	}
	if s0 != s1 {
		t.Fatal("同一字符串字面量应驻留为同一对象，两个常量指向了不同 *ObjString")
	}
}

// compileSource 把一段源码词法 → 语法 → 编译成脚本函数，
// 任何一步报错都直接失败 —— 这些测试只关心「编译成功后的字节码形状」。
func compileSource(t *testing.T, src string) *vm.ObjFunction {
	t.Helper()
	tokens, lexErrs := lexer.New(src).Scan()
	if len(lexErrs) > 0 {
		t.Fatalf("词法错误: %v", lexErrs[0])
	}
	stmts, parseErrs := parser.New(tokens).Parse()
	if len(parseErrs) > 0 {
		t.Fatalf("语法错误: %v", parseErrs[0])
	}
	alloc := newFakeAllocator()
	fn, compErrs := CompileProgram(alloc, stmts)
	if len(compErrs) > 0 {
		t.Fatalf("编译错误: %v", compErrs[0])
	}
	if fn == nil {
		t.Fatal("CompileProgram 返回了 nil 函数")
	}
	return fn
}

// jumpInsn 是一条跳转指令及其解出的目标偏移。
type jumpInsn struct {
	op     vm.Op
	offset int
	target int
}

// scanJumps 沿字节码逐条解码，返回所有跳转指令（OP_JUMP / OP_JUMP_IF_FALSE /
// OP_LOOP）及其目标偏移。前向跳转目标 = 指令位置 + 3 + 偏移；
// OP_LOOP 是后向跳转，目标 = 指令位置 + 3 - 偏移。
// 长度表与 vm/disasm.go 的解码规则一致（本测试的程序不含闭包，
// OP_CLOSURE 的变长分支只是为了解码完整）。
func scanJumps(t *testing.T, fn *vm.ObjFunction) []jumpInsn {
	t.Helper()
	code := fn.Chunk.Code
	var jumps []jumpInsn
	for pc := 0; pc < len(code); {
		op := vm.Op(code[pc])
		switch op {
		case vm.OpJump, vm.OpJumpIfFalse, vm.OpLoop:
			jump := int(code[pc+1])<<8 | int(code[pc+2])
			target := pc + 3 + jump
			if op == vm.OpLoop {
				target = pc + 3 - jump
			}
			jumps = append(jumps, jumpInsn{op: op, offset: pc, target: target})
			pc += 3
		case vm.OpConstant, vm.OpGetLocal, vm.OpSetLocal, vm.OpGetGlobal,
			vm.OpSetGlobal, vm.OpDefineGlobal, vm.OpGetUpvalue, vm.OpSetUpvalue,
			vm.OpCall:
			pc += 2
		case vm.OpClosure:
			idx := int(code[pc+1])
			closure, ok := fn.Chunk.Constants[idx].AsObject().(*vm.ObjFunction)
			if !ok {
				t.Fatalf("OP_CLOSURE 常量 %d 不是函数", idx)
			}
			pc += 2 + 2*closure.UpvalueCount
		default:
			pc++
		}
	}
	return jumps
}

func jumpsOf(jumps []jumpInsn, op vm.Op) []jumpInsn {
	var out []jumpInsn
	for _, j := range jumps {
		if j.op == op {
			out = append(out, j)
		}
	}
	return out
}

// TestCompileProgramIfElse 编译 if-else 程序，断言：
//   - 反汇编含 OP_JUMP_IF_FALSE（条件跳转）和 OP_JUMP（跳过 else）；
//   - OP_JUMP_IF_FALSE 的目标正好是 else 入口的 OP_POP（条件为假时弹掉条件值）；
//   - OP_JUMP 的目标正好是 else 之后的第一条指令（emitReturn 发的 OP_NIL）。
//
// 这三点合起来才算 ifStmt 的回填正确：只含操作码名不检查目标的话，
// patchJump 的 off-by-one（少减 2）根本发现不了。
func TestCompileProgramIfElse(t *testing.T) {
	src := `let a = true;
if (a) { print 1; } else { print 2; }
`
	fn := compileSource(t, src)
	out := fn.Chunk.Disassemble("test")
	if !strings.Contains(out, "OP_JUMP_IF_FALSE") {
		t.Fatalf("if 应编译出 OP_JUMP_IF_FALSE:\n%s", out)
	}
	// "OP_JUMP " 带尾空格：OP_JUMP_IF_FALSE 里是下划线，不会误数。
	if strings.Count(out, "OP_JUMP ") != 1 {
		t.Fatalf("if-else 应恰好编译出 1 条 OP_JUMP（跳过 else）:\n%s", out)
	}

	jumps := scanJumps(t, fn)
	jifs := jumpsOf(jumps, vm.OpJumpIfFalse)
	if len(jifs) != 1 {
		t.Fatalf("应恰好有 1 条 OP_JUMP_IF_FALSE，实际 %d 条", len(jifs))
	}
	if got := fn.Chunk.Code[jifs[0].target]; got != byte(vm.OpPop) {
		t.Fatalf("OP_JUMP_IF_FALSE 应跳到 else 入口的 OP_POP（偏移 %d），实际偏移 %d 处是 %s",
			jifs[0].target, jifs[0].target, vm.Op(got))
	}
	plain := jumpsOf(jumps, vm.OpJump)
	if len(plain) != 1 {
		t.Fatalf("应恰好有 1 条 OP_JUMP，实际 %d 条", len(plain))
	}
	if got := fn.Chunk.Code[plain[0].target]; got != byte(vm.OpNil) {
		t.Fatalf("OP_JUMP 应跳到 else 之后的第一条指令 OP_NIL（偏移 %d），实际偏移 %d 处是 %s",
			plain[0].target, plain[0].target, vm.Op(got))
	}
}

// TestCompileProgramWhile 编译 while 程序，断言：
//   - 反汇编含 OP_LOOP；
//   - OP_LOOP 的目标正好回到循环开头（条件的第一条指令 OP_GET_GLOBAL），
//     即 emitLoop 的后向偏移算对了；
//   - 退出用的 OP_JUMP_IF_FALSE 目标正好是循环后的 OP_POP（条件为假时弹掉条件值）。
func TestCompileProgramWhile(t *testing.T) {
	src := `let i = 0;
while (i < 3) { i = i + 1; }
`
	fn := compileSource(t, src)
	out := fn.Chunk.Disassemble("test")
	if !strings.Contains(out, "OP_LOOP") {
		t.Fatalf("while 应编译出 OP_LOOP:\n%s", out)
	}

	jumps := scanJumps(t, fn)
	loops := jumpsOf(jumps, vm.OpLoop)
	if len(loops) != 1 {
		t.Fatalf("应恰好有 1 条 OP_LOOP，实际 %d 条", len(loops))
	}
	// 循环开头是条件的第一条指令：let 之后第一条是 OP_GET_GLOBAL i。
	if got := fn.Chunk.Code[loops[0].target]; got != byte(vm.OpGetGlobal) {
		t.Fatalf("OP_LOOP 应跳回循环开头 OP_GET_GLOBAL（偏移 %d），实际偏移 %d 处是 %s",
			loops[0].target, loops[0].target, vm.Op(got))
	}
	jifs := jumpsOf(jumps, vm.OpJumpIfFalse)
	if len(jifs) != 1 {
		t.Fatalf("应恰好有 1 条 OP_JUMP_IF_FALSE（退出跳转），实际 %d 条", len(jifs))
	}
	if got := fn.Chunk.Code[jifs[0].target]; got != byte(vm.OpPop) {
		t.Fatalf("退出跳转应跳到循环后的 OP_POP（偏移 %d），实际偏移 %d 处是 %s",
			jifs[0].target, jifs[0].target, vm.Op(got))
	}
}

// TestCompileProgramLogical 编译 `a && b || c`，断言短路跳转的存在与目标：
//   - 恰好 2 条 OP_JUMP_IF_FALSE（&& 一条、|| 一条）和 1 条 OP_JUMP（|| 的）；
//   - && 的 OP_JUMP_IF_FALSE 目标正好是 || 的 OP_JUMP_IF_FALSE
//     （左边为假时直接跳出 &&，落到 || 的判断）；
//   - || 的 OP_JUMP_IF_FALSE 目标是 OP_POP（左边为假时弹掉，算右边）；
//   - || 的 OP_JUMP 目标是 OP_PRINT（左边为真时把左值原样留下，跳过右边）。
func TestCompileProgramLogical(t *testing.T) {
	src := `let a = true;
let b = false;
let c = true;
print a && b || c;
`
	fn := compileSource(t, src)
	out := fn.Chunk.Disassemble("test")
	if strings.Count(out, "OP_JUMP_IF_FALSE") != 2 {
		t.Fatalf("a && b || c 应恰好编译出 2 条 OP_JUMP_IF_FALSE:\n%s", out)
	}
	if strings.Count(out, "OP_JUMP ") != 1 {
		t.Fatalf("a && b || c 应恰好编译出 1 条 OP_JUMP:\n%s", out)
	}

	jumps := scanJumps(t, fn)
	jifs := jumpsOf(jumps, vm.OpJumpIfFalse)
	if len(jifs) != 2 {
		t.Fatalf("应恰好有 2 条 OP_JUMP_IF_FALSE，实际 %d 条", len(jifs))
	}
	if jifs[0].target != jifs[1].offset {
		t.Fatalf("&& 的 OP_JUMP_IF_FALSE（偏移 %d）应跳到 || 的 OP_JUMP_IF_FALSE（偏移 %d），实际跳到偏移 %d",
			jifs[0].offset, jifs[1].offset, jifs[0].target)
	}
	if got := fn.Chunk.Code[jifs[1].target]; got != byte(vm.OpPop) {
		t.Fatalf("|| 的 OP_JUMP_IF_FALSE 应跳到 OP_POP（偏移 %d），实际偏移 %d 处是 %s",
			jifs[1].target, jifs[1].target, vm.Op(got))
	}
	plain := jumpsOf(jumps, vm.OpJump)
	if len(plain) != 1 {
		t.Fatalf("应恰好有 1 条 OP_JUMP，实际 %d 条", len(plain))
	}
	if got := fn.Chunk.Code[plain[0].target]; got != byte(vm.OpPrint) {
		t.Fatalf("|| 的 OP_JUMP 应跳到 OP_PRINT（偏移 %d），实际偏移 %d 处是 %s",
			plain[0].target, plain[0].target, vm.Op(got))
	}
}

// TestCompileProgramErrorClearsRoots：编译报错时返回 nil 函数与错误，
// 且临时根栈被清空（defer 里的 ClearRoots）—— 证明错误路径也不依赖真实 VM。
func TestCompileProgramErrorClearsRoots(t *testing.T) {
	alloc := newFakeAllocator()
	stmts := []ast.Stmt{
		&ast.BlockStmt{P: ast.Pos{Line: 1}, Stmts: []ast.Stmt{
			&ast.LetStmt{P: ast.Pos{Line: 2}, Name: "x", Init: &ast.Literal{P: ast.Pos{Line: 2}, Value: float64(1)}},
			&ast.LetStmt{P: ast.Pos{Line: 3}, Name: "x", Init: &ast.Literal{P: ast.Pos{Line: 3}, Value: float64(2)}},
		}},
	}
	fn, errs := CompileProgram(alloc, stmts)
	if fn != nil {
		t.Fatal("编译报错时应返回 nil 函数")
	}
	if len(errs) == 0 {
		t.Fatal("重复声明同一作用域变量应报错")
	}
	if len(alloc.roots) != 0 {
		t.Fatalf("编译报错后临时根应被清空，实际 %d 个", len(alloc.roots))
	}
}
