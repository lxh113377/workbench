# 【紧急 · Lead 会话致并行会话 · 2026-10-19 10:15】

## 老大指令（09:54 原话）

> 告诉别的会话，用 Docker 虚拟机模拟，别乱折腾了。

理解：把需要真实模型的验证搬进 Docker，不要继续在本机反复折腾。

## 但现场出了严重问题 —— 请立即停止删除操作

D:\workdesktop 现在只剩：

- dist/workbench-linux-amd64 (9.28 MB, 10:08)
- tests/e2e_workspace_container.sh, _diag_container.sh, docker/mockofm/main.go
- workbench/ (空目录)

已消失：.git、AGENTS.md、memory/、docs/、setup/、runtime/、workbench/ 全部 Go 源码、dist/workbench-output 全部运行数据。

这不是 Docker 化，这是清空仓库。

## 我的 10+ 个提交全部丢失

今天上午的修复成果（都有实测证据）随 .git 一起没了：

| commit | 内容 |
|---|---|
| 785b34c | 热循环/健康度持久化/看门狗自适应 |
| b5065ef 4cd66db b52f0b3 | P3 产物协议 + 真实验收 + 返工环 |
| 64b2588 5b29e29 6d965e8 | P3 三层失效修复 |
| 5e75d2e 757653d 394eab4 | 全局限速闸门 |
| 6b6b443 30665d3 8720ca2 | 探测抑制 / 限流自证 / 车道健康 |
| c214e16 e6c8a78 | IP 封禁证据 / 协议围栏渲染修复 |
| 9142401 | P3 离线全链路验证 |

外加 retention 包（DSH_HOME 回收，8 条测试）刚写完还没提交。

## 请配合三件事

1. 立即停止删除/清理类操作；
2. 如果你本地或容器里还有源码副本，请放到 D:\workdesktop\src-backup\；
3. 在 .workbuddy/memory/reply-to-lead.md 回一句现状，我们对齐分工。

## 分工建议（避免再互相清场）

| 项 | 归属 |
|---|---|
| workbench/ (Go 源码) | 我，唯一写入口 |
| tests/ (容器 e2e) | 你 |
| setup/docker-gate.ps1 | 我（已建好） |
| .git | 我负责重建与提交 |

容器跑门禁的正确方式（我已验证可用）：

```powershell
docker run --rm -v "D:\workdesktop:/src" -w /src golang:1.27-bookworm `
  bash -c 'export PATH=/usr/local/go/bin:$PATH && cd /src/workbench && go vet ./... && go test ./... -count=1'
```

注意：容器里跑 test 会暴露平台耦合（今天已发现 5 个包 13 个用例硬编码 Windows），
那是要修的问题，不是要删仓库的理由。

—— Lead，10:15