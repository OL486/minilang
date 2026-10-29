package main

import (
	"strings"
	"testing"
	"time"
)

func TestCountLOCFindsModule(t *testing.T) {
	total, code := countLOC()
	if total == 0 {
		t.Fatalf("countLOC 统计到 0 行，说明 moduleRoot 没找到 go.mod")
	}
	if code == 0 {
		t.Fatalf("countLOC 的非空非注释行数为 0，统计逻辑可能失效")
	}
	if code > total {
		t.Fatalf("代码行数 %d 大于总行数 %d，统计逻辑反了", code, total)
	}
}

func TestNeedsMoreInput(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"未闭合花括号", "if (x) {", true},
		{"未闭合圆括号", "print (1 +", true},
		{"括号配平", "if (x) { print 1; }", false},
		{"字符串里的右括号不参与计数", `print "}";`, false},
		{"未闭合字符串", `print "abc`, true},
		{"普通语句", "let x = 1;", false},
		{"转义引号不结束字符串", `print "a\"b";`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := needsMoreInput(c.src); got != c.want {
				t.Fatalf("needsMoreInput(%q) = %v，期望 %v", c.src, got, c.want)
			}
		})
	}
}

func TestSignedBytesHandlesNegative(t *testing.T) {
	// 直接相减会 uint64 下溢成 1.7e19，这个测试锁住「负数带 - 号」的行为。
	if got := signedBytes(-1024); !strings.HasPrefix(got, "-") {
		t.Fatalf("signedBytes(-1024) = %q，期望以 - 开头", got)
	}
	if got := signedBytes(1024); !strings.HasPrefix(got, "+") {
		t.Fatalf("signedBytes(1024) = %q，期望以 + 开头", got)
	}
}

func TestMedianDuration(t *testing.T) {
	// 传入乱序，验证中位数取的是排序后的中间值，而不是原顺序中间值。
	ds := []time.Duration{3 * time.Millisecond, 1 * time.Millisecond, 2 * time.Millisecond}
	if got := medianDuration(ds); got != 2*time.Millisecond {
		t.Fatalf("medianDuration 返回 %v，期望 2ms", got)
	}
}

func TestHumanBytes(t *testing.T) {
	if got := humanBytes(512); got != "512 B" {
		t.Fatalf("humanBytes(512) = %q", got)
	}
	// 1024 B 必须进位成 1.00 KiB，而不是 "1024 B"
	if got := humanBytes(1 << 10); got != "1.00 KiB" {
		t.Fatalf("humanBytes(1024) = %q，期望 1.00 KiB", got)
	}
	if got := humanBytes(1 << 20); got != "1.00 MiB" {
		t.Fatalf("humanBytes(1MiB) = %q，期望 1.00 MiB", got)
	}
	if got := humanBytes(2048); !strings.Contains(got, "KiB") {
		t.Fatalf("humanBytes(2048) = %q，期望 KiB 单位", got)
	}
}
