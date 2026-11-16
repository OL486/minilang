## 改了什么

<!-- 一句话 + 必要的背景。 -->

## 关联 Issue

Closes #<!-- issue 号 -->

## 怎么验证的

<!-- 贴关键命令的结果，不要只写“测过了”。 -->

```text
make check
make examples
```

## 检查清单

- [ ] `make check` 全绿（gofmt + `go vet` + `go test`）
- [ ] `make examples` 全绿：全部示例在 treewalk 与 VM 两个引擎上输出逐字节一致
- [ ] 涉及语义改动时，两个引擎一起改了，差分测试已补（只改一边就是 bug）
- [ ] 涉及资源上限改动时，`docs/00-spec.md` §6 限额表已同步更新
- [ ] 用户可见的改动已更新 `CHANGELOG.md`（只记已合进 `main` 的内容）
- [ ] 基准数字如有变化，是 `minilang bench` 实测的，已注明机器与口径
- [ ] 没有引入第三方依赖
