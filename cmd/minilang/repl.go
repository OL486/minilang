package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ---------- REPL ----------

func runREPL(o options) {
	runREPLIO(o, os.Stdin, os.Stdout)
}

// runREPLIO 是 REPL 的实现：in/out 可注入，runREPL 只传 os.Stdin/os.Stdout。
func runREPLIO(o options, in io.Reader, out io.Writer) {
	s := newSession(o)
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	fmt.Fprintf(out, "minilang %s  [engine=%s]  :help 查看命令，:quit 退出\n", version, o.engine)
	var pending strings.Builder

	for {
		if pending.Len() == 0 {
			fmt.Fprint(out, "> ")
		} else {
			fmt.Fprint(out, "| ")
		}
		if !sc.Scan() {
			fmt.Fprintln(out)
			if err := sc.Err(); err != nil {
				if errors.Is(err, bufio.ErrTooLong) {
					fmt.Fprintln(out, "  输入行超过 4 MiB 上限，REPL 退出")
				} else {
					fmt.Fprintf(out, "  读取输入失败: %v\n", err)
				}
			}
			return
		}
		line := sc.Text()

		if pending.Len() == 0 {
			cmd := strings.TrimSpace(line)
			switch cmd {
			case ":quit", ":q", "exit":
				return
			case ":help", ":h":
				fmt.Fprint(out, "  :help   帮助\n  :quit   退出\n  :gc     立即执行一次标记-清除\n  :stats  查看对象堆统计\n")
				continue
			case ":gc":
				if s.vm != nil {
					before := s.vm.Stats()
					s.vm.CollectGarbage()
					after := s.vm.Stats()
					fmt.Fprintf(out, "  GC: 对象 %d -> %d, 堆 %d B -> %d B\n", before.Objects, after.Objects, before.Bytes, after.Bytes)
				} else {
					fmt.Fprintln(out, "  tree 引擎没有自己的对象堆，:gc 不做任何事")
				}
				continue
			case ":stats":
				if s.vm != nil {
					printStats(out, s.vm)
				} else {
					fmt.Fprintln(out, "  tree 引擎没有自己的对象堆")
				}
				continue
			case "":
				continue
			}
		}

		pending.WriteString(line)
		pending.WriteString("\n")

		// 括号/花括号没配平就继续读下一行。
		// 这是 REPL 里最容易被忽略的体验细节：没有它，
		// 输入 `if (x) {` 回车就会立刻报一个莫名其妙的语法错误。
		if needsMoreInput(pending.String()) {
			continue
		}
		src := pending.String()
		pending.Reset()
		s.runSource(src, "<repl>")
	}
}

// needsMoreInput 判断是否需要继续读取。
// 只统计字符串字面量之外的括号 —— 否则 `print "}";` 会被误判。
func needsMoreInput(src string) bool {
	depth := 0
	inString := false
	escaped := false
	for _, r := range src {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '(', '{':
			depth++
		case ')', '}':
			depth--
		}
	}
	return depth > 0 || inString
}
