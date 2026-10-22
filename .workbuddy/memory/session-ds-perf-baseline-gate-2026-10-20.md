# session-ds-perf-baseline-gate-2026-10-20（B8/L8：性能基线进 CI）

## 交付
- 新建 docs/benchmark/perf-baseline.json（4 个实测上界 + 3 个 null 字段 + 取证口径）
- 新建 docs/benchmark/性能基线口径.md（口径/差异记账/复算步骤/缺口）
- 新建 tests/check_perf.ps1（229 行，三态 rc=0/1/2）
- 改 .github/workflows/ci.yml：Verify artifact 体积上界改引用基线 + 追加 "Perf baseline gate" 步骤

## 实测读数（2026-10-20 19:45-20:05，本机 LIUXINGHUI）
- EXE：14,199,296 B（对标报告 §3.5 的 7,747,072 B 已 +83.3%）；SHA256 BEC437CE…F66DA1
- dist/SHA256SUMS 记的是 C81CA500…BFA1 → **清单过期**（§3.8 预警的文档一致性风险已发生）
- 首页 1363 B；--ui-probe 口径 P95 = 77.53（干扰轮）/1.20/1.18 ms，P50 ≈ 1.0ms
- 常驻 12.41 MB、句柄 139、线程 7-9（8 次×4s 零变化）；§3.5 的生产实例 28.9MB/464 本次**未复测**
- 上界：exeMaxBytes 20MiB / homeP95Ms 250 / residentMBMax 64 / handleMax 512

## 三态实测（都跑过）
- rc=0：正常产物（体积判成 1 项，6 项 SKIP 带原因）
- rc=2：路径不存在 / 缺 -Exe（故意不 Mandatory，避免交互式提示）/ 基线全 null（一项都没做成 ⇒ 不许 PASS）
- rc=1：原地把 exeMaxBytes 改 12,000,000（< 实际 14,199,296）→ 判红，已按 SHA256 逐字节还原
- -Runtime：真起探针实测 4 项全 PASS（P95 14.31ms / 12.07MB / 132 句柄）

## 踩到的两个坑（下次直接避开）
1. `Skip "x" "a" + $v + "b"` —— 命令论证裸拼会被 PowerShell 拆成多个位置参数，只传进第一段，
   输出截成 "reason=基线 exeMaxBytes 为 "。正解：整串加括号。
2. `$full -replace [char]92, "/" ` —— -replace 的模式参数是**正则**，单反斜杠非法，
   报 "The regular expression pattern \ is not valid" 且把 finally 里的清理整段跳过（留下垃圾 temp 目录）。
   正解：$full.Contains("workbench-perf-")，别转分隔符。
3. 用 tools.edit 改含中文的 .ps1 会**丢掉 BOM** → PS 5.1 下中文注释可能解析报错。改完必须补 BOM 并重跑 PARSE 检查。

## 判定口径（写进脚本注释，防后人改错）
- rc=2 在 CI 里**判红**：基线是本仓受控数据件，取不到 = 护栏静默失效（与 build.ps1 子系统校验器"不在就内联自证"同一立场）
- 运行期读数默认不在 runner 上判：本机 P95 1.2ms 拿去卡 windows-latest 必然误报，误报会把整个护栏的信任打掉

## 仍没盖住的缺口（如实）
- 没有趋势历史（只判上界，没把每次读数存成时间序列）→ 下一步：CI 里把 verdict+读数上传统 artifact
- 缺 exeMinBytes（只判上界，截断靠 Verify artifact 那条硬编码 3MB 下界，不在基线口径内）
- 生产实例内存/句柄未复测，64/512 靠 §3.5 历史读数撑
- taskDurationSecMax / readyMsMax / idleGrowthMBPerHourMax 仍是 null（凭据缺口 + 需 ≥1h 长跑）
