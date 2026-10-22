# 会话 ds 端记忆锚点 R2（2026-10-19 12:50 · M2 远端+CI 基建）

## 本轮成果（老大给了 GitHub PAT 后）
- gh auth login --with-token（账户 lxh113377）；临时 token 文件用完即擦除。
- 建仓 lxh113377/workbench：先私有 → 因私有仓 Actions 计费失败（job 拒启动）转公开（免费）。
- origin 配好，推送 master；修 ci.yml 触发分支 [main]->[main, master]（默认分支实为 master，否则不触发）。
- CI run 39199540690 真跑 2m2s：vet+test(race) 败于 web/workspace 7 用例（Windows 8.3 短路径 RUNNER~1 vs runneradmin 形态差异，CI runneradmin 环境特有；本机同两包全绿）——归 Lead 平台耦合家族，未改其写入口。
- autonomy-e2e.yml 触发分支仍是 [main]，push 到 master 不触发、未注册成 workflow——归 Lead。
- 提交纪律：path-limited add，只提交自己的文件，Lead 41 个未提交改动零碰。

## 复用经验（本条链路下次可直接照走）
1. PAT 配置：gh auth login --with-token 从临时文件读（勿进 argv），用完覆写+删除。
2. GitHub 私有仓 Actions 是付费的：报「account payments have failed / spending limit」=> 要么充值、要么转公开（--visibility public --accept-visibility-change-consequences）。
3. 建仓后默认分支是 master，但 workflow 模板常写 branches:[main] => push 不触发、gh run list 空。务必核对默认分支与触发分支一致。
4. 在他人脏工作区提交：只用 path-limited add（git add -- <精确路径>），提交前 diff --cached --name-only 核对暂存区只有自己文件。
5. CI 失败先本机复现：本机绿而 CI 红，多为环境特有（账户/临时目录/8.3 短路径），改测试归属要看写入口分工。
6. 本机跑 go 需先设 GOCACHE（D:\workdesktop\.workbuddy\gocache），否则报 build cache 缺失。
