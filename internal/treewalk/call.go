package treewalk

import (
	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/semantics"
)

func (in *Interp) callValue(callee any, args []any, p ast.Pos) (any, error) {
	switch fn := callee.(type) {
	case *Function:
		if len(args) != len(fn.Decl.Params) {
			return nil, diag.New(diag.StageRuntime, p.Line, p.Col,
				"函数 %s 需要 %d 个参数，传入了 %d 个", fn.Decl.Name, len(fn.Decl.Params), len(args))
		}
		return in.callUser(fn, args)

	case *NativeFn:
		if len(args) != fn.Arity {
			return nil, diag.New(diag.StageRuntime, p.Line, p.Col,
				"内置函数 %s 需要 %d 个参数，传入了 %d 个", fn.Name, fn.Arity, len(args))
		}
		v, err := fn.Fn(args)
		if err != nil {
			return nil, diag.New(diag.StageRuntime, p.Line, p.Col, "内置函数 %s 出错: %v", fn.Name, err)
		}
		return v, nil
	}
	return nil, diag.New(diag.StageRuntime, p.Line, p.Col, "%s 不是函数，不能调用", semantics.TypeName(callee))
}

func (in *Interp) callUser(fn *Function, args []any) (result any, err error) {
	if in.depth >= in.maxDepth {
		return nil, diag.New(diag.StageRuntime, fn.Decl.P.Line, fn.Decl.P.Col,
			"调用栈溢出：嵌套调用超过 %d 层", in.maxDepth)
	}
	in.depth++
	defer func() { in.depth-- }()

	// 新环境挂在函数定义时的闭包上，而不是调用者的环境上。
	// 挂错这一处，闭包就退化成动态作用域：
	// makeCounter 返回的 inc 会去读调用者的 count，两个计数器就串了。
	env := NewEnv(fn.Closure)
	for i, name := range fn.Decl.Params {
		env.Define(name, args[i])
	}

	defer func() {
		if r := recover(); r != nil {
			rs, ok := r.(returnSignal)
			if !ok {
				panic(r)
			}
			result = rs.value
			err = nil
		}
	}()
	if e := in.execBlock(fn.Decl.Body, env); e != nil {
		return nil, e
	}
	return nil, nil
}
