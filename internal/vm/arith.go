package vm

import (
	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/semantics"
	"github.com/OL486/minilang/internal/token"
)

// ---------- 算术 ----------

func (vm *VM) arith(op Op, line int) error {
	b := vm.pop()
	a := vm.pop()
	if !a.IsNumber() || !b.IsNumber() {
		return diag.New(diag.StageRuntime, line, 0,
			"运算符 %s 不支持 %s 与 %s", opSymbol(op), a.TypeName(), b.TypeName())
	}
	// -、*、/、% 的数值语义只留一份，在 semantics.Arithmetic。
	r, ok := semantics.Arithmetic(arithToken(op), a.AsNumber(), b.AsNumber())
	if !ok {
		return diag.New(diag.StageRuntime, line, 0, "内部错误：未知操作码 %d", byte(op))
	}
	vm.push(NumberValue(r))
	return nil
}

func (vm *VM) add(line int) error {
	b := vm.peek(0)
	a := vm.peek(1)
	// + 的语义（number 相加 / string 拼接）只留一份，在 semantics.Add。
	res, ok := semantics.Add(a.toAny(), b.toAny())
	if !ok {
		return diag.New(diag.StageRuntime, line, 0,
			"运算符 + 不支持 %s 与 %s", a.TypeName(), b.TypeName())
	}
	switch v := res.(type) {
	case float64:
		vm.pop()
		vm.pop()
		vm.push(NumberValue(v))
		return nil
	case string:
		// 注意：两个操作数在 internString 期间都还在栈上，
		// 所以即使这里触发 GC，它们也是根。先 pop 再分配就会踩到
		// 「拼接结果被回收」的坑（只在 GC 恰好触发时出现）。
		s := vm.InternString(v)
		vm.pop()
		vm.pop()
		vm.push(ObjValue(s))
		return nil
	}
	return diag.New(diag.StageRuntime, line, 0,
		"运算符 + 不支持 %s 与 %s", a.TypeName(), b.TypeName())
}

func (vm *VM) compareAndPush(op Op, line int) error {
	b := vm.pop()
	a := vm.pop()
	// <、<=、>、>= 的语义只留一份，在 semantics.Compare。
	res, ok := semantics.Compare(compareToken(op), a.toAny(), b.toAny())
	if !ok {
		return diag.New(diag.StageRuntime, line, 0,
			"运算符 %s 不支持 %s 与 %s", opSymbol(op), a.TypeName(), b.TypeName())
	}
	vm.push(BoolValue(res))
	return nil
}

// opToken 把字节码操作码映射到对应的 token 类型，
// 这样算术/比较/运算符文本都复用 semantics 与 token.OpText 的唯一实现。
func opToken(op Op) (token.Type, bool) {
	switch op {
	case OpAdd:
		return token.Plus, true
	case OpSubtract:
		return token.Minus, true
	case OpMultiply:
		return token.Star, true
	case OpDivide:
		return token.Slash, true
	case OpModulo:
		return token.Percent, true
	case OpGreater:
		return token.Gt, true
	case OpGreaterEqual:
		return token.Ge, true
	case OpLess:
		return token.Lt, true
	case OpLessEqual:
		return token.Le, true
	}
	return 0, false
}

func arithToken(op Op) token.Type {
	t, _ := opToken(op)
	return t
}

func compareToken(op Op) token.Type {
	t, _ := opToken(op)
	return t
}

func opSymbol(op Op) string {
	if t, ok := opToken(op); ok {
		return token.OpText(t)
	}
	return op.String()
}
