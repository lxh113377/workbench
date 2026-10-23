# Changelog

> 本项目所有值得一提的变更都记在这里。
> 格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
> 版本号遵循[语义化版本](https://semver.org/lang/zh-CN/)。
>
> 版本与产物对应关系以 `dist/SHA256SUMS` 与 `workbench.exe --version` 的实测输出为准。

## [Unreleased]

> 规则：未发布的变更一律记在本节；发版时把本节整体提升为新的 `## [x.y.z] - YYYY-MM-DD`，
> 并立刻补一个空的 `## [Unreleased]` 回顶（CI 的 changelog 门禁会校验这一条）。
>
> 修复说明（2026-10-22 自查）：上一轮插入条目时把上面第二行说明误吞了（锚点含它但替换块
> 未重新输出）—— 本轮恢复，并记下这条教训：以「包含后文的多行锚点」做插入时，
> 替换块必须把锚点里的**全部**内容重新输出，否则静默丢行。

### Changed
- **agent-browser 本机读取通道定位并恢复；上游 #1922 复现并提交两条评论（2026-10-23 实测）**：
  **通道根因**（此前 4 次复现全部失败、只报「环境障碉」）：本地静态服务与 agent-browser 不在同一条 pwsh
  进程树，Chrome 被本机 127.0.0.1:7897 代理拒后空转 —— 表现为 `open` 报 `ERR_CONNECTION_REFUSED`，
  或 `open` 成功但 `snapshot`/`get text`/`eval` **全空输出且 rc=0**。
  **解法**（三者缺一不可，已写进 agent-browser skill v3.2.0）：同一个 .ps1 里 `Start-Process node server.js`
  → `agent-browser --engine chrome open` → 后续命令；显式 `--engine chrome`；`Start-Sleep` 留页面脚本执行时间。
  **恢复判据**：`get url` 返回真实 URL + `eval document.querySelector('h1').textContent` 返回真实文本。
  **通道恢复后的复现结果（0.39.1 / Windows / 显式 chrome 引擎）**：`click @e13` 报 `✓ Done` exit=0，
  但 URL 与 DOM **都没变**（连导航都没发生）；同一元素用 CSS `click #go` 却正常导航到 v2。
  这比 #1922 原报的「跳转后旧 ref 静默 no-op」更宽 —— **没有 `frameNavigated` 事件可挂**，
  现居的修复方向（PR #1923 清 `ref_map`）覆盖不到这一形态。
  **已提交上游**（均经 `gh api` 复核存在）：
  - issue #1922 评论 `6355789775`：Windows + 0.39.1 独立复现 + 同文档 no-op 变体 + 对照组；
  - PR #1923 评论 `6355811044`：指出该 PR 覆盖面缺口，并建议判据断言「点击后可观测状态」而非退出码。
  **顺带证伪一条旧口径**：3.1.0 对 `network unroute` 的 `--abort` 桩记「未见解除、不下结论」——
  本轮带同会话干净对照实测，pattern 与无参 unroute **均能解除**（`FETCH_FAILED` → 恢复 `OK {...}`）；
  `--body` 桩同样两侧生效。统一处置不变：清桩后仍须真发请求复验，不拿命令的 ✓ 当事实。
### Fixed
- **修正 `check_resident_longrun.ps1` 的结构性 bug —— phase=running 拦截让内存门禁自 round 41 装上起在自动路径上永不落盘（2026-10-24 round 47 实测坐实）**：
  现象：round 41 给长驻内存建了「落盘 + 趋势消费」机制，6 小时后 resident_longrun 序列恒 0 条 ——
  门禁挂在 check_all、活实例在跑，却一条都没落。查根因不是「没触发」，是判据本身错了：
  `if ($phase -eq running) { Unverified }` 把 state.json 的 phase 当成了「任务活跃度」，
  但 phase 反映的是引擎循环 on/off（Start->PhaseRunning / Stop->PhaseStopped），不是有没有任务。
  对 7x24 常驻服务，引擎壳只要活着就恒 running（实测该字段还陈旧：一度 100+ 分钟不更新），
  于是门禁把最该测的「服务空闲稳态」当成「执行期」永久跳过 —— 与它自己 round 41 的初衷正好相反。
  修复（判据换成引擎侧唯一权威的空闲信号，纯读只读文件、不碰并行会话在途的 engine Go）：
  (1) 空闲 = queue.json 里进行态任务数==0，取 state in {running, evaluating}，
      对齐引擎 model.IsActive()（model/types.go:43）；有进行态 => UNVERIFIED（执行期，内存随并发变化）。
  (2) 稳定判据重写：原「连续 5 次差 <=1MB」是为 stopped 后回落场景设计的，实测服务运行中的
      空闲态 OS RSS 也在 GC 锯齿里波动（15 次采样极差约 6MB），原判据在此永不满足、退化成单点又
      违背 round 41「单次读数没有参照物」的纪律；改为采满 15 点/30s 取中位、用极差 <= 8MB
      判稳（极差过大=执行期余震，读数属瞬态 => UNVERIFIED，不硬取虚高值 —— 实测刚跑完任务的实例落在 85~107MB）。
  三层实测验证：真实活实例产出第一条真实读数（median 66.1MB、极差 1.2MB、活跃任务=0、PASS）；
  变异 -StableBandMB 0 => 极差超带宽 UNVERIFIED 且不落盘（前后条数相等，守住 round 41 负向纪律）；
  变异 -BoundMB 40 -NoRecord => FAIL 正确判红；合成 queue（running+evaluating+rejected+queued）
  => 进行态计数精确为 2（queued/rejected 不误计入）。趋势器同步显示新字段 activeTasks，
  读到首条：余量 61.9MB>40 不触发提示（正确）。
  教训：判据要落到「真实语义」而不是「名字像」—— phase=running 字面最像「在跑任务」，
  实际是引擎开关；我 round 41 建机制时按名字取了它，机制就静默失效 6 小时且看起来一切正常
  （门禁没报错、check_all 照跑）—— 又一次「装了就以为在工作」，正是不消费序列就不会发现的。
- **CHANGELOG.md 累积了 5 个前导 BOM（289 个 .md 里唯一一个），round 44 查明并修复**：
  为新增门禁补 CHANGELOG 条目时，我用「读出来 -> replace(/^\uFEFF/, '') 去 BOM -> 再前置一个 BOM」
  的写法处理文件；但 **JS 的 `replace(/^\uFEFF/, "")` 只去掉一个**，而文件当时已有多个，
  于是每改一次就多攒下一个。跨多轮累积到 **5 个前导 BOM**（HEAD 里就是 5 个，不是我本轮造的）。
  全仓 289 个 .md 扫描：无 BOM 281、单 BOM 7、**多 BOM 只有 CHANGELOG.md 这一个**。
  修法：把开头**连续的** BOM 全剥掉，再前置恰好一个（实测修好后前导 BOM=1、正文 `# Changelog` 完好，
  `check_changelog` 仍 PASS）。
  教训（与双 BOM 第 2/3/4 次同源，那是 .ps1 上的同类问题）：**补 BOM 前必须先数清楚已有几个**，
  不能用「去掉一个再加一个」的假设 —— 剥 BOM 要按**连续前导**循环剥，不能只剥一次。
  顺带记录一个真实判据缺口：`check_ps1_encoding` 只管 .ps1/.psm1 的 BOM，
  **.md 的多 BOM 无人守**（本仓 .md 基本不带 BOM，7 个带 BOM 者里混进多 BOM 不会被发现）。
  本轮不扩判据（避免给 CI 添噪声），记在此作为后续可做的窄面。
- **A7 泄漏判据修复：趋势必须**在同一进程内**算 —— 跨重启的 metrics.csv 曾把 3 天误报成 heap +174% leak（2026-10-24 round 39 实测）**：
  `--soak` 对着**真实的 96 小时 metrics.csv**（9,570 样本）报 `verdict=leak`
  （heap 头 13.4→尾 36.8MB +174%，handles 386→534 +38%）—— 我按这条在册待办去查根因，
  发现它是**误报**：96h 数据里有 **52 次进程重启**（用 num_gc 回退检测，重启后 GC 计数
  从新进程重新起步）。`AnalyzeTrend` 跨段排序后，头 1/3 落在轻载期、尾 1/3 落在重载期，
  得出的「上升趋势」其实是负载形态差 + 重启不连续。**当前进程段内 heap 实为 -2.5%，无泄漏**
  （GC 计数持续增长，堆是锯齿回落的）。
  修复：`TrendPoint` 增加 `NumGC` 字段（metrics.csv 第 6 列，老格式缺省=0），
  `AnalyzeTrend` 判定前先用 `trimToLastProcess` 按 num_gc 显著回退点（>20 次，远超抖动）
  切段，**只对最后一个进程段**算趋势，并在报告里如实带 `processSegments` 字段。
  三个保守设计：① NumGC 全 0（老格式/合成夹具）= 不可判 => **不裁剪**，维持旧行为，
  绝不因「看不清」就悄悄丢数据；② 只裁到最后一段，不丢中间段的历史（判定只用尾段）；
  ③ 段内真泄漏照判 leak（反向判据测试钉死，裁剪不得制造假 ok）。
  判据测试 3 个（restart 台阶不判 leak / 段内真泄漏照判 / 无 NumGC 不裁），
  加原有 5 个全 PASS；**真实数据双向验证**：
  - 修复后：`heap=ok handles=ok`，spanHours 96.14→14.58h（正确裁到最后进程段），
    整体 unknown（14.58h < 24h 门槛）—— **诚实的 unknown 优于虚报的结论**；
  - 变异（禁用裁剪）：立刻回到 `heap=leak handles=leak` 误报 —— 修复确实在起作用。
  顺带：A7 验收（acceptance.go）走同一对调用，**同一个误报源头一并修掉**；
  `e2e_soak.ps1` 的 18 条 fixture 判据全绿（向后兼容无回归）。
  本轮同时验证了 round 37/38 装的读数机制：健康度 WARN（badPkg=3）持续存在，
  其中 cmd/workbench 的红正是并行会话引用尚未实现的 `soakConclusion` 等（TDD 红态），
  我只动 supervisor，未碰他们的所有权。
- **补上 Windows 平台上「可写探针」的测试缺口（round 38，唯一 SKIP 用例审计引出）**：
  全仓 `-count=1 -v` 跑下来只有 1 个 SKIP：`workspace_test.go` 的
  `TestProbeWriteRejectsReadOnlyDir` —— 它在 Windows 上直接 `t.Skip`
  （Unix 用 chmod 0o500 构造只读目录，Windows 权限模型不同）。
  而 `probeWrite` 正是 `workspace.go:335` 判定「目录是否可写」的核心探针
  （external 模式选目录的闸门，不可写就报「目录不可写」+ 建议）。
  **荒谬之处**：本项目是 Windows 单 EXE —— 恰恰在唯一会发布它的平台上，
  「探针能否拒绝只读目录」从未被执行过一次。
  补法：新增 `probe_windows_test.go`（`//go:build windows`），用 `icacls /deny W`
  构造只读目录（本机实测：施加后直写 Access denied、移除后恢复可写，两步均如预期）。
  两处防假绿的设计：
  ① **前置自检** —— 真正调 probeWrite 前先直连 `os.Create` 确认 deny 生效；
     若直连仍写得进去（进程带绕过 ACL 的特权），那是前置条件不成立，显式 SKIP 并说明，
     与被测逻辑失败区分开（禁把「没验成」当「验过了」）；
  ② **cleanup 顺序** —— deny 会挡住 `t.TempDir()` 的删除，故用 `t.Cleanup` 先恢复 ACL
     （Go 的 cleanup 是 LIFO，本注册晚于 TempDir 的清理注册，故先执行）。
  变异验证（两轮才做对，值得记）：
  - 首轮变异「probeWrite 吞掉 OpenFile 的 err」**没有判红** —— 因为 f 为 nil 时
    `f.WriteString` 仍返回 ErrInvalid，探针照样报错，**这不是有效变异**；
  - 换成「直接 return 假装写成功」（模拟探针没真写、永远说可写的真实缺陷形态）
    ⇒ 判红且消息精准：「只读目录应写入失败，但 probeWrite 返回成功 —— 可写探针在本平台形同虚设」。
  教训：**变异要针对「缺陷的真实形态」设计，不是随便删一行** —— 删错地方会得到
  「测试仍 PASS」的假安全感，比不做变异更糟。
  验证：新测试 PASS；workspace 包 -race 全绿；gofmt 门禁 PASS（333 文件，26 豁免）；
  提交前 staged gofmt 闸通过（round 32 装的机制，本轮第一次帮我避免了 CI 判红）。
- **未跟踪测试的健康度读数序列化（round 20 设计补完）+ 双 BOM 犯第四次后的根治（2026-10-24）**：
  **① 健康度序列化**：round 36 给 `check_untracked_go` 加了健康度检测（WARN 哪些包因未跟踪件失败），
  但只是**单次快照** —— 看不出「在途件红了多久」。按 round 20 立下的理由（单次读数不够），
  把健康度也落盘到 `gate-readings.jsonl`（`gate=check_untracked_go_health`），字段含
  `badPkgCount` / `badPkgs`（包名列表）/ `untrackedTests` / `head`。
  首版试过「把原落盘段整体搬到健康度检测之后」，结果**搬坏了括号**
  （parse 报 `L174 Unexpected token '}'`）—— 从 HEAD 恢复后改为**就地追加一小段**，
  不动原有结构。教训：**搬整个块的风险大于收益，能追加就不要搬**。
  首条读数已落盘：`badPkgCount=3`（cmd/workbench / autonomy / shortcut）、`untrackedTests=31`。
  这 3 个包的失败已逐个查明性质：**并行会话未完成的 TDD 红态** ——
  autonomy 的 4 个失败全是风险配置开关未实现（`EscalateMinQuality`/`BlockOnRepeat`/
  `MaxDraftsPerRound`/`OnCostOverrun`），shortcut 是 `--silent` 参数残留未清。
  **测试已写、行为没做**，所有权在并行会话，不代改（已在门禁 WARN 里说明形态）。

  **② 双 BOM 第四次的根治**：本轮又从 HEAD 取字节写回 `.ps1`，造出双 BOM（第四次，前三次在 round 24/26/32）。
  `check_ps1_encoding` 每次都当场抓到 —— 但**修四次说明靠门禁兜底不够**。
  根因：`git cat-file blob HEAD:<path>` 取到的字节已含 BOM，而我写回时又前置了一个。
  根治：`runtime/tmp/restore-from-head.ps1` —— **用 `git checkout HEAD -- <path>` 代替手工字节读写**，
  它天然保持对象里的 BOM/换行状态，并在写完自检 BOM 数。
  已把「能追加就不要搬、能 checkout 就不要手工读写字节」记进 CHANGELOG。
- **`check_untracked_go` 补上「健康度」这一半：未跟踪测试不仅报数量，还报**是否正在失败**（2026-10-24）**：
  round 36 手动跑 `go test` 时发现工作区有 **6 个测试失败**（autonomy 4 + shortcut 1 +
  cmd/workbench build failed），而 `check_untracked_go` 的读数完全看不出这件事 ——
  它只报「31 个未跟踪 / gap=77」，数量连续 5 次稳定，**像是一切正常**。
  但实际形态是「**工作区红 / 干净 HEAD 绿**」（已在干净 worktree 实测 HEAD 三包全绿 rc=0）。
  危害：并行会话的在途件长期停在红色状态，该会话自己可能没意识到；
  别人看到 gap=77 也只以为「只是没提交」，不知道其实是「没提交**且是红的**」。
  改动：门禁在原有数量读数之外，把未跟踪测试文件按包分组、对涉及包跑 `go test`，
  报告哪些包因未跟踪件而失败。**刻意不判红** —— 未跟踪件所有权不在本门禁
  （提交决策由人按 ownership 判），判红会让它从「可见性工具」变成「拦截器」，违背定位。
  **实现踩到一个会产生 7 个误报的坑（已修并留注）**：未跟踪文件路径是
  `workbench/internal/engine`（**已含** workbench 前缀），而 `go test` 的 WorkingDirectory
  已设为 `<root>/workbench` —— 直接拼会变成 `workbench/workbench/internal/engine`，
  `directory not found`、ExitCode=1 被当成 FAIL，**10 个包里误报 7 个**。
  修法：剥掉 `workbench/` 前缀后再拼，且只对 workbench 下的包检测（tests/ 等非模块目录跳过）。
  修正后与手动 `go test` 结果**完全一致**（3 个：cmd/workbench / autonomy / shortcut）。
  另记一次本人在同类的第三次失误：从 HEAD 取字节写回时又造出双 BOM
  （`check_ps1_encoding` 当场抓到）—— 根因是 HEAD 版本已带 BOM，而我又前置了一个。
- **删掉 3 个零断言「假绿测试」，并把这类缺陷上闸（CI 门禁数 14→15）**：
  round 34 审计断言密度时发现 3 个 `1T/0A` 文件（1 个 Test、0 条断言），round 35 处置。
  三个都是 `a5cf134`「仓库损毁后全量恢复」时进来的**调试残留**：
  只有 `fmt.Println` 打印中间状态，没有任何 `t.Fatal/t.Error` —— **永远 PASS，但什么都不保证**。
  逐个核实后确认**全部冗余**（已有等价的真断言覆盖）：
  | 调试测试 | 已有等价覆盖 |
  |---|---|
  | `artifact/debug_loose_test.go` | `loose_real_test.go` 的 `TestParseRealModelOutput`（同 testdata、同调用链、有断言） |
  | `artifact/debug_strict_test.go` | `artifact_test.go` 的 `TestParseStrict` 等 12 个 |
  | `config/zz_debug_test.go` | `config_test.go` 的 `TestNormalizeClamps` 等 11 个 |
  **删除后覆盖率不变**（artifact 84.4%→84.4%、config 70.3%→70.3%）—— 零贡献的实证。
  危害不是风格问题，是实质性的：① 虚增 PASS 数（740 个 Test 里有 3 个是假的）；
  ② 让人以为「这里有测试」而跳过补测试，形成真实覆盖漏洞。
  新增 `tests/check_test_assertions.ps1`，接入 repo-gates（第 15 个）与 `check_all`。
  **判据经过两轮精细化才收敛**（首轮一刀切报了 4 个、其中 3 个合理）：
  - 豁免① **编译期契约断言**：`var _ SomeInterface = ...` 在编译期就完成了断言，
    运行期无需再断言（实例：`TestRunnerContractSatisfied`）；
  - 豁免② **显式声明的不 panic 契约**：doc comment 写明「不得 panic」且函数体无 `fmt.Print*`
    （实例：`TestFinishCancelledOnUnknownTask`、`TestHooksAttachSurvivesNilChain`）。
  - 头注保留这份分类的理由：**不精细化，门禁第一次运行就会产生 3 个假红，离「被删」就不远了**。
  实现上踩到两个坑（都已修并留注）：① 豁免窗口漏了 **func 上方的 doc comment**
  （Go 的文档注释在那里，不在函数体内）；② pattern 写成 `不\s*panic` 匹配不到「**不得** panic」
  （中间隔着「得」字）—— 改为 `不[得应会]?\s*panic`。
  验证：删除后覆盖率不变；三个变异全捕获（无断言判红并点名 `:5` / 有断言 PASS / 编译期契约 PASS）；
  当前基线 **737 个 Test 全部有断言**。
- **把「注释里的数字会过期」这门类做成机器判据（round 31/32 两次同型教训的产物，CI 门禁数 13→14）**：
  round 31 更正了「CRLF 会进索引」的错误机理，round 32 又发现 `check_gofmt.ps1` 头注写
  「35 条债务里 33 条已失效，只剩 2 条 live」而**实际 26 条 live**。
  两次同型：**把一个会随代码演进的数字/结论，写成不带日期的现状断言** ——
  读注释的人会被误导（「以为债务快清完了」），而且无声无息。
  处置分两步：
  ① **修掉已发现的**：`check_gofmt.ps1` 那段改为「历史记录（2026-10-22 首版 35/33/2；
  round 32 复核 26 条）+ 明确指向门禁自身输出」（PASS 行本来就报「N 个登记豁免未清」）。
  ② **做成判据**：新增 `tests/check_comment_staleness.ps1`，接入 repo-gates（第 14 个）与 `check_all`。
  判据刻意收窄：只抓 `只剩/仅有/共 N 条|个|处` 这一种**高方差、高误导价值**的 pattern，
  且同一行或相邻 2 行内有日期 / 「历史记录」/ 「以门禁输出为准」即豁免。
  **不做得更广是刻意的**：本仓教训是「模糊语义检查制造噪声 ⇒ 门禁被删」，
  所以宁肯只抓一类真问题，也不要一个会被关掉的宽泛检查。
  覆盖「受跟踪 + 已暂存」两类文件 —— 首版只查 `ls-files`，变异时用新文件 MISSED，
  而**新文件正是最可能带坏注释的**（与 `check_ps1_eol` 首轮变异 MISSED 同源：
  只覆盖存量、不覆盖增量，等于放过最可能出问题的那批）。
  验证（三个变异全捕获）：
  - MUT-A：无日期的「只剩 3 条 live」⇒ 判红并精确点名 `:2`；
  - MUT-B：补「（2026-10-24 实测）」⇒ PASS；
  - MUT-C：改成「首版时……」的历史记录形式 ⇒ PASS。
- **自我更正：round 26 的「CRLF 会进索引」机理结论是错的（round 31 复刻实验推翻）**：
  round 25 观察到 `check_all.ps1` 以 CRLF=173 进 HEAD、diff 放大成 177 增/169 删；
  round 26 我据此下了结论：「`.gitattributes` 的 clean 过滤器对直接覆写不生效，CRLF 会进索引」，
  并据此建了 `check_ps1_eol.ps1`。
  **round 31 推翻了这个机理**：精确复刻 round 25 的**完整操作序列**
  （读字符串 → 改 → 写回 → `git add` → `git commit`），索引与 HEAD **均为 LF**（CRLF=0）。
  即当前配置（`*.ps1 text eol=crlf` + `core.autocrlf=true`）下，CRLF **根本进不了 HEAD**。
  round 25 那 173 个 CRLF 的真实成因：**`.gitattributes` 规则切换的过渡期** ——
  首版是 `*.ps1 text=auto`（不强制 eol，`git log --diff-filter=A` 实测），round 22 才改成 `eol=crlf`。
  **影响与处置**：
  ① `check_ps1_eol.ps1` 保留，但头注已更正为「**预防性护栏**（防配置回归），非活跃检测」——
  当前配置下它恒 PASS（实测：造 CRLF 文件 add 后索引即 LF）。保留理由是配置回归无声无息、
  而代价（169 行假 diff）极高，且删掉只要一行、重新发现要对全仓做字节审计；
  ② 本轮曾据此写一个 CI 版（`check_ps1_eol_head.ps1`，查 HEAD 入库内容）——
  验证发现它同样恒 PASS（HEAD 里不可能有 CRLF），**已删除**，没有把一个无价值判据挂进 CI；
  ③ 教训记进两处（门禁头注 + 本条）：**「实测过一次」不等于「机理正确」**，
  机制会随配置变化而过期；且**复现实验必须连操作序列一起复刻** ——
  round 26 的变异之所以 MISSED（用新文件测，新文件会被归一），正是因为只复刻了
  「写了 CRLF」这一个特征，没复刻「改的是已跟踪文件 + 当时的 attributes 状态」。
- **第 30 轮盘点发现覆盖缺口：没有任何门禁检查 `.ps1` 语法可解析性（已补，CI 门禁数 12→13）**：
  盘全仓 23 个门禁的职责面时发现：BOM 有门禁、换行有门禁、引用有门禁，
  但**「能否被 PowerShell 解析」这一层是空的**。
  **实测确认缺口（不是推理）**：造一个「字符串未闭合」的 `.ps1` 并暂存，
  现有 3 个最相关的门禁**全部 rc=0 放过** ——
  `check_ps1_encoding`（管 BOM）rc=0、`check_ps1_eol`（管换行）rc=0、
  `check_gate_links`（管引用）rc=0；而 PowerShell 解析器明确报
  `The string is missing the terminator: '`。
  后果：**语法错误的脚本能顺利进库，直到有人运行才炸**。
  本仓已有同族事故多次（BOM 缺失 / 双重 BOM / 未闭合字符串，均靠跑起来才暴露）——
  都属于「本可静态发现、却要等运行时」。
  新增 `tests/check_ps1_syntax.ps1`，接入 **repo-gates（第 13 个）** 与 `check_all`。
  用 `[System.Management.Automation.Language.Parser]::ParseFile` —— **只解析不执行**
  （纯静态、无副作用、不需要权限、不会真跑脚本）。
  至此 `.ps1` 的防护形成**三层**，各自管一个面：
  | 层 | 门禁 | 判什么 |
  |---|---|---|
  | 字节层 | `check_ps1_encoding` | BOM 有无/重复 |
  | 字节层 | `check_ps1_eol` | CRLF 混入 |
  | **语法层** | **`check_ps1_syntax`** | **能否被解析** |
  验证（三个变异全捕获，错误信息带 `file:line` + 具体原因）：
  - MUT-A：字符串未闭合 ⇒ 判红（`:2 The string is missing the terminator`）；
  - MUT-B：合法语法 ⇒ PASS；
  - MUT-C：`if` 缺闭合括号 ⇒ 判红（`:2 Missing closing '}'`）。
  基线：53 个受跟踪脚本全部可解析。
- **`check_noninteractive` 从 22.0s 优化到 0.4s（快 55 倍），判据能力零退化（2026-10-24）**：
  承接 round 28 的同型优化（那次改了 `check_ps1_encoding`，并记下「还有 4 处同型问题」）。
  **动手前先复测，推翻了部分印象**：那 4 处里 3 处其实不慢 ——
  `check_doc_refs`/`check_report_claims` 只遍历 `docs/` 子目录（0.3s）、
  `check_spawn_silent` 有 `-Filter` 提前过滤（0.6s）。**真正慢的只有 `check_noninteractive`（22.0s）**。
  避免了「凭印象优化 3 个不是瓶颈的门禁」—— 无瓶颈就不动，改动面越小越安全。
  根因与 round 28 同型：`Get-ChildItem -Recurse` 遍历整个仓库（275,006 个文件 / 约 20s），
  而门禁真正要扫的只有 57 个脚本。改法相同：`git ls-files '*.ps1' '*.psm1' '*.cmd' '*.bat'`。
  **语义变化已量化（非隐含）**：覆盖从 57 ⇒ 53 个受跟踪脚本。少的 3 个是未跟踪在途件
  （`tests/safe_merge.ps1`、`tests/watch_soak.ps1`、`前端自检.cmd`），
  **实测均无交互模式命中**且属并行会话 ownership —— 不构成漏判。
  该缩小与 `check_gofmt`/`check_ps1_eol` 的「只判将进版本库的内容」口径一致。
  原有的目录排除列表（.git/node_modules/xtmp/...）与 `-Include` 不生效的规避注释
  已转为历史注记保留 —— **为什么不那么写**和**为什么这么写**同样值得留。
  **能力零退化验证（三个变异全捕获）**：
  - MUT-A：含 `Read-Host` 的受跟踪脚本 ⇒ 判红并精确点名 `含交互构造 Read-Host`；
  - MUT-B：非交互脚本 ⇒ PASS；
  - MUT-C：含 `Get-Credential` ⇒ 判红（另一种交互构造，证明不是只认一个模式）。
- **`check_ps1_encoding` 从 20.3s 优化到 0.3s（快 50 倍+），且判据能力零退化（2026-10-24）**：
  起因是给所有门禁做了一次耗时普查，发现它异常慢——而它只是读文件头几个字节查 BOM。
  **根因（量化定位，不是猜的）**：`Get-ChildItem -LiteralPath $Root -Recurse -File`
  遍历**整个仓库** —— 实测 **275,006 个文件 / 19.8 秒**，其中绝大部分是
  `.git/`、`src-backup/`、`_recovery/`、`dist/` 这些巨型目录；
  而门禁真正要检查的只有 **52 个受跟踪脚本**。原有的目录排除列表在递归**之后**才生效，
  所以遍历成本一分没省。
  修法：改用 `git ls-files '*.ps1' '*.psm1'` 取受跟踪列表。三个好处：
  ① 快三个数量级；② **语义更对** —— 门禁判的是「将进版本库的内容」，
  未跟踪的在途件不该判（与 check_gofmt / check_ps1_eol 的 HEAD/暂存区口径一致）；
  ③ 不再需要那一串目录排除列表。
  **能力零退化验证（三个变异全捕获）**：
  - MUT-A：缺 BOM 的受跟踪 `.ps1` ⇒ 判红；
  - MUT-B：补 BOM ⇒ PASS；
  - MUT-C：**双重 BOM** ⇒ 判红（这条是本仓 2026-10-22 自己踩过两次的坑）。
  顺带修正一个 Coverage 盲区：原实现因目录排除漏掉了 1 个脚本（检查数 52→53），
  现在受跟踪的脚本一个不漏。
  **同型问题还有 4 处**（都用了全仓 `Get-ChildItem -Recurse`，只是暂未成为瓶颈）：
  `check_noninteractive`(25.4s)、`check_spawn_silent`、`check_doc_refs`、`check_report_claims`。
  本轮只改最痛的一个；其余记在此，按需再治（改法相同：换 `git ls-files`）。
- **覆盖率读数序列化到 gate-readings.jsonl（2026-10-24）**：
  动机来自 round 22 的实测发现：14 个阈值里 9 个已低于实测 15pp 以上（护栏失效），
  而**没人发现**——因为覆盖率从来只有单次读数，没有序列、没有趋势。
  改动：`setup/build.ps1` 的覆盖率门禁通过后，把当次读数追加到
  `.workbuddy\memory\gate-readings.jsonl`（与 `check_untracked_go` 同一文件、同 schema 风格，
  用 `gate` 字段区分来源）。字段：`env`(ci/local) / `pkgCount` / `avgPct` / `minPkg` / `minPct` / `head`。
  两个设计点：① **带 `env`** —— round 22 实测本机与 CI 覆盖率系统性差 10~20pp
  （且 tray 本机反而更低），不带环境标记的序列会把两种口径混成一锅粥；
  ② **带 `minPkg`/`minPct`** —— 最低包比均值更早暴露退化（均值会被高覆盖包稀释）。
  **验证（两级都做实）**：
  - **单元级**：隔离输入验证通过（解析 3 包 ⇒ avg 68.57%、正确识别最低包 tray=26.3%、
    env/head 均正确带上）；用完即删掉那条假读数（`pkgCount:3` 是模拟特征，不留脏数据）；
  - **端到端**：干净 worktree（HEAD=`7d17756`，`go vet` rc=0 证明 vet 失败只是并行会话
    未跟踪的在途文件）里跑真实 `go test ./... -cover`，29 个包被正确解析并落盘：
    `{"gate":"coverage","env":"local","pkgCount":29,"avgPct":70.66,
     "minPkg":"workbench/cmd/workbench","minPct":5.6,"head":"7d17756"}`；
  - 首轮端到端曾受阻：完整构建在 `go vet` 段失败（并行会话在途文件
    `headless_testcmd_windows_test.go` 的 `hiddenTestCmd redeclared`，未跟踪、非我改动）。
    用 worktree 隔离后拿到证据 —— **受阻时要换隔离环境取证，而不是把「未验证」当结论**。
  顺带确认 round 22 的另一条待办**已闭环**：长驻内存判据的「多轮取中位」在
  `check_resident_longrun.ps1` 里已实现（等内存稳定后取稳定段中位，最多等 40s）；
  「记录数上限」由 `check_queue_size.ps1` 守 —— 双口径都到位。
  另确认 round 19 记的 `lanes.go` 三个零引用导出符号**已自然消解**：
  并行会话补了 `SetPoolEnabled` 使用 `PoolToggle`，`PurgeResult` 由 `PurgeGatewayLanes` 使用，
  `LaneGroupLabel` 已不在文件中（重构掉）—— 死代码门禁 PASS。
- **`.ps1` 的 CRLF 混入索引把 8 行改动放大成 177 增 / 169 删（round 25 真实事故，已上门禁）**：
  round 25 我改 `tests/check_all.ps1` 后，diff 显示 **177 insertions / 169 deletions** ——
  一个 8 行的新增被放大成「几乎整个文件重写」，评审者根本看不出真实改动。
  根因（当时记为 `git cat-file blob` 实测）：**用 CRLF 覆写已跟踪文件后 `git add`，
  CRLF 会原样进索引**（实测 CRLF=71 / LF=0；原样 add 是 CRLF=0 / LF=71）。
  即 `.gitattributes` 的 `*.ps1 text eol=crlf` 的 clean 过滤器对「直接覆写」场景**不生效**。
  附带发现：**新文件反而会被归一**（探针实测：写 CRLF ⇒ 入库 LF）——
  所以这个坑**只咬「改已有文件」**，而那正是最需要防的场景。
  
  > **⚠️ 2026-10-24 round 31 更正（上述机理结论是错的）**：
  > round 31 精确复刻了 round 25 的操作序列（读字符串→改→写回→add→commit），
  > 索引与 HEAD **均为 LF**（CRLF=0）。当前配置（`*.ps1 text eol=crlf` + `autocrlf=true`）下
  > CRLF **进不了 HEAD**。round 25 那 173 个 CRLF 的真实成因是 **`.gitattributes` 规则切换的
  > 过渡期**：首版是 `*.ps1 text=auto`（不强制 eol），round 22 才改成 `eol=crlf`。
  > 故 `check_ps1_eol.ps1` 当前是**预防性护栏**（防配置回归），不是活跃检测。
  > 教训：**「实测过一次」不等于「机理正确」—— 机制会随配置变化而过期，
  > 而复现实验必须连操作序列一起复刻，只复刻「写了 CRLF」这一个特征会漏**。
  新增 `tests/check_ps1_eol.ps1`：查**已暂存 .ps1 的入库内容**（`git cat-file blob :<path>`）是否含 CRLF。
  与 `check_ps1_encoding.ps1` 分工：后者管 BOM，本件管换行。
  验证（用 round 25 的同款场景，**必须用已跟踪文件**才复现）：
  - MUT-A：CRLF 覆写已跟踪 `.ps1` ⇒ 判红并精确点名「含 71 个 CRLF」；
  - MUT-B：LF 覆写 ⇒ PASS。
  （首轮变异曾 MISSED —— 因为用了**新文件**，会被 git 归一，门禁查不到 CRLF。
  这个 MISS 本身证明了一件事：**变异验证必须复现真实场景的每一个特征**，
  只形似会漏。已把该特征写进门禁头注。）
- **两个「从未被执行」的回归测试被解救，并修掉它们对应的两个真实缺陷（2026-10-23）**：
  审计未跟踪文件时发现 `tests/regression/` 下有两个 `.go` 测试文件，声明 `package verify`、
  调用 `splitCommand`，但**物理上不在任何模块内**（`tests/` 无 go.mod）——
  即它们**从未被 `go test` 执行过**，此前任何「测试通过」的结论都不覆盖它们。
  移到正确位置后**当场各暴露一个缺陷**（这正是它们该在的位置）：
  ① **`splitCommand` 不识别反斜杠转义**（`docs/缺陷-验收命令反斜杠污染-2026-10-19.md`）：
  模型 manifest cmd 过度转义（`\\"`）后，`\` 残留进 argv，pwsh 收到 `\if ...` 报「术语不识别」——
  该任务真实验收**永远不可能通过**，反复返工到 repairRounds 耗尽，并把「模型转义问题」
  误报成「产物不可运行」。已按文档方案补转义处理（反斜杠转义下一字符，单引号内不转义，与 POSIX 一致）。
  ② **协议示例命令仍被中文方括号包裹**（`docs/质量铁证-协议示例中文方括号-2026-10-19.md`）：
  `protocol.go` 给模型的示例是 `pwsh ... 【if ... 】`，模型照抄后命令被空格切碎（`【if` 成一个 token），
  验收必败。已改为不带【】、明示引号必须保留，并在硬性要求加一条「禁止给 cmd 加【】等装饰字符」。
  **两个测试的期望值也都修正过**（原期望本身是错的，留记录防重犯）：
  - 反斜杠用例：原期望 argv[3] 完全不含 `\`，但 `\\"` 的真实语义是「第一个 `\` 转义第二个 `\`
    => 字面 `\`」，随后 `"` 才开启引号。要防的缺陷是「引号状态机错乱把脚本切碎」，不是「反斜杠必须消失」；
  - 中文方括号用例：原期望 splitCommand 把【】当引号处理（**错误假设** —— 中文方括号不是引号，
    没有理由不被空格分割）。根因在协议层，故该测试改为守**真正的根因**：
    协议文本里的命令行不得再出现装饰性方括号、且必须带引号。
  验证：`go test ./... -count=1` 全绿（TEST=0）；gofmt PASS（334 文件）；死代码门禁 PASS（323 文件）；
  变异验证：把转义条件改成 `false` ⇒ 两个新测试**双双判红**，恢复后全绿。
- **4 个生产代码文件从未入库 → 「本地绿 ≠ 提交绿」的活实例清零（2026-10-23 实测）**：
  门禁 `tests/check_untracked_go.ps1` 首轮运行即判红，点名 4 个**已被跟踪代码引用、但从未 git add** 的生产文件
  （同一形态本仓已记过一次「依赖文件从未入库 → 干净 clone undefined」，这是它的**当前进行时**）：
  ① `internal/modelpool/lanes.go` —— `PurgeGatewayLanes` 被 `service.go:209`（每轮同步的存量清理）、
     `cmd/workbench/pool_cli.go:158`（CLI 子命令）、`pool_test.go`（4 处判据）调用；
  ②③ `internal/web/hiddenproc_{windows,other}.go` —— `hiddenProcAttr` 被 `autostart_windows.go` 4 处调用；
  ④ `cmd/schedprobe/main.go` —— 独立 CLI（无引用）。
  **风险窗口**：HEAD 侧 `service.go` 没有该调用，故干净 clone 编译 rc=0（不断裂）；
  但只要引用方**先提交**而 `lanes.go` 没跟上，`SyncNow` 的网关车道清理逻辑会**静默消失**。
  处置：按依赖组成组提交（30 文件：4 个新生产件 + 全部调用方 + 全部判据），
  组装校验方式 = **HEAD + 仅本批文件**的 worktree 里跑 `go build` / `go vet` / 三包 `go test`，
  通过后才 `git add` 这批文件（不整仓提交，避免误归属并行会话在途件）。
  顺带按死代码门禁口径删掉 `lanes.go` 的 `LaneGroupLabel`（全工作区零引用）。
  **提交后读数**：门禁从 `FAIL: 4 个生产代码文件未入库` 变为 `PASS: 未跟踪件均为测试文件`；
  干净 clone（`git worktree add --detach HEAD`）`go build` rc=0、`go vet` rc=0、`go test ./...` 29 包全 ok。

### Changed
- **补上 hook 覆盖缺口：「hook 在库里」不等于「hook 在生效」（2026-10-24）**：
  上一轮加了提交前 gofmt 闸，但漏了一个**已被 git 设计注定**的覆盖问题：
  `core.hooksPath` 写在 `.git/config`（仓库本地配置），**不随 clone 传播**（本轮实测确认）。
  于是：CI、并行会话的机器、任何新 clone 默认都没启用 hook —— 「以为有保护，其实没有」。
  三条补全：
  ① `setup/install-hooks.ps1`：一键启用/卸载（幂等，带自检），把「启用」从手工记忆变成一条命令；
  ② `tests/check_hooks_enabled.ps1`：把「闸门实际是否装着」变成**可判事实**（三态：
     PASS=已启用 / FAIL=配置缺失或指向错 / UNVERIFIED=不在 git 仓库，后者按铁律不当 PASS 也不判红）；
  ③ `docs/handoff/提交前闸启用说明.md`：启用命令、它做什么、如何验证、如何卸载、紧急跳过。
  判据与 `check_all` 接线；`check_doc_refs` PASS（61 份活文档、82 处引用）。
  变异验证：取消 `core.hooksPath` ⇒ 判红；指向错误目录 ⇒ 判红；恢复 ⇒ PASS。
- **给「改完 .go 忘跑 gofmt」加了提交前闸（round 23 立下的机制债，本轮还掉）**：
  上一轮第三次因忘跑 gofmt 被 CI 判红后，我在提交信息里写「记下来不够，得有机制」。本轮还债。
  两层落地：
  ① **受版本控制的判据** `tests/check_staged_gofmt.ps1`：只查**已暂存**的 .go
  （那正是即将被提交、也正是 CI 将要判定的内容），LF 归一化后跑 gofmt；
  与 `check_gofmt.ps1` 分工明确 —— 后者判 **HEAD 全树 + 债务过期**（CI repo-gates 用），
  前者判**暂存区**（本地秒级反馈，把 CI 的 5 分钟等待前移到提交前）。
  ② **本地 hook** `.githooks/pre-commit`（**本身也受版本控制**）：bash shebang，转调上面那个判据。
  启用方式一次性生效：`git config core.hooksPath .githooks`（已配好）。

  **实现过程中踩到六个真实的坑，全部实测定位（不是猜的），全部写进代码注释**：
  1. hook 里也要自己找 git —— 本 hook 由 git spawn，其 PATH 里没有 git 可执行文件；
  2. **必须 cd 到仓库根再转调 PowerShell** —— git 传给 hook 的 `GIT_INDEX_FILE` 是**相对路径**
     `.git/index`，而 PowerShell 进程的 cwd 是 system32；不 cd 则 gate 读到**空暂存区**，
     判据恒「通过」—— 这是最危险的假绿（用环境 dump 实测确认，不是推理）；
  3. hook 名必须无扩展名（只有 `pre-commit.cmd` 时 git 找不到入口）；
  4. `git diff --cached` 必须用**显式进程**调用：`& git ... 2>&1` 会把 stderr 并进输出流，
     PowerShell 记 NativeCommandError、`$LASTEXITCODE` 变空、stdout/stderr 皆空（静默失败）；
  5. hook 环境里 **go/gofmt 也不在 PATH**（与坑 1 同源），且首版凭印象写的
     `D:\app-cache\go\bin` 是 **GOPATH 不是 GOROOT**（实际在 `C:\Program Files\Go`）——
     路径猜错时 stderr 为空、ExitCode 非 0，于是「已 gofmt 的文件」也被误判；
  6. gofmt 同坑 4：也必须用显式进程（症状是「文件已 gofmt 却报 gofmt 报错、错误信息为空」）。

  另有两个设计决定：
  - 用 `.githooks/` 而非 `.git/hooks/`：后者不受版本控制（本仓在册教训「非跟踪、git 还原不了」），
    且首版 `#!/bin/sh` 在这台 Windows 机上**静默失效**（PATH 无 sh，git 报 cannot spawn 却放过提交 ——
    用标记文件探针证明 git 根本没调到 hook）。改用 `#!/usr/bin/env bash`（git 自带 bash 可用）；
  - 判据只查暂存区而非全树：那正是 CI 将要判定的内容；未暂存的工作区改动不判（可能还在编辑）。

  验证（全部端到端，三个场景）：
  - 场景 A：暂存一个合法但未 gofmt 的 .go ⇒ `git commit` **rc=1 被拦**，HEAD 未被污染；
  - 场景 B：gofmt 后再提交 ⇒ **rc=0 通过**；
  - 场景 C：暂存一个语法错误的 .go ⇒ 判红并报告 gofmt 报错（坑 6 的修复验证）。
  - 已接入 `check_all`（rc=2 记 UNVERIFIED，不计 PASS）；`check_gate_links` PASS（47 个脚本）。
- **覆盖率阈值按 CI 重设（承接上一条：上一条按本机定阈值被 CI 判红，2026-10-23）**：
  上一条按**本机**读数把 14 个阈值抬到「实测-3pp」，CI 直接判红。本轮取 CI 实际读数重设。
  **根因是本机与 CI 的覆盖率系统性不同**（同一份代码、同一条命令、都不带 -race）：
  | 包 | CI | 本机 | 差 |
  |---|---|---|---|
  | `internal/web` | 37.7 | 57.4 | **-19.7** |
  | `internal/single` | 71.4 | 89.2 | **-17.8** |
  | `internal/supervisor` | 51.7 | 68.0 | **-16.3** |
  | `internal/retention` | 61.5 | 77.6 | **-16.1** |
  | `internal/executor` | 42.7 | 54.3 | -11.6 |
  原因：CI 无桌面会话、无网络、无真实 DSH 安装，部分测试走不同分支或被环境跳过，覆盖率天然更低。
  **门禁在 CI 跑，所以基准必须是 CI** —— 这是本轮最该记住的一条。
  最终阈值（CI 实测 - 3pp）：engine 68.3→65 / web 37.7→34 / executor 42.7→39 /
  modelpool 58.5→55 / supervisor 51.7→48 / cmd 5.7→2 / evaluate 63.8→60 /
  modelroute 50.6→47 / shortcut 28.9→25 / chain 88.6→85 / judge 81.1→78 /
  single 71.4→68 / dshprofile 77.8→74。
  **`tray` 是唯一本机比 CI 低的包**（本机 21.3 vs CI 26.3 —— 它的断言依赖 Explorer 桌面会话），
  故阈值必须取**两个环境的下界**：21.3 - 3 = 18。规则：双环境都要过，就取下界。
  验证：本地完整 `setup/build.ps1` 跑通（29 个包均达标，BUILD=0）；
  阈值有效性此前已用变异验证过（临时抬到 999 ⇒ 精确点名 `engine=75.9%<阈值999%`）。
  **过程教训（已记入注释）**：本轮首次改 `build.ps1` 时，用 `while` 循环定位注释块边界，
  误把参数行之后的 **200 行正文吞掉**（文件被截断到 17 行）。发现后立即从 HEAD 恢复，
  改用「从后往前 + 行号锚定」重做。教训：**改长脚本必须每步验证行数与关键锚点**，
  不能假设循环边界正确 —— 这次是万幸发现有 `git` 可回滚，若发生在无版本控制的文件上就毁了。
- **覆盖率阈值整表重校：14 个阈值里 9 个已低于实测 15pp 以上（护栏失效，2026-10-23）**：
  复核方法：`go test ./... -count=1 -cover` **连跑两次，读数完全一致**（覆盖率稳定，可直接作阈值依据）。
  发现的问题不是「覆盖率低」，而是**护栏太松**：
  | 包 | 旧阈值 | 实测 | 差值 |
  |---|---|---|---|
  | `internal/single` | 16 | **89.2** | +73.2 |
  | `internal/dshprofile` | 2.5 | **79.5** | +77.0 |
  | `internal/engine` | 20 | 75.9 | +55.9 |
  | `internal/web` | 15 | 57.4 | +42.4 |
  | `internal/executor` | 26 | 54.3 | +28.3 |
  | `internal/evaluate` | 50 | 73.7 | +23.7 |
  | `internal/judge` | 55 | 78.6 | +23.6 |
  | `internal/supervisor` | 46 | 68.0 | +22.0 |
  | `internal/modelpool` | 49 | 70.2 | +21.2 |
  **14 个阈值里 9 个比实测低 15pp 以上** —— 意味着这些包损失 20%+ 覆盖，门禁都不会报。
  顺带修正两处**已过时的包能力描述**（2026-10-21 写时它们确无测试，后来补了真实测试）：
  `dshprofile` 现值 79.5%（13 测试 / 0 skip，env override + 临时目录测到 IO 路径）；
  `single` 现值 89.2%（10 测试 / 0 skip，含 `TestSecondAcquireIsRefused`、`TestLockIsExclusiveAgainstNewHandle` 真排他性判据）。
  处置：14 个阈值全部按「**实测 - 3pp 余量**」重设（留正常波动与小幅重构空间，仍能抓 3pp 以上退化），
  并把复核方法、对照表、维护规则（只许抬不许降、降阈值必须写理由）写进 `setup/build.ps1` 注释。
  验证：完整 `setup/build.ps1` 跑通，**29 个有测试的包均达标**（BUILD=0）；
  变异验证：把 engine 阈值临时提到 999 ⇒ 判红并精确点名 `engine=75.9%<阈值999%`（证明门禁真在逐包比对）。
- **为 4 个在途生产件建立「交接记录」，把并行提交的漏提交风险变成可执行清单（2026-10-23）**：
  上轮修好门禁后报出 4 个生产文件未入库。本轮**没有**代提交（那是并行会话的所有权），
  而是做只有我能做的事：把依赖关系查清并写成交接文档。
  实测依赖图（全工作区扫描）：
  - `modelpool/lanes.go` 的方法 `PurgeGatewayLanes` 被 **3 处已跟踪代码**调用：
    `internal/modelpool/service.go:209`（`SyncNow` 的存量清理）、
    `cmd/workbench/pool_cli.go:158`（CLI 子命令）、`pool_test.go` 4 处（含 `TestPurgeGatewayLanesRemovesLegacy`）；
  - `web/hiddenproc_windows.go` + `hiddenproc_other.go` 的 `hiddenProcAttr` 被
    `internal/web/autostart_windows.go` 4 处调用（子进程隐藏窗口属性，`//go:build` 双分支）；
  - `cmd/schedprobe/main.go` 是独立 CLI（125 行，R3 调度行为验证），无代码引用，单独提交即可。
  **关键取证**：HEAD 侧 `service.go` **没有**该调用（`PurgeGatewayLanes` 在 HEAD 全树仅 1 处且是注释），
  故干净 clone 编译 rc=0 —— 当前是**健康的在途状态**（HEAD 自洽、工作区超前）。
  风险只在一种情况下发生：**引用方先提交而这 4 个文件没跟上**（那会让 `SyncNow` 的清理逻辑静默消失）。
  处置：① 新增 `docs/handoff/在途件交接记录-2026-10-23.md` —— 含依赖表、状态取证、
  **给提交方的 git add 命令清单**、提交后自检命令；
  ② `check_untracked_go.ps1` 判红时直接指向该文档（不只报文件名，还给处置路径）；
  ③ 记录一条**待决观察**：`lanes.go` 的导出符号 `LaneGroupLabel`/`PurgeResult`/`PoolToggle`
  全工作区零引用（可能是预留给面板的 API），按死代码门禁口径应由实现方决定补调用点或删掉。
  验证：doc_refs 门禁 PASS（60 份活文档、80 处引用）；交接文档内的 git add 命令已按依赖组逐条核对。
- **门禁自身的 bug 被抓到：未跟踪件解析把 41 个在途修改误报成生产未入库（2026-10-23）**：
  上一轮加的 `check_untracked_go.ps1` 首版有一处**解析缺陷**：用 `-like '*.go'` 直接筛
  `git status --porcelain` 全表，会把 ` M path.go` 这类**已跟踪修改行**也匹配进来。
  实测后果：FAIL 从真实的 **4 处**虚增到 **41 处**（把并行会话全部在途件误报成「生产代码未入库」）。
  修法：先判 `StartsWith('?? ')` 再取 `Substring(3)`， porcelain 的 ?? 后紧跟一个空格。
  **同一轮我还被自己的临时统计脚本骗了一次**（正则转义失效 => 报 untracked=0，与门禁报的 42 个矛盾）——
  两次都印证同一件事：**读数必须由门禁产出，不由一次性脚本产出**。一次性脚本没有验真机制，
  错了没人知道；门禁有 HEAD 判据、有门禁链校验、有 CI 复跑。
  顺带把门禁输出升级：核心读数（未跟踪数/生产未跟踪数/提交与工作区测试函数数/差额/HEAD）
  落盘到 `.workbuddy/memory/gate-readings.jsonl`，使差额趋势可见 ——
  并行会话逐步提交时该差额应下降，若持续上升说明在途件在堆积。
  修复后实测：4 个生产文件（`modelpool/lanes.go`、`web/hiddenproc_other.go`、
  `web/hiddenproc_windows.go`、`cmd/schedprobe/main.go`），测试差额 100 个。
  已验证 HEAD 侧未断裂：HEAD 的 `service.go` **没有** `PurgeGatewayLanes` 调用
  （该符号在全树仅 1 处且是注释），故干净 clone 编译 rc=0 ——
  当前形态是「工作区有调用+定义、HEAD 两者皆无」，属健康的在途状态，提交后会丢失该功能。
- **「本地绿 ≠ 提交绿」被量化：98 个测试只在本机跑，CI 永远看不到（2026-10-23 实测）**：
  审计「未跟踪 .go」发现 **42 个未跟踪文件**（41 个 `_test.go` + 3 个生产文件 ——
  `modelpool/lanes.go`、`web/hiddenproc_other.go`、`web/hiddenproc_windows.go`）。
  两层次风险：
  ① **质量层（可量化）**：HEAD 提交的测试函数 **717** 个，工作区（含未跟踪）**815** 个
     —— **98 个测试只在本地跑**。这意味着过去多轮「重跑本地即绿」里有 98 个判据根本没参与，
     拿本地绿推断提交质量会**系统性偏乐观**。
  ② **功能层（已在途）**：`modelpool/lanes.go` 定义了 `PurgeGatewayLanes` / `LaneGroupLabel`，
     而**已跟踪的** `service.go:209` 与 `pool_cli.go` 已在调用它；该文件 `git log --all` 为空
     （从未入库）。当前 HEAD 的 `service.go` 还没有这行调用，故干净 clone 编译 rc=0 ——
     但一旦工作区版本提交而 `lanes.go` 没跟着提交，`SyncNow` 里的清理逻辑会**静默消失**。
  这正是本仓在册事故（「依赖文件从未入库 → 干净 clone undefined」）的**当前进行时形态**。
  处置：新增 `tests/check_untracked_go.ps1`，把差距变成可见读数：
  - **生产代码未跟踪 ⇒ rc=1 FAIL**（那类一定坏）；
  - 测试文件未跟踪 ⇒ rc=0 但**逐条点名 + 报出差额**（并行在途件是正常工作流，
    不由本门禁判红，但必须可见）；
  - 输出含「提交的测试函数 / 工作区测试函数 / 差额」三行读数。
  **刻意不接入 CI repo-gates**：CI 干净检出天然无未跟踪件，接进去只会恒 PASS（假绿）；
  它是**本地/维护期**判据，接在 `check_all` 里。
  首轮运行即按设计 FAIL 并点名 3 个生产文件（全部为并行会话在途，已记录待其提交）。
- **判据健壮性系统扫描（2026-10-23 r39，验证型工作，无代码改动）**：
  承接 r38「我自己写的判据也可能带 CI 隐患」的教训，全仓 134 个 _test.go 扫四类脆弱模式：
  A runtime.Caller 定位 / B 绝对路径字面量 / C t.Skip / D os.Getenv。逐项核实后**全部为误报或合法**：
  A 只剩 reporoot_test.go 的解释性注释（非调用）；B 是 CSS 正则里的字母冒号反斜杠被扫描式误认
  成盘符 + retention 测试故意喂的危险路径样本；D 零命中；C 的 14 处 skip 都是合法环境守卫。
  **静态扫描只提嫌疑，真正定论靠运行**：
  ① 全量 `go test -trimpath ./...` → **rc=0、0 FAIL、29 包全过**（r38 隐患的直接反证：
  仓库内已无 runtime.Caller 定位残留）。
  ② 编译测试二进制 `-trimpath` 后从**外部目录**（xtmp）运行两条仓根判据 → **PASS 非 SKIP**，
  真正逼出 findRepoRoot「从 cwd 向上走找 tests/ui」这条 go test 平时走不到的分支（go test 总在
  包目录跑），证明 r38 修法在最硬场景成立。
  ③ 全量 `-test.v` 枚举 → **SKIP=0**，即当前无任何休眠守卫（每条带 t.Skip 的判据此刻都实跑，
  如 toast 守卫因 gov.js 确有 2 处调用而实跑 PASS，非跳过蒙混）。
  附带再证一条发布纪律：验证期间 `gov_api.go`（非我写域）被并发会话瞬时写成 `encoding/json`
  未使用而 vet 红，我做 3 次有界重试未代改，它自清后 build clean —— 「不碰他人在途件、等其落定」
  第四次被实测证明是对的。
### Fixed
- **清掉 3 处死代码 + 新增死代码门禁（2026-10-23 审计发现）**：
  全仓死代码审计（未导出函数，全树词边界计数）发现 **3 处零引用函数**：
  `cmd/workbench/main.go:562 openPanelOrPrint`、`internal/executor/executor.go:527 (*Runner).environment`、
  `internal/web/gov_api.go:163 encodeGovJSON`。三处共同形态：**函数体完整、注释还在解释「为什么要它」，
  但没有任何调用点** —— 整树词边界计数均为 1（仅定义行）。
  危害不是运行时而是**阅读与维护**：读代码的人会假设它被调用（注释还在承诺行为），
  于是基于一个假前提做判断。它也是「代码规模只增不减」的微观形态，与 memory 治理同族。
  处置：① 三处全部删除（已逐一双重验证零引用），连带修掉删除后暴露的
  `internal/web/gov_api.go` 未使用 `encoding/json` 导入；
  ② 新增 `tests/check_deadcode.ps1` 把这类缺口变成机器判据，接入 **repo-gates（第 12 个门禁）**
  与 `check_all`。判据只报「名字在本树零引用」这一**可证伪事实**，删不删由人判 ——
  这正是「机器判据给证据、人做决定」的分工；已知假阳性形态（接口实现、反射使用）
  在门禁头部注释里列明，留作后续白名单的输入。
  顺带确认一个**反面具仓健康的事实**：`go test ./... -count=2 -race` 全仓 29 包**零 flaky**
  （每条测试连跑两遍仍全绿）—— 说明测试套件本身稳定，没有时序依赖。
  验证：build rc=0；受影响三包测试全绿；死代码门禁从 rc=1（报 3 处）转为 rc=0（报 0 处）；
  干净 clone 上判据同样能跑（只依赖 git ls-files + 文件读，不依赖产物）。
- **自我更正：上一轮新加的两个判据在 -trimpath 下假红（2026-10-23 r38）**：
  上轮加的 ui_script_portability_test / gate_files_tracked_test 用 runtime.Caller(0) 取本文件
  路径再上溯定位仓根。但 go test -trimpath（发布/CI 构建常见）会把编译期记录的源文件路径裁成
  去前缀形式 => thisFile 不再是真实磁盘路径 => 仓根算错 => **两判据在 -trimpath 下全红**
  （实测：probes 数读成 0、只有 0 个）—— 这正是本仓在册的「判据存在但没挂受理面 / 本机绿
  CI 红」死结类，而这次是**我自己刚引入的**。CI 当前 test job 用 go test -race（不带 -trimpath）
  侥幸躲过，但属定时炸弹。
  修：新增共享 helper findRepoRoot（reporoot_test.go）—— 从 os.Getwd() 逐级向上找含 tests/ui
  的祖先目录。Getwd 是 go test 真实运行目录（文件系统真实路径，不受 -trimpath 影响），无论 cwd
  是包目录还是仓根都稳定命中；找不到则 **Skip（fail-closed，绝不假绿）**。
  **三态实测**：plain / -trimpath / -race 全 PASS；全量 web **126 PASS / 0 FAIL / 0 SKIP**
  （SKIP=0 反证 helper 在真实包目录 cwd 下确实命中仓根，不是靠跳过蒙混）。
  变异 N1（把定位标记改成不存在目录）=> helper 正确 **SKIP 而非假绿**，证明不恒真。
### Fixed
- **前端验收脚本硬编码仓根路径，换机器 / CI 检出即崩（2026-10-23 可移植性审计）**：
  4 个验收脚本把仓根写死成 `const ROOT = "D:\\workdesktop"` 或
  `OUT = "D:\\workdesktop\\dist\\ui-shots"` 这类绝对路径 —— 后果是**前端验收永远无法接进
  CI**：CI 检出到别的目录，脚本一启动就 `ReferenceError` / 找不到文件，判据「存在但只在
  某台机器上能跑」等于没有受理面（与本仓已登记的「判据存在但没挂受理面」同型失效）。
  修：全部改为从脚本自身位置推导仓根 —— `path.dirname(fileURLToPath(import.meta.url))`
  上溯两级。用 `fileURLToPath` 而非 `new URL().pathname`：后者在 Windows 上会得到
  `/D:/...` 还要再剥前导斜杠，正则易写错（本轮 v1 就写错成正则匹配不到，已改）。
  **迁移实证**：把 4 个脚本复制到 `xtmp\relocate-test\tests\ui\` 后，ROOT 推导结果为
  `xtmp\relocate-test`（正确），不再依赖 `D:\workdesktop`。
  **功能零回归**：门禁复跑 **23/23 PASS、errors 全 0**，且此前 5 个 unverified 用例
  （真实窗口 594×350 / 1040×613）现在全部通过。
  新增 `ui_script_portability_test.go` 判据（扫 tests/ui 全部 .mjs，禁绝对路径字面量；
  浏览器安装路径除外 —— Edge 装在固定位置是事实且已支持 `EDGE_BIN` 环境变量覆盖）。
  判据自身踩了两个坑并修：① Go 测试 cwd 是包目录，相对路径 `..\..\tests\ui` 算错
  ⇒ 改用 `runtime.Caller(0)` 定位本文件再上溯 3 层；② 首版误把 Edge 安装路径判成缺陷
  ⇒ 加排除。变异 M1 恢复硬编码 ROOT → CAUGHT、M2 改用 `process.cwd()` → CAUGHT。
### Changed
- **裁剪后基线重取（性能基线口径.md 新增 §8，2026-10-23）+ 上轮结论再精确化**：
  兑现口径文档 §6 的「裁剪后基线重取」待办：按 §2 同一取证方式（`--ui-probe --expect-idle`、
  预热 5 + 60 样本 × 3 轮），唯一差别是**数据根拷入线上裁剪后的 queue.json（5,006 条 / 9.8MB）**。
  读数：P95 = 10.72 / 10.60 / 10.79 ms；**常驻 = 52.93 / 52.81 / 53.02 MB（±0.1，极稳）**；
  句柄 180/185/197。
  由此对上轮结论做一处**必要的精确化**（不改变「基线不该改」的结论）：
  上轮说「探针 13.3MB 与基线 12.41MB 吻合」——那吻合的是**空数据根**形态
  （`check_perf` 用全新 temp，没有任何任务记录）；带当前记录数（5,006 条）后
  探针常驻是 **~53MB**，记录贡献约 40MB。两个数都合法，但含义不同，此前未区分。
  结论仍不变：`residentMBMax=64` 对它实际所测的形态（空载探针）保有 ~5× 余量，**不改**；
  把它换成 53 反而会让 CI 上跑空载探针的判据变成 1.2× 紧箍（一次正常抖动就误判）。
  顺带沉淀一条判据纪律：**脱离记录数谈常驻上界没有意义** —— 任何内存判据必须先声明
  数据规模形态（§4 三形态表已按此拆分：空载探针 / 带载探针 / 长驻空闲 / 长驻执行期）。
- **验收总账（2026-10-23 r35）：八维 + Awwwards 四维 + 规模/异常/性能全部有机器化证据**：
  八维：排版（溢出 0 / 顶距统一 / 卡片语言统一）、留白（三页 padding 同值）、视觉层级（h1 语义已修）、
  色彩（蓝青 66% 主导 + 语义色）、动效（6 场景 60fps / jankFrames=0 / 含 SSE 活跃态）、
  微交互（armed confirm 3 退出通道 / focus-visible 14 处）、响应式（九档宽度 + 200%/400% 缩放）、
  原创性（AI 风格 0/6 真实命中 / 独有语言密度 124 / 零外部依赖）。
  四维：Design（令牌统一 + 字体全系统内置）、Usability（可访问名全覆盖 + 键盘焦点环全可见）、
  Content（h1 + 12 表 aria-label + 16 装饰 aria-hidden + 164 文案规范）、Creativity（同八维原创性）。
  规模/异常/性能：33,728 任务下产出页窗口截断修复、极端长串溢出修复、SSE 10 事件/秒下滚动保持、
  长跑内存非泄漏实证、queue.json 59MB→9.8MB、限流期提示完备、7 页无死路、端到端链路修复。
  工程：web **124 PASS / 0 FAIL**、gov **28 / 0**、全量 0 FAIL；发布 1.7.3；变异体 30+ 全部 CAUGHT。
  **唯一未闭环**：P1 技能升级落库（他方 `benchmark*.md` 2 件在途，按补条二第二档②停手，
  解锁命令已写入 `07-next-steps.part142`）。
  总账全文：`07-next-steps.part143`（八维+四维+规模）+ `part144`（工程面+未闭环+结论）。
### Changed
- **Creativity 维度验收：设计体系原创性量化（2026-10-23 r34）**：
  ① **AI 生成风格标记 0/6 真实命中**（逐项核实，非粗略正则）：
  · `backdrop-filter` 5 处 —— 用于顶栏（页签滚动时内容不糊）与 Toast，属**功能性毛玻璃**而非
  玻璃拟态审美；且有 `@supports not` 降级分支，属工程谨慎而非跟风。
  · `border-radius ≥20px` 仅 2 处 —— 滚动条 thumb（圆形）与启动按钮（`clamp` 大尺寸下的圆角），
  主体体系是 `--radius:12px`。
  · `Inter`/`Roboto` **0 处真实使用** —— 我的正则误匹配了 `cursor:pointer` 里的 `inter`（假阳性）；
  实际字体为 `Bahnschrift SemiCondensed` / `Segoe UI Variable` / `Cascadia Mono`（全 Windows 系统内置）。
  · 无紫色渐变主色、无 emoji 当图标、无 `background-clip:text` 渐变文字。
  ② **独有设计语言密度 124 处**：HUD 定位标/刻度尺 **51**、径向极光 16、自定义 `@keyframes` 16、
  扫描线纹理 15、`:focus-visible` 焦点环 14、`inset` 伪描边 4（兼容 clip-path）、clip-path 切角 3、
  `@property` 动画 3、conic 旋转边框 2。
  ③ **颜色纪律**：38 个十六进制色中 **蓝青系 25 个（66%）** 主导，暖黄 2（警示）+ 红粉 4（危险）
  作语义色，其余 7 个中性灰 —— 严格符合「单一强调色 `--accent:#22d3ee` + 语义色」纪律，无杂色。
  ⇒ 结论：设计体系是自建的「精密仪器 HUD」语言，**不是常见 AI 生成风格的模板套壳**。
### Fixed
- **长驻实例内存判据落地：三档形态实测 + 上界定 128MB（2026-10-23，兑现上轮待办）**：
  上一轮留下待办「长驻形态需要新增判据」。本轮先做**取证**，发现「空闲」本身分两档：
  | 形态 | 实测 RSS | 说明 |
  |---|---|---|
  | 空闲探针（`--ui-probe`） | **13.3MB**（3 轮） | 与既有基线 12.41MB 吻合 |
  | 服务化长驻·**刚启动** | **46.9MB**（10 次采样**完全恒定**） | 可设固定上界 |
  | 服务化长驻·**跑过任务后** | 73 → 98.3 → **95.5~107.4MB 来回**（5 分钟观察） | Go 的 GC 保留内存池，不随任务结束归还 |
  关键机制（实测）：引擎 `--stop` 后 RSS 会**先在高位停留再回落** —— 12s 内 95.1MB、
  20s 时跌到 51.8MB 并稳定。故判据**必须先等内存稳定再取数**，否则在「刚跑完任务」的
  高点上误判（首版实测 102.3MB 超 96MB 上界，而稳定值只有 51.8MB）。
  新增 `tests/check_resident_longrun.ps1`：
  - **稳定等待**：最多 40s，连续 3 次相邻采样差 ≤ 1MB 视为稳定，取稳定段中位（实测稳定耗时 12~22s）；
  - **只认本产物路径的实例**（同机可能有压测/探针副本，按名字全取会串读数）；多实例取启动最早者；
  - **只在空闲态判**：`state.json` 的 phase=running 时直接 UNVERIFIED —— 执行期内存随并发变化属预期，
    拿固定上界卡它只会产生假红；
  - 上界 **128MB**（不是 96）：实测「跑过任务后空闲」在 95.5~107.4MB 来回，96 会随机误判；
    128 仍能抓「量级塌方」（裁剪前 170MB 稳态、或 200MB+）。这是当前证据能支撑的最大结论。
  - 三态：无实例/引擎在跑/内存未稳定 ⇒ rc=2 UNVERIFIED（**不把没测成当合格**）。
  接入 `check_all`（rc=2 显式改写为 0 并保留 UNVERIFIED 字样，与 check_perf/check_queue_size 同口径）。
  验证：PASS 分支（78.6MB / 上界 128）与变异分支（上界压到 50 ⇒ 判红）均实测。
  **并记录一条判据可执行性缺陷**：7x24 实例被 keepalive 每分钟拉起，空闲窗口难得 ——
  实测两次调用都因 phase=running 判 UNVERIFIED。该判据更适合发版前/维护窗口执行，
  不宜挂在高频巡检里；此结论已写进口径文档。
- **自我更正：上一轮「内存基线口径被证伪」是误判（2026-10-23 复测）**：
  上一轮记了「`residentMBMax: 64` 被证伪」，依据是 `--silent` 长驻实例实测 86~170MB / 峰值 752MB。
  **复测后确认那是误判**，必须留记录防重犯 —— 判据没坏，是我当时**拿错了对照形态**：
  - `check_perf.ps1:173` 起的是 `--ui-probe --expect-idle --seconds 120` **空闲探针**、取数窗口约 10 秒；
  - 本轮用**同一命令**实测 RSS **13.3 / 13.3 / 13.4 MB（3 轮）**，与基线 12.41MB 吻合 ⇒
    `residentMBMax=64` 对它自己的形态**是准确的**，不该改。
  - 那 86~170MB 属于**另一种形态**（`--silent` 服务化长驻 + 5,007~33,740 条记录），
    而该形态**本来就没有判据** —— 缺的是「新增」，不是「修正」。
  已把三形态对照表（空闲探针 13.3MB / 长驻 86MB / 裁剪前长驻 170MB）与「两口径不可混用」
  写进 `docs/benchmark/性能基线口径.md`，并把 §6 待办改为准确表述。
  教训（与本仓在册的「读数过期把提交判红」「判据没碰到被测代码」同族）：
  **拿两个不同测量形态的数字互相比对，得出「基线错了」的结论，比基线真的错了更危险** ——
  它会诱导后人去改一个本来正确的护栏。取证时必须先确认「两边量的是同一个东西」。
- **12 个数据表全无语义名称 + 12 处装饰元素无 aria-hidden（2026-10-23 Awwwards 内容维度验收）**：
  ① **表格语义**：静态提取全站 12 个 `<table>`，**12/12 全无 `caption`/`aria-label`** —— 读屏器
  只会念「表格 4 列 3 行」，说不清是什么数据。其中仅 5 个有邻近 h3/h4 可间接推断，7 个语义完全丢失。
  修：全部补 `aria-label`（模型健康度明细 / 失败原因分布 / 排队任务列表 / 执行中与待执行任务 /
  已完成产出 / 任务模板库 / 操作审计流水 / 失败任务列表 / 运行信息 / 车道汇总 / 模型明细）。
  ② **装饰元素噪音**：12 处 `.sep`（视觉分隔符 `·`）+ 4 处 `.dot` 状态点 + 1 处 `.stall-icon`（内容就是
  `!`）全部缺 `aria-hidden`。这些元素的语义是**重复的**（旁边文字已说「引擎 运行中 / 排队 6」），
  读屏器念出来只是噪音。修：全部补 `aria-hidden="true"`。
  **实测复验**：活实例 `app.js` sep 9/9、table aria-label 9/9；`pool.js` sep 3/3、table 3/3；
  stall-icon 与 dot 全部隐藏，与源码逐项比对 `match: true`。
  新增 `content_semantics_contract_test.go` 两条判据（每个 `<table>` 必须有可访问名称 /
  装饰元素必须 aria-hidden）；变异 C1 去掉表格 aria-label → CAUGHT、C2 去掉 sep aria-hidden → CAUGHT、
  C3 去掉 stall-icon aria-hidden → CAUGHT。
- **前端门禁的依赖文件不在版本库里（2026-10-23 受理面完整性审计）**：
  上一轮把 4 个入口脚本（frontend-gate / cdp_probe / design-audit / design-shots）
  `git add` 时以 `create mode` 入索引 —— 说明它们**此前未被跟踪**。顺藤摸出更大的洞：
  `tests/ui/assertions.mjs`（探针读数→判据的**唯一判定层**）、`assertions_selftest.mjs`
  以及 `tests/ui/probes/` 下 **14 个探针**（cdp_probe 按文件名加载）全部是 `??` 未跟踪。
  后果不是本机跑不了，而是**换一个 clone / CI 上根本不存在**：`node tests/ui/frontend-gate.mjs`
  会报 `Cannot find module ./assertions.mjs`，而本地一切正常 —— 这是本仓在册失效形态
  「判据存在但没挂受理面」的升级变体：**受理面的依赖文件不在版本库里**。
  修：16 个门禁必需文件全部入跟踪；`assertions_selftest.mjs` 跑通（判据层可证伪且不误报）。
  新增 `gate_files_tracked_test.go` 判据（git ls-files 校验 4 个关键文件 + probes ≥10 个）；
  变异 V1 把 assertions.mjs 从索引移除 → CAUGHT。

> 过程记录：判据第一版按 `>` 截断取标签，被拼装式 HTML 里的三元运算符 `> 0` 提前截断，
> 导致「明明加了 aria-hidden 也判成缺」的假阳性（本轮实测踩到）。改为固定窗口内直接查找后正常。
> 另核验一次「双实例」疑点：受控双启动测试（连续两次拉起）证明**单例守卫正常工作** ——
> 第二次被拦、最终仅 1 进程、锁未被覆盖。此前的双进程是并发会话各自启动所致，非产品缺陷。
### Fixed
- **裁剪在线上首次生效 + 内存判据口径被证伪（2026-10-23 实测闭环）**：
  上一轮落地的 `Store.PruneTasks` 已**在线上真实生效**（不是「待重启」）：活实例 20:53 启动时
  执行裁剪，`queue.json` **69.3MB → 9.8MB**（33,740 → 5,007 条），审计留痕在册：
  「按龄删 0 条、按上限删 28733 条，保留 5007 条，清孤立链 23101 条」；
  **7 条非终态任务一条未动**（实测逐条列出 7 个 queued id）。
  顺带证伪了一条基线口径 —— 这比功能落地更值得记：
  `perf-baseline.json` 的 `residentMBMax: 64` 取自 `--ui-probe` **短生命周期**实例（12.41MB × 余量），
  而对 `--silent` **长驻**实例实测：
  | queue 记录数 | queue.json | RSS 稳态（3 轮中位） | 峰值 |
  |---|---|---|---|
  | 33,740 | 69.3MB | 170MB | 752MB（6 任务并发期） |
  | 5,007 | 9.8MB | **86MB**（46/99/86 三轮） | 115MB |
  两个结论：① **内存与记录数强相关**（裁剪是内存治理的前置条件，不是可选优化）；
  ② **稳态读数本身波动 2 倍**（46~99MB）—— 拿单点值卡 64MB 会随机误判，
  该字段只适用于短生命周期实例或量级塌方判定。已把上述矩阵与适用范围写进
  `docs/benchmark/性能基线口径.md`（含新增 §6 待办：长驻内存判据需「多轮取中位 + 记录数上限」）。
  新增 `tests/check_queue_size.ps1` 守「裁剪真的在生效」：任务数 ≤ MaxTerminalTasks × 1.1
  （余量吸收启动后新增），并检查「全为非终态仍超限」这种绕过形态；
  **三态语义**：无 queue.json ⇒ rc=2 UNVERIFIED（不当 PASS）。
  接入 `check_all` 时显式把 rc=2 改写为 0 并保留 UNVERIFIED 字样（与 check_perf 同口径）；
  **刻意不接 repo-gates** —— CI 干净检出没有 dist/，接进去会因 rc=2 长期判红。
  实测：门禁 PASS（5,006 条 / 8.62MB，上限 5,000×1.1=5,500）；
  变异验证：合成 6,000 条 ⇒ 判红并点名超限。
- **全站 h1 数为 0：后台三页主标题用 `<span>` 无任何语义层级（2026-10-23 Awwwards 内容维度验收）**：
  三张后台页的主标题此前是 `<span class="pagetitle">管理面板</span>` 这类写法 —— 视觉上是大标题、
  语义上**没有任何层级**。CDP 实测修复前：admin / governance / modelpool 的 `h1` 数**均为 0**，
  DOM 里只有 h3（admin 仅 1 个 h3「系统状态」、治理台 2 个、模型池 4 个）。
  屏幕阅读器用户因此无法用「按标题跳转」定位主标题 —— 这正是无障碍导航的核心方式。
  修：三处 `<span class="pagetitle">` 改 `<h1 class="pagetitle">`，并在 CSS 补 `margin:0`
  重置（`.pagebar` 是 flex 容器，h1 默认 margin 会破坏基线对齐）。
  **实测复验**：admin / governance / modelpool 各 **1 个 h1**（文本分别为「管理面板」「治理控制台」
  「模型池」），旧 span 全部清除，`<main>` landmark 正常。
  **首页不加 h1 是设计决策而非缺陷**：首页契约明令禁止标题元素（第一屏必须极简，只允许
  1 个 button + 3 个 a，div/标题/媒体/表单/表格一律禁止）；且 `<title>` 已提供页面名、
  `<main>` 提供 landmark，屏幕阅读器可达。
  新增 `h1_semantics_contract_test.go` 三条判据（三页必须有且仅有一个非空 h1 / `.pagetitle`
  不得退回 span / h1 必须有 margin:0 重置）；变异 H1 admin 退回 span → CAUGHT、H2 模型池退回
  span → CAUGHT、H3 去掉 margin 重置 → CAUGHT。

### Changed
- **内容维度验收（同轮）另外两项结论**：
  ① **错误提示可操作性**：静态提取全部「失败/错误」类 toast 共 1 条（`详情面板缺失`），
  属渲染契约错误提示，无需给用户下一步动作（开发者向）。其余失败提示均走「XX失败：<原因>」
  形态（前轮已固化判据），用户能看到原因。
  ② **空态完整度 3/5（60%）**：两条「没有失败任务 —— 保持这样最好。」只有「为什么空」无「怎么处理」
  —— 但这是**语义上正确的**（没有失败任务不需要用户处理），非缺陷；其余三条完整。

> ✅ **已发布 1.7.2**（SHA256 `ABFAE364…8F0A2`，官方链 7/7，frontend-gate 23/23）。
> 发布过程记录（一次门禁阻断 + 一次真实修复）：
> ① 首次尝试时 gofmt 门禁判红。逐一核验后确认**我的 7 个改动文件全部干净**，
>    被点名者为 HEAD blob 本就 dirty 的历史遗留件与并行会话新建未跟踪件 ⇒ 不代跑其 gofmt，等待其落定。
> ② 门禁二次运行时点名 `workbench/internal/web/pool_page.go` 属「登记但已不漂移」的**失效豁免**
>    （因本轮把该文件的 h1 修好、格式变干净，豁免反而成了「对该文件永久失明」的隐患）
>    ⇒ 从 `tests/gofmt-debt.txt` 删除该行（这是我造成的，合规可删），剩余豁免 32 条。
> ③ 核对 3 个 store/retention 文件的归属时踩到假阳性：用 `Set-Content` 导出 HEAD blob 会引入 BOM，
>    导致 gofmt 误判 dirty；改用 `git cat-file blob > file` 保留原始字节后确认三者均**已合规**。
> ④ 门禁随即 PASS（318 文件检查），完成发布。教训：**核验 HEAD 格式必须保留原始字节**。
### Fixed
- **`queue.json` 只增不减：59MB 快照 / 33,740 条记录常驻内存（2026-10-23 实测，治本）**：
  上一轮 e2e 判红暴露的「常驻内存 170MB vs 基线 64MB」不是泄漏，是**数据规模**：
  实测 `queue.json` **69.3MB / 33,740 条任务**，其中终态 **33,733 条**（rejected 33,187 条 ——
  正是本仓记录过的空转热循环残留），而非终态只有 **7 条**。根因是结构性的：
  `retention` 只清 `tasks/` **目录**（`PruneTaskDir`），而 `store` 里**从无删除任务记录的 API**；
  `persist()` 又每次全量序列化 + 内存复制一份 `snap.Tasks` ⇒ 常驻 3.3 万对象、并发高峰 752MB。
  处置：
  ① `retention.Policy` 新增 `TerminalTaskDays`(默认 7 天) 与 `MaxTerminalTasks`(默认 5000 条)，
  二者**取交集**（只按天数不够：热循环数小时内可产数万条 rejected，窗口内照样撑爆快照）；
  ② 新增 `Store.PruneTasks(pol, dryRun)`，安全边界六条：**只删终态**（有测试钉死非终态一条不许动）、
  按龄、按上限**最旧优先**淘汰、有任务引用的 chain 保留而孤立 chain 一并清、
  dryRun 一条不删但如实报候选数、裁剪后写审计留痕；
  ③ 接入引擎 `Recover()`：位置讲究 —— 在 `Reconcile()` **之后**（先收敛悬挂 running）、
  `List()`/队列重建**之前**（否则待删记录被塞回队列）；启动期一次，失败不阻塞启动但必须告警。
  **实测抓到一个真 bug（本功能自身）**：首版裁剪后只调 `persist()`，重启记录**全部复活** ——
  因为 `Open()` 的加载顺序是「先读快照 → 再回放 WAL」，被删记录在 WAL 里有历史。
  改用 `compact()`（写快照 **+ 截断 WAL**）才真正生效。这条由判据当场抓到。
  **真实数据验证**（复制线上 69.3MB queue.json 跑）：69.3MB → **9.8MB（-86%）**，
  33,740 → 5,007 条，清孤立链 23,101；**非终态 7 条一条未动**；重启后仍为 5,007（持久化正确）。
  判据 9 条（store 6 + engine 3）+ 配置归一化 2 条；变异验证三条全部 CAUGHT：
  MUT-A 去掉「非终态不许删」守卫 ⇒ 判红（实测「byAge=6」误删 6 条在跑任务）、
  MUT-B 退回 `persist` 不截 WAL ⇒ 判红（记录复活）、MUT-C 上限改为淘汰最新 ⇒ 判红。
  store 覆盖 77.4% → **81.3%**。
- **4 处表单控件缺可访问名称（2026-10-23 Awwwards 四维验收，WCAG 1.3.1 / 4.1.2）**：
  三处 `<label>领域</label><select id="mDomain">` 形态 —— 视觉上标签就在旁边，但 `<label>`
  既未用 `for/id` 关联也未包裹控件，**屏幕阅读器读不到控件名**；另 `#strategySel`（模型调度策略）
  完全无标签。共 4 处：`mDomain` / `tDomain` / `tEnabled` / `strategySel`。
  修：前三处补 `for` 关联，第四处补 `aria-label="模型调度策略"`。
  **实测复验**：源码与构建产物（exe 二进制内文本）四处均确认命中。
  新增 `a11y_name_contract_test.go` 一条判据，双向覆盖：① `<label>文字</label><select id=X>`
  这种「相邻未关联」形态一律判红；② 每个 `<select id=X>` 必须满足之一（`aria-label` / 被 `<label>`
  包裹 / 存在 `for=X` 的 label）。变异 V1 去掉 `for` 关联 → CAUGHT、V2 去掉 `aria-label` → CAUGHT。

### Changed
- **Awwwards 四维验收证据采集（同轮，含一项证伪）**：
  ① **Design**：四页共用同一套令牌（`--bg #04070d`、`--accent #22d3ee`）；字体族 3-4 个
  （`Noto Sans SC` / `Segoe UI Variable` / `Bahnschrift SemiCondensed` / `Cascadia Mono`，**全部 Windows
  系统内置**）；首页 3 种圆角变体、1 个 clip-path、4 个动画元素、6 个过渡元素；治理台 7 个 clip-path
  （HUD 切角密度最高）。
  ② **零外部依赖（彻底确认）**：4 页 HTML 外部 URL **0**、CSS `url(http…)`/`@import` **0**、
  JS 外部域名 **0**、网络字体 `document.fonts.size` = **0**。全部资源来自本机 `/assets/`。
  ③ **证伪**：判据曾报「externalResources: 3」，逐一核实后是 `favicon.svg`/`app.css`/`app.js` 三个
  **本机** `/assets/` 资源 —— 我的正则 `/^https?:/` 把 `http://127.0.0.1` 也算成外部。**非缺陷。**
  ④ **Usability**：四页 `unlabeled` = 0、`tinyControls`（<28px）= 0、`lang=zh-CN` 正确。
  但「unlabeled=0」只是浏览器只查 `aria-label`/`title` 的浅判据 —— 深查（label 关联）才找出上述 4 处。
### Changed
- **动效流畅度实测（2026-10-23 r30，Awwwards 评审硬指标，此前只验「动效存在」未测帧率）**：
  用 rAF 采样 3 秒统计帧间隔分布，6 个场景实测：
  | 场景 | FPS | avgMs | p95 | p99 | 卡顿帧 |
  |---|---|---|---|---|---|
  | 首页入场（前 3s）| 59.0 | 17.02 | 16.8 | 33.3 | **0** |
  | 首页静止 | 60.3 | 16.63 | 16.8 | 16.8 | **0** |
  | 后台入场 | 60.3 | 16.58 | 16.8 | 16.8 | **0** |
  | **后台静止（SSE 活跃）** | **60.3** | 16.61 | 16.8 | 16.8 | **0** |
  | 治理台 | 60.3 | 16.62 | 16.7 | 16.8 | **0** |
  | 模型池 | 60.3 | 16.66 | 16.7 | 16.8 | **0** |
  全部稳定 60fps、**零卡顿帧**（>50ms 帧数 = 0）、p95 = 16.8ms（单帧 16.7ms 预算内）。
  **最关键一条**：SSE 每秒约 10 条事件的重渲染场景下仍保持 60fps —— 反向验证 r23 的
  「400ms 节流 + 滚动快照恢复」修复真实有效（修前是每条事件全量重渲染）。
  首页入场 fps 略低（59.0）且 p99=33.3ms 属预期：入场序列有多个元素同时启动动画，
  首帧会有一次合成层提升，之后即回满帧。
### Changed
- **收尾对账：ds 端欠账 7 项 → 清 3 项（两项实测证伪）、剩 4 类全部有外部卡点（2026-10-23 r29）**：
  本轮把 28 轮以来登记的 ds 端未闭环项逐条核验：
  **① 模型池「网关在线 vs 可用数口径不一致」→ 证伪撤销**：实测 `/api/pool` 为 42 模型
  （41 healthy + 1 degraded），三车道 `available == total`（free 18/18、kilo 19/19、eac 5/5）。
  此前记录的「48/65」是旧偶发截图，当前口径完全一致，非缺陷。
  **② 产出解析器「两套主产出判定口径分裂」→ 证伪撤销**：引擎 `engine.go:1164` 已把 primary
  完整路径写入 `task.OutputFile`；实测 86/86 done 任务全部有 outputFile，抽查 5 个文件实际
  文件大小与记录**完全一致**（4363/705/5948/4799/3383 字节）⇒ `readTaskArtifact` ① 级永远命中，
  ②③ 级是纯兜底，口径分裂隐患不存在。
  **③ CHANGELOG Unreleased 段 14 小节头 → 已清**（前轮已合并，实测无 `### ` 小节头）。
  **仍未闭环 4 类，全部 blocked（非我方可解）**：P1-FONT6 三款商业字体缺授权（blocked-external）；
  P2 技能镜像同步（镜像侧 -Fix 工具本机未定位，blocked-tooling）；P1-FOOTERHOOK pre-commit 接
  evolution 门禁 + P1-PREAD Step 0.55 + 技能升级落库（均 blocked-by-other-holder，他端在途件 `M`）；
  P1-DShB-1 Blender 插件 lib 需宿主重启（重启会掐死在跑会话，P0#35 须有把握）。
### Fixed
- **4 份活文档点名的验收入口 `tests/e2e.ps1` 从未入库（2026-10-23 实测，已恢复）**：
  `docs/` 下 4 份活文档（含《六项要求验收矩阵》给出具体复跑命令）把 `tests/e2e.ps1` 当作
  可复跑的验收入口，但该脚本 **`git log --all` 为空** —— 从未入库，2026-10-19 仓库清空事故后
  只剩 `src-backup/tests/e2e.ps1` 副本。同族：`tests/e2e_soak.ps1`（文档写「期望 18 PASS」，
  同样只在备份里）。使用者按图索骥必然失败，而这两份脚本承载 **74 + 18 条真实判据**。
  处置（**恢复而非删引用**）：
  ① `e2e.ps1` 恢复到 `tests/`，仅改硬编码路径一处（默认产物由 `$PSScriptRoot` 推导，与
  2026-10-22 修的同类失真一致）；实测能跑起来：**24 PASS / 3 FAIL**，证明脚本可用；
  ② `e2e_soak.ps1` 恢复并同样去硬编码；自包含跑出 **18 PASS / 0 FAIL**（与文档期望值吻合）；
  ③ 3 个 FAIL 逐个定性（**不为变绿而放宽判据**）：
  - `admin has governance tab` —— **判据过期**：治理台已改成独立页面，入口在顶栏「更多」菜单的
    `<a href="/governance?token=...">`。改为断言该链接**真实存在且带 token 透传**（强度不降：
    页面能打开但入口不存在，用户根本到不了）；
  - `memory < 200MB` —— **阈值与基线冲突**：仓库基线 `perf-baseline.json` 写 `residentMBMax: 64`，
    而脚本拍 200MB，既放得过 3 倍劣化又不合基线口径。改为**读基线判**，读不到才退回 200MB 并告警
    （不把「读不到基线」当合格）；实测常驻 170MB、并发高峰 532MB ⇒ 判红，**这是真实质量差距**；
  - `first output produced` —— 依赖引擎真实产出任务，属环境性时序（脚本自带 3 分钟等待与重试）。
  ④ 新增 `tests/check_doc_refs.ps1` 把这类「文档点名但入口不存在」上闸：59 份活文档、73 处
  `tests/*.ps1` 引用必须可解析；若已丢失但**就地标注「已丢失/从未入库」并给替代入口**，
  视为已如实披露、不判红。历史快照类文档（回执/事故/定损等）显式排除 —— 它们是当时的事实记录。
  实测：恢复两个脚本后缺失引用从 3 条降到 1 条（`_diag_roundtrip.ps1` 全盘与备份均无、
  `git log --all` 无记录 ⇒ 确已丢失，已在文档就地标注并指向替代入口）。
  变异验证：移走 `e2e_soak.ps1` ⇒ 判红点名 2 处；抹掉「已丢失」披露标记 ⇒ 判红（豁免可证伪）。
- **1920+ 宽屏取景框刻度被稀释成看不见（2026-10-23 CDP 逐档实测）**：
  `@media (min-width:1600px)` 此前只调后台页（content/pagebar/topbar/kpi），**完全没管首页取景框**。
  CDP 逐档读 `::after` 计算样式：1440 / 1920 / 2560 三档读到**完全相同**的刻度色
  （alpha `.5`/`.35`）、线宽（`1.5px`）、间距（`46px`）—— 而 2560 画布面积是 1440 的 3.2 倍，
  刻度被稀释掉，「仪器感」在大屏上消失。
  修：宽屏档加粗线宽 1.5→2px、提亮刻度 rgba(130,200,255,.5/.35) 到 rgba(150,215,255,.72/.5)、
  拉大刻度尺高度 7/5→9/7px、辉光 .55→.7、opacity .85→.92。
  **实测复验**：1440 保持原值（130,200,255,.5 / 1.5px / 7·5px / .85），
  1920 与 2560 生效增强值（150,215,255,.72 / 2px / 9·7px / .92）。
  新增 `tick_scale_contract_test.go` 两条判据（宽屏必须调整且六项增强齐备 / 基础档必须保持
  低亮度 —— 反向保险：大屏提亮是因「距离远画布大」，小屏若提亮会变成噪点抢主体）。
  变异 K1 撤掉宽屏增强 → CAUGHT、K2 基础档被提亮 → CAUGHT、K3 线宽未加粗 → CAUGHT。

### Changed
- **撤销一条经实测证伪的审美待办（同轮）**：此前记录「治理 hero 窄屏字距需调整」。
  CDP 用 `Range.getBoundingClientRect()` 读**真实文字宽度**（而非元素盒宽，后者被块级撑满会误读）：
  390/480/594 三档下「红区」文字宽仅 **71/87/81px（占父容器 15-20%）**、`letterSpacing` 比值
  恒为 `.01`、零溢出。**该条不是缺陷，撤销待办。**
  （教训：判断字号/字距是否过大必须读文字盒宽而非元素盒宽。）
### Changed
- **死路检测 + 跨轮回归扫描（2026-10-23，验证型工作，无代码改动）**：
  ① **死路检测**：7 个页面逐一枚举「渲染完成后的可选动作 × 卡住状态出口」——首页/总览/队列/
  产出/模板/设置/模型池/治理台全部有出口，**无死路**。队列「详情」（r25 修）、产出「导出」、
  设置「重试/停止引擎」、模型池全生命周期、治理台「立即采样」均可达。
  **限流冷却期「无手动解除」经复核判定为正确设计**：backoff 已成熟（300→600→1200→1800
  封顶、冷却期内累加不清零、长期恢复重新起算，`backoff_test.go` 全覆盖），且上游限流是
  **通道级**（换模型无效），手动强制解除只会立即再次 429。自动退避+到点恢复是正确的默认。
  ② **跨轮回归扫描全绿**：gov **PASS 28 / FAIL 0**、web（含全部契约判据）**PASS 116 / FAIL 0**、
  全量 `go test ./...` **0 FAIL** ⇒ 27 轮累计修复无回退，每轮新增判据确实在起防护作用。
  **本轮两处「差点误判」**（第 10、11 次探针问题）：探针限定 `.tabpane.active` 内找按钮而
  漏掉顶栏 ⇒ 误读「总览页零按钮」；测试统计用宽松 `-match 'FAIL'` 命中 `=== RUN` 等行 ⇒
  误报「gov 3 FAIL」（实际 `--- FAIL:` 为 0）。教训：测试统计必须匹配精确前缀 `--- FAIL:`。
### Fixed
- **报告待办状态滞后误导下游：两份报告的里程碑/待办表已过期，且无人校验（2026-10-23 实测）**：
  本目标明确「报告是执行入口」（思考模型把报告与建议放指定地点、由 agent 落地），
  但实测两份在册报告都出现「已完成项仍标 ⏳/待办」，下游 agent 会把它们当未做而重做：
  - `docs/benchmark/WorkBench-开源对标报告.md` §6 里程碑：M2/M3/M4 三条全过期 ——
    实测 M2 `dist/SHA256SUMS` 存在且 ci.yml 已独立读回校验；M3 `internal/logging` /
    `executor/hooks.go` / `web/metrics_prom.go` 均在位；M4 CI 最近结论 success、
    CHANGELOG 与 ARCHITECTURE 均在位；
  - `docs/四项工程改造交付报告-2026-10-22.md` §8 待办：`SHA256SUMS 漂移` 与
    `性能趋势历史` 两条均已由后续提交落地（前者 ci.yml 的 `Verify artifact` 含独立读回比对，
    后者 `Upload perf trend` upload `perf-trend/perf-verdict.jsonl`、`retention-days: 90`、
    按 run_id 分件），状态栏却仍是 ⏳。
  处置：① 按实测证据逐条改为 ✅ 并注明 CI 落点步骤与判据；
  ② 新增 `tests/check_report_claims.ps1` 把可证伪的窄面上闸：**⏳ 行点名的具名制品
  若已出现在 ci.yml ⇒ 状态栏过期，判红**。刻意不做「建议是否落地」的模糊语义校验 ——
  那类判据只会产生噪音与假红，而假红会让门禁被撤（本仓在册教训）。
  故 ⏳ 行若点名外部面（如「向上游提 issue」），显式不在本判据范围。
  首跑即抓出首轮报告的 M2；接入 `repo-gates`（第 10 个门禁）。
  变异验证：把已完成行改回 ⏳ ⇒ 判红并精确点名文件行号；恢复 ⇒ PASS；
  外部面待办（上游 issue）不被误判（当前 4 条 ⏳ 行含它且门禁 PASS）。
  干净 clone 复核：门禁 rc=0（证明不依赖工作区未跟踪件）。
- **治理台「任务失败率 79%」把历史累计当当前状态，误导决策（2026-10-23 风险预警走查）**：
  `FailureRate()` 的口径是 `累计失败 / (累计完成 + 累计失败)`，**无时间窗口**。真实现场：
  近 **1 小时 0 失败**、近 24 小时 287 条历史失败，累计比率恒 **79%**；而治理台文案写
  「当前 79%」并建议「立即执行自适应调整（降负载或扩容），24 小时内复测」—— 把早已恢复的
  历史故障当成当前恶化，会导致**因陈旧数据去扩容**的错误决策。
  修（不改数值语义，只补口径透明度）：① 指标名改为「任务失败率（**累计**）」；
  ② `Eval` 新增 `Note` 字段承载口径提醒：「全时段累计口径：历史失败会长期顶高。判断当前
  是否恶化请结合近期失败数。」并在面板指标行渲染；③ 处置文案由「（当前 79.0%）」改为
  「（**累计** 79.0%；该值为全时段累计…判断当前需结合近期失败数）」。
  **实测复验（生产 API）**：`name=任务失败率（累计）`、`note` 完整下发、处置文案已含「累计」口径。
  新增 `failure_scope_test.go` 两条判据（指标名必须标注累计 + Note 必须说明全时段并指引近期 /
  处置文案不得只写「当前」）；变异 F1 去掉「累计」→ CAUGHT、F2 清空 Note → CAUGHT、
  F3 处置文案退回「当前 79%」→ CAUGHT。
### Fixed
- **队列页 8 个「详情」按钮点了毫无反应（2026-10-23 全链路走查）**：
  `WB.openTask` 里是「拿不到 `#taskDetail` 就静默 `return`」，而该容器此前**只在产出页渲染**，
  队列页没有 ⇒ 队列页那 8 个「详情」按钮点了没有任何反应、也没有任何提示。
  CDP 实测：点击后 `taskDetail=null`、`paneLen` 完全不变（6322）、**零运行时错误** ——
  这是最坏的一类故障：按钮在、点了没反应、用户和开发者都拿不到线索。
  修：① 队列页渲染 `#taskDetail` 容器；② `openTask` 的静默 `return` 改成 `toast` 明示
  （容器缺失属渲染契约违背，必须暴露）；③ 重渲染时保留已展开的详情（队列页是 SSE 目标，
  否则点开的详情会在下一次事件到达时被抹掉——与滚动丢失同型）。
  **实测复验：点击后 73 字符真实内容 + `.doc` 面板可见；SSE 5 秒后详情仍保留（kept=true）。**
  新增 `open_task_contract_test.go` 两条判据（两页都必须挂容器且缺失分支不得裸 return /
  重渲染必须真正读取旧内容）；变异 Y1 撤掉队容器 → CAUGHT、Y2 恢复静默 return → CAUGHT、
  Y3 置空 keepDetail → **首轮 MISSED**（判据查标识符存在，改空串后标识符仍在），
  收紧为断言「实际取值表达式」+「定位旧节点」后 CAUGHT。
### Changed
- **长程资源审计：证明「无内存泄漏」（2026-10-23，验证型工作，无代码改动）**：
  延续规模维度做「长时间运行」验证，两个测量对象结论均为**良性**：
  ① **前端 JS 堆 = 饱和型引擎缓存，不是泄漏**：严格同页签往返（overview ↔ outputs），
  每 20 次切换强制 GC 后采样 —— 0.95 MB → 0.65 → 0.65 → **8.31 MB** → 8.29 → 8.29 → 8.29
  （60→160 次切换**零增长**）。平台化即为一次性 Blink/V8 缓存预热。
  应用侧静态审查佐证：全仓**零 `createElement`**、零 `appendChild`、零 `innerHTML +=`；
  5 个 render 函数内零 addEventListener / 零 setInterval / 零 MutationObserver；
  全局状态全部覆盖式赋值；监听器仅初始化注册 5 次。
  ② **后端进程 = 瞬时峰值，GC 后回落**：10 分钟 × 40 点采样，19:09–19:16 平稳 156–244 MB，
  19:16:40 跳升 416 → 658 MB（同期 handles 468→290、threads 39→32，属大批量对象调用而非句柄
  泄漏），之后回落 **193 MB**。Go GC 懒触发导致，非单调增长。
  **过程教训（写进记忆 07-part139）**：「内存涨了」≠「泄漏」。单点对比会把引擎缓存预热误判
  成泄漏（本轮一度准备报缺陷）。判据须为「多次强制 GC 后**至少 3 个连续等间隔采样点**是否
  平台化」；后端须看**回落值**而非峰值。
  顺带确认一处正确行为（非缺陷）：重启后 `queued=8 / running=0` 持续 40 秒，查
  `/api/status` 得 `rateLimit.limited=true, remainingSeconds=1521`，reason 明确写明「上游限流
  HTTP 429…已整池冷却…自动恢复」—— 系统在正确等待冷却，且 UI 有完整解释（停摆自证生效）。
### Fixed
- **评估器窗口查询 API 零覆盖：多样性门禁的输入无判据（2026-10-23 补）**：
  `internal/evaluate` 的 `DomainShare` / `RecentTitles` / `maxChainDepth` 三函数 0% 覆盖，
  而它们都直接决定**门禁放不放行**：
  ① `DomainShare` 是多样性门禁（`DiversityCap`）的输入。算错的后果是双向的：
  分母用错 ⇒ 占比恒 >1 或恒为 0 ⇒ 门禁要么形同虚设（内容单一化），
  要么过度拦截（候选不入队 → 每 3 秒重试的热循环）；空窗口若返回 NaN（0/0），
  **所有比较都是 false** —— 既不拦也不报，静默失效最难查。
  ② `RecentTitles` 是「最近窗口」的对外读数，n 的越界处理写反会返回空/全量，
  让调用方误判「最近没产出」或「重复泛滥」。
  ③ `maxChainDepth` 是模板链深兜底（未配时 5）。兜底错了：过浅断链、过深刷量。
  新增 `window_contract_test.go` 六条判据（覆盖 65.6% → **73.7%**）：
  精确占比（2/4、1/4、0/4）、空窗口必须返回 0 而非 NaN、
  **滚动窗口一致性**（maxKeep=2 时旧领域必须滚出占比统计）、
  RecentTitles 四种 n 语义（全量/最近两条/超长/非正）+ 顺序契约（最新在末尾）、
  空窗口安全、maxChainDepth 兜底 5（含负链深也兜底、显式值必须被尊重）。
  变异验证三条全部 CAUGHT：MUT-A 分母写死 1.0 ⇒ 两条判据判红（实测抓到 `DomainShare=2`）；
  MUT-B 窗口多算一条 ⇒ 判红（实测抓到多返回「第二篇」）；MUT-C 兜底改 1 ⇒ 判红。
- **store 核心持久化 API 零覆盖：路径穿越校验与写时复制语义无判据（2026-10-23 补）**：
  逐函数扫 `internal/store` 发现 12 个函数 0% 覆盖，其中三条是**引擎与 Web 层直接调用**的核心 API：
  ① `SaveTaskArtifact` 的文件名合法性校验 —— 若放行 `..\`、`a:b`、`a/b`，产物可写到任务目录之外
  （路径穿越 / NTFS 备用数据流写入），属安全面而非功能面；
  ② `Update` 的**写时复制**语义 —— 若退回原地写 `*t = cp`，与「持有旧指针的读方」形成 data race
  （2026-10-19 容器内 `-race` 实测抓到过，且该修复曾被整文件覆盖丢掉）；
  ③ `UpsertChain`/`GetChain` 的副本语义 —— 若返回内部指针，调用方改它即污染台账。
  新增 `store_api_test.go` 六条判据（覆盖 62.6% → **77.4%**）：非法名十种形态全部拒绝且
  确认任务目录外无落盘、合法名读写保真且位置契约不变、缺失产物必须报错而非返回空内容、
  写时复制不污染旧指针、链台账双向副本语义（改返回值/改入参均不得影响台账）、
  `Audit` 自动补时间戳且真落盘、`SaveState` 刷新 `UpdatedAt` 且重开可读回。
  **关键返工（判据载体选错，MUT-A 首轮 MISSED）**：第一版用 `Get` 拿「旧指针」验证写时复制 ——
  测不出来，因为 `Get`/`List`/`Transition` 都返回副本，原地写对内无法观测。
  真正可观测的载体是 **`Create` 的入参指针**：`Create` 直接把调用方的 `t` 存进 map
  （`s.tasks[t.ID] = t`），调用方手里的指针**就是内部指针**。换载体后同一变异立即判红
  （报「调用方/旧内部指针被就地改写」）。这与本仓在册的「判据没碰到被测代码」同族，
  但更隐蔽：**判据逻辑对、断言也对，只有观测点选错了**。
  变异验证：MUT-B 去掉产物文件名校验 → 判红（点名 `../escape.md`）；
  MUT-A（换载体后）Update 退回原地写 → 判红。
- **无头环境判定 `headlessEnv()` 0% 覆盖：「绝不弹窗」的硬保证没判据（2026-10-23 补）**：
  `cmd/workbench/headless.go` 的 `headlessEnv()` 此前 **0% 覆盖**，而它是「绝不弹窗」的
  **环境层硬保证**。双层语义：代码层 `shouldOpenPanel(silent)` 声明「要不要弹」，
  环境层 `headlessEnv()` 声明「准不准弹」，**两者都放行才真的弹**，环境层覆盖代码层。
  它判错的两种后果都是本仓真实事故：
  漏判（该 true 却 false）⇒ CI/计划任务里真的往桌面推 Edge --app 窗口，把无人值守机器搞挂
  （2026-10-19 老大反复反馈「一直在跳这个，可不可以不要」）；
  误判（该 false 却 true）⇒ 用户双击快捷方式「点了打不开」。
  新增 6 条判据：marker 四种真值 × 大小写/空白不敏感、明确关闭值（0/false/no/off/空/disabled）
  不得判无头、**CI 任意非空即无头**（各平台取值不一：true/1/yes/构建号）、
  CI 空值不得触发、marker 与 CI 是「任一成立」不是「都成立」、两源都关必须允许弹面板。
  同批补 `selfCheckWord`（把 bool 翻成人话的唯一位置 —— 翻错就是总验收报告虚报「全绿」）
  与 `keysOf`（报告漏键 = 漏项）两条小判据。
  变异验证（两条）：MUT-A 忽略 CI 变量 ⇒ 判红；MUT-B **恒无头** ⇒ **三条判据同时判红**
  （覆盖「显式关闭仍不弹」「CI 空值仍不弹」「两源都关仍不弹」三个方向）。
- **托盘完整性错误类型 0% 覆盖：`labelError` 是「静默没图标」的唯一诊断通道（2026-10-23 补）**：
  逐函数扫 `internal/tray` 发现 `labelError.Error()` / `Unwrap()` **全 0% 覆盖**，
  `raiseToMedium` 的空目录分支、`integrityName` 的未知 RID 分支也未测。
  而 `labelError` 承载的是现场最难排查的形态：安装目录被打上 Low 完整性标签后，
  Explorer 拒收 `NIM_ADD` ⇒ **托盘图标不出现且没有任何报错**（2026-10-20 沙箱残留事故）。
  若 `Error()` 丢了目录或 icacls 输出，或 `Unwrap()` 断了，现场就只能猜。
  新增 `integrity_error_contract_test.go` 六条判据（覆盖 17.6% → **21.3%**）：
  `Error()` 必须同时含目录 + icacls 原始输出 + 「icacls」字样；无输出时**不得渲染空括号 `()`**
  （会被误读为「输出被吞」）；`Unwrap` 必须可 `errors.Is` 穿透（按类型分流而不是字符串匹配）；
  `raiseToMedium` 空/纯空白目录直接 `os.ErrInvalid` **不得起子进程**（无意义的 icacls 调用会被安全软件盯上）；
  `integrityName` 五个已知级别精确映射 + 未知 RID 必须落 `Other`（冒充已知级别会把排查引向错误方向）；
  `isLowIntegrity` 只认 Low（误判会触发无谓的特权 SACL 修改，漏判则自愈永不触发）。
  变异验证三条全部 CAUGHT：MUT-A `Error()` 丢弃 icacls 输出 → 判红；
  MUT-B `isLowIntegrity` 恒 true → 判红；MUT-C `Unwrap` 返回 nil → 判红。
  取舍说明：`Run` / `wndProc` / `showMenu` 等仍为 0% —— 它们依赖真实桌面会话（需要 Explorer 与
  HWND），单测只能得到假绿。这与本仓对 `tray` 包阈值的既有说明一致（长期 16%），非本轮遗留缺陷。
- **执行钩子（P1-2 公开扩展点）零测试：三条契约无判据守着（2026-10-22 补）**：
  逐函数覆盖率扫描发现 `internal/executor/hooks.go` **全部函数 0% 覆盖** ——
  `NewHooks` / `AddBefore` / `AddAfter` / `RunBefore` / `RunAfter` / `AttachHooks` 无一有判据。
  而 hooks 是 P1-2 交付的**公开扩展点**（对标 river 的 HookWorkBegin/HookWorkEnd 与
  asynq 的 middleware），引擎靠它插入日志/指标/审计等横切逻辑。三条契约一旦破，故障形态是
  **横切逻辑静默不生效**或**钩子把任务搞死**，都不会让编译或其它测试失败：
  ① 前置钩子返回错误 ⇒ 本次不执行（不能照样跑）；
  ② 后置钩子必须**必然**被调用（含失败路径），否则记账缺口 = 观测面破洞；
  ③ 任何钩子 panic 都被 recover，绝不影响任务本身。
  新增 `hooks_contract_test.go` 九条判据：注册顺序、动态追加、首个错误即停且可 `errors.Is` 溯源、
  前置 panic 转错误、**后置单个 panic 不影响其余（三条都执行）**、失败路径如实传 res/err、
  nil 集合与 nil 函数项均安全、`SetHooks`/`AttachHooks` 两个装配入口不分叉。
  变异验证：MUT-A 去掉后置 recover → **panic 逃逸判红**；MUT-B 前置钩子忽略错误 →
  **两条判据同时判红**。executor 覆盖 52.9% → **53.9%**。
- **SSE 每秒 10 次全量重渲染 → 滚动被反复弹回顶部（2026-10-23 并发时序走查）**：
  6 个任务并跑现场实测 SSE 峰值约 **10 条/秒**，而 `onmessage` 每条都对 overview/queue
  做全量 `innerHTML` 重渲染。两个真问题：
  ① **滚动位置被弹回顶部**：用户把「任务」页表格滚到 100px 后，重渲染把整个
  `.table-scroll` 元素换掉（实测 `elemStillInDom=false`），新节点 `scrollTop` 归 0 ——
  30 次采样（200ms 间隔）**全部为 0**，等于列表每秒被弹回顶部十次，根本没法看。
  ② **事件频率 × 全量重建的浪费**：虽然当前内容量小、长任务仍为 0（CLS 也是 0），
  但这是平方级开销，内容一多必掉帧。
  修：渲染前快照滚动位置、渲染后恢复；SSE 重渲染合并到 **400ms 窗口**（窗口内多条事件
  只渲染最后一帧 —— 数据本来就是全量拉取，丢中间帧不丢信息）。
  **修复中踩到两个必须同时满足的点，缺一无效（第一版修复完全无效即此因）**：
  · 不能按 `scrollHeight` 手动夹紧 —— 刚换完 innerHTML 时布局未完成、`scrollHeight` 偏小，
    `maxTop` 算成 0，`Math.min(top, 0)` 直接把滚动位置夹成 0；
  · 必须**双 rAF** 后再赋值 —— 第一帧完成布局，第二帧赋值才不被丢弃。
  **修后实测：30 次采样 `zeroCount=0 / nonZeroCount=30`，最终 scrollTop 恒 150（保持）。**
  新增 `sse_render_contract_test.go` 两条判据（SSE 必须节流 / 渲染必须保住滚动且不得手动
  clamp、必须双 rAF）；变异 X1 恢复手动 clamp → CAUGHT、X2 改单帧 rAF → CAUGHT、
  X3 删节流 → CAUGHT。
### Fixed
- **单实例锁零测试：「后台实例爆炸」的防线本身没被判据守着（2026-10-22 补）**：
  覆盖率扫描发现 `internal/single` 只有 **16.2%**（阈值 16，**贴着门禁线**），且这 16.2% 全来自
  `ProbeExactPort` 的端口判据 —— **`Acquire` / `Release`（本包唯一的互斥正确性代码）零测试**。
  而它是 2026-10-20「后台实例爆炸」事故（实测 24 个 workbench.exe 并存）的直接防线：
  锁失效的后果是多个 scheduler 抢同一份 queue.json —— 任务被重复执行、账本互相覆盖。
  这类缺陷不会编译失败、不会测试失败，只在现场表现为「怎么后台显示有这么多在运行」。
  新增 `acquire_test.go` 九条判据（覆盖 16.2% → **89.2%**）：
  锁文件 payload 契约（pid/at，故障排查第一手证据）、二次获取必须被拒、
  释放后可重取、释放只清内容不删文件（防 TOCTOU 双持锁）、Release 幂等且 nil 安全、
  父目录自建（首次安装不能因缺目录起不来）、并发 8 抢恰好 1 成功、
  **锁定期间新句柄加锁必须失败**（排他性正向证明）、锁文件保留。
  变异验证（三条，其中一条首轮 MISSED 后加严）：
  MUT-A 破坏「已运行」判定（running 恒 false）→ 两条判据同时判红；
  MUT-C 让 LockFile 永不排他 → **三条判据同时判红**（含新增排他性判据）；
  **MUT-B 删掉 Release 里的 `unlockFile` 首轮未被捕获** —— 因为 `Close()` 也会释放文件锁，
  原判据（「Release 后能重取」）根本没碰到 unlock 行。这是本仓在册的
  「判据没碰到被测代码」失效形态（与看门狗判据第一版同源）。
  处置：改用**排他性正向判据**（锁定期间第二个句柄也拿不到锁），它直接证明锁真的排他，
  比「Release 后能重取」更强（后者可被 Close 掩盖）；并在测试注释里记录
  「unlock+Close 双释放是刻意冗余，谁先谁后都能放开」这一事实，避免后人误删。
- **gofmt 门禁口径校准：按 HEAD 判，不按工作区（2026-10-22 四轮实测，解「本机绿 CI 红」死结）**：
  这个门禁本轮反复红绿，根因不是代码而是**判定基准没想清**。实测三面事实：
  ① CI 判的是 HEAD；② 本机工作区常被并行会话改得比 HEAD 更新（甚至本地已合规）；
  ③ 两者可同时为真 ⇒ 同一门禁在两个环境给出相反结论。踩到的具体死结：
  - **本机绿 CI 红**：我按「工作区已合规」删掉 33 条债务，但那些文件在 HEAD 仍漂移
    （并行会话本地格式化过却未提交），于是 CI 判红；
  - **本机红 CI 绿**：`shortcut_windows_test.go` 工作区被在途改动弄漂移而 HEAD 合规，
    门禁把它当违规判红，CI 却是绿的。
  修：**必要性、豁免匹配、违规判定全部按 HEAD 版本复算**（CI 判什么就判什么）；
  工作区漂移单独归类为「并行在途件，不计 rc」并打印。三面语义：
  `exempt`（登记且 HEAD 仍漂移）/ `fatal`（HEAD 漂移未登记，计入 rc）/ `inflightOnly`（仅工作区漂移）。
  实施中抓到第 4 个 PowerShell 内容改写坑（本轮同族四连）：`git show | -join` 会**丢最后一个换行**
  （实测 len 2659 vs 2660），gofmt 因此误判「缺结尾换行」=> 假漂移。改用 `cmd /c ... > file` 写原始字节。
  另修正一处路径基准：`$Root` 是 `<repo>/workbench`、`.git` 在仓库根，
  `Test-Path (Join-Path $Root '.git')` 恒 false ⇒ HEAD 复核整段被静默跳过（已改 `rev-parse --show-toplevel`）。
  三向验证（干净 clone，等价 CI）：A 基线 → PASS 带 34 条豁免；B 删一条 live 豁免 → 判红点名该文件；
  C 登记一条 HEAD 已合规的文件 → 判红点名「失效」。
- **极端内容溢出：无空格长串把容器撑破、整页横向溢出近 1000px（2026-10-22 CDP 注入实测）**：
  产出里常出现 URL / 哈希 / 代码标识符这类**无空格长串**。注入构造值实测（容器 900px）：
  · `.doc` 产出面板里 600 个无空格 ASCII → clientWidth 898 但 scrollWidth **5831**（溢出 6.5 倍）
  · `pre` 代码块里 800 个无换行字符 → scrollWidth **5657**
  · 表格里 300 字单元格 → 表格被撑到 **2334px**（容器只有 900px）
  · 整页横向溢出 `docOverflowX = 979px`
  中文靠 CJK 原生断行不受影响（同用例中文正常），只有 ASCII 长串需要显式规则。
  修：`.doc` / `pre,.logbox` / `th,td` 三处补 `overflow-wrap:anywhere;word-break:break-word`。
  **修复中踩到一个反直觉点**：只给 `<pre>` 加 break 规则**完全无效** —— pre 默认
  `white-space:pre` 根本不换行，复测 scrollWidth 仍是 5657 分毫未动；必须先 `pre-wrap`
  才能让断行规则起作用（第一版修复失败即此因）。
  **修后实测：898 / 898 / 900，docOverflowX = -10（零溢出）。**
  新增 `overflow_wrap_contract_test.go` 三条判据（三处容器必须能断长串 / pre 必须 pre-wrap
  且不得退化成 pre-line-nowrap / html-body 上不得出现 overflow-x:hidden 掩盖问题）；
  变异 E1 删 pre-wrap → CAUGHT、E2 删单元格断行 → CAUGHT、E3 用 overflow-x:hidden 掩盖 → CAUGHT。
### Fixed
- **豁免/债务清单会腐烂：35 条 gofmt 债务里 33 条已失效，门禁仍在册遮蔽（2026-10-22 实测）**：
  `tests/gofmt-debt.txt` 建立时有 35 条（并行会话在途件）。那些文件后来陆续被入库并格式化，
  但**债务条目没人清** —— 门禁看上去一直「PASS 带 35 条豁免」，实际只有 2 条仍 live。
  危害不是噪声，而是**失明**：在册豁免会让该文件**将来再次漂移**时静默放过（豁免按路径匹配，
  不区分「修复前」与「修复后」），而门禁外表仍是绿的 —— 比缺判据更危险。
  实证清理：逐条用「LF 归一化后 gofmt 是否仍判红」复算 → 35 条中 **33 条 stale、2 条 live**；
  债务清单收敛到 2 条（shortcut 两件，仍是并行在途件，git status M 且工作区未格式化）。
  机制加固（两类清理都上闸，可证伪）：
  ① `check_gofmt.ps1`：登记但已不漂移 ⇒ **判红并点名要求删除**。
  ② `check_spawn_silent.ps1`：登记的已知缺陷若该文件已无违规 ⇒ 同样判红。
     但加了 **HEAD 感知**（实测取舍）：本机工作区常已被并行会话修好而 HEAD 未修，
     此时按工作区判红会形成死结（本地永红 + 删豁免则 CI 立刻红）。故用 HEAD 内容复核一次：
     仍有违规 ⇒ 只提示「豁免暂不可删」不判红；也无违规 ⇒ 真腐烂，判红。
  变异验证：MUT-A 追加一条必失效豁免 → 判红点名 queue.go；MUT-B 删掉一条 live 豁免 →
  判红点名对应文件；spawn 三向验证（HEAD 缺陷版 PASS 带 KNOWN / 新违规判红 / 豁免与现实不符判红）。
  实施中修掉自己引入的 3 个 bug（记此防重犯）：`$Root` 是 `<repo>/workbench` 而 `.git` 在仓库根
  ⇒ HEAD 复核整段被静默跳过；`git show 'HEAD:workbench/...'` 需先 `rev-parse --show-toplevel`；
  `-replace [string][char]92` 被当成正则模式（InvalidRegularExpression），改用 `.Replace()`。
- **数据规模触发：产出页显示 0 份产出 / 队列页今日产出恒 0（2026-10-22 真实规模实测）**：
  当前真实库存 **33,728 个任务**（其中 done 86 / degraded 6 / rejected 33,187），
  两个只在数据量长大后才会出现的功能性缺陷：
  ① **产出页空白**：原写法 `/api/tasks?limit=3000` 取最新 3000 条再前端 `filter(state==='done')`。
  最新窗口被 2,919 条 rejected 挤满 ⇒ 窗口内 done=0 ⇒ 页面显示「还没有产出」，
  而库存实际有 86 份完成品（81 份通过验收）。用户会以为系统坏了。
  修：改用后端本就支持的 `?state=` 过滤（实测 done 86 条 54ms，比拉 3000 条 15ms 更准更快）。
  **实测复验：产出页 0 → 86 份产出 / 81 份通过验收。**
  ② **队列页「今日产出」恒 0**：该值读 `cache.status.stats.todayDone`，而 `cache.status` 只在
  `renderOverview()` 里赋值 —— 用户直接从 `#queue` 进页面时 overview 从未渲染，
  `cache.status` 永远 undefined。CDP 实测进队列页后 1.2/2.5/4.0/6.0s 四次采样
  `cacheStatus` 均为 false，而 API 实际 `todayDone=8`。修：队列页自己取 `/api/status` 并写入
  cache。**实测复验：今日产出 0 → 8。**
  新增 `scale_contract_test.go` 两条判据（产出页必须按 state 取数且不得退回裸 limit 写法 /
  队列页必须自己取 status 且写进 cache）；变异 S1 退回裸 limit → CAUGHT、
  S2 不取 status → CAUGHT、S3 取了但不写 cache → CAUGHT。
  同类修复：队列页 active 列表同型缺陷（原拉 limit=600 再前端过滤），一并改为按状态取。

> ✅ **已发布 1.6.4**（SHA256 `D86C98AC…4675`，官方链 7/7，frontend-gate 23/23）。
> 过程记录：首次发布尝试被 3/6 步 gofmt 门禁阻断，点名 6 个**非本轮改动**的文件
>（`assets_gov.go` / `export.go` / `pool_page.go` / `autostart_windows.go` /
> `tray_window_style_test.go` / `deadlock_test.go`）—— 已用 `git cat-file blob HEAD:<path>`
> 证明其 HEAD blob 本身就 gofmt 脏，属并行会话在途件；按「不碰他人在途件」纪律不代跑 gofmt，
> 等其落定（实测约 20 分钟后门禁自清 PASS）再发布。教训：**发布被阻断时先证明归属再等，不要代改**。
### Fixed
- **文案与交互语义跨页一致性（2026-10-22 机器化审计，164 条用户可见文案）**：
  从源码提取全部按钮/提示/空态文案做规范检查，发现两类真问题：
  ① **同一动作两种措辞**：保存开机自启的按钮写作「应用」，而同页其它保存动作一律「保存」——
  用户得猜「应用」是不是和执行类操作一样危险。统一为「保存」。
  ② **同一语义的空态两种写法**：管理面板「没有失败任务」有两处（折叠块 + 详细块），
  一处「最近没有失败任务。」、一处「没有失败任务 —— 保持这样最好。」；翻到两处会以为
  说的是两码事。统一为后者。另把删除模板后的含糊提示「已处理」改成「已删除该模板」。
  **审计结论（好的部分，已固化成判据防回退）**：57 个按钮 0 带句末标点、23 处失败提示
  100% 采用「XX失败：<原因>」形态、无英文成功提示（OK/Success/Done）、无模糊错误话。
  新增 `copy_consistency_test.go` 三条判据（按钮是动作词且无句末标点 / 失败提示必须点名 /
  同语义空态只能一种写法）；变异 W1「保存」退回「应用」→ CAUGHT、W2 空态退回两种写法 →
  CAUGHT、W3 toast 改含糊话 → CAUGHT。
### Fixed
- **跨页一致性审计：同一产品两种卡片语言 / 两种内容顶距（2026-10-22 机器化审计）**：
  以 CDP 逐页读 computedStyle 对四页做机器化比对（13 项度量，差异 11 项里多数是内容长度
  的必然结果，真不一致 2 项）：
  ① **卡片语言分裂**：治理台的 `.gov-card` 用圆角（`--radius` 12px）+ margin 14px + 实边框，
  而管理/池的 `.card` 用 HUD **切角**（`clip-path` + radius 0）+ margin 18px + inset 描边。
  设计体系头注释写明「HUD 面板的角不是圆的，是斜切的」（`clip-path` 9 处、`--cut` 为主语言），
  故 `.gov-card` 是异类 ⇒ 对齐 `.card`（切角/radius 0/18px 节奏/inset 描边）；
  悬停改用内描边换色（切角卡没有可描边的 border，会被 clip-path 切掉）。
  ② **内容区顶距 26px vs 14px**：管理面板 DOM 是 `topbar-shell → tabs → content`，
  而 `.topbar-shell + .content` 是**相邻**兄弟选择器、匹配不到（中间隔着 `.tabs`），
  于是有页签的页回落 26px、无页签的页 14px。补 `.tabs + .content` + `.topbar-shell ~ .content`
  双保险。
  复测：三页 `paddingTop` 全部 **14px**（改前 admin 26px）；三页 `borderRadius=0px`、
  `marginBottom=18px`、`borderTopColor=transparent` **完全一致**。
  新增 `cross_page_consistency_test.go` 两条判据（两卡基座度量同源 / 内容顶距选择器齐备）；
  变异 C1 gov-card 退回圆角 → CAUGHT、C2 删 `.tabs + .content` → CAUGHT。
### Fixed
- **check_snapshot_build 在 CI 上因 tar.exe 文件名编码失真而误判（2026-10-22 实测定位）**：
  该门禁在 CI 持续判红，报 `TestRepoRootHasRequiredAnchors: repoRoot(...snap) 下读不到方案文档`。
  逐层排掉后拿到决定性证据：`repoRoot()` **正确**返回了快照根，但 `snap\docs\自主运行方案.md`
  在磁盘上不存在 —— 文件不是没提交，是**解包时名字坏了**。
  根因：门禁用 `tar -xf` 展开 `git archive`，而 CI runner 的 tar.exe(bsdtar) 按**系统代码页**
  （英文 Windows = 437）解码 tar 头里的 UTF-8 文件名，中文名落盘成乱码。本机（代码页 936）
  恰好正确，故本机全绿 —— 典型的「环境相关、本机不可复现」。
  这比「判据过严」严重：门禁的全部价值是「快照里到底有什么」，若解包本身失真，判据不可信。
  修：改用 `git --git-dir --work-tree checkout-index -a -f` 展开快照 —— 语义等价（同取 HEAD），
  但文件名由 git 按字节处理，**不经过 tar.exe 的编码层**，与平台代码页无关。
  `git archive` 保留仅用于「快照大小」这一行体积读数。
  验证：本机 rc=0；同时对比两种展开方式在中英文锚点上的落盘保真度。
- **repo-gates 第二、三轮判红（2026-10-22 连续实测，红因逐个下移）**：
  门禁挂上 CI 后按预期开始工作，每轮红点都是真问题，逐个修到绿：
  ① **PS5.1 ParserError**（Unexpected token / Missing closing）：25 个含中文的 .ps1 缺 UTF-8 BOM。
    不只是我改过的 3 个 —— 构建链核心 `setup/build.ps1`、CI 在跑的 `check_perf.ps1` 都缺。
    修：全部只前置 EF BB BF；新增 `check_ps1_encoding.ps1` 上闸（含非 ASCII 才要求 BOM）。
  ② **双重 BOM**（我自己两次犯）：在已有 BOM 的文件上再前置一次 ⇒ PS5.1 把第二个 BOM 当命令字符，
    报 `assignment expression is not valid`。是「补 BOM」这类修复的高发失误，已扩展编码门禁覆盖
    （连出两个 EF BB BF 即判红），并当场抓出 `check_all.ps1` 的双重 BOM 修掉。
  ③ **check_snapshot_build 在 CI 上 `TestRepoRootHasRequiredAnchors` 判红**：
    `repoRoot()` 靠目录探测找仓库根，在本机各种形态（8.3 短名 / 纯 ASCII TEMP / PS5.1 解包 /
    非仓库 cwd）全部通过，CI runner 上返回空串 —— 环境（locale/代码页）相关，本机无法复现。
    处置：快照门禁**已知**根在哪，就显式注入 `repoRoot()` 自带的 `WB_REPO_ROOT` 覆盖位，
    不让被测代码去猜；探测逻辑本身的正确性仍由工作树里的同一用例守护。
  ④ **check_gate_links 连续两轮点名新断链**（它自己抓自己）：先抓到 `check_ps1_encoding.ps1`
    未入库，再抓到 `setup-out-of-box.ps1` 引用的 `fix-runtime-patch.ps1` / `qa-artifacts.ps1` 未入库。
    两者均入库（`.bak-inject-append` 备份件不提交，无引用）。
  实测：PS5.1 下 9 个门禁全 PASS；`check_gofmt` 仍为「PASS 带 34 条显式豁免未清」（并行在途件）。
- **PowerShell 脚本编码：25 个含中文脚本缺 UTF-8 BOM，CI 的 PS5.1 会 ParserError（2026-10-22 两轮实测）**：
  本轮把 8 个门禁挂上 CI 后，repo-gates job 判红，报的是
  `Unexpected token` / `Missing closing ')'`，行号指向 `check_snapshot_build.ps1:42`
  的 `Write-Host ("  仓库: " + $Repo)` —— 本机 PS7 一切正常，CI 的 PS5.1 却把中文按 ANSI 解码拆坏字节。
  这是同一坑的**第二次复现**（第一次是本轮新建 check_gofmt/check_gate_links 时）：
  修好后又踩一次 —— 编辑那三个既有门禁时写回文件把 BOM 剥掉了。
  危害比缺判据更隐蔽：门禁报的是**它自己**的语法错，不是被测缺陷。
  排查：写 `tests/check_ps1_encoding.ps1` 扫全仓，实测 **25 个**含非 ASCII 的 .ps1/.psm1 缺 BOM ——
  不只是我碰过的 3 个，还包括构建链核心 `setup/build.ps1`、以及正在 CI 里跑的 `check_perf.ps1`
  （它侥幸没炸：含中文的字符串不在解析路径上）。属于系统性技术债，不是个别失误。
  处置：① 25 个脚本一律**只前置 EF BB BF**（不重写正文，符合判据自身给的修复指引），
  前置后逐个 `Parser` 语法校验通过（12 个抽查 + 全量）；② 新增编码门禁并接入 CI 的 repo-gates
  与 check_all：判据只针对真实失效面（**含非 ASCII 才要求 BOM**，纯 ASCII 脚本不强求，避免无意义噪声）；
  ③ 实现时踩到本仓在册坑：`Get-ChildItem -Include` 在 `-LiteralPath` 下不生效，
  首版把 4074 个 .md/.json 也判成脚本 —— 改为显式扩展名过滤。
  验证：PS5.1 真实执行 9 个门禁全 PASS；干净 clone 复核后仅剩「新脚本未入库」的预期断链（提交即清零）。
- **门禁判据「存在但没挂受理面」：23 个受跟踪门禁脚本里 CI 只执行 4 个（2026-10-22 实测）**：
  本仓 tests/ 下有 23 个受跟踪 .ps1，CI（.github/workflows/ci.yml）此前只跑
  `check_changelog / check_noninteractive / check_issue_forms / check_perf` 四条。
  其余一条都没被任何自动化执行 —— 包括本轮该建但**从未上闸**的 `check_gofmt`、
  以及 `check_gate_links / check_tray_window / check_spawn_silent / check_snapshot_build`。
  「判据写了不等于判据生效」是本仓在册失效形态，这次是它自己复现。
  处置：新增独立 job `repo-gates`（与 vet/test 分开，失败可归因到具体门禁，不淹在测试输出里），
  逐条执行 8 个纯源码门禁、**逐个判 rc**（反掩盖：任一 rc!=0 即整体失败，不允许聚合洗绿）。

  实施中顺带挖出三个真问题（都当场修掉）：
  ① **门禁硬编码绝对路径** `D:\workdesktop`（check_all / check_tray_window / check_spawn_silent /
     check_snapshot_build 都有）：在干净 clone / CI 里该路径不存在，门禁会检查**另一棵树**。
     实证：我在 worktree 里注入 tray 缺陷做变异测试，门禁仍 PASS —— 因为它读的是主区文件。
     修：默认根一律由 `$PSScriptRoot` 推导（tests/ 的父目录），显式传参仍可覆盖。改正后同一变异
     **立即判红**（`FAIL Run 未调用 hideTrayWindow`）。
  ② **check_spawn_silent 抓到 HEAD 上的真缺陷**：`internal/web/autostart_windows.go` 三处
     `exec.Command("reg", ...)` 缺 `hiddenProcAttr()`，而 reg.exe 是 CONSOLE 子系统程序 ——
     用户每点一次「开机自启」就闪一个黑窗。该修复已在工作区由并行会话完成但未入库。
     处置：门禁加**显式已知缺陷豁免**（逐条打印 KNOWN + 三态措辞「PASS（带 N 条豁免，未清零）」），
     绝不打纯 PASS；变异验证「新增一处未登记违规」→ 判红并点名。
  ③ 豁免初版**静默失效**：违规路径带前导 `/` 而豁免键不带，Contains 匹配不上 → 仍判红。
     修：路径规范化去前导斜杠；这正是「豁免机制自己也要被判据验」的例证。

  验证：干净 HEAD worktree + 完整拷入本轮改动的 8 个门禁 → **8/8 PASS**；本地同样 8/8 PASS。
  变异验证：tray 缺陷 → 判红；新增未登记 spawn 违规 → 判红；恢复 → PASS。
- **WCAG 1.4.4 缩放审计：400% 放大下正文字号反而变小（2026-10-22 实测缺陷）**：
  `@media (max-width:480px){ body{font-size:14px} }` 在 400% 缩放（视口 360px）时触发，
  把正文从 15px 降到 14px。窄视口有两种成因：① 手机（真小屏，14px 合理）；② **用户缩放放大**
  （200%/400% 后视口同样变窄）。CSS 无法区分两者，而后者里「为了看清而放大、字号反而变小」
  与 1.4.4 的意图正好相反。修：删掉该字号覆盖，保留 15px —— 窄视口靠**布局**消化
  （单列/卡片化/换行），不靠牺牲可读性。
  实测对比：400% 下 bodyFont **14px → 15px**，且零溢出、零截断不变。
  新增 `zoom_contract_test.go` 两条判据（body 字号不得被媒体查询改小 + 窄屏适配必须保留
  布局手段）；变异 Z1 恢复 14px → CAUGHT、Z2 删表格卡片化 → CAUGHT。

### Added
- **多 BOM 门禁（`tests/check_multi_bom.ps1`，CI 第 17 个，round 44 记的待办兑现）—— 但它最值钱的是开发过程**：
  判据：受跟踪文本文件在 HEAD 中的原始字节若有 >=2 个连续前导 EF BB BF 即判红
  （单 BOM 与无 BOM 都合规；.ps1 必须带 BOM、.md 多为无 BOM，两类都不强迫改）。
  起因是 round 44 我查出的「CHANGELOG.md 攒了 5 个前导 BOM」，且 round 45 实测确认
  clean 过滤器只管换行、**不碰 BOM**（3 BOM -> add 后仍 3 -> 补规则仍 3 -> 提交仍 3），
  多 BOM 会一路进库，而上轮只把它记成一条待办、没上门禁。
  **本轮真正的教训：传输层自己会污染数据，而且症状是「漏检还很像全绿」。**
  首版把请求写进 `git cat-file --batch` 的 stdin，两条常见写法都被污染：
  ① `New-Object IO.StreamWriter($stream)` —— PS5.1（.NET Framework）默认吐 UTF-8 BOM，
     第一条请求变成 `<BOM>HEAD:x.md`，git 回 missing；
  ② 直接 `BaseStream.Write($bytes)` —— **逐字节 hexdump 证明写入是干净的**（48 45 41 44...），
     git 仍回 `<BOM>HEAD:multi.md missing`，BOM 来源未查明。
  后果：首版报「查 1620 个」完全正常，**实际第一个文件从未被检查**；修好后真实数是 1621，
  差的那 1 个正是被跳过的。查 BOM 的门禁被 BOM 咬了还漏检 —— 若无变异测试（造一个
  含 3-BOM 文件的一次性仓库）就会带病上线，而这门禁的全部意义就是抓这种东西。
  定稿：请求写临时文件 + `cmd /c` 文件重定向（字节由 OS 保证；node 同字节 input 实测返回
  blob 17 含 3 个 BOM 为参照），并强制「响应记录数 == 请求数」，对不上直接 UNVERIFIED ——
  **静默漏检比误报更危险，因为它看起来是绿的**。
  cmd 引号规则也是实测三轮才定稿（在册）：`/c "<带空格exe> ..."` 会被剥外层引号去执行
  'C:\Program'；`/c "<exe> <args>" < "<file>"` 会把重定向当参数喂给 git（unknown option）；
  唯一可靠是整体再包一层：`/c ""<exe>" cat-file --batch < "<file>""`。
  四态实测全对：真实仓库 rc=0（1621 个：单BOM 67 / 无BOM 1554 / 多BOM 0，耗时 3.5s）、
  变异仓库 rc=1 且点名 `multi.md 前导BOM=3`、非 git 目录 rc=2、**目录不存在也走 rc=2**
  （首版这里会抛裸 Win32Exception「The directory name is invalid」，round 46 补前置校验——
  路径打错字必须 UNVERIFIED，不是崩栈）。
  上闸前已确认实况多 BOM=0 => 上线即绿，不给 CI 添噪声。
- **入库 EOL 门禁（`tests/check_repo_eol.ps1`，CI 第 16 个）—— 补上「已入库的 CRLF 残留」这个无人管的层（round 44）**：
  顺着 round 25 那次「173 个 CRLF 进 HEAD、diff 被放大成 177 增/169 删」做全仓核查：
  用 `git ls-files --eol` 扫全部 2,043 个受跟踪文件，实况 i/lf=1944、i/none=95、i/-text=4，
  **i/crlf 与 i/mixed 均为 0** —— 当前库是干净的。但**没有任何门禁在守这个不变**：
  已有的 `check_ps1_eol.ps1` 查的是**暂存区**，残留一旦提交上去就永远看不见了。
  新门禁查**已在库**的（i/crlf / i/mixed 即判红），三层时点各管一段：
  check_ps1_eol 管提交前暂存区（本地）、check_repo_eol 管已入库全量（CI 兜底）。
  **两个关键实验把机理钉死（一次性临时仓库，用完即删）**：
  - 实验1：`core.autocrlf=false` 时 add 一个 CRLF 文件 => 报 `i/crlf`（能进库）；
  - 实验2：之后补 `*.ps1 text eol=crlf` 并提交、不 renormalize => **依旧 i/crlf、
    HEAD 原始字节仍含 CRLF** => CRLF 一旦进对象库，后来加规则**不会自动纠正**，会一直残留。
  修复手段因此写进判红提示：`git add --renormalize <文件>`（不 renormalize 就一直留着）。
  **顺带更正 round 42 我写下的一句过强结论**：当时写「当前配置下 CRLF 根本进不了 HEAD」。
  那只对**本机**成立 —— `core.autocrlf` 是仓库本地配置、**不随 clone 传播**（round 41 已实测
  `core.hooksPath` 同性质），CI runner 与他人的机器完全可能是 false/input。
  「本机不会发生」不等于「仓库不会发生」，而这正是本门禁必须存在的理由 ——
  与本仓反复踩的「实测一次当机理正确」（round 26/31/42）同族，第三次记。
  验证（三态全覆盖，不是只测 happy path）：真实仓库 rc=0；对含 CRLF 残留的临时仓库
  rc=1 且点名 `a.ps1 i/crlf（w/crlf，…）`；非 git 目录 rc=2 UNVERIFIED。
  刻意不管 i/-text（二进制/标了 -text：.exe/.syso/.pyc、故意保留编码的样本）与
  i/none（文件根本没有换行，实测证据件 result.py 即此类）—— 避免假红。
  上闸前已确认实况为 0 违例，所以**上线即绿**，不是给 CI 添噪声。
- **趋势器补「新鲜度」：不再把末次落盘的状态当成现在的状态（round 42 实测引出）**：
  自己上一轮（round 40/41）建的趋势机制有个真实缺陷，本轮第一次用就暴露：
  它报「红窗 1.3h」，而我复核时红态已 >2h —— 因为**落盘是被动的**（靠人跑门禁产生），
  序列只覆盖到最后一次落盘的那一刻，**之后是盲区**，而趋势器不报这件事。
  这就是本仓反复踩的「读数过期」同族问题（round 22 报告状态栏滞后、round 32 数字断言过期，
  这次轮到我自己建的机制）。
  修法：每条趋势都报「末读数距今多久」；超 6h 打陈旧警告，明说「下面的趋势只覆盖到
  末读数那一刻，当前实况未测」。6h 的依据：本仓读数非定时产生（实测 24 条/14h，
  约半小时一条），超 6h 无新读数即可判陈旧。
  health 分支的「红窗」同时改成「**至少** Nh（测于两次落盘之间，末读数后是盲区）」——
  诚实标注下界而不是假装知道现在。
  验证：真实数据现在报「末读数距今 0.0h、红窗至少 2.2h（4/4 全红）」；
  合成一条 10.8h 前的旧读数序列 ⇒ 陈旧警告如期触发（合成文件用后即删，不污染真实序列）。
  顺带把机制跑起来的数据补齐（重跑落盘）：红态从 3 次连续升为 4 次连续、跨 2.2h，
  进一步支持「值得和并行会话对齐一次收尾」。
- **长驻内存读数纳入序列机制（round 41，趋势机制的最后一块）**：
  round 20/40 已给 untracked / health / coverage 三个序列建了落盘 + 消费端，
  但**长驻内存这个核心指标偏偏没落** —— 而内存正是前几轮反复量错的维度：
  round 22 把探针与长驻两种形态互比、round 25 单点抽样得出「稳态波动 2 倍」
  （后证实是空闲/执行期两态之差）、round 28 又发现双环境与 Explorer 的影响。
  **三次误判同一根因：单次读数没有参照物。**
  改动：`check_resident_longrun.ps1` 判定后落盘一条（medianMB / spreadMB / boundMB /
  phase / stableWaitS / pid / verdict / head）；趋势器加第四分支 ——
  报中位首→末方向（涨=泄漏或负载上移）、序列内最大极差（GC 归还变慢的早期信号）、
  以及**离上界余量**（<40MB 提示「一次正常抖动就可能误判，该复核上界是否定太紧」，
  即 round 22 双环境教训的自动化）。
  **关键的负向行为（真实执行验证）**：引擎 running 时门禁走 UNVERIFIED，
  **且一条都不落盘**（before=0→after=0）—— 「没测成」绝不进趋势序列，
  否则内存序列会被执行期高值污染，重演 round 25 的误判。
  验证分三层：① 落盘块字段完整（隔离验证，PASS/FAIL 两分支）；
  ② 趋势器合成数据端到端（+6MB 触发「上升」方向、余量 36<40 触发提示，两分支都覆盖）；
  ③ 真实执行确认负向路径（UNVERIFIED 不落盘）。合成假读数用后即删，不污染真实序列。
  如实记：内存序列的**真实**读数要等空闲窗口才有（不为验证去停生产实例，那会打断
  并行会话的任务）；当前该序列长度 0，趋势器对空序列正常跳过，不算断。
- **补上趋势的「消费端」：新增 `tests/check_readings_trend.ps1`（round 40）**：
  round 20 起，多个门禁把核心读数落盘到 `.workbuddy/memory/gate-readings.jsonl`，
  动机是 round 31/32 的教训「单次读数会被一次性脚本骗，序列不会」。
  但 round 40 检查发现：**数据一直在积累，没有任何东西在消费它** ——
  没有趋势工具，等于记了流水账没人对账。本件把序列变成信息。
  三个消费点：`check_untracked_go`（gap / 未跟踪数 首→末）、
  `check_untracked_go_health`（badPkgCount 首→末 + 红读数占比 + **尾部连续红的次数与时长**）、
  `coverage`（avgPct/minPct 首→末，**区分 local/ci**：round 22 已证两环境系统性差 10~20pp，混算没意义）。
  **定位是 informational 报告、不是拦截器**（有数据 rc=0 / 无数据 rc=2 UNVERIFIED）——
  「趋势变差」的语义因人因时而异（gap 上升可能只是并行会话在写新测试），
  拿它做硬闸会制造噪声（本仓在册：模糊语义检查 ⇒ 门禁被删）。
  **它第一次运行就产出了单看快照绝对看不到的信息**：
  `gap 175 → 77（-98，改善方向）`、未跟踪 .go 97→31 —— 而此前每次读到「gap=77」
  都只当它是一个静态数，看不出它其实正在往好的方向走。
  健康度序列则如实显示 badPkg=3 已连续 3 次读数全红（红窗 1.3h），
  并给出「在途会话多半没在收尾，值得对齐所有权」的提示。
  健壮性（实测）：PS5.1 的 `Add-Content -Encoding UTF8` 可能给追加段夹带 BOM，
  解析端先 `Trim([char]0xFEFF)` 再 ConvertFrom-Json —— 用**故意带 BOM 的行**实测 7/7 全解析；
  `-Path` 参数支持注入别的读数文件做端到端自测（默认路径由 `$PSScriptRoot` 推导，
  不硬编码 —— 2026-10-22 的在册教训）。
  另确认一项边界：**读数文件本身不入库**（`.workbuddy/memory/gate-readings.jsonl` 未跟踪）。
  这是刻意的：数据里含本机路径与时间戳，属机器本地历史；**工具入库、数据不入库**；
  读数缺失时本件走 rc=2 UNVERIFIED，不把「没有历史」当「趋势没问题」。
  实现注记：本次写文件彻底避开反引号（换行用 [char]10、BOM 用 [char]0xFEFF）——
  「外层模板吃掉 PowerShell 反引号」我已踩第三次，这次从写法上根除而不是事后修。
- **200%/400% 缩放审计（WCAG 1.4.4，同轮）**：5 页 × 2 档实测（720×450 / 360×225，
  deviceScaleFactor 2/4）：`overflowX` 全 ≤0、越界元素 0、内容截断 0、按钮高度 36px 达标、
  正文最小 12.5px。缩放场景此前从未实测过，本轮证明布局在极端缩放下不塌。
### Added
- **打印样式（@media print）—— 此前全站零打印规则（2026-10-22 打印介质实测）**：
  CDP 模拟打印介质实测，打印预览里 body 仍是 rgb(4,7,13) 深底 + 极光渐变 + 顶栏 + 页签 +
  按钮残骸，**纸上是满页黑块、完全不可用**。而界面本身有「打印自检报告」按钮，
  用户真的会按 Ctrl+P —— 这个场景从来没被设计过。
  新增打印版「白纸黑字」：关掉所有装饰层（渐变/网格/辉光/扫描线/伪元素）、隐去导航与操作面
  （顶栏/页签/工具栏/按钮/Toast）、卡片退化为分隔线（省墨且清晰）、文本与表格强制深色、
  状态色压深保留语义（黑白打印机也能辨）、**折叠块强制展开**（纸质件无法点击，未展开
  等于内容丢失）、长表格不截断且跨页可断、外链打印出 URL。
  复测（同一探针）：body 深底变成 **白底 rgb(255,255,255) 加黑字 rgb(17,17,17)**、
  topbar/tabs/btn 全部 display:none、card 白底无渐变。
  新增 print_styles_contract_test.go 三条判据；变异验证 P1 摘掉整块 CAUGHT、P2 白底改黑
  首轮 **MISSED**（判据只查 background:#fff 子串，color:#111 被保留导致子串仍在），
  收紧为整串并加反向深色断言后 CAUGHT、P3 删折叠展开 CAUGHT。
- **键盘导航审计通过（同轮，无需修改）**：CDP 真 Tab 键 × 5 页实测，焦点环全部可见
  （outline=solid/2px 且 :focus-visible=true），治理台 SUMMARY 可聚焦、Space 可展开。
  同时留下一条踩坑：**探针实例有 --seconds 时限，超时后 Edge 渲染网络错误页，CDP 仍可连、
  evaluate 仍返回，读数看起来完全正常但测的是错误页** —— 本轮据此一度误判两个不存在的
  缺陷。防护：跑探针前先 curl 断言 HTTP 200。

### Added
- **模型池：各免费池「一键启用 / 一键停用」+ 网关专属/其它车道下线（老大 2026-10-22 令）**：
  1) **整池开关** —— 新增 `Manager.SetPoolEnabled(lane, group, enabled, actor)`：面板每张分组卡头部
     带「一键启用/一键停用」两枚按钮（`POST /api/pool/lane`），CLI 侧 `--model-pool lane <lane> <enable|disable>`
     同口径。判据要点：**group 优先于 lane**（分组卡就是「池」的单位；自填端点常是 lane=aggregate 而
     Group=「自定义」，取交集会把真实存在的池判成空池）；手动开关一律清掉 `AutoDisabled`，否则「一键停用」
     会被下一轮网关同步按「仍在广播」自动 revive，按钮成摆设。停用走两段式确认（armConfirm），
     一次误点不得让整条渠道退出调度。缺 `enabled` / 缺 lane+group / 未知车道 → 400，绝不静默成功。
  2) **网关两条车道去掉，只留 eac/kilo/ofm 等免费车道** —— `SpecFromGatewayModel` 的网关专属/网关其它
     两个分支改为 `default: return Spec{}, false`（非 `our-free-model*` 形态一律不纳管，计入 `SyncResult.Skipped`）；
     新增 `Manager.PurgeGatewayLanes` 清理**历史账本存量**（判据 provider=="gateway" ∪ 分组名命中，
     幂等、不误删用户自填端点），`Service.SyncNow` 每轮开头自动调用，另给 `POST /api/pool/purge` 与
     `--model-pool purge-gateway` 两个人工/脚本入口。前端 GROUP_ORDER 摘除该二组，车道水位只报 3 条免费池。
  实测：真实网关广播 56 个 id → 入池 38 / 跳过 18；线上实例首轮同步清掉 25 条存量；EAC 池一键停用
  `available 42→37` 且路由表同步收缩、再启用回 42 并落盘。`go build/vet` = 0，`go test ./...` 29 包全绿
  （modelpool 覆盖率 70.2%，门禁阈值 49）。新增回归：modelpool 4 条 + web 3 条（含反假绿坏样本）。
  产物经 `setup/build.ps1` 七道门禁重建（旧件备份在 `dist/archive/`）。
