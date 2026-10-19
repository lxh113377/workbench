# 模型池 Docker 端到端门禁

> 对应老大指令「用 Docker 虚拟机模拟，别乱折腾了」。
> 归属：`tests/docker/`（容器 e2e）。**不碰 `workbench/` 源码**，发现的缺陷以复现证据交回。

## 一条命令

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File tests\docker\run_pool_e2e.ps1
```

四腿，任一红即退出码 1：

| 腿 | 内容 |
|---|---|
| 1 | 交叉构建 4 个夹具（`CGO_ENABLED=0 GOOS=linux`，静态）+ `go vet` |
| 2 | 交叉构建 workbench linux 二进制 + ELF 静态性自检 |
| 3 | `debian:bookworm-slim`（dash）+ `alpine:3.20`（busybox ash）各跑一遍全链断言 |
| 4 | `golang:1.27-bookworm` 干净工具链跑 `go vet` + `go test`（池门禁必绿；全仓测试作情报项） |

常用参数：

```powershell
-SelfCheckOnly                    # 只验 mock 网关地基（15 项）
-Binary D:\x\workbench-linux      # 用现成二进制（源码树未恢复时）
-Source D:\path\workbench         # 对指定源码树构建并跑门禁（对照验证）
-SkipBuild                        # 复用已构建夹具
```

## 为什么必须在容器里跑

1. **本机绿不算数**：宿主反复重建 EXE、与并行会话抢文件，正是「乱折腾」。
2. **宿主 OFM 网关连不到**：它绑 `127.0.0.1:8326`，而容器内的回环是容器自己的。
   所以用 mock 网关 —— 顺带把「鉴权被拒 / 上游 5xx / 端口没开」变成可控可复现的断言。
3. **交叉编译可行**：workbench 纯标准库，`CGO_ENABLED=0 GOOS=linux` 实测通过。

## 目录

```
tests/docker/
  run_pool_e2e.ps1      宿主驱动（四腿）
  pool_e2e.sh           容器内全链断言（79 项，POSIX sh）
  mock_selfcheck.sh     mock 网关地基自检（15 项）
  testdata/             数据面夹具（单一数据源，e2e 与诊断共用）
  mockofm/              模拟 OFM 渠道网关：/v1/models + /v1/chat/completions + 三态热切
  pooljson/             把 status/routes/probe 的 JSON 摊成 KEY=VALUE（alpine 无 jq）
  poolapi/              驱动面板 /api/pool/*（自动从 /admin 取 token，支持坏 token）
  httpget/              最小 HTTP 客户端，返回真实 HTTP_STATUS
  diag/                 一次性取证脚本（保留复现路径，不参与门禁）
  bin/ fixtures/        构建产物与工作区（.gitignore 排除，勿入库）
```

## 反假绿设计（本轮实测踩出来的，不是理论）

| 坑 | 实测证据 | 对策 |
|---|---|---|
| debian:bookworm-slim **既没 curl 也没 wget** | `wget: not found` 退出码 127 | 自带 `httpget` 静态二进制 |
| busybox wget 对 4xx 只给非 0 退出码，与「工具缺失 127」撞值 | 「应当被拒」的断言在缺工具时**假绿**（实测 5 PASS 全是假的） | 断言取真实 `HTTP_STATUS`；`000`(连不上)/`401`/`502` 可区分 |
| 链路坏了继续计数 ⇒ 空跑判绿 | — | `start_panel` 后先自检拿到 200，拿不到就 `bail` 退出 |
| `/test` 只读挂载，脚本写不进去 | `cannot create /test/mock-mode` | 临时文件一律写 `/tmp` |
| 判据被管道吞退出码 | — | 按标记行判定（`E2E_PASS`/`MOCK_CHECK_PASS`），不看 rc |
| ok/total 自洽性 | — | `ok != total` 或 `total < 50` 一律判红（防检查项静默消失） |

## 断言口径的两个关键约定

1. **一律走常驻面板进程**。CLI 每次新建 Service，`lastProbe`/网关健康是内存态 ——
   `--model-pool status` 里 `PROBE_OK` 恒 0、`GATEWAY_STATUS` 恒 0，拿它断言等于断一个空读数。
   且 CLI 输出的是未脱敏原始 Spec，其 `state` 是 `json:"-"` 不落 JSON，逐模型状态只有面板 `redactSpec` 才有。
2. **每个网关态用干净面板**。面板常驻会把上一态的冷却留在内存里污染下一态读数
   （实测 `AVAILABLE` 恒 0）。故 `stop_panel; reset_health; start_panel` 三件套。

另有时序坑：`panel.url` 文件出现时启动探测（延迟 1.5s）还没跑，
必须等 `PROBE_RUNS>=1` 才断言，否则 `PROBE_OK` 读到 0（实测 `PROBE_RUNS=0` → 6 秒后 `PROBE_OK=9`）。

## 门禁抓到的真实缺陷（均有复现证据）

### 1. 带斜杠模型 id 的 action 路由（已修，live 树已吸收）

`/api/pool/models/<id>/disable` 里 `<id>` 本身含斜杠（`z-ai/glm-5.3`、`vendor/e2e-model`）。
按 `/` 切第一段会把 id 截成 `vendor`、把 `e2e-model` 当动作词，
于是停用/启用/恢复/删除全 400。

**为什么单测没抓到**：`pool_api_test.go` 只测了带斜杠 id 的**删除**，没测 disable/enable/restore。
容器 e2e 补上了这个盲区 —— 这正是把验证搬进 Docker 的价值。

### 2. 地区受限模型被自动恢复启用（本轮修复）

`sync.go` 的自动恢复逻辑用「网关此刻仍在广播该模型」当作「上游判定已过期」的证据。
但地区受限模型被网关广播成 `our-free-model-region/<id>` —— 对这类模型该信号**恒真**，
于是每轮同步都把「默认停用」翻回启用。

复现：`region_revive_test.go` 的 `TestSyncDoesNotReviveRegionLimited`（修前 FAIL，修后 PASS）。
修法：恢复判据加 `broadcastRoutable()` —— 只有裸 `our-free-model/<id>`（可路由车道）
的重新出现才算上游判定过期；`-region` 前缀的出现恰恰是「仍然受限」的复证。

同时清掉 `sync.go` 里两段重复的 revive 块（合并产物，第一段绕过了「写入后才 SetEnabled」的顺序修正）。

### 3. mock 网关 `/ctl/state` 自死锁（本轮修复）

`g.mu.Lock()` 后调用会再加锁的 `currentMode()` —— `sync.Mutex` 不可重入，该请求永久挂起。
与项目记忆里 `chainBonus` 持写锁再取读锁同形。修法：临界区外先取好 mode。

## 容器内不可验证项（如实记录，不折算通过）

| 项 | 原因 | 佐证来源 |
|---|---|---|
| 状态日志 `kind=switch` / `skip` | 需要真实模型调用失败；容器内没有 DSH headless | 本机 `workbench-output/models/model-status.jsonl` 实测有 switch 记录 |
| 真实 OFM 上游可用性 | 容器出口 IP 与宿主不同，且宿主网关绑回环 | 本机面板实测 total=35 / available=25 |

## 全仓平台耦合清单（交 Lead，属 `workbench/` 范围）

第 4 腿会把容器内失败的用例写到 `fixtures/platform-coupling.txt`。
本轮实测 **17 包通过 / 11 用例失败 / 5 包涉及**，均为硬编码 Windows 假设，例如：

- `TestWorkspaceAPIBrowse`：断言「空路径返回盘符列表」→ Linux 无盘符（Windows 绿 / Linux 红，已双侧实测）
- `TestRepoRootHasRequiredAnchors`：仓根锚点用反斜杠路径
- `TestTrashRefusesDangerous`：回收站是 Windows 概念
- `TestRunPassAndFail` / `TestRunTimeout`：依赖外部命令的 Windows 行为

这些**不是**容器环境的错，是「别人机器上能不能跑」的真问题 —— 按 Lead 的话：
「那是要修的问题，不是要删仓库的理由」。

## 实测结果（2026-10-19 12:15）

```
==> 交叉构建夹具 linux/amd64 CGO=0            PASS
==> 交叉构建 workbench linux/amd64            PASS
==> 产物静态性自检(ELF)                        PASS
==> 模型池全链验收 debian:bookworm-slim(dash)  E2E_PASS ok=79 total=79
==> 模型池全链验收 alpine:3.20(busybox ash)    E2E_PASS ok=79 total=79
==> 池门禁 go test (golang:1.27-bookworm)      PASS (modelpool + web TestPool 全绿)
==> 全仓 go test 平台耦合体检（情报项）          容器内通过包=17 失败用例=11 涉及包=5
DOCKER_GATE_PASS
```

抖动验证：同一二进制连跑 3 次，均 `E2E_PASS ok=79 total=79`（修 region 缺陷前会随机翻红 1 项）。
两发行数与结果完全一致，证明断言不含发行版特定假设。


---

## 附：尚未提交的修复（防再次丢失，完整 diff 留档于此）

仓库刚经历过一次损毁（10:15 通报），以下改动截至 12:24 仍只在`workbench/`工作区、
未进任何提交。`workbench/` 的写入口归主会话，故不代为提交；若工作区再次被清空，
可照此恢复。

### A. `workbench/internal/modelpool/sync.go`（两处）

1. 新增导出前辅助函数：

```go
// broadcastRoutable 判断网关是否以「可路由车道」形态广播了该模型。
//
// 只有裸 our-free-model/<id> 算可路由；our-free-model-region/<id> 是地区受限、
// deepseek-official/<m> 是网关专属车道，两者都不能作为"上游判定已过期"的证据。
func broadcastRoutable(gw Gateway, id string) bool {
	want := "our-free-model/" + id
	for _, m := range gw.Models {
		if m == want {
			return true
		}
	}
	return false
}
```

2. `SyncFromGateway` 内：删除重复的第一段 revive 块（它绕过"写入后才 SetEnabled"
的顺序修正），并把第二段的判据加上可路由条件：

```go
// 改前
revive := !existing.Enabled && existing.AutoDisabled
// 改后
revive := !existing.Enabled && existing.AutoDisabled && broadcastRoutable(gw, existing.ID)
```

### B. 新文件 `workbench/internal/modelpool/region_revive_test.go`

两条测试：`TestSyncDoesNotReviveRegionLimited`（改前 FAIL / 改后 PASS）与
`TestSyncRevivesRoutableAutoDisabled`（正例，防把恢复能力一并关掉）。

### C. 实际 git diff（`git diff -- workbench/internal/modelpool/sync.go`）

```diff
git : warning: in the working copy of 'workbench/internal/modelpool/sync.go', LF will be replaced by CRLF the next time
 Git touches it
At line:3 char:1
+ git diff -- workbench/internal/modelpool/sync.go 2>&1 | Out-String
+ ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
    + CategoryInfo          : NotSpecified: (warning: in the... Git touches it:String) [], RemoteException
    + FullyQualifiedErrorId : NativeCommandError
 
diff --git a/workbench/internal/modelpool/sync.go b/workbench/internal/modelpool/sync.go
index 334a4b2..67a957a 100644
--- a/workbench/internal/modelpool/sync.go
+++ b/workbench/internal/modelpool/sync.go
@@ -29,6 +29,20 @@ type SyncResult struct {
 	At      int64    `json:"at"`
 }
 
+// broadcastRoutable 判断网关是否以「可路由车道」形态广播了该模型。
+//
+// 只有裸 our-free-model/<id> 算可路由；our-free-model-region/<id> 是地区受限、
+// deepseek-official/<m> 是网关专属车道，两者都不能作为"上游判定已过期"的证据。
+func broadcastRoutable(gw Gateway, id string) bool {
+	want := "our-free-model/" + id
+	for _, m := range gw.Models {
+		if m == want {
+			return true
+		}
+	}
+	return false
+}
+
 // SpecFromGatewayModel 把一个网关模型 id 翻译成池内 Spec。
 func SpecFromGatewayModel(gw Gateway, modelID string) (Spec, bool) {
 	id := strings.TrimSpace(modelID)
@@ -177,22 +191,18 @@ func SyncFromGateway(m *Manager, gw Gateway, actor string) SyncResult {
 		if existing.Note == "" {
 			existing.Note = spec.Note
 		}
-		// 自动恢复：网关此刻仍在广播该模型，说明上游判定已过期（availability.json
-		// 是插件按探测周期写的旧快照）。只恢复「当初是我们自动停用的」那些，
-		// 用户手动停用的绝不擅自打开。
-		if !existing.Enabled && existing.AutoDisabled {
-			existing.Enabled = true
-			existing.AutoDisabled = false
-			existing.Note = "网关重新广播，已自动恢复启用"
-			res.Revived = append(res.Revived, existing.ID)
-		}
-		// 自动恢复：网关此刻仍在广播该模型，说明上游判定已过期（availability.json
-		// 是插件按探测周期写的旧快照）。只恢复「当初是我们自动停用的」那些，
-		// 用户手动停用的绝不擅自打开。
+		// 自动恢复：网关此刻仍在**可路由车道**广播该模型，说明上游判定已过期
+		// （availability.json 是插件按探测周期写的旧快照）。只恢复「当初是我们
+		// 自动停用的」那些，用户手动停用的绝不擅自打开。
+		//
+		// 必须看广播形态：地区受限模型被网关广播成 our-free-model-region/<id>，
+		// 对这类模型「仍在广播」是恒真信号 —— 拿它当恢复依据会把地区受限
+		// 模型每轮重新启用，"默认停用"形同虚设（容器 e2e 实测断言翻红，
+		// 回归测试 TestSyncDoesNotReviveRegionLimited）。
 		//
 		// 顺序很重要：UpsertPreserve 会以「磁盘上的当前值」为准保留 Enabled，
 		// 所以必须在写入之后再调 SetEnabled(true)，否则恢复动作会被自己覆盖掉。
-		revive := !existing.Enabled && existing.AutoDisabled
+		revive := !existing.Enabled && existing.AutoDisabled && broadcastRoutable(gw, existing.ID)
 		if revive {
 			existing.AutoDisabled = false
 			existing.Note = "网关重新广播，已自动恢复启用"
```
