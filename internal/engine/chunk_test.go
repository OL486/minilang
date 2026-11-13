package engine

import (
	"strings"
	"testing"
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

func TestChunkLineAt(t *testing.T) {
	c := NewChunk()
	c.Write(byte(OpNil), 3)
	c.Write(byte(OpReturn), 3)
	if got := c.LineAt(0); got != 3 {
		t.Fatalf("LineAt(0) = %d, want 3", got)
	}
	if got := c.LineAt(1); got != 3 {
		t.Fatalf("LineAt(1) = %d, want 3", got)
	}
	if got := c.LineAt(2); got != 0 {
		t.Fatalf("LineAt(2) = %d, want 0 (out of range)", got)
	}
	if got := c.LineAt(-1); got != 0 {
		t.Fatalf("LineAt(-1) = %d, want 0 (out of range)", got)
	}
}

// TestDisassembleClosureLength 验证反汇编对 OP_CLOSURE 的长度计算：
// 指令长度 = 2（opcode + 常量下标）+ 2 * UpvalueCount，
// 算错一位会让之后所有指令全部错位。
func TestDisassembleClosureLength(t *testing.T) {
	fn := &ObjFunction{Name: "inc", Arity: 0, UpvalueCount: 1, Chunk: NewChunk()}
	c := NewChunk()
	idx := c.AddConstant(ObjValue(fn))
	c.Write(byte(OpClosure), 1)
	c.Write(byte(idx), 1)
	c.Write(1, 1) // isLocal = 1
	c.Write(0, 1) // index = 0
	c.Write(byte(OpReturn), 2)

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
	c.Write(byte(OpClosure), 1)
	c.Write(byte(idx), 1)
	c.Write(byte(OpReturn), 2)

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
	c.Write(byte(OpConstant), 1)
	c.Write(0, 1)
	c.Write(byte(OpPrint), 1)
	c.AddConstant(NumberValue(42))

	out := c.Disassemble("test")
	if !strings.Contains(out, "OP_CONSTANT") || !strings.Contains(out, "OP_PRINT") {
		t.Fatalf("Disassemble 缺少指令:\n%s", out)
	}
	if !strings.Contains(out, "42") {
		t.Fatalf("Disassemble 没有打印常量 42:\n%s", out)
	}
}
