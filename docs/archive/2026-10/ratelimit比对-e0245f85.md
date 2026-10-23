# 逐文件差异比对：full-src2 的 ratelimit vs src-backup 基线

> 会话 e0245f85 · 2026-10-19 11:0x ｜ **只读比对，未修改任何源文件**
> 判据：SHA256 逐文件 + 容器逐包编译（golang:1.27-bookworm, linux/amd64）

## 结论（一句话）

**不是需要补齐，而是「双方已有且字节级完全相同」** —— ratelimit 不构成叠加污染风险；
真正的污染发生在其它 24 个交集包（89 个文件内容不一致）。

## 一、ratelimit 三源逐文件 SHA256（完整值）

| 文件 | src-backup（基线） | full-src2（供体） | full-src |
|---|---|---|---|
| limiter.go | `e485eb55...b15050` | `e485eb55...b15050` | `e485eb55...b15050` |
| limiter_test.go | `3436407f...b06951` | `3436407f...b06951` | `3436407f...b06951` |

**三源完全一致（同为 3908B / 4129B）** ⇒ 该包在事故中未受损，也不存在版本分叉。

## 二、为什么上轮说缺 ratelimit —— 时序问题

| 时刻 | 事实 |
|---|---|
| 上轮审计（约 10:52） | 基线 `internal/ratelimit` **不存在** ⇒ 判为整包缺失、需从 full-src2 补 |
| 本轮复核（11:01） | 基线 `internal/ratelimit` **已存在**（目录 mtime 10:58:52） |

即：**Lead 在我出报告后已自行补齐**，且补的就是 full-src2 那一份（哈希一致）。我的旧建议已过期，特此更正。

## 三、ratelimit 的耦合面（判定会不会污染代际的关键）

| 维度 | 实测 |
|---|---|
| 仓库内依赖 | **0 个**（只 import `context / sync / time / testing`） |
| 导出符号 | `Limiter` / `Snapshot`（类型）+ `New / Wait / TryTake / Snapshot`（函数） |
| 消费点 | `engine/engine.go`（Snapshot）、`engine/limiter_snapshot_test.go`（Limiter, New）、`engine/ratelimit_config_test.go`（New）、`executor/limiter_integration_test.go`（New） |
| 容器逐包编译 | **OK ratelimit**（基线当前快照实测） |

**判定**：该包**零仓库内依赖**，是纯标准库叶子包 ⇒ 结构上不可能引入代际耦合。
消费点全是调用方，符号齐全（无 `undefined` 报错）。**它是干净的补齐，不是污染。**

## 四、真正的污染面（供体 vs 基线的全量比对）

| 指标 | 数值 |
|---|---|
| 交集包数 | 24 |
| **内容完全一致的文件** | **65** |
| **内容不一致的文件** | **89** |
| 仅基线有（full-src2 缺） | 20 |
| 仅 full-src2 有（基线缺） | 8 |

**包级存在性差**：

- 仅基线有：`browser, retry, single, tray`（4 个平台包，供体缺）；
- 仅 full-src2 有：**无**。

**差异最大的包**（体积差越大越可能是代际错配）：

| 文件 | 基线 | full-src2 | 判读 |
|---|---|---|---|
| `engine/engine.go` | 66,110B | 39,649B | **差 26KB ⇒ 不同代**（基线更新） |
| `config/config.go` | 30,805B | 11,516B | **差 19KB ⇒ 不同代** |
| `engine/engine_autonomy.go` | 16,229B | 16,775B | 轻微差异 |
| `autonomy/*（7 个文件）` | 有 | **无** | 供体中该模块不完整 |
| `artifact/*（5 个文件）` | 全部 | 全部 | 均有差异（基线含 loose 解析） |

明细逐条见 `docs/ratelimit-diff-e0245f85.json`（字段 `fileDiff.details`，含 kind 与字节数）。

## 五、更新后的定损结论

1. **ratelimit：无需任何动作**（三源一致 + 零仓库依赖 + 编译 OK）；
2. **基线仍应是 src-backup**（191 个 .go、28 包、无 0 字节文件）；
3. **绝不可从 full-src2 批量取文件补基线** —— 89 个文件内容不一致，其中 engine/config 差 19~26KB，批量取会再造代际错配；
4. 当前基线 9 个损坏包仍待修（modelpool 5 处、verify、evaluate、gov、autonomy、config、supervisor、executor、engine）；
5. full-src2 的用处只剩作为差异对照证据保留（其独有包为 0，4 个平台包基线本就有）。

## 六、声明

本次比对**纯只读**：未写入 `workbench/`、未改基线任何文件、未碰 `.git`。
产出仅两个新文件（本报告 + JSON 清单），均落在 `src-backup/docs/`。