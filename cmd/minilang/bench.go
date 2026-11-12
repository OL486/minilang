package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/OL486/minilang/internal/engine"
	"github.com/OL486/minilang/internal/semantics"
	"github.com/OL486/minilang/internal/tree"
)

// ---------- 基准测试 ----------

// benchSource 是被测程序。用 let 接收结果而不是 print，
// 避免 I/O 时间混进测量区间。
const benchSource = `fn fib(n) {
  if (n < 2) { return n; }
  return fib(n-1) + fib(n-2);
}
let benchResult = fib(30);
`

func runBench(o options) {
	s := newSession(o)

	// 词法 + 语法只跑一次，与 runSource 共用 parseSource，
	// 不再手写第二份流水线。计时只覆盖执行，不含编译。
	stmts, ok := s.parseSource(benchSource, false)
	if !ok {
		os.Exit(1)
	}

	fmt.Printf("minilang 基准测试  engine=%s  fib(30)\n", o.engine)

	var times []time.Duration
	var result string
	var heapBefore, heapAfter uint64

	if o.engine == "tree" {
		heapBefore = readHeap()
		for i := 0; i < 3; i++ {
			start := time.Now()
			if err := s.runTree(stmts); err != nil {
				fmt.Fprintln(o.errOut, "执行失败:", err)
				os.Exit(1)
			}
			times = append(times, time.Since(start))
		}
		heapAfter = readHeap()
		if v, ok := s.interp.Globals().Lookup("benchResult"); ok {
			result = tree.FormatValue(v)
		}
	} else {
		// 编译一次（反汇编在这里打印一次），只计时三次执行。
		fn, ok := s.compile(stmts, "<bench>", benchSource)
		if !ok {
			os.Exit(1)
		}
		heapBefore = readHeap()
		for i := 0; i < 3; i++ {
			start := time.Now()
			if _, err := s.runCompiled(fn); err != nil {
				fmt.Fprintln(o.errOut, "执行失败:", err)
				os.Exit(1)
			}
			times = append(times, time.Since(start))
		}
		heapAfter = readHeap()
		if v, ok := s.vm.Global("benchResult"); ok {
			result = engine.Format(v)
		}
	}

	median := medianDuration(times)
	fmt.Printf("  fib(30) = %s\n", result)
	fmt.Printf("  三次耗时: %s, %s, %s\n", fmtDur(times[0]), fmtDur(times[1]), fmtDur(times[2]))
	fmt.Printf("  中位数:   %s\n", fmtDur(median))
	fmt.Printf("  Go 堆: 运行前 %s -> 运行后 %s（差 %s）\n",
		semantics.HumanBytes(heapBefore), semantics.HumanBytes(heapAfter), semantics.SignedBytes(int64(heapAfter)-int64(heapBefore)))
	total, code := locCounts()
	fmt.Printf("  Go 源码行数: %d 行（非空非注释 %d 行）\n", total, code)
}

func medianDuration(ds []time.Duration) time.Duration {
	cp := make([]time.Duration, len(ds))
	copy(cp, ds)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	return cp[len(cp)/2]
}

func fmtDur(d time.Duration) string {
	return fmt.Sprintf("%.1f ms", float64(d.Nanoseconds())/1e6)
}

func readHeap() uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// ---------- 代码行数统计 ----------
//
// 行数统计只在 bench 需要时才跑一次（sync.Once 懒加载）。
// 以前 init() 在每次启动（包括 `minilang version`）时都从工作目录向上找 go.mod、
// 遍历并读取全部 .go 文件 —— 现在启动路径上不再有任何文件系统遍历。

var (
	locOnce           sync.Once
	locTotal, locCode int
)

func locCounts() (total, code int) {
	locOnce.Do(func() {
		locTotal, locCode = countLOC()
	})
	return locTotal, locCode
}

// countLOC 统计当前模块的 Go 代码行数（不含 _test.go）。
// 用运行期统计而不是外部脚本，是为了让 S10 的对比表在任何机器上都能复现。
func countLOC() (total, code int) {
	root, err := moduleRoot()
	if err != nil {
		return 0, 0
	}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+"node_modules"+string(filepath.Separator)) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		inBlock := false
		for _, line := range strings.Split(string(data), "\n") {
			t := strings.TrimSpace(line)
			if t == "" {
				continue
			}
			total++
			if inBlock {
				if strings.Contains(t, "*/") {
					inBlock = false
				}
				continue
			}
			if strings.HasPrefix(t, "//") {
				continue
			}
			if strings.HasPrefix(t, "/*") {
				if !strings.Contains(t, "*/") {
					inBlock = true
				}
				continue
			}
			code++
		}
		return nil
	})
	return total, code
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("找不到 go.mod")
		}
		dir = parent
	}
}
