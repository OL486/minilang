package treewalk

import (
	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/semantics"
	"github.com/OL486/minilang/internal/token"
)

func (in *Interp) eval(e ast.Expr, env *Env) (any, error) {
	switch n := e.(type) {
	case *ast.Literal:
		return n.Value, nil

	case *ast.Variable:
		v, ok := env.Lookup(n.Name)
		if !ok {
			return nil, diag.New(diag.StageRuntime, n.P.Line, n.P.Col, "未定义的变量 '%s'", n.Name)
		}
		return v, nil

	case *ast.Assign:
		v, err := in.eval(n.Value, env)
		if err != nil {
			return nil, err
		}
		if !env.Assign(n.Name, v) {
			return nil, diag.New(diag.StageRuntime, n.P.Line, n.P.Col, "不能给未定义的变量 '%s' 赋值（minilang 没有隐式全局变量）", n.Name)
		}
		return v, nil

	case *ast.Grouping:
		return in.eval(n.Inner, env)

	case *ast.Unary:
		v, err := in.eval(n.Right, env)
		if err != nil {
			return nil, err
		}
		switch n.Op {
		case token.Bang:
			return !semantics.Truthy(v), nil
		case token.Minus:
			f, ok := v.(float64)
			if !ok {
				return nil, diag.New(diag.StageRuntime, n.P.Line, n.P.Col, "一元 - 只能作用于 number，实际是 %s", semantics.TypeName(v))
			}
			return -f, nil
		}
		return nil, diag.New(diag.StageRuntime, n.P.Line, n.P.Col, "内部错误：未知一元运算符 %s", n.Op)

	case *ast.Logical:
		// 短路：左边已经决定结果时，右边根本不求值。
		// 这里返回的是操作数本身而不是 true/false —— 与 Go/Python 一致。
		// 如果改成返回 bool，`nil || "x"` 就无法用来写默认值了。
		l, err := in.eval(n.Left, env)
		if err != nil {
			return nil, err
		}
		if n.Op == token.OrOr {
			if semantics.Truthy(l) {
				return l, nil
			}
		} else {
			if !semantics.Truthy(l) {
				return l, nil
			}
		}
		return in.eval(n.Right, env)

	case *ast.Binary:
		l, err := in.eval(n.Left, env)
		if err != nil {
			return nil, err
		}
		r, err := in.eval(n.Right, env)
		if err != nil {
			return nil, err
		}
		return binaryOp(n.Op, l, r, n.P)

	case *ast.Call:
		callee, err := in.eval(n.Callee, env)
		if err != nil {
			return nil, err
		}
		args := make([]any, len(n.Args))
		for i, a := range n.Args {
			v, err := in.eval(a, env)
			if err != nil {
				return nil, err
			}
			args[i] = v
		}
		return in.callValue(callee, args, n.P)
	}
	return nil, diag.New(diag.StageRuntime, e.Pos().Line, e.Pos().Col, "内部错误：未知表达式类型 %T", e)
}
