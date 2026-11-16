package vm

import (
	"strings"
	"testing"

	"github.com/OL486/minilang/internal/ast"
)

func TestAddConstantLimit(t *testing.T) {
	c := NewChunk()
	for i := 0; i < 256; i++ {
		if got := c.AddConstant(NumberValue(float64(i))); got != i {
			t.Fatalf("AddConstant(%d) = %d, want %d", i, got, i)
		}
	}
	if got := c.AddConstant(NumberValue(256)); got != -1 {
		t.Fatalf("AddConstant beyond 256 = %d, want -1", got)
	}
	if len(c.Constants) != 256 {
		t.Fatalf("len(Constants) = %d, want 256", len(c.Constants))
	}
}

func TestChunkPosAt(t *testing.T) {
	c := NewChunk()
	c.Write(byte(OpNil), ast.Pos{Line: 3, Col: 5})
	c.Write(byte(OpReturn), ast.Pos{Line: 3, Col: 7})
	if got := c.PosAt(0); got != (ast.Pos{Line: 3, Col: 5}) {
		t.Fatalf("PosAt(0) = %+v, want {Line:3 Col:5}", got)
	}
	if got := c.PosAt(1); got != (ast.Pos{Line: 3, Col: 7}) {
		t.Fatalf("PosAt(1) = %+v, want {Line:3 Col:7}", got)
	}
	if got := c.PosAt(2); got != (ast.Pos{}) {
		t.Fatalf("PosAt(2) = %+v, want zero Pos (out of range)", got)
	}
	if got := c.PosAt(-1); got != (ast.Pos{}) {
		t.Fatalf("PosAt(-1) = %+v, want zero Pos (out of range)", got)
	}
}

// TestDisassembleShowsColumns 验证反汇编打出「行:列」：
// 同一行的后续指令仍用 | 占位，行号变化时才重新打印位置。
func TestDisassembleShowsColumns(t *testing.T) {
	c := NewChunk()
	c.Write(byte(OpConstant), ast.Pos{Line: 2, Col: 7})
	c.Write(0, ast.Pos{Line: 2, Col: 7})
	c.Write(byte(OpPrint), ast.Pos{Line: 2, Col: 7})
	c.AddConstant(NumberValue(42))
	c.Write(byte(OpReturn), ast.Pos{Line: 3, Col: 1})

	out := c.Disassemble("test")
	if !strings.Contains(out, "2:7") {
		t.Fatalf("Disassemble 没有打印列号 2:7:\n%s", out)
	}
	if !strings.Contains(out, "3:1") {
		t.Fatalf("Disassemble 没有打印列号 3:1:\n%s", out)
	}
	if !strings.Contains(out, "|") {
		t.Fatalf("Disassemble 同行指令应以 | 占位:\n%s", out)
	}
}

// TestDisassembleClosureLength 验证反汇编对 OP_CLOSURE 的长度计算：
// 指令长度 = 2（opcode + 常量下标）+ 2 * UpvalueCount，
// 算错一位会让之后所有指令全部错位。
func TestDisassembleClosureLength(t *testing.T) {
	fn := &ObjFunction{Name: "inc", Arity: 0, UpvalueCount: 1, Chunk: NewChunk()}
	c := NewChunk()
	idx := c.AddConstant(ObjValue(fn))
	c.Write(byte(OpClosure), ast.Pos{Line: 1, Col: 1})
	c.Write(byte(idx), ast.Pos{Line: 1, Col: 1})
	c.Write(1, ast.Pos{Line: 1, Col: 1}) // isLocal = 1
	c.Write(0, ast.Pos{Line: 1, Col: 1}) // index = 0
	c.Write(byte(OpReturn), ast.Pos{Line: 2, Col: 1})

	out := c.Disassemble("test")
	if !strings.Contains(out, "OP_CLOSURE") {
		t.Fatalf("Disassemble 缺少 OP_CLOSURE:\n%s", out)
	}
	if !strings.Contains(out, "upvalues=1") {
		t.Fatalf("Disassemble 没有标出 upvalues=1:\n%s", out)
	}
	if !strings.Contains(out, "local[0]") {
		t.Fatalf("Disassemble 没有标出 local[0]:\n%s", out)
	}
	// OP_CLOSURE 占 4 字节（偏移 0–3），OP_RETURN 必须在偏移 4。
	if !strings.Contains(out, "0004") || !strings.Contains(out, "OP_RETURN") {
		t.Fatalf("OP_RETURN 应在偏移 0004:\n%s", out)
	}
}

func TestDisassembleClosureNoUpvalues(t *testing.T) {
	fn := &ObjFunction{Name: "f", Arity: 0, UpvalueCount: 0, Chunk: NewChunk()}
	c := NewChunk()
	idx := c.AddConstant(ObjValue(fn))
	c.Write(byte(OpClosure), ast.Pos{Line: 1, Col: 1})
	c.Write(byte(idx), ast.Pos{Line: 1, Col: 1})
	c.Write(byte(OpReturn), ast.Pos{Line: 2, Col: 1})

	out := c.Disassemble("test")
	if !strings.Contains(out, "upvalues=0") {
		t.Fatalf("Disassemble 没有标出 upvalues=0:\n%s", out)
	}
	// OP_CLOSURE 占 2 字节，OP_RETURN 必须在偏移 2。
	if !strings.Contains(out, "0002") || !strings.Contains(out, "OP_RETURN") {
		t.Fatalf("OP_RETURN 应在偏移 0002:\n%s", out)
	}
}

func TestDisassembleSimple(t *testing.T) {
	c := NewChunk()
	c.Write(byte(OpConstant), ast.Pos{Line: 1, Col: 1})
	c.Write(0, ast.Pos{Line: 1, Col: 1})
	c.Write(byte(OpPrint), ast.Pos{Line: 1, Col: 1})
	c.AddConstant(NumberValue(42))

	out := c.Disassemble("test")
	if !strings.Contains(out, "OP_CONSTANT") || !strings.Contains(out, "OP_PRINT") {
		t.Fatalf("Disassemble 缺少指令:\n%s", out)
	}
	if !strings.Contains(out, "42") {
		t.Fatalf("Disassemble 没有打印常量 42:\n%s", out)
	}
}
