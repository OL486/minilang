package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

type options struct {
	dumpTokens   bool
	dumpAST      bool
	dumpBytecode bool
	trace        bool
	stats        bool
	engine       string
	gcOff        bool
	out          io.Writer
	errOut       io.Writer
	// stdin 是 `minilang run -` 的输入来源，默认 os.Stdin；
	// 设为其他 reader 只为让测试能注入 stdin，不影响正常启动路径。
	stdin io.Reader
}

func defaultOptions() options {
	return options{engine: "vm", out: os.Stdout, errOut: os.Stderr, stdin: os.Stdin}
}

func addFlags(fs *flag.FlagSet, o *options) {
	fs.BoolVar(&o.dumpTokens, "dump-tokens", false, "打印词法分析后的 token 流（类型/字面量/行号）")
	fs.BoolVar(&o.dumpAST, "dump-ast", false, "打印解析后的 AST（缩进树形）")
	fs.BoolVar(&o.dumpBytecode, "dump-bytecode", false, "打印反汇编结果（偏移/opcode/操作数/常量池）")
	fs.BoolVar(&o.trace, "trace", false, "逐指令打印 pc / opcode / 栈顶（仅 VM 引擎）")
	fs.BoolVar(&o.stats, "stats", false, "执行结束后打印对象堆与 GC 统计")
	fs.BoolVar(&o.gcOff, "gc-off", false, "关闭 GC（仅用于对比，长时间运行会吃满内存）")
	fs.StringVar(&o.engine, "engine", "vm", "执行引擎: vm | tree")
}

// parseArgs 把参数重排成「所有开关在前、位置参数在后」再交给 flag。
// 只有 -engine 需要带值，且支持 -engine=vm 与 -engine vm 两种写法。
func parseArgs(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) == 0 || a[0] != '-' || a == "-" {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		if !containsEq(a) && (a == "-engine" || a == "--engine") && i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	_ = fs.Parse(flags)
	return positional
}

func containsEq(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return true
		}
	}
	return false
}

func checkEngine(o *options) {
	if o.engine != "vm" && o.engine != "tree" {
		fmt.Fprintf(os.Stderr, "未知引擎 %q，可选 vm | tree\n", o.engine)
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stdout, `minilang %s — 一门带字节码 VM 的小语言

用法:
  minilang run <file.ml|-> [flags]  执行脚本（- 从 stdin 读）
  minilang <file.ml> [flags]       同上
  minilang check <file.ml|-> [flags] 只做词法/语法/编译检查，不执行（- 从 stdin 读）
  minilang repl [flags]            交互式解释器
  minilang bench [flags]           fib(30) 基准测试

调试开关:
  --dump-tokens      打印 token 流
  --dump-ast         打印语法树
  --dump-bytecode    打印反汇编
  --trace            逐指令打印 pc / opcode / 栈顶（仅 VM 引擎）
  --engine=vm|tree   选择执行引擎（默认 vm）
  --stats            执行后打印对象堆统计
  --gc-off           关闭 GC（对比用）

REPL 命令:
  :help  显示帮助    :quit  退出
  :gc    立即回收    :stats 查看堆统计
  :load <file>  执行文件    :engine [tree|vm]  切换引擎
  :history  查看输入历史（保存在 ~/.minilang_history，最近 500 行）
`, version)
}