- **测试层 Windows 平台耦合：`go vet` 在 Linux 容器内编译失败（2026-10-22 与上一条同轮修复）**：
  `internal/executor/linkdir_silent_test.go` 与 `cmd/workbench/headless_test.go` 在**无 build tag** 的
  `_test.go` 里使用 `syscall.SysProcAttr{HideWindow, CreationFlags}` —— 这两个字段只在 Windows 存在，
  于是 `tests/docker/run_pool_e2e.ps1` 的「池门禁 go test」段在 `go vet ./...` 就红
  （实测 `vet: ... HideWindow undefined`），把模型池的 Linux 侧验收整体拖红。
  修法：Windows 专属断言按平台拆开 —— `linkdir_silent_test.go` 加 `//go:build windows`；
  `hiddenTestCmd` 从无 tag 文件抽出，拆成 `headless_testcmd_windows_test.go`（真设窗口标志）与
  `headless_testcmd_other_test.go`（非 Windows 同签名直通，保住 4 个调用方文件的编译）。
  验证：Windows `go vet ./...`=0、Linux 容器内 `go vet ./...`=0；
  `DOCKER_GATE_PASS`（debian dash + alpine busybox **双腿各 102/102**、池门禁 go test PASS）。
  产物重建 9.80 MB / SHA256 `1D64BDCB…`。

