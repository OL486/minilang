package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ---------- REPL ----------

// maxHistoryLines 是持久历史文件的上限：只保留最近的 N 行。
const maxHistoryLines = 500

// historyFileName 是持久历史文件的文件名，放在用户主目录下。
const historyFileName = ".minilang_history"

func runREPL(o options) {
	runREPLIO(o, os.Stdin, os.Stdout, historyPath())
}

// historyPath 返回持久历史文件的路径；拿不到主目录时返回 ""（表示禁用历史）。
func historyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, historyFileName)
}

// history 是 REPL 的输入历史：启动时从文件读入，退出时写回。
// 注意：这里只有「持久记录」，没有行编辑——方向键召回历史
// 需要 raw mode 的自研行编辑，那是另一档工作，见 issue。
type history struct {
	lines []string
}

func loadHistory(path string) *history {
	h := &history{}
	if path == "" {
		return h
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// 文件不存在是正常情况（第一次进 REPL），静默返回空历史。
		return h
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		h.lines = append(h.lines, line)
	}
	h.trim()
	return h
}

// add 记录一行输入：空行不记，连续重复不记。
func (h *history) add(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	if n := len(h.lines); n > 0 && h.lines[n-1] == line {
		return
	}
	h.lines = append(h.lines, line)
	h.trim()
}

func (h *history) trim() {
	if len(h.lines) > maxHistoryLines {
		h.lines = h.lines[len(h.lines)-maxHistoryLines:]
	}
}

// save 把历史写回文件；path 为空或写失败时静默跳过——
// 历史丢了不值得打断 REPL。
func (h *history) save(path string) {
	if path == "" || len(h.lines) == 0 {
		return
	}
	_ = os.WriteFile(path, []byte(strings.Join(h.lines, "\n")+"\n"), 0600)
}

// runREPLIO 是 REPL 的实现：in/out/histPath 均可注入，runREPL 只传
// os.Stdin/os.Stdout/historyPath()。
// 统一自 #47 与 #48 的两个版本：#47 注入输入与历史路径，#48 注入输入与
// 输出；统一后四个参数一次满足两边测试的注入需求。
func runREPLIO(o options, in io.Reader, out io.Writer, histPath string) {
	if out == nil {
		out = os.Stdout
	}
	if o.errOut == nil {
		o.errOut = os.Stderr
	}
	// 只有 o.out 未设置时才用注入的 out：调用方可能已把 o.out 指到别处
	//（例如测试把程序输出接到 buffer、提示符走 os.Stdout），无条件覆盖
	// 会让 session 的输出跑掉。生产路径 defaultOptions 已设 out=os.Stdout，
	// 与注入的 out 是同一处，行为不变。
	if o.out == nil {
		o.out = out
	}

	s := newSession(o)
	hist := loadHistory(histPath)
	defer hist.save(histPath)

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	fmt.Fprintf(out, "minilang %s  [engine=%s]  :help 查看命令，:quit 退出\n", version, o.engine)
	var pending strings.Builder

	for {
		if pending.Len() == 0 {
			fmt.Fprint(out, "> ")
		} else {
			fmt.Fprint(out, "| ")
		}
		if !scanner.Scan() {
			fmt.Fprintln(out)
			if err := scanner.Err(); err != nil {
				if errors.Is(err, bufio.ErrTooLong) {
					fmt.Fprintln(out, "  输入行超过 4 MiB 上限，REPL 退出")
				} else {
					fmt.Fprintf(out, "  读取输入失败: %v\n", err)
				}
			}
			return
		}
		line := scanner.Text()
		hist.add(line)

		if pending.Len() == 0 {
			cmd := strings.TrimSpace(line)
			switch {
			case cmd == ":quit" || cmd == ":q" || cmd == "exit":
				return
			case cmd == ":help" || cmd == ":h":
				fmt.Fprint(out, "  :help    帮助\n  :quit    退出\n  :gc      立即执行一次标记-清除\n  :stats   查看对象堆统计\n  :load <file>  执行文件内容\n  :engine [tree|vm]  查看/切换执行引擎（切换后新建会话）\n  :history  查看输入历史\n")
				continue
			case cmd == ":gc":
				if s.vm != nil {
					before := s.vm.Stats()
					s.vm.CollectGarbage()
					after := s.vm.Stats()
					fmt.Fprintf(out, "  GC: 对象 %d -> %d, 堆 %d B -> %d B\n", before.Objects, after.Objects, before.Bytes, after.Bytes)
				} else {
					fmt.Fprintln(out, "  tree 引擎没有自己的对象堆，:gc 不做任何事")
				}
				continue
			case cmd == ":stats":
				if s.vm != nil {
					printStats(out, s.vm)
				} else {
					fmt.Fprintln(out, "  tree 引擎没有自己的对象堆")
				}
				continue
			case cmd == ":history":
				for i, l := range hist.lines {
					fmt.Fprintf(out, "  %4d  %s\n", i+1, l)
				}
				continue
			case cmd == ":engine":
				fmt.Fprintf(out, "  当前引擎: %s\n", s.opt.engine)
				continue
			case cmd == ":load":
				fmt.Fprintln(out, "  用法: :load <file>")
				continue
			case strings.HasPrefix(cmd, ":engine ") || strings.HasPrefix(cmd, ":engine\t"):
				fields := strings.Fields(cmd)
				if len(fields) != 2 {
					fmt.Fprintln(out, "  用法: :engine tree|vm")
					continue
				}
				switch fields[1] {
				case "vm", "tree":
					o.engine = fields[1]
					s = newSession(o)
					fmt.Fprintf(out, "  已切换到 %s 引擎（新会话，之前定义的变量已清空）\n", fields[1])
				default:
					fmt.Fprintf(out, "  未知引擎 %q，可选 vm | tree\n", fields[1])
				}
				continue
			case strings.HasPrefix(cmd, ":load ") || strings.HasPrefix(cmd, ":load\t"):
				fields := strings.Fields(cmd)
				if len(fields) != 2 {
					fmt.Fprintln(out, "  用法: :load <file>")
					continue
				}
				data, err := os.ReadFile(fields[1])
				if err != nil {
					fmt.Fprintf(o.errOut, "无法读取文件 %s: %v\n", fields[1], err)
					continue
				}
				// 复用 runSource：语法/运行时错误走同一套诊断格式，不退出 REPL。
				s.runSource(string(data), filepath.Base(fields[1]))
				continue
			case cmd == "":
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
