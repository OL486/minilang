# minilang —— 常用任务入口。
#
# 只需要 Go 1.22+，不需要 make 之外的任何工具：
# `make release` 会交叉编译全部平台并用 Go 自己打包（tools/pack），
# 不依赖 zip/tar/sha256sum 这些在 Windows 上不齐的命令。

BINARY  := minilang
VERSION := $(shell sed -n 's/^const version = "\(.*\)"/\1/p' cmd/minilang/main.go)
LDFLAGS := -s -w

ifeq ($(OS),Windows_NT)
	EXE := .exe
else
	EXE :=
endif

OUT := $(BINARY)$(EXE)

.PHONY: all build install test vet fmt fmt-check check release clean bench examples repl help

all: check

## build: 构建当前平台的二进制
build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT) ./cmd/minilang
	@echo "built $(OUT) (version $(VERSION))"

## install: 安装到 $GOBIN / $GOPATH/bin
install:
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/minilang

## test: 跑全部测试
test:
	go test ./... -count=1

## vet: 静态检查
vet:
	go vet ./...

## fmt: 格式化
fmt:
	go fmt ./...

## fmt-check: 检查格式（CI 用，不修改文件）
fmt-check:
	@out=$$(gofmt -l ./cmd ./internal ./test ./tools 2>/dev/null); \
	if [ -n "$$out" ]; then echo "以下文件未格式化:"; echo "$$out"; exit 1; fi; \
	echo "gofmt: clean"

## check: 提交前必过的一道关（格式 + 静态检查 + 测试）
check: fmt-check vet test
	@echo "check: all green"

## release: 交叉编译 5 个平台并打包到 dist/
release:
	go run tools/pack/main.go

## bench: 两个引擎各跑一次 fib(30) 基准
bench: build
	./$(OUT) bench
	./$(OUT) bench --engine=tree

## examples: 跑全部示例，并逐个校验两个引擎输出一致
examples: build
	@fail=0; \
	for f in examples/*.ml; do \
		if diff -q <(./$(OUT) run "$$f") <(./$(OUT) run "$$f" --engine=tree) >/dev/null 2>&1; then \
			echo "OK   $$f"; \
		else \
			echo "FAIL $$f"; fail=1; \
		fi; \
	done; \
	exit $$fail

## repl: 启动交互式解释器
repl: build
	./$(OUT) repl

## clean: 清掉二进制与打包产物
clean:
	rm -rf dist $(BINARY) $(BINARY).exe

## help: 列出所有目标
help:
	@echo "用法: make <target>"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'