### Fixed
- **仓库全库 Go 格式漂移 + 上闸（2026-10-22 实测：73/286 个假红根因是 CRLF，真漂移 52 个）**：
  本仓长期「CI 无 gofmt 门禁」，此前只记过一句「所以仓库存在长期格式漂移」。本轮把它挖到底：
  ① **直接原因是假红淹没** —— 仓库无 `.gitattributes` 且 `core.autocrlf=true`，工作副本全是 CRLF，
  而 gofmt 只接受 LF：对工作副本跑 `gofmt -l .` 得 **73 个**，干净 clone 上得 **286 个**（整文件重写）。
  实测反证：chain.go 归一化成 LF 后立刻为 0。② **但里面确实混着真漂移** —— 归一化后仍有 54 个文件
  需要格式化（config.go / main.go / html.go 等），全是 struct tag 与 composite literal 的对齐空格。
  两类混在一起，谁上闸谁被假红劝退，这就是门禁至今缺位的机制性原因。
  修：① 新增 `.gitattributes`：`*.go text eol=lf`（gofmt 口径）+ 脚本/yml/md `text=auto` + 二进制 binary；
  ② 新增 `tests/check_gofmt.ps1`：**把每个受跟踪 .go 归一化成 LF 再判**，只判代码格式、不判行尾
  （两者职责分开、互不掩盖），首跑点名 55 个真漂移；③ 批量 `gofmt -w` 修复 **52 个**（纯空白，
  已机器证明：253 个文件的「剥离全部空白后逐字节比对」0 处内容变化）；④ 接入 `build.ps1 3/6` 与
  `check_all` 独立一段，CI 侧待 `.github/workflows/ci.yml`（并行会话在途件）入库后再接。
  豁免机制（非静默跳过）：`tests/gofmt-debt.txt` 显式登记 2 个**并行会话在途**的 shortcut 文件
  （gofmt 会重排其文档注释结构 = 代改他人代码，本轮不碰）；门禁仍点名+计数，措辞用三态
  「PASS 但 N 个豁免未清」，绝不在有豁免时打「合规」。清零条件写进 debt 文件。
  变异验证：清空 debt → 判红并点名 2 个；注入未登记漂移（queue.go struct 对齐）→ 判红并点名该文件；
  恢复 → PASS。回归：`go build`/`go vet`/`go test ./...` 全 rc=0（52 个纯空白改动零行为变化），
  `check_all` 九段全 PASS/合规 SKIP。
