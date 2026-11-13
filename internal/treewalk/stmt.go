package treewalk

import (
	"fmt"
	"github.com/OL486/minilang/internal/ast"
	"github.com/OL486/minilang/internal/diag"
	"github.com/OL486/minilang/internal/semantics"
)

func (in *Interp) execBlock(stmts []ast.Stmt, env *Env) error {
	for _, s := range stmts {
		if err := in.execStmt(s, env); err != nil {
			return err
		}
	}
	return nil
}

func (in *Interp) execStmt(s ast.Stmt, env *Env) error {
	switch n := s.(type) {
	case *ast.LetStmt:
		v, err := in.eval(n.Init, env)
		if err != nil {
			return err
		}
		env.Define(n.Name, v)
		return nil

	case *ast.FnDecl:
		// 先定义再执行 body：这样递归函数在函数体内能查到自己的名字。
		// 如果反过来（先建闭包再 define），递归调用会在定义处查不到自己，
		// 报「未定义变量 f」—— 这是最经典的 tree-walking 闭包 bug。
		env.Define(n.Name, &Function{Decl: n, Closure: env})
		return nil

	case *ast.PrintStmt:
		v, err := in.eval(n.Value, env)
		if err != nil {
			return err
		}
		fmt.Fprintln(in.out, FormatValue(v))
		return nil

	case *ast.ExprStmt:
		_, err := in.eval(n.X, env)
		return err

	case *ast.BlockStmt:
		// 块引入新作用域。用同一个 parent 链，不复制环境：
		// 复制会让闭包捕获到一份快照，而不是活的作用域。
		return in.execBlock(n.Stmts, NewEnv(env))

	case *ast.IfStmt:
		cond, err := in.eval(n.Cond, env)
		if err != nil {
			return err
		}
		if semantics.Truthy(cond) {
			return in.execBlock(n.Then, NewEnv(env))
		}
		if len(n.Else) > 0 {
			return in.execBlock(n.Else, NewEnv(env))
		}
		return nil

	case *ast.WhileStmt:
		for {
			cond, err := in.eval(n.Cond, env)
			if err != nil {
				return err
			}
			if !semantics.Truthy(cond) {
				return nil
			}
			if err := in.execBlock(n.Body, NewEnv(env)); err != nil {
				return err
			}
		}

	case *ast.ReturnStmt:
		var v any
		if n.Value != nil {
			var err error
			v, err = in.eval(n.Value, env)
			if err != nil {
				return err
			}
		}
		panic(returnSignal{value: v, pos: n.P})

	default:
		return diag.New(diag.StageRuntime, s.Pos().Line, s.Pos().Col, "内部错误：未知语句类型 %T", s)
	}
}
