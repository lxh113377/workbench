# scripts/ 管线件清单（2026-10-20 轮18 立）

> 本目录历史堆积 240+ 脚本（含本会话 104 个即兴诊断件）。为免「下季 cron 或新人无法分辨哪几个是管线件」，
> 轮18 将 `_rNN*` 即兴件全部移入 `_archive/`（内容已入数据件与提交信息，无信息损失），并立本清单。
> **判定规则**：cron 指令点名、或 leg0/05/06 文档点名 = 管线件；其余为一次性工具，用后即归档。

## 一、cron 直接调用（长效机制核心）

| 脚本 | 作用 | cron 步 |
|---|---|---|
| `leg0_consistency_audit.py` | 四集对账+9 判据（RED 裁决覆盖/文档读数）| 第0步+第6步 |
| `probe_channel.py` | ungh.cc 限流探测，退出码 0/3 | 第0.5步 |
| `skill_inventory.py` | 全量盘点（目录名索引，python -X utf8）| 第2步 |
| `quarterly_skill_sweep.py` | 四腿三态扫描（委托壳/候选池门/SSL 重试）| 第3步 |
| `skill_hitrate_eval_v2.py`（在焚诀 eval）| 质量代理评测，UTF-8 冻结+验码 | 第7步 |

## 二、矩阵/台账生成链（数据派生，改数据后需重跑）

| 脚本 | 作用 |
|---|---|
| `score_matrix.py` | 五维矩阵（合并 batchA/D/E + watchlist-gate + 星值回填）|
| `make_matrix_batchD.py` / `make_matrix_batchE.py` / `make_matrix_batchA.py` | 各批次矩阵源 |
| `make_ledger08.py` | 08 全量检索台账 |
| `top1_ledger.py` | 07 累计命中台账 |

## 三、登记表维护件（结构性变更时才跑，非每季）

| 脚本 | 作用 |
|---|---|
| `build_watchlist.py` | 登记表重建（**默认 DRY-RUN**，血缘优先+聚类告警）|
| `mark_entry_kinds.py` / `reclass_r3.py` | entry_kind 三分法标记/重分类 |
| `fill_baselines.py` / `fill_baselines2.py` / `locate_paths.py` | 基线/入口件补录 |
| `append_watchlist_batchD.py` / `pin_agent_browser.py` | 候选追加/血缘钉回 |
| `refresh_matrix_activity.py` / `rescore_matrix.py` | 星值刷新/重算（已被 score_matrix 固化，保留备查）|

## 四、历史检索/验证件（一次性，产物已入库）

`batchC_search.py`、`batchD_verify.py`、`verify_candidate_paths.py`、`fetch_repo_facts.py`、`fetch_trees.py`、`fetch_pair_sizes.py`、`stamp_versions.py`、`split_absorbed.py`——轮5-12 的调研/吸收/盖章工具，产物见 `raw/` 与 `data/`；如复现对应轮次工作才需重跑。

## 五、归档区与纪律

`_archive/` = 本会话即兴诊断件 104 个；`data/_archive/` = 数据碎片 16 件。保留不删：审计痕迹+万一要复现某轮诊断。

**新纪律（轮18 立）**：新临时脚本一律以 `_` 前缀当日目录即弃，不得堆在主目录；单轮结束未入管线清单的一律进 `_archive/`。