- **预检总门禁断链：check_all 引用的 4 个门禁从未入库，干净 clone 里静默降级（2026-10-22 盘点实测）**：
  `tests/check_all.ps1`（受跟踪）把散落的单点门禁串成一次调用，但它 Join-Path 引用的
  `check_subsystem / check_spawn_silent / check_no_shrink / check_homepage` 全部 untracked ——
  而各段的前置检查是「脚本不存在 ⇒ SKIP 且 rc=0」，于是 CI/干净 clone 里总门禁**绿色地**只跑
  入库的那几条；本机全绿掩盖了「别人拿到的门禁更弱」这个事实。同族事故本仓在册：
  `check_snapshot_build.ps1`（git archive 导出 HEAD 后 go build 报 undefined，因 4 个依赖从未入库）
  ——那是 Go 侧，本次是脚本侧复现。
  修：① 四个门禁脚本 + `keepalive.ps1`（计划任务实际拉起的守护件，10-19/10-20 定稿，非在途件）入库；
  ② 新增机器判据 `tests/check_gate_links.ps1`：**受跟踪**脚本以代码行引用的每个 `tests\x.ps1`
  必须存在**且受跟踪**（注释行不算声明，与 check_noninteractive「窗口只取代码行」同口径；
  untracked 之间的引用不查，不误伤并行在途件），并已接入 check_all 作为独立一段 —— 断链从
  「静默 SKIP」变成「显式 FAIL」；首跑即点名 5 条（含它自己），入库后全绿。
  ③ 顺带修 check_noninteractive 一处误报：它把 `xtmp/oss/*`（对标 clone 的上游仓 scratch，
  .gitignore 已忽略、CI 里不存在）里上游脚本的 Read-Host 判成**本仓**违规（7 处）；skipDirs
  补 xtmp 后复扫 42 个在册脚本 0 违规。实测：check_all 八段全 PASS/合规 SKIP（rc=0）、
  check_gate_links 双向验证（先判红 5 条、入库后 rc=0）、check_snapshot_build 纯净检出编译仍绿。
