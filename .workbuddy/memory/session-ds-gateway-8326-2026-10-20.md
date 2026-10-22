# 会话 ds 端记忆锚点（2026-10-20 16:4X）：网关错误 8326 根因与修复

## 一句话

老大实报的「网关错误：端口未监听: dial tcp 127.0.0.1:8326: connectex: ...」不是网关故障，
是**两条缺陷叠加**：措辞错位（把正常环境态渲染成红色故障）+ 缺陷放大（预算判据恒假 + 同址重复拨号）。

## 根因（完整链）

1. `gateway_check.go:44`（修复前）把 Go 裸 dial 错误塞进 `Detail`；
2. `assets_pool.go:68`（修复前）一律用 `color:var(--danger)` + 「网关错误：」渲染；
3. `service.go:212` 本就有 catalog 回退兜底（8026 没起时模型照常纳管）——所以这**本不该是红色**；
4. `gateway_check.go:112`（修复前）`time.Now().After(time.Now().Add(budget))` **恒假** ⇒ budget 从未生效；
5. 候选是 base × key 笛卡尔积：本机 1 个 base，任务隔离 HOME 实测 **225 份 api-key**
   ⇒ 端口没开时逐个 key 重复拨同一个端口。

实测量级：`dist/workbench-output/models/model-status.jsonl` 里 `gateway-fail` **143 条**，
且每 600s 一条连续成片（1792478450962 → 1792483292173 连续 9 个周期）。

## 修复

- `gateway_check.go`：本机回环未监听 → 可行动中文；预算改真 deadline；抽 `discoverVerifiedWith` 内核，
  同 base 端口没监听后跳过该 base 的其余 key。
- `assets_pool.go`：按 `gateway.reachable/authorized` 分三级措辞（未监听=中性 / 鉴权失败=红 / 其余=错误）。
- 新增 3 个测试文件 6+2 条判据（含端到端 SyncNow 与真实候选等价性）。

## 验证证据（可复跑）

- `go vet ./...` = 0；`go test ./... -count=1` **27/27 包全绿**；modelpool 覆盖 68.6%。
- **变异测试**：把两处修复分别改回缺陷形态，判据立刻红——
  预算回退恒假 ⇒ `TestDiscoverBudgetIsHonored` FAIL（20/20 次拨号）；
  去掉同址短路 ⇒ `TestDiscoverSkipsKeyLoopOnDeadBase` FAIL（30 次重复拨号）。
- **隔离实例端到端**：关闭端口 + 临时便携根，`model-status.jsonl` 记录可行动中文，catalog 回退 `新增 2`。
- **活体部署**：`dist/workbench.exe` 已换新件并复验（网关在线 `available=53/65`）。

## 方法教训（值得固化）

- **「错误信息」要当交付物审**：用户看到的那一行，决定了它被判成故障还是环境态。
  这个 bug 的代码逻辑其实是**对的**（有兜底、不判死模型），坏的是它**呈现**出来的样子。
- **恒假判据要专门找**：`X.After(X.Add(budget))` 这种「看起来在判预算、实际恒假」的形式，
  编译器与 vet 都不报。写判据时先问「这个条件在什么输入下会为真」，答不上来就是恒假式。
- **笛卡尔积候选在「资源不可达」时是放大器**：1 base × N key，端口一死就放大 N 倍拨号与噪音。
  按「资源维度」而非「凭据维度」短路。
