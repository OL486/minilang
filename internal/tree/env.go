// Package tree 是 tree-walking 解释器（S3–S4）。
//
// 它的价值不是「能跑」，而是「能当参照物」：
// 后面字节码 VM 的每一个语义细节（真值判断、类型错误、闭包捕获时机）
// 都以它为基准做 diff。所以这里刻意写得直白，不做任何优化。
package tree

import (
	"github.com/OL486/minilang/internal/ast"
)

// Env 是作用域链上的一个节点。
//
// 为什么用 map + parent 指针，而不是「栈 + 名字→槽位」的编译期方案：
// tree-walking 阶段没有编译期，名字只能运行时查。
// map + 链式查找是唯一不需要预扫描的写法。代价是每次取变量都要走链表，
// 这个代价正是 S5 基线数字的一部分 —— 后面 VM 把它换成「编译期算好的栈槽下标」，
// 就是为了消掉这条链。
type Env struct {
	vars   map[string]any
	parent *Env
}

// NewEnv 创建一个作用域，parent 为 nil 时是全局作用域。
func NewEnv(parent *Env) *Env {
	return &Env{vars: make(map[string]any), parent: parent}
}

// Define 在当前作用域声明一个变量。允许覆盖同作用域已有变量，
// 因为 REPL 里重新 let 同名变量是正常操作。
func (e *Env) Define(name string, v any) {
	e.vars[name] = v
}

// Lookup 沿作用域链向上查找。
func (e *Env) Lookup(name string) (any, bool) {
	for cur := e; cur != nil; cur = cur.parent {
		if v, ok := cur.vars[name]; ok {
			return v, true
		}
	}
	return nil, false
}

// Assign 沿作用域链赋值：找到最近的那个声明就改它。
//
// 这里的「找最近」不能换成「在全局改」：
// 闭包里的 `count = count + 1` 必须改到 makeCounter 那一层局部变量上，
// 否则两个计数器会共享同一个全局 count —— 这正是 S4 验收用例要抓的 bug。
func (e *Env) Assign(name string, v any) bool {
	for cur := e; cur != nil; cur = cur.parent {
		if _, ok := cur.vars[name]; ok {
			cur.vars[name] = v
			return true
		}
	}
	return false
}

// Function 是用户定义的函数值。Closure 指向定义时的环境，
// 这就是闭包的全部秘密：函数值 = 代码 + 定义处的作用域。
type Function struct {
	Decl    *ast.FnDecl
	Closure *Env
}

// NativeFn 是内置函数。用 Go 函数实现，不经过 minilang 的语法树。
type NativeFn struct {
	Name  string
	Arity int
	Fn    func(args []any) (any, error)
}
