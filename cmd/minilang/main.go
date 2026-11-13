// Command minilang 是 minilang 语言的命令行入口。
//
//	minilang run <file.ml>        执行文件
//	minilang repl                 交互式解释器
//	minilang bench                跑 fib(30) 基准测试
//
// 三个调试开关（--dump-tokens / --dump-ast / --dump-bytecode）可以任意组合，
// 用来逐段检查流水线。它们输出到 stdout，执行结果也输出到 stdout，
// 报错输出到 stderr —— 这样 `minilang run x.ml > out.txt` 拿到的是干净的程序输出。
//
// 文件分工：main.go 只做子命令分发；flags.go 管参数；
// session.go 管「词法→语法→执行」流水线；repl.go 是交互式解释器；
// bench.go 是基准测试（含行数统计，且只在 bench 时才统计）。
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const version = "0.1.0"

func main() {
	args := os.Args[1:]

	// 先把子命令摘出来，剩下的才交给 flag 解析。
	cmd := "run"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "repl", "bench", "run":
			cmd = args[0]
			args = args[1:]
		case "help", "--help", "-h":
			usage()
			return
		case "version", "--version", "-v":
			fmt.Printf("minilang %s\n", version)
			return
		}
	}

	o := defaultOptions()
	fs := flag.NewFlagSet("minilang", flag.ExitOnError)
	addFlags(fs, &o)
	// 自己先把参数分成「开关」和「位置参数」两组。
	// 标准库 flag 遇到第一个非开关参数就停止解析，
	// 于是 `minilang run a.ml --dump-ast` 里的 --dump-ast 会被静默忽略 ——
	// 这个坑不修的话，验收时最常用的写法恰好不生效。
	positional := parseArgs(fs, args)
	checkEngine(&o)

	switch cmd {
	case "repl":
		runREPL(o)
	case "bench":
		runBench(o)
	default:
		if len(positional) < 1 {
			usage()
			os.Exit(2)
		}
		os.Exit(runFile(positional[0], o))
	}
}