- **newTestEngine 收尾等待仍失效（一修的反例，2026-10-22 本地 -cpu=1 复现）**：
  上一条（1852ac2）把等待写成「进入 newTestEngine 就 `go inflight.Wait()`、cleanup 时 `<-fin`」——
  **结构上不成立**：WaitGroup 计数为 0 时 Wait 立即返回，那个 fin 通道只反映「构造瞬间没有在途」，
  runOne 之后才 `Add(1)` 的 postProcess（engine.go:1243）永远等不到；CI 上 1852ac2 自己的 run
  （39733004935）随即同判据再红即为证据。本机普通跑不复现，用 `-cpu=1` 放大调度窗口后
  20 连跑 7 红（三个 plan 用例同根因）。修法：把 Wait 挪进 cleanup 闭包**内**才启动 go Wait()，
  等的是「测试跑完时真正在途的工作」；5s 有界兜底不变。修复后 20 连跑全绿、整包 -cpu=1 ×3 全绿。
  教训（与本仓「判据要直接测真实现」同型）：**修并发时序问题，必须先在能稳定复现的口径下
  证伪旧修法**（-cpu=1 / 高 count），否则「本机绿 + 结构错」的修法会上线再红一轮。
- **engine 包 CI 独有假红（第二次同型）：newTestEngine 收尾不等在途 postProcess，RemoveAll 被并发新增击穿（2026-10-22 实测）**：
  docs-only 提交 609135a 的 CI run 判红 `TestPlanModelDiffersFromExecutorRole: TempDir RemoveAll
  cleanup: ... workbench-output: The directory is not empty` —— 与「契约测试连红 8 次」同族：
  红与被推送内容无关，本机 20 连跑不复现（窗口关着），慢 runner 上才张开。
  根因链：runOne 成功路径把 postProcess（自评/摘要/链式后继）丢进 goroutine（engine.go:1244，
  e.inflight 记账），测试函数返回时它还在往 layout.TaskDir 写；而 newTestEngine 的收尾
  只关了 store 句柄，没排在途工作 —— RemoveAll 删到一半被新增文件击穿，报「目录非空」。
  修：t.Cleanup 增加一条**有界 5s** 的 inflight 排空（注册在 st.Close 之后 ⇒ 后进先出，
  先排空再关 store 再删目录，与生产 stop() 的两段 drain 同型）；等待不许挂死测试。
  实测：go test ./internal/engine -count=1 -race rc=0、-count=5 -race rc=0；
  诚实记录：本机 -count=20 下「摘掉排空」的变异体也不判红（窗口关着），
  判红证据在 CI 侧（docs-only 提交不可能改出功能回归，红点只能来自收尾竞态）。
  与上一条 dshprofile 修复同理：**这类红一律修测试收尾，绝不降判据、绝不重跑掩盖**。
