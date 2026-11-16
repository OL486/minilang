---
name: Bug 报告
about: 程序行为与规范 / 文档不一致，或崩溃、输出错误
title: "[bug] "
labels: bug
assignees: ""
---

## 现象

<!-- 一句话：发生了什么，期望发生什么。 -->

## 最小复现

<!-- 贴能复现的最小 .ml 程序，越短越好。不要贴整个项目。 -->

```minilang
<!-- 在这里写复现代码 -->
```

跑的命令：

```bash
minilang run repro.ml
```

<!-- 如果只在某个引擎上复现，注明；两个引擎行为不一样本身就是 bug。 -->
引擎：<!-- 默认 VM / --engine=tree / 两个都复现 -->

## 实际输出

<!-- 贴完整输出，含报错文本与退出码。 -->

```text
```

## 期望输出

<!-- 按 docs/00-spec.md，这个程序应该输出什么。 -->

## 环境

- minilang 版本：<!-- `minilang version` 的输出 -->
- Go 版本：<!-- `go version` 的输出 -->
- 操作系统：<!-- 如 Linux x86_64 / Windows 11 / macOS arm64 -->

## 补充

<!-- 相关的 spec 章节、已查过的文档、怀疑的范围。 -->
