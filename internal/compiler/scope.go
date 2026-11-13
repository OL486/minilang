package compiler

import (
	"github.com/OL486/minilang/internal/vm"
)

// ---------- 作用域 ----------

func (c *Compiler) beginScope() { c.fn.scopeDepth++ }

func (c *Compiler) endScope(line int) {
	c.fn.scopeDepth--
	for len(c.fn.locals) > 0 && c.fn.locals[len(c.fn.locals)-1].depth > c.fn.scopeDepth {
		if c.fn.locals[len(c.fn.locals)-1].captured {
			// 被闭包捕获过的局部变量不能简单 POP：必须先把值搬到堆上，
			// 否则栈槽被后续代码复用时，闭包看到的就是别人的值。
			c.emit(vm.OpCloseUpvalue, line)
		} else {
			c.emit(vm.OpPop, line)
		}
		c.fn.locals = c.fn.locals[:len(c.fn.locals)-1]
	}
}

func (c *Compiler) declareLocal(name string, line int) {
	for i := len(c.fn.locals) - 1; i >= 0; i-- {
		if c.fn.locals[i].depth < c.fn.scopeDepth {
			break
		}
		if c.fn.locals[i].name == name {
			c.errorAt(line, "同一作用域内重复声明变量 '%s'", name)
		}
	}
	if len(c.fn.locals) >= maxLocals {
		c.errorAt(line, "局部变量超过 %d 个", maxLocals)
	}
	c.fn.locals = append(c.fn.locals, local{name: name, depth: c.fn.scopeDepth})
}

// resolveLocal 在当前函数里找变量，返回栈槽位（= locals 下标）。
//
// 为什么用绝对槽位而不是「相对栈顶偏移」：
// 相对偏移的写法在赋值时特别容易差一 —— 编译期算出「栈顶往下第 2 个」，
// 运行期 OP_SET_LOCAL 执行时栈顶可能已经因为压入右值而上移了一格，
// 结果写到了隔壁变量上。这类 bug 只在「赋值表达式的值被用到」时出现，
// 比如 `x = y = 1` 或者 `print x = 1`，非常难复现。
// 绝对槽位 + slotBase 的组合让「槽位」与「栈顶」彻底解耦：
// 不管栈顶在哪，第 n 个局部变量永远是 stack[slotBase+n]。
func (c *Compiler) resolveLocal(fs *fnState, name string) int {
	for i := len(fs.locals) - 1; i >= 0; i-- {
		if fs.locals[i].name == name {
			return i
		}
	}
	return -1
}

// resolveUpvalue 沿函数嵌套链向外找变量。
//
// 递归是关键：如果当前函数没找到，先看外层函数的局部变量（捕获它），
// 再看外层函数自己的 upvalue（传递捕获）。
// 只有一层的话，三层嵌套闭包的最内层就拿不到最外层的变量。
func (c *Compiler) resolveUpvalue(fs *fnState, name string) int {
	if fs.enclosing == nil {
		return -1
	}
	if idx := c.resolveLocal(fs.enclosing, name); idx >= 0 {
		fs.enclosing.locals[idx].captured = true
		return c.addUpvalue(fs, uint8(idx), true)
	}
	if idx := c.resolveUpvalue(fs.enclosing, name); idx >= 0 {
		return c.addUpvalue(fs, uint8(idx), false)
	}
	return -1
}

func (c *Compiler) addUpvalue(fs *fnState, index uint8, isLocal bool) int {
	for i, uv := range fs.upvalues {
		if uv.index == index && uv.isLocal == isLocal {
			return i
		}
	}
	if len(fs.upvalues) >= maxUpvalues {
		c.errorAt(0, "upvalue 超过 %d 个", maxUpvalues)
	}
	fs.upvalues = append(fs.upvalues, upvalueDesc{index: index, isLocal: isLocal})
	return len(fs.upvalues) - 1
}