- **CI 覆盖率门禁判红（build 5/6 步）：dshprofile 只有 patch 文案判据、环境构建链零覆盖（2026-10-22 实测）**：
  上一条修复把 test job 变绿后，红点下移到 **build + coverage gate**：`覆盖率门禁未通过：
  workbench/internal/dshprofile=1.4%<阈值2.5%`。根因不是阈值设错，是该包此前 3 条判据全在
  验 cordis.patch.yml 的文案，而 LocateDSH / ensureOFMData / ensureProfile / Bootstrap 这条
  「环境准备主链」没有任何测试。**处置是补真判据而不是降阈值**。
  新增 `bootstrap_env_test.go` 十条（外部依赖全走环境变量注入，CI runner 无 DSH Desktop 也能跑）：
  LocateDSH 优先 DSH_DESKTOP_EXE、CLI 文件缺失必须点名报错、resolveFromShim 双向、
  ofmSourceDir 认 index.js 不认空目录、ensureOFMData 落「caveman/ponytail=off + 32768」质量配置
  且已存在不重写、writeIfChanged 内容未动不碰 mtime、copyDir 嵌套、Bootstrap 端到端 + 二次幂等。
  **顺带抓到并修掉一个真 bug**：`resolveFromShim` 按 `"\\n"` 字面量（反斜杠+n）切分 shim，
  永远切不开 —— 整文件被当一行，首个引号落在哪全看 shim 写法；已装的真实 shim 恰好写法配合才正确，
  前置有多条带引号路径时**解析出错误 exe**。改为按真换行切分，并加反向判据
  `TestResolveFromShimSkipsEarlierQuotedPaths`（旧实现下必红：实测抓到它返回 "Junk First.exe"）。
  变异验证：把修复回退成字面量切分 → 判据 CAUGHT；恢复 → dshprofile 覆盖 1.4% → **79.5%**。
  实测：`go test ./... -cover` 29 包对照 PerPackageCover 全表零未达标、`go vet ./...` rc=0、
  `go test -race -count=1 ./...` rc=0。
- **远端 CI 连续 8 次假红：契约测试的收尾方式在 Windows 上不成立（2026-10-22 实测）**：
  远端 CI 自 08:46(+08) 起连续 8 次 red，失败点全部是同型的一句 ——
  `TempDir RemoveAll cleanup: unlinkat ...\TestSilentDoesNotOpenPanel*\002: The process cannot access
  the file because it is being used by another process`。其中**一次判红的提交是纯文档提交**
  （docs/changelog），说明红与被推送内容无关：`cmd/workbench` 契约测试把 t.TempDir() 当子进程的
  工作目录用，而 Windows 上目录里只要有进程还活着或句柄未释放就删不净 —— t.TempDir() 把「删不净」
  记成**测试 FAIL**，于是契约判据全绿、CI 却连红（门禁形同虚设，且没人发现）。
  处置（`headless_test.go` 新增三条可复用能力，`silent_contract_test.go` 改用它们）：
  ① `killTree` 收整棵进程树（taskkill /T /F）—— 旧实现只 Kill 直接子进程，收不回脱离进程树的后代；
  ② `reapSpawnedExe` 按**可执行文件绝对路径**精确回收残留实例（路径经环境变量传入，避免命令转义；
  只可能命中本测试自建目录下的实例，不误伤用户正在跑的实例）；
  ③ `newSpawnDir` + `removeSpawnDirWithin`：自管目录、重试删除、**删不净只留证不判红**
  （契约判据在收尾前已跑完入库，环境残留不该伪装成契约被破坏），并打印「CommandLine 命中该目录」
  的进程快照供定位。新增 `spawn_cleanup_test.go` 两条判据 + **变异验证**：
  M-A 去掉 taskkill → 孙进程判据 CAUGHT（实测报出 9.83s 失败）、M-B 把留证改回判红 → 容忍判据 CAUGHT。
  实测：`go test -race -count=1 ./...` rc=0（CI 同命令）、`go vet ./...` rc=0；顺带把本包两个被改文件的
  gofmt 漂移归零。
- **retry 策略非法值只静默钳制、无痕迹（2026-10-22 对标 river `job_executor.go:563-571`）**：
  `retry.Policy.nextDelay` 对「算出来的等待非正」一律钳到 **1ms** —— 那是热循环配方
  （高频失败时每秒重试一次，既打上游又刷台账）；且配置写坏与「值本来就小」在日志里完全同形。
  修：构造期跨字段矛盾（`MaxDelayMs < BaseDelayMs`、抖动越界、退避列表非正项）登记 `Policy.Fixes`；
  运行期算出非正等待时回落可信的默认指数退避并登记 `Policy.Warnings`（去重）；
  引擎在 3 处装配点（New / Recover / ReloadConfig）读 `Fixes` 写 WARN 日志。
  判据 18 条（含反向：合法配置零 Fixes/Warnings）；变异两条：删抖动塌陷兜底 → 判红（实测抓到 692ms 热循环值）、
  删 Fixes 登记 → 判红。retry 覆盖 89.1% → **95.7%**。
- **`config.Normalize()` 零跨字段校验（31 处钳制全是单字段）+ v3 迁移判据被版本号污染（2026-10-22 对标 river `client.go:528-534`）**：
  ① 新增 `normalizeCrossField`，三条**功能不可达**判据：`StallSeconds >= DefaultTimeout` 时看门狗
  永不可能先于硬超时触发（stalled 成死配置）→ 钳到超时一半；`ReplanRounds >= RepairRounds` 推翻
  「先用便宜手段」的设计顺序 → 钳到返工-1；`PlannerMaxMinutes >= DefaultTimeout` 使方案阶段挤占全部执行预算 → 钳到超时一半。
  每一条登记 `Config.Warnings`（不入盘，`canonicalizeForCompare` 里抹平，防无谓重写）。
  ② **实测发现既有缺陷**：`Normalize()` 开头把零值配置的 `SchemaVersion` 直接抬到 6，
  而 v2→v3 迁移分支的判据用的是这个**被改过的值** ⇒ 零值/半截配置永远不命中迁移，
  `appendProtocol` 停在 false、`repairRounds` 停在 0（P3 产物协议形同虚设）。
  这正是本文件记过两次的「版本号被提前抬升」的**第三处复现**；改为用项目自己文档里写的 `fromSchema`（进入时的版本）。
  ③ 反向判据：跨字段段**不得触碰自洽配置**（默认 420s 不得被改；一轮实测写反方向把默认抬到 1860s 被既有测试抓出）。
  ④ 一条判据写了又撤（`DailyCap <= Concurrency`）：它把「一天只产 1 条」这种**刻意收紧**当矛盾配置抬到 200，
  被两条既有测试当场判红 —— 跨字段校验只修「功能不可达」，不替用户调参。留作反例记录。
  判据 7 条；变异两条：摘掉跨字段调用 → 三条判据同时点名、v3 判据退回污染值 → 判红。config 覆盖 69.1% → **70.0%**。
- **失败与取消路径「状态已迁、台账未写」（2026-10-22 对标 asynq `processor.go:336-338`）**：
  ① `failTask` 改造前顺序是 `Update`(迁状态) → `Audit` ⇒ 存在窗口「状态已是 failed，台账查不到为什么」；
  更糟的是 `Update` 返回错误时只 emit 一条**瞬时事件（不落盘）**，任务停在 running、既不重试也不统计。
  ② `rescueInterrupted` 的判死分支**改造前零审计** —— 判死是不可逆终态决策，却没有任何可追溯台账。
  ③ `finishCancelled` 只在迁移**失败**时才写审计，正常取消只有瞬时事件（事后无法区分「用户取消」与「任务失败」）。
  修：新增 `writeLedgerThenTransition` seam（沿用本仓 `autonomy.writeLedgerThenActivate` 同型范式），
  三处全部改为「先留痕后生效」；`Store.Audit` 由 `void` 改为**返回错误**（改造前 `_ = s.audit.Append` 把失败全吞），
  审计写失败时退到任务事件流留痕（两级留痕面写在不同目录，同时不可写的概率极低），保证「至少一处留痕」是结构保证。
  判据 6 条；变异两条：seam 反序 → 判红（实测抓到 `[state ledger]`）、
  摘掉判死专用审计 → 判红（**关键**：`Transition` 自己也会写一条 kind=transition 的审计且文案重合，
  第一版判据只搜文案被一条变异放过，加严为**按 kind=error 过滤**后才真正抓住）。engine 覆盖 74.2% → **75.3%**。
- **卡死看门狗只认 stdout 事件，不认模型写盘（2026-10-22 对标 river `watchStuck` 后新增，会直接丢产出）**：
  `res.Touch()` 只在 `consume()` 解析到 stdout 事件时刷新活跃度。而 artifact 产出协议下模型常常
  「先静默写文件、最后才吐 final 事件」，长文写作类任务的静默写盘期可达数分钟 ——
  只认事件时，这类任务会在 `StallSeconds`（默认 420s）到期被判卡死并杀进程树，**已写出的产物一并丢失**（不可逆）。
  修：活跃度改双信号取较新者 —— ① stdout 事件 ② workspace 产出文件 mtime（`LatestOutputMtimeMs`，
  扩展名白名单与 `hasWorkspaceOutput` 同口径，目录不可读时退化为「无信号」= 改造前行为，不误判）；
  判定逻辑收敛到纯函数 `IsStalled`（可单测、可变异），看门狗 goroutine 只负责周期调用与善后。
  判据 5 条；变异一条：`IsStalled` 回退成单信号 → 判红。executor 覆盖 49.2% → **52.9%**。
- **审计/留痕顺序判据第一版测重了对象（2026-10-22 自查，防重犯）**：
  第一版在测试里**重写了一遍判据逻辑**（把「取较新者」在测试内实现一次），
  变异把实现回退成单信号时照样全绿 —— 判据根本没碰到被测代码。
  同一轮还踩到第二次：看门狗判据写成测试内联逻辑、变异无效；
  以及 `config` 变异「摘掉跨字段调用」第一版没真摘掉（编辑锚点里调用行仍在注释之后）→ 假绿。
  教训：**变异测试必须先在实现上制造真实回退、并确认回退后的行为差异**；
  判据要直接调用被测的纯函数/seam，不重写逻辑。已按此重写全部新判据并复跑变异。
- **假 running 无人认领：卡死救援只有启动时一次扫，无持续轮询（2026-10-22 第五项，对标 river `internal/maintenance/job_rescuer.go`）**：
  L4（stuck_rescue.go）覆盖「进程崩溃后重启」；但**进程活着而任务失去归属**
  （goroutine 未起/早退未收尾/状态与内存表分叉）时，面板显示「正在执行 N」而实际零在跑，
  这类任务既不重试也不统计、还永久占着 running 计数与其链（queue.Pop 会静默跳过 busy 链上的
  queued 任务，queue.go:107 ⇒ 回收了但取不出＝假入队）。
  修：新增 `internal/engine/runtime_rescue.go` —— 调度 tick 内每 30 秒扫台账（river 同款周期），
  连续 max(StallSeconds×2, 600s) 处于「台账 running 但本进程无 goroutine」才动手；
  **两阶段确认**（首扫只登记候选，二扫达阈值才回收）。中途改用任务 UpdatedAt 判静默的方案
  被实测否决：`store.Update` 在 mutator 后无条件覆写 UpdatedAt，无关写入会重置计时且判据不可测。
  四条安全边界各钉反向判据：在飞(goroutine 存在)不碰、取消中不碰、阈值非法不碰、登记表随任务
  离开 running 自动清理。回收动作释放链占用 + 与 L4 共用同一份 CrashRescues 额度（防双预算翻倍）；
  留痕一律 kind=error 审计先于状态生效（沿用本轮 ledger seam）。
  判据 8 条；变异三条全部判红：摘在飞门 →「误杀在飞」点名、摘两阶段 → 三条点名、
  摘 ReleaseChain →「busy=1 want 0」点名（第三首版断言写成 busy>1 曾放过变异，已加严 ——
  「占用没恶化」≠「被释放」，queue.Pop 的静默跳过会让弱断言假绿）。engine 覆盖 75.3% → **75.9%**。
### Added
- **慢后端加载反馈：骨架占位 + 采样按钮 busy（2026-10-22 CDP 限速实测缺陷）**：
  ① 切页签首帧整片空白 —— 实测（CDP 限速 1.2s）模板页 `pane.innerHTML` 长度 = **0**，
  1600ms 后才有内容，用户无从判断「正在读」还是「坏了」；② 治理台「立即采样」按钮
  **零反馈** —— +0/+700/+3700ms 三帧读数完全一致（文案与 disabled 全程不变），而采样要跑
  一次真实量测、可能数秒，用户只会当成「没反应」再点一次。
  修：新增 `loadingPane`（切页签时若面板仍空则先给骨架）与 `failPane`（**失败必须替换占位**，
  否则「读失败」会显示成「一直在读」—— 判据卷「Loading 兜底不得永驻」的另一半）；
  采样按钮补 `disabled + 采样中…` 并在 `finally` 复原；采样失败的 `alert()` 换成 `toast()`
  （原生弹窗阻塞界面且不属于本设计体系）。新增 `.skeleton` 骨架样式（合成层友好的背景位移）。
  新增 `loading_feedback_contract_test.go` 两条判据（断言**调用点**而非标识符；含「禁原生 alert」），
  变异验证 M1 占位调用 / M2 失败态替换 / M3 采样 disabled / M4 恢复 alert 全部 CAUGHT。

### Fixed
- **治理台 `toast is not a function` 运行时回归（2026-10-22，门禁当场判红）**：把采样失败的
  `alert()` 换成 `toast()` 时，漏了 gov.js **没有** toast 定义 —— 三张页面（app.js / pool.js /
  gov.js）是各自独立的脚本包，某一页的 helper 对另一页不可见。前端门禁 `interact-governance`
  实测报错：`errors=["reject: toast is not a function"]`、`runtimeErrors=1`，判定 **FAIL**
  （ok=22 fail=1）—— 这是本轮唯一一次真回归，且被门禁在发布前抓住。
  修：给 gov.js 补同款 `toast` helper；新增 `toast_helper_contract_test.go`
  （govJS 一旦调用 `toast(` 就必须有 `const toast =`；另两页同规则反向断言），
  变异验证「摘掉 gov 的 toast 定义」→ CAUGHT。
  复测：门禁回到 **ok=23 fail=0**（`interact-governance errors=0`），发布 1.5.8。

### Added
- **慢后端加载反馈**（同日实测）：切页签首帧整片空白（`pane.innerHTML`=0）→ 骨架占位；
  治理台「立即采样」零反馈（+0/+700/+3700ms 三帧读数一致）→ `disabled + 采样中…` 并在
  `finally` 复原；`alert()` → `toast()`。端到端复测：采样按钮 `采样中…/true` → 复原；
  模板页首帧由 0 → 126 字符「读取中」骨架 → 1600ms 后真实内容。

> ⚠️ 本轮**换件已完成**（1.5.8，SHA256 AB9DAD18…DAED），但记录一条过程事实：
> 本轮 build.ps1 先后被并行会话在途件阻断 **3 次**（retry.go copylocks → stuck_rescue.go
> 未使用 import → stuck_rescue.go 语法错，均为写入中途的瞬时态），另有 1 次全量测试
> 撞上写入窗口误报失败；最后一次官方链 7/7 通过。
> ⚠️ 发布历史（前轮欠账）：树曾被并行会话在途件连续阻断 —— (a) `internal/retry/retry.go` 的
> `Policy` 含 `sync.Mutex` 仍按值传参（`go vet` copylocks，官方链 3/6 步判红）；
> (b) `internal/engine/stuck_rescue.go` 未使用 import（`go build` 失败，实测 11:02 仍在编辑）。
> 本端改动自身 `go vet ./internal/web` rc=0、包内测试全绿、CHANGELOG 门禁合规；
> 端到端 CDP 复测待树可编译后补。

### Added
- **内联编辑器键盘可达性：Escape 关闭 + 焦点回退（2026-10-22 走查实测缺口）**：
  池内联编辑器（新增/编辑模型）与模板内联编辑器此前都只能靠鼠标离开 —— 池编辑器有「取消」
  按钮但**没有 Escape 绑定**，模板编辑器连「取消」入口都没有；键盘用户进得去出不来。
  两处同型补齐：① Escape 关闭（文档级 keydown，只挂一次，不随重渲染累积监听）；
  ② 关闭后焦点**精确退回触发按钮**（打开时记 `document.activeElement`，关闭时还回去）；
  ③ 模板编辑器补可见「取消」按钮（与池编辑器对齐）。
  端到端实证（CDP 真按键）：池 → 打开时 focus=BUTTON:edit，Escape 后 editorOpen=false 且
  focus 仍是那个 edit 按钮；模板 → 按钮列表 [保存模板, 取消]，Escape 后内容清空、
  焦点回到「新建模板」。
  新增 `keyboard_dismiss_contract_test.go` 两条判据（三构件齐备：Escape 绑定/回焦记录/取消入口；
  keydown 绑定不得随渲染累积）。变异验证：M-A 摘掉池 Escape → CAUGHT、M-B 摘掉模板取消 →
  CAUGHT、**M-C 摘掉回焦记录首轮 MISSED**（判据只断言标识符 `editorReturnFocus`，而声明行
  与关闭函数里都有它 ⇒ 收窄为断言赋值语句本身后才 CAUGHT，与 round 10 的 M2 同源教训）。

> ⚠️ 本轮**未换件**：官方 build.ps1 在 3/6 `go vet` 步失败，根因是**并行会话在途改动**
> `internal/retry/retry.go`（HEAD 无 sync.Mutex、工作区有，10:30 改）—— `Policy` 含 Mutex
> 仍按值传参，vet copylocks 判红。不代改他人在途件，也不把已知 vet 缺陷打进 24/7 活实例。
> 本端改动自身 `go vet ./internal/web ./internal/gov` rc=0、包内测试全绿。

### Fixed
- **`retry.Policy` 运行期状态迁出结构体 —— 解掉 go vet copylocks（官方链 3/6 步判红；2026-10-22，清掉上方 ⚠️ 的成因 (a)）**：
  在途改动给 `Policy` 内嵌了 `warnMu sync.Mutex`（告警去重表 + 抖动随机源），而 `Policy`
  在本仓是**按值传递**的（`engine/helpers.go` 构造字面量后交给 `New`）—— vet 判死三处：
  `retry.go:138 New passes lock by value`、`retry.go:179 assignment copies lock value`、
  `engine/helpers.go:37 call of retry.New copies lock value`。后果不止静态告警：
  每复制一份策略就多出一份独立告警表，`Warnings()` 读数随副本而异。
  处置：运行期状态收进 `runtime *policyRuntime`（指针，锁只此一份），配置字段仍可按值传；
  `New()` 一律重建运行期状态（构造器语义）。新增 `runtime_state_test.go` 三条判据 ——
  `New` 不得继承入参告警（旧实现在此条判红）、按值复制必须共享同一份告警表、零值策略不 panic。
  实测：`go vet ./...` rc=0、`go test ./...` rc=0、`-race` 绿；并把本次改动镜像到 HEAD 的干净
  worktree 复跑全量 vet+test rc=0（等价 CI 判据）。
- **治理台考核矩阵三态失真（2026-10-21 前端走查实测）**：`CheckItem` 只有 bool `Pass`，
  unverified 项被序列化成 `"pass":false`，而前端去读根本不存在的 `it.level` ⇒ 六格全渲染
  FAIL，与同一页头部「3 PASS / 2 FAIL / 1 未验证」自相矛盾。修复：Go 侧补显式
  `state`（ok/fail/unverified）字段，前端优先读 state 并保留旧形态回落；
  新增 `checklist_state_test.go` 两条判据（三态分布 ⇄ 计数自洽、序列化必须带
  state=unverified），并对真实源码注入缺陷实测 rc=1（判据可证伪，非恒真）。
- **模型池组内序号从 2 开始（2026-10-21 走查实测）**：调用方传 `i+1`、`poolRow` 内又
  `(i+1)` 双重加一。改为调用方传 0 基索引，序号列恢复 1,2,3。
- **总览 KPI 随事件流反复淡入（走查截图抓到半透明读数）**：SSE 每来一条事件即重建
  overview DOM，`kpiIn` 入场动画照播 ⇒ 读数闪烁。新增 `overviewAnimated` 门：入场动画
  只属于首帧，后续刷新走 `.grid.no-anim .kpi{animation:none}`。
- **成功率把「还没跑过」读成 0.0%（假读数）**：完成数为 0 时该格显示「—」，有分母才显百分比。
- **「运行中但今日产出 0」四重缺陷叠加（2026-10-21 老大实报）**：面板显示
  `引擎 运行中 ｜ 排队 0 ｜ 今日产出 0`，而实例确实活着。取证（逐条可复算）：
  当日 00:00:02–01:39:59 产出 2000 条自主草案，**标题与 Prompt 去重后都只有 1 个**，
  全部被评估器拒绝（综合分 48/54 < 阈值 55），01:39:59 之后当天再无任何新任务。
  四层根因：① `DeriveNextGoal` 在**上一代标题**上继续追加「 · 深化：X」，代际复利把目标标题
  顶到 31 rune / 78 字节；② `model.Truncate` 的 n 是**字节**而标题上限常量是 **rune**（80），
  中文 3 字节/rune ⇒ 89 字节截到 80 后回退合法边界正好停在 78 字节，**刚追加的阶段后缀被整体吃掉**，
  标题退化成目标标题本身、每轮逐字相同；③ 评估器 `novelty()` 对同目标不做区分，而自主 Prompt 固定注入
  「项目目标 + 已完成阶段」，同目标相邻阶段相似度天然 0.9+ ⇒ novelty=0 ⇒ 价值 41 / 综合 48 **恒不通过**
  （`gates()` 里早有同目标豁免，novelty 没跟上，同一语义两处不一致 = 豁免被抵消）；
  ④ `tasksToday()` 把 **rejected 草案也算进 dailyCap**，2000 条从未执行过的草案把配额吃满后
  两条供货链同时停机一整天。修复：新增 `composeTitle()`（按 rune 限长、焦点段整体保留）与
  `rootGoalTitle()`（根标题重拼，掐断复利）；`novelty()` 同目标只认精确指纹、
  `gates()` 把「精确重复（任何目标都拦）」与「模糊相似（同目标豁免）」分开判；
  `tasksToday()` 不计 rejected 且模板链统一走它；新增 `rejectionPressure()` 拒绝熔断（阈值 5，
  连续被拒即停手并留痕）。**变异验证四条**：恢复旧码后对应用例逐条判红、复原后判绿。
- **静默停摆自证 `StallReason`（2026-10-21）**：`internal/engine/stall_reason.go` 把
  「为什么此刻没有产出」归纳成 8 个稳定原因码 + 人话文案（结构性阻塞 > 限流 > 配额耗尽 >
  规划熔断 > 目标阻塞 > 池冷却 > 无来源），经 `/api/status` 的 `stallReason` 输出，
  面板总览渲染 `stall-banner`、顶栏把原因顶到读数位置。原则：有产出时**不渲染**（不制造噪音）。
  根因是 24/7 最坏的失效形态——进程活着、phase=running、没有任何错误，只有产出是 0。

### Fixed
- **390px 窄屏管理面板表格列宽塌陷（2026-10-21 窄屏走查实测）**：`table-layout:fixed`
  把 6-8 列均分成每列 ~48px，「价值先验」折成 4 行、「状态」列跑位。修法：管理面板全部
  表格补 `data-label`（排队/执行中/产出/模板/模型健康度/失败原因六张表），≤480px 切
  卡片模式（thead 隐藏、tr 变卡、td 前置字段名，复用池表已验证范式）。
  取证：隔离实例 390px `theadVisible=0 / labeledTd=48`、队列 4 行转卡（row display:block）。
- **治理台「风险预警」文案读数不可扫 + round2 负数舍入方向错（2026-10-22 接续实测）**：
  上一轮只修了「量化指标」卡（展示层 govNum），但「风险预警」卡的 Detail 文案由
  `gov.VolumeAlerts` 在**数据源头**用 fmt.Sprintf 直接拼数字，展示层修不到它，
  于是同一页两处读数格式不一致：`任务失败率 当前 0.51比例`（单位粘连 + 量级读不出）、
  `日吞吐量 …（占用率 400%）`（低方向指标越小越坏，报 400% 会让人以为超载，
  真实含义是「只达红线要求的 25%」）。修：新增 `gov/metric_text.go`
  （numberText 千分位 / percentText 按量级取精度 / metricValueText 比例换算并吞掉
  单位词 / usagePhrase 按方向说人话），Detail 与所有消费面同源格式化；并去掉 Detail
  里重复的指标名前缀（面板 Title 与 Detail 并列渲染，「并发执行数 并发执行数…」念两遍）。
  顺带抓到 `round2` 既有 bug：`float64(int64(v*100+0.5))/100` 是向正无穷舍入，
  负数算错方向（-1234.5 → -1234.49）；本包 pearson() 相关系数取值域 [-1,1]，负相关
  是真正常态，故非理论问题。改用 math.Round。由 metric_text_test.go 千分位用例抓到。
  实测：预警三条 = 「当前 51%，警示线 7.0%，红线 10%（达红线 510%）」
  「…仅为红线要求的 38%」，页内浮点噪声/「比例」粘连/旧「占用率」措辞扫描均为 0。
  新增 5 条判据（千分位/百分数量级/比例吞单位/占用率方向语义/Detail 不重复 Title），
  变异验证 M-A 退回带前缀 → CAUGHT、M-B 比例退回裸小数 → CAUGHT。
- **治理台「量化指标」读数不可扫（2026-10-22 展开态走查实测）**：三类问题同源——
  数值原样拼进 HTML。① 阈值带 IEEE754 噪声：`Warn`/`Red` 未 round（阈值多为
  `max*0.8` 算式得出），面板显示「开发执行数 警示 4.800000000000001」
  「任务失败率 警示 0.06999999999999999」共 20 位裸浮点；② 比例类指标显示裸小数
  （`0.51` / 红线 `0.1`），读不出「51% 失败率」的量级；③ 大数无分组（`4000000 元`）。
  修：Go 侧 `Warn`/`Red` 与 `Value` 同精度 round2（源头修，不在展示层掩盖）；前端新增
  `govNum`（比例→百分数且阈值同步换算、≥1000 千分位分组）。实测：页内浮点噪声扫描
  0 处，「任务失败率 51% / 警示 7.0% / 红线 10%」、「单契约金额 警示 4,000,000」。
  新增 `eval_readability_test.go` 两条判据（13 项 × Value/Warn/Red/Ratio 小数位 ≤ 2 +
  占用率方向语义）；变异验证 M1 退回不 round → CAUGHT，M2「Value 也不 round」首轮
  **MISSED**（样本字段经 round2 前后无差别 = 覆盖面不足），补长小数 MemMB 后 CAUGHT。
- **已完成任务的产出在面板显示「（暂无产出）」、批量导出静默跳过大半任务
  （2026-10-22 交互面走查实测，功能级缺陷）**：详情接口与导出都硬编码
  `ReadTaskArtifact(id, "final.md")`，但产出文件名由 `engine.choosePrimary`
  按 outputFormat 与体积决定（实测样本 `article.md`），`final.md` 只是
  「模型一个文件都没写」时的兜底名 ⇒ 绝大多数任务读不到内容。
  实测读数：outputBytes=5935 的任务，面板 `.doc` 只有 6 个字符（就是那句占位文案）。
  修复：新增共用解析器 `readTaskArtifact`（三级回落：① `task.OutputFile` 绝对路径
  ② 目录 final.md ③ 目录内最大 .md/.txt，且拒绝 0 字节文件），详情与导出同源调用；
  接口另回 `contentFrom` 标明实际命中的文件。修复后面板 `.doc` 91px→620px、
  6 字符→2040 字符（7 个 h2 + 5 个 li 真渲染、无原始 markdown 泄漏），
  导出 79 篇 / 311KB / 零空壳。新增 `task_artifact_test.go` 8 例（三级回落 +
  目录不存在 / nil task / 0 字节三个反向），并对真实源码注入 3 个变异体实测判红：
  M1 退回硬编码 final.md、M2 空文件算命中、M3 扫描不过滤扩展名（会读到 events.jsonl）。
