package compiler

import (
	"io"
	"strings"
	"testing"

	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/engine"
	"github.com/OL486/minilang/internal/lexer"
	"github.com/OL486/minilang/internal/parser"
)

// newTestCompiler 搭一个最小的编译器：一个空脚本函数，槽位 0 预留给脚本闭包自身。
func newTestCompiler() (*Compiler, *engine.VM) {
	vm := engine.NewVM(io.Discard)
	fn := vm.NewFunction("", 0)
	c := &Compiler{vm: vm, fn: &fnState{fn: fn, locals: []local{{name: "", depth: 0}}}}
	return c, vm
}

// TestPatchJump 验证跳转回填的偏移量是「相对操作数之后那个字节」：
// jump = 当前位置 - 操作数位置 - 2，少减 2 会让所有 if 分支多执行两条指令。
func TestPatchJump(t *testing.T) {
	c, _ := newTestCompiler()
	offset := c.emitJump(engine.OpJump, 1)
	c.emit(engine.OpNil, 1) // 1 条填充指令
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
	vm := engine.NewVM(io.Discard)
	stmts := []ast.Stmt{
		&ast.LetStmt{P: ast.Pos{Line: 1}, Name: "x", Init: &ast.Literal{P: ast.Pos{Line: 1}, Value: float64(42)}},
		&ast.PrintStmt{P: ast.Pos{Line: 2}, Value: &ast.Variable{P: ast.Pos{Line: 2}, Name: "x"}},
	}
	fn, errs := CompileProgram(vm, stmts)
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
	vm := engine.NewVM(io.Discard)
	fn, compErrs := CompileProgram(vm, stmts)
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
