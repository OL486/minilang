package compiler

import (
	"strings"
	"testing"

	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/vm"
	"github.com/OL486/minilang/internal/lexer"
	"github.com/OL486/minilang/internal/parser"
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
	offset := c.emitJump(vm.OpJump, 1)
	c.emit(vm.OpNil, 1) // 1 条填充指令
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
	c.declareLocal("x", 1)
	c.declareLocal("y", 1)

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
	c.declareLocal("inner", 1)
	if got := c.resolveLocal(c.fn, "inner"); got < 0 {
		t.Fatalf("作用域内 resolveLocal(inner) = %d, want >= 0", got)
	}
	c.endScope(1)
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