- **导出文档的领域标签与面板漂移（md / txt / docx 三条路径）**：面板表格已中文化
  （app.js 的 DOMAIN_TEXT），但导出在 Go 侧拼元信息行、仍写裸枚举 ⇒ 同一份数据
  面板显示「内容创作」、导出文档里是「content」。新增 Go 侧 `domainLabels` +
  `domainLabel()`（未知枚举原样返回以便发现缺项，不吞数据），三处导出接线；
  `domain_label_test.go` 把「两张表同源」做成构建期判据（正则抠 app.js 的
  DOMAIN_TEXT 与 Go 表逐键比对，双向差集点名），注入「值漂移」与「缺键」
  两个变异体均实测判红。修复后导出 79 篇领域行 100% 中文、英文泄漏 0。
- **首页主体尺度不随画布呼吸（2026-10-22 超宽屏走查实测）**：主按钮与入口条固定 320px，
  在 2560/3440 宽下取景框与雷达环都在放大、唯独主体不变，主体占比塌到 13%/9% ⇒
  「精密仪器」变成「仪器里的小按钮」。改为 `clamp(320px,26vw,460px)` + 高度
  `clamp(120px,11vw,164px)` + 入口条字号/行高同步。CDP 逐档实测阶梯：
  390→320(82%) / 1024→320(31%) / 1440→374(26%) / 1920→460(24%) / 2560→460(18%)，
  六档（含 740×360 矮屏，高度自动收到 90px）零溢出、主体全部在视口内。
  系数取 26 而非 16 的原因：16vw 在 1440 只有 230px、被 320 下限兜住，
  会让 1440-1920 整段成为「占比从 22% 掉到 17% 却一点没放大」的死区。

### Added
- **首页入场序列「镜头推进」（2026-10-22 动效篇补强）**：获奖级第一屏的标准是叙事性入场，
  此前首页只有常驻呼吸、打开即终态。新增三段式 1.5s 时间轴：环境光从景深亮起（blur 6px→0）
  → 主按钮推近 + 点亮（translateY 14px + scale .965 → 落定，.28s 起）→ 入口条落位（.62s 起）。
  两条实现纪律：① 全部驱动 transform/opacity/filter（合成层属性，不触发 layout）；
  ② **动画顺序即优先级** —— `animation-play-state:paused`（hover）只暂停列表第一个动画，
  故常驻 respire 放第一位、入场放最后，用户在入场途中 hover 不会把推近卡死。
  实证（CDP 逐帧）：80ms 暗场仅环境网格、300ms 主体落定 + 入口 op 0.84、700ms 全落位；
  `prefers-reduced-motion` 下降级为静态完整态（animDur=1e-06s 且 opacity=1，不卡透明）。
- **修复「同规则块两条 animation 声明互相覆盖」**：`body.home::before` 的入场与常驻 bloom
  原为两条独立声明，后者整体覆盖前者 ⇒ 入场被静默关掉（走查实测才发现 bloom 仍在、
  入场没跑）。合并为同一 animation 列表（`homeStageEnv …, bloom 12s … 1.5s infinite`）。

### Fixed
- **官方 build.ps1 全链首次跑通（2026-10-22）**：此前 2/6 资源步必失败（app.rc 缺件 +
  `$ErrorActionPreference="Stop"`），且 6/6 后有一个孤儿 `} else {`（首页门禁块误植了
  子系统的内联兜底，PowerShell 解析报错）——两个缺陷都因为「从没跑过 2/6 之后」而潜伏。
  修：2/6 改为「有 rc 才 windres，无 rc 则校验 git 跟踪的静态 syso 含 VS_VERSION_INFO」；
  孤儿 else 归位到子系统 if 块；dshprofile 覆盖率按实测基线设分档阈值 2.5
  （18 函数里只有 profilePatch 是纯函数已 100%，其余为真实 IO 型，与 tray/single 同型）。

### Added
- **获奖级前端走查第一轮（2026-10-21，八维自检）**：以 Awwwards/Webby/FWA 标准逐页目检四页
  （首页/管理台/模型池/治理台）× 三视口，落地：① 治理台 `.gov-card` 补基座样式（此前只有
  反光没有盒体，卡片呈裸文字漂浮）；② 首页第一屏补品牌铭牌（伪元素承载，不破「极简首页」
  DOM 契约，宽屏走取景框层、窄屏保留铭牌）；③ 表格领域中文化（code→代码生成 等六域，
  搜索同时认中文）；④ 数字列右对齐 + 操作列 `td.ops` 原语（替代 4 处把按钮拉成列的内联
  `td.row`）；⑤ 微交互补齐：页签悬停下划线预览、折叠标题悬停指示点变信号色、治理校验格
  悬停微抬、菜单/折叠/toast 存在感动效、输入 focus 光晕统一；⑥ 卡顶巡游光降亮（.95→.55）
  放慢（9s→11s）、KPI 角标阴影收进格内；⑦ 池页悬停残留旧信号橙 → 电青（全站唯一强调色）；
  ⑧ theme-color 三页统一 #04070d（与冷底一致）；⑨ 空态卡壳治理：模板页空 editor 卡不再
  常驻，编辑器展开时自带卡壳；⑩ Escape 关闭「更多」菜单 + toast 补 role=status 播报。
  验证：go vet 0、go test ./... 29 包全绿、frontend-gate 23/23 PASS、design-audit 15 页
  低对比 0 / 焦点缺失 0 / 溢出 0（最低对比 5.29:1）。
- **双角色闭环：思考模型只出方案，agent 模型负责执行（2026-10-21 老大立规）**：
  立规原话「思考模型只出方案，agent 模型负责执行，不然只出方案没有执行落地的模型无法闭环」。
  改造前每个任务只调一次模型、task.Prompt 原样喂过去：有推理优势的模型被当写稿机，
  擅长执行的 agent 模型拿不到任何结构化指令，验收失败只能同一模型重做（方案层面错了永远修不掉）。
  新增方案层 `internal/blueprint`（提示词 → 解析含截取降级 → 渲染执行指令 + 路径/解释器白名单校验）
  与引擎编排 `engine/plan_stage.go`（选模型、调用、落盘、退化）；`runOne` 改为
  **方案阶段 → 渲染执行指令 → 执行阶段** 两次独立调用，方案模型与执行模型可分别指定
  （`plan.plannerModels` / `plan.plannerTier`，默认 quality）。
  闭环分三级、顺序写死在 `completeTask`：返工有余量 → repair（改实现，便宜先用）；
  返工用尽且重规划有余量 → **replan**（`engine/replan.go`，回到方案层改前提）；
  两者用尽 → degraded。方案层自身失败（429 等）**不判任务死刑**：退化直执行 +
  `PlanDegraded=true` 落盘，面板可区分「方案跑过」与「方案挂了」。
  配置面新增 `plan` 段（enabled/plannerModels/plannerTier/plannerMaxMinutes/replanRounds/
  appendPlanToPrompt），`schemaVersion 5 → 6`，老安装零值由 `NormalizePlan` 按开启补齐。
  证据：新增 `--plan-selftest` 离线自检（桩执行器跑全链，12/12 PASS，不耗额度）+
  21 条单测 + 黑盒分发契约测试。详见 `docs/双角色闭环交付报告-2026-10-21.md`。
- **产物体积归因 + 双向体积护栏 + 清单/趋势双收口（issue #8 四判据收口，2026-10-21）**：
  ① **体积归因**：两天 +83.3%（7,747,072 增至 14,199,296 B）主因 = **在未提交合并工作区上构建**
  （模型池分组 UI、assets 重写等在途改动全部进了旧 EXE），非依赖/代码膨胀；提交态
  （61decb5）构建为 9,650,176 B，对照 10-18 基准真实增量 +24.6%（四项改造 + Prometheus 导出 + 前端改动），
  归类**预期**。② **exeMinBytes 双向判**（判据 3）：基线加 exeMinBytes=7,747,072（=10-18 基准原值，
  锚定上一代完整功能），check_perf.ps1 增判定 1b，ci.yml 的 3MB 硬编码下界改引基线；
  跌破下界=产物缺重大组件（资产丢失/误裁包/半成品），与膨胀同为回归、只是方向相反。
  ③ **SHA256SUMS 读回校验**（判据 2 后半）：CI Verify artifact 步独立比对清单 hash 与实测 hash，
  漂移判红不允许跳过（setup/build.ps1 构建后强制重写清单已在 HEAD 就位）。
  ④ **perf 趋势时间序列**（判据 4）：每次 CI 把 verdict+体积读数落盘 perf-trend/perf-verdict.jsonl
  并 upload 成 artifact（命名含 run_id 防覆盖、retention 90 天）——上界拦翻倍级膨胀，
  趋势拦每次涨 5% 的缓慢贴边。
- **实例回收 + 托盘窗样式修复（2026-10-20 现场两连）**：
  `tests/instance_guard.ps1`（机器级实例盘点 + 有界回收，六条裁决：生产实例只留 1 个 / 用户显式退出后残留 /
  探针超预算 / 探针无预算超 TTL / 全局硬上限 MaxInstances / 探针父死兜底），接入 `tests/keepalive.ps1` 每分钟一跳，
  每次真清理留痕 `runtime/instance-guard.log`。根因：四条拉起链（Run 键 / 计划任务 / SelfHeal / 探针）
  + 单实例锁按 runtime 目录 + 无任何回收者，且 Win32_Process.Create 让「父死子退」失效 —— 现场实测 24 个孤儿实例。
  同时修 `internal/tray/tray_windows.go`：托盘消息窗补 `WS_EX_TOOLWINDOW` + `WS_POPUP` +
  `SWP_HIDEWINDOW`（原先 dwExStyle=0，导致每个实例在任务视图/Alt-Tab 留一张关不掉的空白卡片，
  实测 `ex=0x00000100 TOOLWINDOW=False`），标题改为「内部消息窗口，请忽略」，类名提常量并加 4 个守护测试。
- **首页契约门禁**：`tests/check_homepage.ps1`（产物必须含新页锚点 `home-entries`、不得含旧页锚点 `home-badge`、
  必须有 `UI_PROBE_READY` 通道，且真探针抓 HTTP 首页同为新页），已进 `setup/build.ps1` 构建链与
  `tests/check_all.ps1` 总预检。根因：21:52 的 dist 产物是从旧树构建后覆盖进来的，服务的是旧首页且丢了
  `--ui-probe` 分支，而当时的门禁只看 PE 子系统 + 体积，没有一条判据看页面内容。三态自证 rc=0/1/2。
- **Prometheus 文本格式指标导出（P1-3）**：`internal/web/metrics_prom.go` 提供 `GET /metrics`
  （exposition v0.0.4，纯标准库），把任务状态分布、尝试总数、**按 ErrorKind 的失败分类**、
  任务耗时 sum/count 与进程侧 goroutine/堆/GC 暂停投影成监控系统可直接抓取的形式。
  三条硬约束写在结构里而非靠自觉：样本只能挂在已登记族上（`# HELP`/`# TYPE` 成对写出，
  重复登记复用同族）；浮点经 `promSanitizeFloat` 兜底，绝不产 NaN/±Inf；标签名走白名单、
  标签值按规范转义且非法 UTF-8 先修正、超长按 UTF-8 边界截断。零任务时每个合法状态仍输出
  0 值序列（采集端可预先建面板，不必等序列凭空出现）。新增 8 条判据含逐行 exposition 校验
  与「同 (name,labels) 重复序列」检测（变异测试：删 `# TYPE` 与改回两条同名样本均判红）。
- **性能基线护栏（B8 / L8）**：`docs/benchmark/perf-baseline.json`（数据件，含 provenance、
  上界推导与三个 null 字段的原因）、`docs/benchmark/性能基线口径.md`（逐字段取证命令与三轮
  原始读数）、`tests/check_perf.ps1`（229 行三态门禁：0=PASS / 1=FAIL / 2=UNVERIFIED，
  「没测成」绝不当成「测得过」），并把 `ci.yml` 的产物体积上界从硬编码 40MB 改为引用基线、
  新增 `Perf baseline gate` 步骤。实测三态齐验（真实产物 rc=0、缺参/缺文件 rc=2、
  变异上界 rc=1，基线按 SHA256 逐字节还原）。
- **开源对标报告入主树**：`docs/benchmark/WorkBench-开源对标报告.md`（8 个 peer：
  n8n / dify / AutoGPT / autogen / temporal / river / asynq / windmill，八维对比 +
  P0/P1/P2 清单 + M0-M4 路径），此前只存在于受 `.gitignore` 屏蔽的 `src-backup/`。
- **知识盲区与学习计划**：`docs/知识盲区与学习计划.md`，B1-B8 盲区绑容器实测读数，
  L1-L9 学习路径含精读对象与机器判据。
- **开源化三件套**：`ARCHITECTURE.md`（单页定位/进程模型/包结构/五条架构取舍）、
  `.github/workflows/ci.yml`（vet + `go test -race` + 覆盖率门禁 + 产物校验）、
  `autonomy-e2e.yml`（容器内确定性通道跑完整自主闭环）。
- **issue/PR 协作模板**：`.github/ISSUE_TEMPLATE/`（缺陷报告 / 功能请求 / config.yml）、
  `.github/pull_request_template.md`（判据必须贴命令与真实输出）、
  `.github/labels.json` + `setup/labels.ps1`（12 个正交标签）。
- **零弹窗拉起共用件** `tests/lib_silent_launch.ps1`：按 PE 子系统分流，
  GUI 走 `Win32_Process.Create`（保持脱离作业对象），CONSOLE 走
  `Start-Process -WindowStyle Hidden`。
- **崩溃救援额度（L4）**：`model.Task.CrashRescues` 记录跨崩溃的救援轮次，
  `internal/engine/stuck_rescue.go` 的 `decideRescue` 分流「重新入队 / 判死」，
  状态机新增 `interrupted → failed` 出口。修的是**无界崩溃循环**：
  原先 `Recover()` 把 interrupted 无条件转回 queued，而 `Attempts` 是单次 `runOne`
  内的序号（每轮被覆盖），不能当崩溃预算 —— 于是"一跑就弄死进程"的任务
  配合 KeepAlive 每分钟拉起会无限重投，看起来"一直在干活"实际零产出。
  判死写 `ErrorKind=crash_loop`，面板失败原因分布可区分。
- **社区协作面（L7）**：`.github/ISSUE_TEMPLATE/`（缺陷报告 8 字段 / 功能请求 7 字段 /
  config.yml）、`.github/pull_request_template.md`、`.github/labels.json` +
  `setup/labels.ps1`（12 个正交标签，含本仓特有的 `flaky` / `platform-coupling` /
  `external-constraint`）。模板把本项目的判据文化写成必填项：
  「取不到数写 UNVERIFIED 不要填 0」「必须贴命令与真实输出」「外部约束另打标签」。
- **三条仓库级门禁（已挂 CI 的 Repo hygiene gates 步骤）**：
  `tests/check_changelog.ps1`（Unreleased 必须居首 / 日期必须 YYYY-MM-DD / 版本降序）、
  `tests/check_noninteractive.ps1`（禁 `Invoke-WebRequest` 缺 `-UseBasicParsing`、
  计划任务缺 `-NonInteractive`、以及 Read-Host / Get-Credential / -Confirm 等交互构造）、
  `tests/check_issue_forms.ps1`（Issue Forms 结构：顶层三键、每块 attributes.label、
  dropdown/checkboxes 的 options 必须位于 attributes 之内、contact_links 必须用 about）。
  三条判据均经**变异测试**验证能抓回对应缺陷（不是"跑过了"就算数）。
- **CONTRIBUTING 新增三节**：九 协作流程与判据文化、十 零弹窗纪律、十一 延伸文档。

### Fixed
- **无头执行：测试不再弹窗、测试不再被实例守卫杀掉（2026-10-21 老大指令「不要给我跳出来窗口，无头执行」）**：
  ① **产品层**：`internal/browser` 新增 `WB_HEADLESS`（或 `CI`）判定，命中即**不启动任何浏览器**；
  Edge/rundll32 回落路径补 HideWindow + CREATE_NO_WINDOW；`cmd/workbench` 新增 `--headless`
  （与 `--silent` 同为"不弹面板"，保留旧名是因为既有计划任务已用它）与 `headlessEnv()`
  环境层兜底（`openPanel` 先判 env —— 某处分支漏判 silent 也不会污染桌面）。
  实测：`WB_HEADLESS=1` 起实例，msedge 窗口数增量 **0**。
  ② **测试层**：`hiddenTestCmd`（HideWindow + CREATE_NO_WINDOW + 注入 `WB_HEADLESS=1`）
  接管 `cmd/workbench` 全部子进程 spawn。
  ③ **找出并修掉"测试随机红"的真因**：`go test -c` 编出的测试二进制同样带
  `ProductName=个人AI工作台`（rsrc 被链接进去），命令行又不含 `--ui-probe`/`--soak`
  ⇒ `tests/instance_guard.ps1` 判成**"生产实例重复"** ⇒ 每分钟的 keepalive(`-Apply`)
  **直接 kill 它**。现场表现：`go test ./cmd/workbench` 隔几次整包
  `exit status 0xffffffff`、日志无线索（"测试假红"会让真缺陷被当噪声）。
  修法：Go 测试二进制特征（`-test.` 参数 / `*.test.exe`）纳入"非生产"判据，
  按预算/TTL 回收，绝不按"生产重复"杀。**实测：修复前 2/5 随机红 → 修复后 6/6 全绿**。
- **限速闸门补构造期接线（D9 自查补漏）**：上一批写的是「`New`/恢复/`ReloadConfig`/`SetRunner` 四接线」，
  实测当时只有 3 处调用点（`New()` 里没有）。查的过程发现真实缺口：
  `cmd/workbench/main.go:419` 是第二个引擎入口（一次性 CLI 自检，只 `engine.New` 后直接
  `SelfTestArtifactPipeline`，全程不调 `ReloadConfig`），那条路径跑到真模型时不受任何速率约束。
  补 `New()` 内的 `applyRateLimit`（幂等：主路径随后 `ReloadConfig` 再装一次只是替换令牌桶，
  且 `New`→`SetLogger`→`ReloadConfig` 之间不发请求）⇒ 调用点 3 → 4，表述名实相符。
  新增 `newBareEngine`（复刻该 CLI 路径）+ `TestRateLimiterWiredAtConstruction`。
  **变异验证**：撤掉 `New()` 接线后只有这一条判红、其余 4 条限速用例仍绿 ⇒ 证明它不是
  `ReloadConfig` 用例的重复劳动，而是唯一覆盖 CLI 路径的那条（`0ae60ff`）。
- **语义层绕过执行器（缺陷 D7）**：`judge.run()` 原先每次自己 `executor.New(...)`，于是
  ① 单元测试注入的假执行器（`engine.SetRunner`）管不到语义子任务 —— `go test` 会真的拉起
  DSH Desktop 子进程（取证：`t.TempDir` 残留 `tasks/summary-task-done-e2e` 与
  `tasks/critique-task-done-e2e`，本机同时堆积 20+ 个 DSH 进程；真实单任务 65-148s 而测试
  只 `WaitInflight(5s)` ⇒ 后处理仍在写 `events.jsonl` 时用例结束，Windows 下 TempDir 的
  RemoveAll 报 "used by another process"，engine 包判红、容器与 CI 却绿）；
  ② 生产路径绕过模型池 —— `main.go` 装在 `e.runner` 上的 FailoverRunner（故障转移 + 车道
  健康度）与全局限速闸门对语义子任务全部失效，每个任务额外 2-3 次裸请求不受任何约束；
  ③ 执行钩子的尝试日志对语义子任务失效。修复：`judge.SetRunner` + `engine.SetSemanticRunner`，
  引擎构造与 `SetRunner` 时同步注入。
- **限速闸门生产从未接线（缺陷 D9）**：`internal/ratelimit` + `executor.SetLimiter` + 面板
  `RateLimiterSnapshot` 三件套 2026-10-19 就写好了，但实测**生产没有任何调用点**
  （`ratelimit.New` 全仓仅出现在测试里），`requestsPerMinute=20` 是装饰品 —— 面板显示限速、
  实际不限速，正是 08:00-08:04 每分钟 45-70 次请求打爆免费车道的结构性原因。修复：新增
  `internal/engine/runner_wiring.go` 的 `applyRateLimit`，沿装饰器链下探装配，`New`/恢复路径/
  `ReloadConfig`/`SetRunner` 四处接线；配置热更新会真的改变发出速率。
- **事件文件句柄生命周期（缺陷 D8）**：`executor.run()` 用 `defer eventsFile.Close()`，而
  `consume` 协程可能在 `Run` 返回后仍在写（`wg.Wait` 有 15 秒超时兜底）—— 对已关闭文件继续
  Write 的错误被 `_` 吞掉，事件尾部静默丢失。改为 `sync.Once` 包裹的 `closeEvents`：所有早退
  路径（pipe 接管失败 / 隔离环境准备失败 / `cmd.Start` 失败）由 run 的 defer 释放，正常路径由
  consume 自己收尾，超时兜底路径主动关闭两个读端解阻塞并再给一次有界等待。
  新增 `events_lifecycle_test.go`（含变异测试：撤掉 defer 后改名判红）。
- **自评解析接受无关 JSON（缺陷 D10）**：`extractJSON` 抓「第一个平衡 JSON」，模型回答里若含
  产物协议 manifest（`{"files":[...],"verify":[...]}`）等其它 JSON，`Unmarshal` 到 `Score`
  会全字段零值却返回 nil error ⇒ 自评质量被误判为 0 且 `degraded=false`，上层以为是模型的
  诚实评分。后果链：质量 0 → `ShouldExtend` 判否 → 链不延伸；自主验收按 quality 门槛判失败
  → 闭环不推进（`TestClosedLoopThroughRunOne` 实测红 5.57s）。修复用 key 存在性判据（不是数值
  范围），`quality=0` 仍是合法评分；反向用例守护收紧不误伤。
- **构造期告警永久静默（缺陷 D12）**：`engine.New()` 里 `e.log` 先是 Nop，而 `main.go` 在
  `New()` **返回之后**才 `SetLogger` —— 于是构造期所有「失败只告警不阻断」的告警（工作目录
  越权审计打不开、自主子系统初始化失败）写进 `io.Discard`。现场表现为自主链路没起来、
  `workspace-audit.jsonl` 始终为空，但日志与面板里一条原因都查不到。修复：`warnDeferred`
  缓冲 + `SetLogger` 冲刷（清空后重复 SetLogger 不重写）；同时删掉 `New()` 里那行自杀式的
  `e.log = logging.Nop()` 重设。
- **面板日志拿不到 component（缺陷 D11）**：`logging.At()`/`With()` 只把固定字段加到 slog 侧，
  handler 通道（面板 `/api/logs` 走这条）的 `Record` 另起一路、仅从本次 kv 构造 —— 落盘 JSONL
  有 `component`、面板读数却没有，`ARCHITECTURE.md` 承诺的「component 字段可按来源过滤」在
  面板链路上是假的。修复：`Logger.attrs` 自存一份并在 emit 时并入（本次 kv 覆盖同名键，与
  slog 语义一致）；`copyAttrs` 返回独立副本，防兄弟子日志器串字段。logging 覆盖 82.8% → 93.9%。
- **无人值守弹窗挂死**（老大 2026-10-19 17:29 反馈"这个东西后台静默不要弹出窗口"）：
  守护计划任务每分钟拉起 `tests/keepalive.ps1`，其中一条 `Invoke-WebRequest` 缺
  `-UseBasicParsing` —— PowerShell 5.1 因此调用 IE 引擎解析响应页，而面板页含 `<script>`
  （实测 2 处），触发 "Security Warning: Script Execution Risk … Do you want to continue?"
  等人工按键；同时任务注册命令行缺 `-NonInteractive`，两者叠加 = 桌面弹窗 + 任务永久挂起。
  修复：`-UseBasicParsing` + `$ConfirmPreference="None"` + `$ProgressPreference` +
  注册补 `-NonInteractive` 与 `Settings.Hidden`；新增 `tests/check_noninteractive.ps1`
  静态门禁（已挂 CI，变异测试验证可抓「只在注释里提 flag」的洗绿形态）。
- **自主闭环决策留痕顺序**（issue #1）：`autonomy.advanceLocked` 与 `bootstrapLocked`
  的恢复路径原先「先切活跃目标、后写 nextGoal 决策」，而两者写不同文件
  （goals.json / decisions.jsonl，非原子），留下一个可观测窗口 —— 面板与
  `--selftest` 判据 7b 会看到「目标已切换但台账无推导记录」，CI 上
  `TestRunOneDrivesAutonomyClosedLoop` 的偶发红即源于此。现收敛为
  `writeLedgerThenActivate`（先留痕后生效），并由顺序守卫测试钉死。
- **非 Windows 删除无安全阀**（issue #2）：`retention.Trash` 的 `!windows` 分支此前是裸
  `os.RemoveAll`，危险路径判定只存在于 Windows 分支 —— 实测 `Trash("/")` 在 Linux
  会递归删掉文件系统根。现 `refuseDangerousPath` 上移为平台无关实现，两条删除路径同过闸。
- **产物子系统门禁**：`setup/build.ps1` 校验 EXE 的 PE `Subsystem` 必须为 2(GUI)，
  非 GUI 直接判红。控制台子系统 EXE 被计划任务/`Win32_Process.Create` 拉起时
  Windows 必分配可见控制台窗口（桌面反复弹窗的根因）。
- **测试硬编码 Windows 工具链**（issue #2）：engine / verify 的 10 个用例改为解释器探针
  （pwsh→python3→python→node）；Windows 上全缺时 `t.Fatal`，禁止 `t.Skip` 洗绿。
  Linux 容器 `go test ./...` 从「3 包 10 用例 FAIL」转为全绿。
- **失败判据被偷换**：`TestMaterializeArtifactsFail` 原先只断言 `Pass == false`，
  而解释器缺失导致的「启动失败」同样 Pass=false —— 测试绿但什么都没验。
  现补断 `FailReason` 必须含「退出码 2」。

### Changed
- **仓库卫生：可再生产物入 .gitignore（`bce8e53`）**：`git ls-files --others` 实测 **53,751 个
  untracked**（xtmp 22,397 / dist 13,400 / `.workbuddy/gocache` 13,353 / runtime 3,792 /
  `tests/docker-autonomy/evidence` 507+）⇒ `git status` 一屏滚不完，「目录卫生」类判据读数全是噪音。
  只忽略可再生的缓存与跑出来的产物；**不忽略整个 `.workbuddy`**（`git ls-files` 实测 `memory/`
  下 5 个 `reply-to-lead*.md` 受跟踪，一把梭会把项目记忆藏起来）。
  安全断言（防「忽略=删除」事故重演）：把全部 1,838 个已跟踪文件喂给 `git check-ignore --stdin`
  ⇒ 被新规则命中 **0 个**；`git status` 里 'D' 条目 **0**。结果 untracked **53,751 → 210**。
  如实登记边界：`/dist/` 比原有条目更宽，将来若要在 dist 下**新增**交付物需 `git add -f`。
- **执行层抽象收敛（L5）**：`engine.SetLogger` 原先断言 `e.runner.(*executor.Runner)` ——
  接口里带具体类型等于没脱离结构体，一旦执行器被 `FailoverRunner` 包住（`main.go` 的做法）
  断言就不匹配，尝试日志对模型池路径静默失效。改为沿 `Inner()` 链找 `AttachHooks` 能力接口
  （`executor.Runner.AttachHooks` + `modelpool.FailoverRunner.AttachHooks`），新增执行通道只要
  实现 `RunnerContract + AttachHooks` 就自动获得尝试日志。三条守护测试（含 nil 链不 panic、
  BeforeRun 中止归 `boot_failed`），变异测试验证「回退成具体类型断言」必判红。
- **`limiter_integration_test.go` 判据改相对量**：原断言「单次 Run < 2 秒」在全量并行
  （`go test ./...`）下实测 4.12 秒而判红 —— 那 4 秒是 `linkDir` spawn cmd.exe + 建目录 +
  写覆盖层的宿主排队开销，与「限流器是否等待」无关，属判据测错对象。改为测**两次连续 Run 的
  相对差**（满桶首次立即通过、第二次才被 rate 拖住），宿主整体变慢时两次一起变慢不受影响。
- **`limiter_snapshot_test.go` 判据方向纠正**：改造前断言「未装闸门 = 快照零值」，把 D9 那个
  「生产从未接线」的缺陷当成了正确行为。现在要求装配后读数非零（零值即回归）。
- **覆盖率提升并固化基线**（issue #3）：`internal/chain` 0% → 88.6%、
  `internal/judge` 0% → 63.7%（含空输入 / 非法 JSON / 越界钳位 / 40 万字符超长负面用例）；
  `build.ps1 -PerPackageCover` 新增 `chain=80`、`judge=55` 诚实基线（只许抬不许降）。
- CI 触发分支补 `master`（建仓默认分支实为 master，此前 workflow 只写 main 导致 push 不触发）。
- Windows 8.3 短路径（`RUNNER~1`）导致的 CI 假失败：路径比较统一经 `evalExisting` 归一。
- `store.Update` 由原地写改为换指针，消除与 `runOne` defer 的真 data race（`-race` 哨兵抓出）。

### Changed
- **模型池清单改为按渠道分组管理**（老大 2026-10-20 反馈：「控制台模型池页面乱七八糟的，
  做成像 Our Free Model 一样统一分类管理的」）。改造前 37 个模型平铺成一张 9 列表，
  不分渠道、不看健康度，一屏全是行 —— 「乱七八糟」的具体来源是：OFM 车道 13 个里有 10 个
  是「上游判定不可用」的自动停用项，与真正能用的 3 个混在一起，把可用的埋掉了。
  现在按 `Spec.Group` 分四张卡片（OFM 免费 / Kilo 免费池 / EAC 协付 / 网关专属车道），
  每张卡片头部是该组聚合健康度（`pill` 定色 + 可用/总数 + 下线 + 停用 + 成功/失败 + 均延迟），
  形态对齐 Our Free Model 设置页的渠道卡片。组内再分两层：**能调度的默认展开**，
  **停用/下线的折进「另 N 个停用/下线」**。序号改组内独立编号（原跨组连续编号会让第 2 组
  第一行显示「14」，读起来像「全池第 14 个」）。分组维度由服务端 `Spec.Group` 给出，
  前端不自己推断，避免两处口径漂移。
  实测（37 条真实池数据、`--ui-probe` 隔离实例）：4 分组卡片、4 个默认展开、2 个默认折叠，
  组头读数 `OFM 免费 可用3/13 停用10 成功/失败3/1`、`Kilo 免费池 可用17/17 均延迟86.9s`、
  `EAC 协付 可用5/5`、`网关专属车道 可用0/2 停用2`。`go build`/`go vet` rc=0、
  `go test ./...` **27 包全 ok**（含 `TestPoolTouchContract` 窄屏 data-label 契约未破）。
  改动文件：`internal/web/assets_pool.go`（`renderList` 重写 + `poolRow` 抽函数 + `poolFold` 默认展开）、
  `internal/web/pool_page.go`（新增 `.pool-group*` 页内样式，未污染全局 `assets_css.go`）。

### Fixed
- **「任务/产出/模板/设置 子页面被挡住」**（老大 2026-10-20 二轮反馈：「除了总览界面没问题，
  任务、产出、模板、设施子页面都被挡住了」）。**这不是错觉也不是缓存**：老大窗口是 1040×613
  物理像素，而本机屏幕 175% 缩放（`devicePixelRatio=1.75`），页面真实 CSS 视口只有 **594×350**；
  在这么矮的视口里，顶栏+页签条吃掉 **239px = 68% 屏高**，子页只剩 **99px** 可见。
  总览首屏是 4 个矮小的 KPI 数字，能露出来；其余四页首屏是卡片标题区，几乎全被推出视口 ⇒
  体感「被挡住」。（先按 z-index/遮挡查了两轮全绿，因为 `elementFromPoint` 全命中正确元素；
  真正的判据是**内容可见高度占视口比例**。）
  修复：`@media (max-width:720px)` 重做窄屏排版 —— 顶栏由「三行堆叠」改单行（品牌只留 9px 色块、
  状态与按钮同排）、页面标题 30px→15px、页签 44~46px→38px；导航总高 **239px → 78px（-67%）**，
  内容可见 **99px → 222px（+124%）**。同时修两个连带缺陷：① `@media (pointer:coarse), (max-width:720px)`
  把「窗口窄」误当「手指触屏」，桌面窄窗口页签被强制 44px，已拆成只有真触屏才 44px；
  ② 模型池顶栏 4 个按钮在 390px 下把页面撑宽 18px（`scrollWidth=408 > clientWidth=390`），
  `.topactions` 改 `flex-wrap:wrap + min-width:0`，实测 `overflowX=0`。
  新增 `pane` 前端契约（逐页签量：内容是否钻进导航区 `overlap≤1px`、页签条是否被压扁
  `tabsHeight≥30`、**导航占视口 `navRatio≤45%`**），门禁用例 **20 → 23**，新增
  `pane-geometry`(1440×900) / `pane-geometry-narrow`(1040×613) / **`pane-geometry-realwindow`(594×350 = 问题现场)**。
  双向反证：修复前 v1.3.2 在新判据下 **FAIL**（`navRatio=55.1`、1040 档模板页 `paneTextLen=0`），
  修复后 **PASS 23/23**（`navRatio=22.3%`、五页 `overlap=0`、`visible=222px`）。
  `go vet` rc=0、`go test ./...` **27 包全 ok**；发布件 v1.3.3（SHA256 `C92E074F…62CC3`）。
  详见 `docs/子页面被挡住-根因与修复-2026-10-20.md`。
- **页签条被 flex 压成 0 高度（真渲染 bug）**（老大 2026-10-20 二轮截图反馈「页签像被切掉半截」）：
  CDP 实测 `.tabs` 的 `height = 0`，而内部 `.tab` 高 46px，页签溢出父容器叠到下方卡片上。
  根因是 flexbox 规范细节：`body.admin` 为 `flex-direction:column`，而 `.tabs` 带 `overflow-x:auto`，
  **overflow 非 visible 的 flex item 自动最小尺寸为 0**，于是被内容挤扁（滚动时 z-index 4 < 吸顶栏 20 更明显）。
  修复：`.tabs` 显式 `flex:0 0 auto` + `min-height:46px`。实测改前 `tabs.h=0` → 改后 `tabs.h=46`
  （同一探针、同一 1040×613 窗口）。新增 `TestTabsCannotBeSquashedByFlex`（只取 `.tabs{}` 规则块断言 +
  剥离反证防恒真）。同类风险已排查：`.content` 无 overflow、`.table-scroll`/`.logbox` 在 block 卡片内，
  只有 `.tabs` 同时是 flex item 且自带 overflow。
- **任务页空态摆了两张空卡**（同一张截图）：三张卡里两张只写「队列是空的」「当前没有执行中」，
  而此刻唯一能做的事（加任务）还藏在折叠里。改为全空时只留一张卡 = 状态摘要 + 一句解释
  「引擎会自动从模板生成，不需要你手动加」+ **直接摊开**的填表；有任务时才回到表格 + 折叠表单。
  新增 `TestQueuePageHasActionWhenEmpty`。发布件 v1.3.2（SHA256 `9286B468…1CF3`）。

### Changed
- **界面简化：管理面板 10 个页签收敛为 5 个，次级信息全部折叠**（老大 2026-10-20 反馈
  「页面乱七八糟的东西太多了，不够简洁，对用户不友好」）。这不是换皮，是信息架构手术：
  旧版把「系统能吐出的所有数字」平铺在一屏 —— 总览 10 张 KPI 卡 + 5 张明细卡顺排（1414px）、
  模型池 9 列表格 + 常显 12 字段添加表单（1228px）、治理台 hero 结论带下面再念一遍同内容的
  banner，用户要的信息反而被淹没。
  改法五条：① 页签 **10 → 5**（总览/任务/产出/模板/设置；自主+治理并入总览，评估+工作目录+
  审计日志并入设置），旧深链 `#logs`/`#evaluate`/`#workspace` 经 `LEGACY_TABS` 映射、
  hash 原样保留，老书签不失效；② 首屏只留 **4 个真关心的读数**（今日产出/排队/成功率/累计完成），
  其余降级进折叠块；③ 新增 `<details class="fold">` 折叠件，模型健康度/运行时资源/失败原因/
  评估权重/换工作目录/访问审计/操作流水/失败任务一律收起；④ 空态改成一句话人话
  （「队列是空的 —— 引擎会自动从模板生成任务」），不再是整块空白；⑤ 顶栏 6 按钮 + 3 链接
  收敛为 3 按钮 + 1「更多」菜单。
  治理台顶栏同步统一为「刷新 + 立即采样 + 更多」，跨页入口收进菜单。
  模型池同步瘦身：9 列 → 6 列（删「延迟」「最近错误/备注」两个半数行为空的列，合并成模型名
  下的小字），行内操作 6 → 4 高频 + 「⋯」收纳恢复/删除，添加表单从常显改折叠（高级参数再折一层），
  车道表并入总览、状态日志折叠。治理台删除与 hero 完全重复的 banner，首启无数据时给
  「点立即采样」的说明而非铺 7 张全 0 卡，四个取证明细卡折叠。
  判据同步（简化不许回退，也不许把功能删掉）：新增 `TestAdminTabsStaySimplified`
  （页签清单双向钉死 + 旧深链映射必须存在）、`TestTopbarHasSingleMoreMenu`、
  `TestAdminPageHasWorkspaceSection`（工作目录从独立页签变设置页一块，但 `fillWorkspace`
  能力必须在）；前端 `ADMIN_TABS` 常量 + tabs 契约数量/清单双向校验，`assertions_selftest`
  新增 4 条反证（页签被加回 / 丢主入口 / 旧书签成死路 / 旧书签丢 hash），cdp_probe 新增
  `legacy-hash-admin` 用例，门禁用例 19 → **20**。
  顺手修掉门禁自身的一处假绿隐患：探针已增用例但 `frontend-gate.mjs` 的 `EXPECTED_CASES`
  仍停在 19（回执显示「20/19」），该常量是「结果集完整」的唯一判据，写小即允许漏跑仍判 PASS；
  已改 20 并加启动前自检（从探针源码数用例，不符即 exit 3 硬停），双向反证通过。
  非技术用户视角补做（v1.3.1）：把模型池/设置页的行话改成白话并附代价说明 ——
  线协议→「接口类型（OpenAI 标准对话 / Responses / Anthropic 格式）」、Tier→「用途偏好
  （质量优先（慢一点，写得好）/ 均衡 / 速度优先）」、车道→「线路来源」、BaseURL→「接口地址」、
  DSH profile→「运行配置（一般不用改）」；模板库从 24 行全平铺改为**默认只显示价值先验最高的
  8 个 + 「还有 N 个，全部展开」**并加搜索框（按名/领域过滤，250ms 去抖）。
  新增 `TestTemplatesLibraryIsSearchableAndFolded`、`TestFormLabelsAvoidJargon`（后者反向禁止
  ">Tier<"/"线协议<select"/"DSH profile</td>" 回潮）。
  实测：`go vet ./...` rc=0；`go test ./... -count=1` **27 包全 ok**；候选件与发布件前端门禁
  均 **PASS ok=20 fail=0 unverified=0**；量化设计审计 15 页低对比 **0 处**（最低 5.29:1）、
  焦点缺失 0、横向溢出 0；admin 总览页高 1414px → **471px**。发布件 `dist\workbench.exe` v1.3.0
  （换件前备份 `workbench.exe.bak-pre-simple-*`）。详见 `docs/界面简化-信息架构收敛-2026-10-20.md`。


### Fixed
- **托盘「退出工作台」关不掉**（老大 2026-10-19 反馈「程序怎么我点退出工作台关不掉了」）：
  点退出后进程确实退了，但**≤60 秒就被外部守护重新拉起并自动开跑**，用户观感＝关不掉。
  根因是「生产者与消费者各缺一半」：产品侧早有表达能力（`engine.MarkUserQuit` 落
  `state.json` 的 `userQuit` + `runtime/user-quit.flag`），但**只有 `--quit`/面板
  `/api/engine/quit` 这条 HTTP 路径在用**，托盘这条不经 HTTP 的入口漏了；
  而守护 `tests/keepalive.ps1`（计划任务 `WorkBenchKeepAlive`，每分钟一次）创建至今
  **从未读取过该标记**，把 `stopped`/`idle` 一律当「该拉起来」。
  实测复现（旧二进制 + 真实托盘命令 `WM_COMMAND 1006`）：退出后 60s 被拉起开跑、120s 又拉一次。
  修复：① `main.go` 新增 `quitByUser`（先 `MarkUserQuit` 再优雅退出），托盘 `OnQuit` 接入；
  **signal(SIGTERM) 路径刻意不落标记**——被动中断重启后理应接续，落标记会破坏 7x24。
  ② `tests/keepalive.ps1` 新增 `Test-UserQuit`（flag 主判据 + `state.json` 回退），
  **两处**拉起判定都加 `-not $userQuit` 守卫；脚本**绝不自己清标记**（清除点只在
  `Engine.Start`，守护替用户清标记等于替用户改主意）。
  验证：隔离实例端到端 `tests/quit_acceptance.ps1` **13 PASS / 0 FAIL**（含负向对照：
  清标记后守护必须拉起，证明是「尊重意愿」而非「关掉守护」）；变异测试
  `tests/quit_mutation_check.ps1` 证明去掉守卫即复现缺陷；真实安装盘上托盘退出后
  `userQuit=true` **连续 105 秒（≥2 个守护周期）未被拉起**（标记存活比「没看到进程」更强：
  只有 `Engine.Start` 会清它，它还在就说明守护没动手）。
  详见 `docs/缺陷-托盘退出关不掉-2026-10-19.md`。
- **首页入口「启动中点它白点」**（2026-10-19 目标轮：添加首页进入子页面的入口）：首页唯一按钮就是子界面入口，  但实测抓到两类会让入口失效的时序缺陷 —— ① busy 态被 `lock:true` 禁用，disabled 的按钮连 `click` 都不派发；  ② 开屏 900ms 自动启动在途时，用户这一击被 `if (inflight) { return; }` 直接吞掉，页面停在首页。  取证：CDP 直连把 `/api/engine/start` 拖慢 6s 造出在途窗口，点击后 9s 内 `location.href` 从未离开 `/`（`landed-not-admin`）。  修复：busy 改 `lock:false`（防重入交给 inflight），在途点击记 `pendingEnter`，启动收敛后补一次 `enterPanel()`——吞请求不吞意图。  判据：新增 `home_entry_contract_test.go`（正则断言，抗视觉重做的空格漂移）+ 前端探针 `home-enter` 用例  （`tests/ui/probes/enter.js` 真实点击 → 轮询落地 URL），门禁用例 18→19。实测：`go vet ./...` rc=0、  `go test ./internal/web ./cmd/workbench` 全绿、`frontend-gate` 19/19 PASS、busy 窗口点击端到端落到 `/admin?token=`。
- **快捷方式「点了打不开」**（老大 2026-10-19 反馈）：桌面快捷方式写死了 `--silent`，
  而该参数语义是"服务化实例：不弹面板"，于是双击后进程与端口都正常，
  **桌面上却什么都不出现**。实测对照：`--silent` 新起可见窗口 0 个；
  不带则 1.7s 内出现面板窗口。修复：快捷方式 `Arguments=""` + `WindowStyle=1`，
  回读校验从"只看 TargetPath"升级为 TargetPath+Arguments 双校验；
  `ensureShortcuts` 从"跑过一次永远跳过"改为每次启动校验并修复（否则旧快捷方式永不被纠正）。
  详见 `docs/缺陷-点击打不开与托盘无图标-2026-10-19.md`。
- **托盘图标永不出现**（老大 2026-10-19 反馈「状态栏也没有图标」）：安装目录
  `D:\workdesktop` 被文件系统沙箱打上 **Low 完整性标签**，从其启动的进程继承 Low IL，
  被 UIPI 拒绝向 Medium 的 Explorer 注册托盘图标（`NIM_ADD` → `Access is denied`）。
  取证为排除法：同一二进制在 `C:\Temp`/`D:\`/`D:\app-cache` 全部 `ret=1 OK`，
  只有 `D:\workdesktop\**` 失败；且 Low 进程自己修不了（`icacls` 返回 1299）。
  四层处置：① 目录标签提升为 Medium（实测图标立即出现）；② `tests/keepalive.ps1`
  每分钟自愈（Medium 上下文）；③ `internal/tray/integrity_windows.go` 进程内自检 +
  失败原因经 `tray.Options.OnWarn` 上报日志与事件流（杜绝"静默少一个图标"）；
  ④ `setup/build.ps1` 新增完整性标签门禁。顺带：托盘图标改为取 EXE 自带图标
  （原为 `IDI_APPLICATION` 通用白板图），并接 `TaskbarCreated` 广播在 Explorer
  重启后重新注册。
- **`--silent` 未覆盖「已在运行」分支**：守护/压测/自愈用 `--silent` 撞上既有实例时，
  原实现仍会往桌面弹面板（老大反复反馈"一直在跳"的来源之一）。现两处面板调用点
  统一经 `shouldOpenPanel(silent)` 判断，并新增 `已打开面板：` 输出标记，
  使该行为可被黑盒判据抓住（双向判据 + 变异测试验证可证伪）。
- **桌面快捷方式被临时目录副本劫持（复发性「打不开」根因）**：`isCanonicalExe` 原判据
  只判「文件名 + 同目录数据根」，而隔离 e2e 为了模拟真实实例，**正是**在
  `t.TempDir()` 里复制二进制并建出 `runtime/`、`workbench-output/` —— 形状完全一致。
  于是只要机器上跑过一次端到端测试，`ensureShortcuts` 就把桌面快捷方式改写到
  `D:\app-cache\temp\Test* 002\workbench.exe`（该目录随后被测试框架清掉），
  老大双击就是"打不开"。本轮实测抓获：快捷方式 TargetPath 指向已不存在的临时路径。
  修复：判据加第三条「不落在系统临时目录内」，并拆成 `hasInstallShape` + `underTempDir`
  两条正交判据分别可测（合成判据在测试里恒假）。变异测试验证：去掉临时目录排除即判红；
  连跑两轮全量测试后快捷方式始终指向 `dist\workbench.exe`。
  这也解释了该现象为何此前"修好了又复发"——不是同一 bug 反复，而是两个 bug 轮流生效。
- **产物 MOTW 安全警告**：带 `Zone.Identifier` 的 EXE 双击必弹「无法验证发布者」，
  属"必须人工点一下"的步骤，无人值守下直接卡死启动（实测 ShellExecute 报
  `The operation was canceled by the user`）。`setup/build.ps1` 新增 MOTW 门禁：
  带标记即 `Unblock-File`，清不掉则构建判红。

### Added
- **首页子页面入口条**（老大 2026-10-19 当轮指令：「其它子页面的入口在哪？出现在这个页面上」）：
  首页按钮下方新增三个可见入口「管理面板 / 模型池 / 治理」（`.home-entries`，href 服务端注入 token）。
  旧「隐藏后门 ·」契约随之作废——`TestIndexPageMinimalContract`/`TestHiddenBackdoorIsDotOnly` 改写为
  `TestIndexPageEntryContract`（1 按钮 + 3 可见入口 + 禁 aria-hidden/tabindex=-1 + 禁表单），
  占位符 6→8、探针 HOME_DEFAULTS anchors 1→3、textVisibleLen 阈值改走期望常量（曾硬编码 6 致自测假红）。
  实测：`go test ./internal/web` 全绿；前端门禁 19/19 PASS；三个入口逐一真实点击均带 token 落地对应子页；
  发布件换件重启后 17890 活实例复验（入口可见 + 点「模型池」→ `/modelpool?token=`）。
- **开源对标报告入主树**：`docs/benchmark/WorkBench-开源对标报告.md`（8 个 peer：
  n8n / dify / AutoGPT / autogen / temporal / river / asynq / windmill，八维对比 +
  P0/P1/P2 清单 + M0-M4 路径），此前只存在于受 `.gitignore` 屏蔽的 `src-backup/`。
- **知识盲区与学习计划**：`docs/知识盲区与学习计划.md`，B1-B8 盲区绑容器实测读数，
  L1-L9 学习路径含精读对象与机器判据。
- **开源化三件套**：`ARCHITECTURE.md`（单页定位/进程模型/包结构/五条架构取舍）、
  `.github/workflows/ci.yml`（vet + `go test -race` + 覆盖率门禁 + 产物校验）、
  `autonomy-e2e.yml`（容器内确定性通道跑完整自主闭环）。
- **issue/PR 协作模板**：`.github/ISSUE_TEMPLATE/`（缺陷报告 / 功能请求 / config.yml）、
  `.github/pull_request_template.md`（判据必须贴命令与真实输出）、
  `.github/labels.json` + `setup/labels.ps1`（12 个正交标签）。
- **零弹窗拉起共用件** `tests/lib_silent_launch.ps1`：按 PE 子系统分流，
  GUI 走 `Win32_Process.Create`（保持脱离作业对象），CONSOLE 走
  `Start-Process -WindowStyle Hidden`。
- **崩溃救援额度（L4）**：`model.Task.CrashRescues` 记录跨崩溃的救援轮次，
  `internal/engine/stuck_rescue.go` 的 `decideRescue` 分流「重新入队 / 判死」，
  状态机新增 `interrupted → failed` 出口。修的是**无界崩溃循环**：
  原先 `Recover()` 把 interrupted 无条件转回 queued，而 `Attempts` 是单次 `runOne`
  内的序号（每轮被覆盖），不能当崩溃预算 —— 于是"一跑就弄死进程"的任务
  配合 KeepAlive 每分钟拉起会无限重投，看起来"一直在干活"实际零产出。
  判死写 `ErrorKind=crash_loop`，面板失败原因分布可区分。
- **社区协作面（L7）**：`.github/ISSUE_TEMPLATE/`（缺陷报告 8 字段 / 功能请求 7 字段 /
  config.yml）、`.github/pull_request_template.md`、`.github/labels.json` +
  `setup/labels.ps1`（12 个正交标签，含本仓特有的 `flaky` / `platform-coupling` /
  `external-constraint`）。模板把本项目的判据文化写成必填项：
  「取不到数写 UNVERIFIED 不要填 0」「必须贴命令与真实输出」「外部约束另打标签」。
- **三条仓库级门禁（已挂 CI 的 Repo hygiene gates 步骤）**：
  `tests/check_changelog.ps1`（Unreleased 必须居首 / 日期必须 YYYY-MM-DD / 版本降序）、
  `tests/check_noninteractive.ps1`（禁 `Invoke-WebRequest` 缺 `-UseBasicParsing`、
  计划任务缺 `-NonInteractive`、以及 Read-Host / Get-Credential / -Confirm 等交互构造）、
  `tests/check_issue_forms.ps1`（Issue Forms 结构：顶层三键、每块 attributes.label、
  dropdown/checkboxes 的 options 必须位于 attributes 之内、contact_links 必须用 about）。
  三条判据均经**变异测试**验证能抓回对应缺陷（不是"跑过了"就算数）。
- **CONTRIBUTING 新增三节**：九 协作流程与判据文化、十 零弹窗纪律、十一 延伸文档。

### Fixed
- **无人值守弹窗挂死**（老大 2026-10-19 17:29 反馈"这个东西后台静默不要弹出窗口"）：
  守护计划任务每分钟拉起 `tests/keepalive.ps1`，其中一条 `Invoke-WebRequest` 缺
  `-UseBasicParsing` —— PowerShell 5.1 因此调用 IE 引擎解析响应页，而面板页含 `<script>`
  （实测 2 处），触发 "Security Warning: Script Execution Risk … Do you want to continue?"
  等人工按键；同时任务注册命令行缺 `-NonInteractive`，两者叠加 = 桌面弹窗 + 任务永久挂起。
  修复：`-UseBasicParsing` + `$ConfirmPreference="None"` + `$ProgressPreference` +
  注册补 `-NonInteractive` 与 `Settings.Hidden`；新增 `tests/check_noninteractive.ps1`
  静态门禁（已挂 CI，变异测试验证可抓「只在注释里提 flag」的洗绿形态）。
- **自主闭环决策留痕顺序**（issue #1）：`autonomy.advanceLocked` 与 `bootstrapLocked`
  的恢复路径原先「先切活跃目标、后写 nextGoal 决策」，而两者写不同文件
  （goals.json / decisions.jsonl，非原子），留下一个可观测窗口 —— 面板与
  `--selftest` 判据 7b 会看到「目标已切换但台账无推导记录」，CI 上
  `TestRunOneDrivesAutonomyClosedLoop` 的偶发红即源于此。现收敛为
  `writeLedgerThenActivate`（先留痕后生效），并由顺序守卫测试钉死。
- **非 Windows 删除无安全阀**（issue #2）：`retention.Trash` 的 `!windows` 分支此前是裸
  `os.RemoveAll`，危险路径判定只存在于 Windows 分支 —— 实测 `Trash("/")` 在 Linux
  会递归删掉文件系统根。现 `refuseDangerousPath` 上移为平台无关实现，两条删除路径同过闸。
- **产物子系统门禁**：`setup/build.ps1` 校验 EXE 的 PE `Subsystem` 必须为 2(GUI)，
  非 GUI 直接判红。控制台子系统 EXE 被计划任务/`Win32_Process.Create` 拉起时
  Windows 必分配可见控制台窗口（桌面反复弹窗的根因）。
- **测试硬编码 Windows 工具链**（issue #2）：engine / verify 的 10 个用例改为解释器探针
  （pwsh→python3→python→node）；Windows 上全缺时 `t.Fatal`，禁止 `t.Skip` 洗绿。
  Linux 容器 `go test ./...` 从「3 包 10 用例 FAIL」转为全绿。
- **失败判据被偷换**：`TestMaterializeArtifactsFail` 原先只断言 `Pass == false`，
  而解释器缺失导致的「启动失败」同样 Pass=false —— 测试绿但什么都没验。
  现补断 `FailReason` 必须含「退出码 2」。

### Changed
- **覆盖率提升并固化基线**（issue #3）：`internal/chain` 0% → 88.6%、
  `internal/judge` 0% → 63.7%（含空输入 / 非法 JSON / 越界钳位 / 40 万字符超长负面用例）；
  `build.ps1 -PerPackageCover` 新增 `chain=80`、`judge=55` 诚实基线（只许抬不许降）。
- CI 触发分支补 `master`（建仓默认分支实为 master，此前 workflow 只写 main 导致 push 不触发）。
- Windows 8.3 短路径（`RUNNER~1`）导致的 CI 假失败：路径比较统一经 `evalExisting` 归一。
- `store.Update` 由原地写改为换指针，消除与 `runOne` defer 的真 data race（`-race` 哨兵抓出）。

## [1.3.0] - 2026-10-19

### Added
- **WAL 事件重建（P2-3）**：队列写路径从"每次迁移全量重写 queue.json"改为
  "追加 `queue.wal.jsonl` + 定期压缩成快照"。启动时按「快照 ⊕ WAL 回放」重建内存状态，
  崩溃后只丢未落盘的最后半行（回放时保守丢弃坏行及其后续）。
  - 新增 `store.Savepoint()` 语义：写快照 → fsync → 截断 WAL（任一步崩溃不丢数据）
  - 压缩阈值默认 2000 条，可在 `wal.threshold` 调整
  - 实测：100 次迁移 WAL 均 759 字节/条，而全量快照 100,786 字节 ⇒ 旧实现每次写放大 ~133×
- **CONTRIBUTING.md**：硬约束、本地环境、常用命令、提交前检查清单、代码组织约定、提交信息规范、发布流程、排障入口。
- **CHANGELOG.md**：按 Keep a Changelog 回填 v1.0.0 / v1.1.0 / v1.2.0 三个版本。

### Changed
- **executor 纯逻辑抽取（P1-2 收尾）**：把 `Run` 里的参数拼装、环境变量构造、超时解析、
  stderr 摘要、结果判定抽成纯函数（`BuildArgs`/`BuildArgsWithPatches`/`BuildEnv`/`ResolveTimeout`/
  `FinalizeResult`/`stderrSummary`/`effectiveMaxBytes`/`normalizeConcurrency`），`Run` 只留编排。
  executor 覆盖率 **14.4% → 27.4%**。
- `setup/build.ps1` 覆盖率基线更新：executor 14→26、新增 modelpool=49。
- `tests/e2e.ps1` 加固（应对同机并发会话互相杀进程）：
  1. 进程清理只针对「本验收目标 Exe 路径」，不再按进程名全杀；
  2. 新增 `Invoke-Api`/`Refresh-Session`：连接被拒时重新解析 panel.url 与 token 后重试；
  3. 非空产出率改为只看「本轮新产出」任务，历史遗留短件不计入（避免判据被历史数据绑架）。

### Fixed
- `store` 在 Windows 上长期持有 WAL 文件句柄会导致测试临时目录清理失败（`unlinkat: file in use`）——
  改为每次追加 open→write→sync→close，不持长期句柄。

## [1.2.0] - 2026-10-19

### Added
- **结构化日志（P1-1）**：新增 `internal/logging` 包，基于标准库 `log/slog` 输出单行 JSON 到
  `<DataRoot>/logs/workbench.jsonl`；提供 `At(component)` 子日志器、`Redact` 凭据打码、级别过滤。
- **执行钩子与执行器接口（P1-2）**：`executor.Hooks`（`BeforeRun`/`AfterRun`，钩子 panic 被 recover 且不影响主流程）；
  新增 `executor.RunnerContract` 接口与 `Engine.SetRunner` 注入点，引擎不再依赖具体执行器结构体。
- **指标接口（P1-3）**：`GET /api/metrics` 返回堆内存/Alloc/goroutine/句柄/GC、任务聚合、
  最近 30 条尝试台账与资源阈值上限；面板概览页新增「运行时指标」卡片。
- **失败原因分布（P1-4）**：`Engine.FailureBreakdown()` + `GET /api/failures`，
  按 `ErrorKind` 聚合失败任务（count/lastAt/samples，空态返回 `empty: true`）；面板新增「失败原因分布」卡片。
- **CI（P2-2）**：`.github/workflows/ci.yml`（windows-latest）：`go vet` → `go test -race` →
  `setup/build.ps1`（含覆盖率门禁）→ 产物校验（存在性/大小区间/`--version`/`--diagnose`）。
- 验收脚本新增第 9 段 OBSERVABILITY（12 项断言）。

### Changed
- `setup/build.ps1` 覆盖率门禁支持 `-PerPackageCover` 分档基线；默认阈值 55%。
- 引擎日志统一带 `component=engine` 字段，可按来源过滤。

### Removed
- 下线三处各自为政的裸日志写入：`engine-debug.log`（engine.debugf）、
  `supervisor.log`（自愈记录）、`crash.log`（panic 兜底）；内容并入 `workbench.jsonl`。

### Fixed
- 编辑/日志字段口径不一致导致的排障困难（同一事件分散在三个文件、格式各异）。

## [1.1.0] - 2026-10-18

### Added
- **错误分类重试策略**：新增 `internal/retry`（11 类失败原因 → retry/fail/cancel 三分判定，
  指数退避 + 向下抖动 + 可重试白名单；旧 `retryBackoffMs` 列表配置完全兼容）。
- **执行尝试台账**：`tasks/<id>/attempts.jsonl` 追加写，逐次记录 attempt/model/kind/action/delay/elapsed。
- **优雅停机两段 drain**：新增 `executor.shutdownGraceSeconds`（默认 20s），宽限期后强制取消在跑任务。
- **覆盖率门禁**：`setup/build.ps1` 新增 `-CoverThreshold`（默认 55）与 `-CoverGate` 相关开关。
- 对标报告：`docs/benchmark/WorkBench-开源对标报告.md`（8 个同类开源项目八维对比 + 证据附录）。

### Changed
- `Engine.CancelTask` 对运行中任务从"只写审计"改为**真正取消执行上下文**。
- `failTask` 不再把 `ErrorKind` 硬编码成 `execution`，改为写入执行器给出的真实分类。

### Fixed
- 任务完成后 `events.jsonl` 被空内容覆盖为 0 字节（现场 29 个任务受影响）。
- 被拒任务无法重试：状态机缺少 `rejected → queued` 通道，与 `RetryTask` 允许集矛盾。

## [1.0.0] - 2026-10-18

### Added
- 首个可用版本：Windows 单 EXE 桌面工作台（Go 纯标准库，7.4 MB，无外部运行时依赖）。
- 19 个内部包：配置/存储/队列/引擎/执行器/评估/链生成/语义判分/模型路由/模板/隔离 profile/面板/托盘/单实例/自愈/快捷方式/浏览器拉起。
- 24 条内置任务模板（6 领域 × 4），模板 CRUD、导入导出、恢复默认。
- 三维评估（价值/成本/可行性）+ 硬门禁（重复/高相似/冷却/日上限）+ 语义判分（异步降级安全）。
- 链式迭代：后继模板、自评 nextIdeas、前序产出摘要注入、链深度与扇出限制。
- 全自动闭环：生成 → 评估 → 自动批准 → 队列 → 执行 → 归档 → 自评 → 后继。
- 面板（7 页签 + SSE 实时推送）、MD/TXT/DOCX 导出、开机自启、系统托盘、崩溃恢复。
- 资源自愈：内存/句柄/goroutine 采样，连续越阈自动重启。
- 验收：e2e 20 项全过，单文件 EXE 净目录可跑。

### Known issues
- 7×24 长时压测未跑满 24h（已完成 3 轮长时 + 3 次崩溃注入）。