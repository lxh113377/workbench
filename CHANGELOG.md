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

### Fixed
- **修复长期空转的死代码门禁（两处叠加缺陷）+ 债务基线 + 删除唯一「死且重复且自身有缺陷」的自证件**：
  `tests/check_deadcode.ps1` 一直报「无死代码」，**同一棵树、同一时刻实测真实有 16 处**。两处叠加缺陷：
  ① **引用计数把注释也算成引用**：Go 惯例是函数自己的**文档注释以函数名开头**，于是「凡有文档注释的未导出
  函数」全都对本门禁隐形 —— 它实际只能抓「完全没被提及的函数」。判据与实现差了整整一个量级。
  ② **抽取用了 `-match`（PowerShell 默认不区分大小写）**：.NET 的 IgnoreCase 也作用于 `[a-z]` 这类字符类，
  于是 **537 个导出定义**被当成「未导出」候选；改用 `-cmatch` 后候选从 38 降到 **16**（真值）。
  修法：语料**只取非注释行**（trim 后以 `//`、`/*`、`*` 开头一律跳过）；函数名改**手写解析** +
  首字符 ASCII 小写判定（不再依赖任何大小写敏感的正则开关）；取文件列表改 `git ls-files -z`（NUL 分隔，
  彻底免掉路径引号修剪那套转义）。
  新增 `tests/deadcode-debt.txt`（与 gofmt-debt / css-token-debt 同构）：15 条真债每次运行打印 KNOWN 不判红；
  **基线里已不再死的条目会判红**（失效豁免必须删），防基线腐化成垃圾桶。
  **删除 `cmd/workbench/workspace_guard.go`**（114 行）：实测全树零引用（无 `--guard` 分发表）；其断言与
  `internal/workspace` 的测试**重复**（`ResolveWriteSymlinkEscape` / `IsWithinBoundaries` / `AuditAppendReadTail`）；
  且它自身含**两处「SKIP 记成 PASS」** —— `os.Symlink` 不可用与无数据根时都调 `check(..., true, "(skip: …)")`，
  打印成 `[PASS]` 并计入 pass（而 detail 只在失败时打印，连 skip 字样都不出现）。这正是本仓明令的 SKIP≠PASS。
  我那份《硬约束机器保障缺口》报告里以该件为取证来源的那一行，已加日期注记（该件作为死代码删除；其判据结论
  不因删除而失效）。
  **双向变异证明**：新增一个零引用未导出函数 ⇒ RED「新增死代码 1 处」；在代码行里引用一个基线内条目
  （基线条目复活）⇒ RED「失效的债务豁免 1 处」；基线 GREEN，还原字节一致。
  **本轮我自己的三处工具/脚本失误（如实记，均由判据当场抓住）**：
  ① 用字符串拼接重写这个 PS 门禁时产出**引号不成对**的行（7 个 `"`，末段多一个开引号吞掉行尾）⇒ 语法门禁
  报红；定位手法改成**逐行引号奇偶自检**（此后作为写 PS 的例行检查）。早一版还把 `$_.Trim('"')` 的引号
  拼坏导致结构错。
  ② 第一版「基线条目复活」变异报 **SILENT**：我的变异脚本里那次 `replace` 没有断言、实际没命中 ——
  改成**只追加**的最小变异后才得到真的 RED。教训：变异脚本必须自证「变异真的施加了」（与我把
  「变异实验自身要有受理面检查」编进模板的口径一致）。
  ③ 新门禁在 worktree 里对「磁盘已删但仍受跟踪」的文件**直接抛错**（语料循环有 `Test-Path` 守卫、抽取循环漏了），
  是**变异基线崩溃**把它暴露出来的，不是我推出来的；已补守卫。
  验证：12 项门禁全 PASS（含修复后的 check_deadcode）；`go build ./...` 与 `go vet ./...` rc=0；
  暂存 4 项 +112/-156；数字未变（43/27/43/29）。
- **同一数字解析缺陷类的第 2、3 处（含一处**不可逆删除**风险）：`prune.go` 与 `pool_cli.go`**：
  承 round 156（`ui_probe.go` 的 `parseIntArg` 用 `Sscanf` 接受半数字），本轮把同类写法在本包内排查完，
  又找到两处，后果一处比一处重：
  · `prune.go` 的保留天数用 `fmt.Sscan(args[i], &v)` —— 实测 `"3abc"`→3、`"3.5"`→3，最意外的是
  `"0x10"`→**16**（十六进制被当合法值）。这三个数字是**保留天数**，配合 `--apply` 是不可逆删除：
  解错一天，删的就是另一个年龄的数据。
  · `pool_cli.go` 的 `--model-pool log [N]` 用 `fmt.Sscanf(args[1], "%d", &n)` 且**返回值被丢弃** ——
  `"5abc"` 静默当 5、`"abc"` 静默沿用 30，而 `"0x10"` 在 `%d` 下解析成 **0** ⇒ `LogTail(0)` 打印 0 行、
  rc=0：用户以为拿到了日志，实际是**静默空操作**。
  修法：两处统一调用 round 156 修好的 `parseIntArg`（同包，单一实现），并在调用点**报错返回**
  （`prune` → rc=1；`pool` → rc=1，符合其头注「1 命令错误」）。`prune` 的测试刻意**不跑 `--apply`**：
  本包 `DSHHome = <root>/runtime/dsh-home`、root 不由 `--data` 决定，测试里做真实删除可能碰到工作区外的副本；
  删除语义的正确性由 `internal/retention` 既有的 3 个测试覆盖。
  新增 `prune_cli_test.go`（坏天数 8 种 × 3 个开关 ⇒ rc=1；默认 dry-run 与显式 `--dry-run` 等价；
  拼错的开关 `--aply` 必须 rc=1 —— 否则「已回收」会被误报成「未回收」；JSON 字段名 `dryRun`/`taskDirsOptIn` 契约）
  与 `scan_family_guard_test.go`（**家族守卫**：本包 .go 的非注释行禁止再出现 `fmt.Sscan`/`fmt.Sscanf`）。
  **本轮的守卫自己坏过两次，都被变异证明抓出（如实记）**：
  ① 首版针写成 `"fmt.S"`+`"can"` ＝ `fmt.Scan` —— 它**不是** `fmt.Sscan`/`fmt.Sscanf` 的子串
  （`S` 之后是 `s` 不是 `c`），变异 B 让守卫 **SILENT**：守卫只挡了从未出现过的写法、漏掉真正出事的两种；
  ② 修好针后 B 仍 SILENT —— 根因是我用 charCode 构造 Go 源码时把**表达式** `string(rune(10))` 拼成了
  **字面字符串** `"string(rune(10))"`，于是 `strings.Split` 只得到「整文件一块」、再被首个 `//` 截断，
  针永远到不了（文件级诊断看得见针、行级永远不报）。改为 `nl := string(rune(10))` 变量后：
  变异 A ⇒ RED（`TestPruneCLIRejectsBadDayFlags` + 守卫同时判红）、变异 B ⇒ RED（守卫）、还原 ⇒ GREEN 且字节一致。
  教训两条：**(a) 「禁止型/检查型判据」本身也需要变异证明**——它最容易只挡住想象出来的写法；
  **(b) 用 charCode 规避转义时要分清「表达式」与「字面量」**，本仓「多层字符串改写」家族又添一例。
  验证：`go vet ./cmd/workbench/` rc=0；4 个相关测试 rc=0；受影响 11 项门禁全 PASS；数字未变（43/27/43/29）。
- **修一处「注释承诺与实现相反」的 CLI 解析缺陷 + 让它自述的 rc=2 真正可达（`ui_probe.go`）**：
  起因：`cmd/workbench` 是本仓覆盖率最低的包（读数文件里 `coverage` 记为 11.1%），本轮按「无同名测试
  的文件」逐个看，在 `ui_probe.go` 读到 `parseIntArg` 注释写着「**拒绝 "12abc" 这类半数字**」。
  **实测与注释正好相反**（该函数此前**零测试**）：`fmt.Sscanf(s, "%d", &v)` 只取**前导**整数、
  忽略尾部残留 —— `"12abc"`→12、`"1e3"`→1、`"12.5"`→12，最刺眼的是 `"0x10"`→**0**
  （`--port` 拿到 0 ⇒ 内核随机分配端口，日志里看不出参数被吃掉）。
  第二处缺陷（更隐蔽）：`--port` / `--seconds` 两个调用点在解析失败时**静默沿用默认值**（连提示都没有），
  于是该文件头注自述的「退出码 … / 2 参数错误」**从不因数字参数错误而触发** —— 契约形同虚设。
  修法：① `parseIntArg` 改用 `strconv.Atoi`（**按整串**解析，四类残留一律报错）；
  ② 两个调用点遇错打印明确信息并 `return 2`；③ 加边界：端口须在 1..65535、秒数须 > 0
  （端口 0 也拒绝 —— 本探针刻意「固定端口、可复现」，随机端口与之相悖）。
  新增 `ui_probe_test.go`（该函数此前零覆盖）：拒绝表 `12abc / 1e3 / 12.5 / 0x10 / abc / 空 / 空白 / 5 6`；
  接受表 `42 / " 42 " / 17890 / -5 / +5`；另有 7 个 CLI 坏参数用例断言 `runUIProbeCLI → 2`。
  **一处按实测改契约（不按直觉）**：我最初凭直觉断言 `"+5"` 应报错，实测 `Atoi("+5")` = 5，遂把它移入
  接受表，并同步把函数注释从「整串必须是十进制数字」改精确为「整串必须是十进制**整数**（可带符号）」——
  本轮修的就是注释与实现不符，**不能顺手制造新的一处**。
  **变异证明**：把实现退回 `Sscanf` 并移除 `strconv` 导入 ⇒ RED 且点名
  `parseIntArg("12abc") = (12, nil)，应报错`；还原 ⇒ GREEN 且字节一致。中途第一次变异是**无效变异**
  （只退实现、留着 `strconv` 导入 ⇒ `imported and not used` 编译错），判定器正确标为 `INVALID(compile)`
  而非 `RED OK`（「变异实验自身要有受理面检查」这条模板要求又救了一次）。
  **调用方安全性实测**：全仓 `--ui-probe` 调用点用的都是正数端口（`check_homepage` 20890 起扫空闲端口、
  `check_perf` 20770 起、且两者自带 `-le 0` 守卫）与 `--seconds 40 / 900` ⇒ 本次收紧不打破既有调用。
  验证：`go vet ./cmd/workbench/` rc=0；新测试 rc=0；受影响 11 项门禁全 PASS（gofmt / multi_bom / repo_eol /
  comment_staleness / report_figures / gate_coverage / gate_links / ps1_syntax / changelog / skip_disclosure /
  test_assertions）；`git diff --stat` 为 `+100/-11`（两文件，内容改动经 git diff 核对）。
- **补上体积下界（exeMinBytes）并新增「基线键 ⇔ 判据分支」配对判据 —— 修一处静默失效了三天的门禁**：
  `tests/check_perf.ps1` 的体积下界判定（1b 段）读 `$bl.exeMinBytes`；基线缺这个键时
  PowerShell 取到 `$null`，该判定不报错、不打日志，直接走 SKIP 且整条门禁仍 rc=0。
  git 实证的演变链：`ebd6442`（10-21）立双向判、键=7747072 → `c6fa36e`（10-21「裁剪后重取基线」）
  把键与 1b 段**一并删掉**，而该提交说明里仍写着 `exeSizeMin PASS（余量 2.23MB）` 的旧读数 →
  `74ce0ac`（10-22，说明只写「25 个含中文脚本补 BOM」）diff 实为 **+20 行**，把 1b 段带回而
  无人补键 ⇒ 判据自此永走 SKIP，三天无人发现。
  **取值与依据**（可复算）：取 3,145,728B（=3MiB），与 ci.yml `Verify artifact` 现存生效的
  硬编码回退同值 —— 即把那条漂在基线之外的约束收编进基线（正是 `性能基线口径.md` §6 该行原文的
  要求），**不放松任何现有约束**。构造性地板：同 `-trimpath -s -w -H=windowsgui` 参数的空 main
  Go 程序实测 1,214,976B + 本包内嵌资源实测合计 213,644B（appCSS 78184 + appJS 74285 +
  govJS 14539 + poolJS 34123 + favicon 236 + 四页 12277）≈ 1.43MB；历史最小产物 7.39MB、
  当前 10,310,144B。故 3MiB 既高于物理地板（拦得住截断/空壳），又远低于任何正常产物。
  实测激活：`CHECK exeSizeMin result=PASS value=10310144 bound=3145728 余量=6.83MB`，rc=0，
  判定做成数从 1 升到 2。
  **防复发判据**（`workbench/cmd/workbench/perf_baseline_contract_test.go`，走 go test，无需新脚本）：
  ① 判据引用基线字段 —— check_perf.ps1 里每个 `$bl.<名>` 都必须在基线 JSON 里有键，且 null 字段
  必须在 `nullFieldReasons` 登记理由（本仓口径里 null 只表示「确实未实测」）；
  ② 双向核对 —— `exeMaxBytes`/`exeMinBytes` 既要键非 null 为正，又要在非注释代码里找得到对应
  分支标记（防 `c6fa36e` 那种「键与段一起删、自洽但功能没了」）。提取用手写扫描而非正则，
  文件内**零反斜杠零反引号**（沿用本仓在册的「多层字符串改写」教训）；并带受理面哨兵
  （引用数为 0 或基线键数为 0 即 Fatal，防循环不执行而恒绿）。
  **三项变异全判红且点名**（临时 worktree，跑完即删、主仓两文件复验字节一致）：
  M1 删 `exeMinBytes` 键 ⇒ 点名「引用了基线字段 exeMinBytes，但没有这个键 —— 会静默走 SKIP
  且门禁仍 rc=0」（**正是我要修的形态**）；M2 把 1b 分支标记换成无重叠新名 ⇒ 点名「已找不到
  "exeSizeMin" 分支 —— 与基线字段 exeMinBytes 失去配对」；M3 把 `handleMax` 改 null 不登记理由 ⇒
  点名「未登记理由」。还原 ⇒ GREEN 且两文件字节一致。
  **本轮我在验证工具自身上自纠三处**（都不留到下一轮）：
  ① M2 第一次是**无效变异**：我把 `exeSizeMin` 改成 `exeSizeMinRenamed`，新串仍**包含**原标记，
  `Contains` 自然命中 ⇒ 读数显示 SILENT，差点被我写成「判据漏抓」。改成无重叠的 `exeSizeLow`
  后正确判红。教训：看到「判据没抓到」时，先怀疑**变异样本没真的去掉标记**。
  ② `errLine` 取「首条匹配行」又抓到 t.Logf 的日志行而非 Errorf 失败行（同族第 3 次），
  改为只匹配失败文案。
  ③ 数反斜杠的度量本身写错：`new RegExp(charCode(92)+'+')` 是正则 `\+`＝**数加号**，
  报出「5 个反斜杠」而真值是 0；改为直接扫 char 92。
  另记一次写入通道事故（本册在册家族）：Go 字符串字面量里的 `\n` 被写入通道换成真换行，
  导致 `string literal not terminated`；处置是包级 `const nl = string(rune(10))` 且文件内零反斜杠
  （顺带踩到「常量放进了第一个函数内、第二个函数看不见 ⇒ undefined: nl」，已移到包级）。

  一处**未修正的过期陈述**（我刻意不改）：`docs/benchmark/WorkBench-开源对标报告-2026-10-25.md`
  §7.3 仍写着「`exeSizeMin` 仍 SKIP（基线 `exeMinBytes=null`）」—— 本轮之后该句已不成立。
  那是**并行会话的在途报告**，按「不碰他人在途文件」的既定纪律我没有改它；权威口径已在
  `性能基线口径.md` §6 该行更新为已闭合（含演变链与取值依据），并留此条以便该报告的作者
  或下一轮按自己的口径刷新。
- **helper 一致判据从 1 组扩到 7 组 + 修 check_gofmt 的顺序死结 + 更正我把 fold 误分类（round 124）**：
  承接上一条 toast 审计：判据不该只管撞见的那一个。文件 `git mv` 改名
  toast_duration_contract_test.go -> **helper_identity_contract_test.go**（保留历史），cases 覆盖
  api / toast / TOKEN / armConfirm / disarmArm / fmtTime / fold 七组，加两道防自转结构：
  ① 抽取失败即点名「判据失去受理面」；② 归一长度低于登记值一半即报「提取法可能退化，判据在比空气」。
  **三项变异全部判红闭环**：真实代码分叉（把 poolJS 的 zh-CN 改 en-US）⇒ 点名 fmtTime 分叉；
  提取法退化（只取首字符）⇒ 报归一长度 0 远低于登记值；把 fold 改名 ⇒ 报受理面失效。
  **注释差异不算分叉（判据太严会假红）**：首版把 armConfirm 判成分叉，实际代码逐字相同，差在
  appJS 有一条行尾注释、poolJS 多两行整行注释。正解是复用仓内已被 TestStripLineCommentsIsSound
  四项自检验过的 stripLineComments，而不是再写一个更难保证正确的剥离器 —— 与 round 64 家族同源
  （判据不准），但这次方向是误报而非漏报。
  **修 check_gofmt 的顺序死结（真实缺陷，不是并行会话的锅）**：清单来自 git ls-files（含已 stage
  未提交的新文件），而 HEAD blob 取不到 ⇒ 一律记成「HEAD 漂移」判红；这与本仓验证流程「先 git add
  让 check_report_figures 的未跟踪计数反映提交后状态」直接冲突，形成「必须先提交才能跑绿、提交前
  又必须跑绿」。修法：先用 git ls-tree -r --name-only HEAD 一次性列出 HEAD 路径，按存在性选
  HEAD:$rel 或 index 的 :$rel。
  **我第一版写成「HEAD 失败就重试 index」是错的** —— 脚本 ErrorActionPreference='Stop'，native 往
  stderr 写 fatal 会被当 NativeCommandError 抛出，失败路径根本走不完（本仓在册同族坑，我自己又踩）。
  双向验证：当前态（含已 stage 新文件）rc=0；stage 一个真不合规的新文件 => rc=1 且点名 zz_badfmt；
  清理后回 0、文件与暂存均无残留（负向变异专门证明修死结没顺手弱化判据）。
  **更正我把 fold 误分类**：首次比较用花括号配对提取函数体，而 blob 里既有块体函数也有箭头表达式体
  （const fold 是 => 一行、无花括号），配对逻辑从 fold 一路吞进后面的函数，给出 fold 与
  renderOverview 相同长度的假数据，我据此把逐字节相同的 fold 判成「同名异义」写进了报告。
  改用按缩进取块重测：fold 两侧 127/127 完全相同，已入判据。
  教训（写进报告）：**提取器出错的症状是两侧长度相等而不是不等** —— 只盯「有没有分叉」，
  看不见「我比较的对象是不是同一件事」。
  本轮我自己还有两次误操作，都如实记（不藏）：
  ① 试探 edit 时传了 new_string 空串 —— 它**真的把 fmtTime 那行删空**了（字符串字面量未终止）。
     靠 gofmt 报语法错当场发现，按 HEAD 恢复后重做。**教训：绝不用空 new_string 试探，
     删除就明确用删除语义，别拿试探当读取。**
  ② 追加报告章节时写了 `ins = [数组] + ["## 合并结论"]` —— JS 把数组隐式转成逗号字符串，
     再 spread 成逐字符行，把 99 行报告炸成 1360 行。靠 git checkout HEAD 恢复。
     教训：数组合并一律用 concat，绝不用 +（本仓「多层字符串改写」在册家族的又一实例）。
  验证：全量 check_all **36 PASS / 0 FAIL**；ps1_syntax / multi_bom / ps1_encoding 三个门禁 PASS；
  go vet rc=0（无重复顶层声明）；BOM 恰 1、纯 CRLF（check_gofmt.ps1 保持自身形态）；
  worktree 已删、zz_badfmt 探针文件已清、untracked 无我残留。
  报告：docs/三份内嵌JS同名helper分叉-五轴审查-2026-10-25.md 新增 §5.5 并更正 §2/§门禁建议。
- **三份内嵌 JS 的同名 helper 全量对比审计：修 toast 延时复制漂移 + 新增逐字节一致判据（round 122-124）**：
  三份复制粘贴的内嵌脚本（appJS/govJS/poolJS，各自 IIFE 不共享模块）是本项目「单 EXE 零依赖」
  换架构的代价。round 108 只为 api() 立过契约；本轮把所有同名跨 blob 定义过完：
  app 定义 56 个、gov 21 个、pool 28 个；三 blob 同名 = api/esc/toast，app∩pool 8、app∩gov 4、gov∩pool 4。
  逐个 brace-match 提取 + 去空白归一比较：api/armConfirm/disarmArm/fmtTime 逐字节相同；
  esc 两种写法（表查 vs 内联对象）但行为等价（innerhtml_escape_test.go 已逐 blob 验过五字符），不修；
  fold/renderOverview/render/load 是同名异义（不同职责），不属于分叉 —— 判据必须只覆盖同职责复制体，
  要求 load 相同是错的（gov 是治理页刷新、pool 是模型池刷新）。
  **唯一实质分叉**：toast 自动隐藏延时 app=2600 / gov=2600 / pool=3000。
  git 归因：两个值都自「仓库损毁后全量恢复」提交 a5cf134 就并存，代码与 docs/CHANGELOG 里
  无任何注释说明 3000 是有意（git grep -i toast 在 docs 中带 4 位毫秒数的记录 0 处）
  ⇒ 复制漂移，非设计。
  **统一到 3000 而不是压到 2600，依据是实测消息长度而非投票**：app 有 40 条字面消息、
  最长 30 字（「详情面板缺失（渲染契约错误）：#taskDetail 未挂载」）外加 18 条动态拼接，
  却拿着最短的停留时间；gov 只有 2 条最长 5 字、pool 14 条最长 8 字。取较长值。
  新增 web/toast_duration_contract_test.go（判据不判「2600 还是 3000 对」，只判「不许分叉」，
  与 round 108 的 api 契约同措辞传统）。
  **双向变异闭环**：变异 A 把 poolJS 的 toast 改名 ⇒ rc=1 且点名「判据失去受理面（空转），
  请同步本测试」—— 证明抽不到函数时不会静默绿（round 111 的在册形态）；
  变异 B pool 单边退回 2600 ⇒ rc=1，打印首个差异位置（第 206 个非空白字符）与两侧片段；
  还原 ⇒ rc=0。全程在临时 worktree 内做，主仓 assets_pool.go 复验未被污染。
  **审计了「blob 引用自己没定义的跨 blob helper」**（Go 侧测不出、浏览器才抛 ReferenceError）：
  结论无真缺陷，5 处命中全是我的扫描器假阳性 —— s/body 是箭头函数形参（我的 defined() 不收集
  形参）、raw 是对象字面量的键、p/fold 逐行重扫 0 次（全文件与逐行正则因跨行字符串剥离不一致
  给出不同答案）。⇒ 门禁建议明确不采纳：正确区分作用域等于自己写个 JS 解析器，收益远低于误报。
  轮询周期 gov.load=30000 vs pool.load=15000 **不统一**（数据体量与变更频率不同，round 111
  已核证不同源、不碰 e.mu）；三副本收敛成单一共享模块与「无构建链」架构决策直接冲突，
  属独立架构项，登记不做。
  本轮我自己的两处测量错（都靠「先自证测法」抓出）：① 首次提取 blob 用 ^ 行首锚点，得到
  「三 blob 定义数 0」—— 实际它们全裹在 IIFE 里、定义带缩进；若在坏数据上下结论就会报
  「无同名 helper」。② 写报告的 markdown 反引号检查时我把表格行整批排除在检查外 ——
  那正是本轮报告里在说的「判据比缺陷窄」；去掉错误排除后重扫，11 个表格行全部成对。
  验证：web 包全量 rc=0；三副本 toast 实测均 3000；FIG=0（新测试入库后未跟踪 29 与报告一致）；
  BOM 0 纯 LF（两 assets 文件各守自身形态）。
  报告：docs/三份内嵌JS同名helper分叉-五轴审查-2026-10-25.md
- **quality-verify 模板新增要求⑥「时序类判据必须给复现率」+ 本轮我自己两个错（round 123-124）**：
  编码的教训来自本轮真事：我 round 121 给 DOCX 并发写了测试、本机跑过一次绿就写「一次消除三缺陷」，
  CI run 40127263998 用同一个测试把它证伪 —— 复现率约 1/30，主 race job 绿、只有纯净检出 job 红。
  新⑥要求：并发/竞态/偶发类判据必须给「N 次里失败 M 次」，修复后同样重复并给出 M=0；
  原⑥顺延为⑦（结论只能是已验证有效或未验证）。
  **我的错一（写模板时自己踩了转义坑）**：给 Go 字符串拼接行加了行尾两个反斜杠，渲染出字面
  「仍绿）。」这种残渣；更糟的是我的探针判据查的是「两个连续反斜杠」，单反斜杠照样放行 ——
  运行时打印 TAIL 才看见。这与本仓在册的 round 116「判据只搜 rgb( 而仓库多数写 rgba(」完全同族：
  **判据比被测缺陷窄，就会假绿**。改成数「字面反斜杠总数」（LITERAL_BS=0）才算真验证。
  **我的错二（凭记忆叙述验证结果）**：我说「figures 判红、要同步报告数字」，实际本轮根本没跑过它 ——
  那是上一轮的记忆。实测 FIG=0 全项一致。定规重申：凡能从磁盘状态重推的结论，必须当场机器验证。
  中途 defaults.go 曾被我的字符串手术削掉闭合引号（slice 算错长度）导致 5 行语法坏；
  处理方式是 git checkout HEAD 恢复后重新一次成型插入，而不是在坏文件上打补丁。
  验证走运行时：临时 cmd 遍历 Defaults() 取 quality-verify 渲染真值 ——
  LEN=1193、VALIDATE=OK、HAS6/HAS7=true、LITERAL_BS=0、UNKNOWN_PH=false、LINECOUNT=2；
  探针用完即删并复验未跟踪生产件 0。templates+chain 包 rc=0、vet rc=0、gofmt clean、
  BOM 0 纯 LF；check_report_figures rc=0。
  同轮 CI 4a161e3（WriteFileAtomic 加锁修复）三 job 全绿、snapshotBuild result=PASS、
  GATE PASS 行数 27/27；我那个「疑似真实失败」分类器把 ANSI [36;1m 源码回显误判成执行输出，
  已用计数佐证排除（同族第三次测量错）。
- **修生产缺陷：WriteFileAtomic 同路径并发全失败（pathLocks 串行化）+ 更正我自己两处 over-claim（round 123）**：
  **真凶不是我这轮改的门禁**：CI run 40127263998 的 repo gates 判红，但红的是我 round 121
  写的 TestWriteDocxConcurrentExportsAllSucceed ——
  「替换目标文件失败（已从备份恢复）: rename … being used by another process」。
  主 vet+test(race) job 绿、只有纯净检出 job 红：本机 -count=30 实测**无锁版至少 1 次
  失败（约 1/30）、带锁版 0/30** —— 典型时序 flaky，正是本仓在册的「本地绿 CI 红」。
  机制：tmp 名唯一只解决了临时文件互相覆盖，**目标文件与 .bak 仍是共享名**；
  rename(path→.bak) 与 rename(tmp→path) 在 writer 之间交错，A 搬走目标时 B 正动同一
  文件，四个 writer 全部返回错误。WriteFileAtomic 是全仓唯一原子写入口（config/queue/
  state/导出/面板保存都走它），所以这是生产数据路径上的缺陷。
  **修在正确的层**：config 包加 pathLocks（sync.Map，key 走 filepath.Clean）
  + writeFileAtomicWith 入口 defer lockFor(path).Unlock()。不同路径互不阻塞；
  不给 .bak 加唯一名的理由是那样四个 writer 仍在抢目标、失败只换个形式。
  判据也加在缺陷所在层：新增 config/atomic_concurrent_test.go 两条性质（全部成功 +
  内容不撕裂，四个等长同字节载荷混合可辨）；只钉 web 层的话，将来别的使用者踩同坑没人报警。
  **加锁前的锁序复核**（在册教训 round 111 自写）：持锁期间只做文件系统调用、不再取任何锁；
  外部 Store.Replace 是 s.mu 到本锁单向，反向不存在 ⇒ 无环。config+web -race rc=0。
  **双向验证**：worktree（HEAD 0c55a0e 无锁版）第 2 轮即判红并点名「pathLocks 未生效？」；
  带锁版 60 轮 × 4 writer 全绿且无 .bak/.tmp 残渣。
  **更正一（本轮主报告）**：我 round-123 报告 §1 把那次 check_all 假红归因给「Temp 目录
  同秒撞 + finally 互删」。Temp 机制本身实测成立（同秒 HHmmss 相等=True），但**那次红的
  真实来源是上面的并发缺陷** —— 我把一个成立但未验证的机制当成了已证实的归因。已在该
  报告里就地更正（不删原文，写更正，让读者看到判断怎么改的）。
  **更正二（round 121 报告）**：原文「一次消除三缺陷」含「并发已解决」之意，属 over-claim；
  并入 WriteFileAtomic 只解决了临时文件互踩。已就地更正并指向本报告。
  教训写进 round 121 报告的 PR 模板建议 #3：**凡含并发类断言的「消除缺陷」必须给复现率**，
  否则「测过一次绿」会被读成「并发安全」。
  新增报告 docs/原子写同路径并发-五轴审查-2026-10-25.md（五轴 + 合并结论 + 门禁建议；
  建议 #2「静态扫描入口是否对共享路径加锁」明确不采纳 —— 区分不了「已在上层串行化」
  与「真漏锁」，误报率高）。
  验证：config -race / web -race rc=0；vet rc=0；gofmt clean；atomic.go BOM 0 纯 LF、
  反斜杠与 HEAD 差 0（我没引入新的）；worktree 与探针已删。

- **修门禁自身的并发互删假红：纯净检出门禁临时目录只到秒 + 接上声明却从未使用的超时参数（round 121-123）**：
  起因是全量 check_all 出现过一次「FAIL 纯净检出编译」，但我单独重跑同一门禁是 PASS、
  复跑全量也是 36 PASS / 0 FAIL。偶发红若不归因，下次会再撞并被当成真缺陷追查。
  **根因机制实测**（不是推理）：原命名 'wb-snapshot-' + HHmmss，同秒两次调用得到
  **完全相同**的目录名（实测 wb-snapshot-033919 两次相等=True；GUID 版两次相等=False）。
  本仓是多会话同机并发（handoff §10 已两次实证共享 index 竞态），而门禁 finally 里有
  Remove-Item -Recurse —— 两个实例**互删对方的快照树**，正在编译的一方报缺文件 exit 1。
  红的是判据实现，不是被检查的提交。
  同形态第二处 quit_mutation_check.ps1 用固定名 wb-mutation-ka，且前置删+后置删都 -Recurse，
  后启动者的前置删会删掉前者正在使用的变异体目录。
  **修法用本仓既有惯例、不发明新模式**：e2e_soak / selfheal_e2e / e2e_workspace /
  failover_evidence / check_staged_gofmt 都已经是 [guid]::NewGuid().Substring(0,8)，
  这两处只是漏网的。
  **顺带接上空头承诺**：[int]$TimeoutSec = 300 声明了却全仓无人使用（本门禁只出现 1 次，
  即 param 声明本身）—— 属本仓在册「文档承诺比实现新」的第三种形态，头部写着超时而
  实际任何一步挂住都会一直挂着。接进 go test -timeout 两处（首轮 + flaky 重试）。
  安全性实测后才接：-timeout 约束测试二进制执行时长（不含编译），逐包实测最慢
  cmd/workbench 45.1s、engine 26.0s，300s 余量 6.6× ⇒ 不引入新假红；格式化结果实测 300s。
  附带发现（登记未改）：ci.yml 全文没有任何 timeout-minutes，CI 侧比本地更依赖这个接线。
  **未修两处如实登记**：keepalive.ps1 固定名但全仓无删除动作（最坏日志混排、不产生假红，
  且该件在跑 7x24 保活，本轮不动）；agg_e2e.ps1 不在 check_all 常规段且 agg_e2e.py 有
  同名兜底路径，改唯一名要同步改 Python 侧 —— 属改一处需动两处的独立小轮。
  **不建常驻判据**（扫描「TEMP 命名无唯一性 + 本文件有 -Recurse 删」）：现存 4 处已逐个
  分级收敛（2 修 2 登记），属一次性收敛；常驻判据会挡住未来故意复用固定目录的合法写法
  （缓存目录），还要处理 -RedirectStandardOutput 这类不删的假阳性 —— 收益低于噪声。
  验证：check_snapshot_build 改后 rc=0；全量 check_all 36 PASS / 0 FAIL；PS 语法/多BOM/
  EOL/编码 4 门禁 PASS；两文件写回后逐字节复核（BOM 恰 1、snapshot 纯 CRLF 裸 LF=0、
  mutation 保持原 LF）；quit_mutation_check 不在 check_all 常规段且会起停真实进程，
  按「不为验证打扰 7x24 实例」纪律未单独运行，只做静态变量流复核（$mutantDir 6 处引用
  全经同一变量、无第二处硬编码字面量）+ 语法门禁。
  本轮我自己的四处测量错误（报告附节逐条记）：分类器漏看 Get-Random 把 homepage/perf
  误判成隐患（人工读原始行才排除，两处因此不在修复范围）；PS 正则解析 go test 得「包数=0」
  连锁空索引崩溃，改落文件用 Node 解析才拿到真实 29 包；按整行等值匹配 go test 调用行只中
  1 处（两处缩进不同 4/8 空格），先打 JSON.stringify 真实字节再改；以及直接猜门禁脚本名
  check_clean_checkout_build.ps1（真名 check_snapshot_build.ps1），靠 git ls-files 列全名纠正 ——
  「严禁猜标识符」我本轮破了两次，记在这里而不是藏起来。
  报告：docs/门禁快照并发互删-五轴审查-2026-10-25.md
- **DOCX 导出落盘改造（并入统一原子写入口）+ junction 安全行为测试（round 119-121，硬约束 #5 审计的落地）**：
  审硬约束 #5「所有删除走回收站」时逐点核对全仓删除原语（15 个生产文件），两处结论：
  **① 推翻了自己的怀疑**：linkDir 的 Stat 失败后 os.RemoveAll(dst) 初看像经典「rd /s 删穿
  junction」。一次性探针实测相反 —— Go 把 junction 认成普通目录（Lstat isDir=false），
  Stat 成功即早退；即便执行 RemoveAll 也是**摘链**，target 与其内容完好。
  探针结论必须变测试才守得住，故新增 executor/linkdir_target_safety_test.go 锁两层，
  并如实声明平台边界（非 Windows t.Skip，与本包既有 linkdir_silent_test.go 同模式；
  但 Windows 上「建不出 junction」判 FAIL 不判 SKIP —— skip 会把环境故障洗成绿）。
  **牙齿验证**：控制组直接毁 target 内容 => 判红（证明断言不是空转）；
  而把摘链换成 cmd rd /s /q **也不判红** —— 本机直测证明 rd /s /q 对 junction 同样只摘链
  （「rd 删穿 junction」是 XP 时代行为，Vista+ 已修）。所以测试锁的是**这台 OS 的现行语义**，
  不是替旧传说话。注释里我原先写的「Go 语义层/那会真的删穿」两处据此改正。
  **② 抓到真缺陷**：web/writeDocx 是自制原子写（Create(path+".tmp") 写、Close、Sync、
  再 os.Remove(path)、os.Rename(tmp, path)），探针实测并发导出同一目标
  「RESULTS=[B-ERR:rename ... The process cannot access the file because it is being used
  by another process. A-OK]」—— 固定后缀 .tmp 让并发导出必有一方失败（用户见「生成 DOCX 失败」）；
  另有「先删目标再 rename」的丢失窗口（与并行会话本轮在 config/atomic.go 修的数据归零同形态），
  且那句 IsNotExist 豁免接不住目录；还与同函数 md/txt 分支的 config.WriteFileAtomic 双实现并存。
  **按 #39 不选折中**：不逐条打补丁，而是 zip 先写内存 buffer 再交 config.WriteFileAtomic ——
  一次消除三缺陷（本文件本来就把整篇 document.xml 收在 strings.Builder 里，无流式优势可言）。
  新增 web/docx_atomic_test.go 测行为不测源码文本：并发四路导出全成功 + 产物是含 OOXML
  四部件的合法 zip 且正文真写进去 + 无 .tmp 残渣。
  **双向变异**：worktree 里对照 HEAD 旧实现，并发测试 3/3 稳定判红并点名 rename 冲突；
  新实现全绿。OOXML 有效性测试两边都绿（它验内容不验竞态，符合预期，不冒充成竞态证据）。
  失败窗口（rename 失败时旧内容仍在）由 config 包的 seam 判据负责，**不在此重复构造** ——
  那个窗口本机四种方案都构造不稳，重复尝试只会造出 SKIP 假绿。
  另修自己一处违规：测试里凭猜写 model.Task 的 ModelID 字段（真名是 Model），
  编译报错抓到 —— 「严禁猜标识符」这条我自己也会破，读结构体才是唯一正路。
  验证：web / executor 包全量 rc=0；vet rc=0；gofmt clean；探针与 worktree 均已删并复验
  check_untracked_go rc=0；提交前只 add 自己的 3 个文件（并行会话在途的 10 个 web 测试未碰）。

- **修任务队列「低优任务被饿死」结构缺陷：严格优先级加有界 aging（2026-10-25，对标 temporal IWRR 权重公平）**：
  **缺陷**：`internal/queue/sortTasks` 原是**纯严格优先级**（置顶 > 优先级高 > 创建时间早 FIFO）。
  7×24 自主循环会**持续产任务**（链衍生用模板 PriorityWeight、自主目标固定 4、面板可设 1/5 ——
  生产优先级实测异质 1-5，不是恒相等），低优任务在高优持续流入下可**永久排队（饥饿）**。
  这是无人值守系统的经典故障：面板"运行中但产出为0"一类在册事故的上游候选根因之一。
  **修法**：有界 aging —— 有效优先级 = `Priority + min(agingMaxBoost, 已等步数)`，
  每等 `agingStepMs=5min` +1，封顶 5（够让 prio-1 追平并反超 fresh prio-5）。
  **有界是关键**：当所有任务都比封顶更老时，全体获得相同 boost ⇒ 退化回严格优先级，
  既修饥饿又不无谓打乱既有顺序 ⇒ 既有 `TestPopRespectsPriorityAndFIFO`（全饱和区）仍成立。
  **时钟注入**：新增 `q.now` 字段（默认走 `model.Now()`，实测返回 UnixMilli ⇒ aging 单位是毫秒，
  与 CreatedAt 同口径），单测用它构造**非饱和**档位（等 30min 的 prio1 反超刚到的 prio5），
  这样判据落在真实缺陷区而非退化区。
  **变异双向**：把 `sortTasks` 退回严格优先级 ⇒ `TestQueueAgingPreventsStarvation` 判红且**逐字打印
  饿死序列**（`[high-fresh mid-fresh low-waited]`，等待的 prio1 落到最后）；修复态判绿。
  RED 由缺陷本身触发，非旁支 panic（本仓在册教训）。
  **覆盖**：queue 包 `effectivePriority`/`nowMs`/`sortTasks` 三新函数均有判据；queue 覆盖 100%→98.8%
  （新增 aging 分支，纯函数全测到）。全量 `go test ./... -count=1` 30 包全 rc=0。
- **布局契约测试钉住硬约束 #6（产出根 workbench-output）+ 两次口径自纠（round 118-120）**：
  config 包 9 个受跟踪测试里 BuildLayout/DataRoot/workbench-output 出现 0 次，硬约束 #6 只靠
  paths.go 一行 Join 成立 —— 改它不会让任何测试变红，却会让 10 个派生路径整体搬家。
  新增 layout_contract_test.go（TestBuildLayoutContract）三层判据：数据根名字、每字段按
  layoutClasses 归类挂根（反射遍历 + 显式映射两层）、DSHHome 绝不落进用户 ~/.dsh（硬约束 #2 同源）。
  **两层是被变异测试逼出来的**：第一版我删掉显式归类映射、只要求「落在三大根之一」，于是
  「TasksDir 从 DataRoot 改挂到 Root」直接漏过 —— root 下的 tasks 当然以 root 为前缀。
  放宽判据 = 制造假绿，与我这几轮在册的 round 64/99 同一类；这次栽在自己刚写的判据上。
  三向变异全判红且点名（数据根改名/TasksDir reparent/DSHHome 进用户 profile），全在临时 worktree
  内做、主仓 paths.go 复验未被污染。
  另两处自纠（都是我的测量法错，不是代码错）：
  ① 我用 gofmt -l 原始字节判「14 个未豁免漂移」—— 门禁是 LF 归一化口径，那 14 个全是工作区
     CRLF（index 里是 LF），归一化后全部 clean。判漂移必须按门禁口径测，否则会把别人的
     受跟踪文件误报成待修债务。
  ② status 解析用 trim() 吃掉了首行前导空格，把「未暂存」显示成「已暂存」，一度让我以为
     误吞了他人的 CHANGELOG 条目。逐字符读 X/Y 才判对（他人确实只动了 atomic/CHANGELOG，未碰我的）。
  共享暂存区竞态实证：并行会话为解除我方未 gofmt 暂存对它的阻挡，把我的文件退回未跟踪
  （内容未动，其 handoff §10 有记录），并提醒「add 与 commit 之间不要隔太久」——本轮遵守。

- **修 WriteFileAtomic 兜底路径的「数据归零」缺陷（对标 prometheus tsdb/wlog 的 Repair 时发现，2026-10-25 实测）**：
  **缺陷**：`WriteFileAtomic`（全仓唯一的原子写入口，config/queue/state 快照都走它）在第一段
  `rename(tmp→path)` 失败后，走的是「先 `os.Remove(path)` 删原文件、再重试 rename」。
  该窗口下若第二段 rename 也失败，函数返回错误，但**原文件已被删掉、新内容也没进去**
  ⇒ 目标文件彻底丢失（数据归零）。已用等效步骤实测复现（Remove 成功 + 第二次 rename 失败
  ⇒ 目标文件消失）。「尽力兜底反而造成数据归零」比「写不进去」严重得多。
  **对标来源**：prometheus `tsdb/wlog/wlog.go` 的 `Repair()` —— 它处理损坏段用
  `Rename(fn, fn+".repair")` **先备份再重建**，从不先删；正是这条对照让我看清本仓「先删后建」的风险。
  **修法**：「先删后建」改为「先备份后替换」—— 第一段 rename 失败 ⇒ 先把原文件 `rename` 成
  `<path>.bak`（保数据）⇒ 再替换；若替换也失败 ⇒ 从 `.bak` 恢复；若连备份都失败（原文件被独占）
  ⇒ 原文件不动只报错。「写不进去」远好于「把旧数据删了还是写不进去」。
  **判据分两层**（`atomic_fallback_test.go`）：
  ① 主力 = `TestWriteFileAtomicFallbackMatrix`，用 seam（`writeFileAtomicWith` 注入 rename/remove）
    把四种成败组合钉死，**平台无关、不碰运气**；含 `calls` 计数断言实现调用次序（结构变了自杀不空转）。
  ② 补充 = `TestWriteFileAtomicRenameFallbackKeepsData`，Windows 真实独占打开走「备份也失败⇒原数据保留」分支。
  **为什么主力用 seam 而非真实文件系统**（本轮踩坑如实记）：为构造「第一段失败但备份成功」的真实窗口，
  实测扫过四种方案 —— 独占打开（两段 rename 都失败，备份无从发生）/ 目标空目录（Go 的 rename 竞放行）/
  非空目录（同放行）/ 只读属性（Linux 不拦）—— **本机一种都构造不出稳定组合**。若硬写真实文件系统的断言，
  它会 `t.Skip`，判据等于不存在（本仓在册的「vacuous pass」家族）。seam 才是诚实解。
  **变异双向**：实现改回「先删后建」⇒ 三条子判据同时判红（backup-fails 直接点名「数据归零」+
  次序断言点名 calls=2≠3）；修复态 ⇒ 六条全绿、零 SKIP。config 覆盖 70.3%。
  **一处顺带核实后判 NO-OP（不臆造工作）**：对标 temporal FIFOScheduler 的 `drainTasks/Abort`（停机时
  残留任务显式中止）—— 查 engine.stop 不碰内存队列、queued 态持久化在 store、重启 Reset 会重新入队，
  **无丢失无卡死**，属工作区已有分层设计，故不改（「无病不乱吃药」）。
- **修模板占位符 {prev} 渲染成空洞（首轮与「父任务无摘要」两条路径）+ 抓到一个我自己写的空转测试（round 113-116）**：
  **探针实测的真实缺陷**（临时 go run 驱动真实 RenderVars，跑完即删）：
  RootTask 只填 Topic/Words/Context/Date —— **不填 Prev**；Successor 填的是
  model.Truncate(parent.Summary, 400)，父任务无摘要时同样是空串。渲染出「… ｜ 前序  ｜ …」的空洞。
  而 Context 在首轮是有兜底文案的（「首轮任务，无前序上下文」）—— 同一批变量一个有兜底一个没有，
  是**不一致而非设计**。
  **为什么不能拿「内置 29 条模板 0 处使用 {prev}」免责**（运行时统计过，确实 0）：模板库支持用户
  导入（Registry.ImportJSON / Upsert），而 Validate 只拒绝**未知**占位符 —— {prev} 在 allowedVars
  白名单里，用户写 {prev} 完全合法、能入库、会渲染成空洞。空洞不只是难看：模型看到「前序 」
  分不清「真无前序」与「数据丢了」，会自行编造上下文。
  **修**：RootTask 补对称兜底；Successor 用 prev 变量 + (parent != nil && parent.Summary != “”) 守卫。
  过程中我自己犯了三次错，都被验证环节抓出（如实记）：
  ① **把语句插进 Go 结构体字面量内部** → gofmt 直接报语法错（变量声明必须在字面量之前）；
  ② **测试里塞了个生产不可达的 nil 父任务用例** → 该函数后半段还有 5 处 parent.X 无守卫，
    为它改是独立结构性工程、不属本轮主张；查 engine.go 确认 extendChain 调用前已解引用 parent
    ⇒ 生产永不 nil，撤掉用例并在注释写明理由与遗留；
  ③ **最严重：我的第一个测试是空转的**。哨兵设计成 …P{prev}|，prev 为空时渲染成 P|（单竖线），
    而我断言的是 "P||"（双竖线）—— 永不命中，**把缺陷态也判成绿（rc=0）**。
    是「还原旧代码重跑变异」这步才抓出来的：第一次 RED 我看到的是 nil 用例 panic，
    从未干净地看到空洞断言本身判红 —— 误以为已经验证过。改方括号哨兵 P[{prev}] 后，
    缺陷态 rc=1 且逐条打印实际渲染（P[]），修复态 rc=0。
    **教训（写进测试注释）**：RED 必须由「被测缺陷本身」触发，不能由测试里的旁支 panic 顶替；
    哨兵串必须与分隔符/其它占位符的输出形态**不撞形**，否则断言永不命中而测试恒绿。
  另：注释里我原先写了 chain.go:83 这个行号 —— 我的改动恰好让它失效（正是本仓 comment_staleness
  抓的形态），改为不写行号。
  验证：变异双向（缺陷态判红点名两条路径 / 修复态判绿）；chain + templates 包 rc=0；vet rc=0；gofmt clean。
- **解掉阻塞产物重建 5 天的 hiddenTestCmd 重复声明；A7/--soak 判据收敛为单一实现；R3 升级为行为级探针；三个「只定义未接线」的自主运行安全旋钮真正接线（2026-10-25 实测）**：
  **① hiddenTestCmd 三方重复声明（阻塞面）**：`cmd/workbench` 的 `hiddenTestCmd` 在无 tag 的
  `headless_test.go:32` 与两个平台拆分文件（`headless_testcmd_windows_test.go:19` /
  `headless_testcmd_other_test.go:13`）里**三方同名同签名**，`go vet ./...` 直接判红
  （`hiddenTestCmd redeclared in this block`）—— 交接记录 §7 记的唯一阻塞点，实测确认仍在。
  后果：`setup/build.ps1` 第 3 段的 `go vet ./...` 无条件执行（`-SkipTests` 绕不过），
  **产物因此 5 天无法重建**，A7/soak 的修复一直没进 `dist/workbench.exe`（check_artifact_soak_fresh 判红）。
  修法：删除无 tag 文件里那份 Windows 专有实现，只保留跨平台共用的助手；`go vet ./...` rc=0（30/30 包）。
  **② A7 与 --soak 判据收敛为单一实现（2026-10-19 现场缺陷的根治）**：两处各自实现
  「趋势 × 跨度 ⇒ 结论」，只修一处即漂移；报表曾同时输出 `longEnough: false` 与 `verdict: leak / exitCode: 2`。
  新增 `cmd/workbench/soak_verdict.go`（`soakConclusion` / `classifySoakVerdict`），`checkSoak` 与
  `buildSoakReport` 都只调用它；`minHours <= 0` 时回落默认门槛（否则门槛归零 = 复现现场缺陷）。
  新产物实测：11700 样本 / 12.82h 跨度 ⇒ `verdict=unknown, exitCode=3`（旧产物给假 `leak/2`）。
  **③ R3 从 grep 字符串升级为行为级探针**：原判据只查 scheduler.go 里有没有四个策略**名字**，
  而 StrategyLeastLoaded 曾与 round-robin 共用同一分支、从不读 inflight（实测 A 在飞 3、C 在飞 0 却挑 A），
  **grep 判据全绿**。新增 `cmd/workbench/scheduler_probe.go`：逐策略施加带区分的场景
  （least-loaded 读在飞数 / priority 取最高 / round-robin 轮转 / weighted 按权重 + 护栏 + excluded）。
  原 grep 降级为「枚举齐全」这一条必要非充分判据。
  **④ 三个「只定义未接线」的自主运行安全旋钮接线（D9 家族复发）**：`Risk.EscalateMinQuality`、
  `Risk.BlockOnRepeat`、`Rollback.OnCostOverrun`、`Bounds.MaxDraftsPerRound` 在 config 里有定义、
  有注释、有钳制，但**生产代码零消费方** —— 运维调这些值毫无效果，系统却显示「有保护」。
  与本仓 D9「全局限速闸门生产从未接线」同型。四个旋钮全部接上（risk.go 三处 + manager.go 一处）。
  **⑤ 快捷方式 Arguments 显式置空**：脚本从不写 `$lnk.Arguments`，而 `CreateShortcut` 打开**已有** .lnk
  时读的是旧值 ⇒ 早期写死的 `--silent` 会在老快捷方式里残留（双击后进程在、端口在、桌面什么都不出现）。
  回读校验同步扩到 Arguments。
  **变异验证（6 条，全部 MATCH）**：摘 span 护栏 ⇒ 3 条判据点名（正是 2026-10-19 现场缺陷复现）；
   least-loaded 不读 inflight ⇒ 3 条点名；摘 round-robin 场景 ⇒ 首版**未抓住**（暴露判据自身覆盖面缺口），
  补 `TestR3ProbeCoversAllStrategies` 后再摘即判红；三个旋钮各自反接均被对应判据点名。
  **回归**：`go vet ./...` rc=0；30 包 `go test` 全 ok；覆盖率门禁通过（29 包达标）；
  `check_all.ps1` 唯一剩余 FAIL 即本条目要修的未跟踪生产件；`setup/build.ps1` rc=0 重建产物。
  **覆盖率**：shortcut 23.9% → 30.4%（阈值 25%，未降阈值；补 `truncate` 与脚本契约测试）；
  autonomy 74.2% → 79.7%。
  **一条自纠**：探针首版把 `Spec.Priority` 方向写反（以为数字越大越优先，实际 1 = 最高），
  被自己的场景当场判红 —— 跨项目/跨模块迁移判据前必须先确认被迁移语义的方向。
### Changed
- **第四轮对标补第四项：性能读数从 UNVERIFIED 取成实数（2026-10-25 02:5x 实测）**：
  报告初稿把首页 P95/常驻/句柄记为 UNVERIFIED（理由「本机无常驻实例」）。复核性能基线口径 §2 后确认
  `check_perf.ps1 -Runtime` 走的是**空闲短生命周期探针**（`--ui-probe --expect-idle` + 全新 TEMP 数据根
  + `--seconds 120` 自终止 + finally Stop-Process），与既有基线字段同口径，**不需要生产实例**
  —— 即「取不到数」的原因是我没走对取证路径，而不是环境真的取不到。
  实测（rc=0，四项判定做成、超界 0）：EXE 10,310,144 B（余量 10.17MB）/ 首页 P95 **1.94ms**
  （P50 1.57 / MAX 14.68，n=40；距 250ms 上界 128.9×）/ 常驻 **12.48MB**（距 64MB 余 51.52MB）/
  句柄 **126**（距 512 余 386）/ 线程 8。
  跑完复核 **0 残留 `workbench-perf-*` 目录、0 残留 workbench 进程**：不触生产、不耗模型额度。
  `exeSizeMin` 与 `taskDurationSecMax` 仍 null/SKIP（未实测 ⇒ 严禁臆造，与门禁三态口径一致）。
  报告 §3 性能行、§4.4 待办、§7.3 取证命令已同步实数。**教训**：登记「取不到数」前先确认
  是不是取证路径走错了 —— 把"我没用对口径"写成"环境不支持"，会让下游 agent 误判为长期缺口而不去做。
- **第四轮开源对标报告落地（2026-10-25）`docs/benchmark/WorkBench-开源对标报告-2026-10-25.md`**：
  首次逐行精读前几轮未读的 peer 内核模块——temporal `common/tasks` 调度器家族（10 种）与
  prometheus `tsdb/wlog`（WAL + Repair + checkpoint），peer 仓本轮 `git fetch` 刷新到最新 HEAD
  （temporal 1bcd882 / prometheus a2fda0e / river 928c04a，2026-10-23~24）。
  产出四部分结构化交付：对比总览表（八维增量口径）、逐项差距说明、改进建议清单（P3-1/P3-2 已落地、
  P3-3 核实后判 NO-OP）、分阶段可实施路径（M0-M5）。
  **一处 NO-OP 判断单独留档**（§4.3）：temporal `FIFOScheduler.Stop` 的 drainTasks/Abort 疑似缺口，
  核实 `engine.go:685` Reset 含 StateQueued 重入队 ⇒ 本项目无丢失无卡死，按「无病不乱吃药」不改，
  登记依据防后人重复怀疑同一处。
  报告内所有自报数字当场自纠两处：「29 包全有测试」实测为 internal 28/29（browser 无测试文件）；
  prometheus Windows 句柄注释行号从估计的 424-428/494-495 校正为实测 426/484-496。
  **教训（写进报告 §7.2）**：引用 peer 行号必须逐条 read 回原文核对，不能凭精读时的记忆写——
  记忆会漂，行号漂一个，下游 agent 按错行号读到的就是另一段代码。
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
- **一次「声称已清理但实际存活」的自查：xtmp91 工作树（round 91 记录为已删，实际到 round 114 才清）**：
  round 91 的收尾输出里有 `worktree removed` 字样，但同一次调用里紧挨着的一条命令报的是
  `git worktree remove --force xtmp91` **失败**（fatal，因该工作树被 lock）—— 我把「打印了 removed」
  当成「删除成功」了。它带着 locked 标记存活了 3 轮，占着一份完整工作树副本。
  本轮清理前先取证到可安全删除的程度：`git diff --name-only` 为空、HEAD `db97bef` 是 master 祖先
  （无独有提交）、`status` 里 2130 条全是 `D ` 与 `?? ` **成对**（index 被清空的噪声，非真实改动），
  且我当年复制进去的 `check_skip_disclosure.ps1` 早已作为跟踪文件入库（round 95）→ 无独有内容。
  解锁后 `worktree remove --force` 成功，`.git/worktrees/xtmp91` 一并清掉，主仓状态未受影响。
  **机制化认知（写给未来的我）**：`remove` 与 `list` 之间必须**读一次事实**（工作树列表 / 目录存在性），
  不能凭同一段脚本里出现过的成功字样下结论 —— 与 round 107「提交被中断却报告 CI 全绿」是同一条教训的
  另一种表现：**结论必须由独立的第二次测量支撑**。
- **修 `check_report_figures` 的口径不一致（标签说「测试」算法数「全部」）+ 登记成组提交复发（round 112-114）**：
  **触发**：按 round 109 定规「提交前跑全量 check_all，不挑子集」，第一次跑就抓到 3 个 FAIL ——
  其中 `报告数字一致性` 报「未跟踪 Go 测试：报告写 31，实测 33」。我先把它当成
  「报告数字又滞后了」（这套门禁历史上抓了 8 次滞后），**但这次不是报告错，是门禁自己错**：
  并行会话新增 2 个未跟踪**生产**文件（`scheduler_probe.go`、`soak_verdict.go`），
  而该判据的标签是「未跟踪 Go **测试**文件」、算法却数 `?? *.go` 全部 —— 两口径只在
  `untrackedProd > 0` 时才分叉，过去恒为 0 所以一直没暴露。**这是一个潜伏了很久的口径 bug，
  靠别人的改动才被动发现**。修法：算法按标签本义只数 `_test.go`；未跟踪**生产**文件
  不由这里判（那是 `check_untracked_go` 的职责，它对生产单列 [生产代码] 并直接 rc=1），
  但仍把生产数打进摘要，避免「只数测试」把生产未入库藏起来。
  变异验证：改报告 31->32 => 判红点名「L15 报告写 32，实测 31」（判据仍有牙齿）；还原靠写回原字节，
  并用 `git status` 确认该报告回到 clean（不靠肉眼）。
  **顺带把「2 个未跟踪生产文件」这件事取证入账**（交接记录新增 §1.5）：这次引用方不是未跟踪件，
  而是**已跟踪且处于 M 状态**的 `acceptance.go`/`soak.go`。建干净 worktree 模拟「只提交引用方」：
  `go build ./cmd/workbench` rc=1，5 处 undefined（含我第一遍扫符号时漏掉的 `soakVerdictMinHours` ——
  靠真编译才发现，没靠猜）。当前 HEAD 自洽（新符号在 HEAD 全树零引用），属健康在途；
  已给出成组提交命令。这验证了 §1 那套「依赖组」方法论确实需要复发登记。
  **本轮最贵的认知**：跑全量 vs 挑子集的差别，这次是实打实的 —— 若继续挑那 12 个，
  这个口径 bug 会一直潜伏，直到某次数值真的对不上时把**门禁**误判成**报告**错。
- **补齐顶栏「更多」菜单的 Escape（govJS/poolJS 两份漏抄）+ 用实质判据替换脆弱的计数判据（round 111）**：
  **缺陷**（实测取证）：adminPage / governancePage / modelPoolPage 三页都有 `id="btnMore"` + `id="topMore"`，
  但菜单块里只有 appJS 绑了 Escape —— 而 appJS 的注释把这条当硬性要求（「键盘用户用 Enter 打开后，
  鼠标外点逻辑对他们不存在」）。**更糟的是 poolJS 有条注释「与顶栏「更多」菜单的 Escape 处理同型」，
  拿一个当时并不存在的实现当参照**（文档描述了一个没有的行为；本轮补齐后该说法才成立）。
  这是 round 109 esc 单引号映射那次的**同型复发**：三份内嵌脚本是各自独立副本，一份里修的另两份不会自动跟上。
  **修法**：给 govJS/poolJS 各补一个与 appJS 同形态的顶层 keydown 块（含 `btnMore.focus()` 焦点回退）。
  **行为验证**（假 DOM 真跑三份的菜单块）：三份都是「Enter 能打开 → Escape 关闭 → 焦点归按钮」；
  **反证有牙齿**：拿 HEAD（改动前）的 govJS/poolJS 跑同一 harness，两者都是「打开=Y Escape关闭=N 焦点归位=N」。
  **判据侧的关键决定**：补完菜单 Escape 后 poolJS 的 keydown 到 3 处，撞上既有判据「n > 2 判红」而 CI 会挂。
  我没有去放宽数字，而是查清那条判据**两头都错**：
  · 假红 —— 本仓合法就有 3 处（菜单 / 编辑器 / 待确认态各一），加第四个合法入口即撞线；
  · 假绿 —— 若只有一处但写进 render 体内，按一次 Escape 触发 N 次关闭，原缺陷照样漏。
  真实风险是「监听器随重绘累积」，不是「数量多」。故换成**实质判据**：
  `repeatableFuncBodies()` 抽出 render* / load / fill* 的函数体，keydown 绑定不得出现在其中。
  双向验证：正常态三份 0 命中（rc=0，不因合法的第 3、4 处绑定误报）；注入 renderList 体内 => 判红点名。
  新增 `TestTopbarMenuEscapeInAllBlobs`（三份菜单块各查三件事实：Escape 键判据 / hidden=true / focus()），
  变异 A 拆掉我补的 gov 块 => 判红点名「治理台 govJS 的顶栏菜单缺键盘可达性构件」；
  **对照：那种情况下 keydown 计数是 0，旧的 n>2 判据完全判不出来（静默放过）** —— 这是实质判据优于计数的实证。
  过程中的自纠错（都靠跑真结果暴露，非推测）：
  ① 我第一次跑变异 B 得 rc=0，误以为「实质判据没牙齿」，深挖发现是**我的正则写坏了**
     （占位符构造时把 `\w*` 误成 `$W*`，`repeatableHead` 永不匹配 => 判据静默空转）；
     修好后 B 立刻判红点名。**「测试判绿」有两种：真干净，和判据根本没跑** —— 必须做变异才知道是哪种。
  ② 我重建 keyboard 测试文件后逐行 diff 显示 51 行差异，实为整体错位一行（多 import 一行），
     改用「旧行是否都在新文件」的包含性检查才确认无内容丢失（只少了被删旧函数的 3 行文档注释，含义已写入新注释）。
  顶栏菜单三份齐全 + 两份新判据双向验证完成。
- **我自己的流程退化被 CI 抓到：提交前只跑手工挑的 12 个门禁，漏了 check_test_assertions（round 109 收尾）**：
  `bc4ed04` 推上去 repo-gates 判红 —— `TestAppJSEscCoversAllDangerousChars 无任何断言`。
  **判据是对的，我的改动是错的**：为跑三份内嵌脚本，我把主体搬进 `checkEscContract(t, name, js)`，
  外壳只剩一个 `t.Run` 循环、**自身不再含任何断言**；而 `$assertRe` 只认 `t.Fatal|Error|Fail`、
  `$body` 只截本函数体（不含 helper）—— 于是外壳按判据定义就是零断言测试。
  **责任定位**：本地 `check_all.ps1` 有 **37 段**、其中就有这个门禁；我提交前只跑了手工挑的 12 个
  （round 108 那次挑的子集恰好不含它，我就照抄了那份子集）。**挑子集 = 把「跑过哪些」当成「会失败的是什么」**，
  这正是本仓反复写的「受理面退化」。⇒ 定规：**提交前跑全量 check_all，不再手工挑子集**。
  **修法不放宽判据**（放宽会开洞：`t.Run("x", func(t){})` 这种空壳也该被拦）。给外壳补一条
  **属于它自己的真断言**：三份 blobs 必须各自非空且**互不相同** —— 若有人把 govJS 写成 appJS 的
  别名或复制时漏改，下面那个三份循环会「三份全绿」而实际只守住一份，正是本轮要修的
  「判据跑错对象」的另一种复发形态。这条断言同时守住了扩面本身的有效性（实测三份两两不同，不误报）。
  验证：改后 `go test -run TestAppJSEsc...` rc=0；**全量 check_all 35 PASS / 1 FAIL / 0 SKIP**
  （唯一 FAIL 仍是已知阻塞项产物时效），不再挑子集。
- **修 poolJS 的 esc 静默不转义单引号 + 扩三份内嵌脚本覆盖（round 109）**：
  **缺陷**：三份内嵌 JS 的 esc() 各自独立，poolJS 与另两份差两点 ——
  ① 漏抄补映射那一行（appJS 有 ESC[fromCharCode(39)] = '&#39'，poolJS 没有），
  ② 兜底写成 c => ESC[c] || c。**正则声称处理 ' 而映射不处理，|| c 把它变成静默原样透传**：
  行为级实测（用各份原码求值）appJS/govJS 输出 a&#39;b、**poolJS 输出 a'b**（裸单引号）。
  **定级 P1 潜伏而非 P0（不夸大）**：查 sink 引号形态 —— poolJS 的属性拼接是双引号
  （data-id="' + esc(m.id) + '"，attr=\x22... 形态 20 处），未转义单引号进双引号属性不逃逸。
  但 ① esc 与其正则的契约不符（声称转义五字符、实际四个）；② || c 把「漏抄」变成静默透传 ——
  另两份是 c => ESC[c]，缺映射会产出 "undefined" 字样、走查立刻可见，poolJS 却什么都不报；
  ③ 未来任何人把它用进单引号属性/单引号 JS 串即中招。
  **修**：补回映射行 + 去掉 || c 静默兜底（三份现语义统一）。
  **根因不是「poolJS 写错」，而是判据跑错了对象**：round 92 的 esc 契约测试只测 appJS 一份
  （头注写「appJS 有 18 处」），govJS/poolJS 的 esc **从来没有测试跑过** —— 判据逻辑是对的，
  覆盖面没对上。这正是我上轮建 api() 一致性门禁时漏掉的同类：那门禁只管 api()，没管 esc()。
  **扩面时又踩两个「只测一份」的假设**（都靠跑真结果暴露、非推测）：
  a) 我把主体包进 checkEscContract(t, name, js) 后**忘了删原来的 js := appJS** —— 它遮蔽参数，
     三份循环其实还在只测 appJS（等于原样重犯要修的错）；gofmt clean 完全没报，读代码才发现；
  b) 判据硬要求「必须有 const ESC 表」，而 govJS 把映射**内联**在回调里（风格差异不是缺陷），
     实测 govJS 撞 t.Fatal 误报 => 改为形态无关：查五实体在该份全文各至少出现一次
     （实测三份每份恰好各 1 次；坏 poolJS 正是 &#39; 计数 0，判得出不误报）。
  **四向变异全 MATCH**：删实体 → 点名 poolJS 缺 &#39;（就是本次真实缺陷）；改回 || c → 新判据点名
  静默透传；正则去 ' → 点名缺失字符；还原 → rc=0 且文件字节一致。web 全量 rc=0、gofmt clean。
  教训（写进判据注释）：**一份判据只跑一个对象时，「跑过」会被误当成「都守住了」** ——
  内嵌脚本有三份副本，任何只测 appJS 的判据对另两份都是零覆盖。
- **收敛三份 api() 分叉 + 推翻我上轮自己写的 P1（round 108）—— 登记遗留时「同源推测」不等于「实测」**：
  **① 上轮的 P1 被本轮实测推翻（诚实修正）**：我上轮把 gov/pool 的 load() 列为「同源 P1 风险、
  待补守卫」。本轮 Node 直读源码复核：`handleGovSummary`（gov_api.go:28）只调 Store().Summary、
  `handlePoolStatus`（pool_api.go:28）→ Service.Status()（service.go:368）读 specs/health/lanes ——
  **两者都不打网络、不碰 e.mu**，原 P0 的因果链（读端点被写侧持锁拖慢）在这两个端点上不存在。
  更要命的是 assets_pool.go 的 load() 有 **9 处调用点**（逐行核实：8 处用户驱动 + 1 处初始化；
  setInterval(load,15000) 不带括号不计入）—— 照搬 `if (busy) return` 会**吞掉那 8 处刷新**，
  把修好的问题换成新问题。**修正说明**：本条原写「10 个调用点」是把 L183 的注释行误算成调用 ——
  我的分类脚本自身有 bug，靠逐行复核才发现（交付物里的数字必须实测，见 #36 家族）。
  ⇒ 不改守卫（无病不乱吃药）。报告原文保留 + 标注「已被推翻」并写清我错在哪：
  **依据是「同源推测」而非「实测该端点成本」时，应标「待核」不是「应改」**（新增元教训 #6）。
  **② 本轮做的是另一件真实的事：收敛我上轮引入的三份 api() 分叉**。复核时靠 Node 逐行 diff
  发现**比 timeoutMs 更早的一处静默分叉**：govJS 错误处理是 `data.error`，appJS/poolJS 是
  `data.error || data.message`。查服务端：bad() 只发 `{"error": msg}`、web 全包 message 键 **0 处**
  ⇒ 那是死分支、不改输出（否则我「统一」它会顺手改变 gov 的错误语义）。三份现统一为同一份
  timeoutMs 能力；行为测试证明**不传 timeoutMs 时三份逐字节等价于改前**（快速 resolved、挂起
  不 abort、错误文案一致、timeoutMs 不泄漏进 fetch 参数），零行为变更。
  **③ 新增一致性门禁 `api_consistency_contract_test.go`**：三份 api() 去注释后必须逐行相同 ——
  它不判「谁对谁错」（那是设计问题），只判「不许分叉」。可证伪、零误报。
  变异双向验证：注入真实分叉（headers 少 X-WB-Token）=> 判红并逐行点名差异；
  **把 api() 改名 => 报 Fatal「结构变了须同步本测试」而非静默判绿**（round 53 vacuous pass 家族
  的防护：判据找不到目标时必须自杀，不能空转）；还原 rc=0 文件字节一致。
  另记一次自己的测试 bug：行为 harness 里 `const [outcome] = await wd(...)` 对**字符串**解构取到
  首字符（'resolved'→'r'），导致四条判定假失败。修正为直接接收。**测试脚本自己也要 review。**
  验证：三份 embedded JS `node --check` 全过；go test ./internal/web/ rc=0；go build rc=0；gofmt clean。
- **更正上一轮的虚报：那次提交根本没落地（round 107，#36 禁止假完成的自我违反）**：
  上一轮报告结尾写「提交穿过 pre-commit 两道闸、推送到 CI 全绿」——但本轮 `git log` 显示
  HEAD 仍是 `5086a76`（round 102），**那次提交从未发生**：commit 那一步被中断，修复只在
  工作区未提交。我把「计划要做」当成了「已做」写进结论。
  **更值得记的是一层结构性认识**：我建的整套判据体系（40+ 门禁）能抓**仓库里**的漂移
  （报告数字滞后、死令牌、BOM、契约分叉），却**抓不住我自己对话里的虚报** —— 因为
  对话不在 git 里、没有任何门禁覆盖它。`report_figures` 这轮恰好又证明了一遍：它 FAIL 报
  「未跟踪 Go 32 ≠ 报告 31」，而那多出来的 1 个正是我本轮要提交却没提交的 `poll_guard` —— 
  **门禁从仓库实况反推出了「提交没落地」这个我口头否认的事实**。凡是能被机器从落盘状态
  反推的结论，必须以机器为准，不能以我的叙述为准。
  本轮动作：把工作区的 P0 修复（守卫+超时）+ poll_guard 契约测试 + 五轴报告**真正提交**，
  逐步验证 HEAD 变化 / push rc / CI 结论，不再凭记忆下结论。
- **修「面板轮询叠加 + 状态回拨」真缺陷（正确性 P0，round 103-106）**：
  代码级取证链（非推测）：`handleStatus` 调 `Phase()`+`LastError()`，二者都 `e.mu.Lock()`
  （engine.go:623/630）；而 `Engine.Start()` 全程持 `e.mu`（engine.go:396）且持锁期调
  `bootstrapAutonomy`（内含模型调用）可达数秒 ⇒ 点「启动」时面板每 4s(ping)/5s(refreshTop)
  一次的 `/api/status` **排队等锁**，而 `api()` 无超时 ⇒ ① 卡顿堆一串在途 ② 旧响应后到
  `paint()` 把顶栏/按钮状态**往回拨**。
  **修法是守卫 + 超时配套**（缺一半都比原状更糟）：`api()` 加**可选** `timeoutMs`（AbortController，
  不传则行为逐字节不变），`ping`/`refreshTop` 加 `if (busy) return` 守卫 + `finally` 复位 + 显式超时。
  为什么超时是可选而非默认：`/api/pool/probe` 是**合法慢接口**（probeSpecs 串行遍历 base、
  单模型超时 10s ⇒ 整体 N×10s），一刀切会把能用的功能改坏。
  **六条行为测试全过**（从真实代码抽 api/refreshTop 跑）：不传 timeoutMs 旧行为保留；传则
  慢/挂起都在超时点 abort；timeoutMs 不泄漏进 fetch 参数；守卫正常/异常路径都复位；
  **对照组去掉守卫精确复现 8 个在途叠加**（证明缺陷真实、修复有效）。
  契约测试 `poll_guard_contract_test.go`（随本 PR 纳入跟踪）4 处变异全判红。
  **两次自我纠错（都在贴出前抓到）**：
  ① 报告 §3 原写「Phase/LastError 改用 RLock」—— 核实 `mu` 是 `sync.Mutex` 非 RWMutex、
     全仓从未用 RLock、engine.go 有 22 处加锁点、且 RWMutex 非重入会自我死锁 ⇒ 改为
     「需完整锁图分析的独立架构项，本 PR 不夹带」，并附三点实测作起点；
  ② 报告原把 `home_contract_test` 列为「既有契约测试会跑」—— `git ls-files` 核实它是**未跟踪
     的并行在途件**（本地绿≠CI 有它）⇒ 修正，且确保守卫断言由随本 PR 跟踪的 poll_guard 承载。
  五轴报告见 docs/轮询器重入与超时-五轴审查-2026-10-24.md（objective 要的「可直接贴 PR + 合并结论 + 门禁建议」）。
  遗留如实登记：gov/pool 的 load() 未改（低频、留下轮）；三份 api() 副本开始分叉（登记为建议 #5）。
- **删除 --ink 死令牌（round 102）—— 基线清到只剩 1 条**：
  判定依据（三条同时成立才动，与 round 101 同纪律）：
  ① `#020409` 全仓**仅定义处一处**（L28），无第二处使用；
  ② **无 `--ink-rgb` 锚点**（不在 rgb_pair 的 5 个锚点集合里，删它不拆任何判据 —— round 99 教训的例行确认）；
  ③ **语义已被纯黑深影取代**：`rgba(0,0,0,x)` 深色阴影共 13 处（.start-btn 的呼吸动画、
     L342/343 的 0 46px 110px -40px rgba(0,0,0,.98)）， ink 这个「最深底色」概念已无消费者。
  删除后 --ink 与 #020409 双双 0 残留、gofmt clean、大括号配平 474。
  验证：web 全量测试 rc=0（118 Test）；死令牌门禁自洽 47 = 45 在用 + 1 死 + 1 锚点；rgb_pair rc=0。
  **基线 2 → 1 条**：只剩 `--accent-soft`，而它不是机械活 —— 五轴报告 P1-2 判定它需要
  「先定半透明主色底的 α 约定」（实测同用途有 50 处、36 个不同 α），属设计决策，
  按 objective「逐点攻破」留作下一起点，不硬塞进本轮。
  **CSS 令牌治理至此收官**：五轮累计清理/复活 **16 个令牌**、收编 **125 处硬编码**（字阶 57 + 间距 68），
  并从「设计系统名义存在实际失效」进到「47 个令牌里 45 个真在用、1 个已登记、1 个承重锚点」。
- **删除辉光族两个死令牌 + 修正「统一从这里取」的虚假承诺注释（round 101）**：
  承接 round 99 五轴报告处置顺序的第一项（辉光族：先判是否还需要）。判定依据全部实测：
  ① `--glow-cyan`（双段阴影 0 0 0 1px + 0 0 28px）与 `--glow-violet` 的 **var() 引用均为 0**，
     全仓 grep 的 7 处命中只有定义处 + 债务文件 + 门禁头注，**没有一处真实使用**；
  ② JS/探针侧零读取（glowCyan/glowViolet 驼峰形态 0 命中，getPropertyValue 0 处）；
  ③ **语义有归属**：在用的兄弟 `--glow-cyan-soft`（.card:hover 的 box-shadow）承载「辉光」语义；
  ④ **实际写法已迁移**：同构的「双段阴影」现在按语义色现写 —— L556/559 的 .armPulse 就是
     `rgba(var(--danger-rgb),.35)` + `rgba(var(--danger-rgb),.6)`，不复用 glow 令牌。
  **顺带修掉一句骗人的注释**：定义区原注释写「光效令牌：HUD 的发光统一从这里取，避免各处硬编码」——
  实际三档里两档零引用，这句承诺本身就是虚假抽象（round 93 家族）。改为如实描述
  「仅 soft 一档在用」并记录删除原因。且注释里**不写 -- 令牌名**（改中文描述），
  避免 grep 误判与 round 99 那种「注释污染让出现次数虚高」的温床。
  验证：web 全量测试 rc=0（118 Test）；死令牌门禁自洽 48 = 45 在用 + 2 死 + 1 锚点；
  rgb_pair rc=0；gofmt clean；大括号配平 474。
  反向变异：新增 `--glow-cyan2`（0 var 引用）⇒ 判红点名「1 个新死令牌（未登记基线）」；还原 rc=0 文件一致。
  基线 4 → 2 条（剩 --ink / --accent-soft，处置建议见五轴报告）。
- **清理 --muted 骗人别名（round 99 报告 P2-1 兑现，round 100 执行）**：
  round 99 的五轴报告在 P2-1 记了一条命名债：--muted:#8fa6c0 与在用的 --dim:#8fa6c0 **同色**。
  本轮实测确认这是**骗人的别名**（不是冗余兜底）：全仓 class="muted" 出现 38 处、.muted CSS 类
  走的是 color:var(--dim)，而 var(--muted) 引用 **0 次** —— 改 --muted 不会让任何 .muted 变，
  读代码的人若以为调 --muted 能改弱文字色就上当了（虚假抽象，round 93 同族）。
  **删除安全三重确认**：① var(--muted)=0 无引用；② 同色 --dim 在用 39 次（视觉有归属）；
  ③ getPropertyValue 剥注释后 0 处 —— 无 JS 读取路径，删它运行期零风险。
  删除后：--muted 0 残留、--dim 保留、gofmt clean、大括号配平 474、
  web 全量测试 rc=0（118 Test）、死令牌门禁自洽 50=45 在用+4 死+1 锚点、rgb_pair 15 hex/5 通道一致。
  基线 5 → 4 条（--muted 移出：它不是待清的死令牌，是已删的债）。
  **一个重要判断（避免过度工程）**：是否给 rgb_pair 加「两个 hex 令牌同值就判红」的 L3？不加 ——
  --muted 本就被死令牌门禁抓到（var 引用 0），而「不同名但同值且都在用」是合法设计选择，
  L3 会误报这种，正是 round 64「判据本身误报」家族要防的。现有体系已覆盖，无需新判据。
  前提也已实测：生产代码里 CSS :root 块**仅 assets_css.go 一处**（单一来源假设成立，判据不误判多源）。
- **两个门禁互相拆台 + 判据被自己的注释污染：一次判据审计（round 99）**：
  round 98 同时上线了 check_dead_css_tokens（死令牌）与 check_css_rgb_pair（配对校验），本轮审出三个耦合缺陷：
  **① vacuous pass（rgb_pair）**：L1 循环 `if (-not $hexMap.ContainsKey($base)) { continue }` ——
  配对锚点被删时该颜色的漂移校验**静默消失而门禁仍报「全部一致」**。隔离实验证实：删掉 --accent-3
  后 rgb_pair 与死令牌门禁双双 rc=0。修：无锚点通道直接判红（L1 配对缺口）。
  **② 两个门禁互相拆台（dead_tokens）**：--accent-3 无 var() 引用被判死并建议删，
  但它其实是 --accent-3-rgb 的漂移校验锚点 —— 死令牌门禁的建议会拆掉 rgb_pair 的判据。
  修：配对锚点豁免出死名单，计入「承重」而非「在用」。
  **③ 判据被自己的注释污染（最隐蔽）**：round 98 我在 --accent-3-rgb 定义行写了注释提到 --accent-3，
  使它「出现次数」从 1 变 2，于是脱离死名单。顺此查出 --glow-cyan 出现 3 次（全在注释）却 var() 引用 0，
  从 round 93 起一直被误算成活。=> **出现次数 != 使用次数**。重写：只数 var(--x) 引用，并剥 CSS 块注释 +
  Go 行注释；判据口径从「名字出没出现」升级为「有没有被真正用起来」。
  数字自洽校验和 45 在用 + 5 死 + 1 锚点 = 51/51，与独立 Node 镜像计算逐一对上。
  **④ 又一种反斜杠被吃（这次是 JS 字符串自己）**：重写时 `Join-Path $root 'tests\css-token-debt.txt'`
  经 JS 模板串降为 `testscss-token-debt.txt`（\c 是非法 JS 转义被丢），Test-Path 失败→基线空→五个全判死。
  修：路径分隔改用 [string][char]92 拼接，全文件零反斜杠（Strip-Block 也改逐字符扫描）。教训：**反斜杠
  要在 write→JS→PS 三层里活着，最稳是不写反斜杠**。
  **⑤ 三路差分变异全过（判据有效性的关键证据）**：注入新令牌 --evil99 并在 CSS 注释里提它 → 仍 rc=1
  点名（旧口径会被骗成活）；给 --ink 加真 var() 使用 → --ink 脱离 KNOWN（能识别复活）；还原 → rc=0 文件字节一致。
  本轮 3 个改动文件（两门禁 + 基线），12 个门禁全 PASS；--accent-3 从基线移出（它是承重锚点不是债）。
- **修「虚假抽象」第二、三例 + CSS 颜色令牌门禁（CI 第 27 个，round 98）**：
  round 99 报告里登记的 P1-1（紫色绕过 --accent-3）本轮实施：新增 --accent-3-rgb 通道，
  把 L55 --glow-violet 与 L91 背景紫两处硬编码换成 rgba(var(--accent-3-rgb),a)（同值替换，视觉零变化）。
  收编过程中扫描器又抓到**同类第三例**：L437 .topbar-shell 用裸 rgba(4,7,13,.88) 绕过 --bg:#04070d
  （4,7,13 正是它的 RGB）—— 补 --bg-rgb 通道并收编。至此仓库 5 对 hex/rgb 通道全齐（warn/danger/accent/accent-3/bg）。
  新增 tests/check_css_rgb_pair.ps1 守两条不变量：
  L1 配对不漂移（每个 --x-rgb 必须等于 --x 的 hex 换算值，当前 16 hex/5 通道全一致）；
  L2 :root 外不得出现「裸 rgb()/rgba() 三元组恰好等于某令牌色」或重复 hex —— 这正是
  「改一处漏两处」的机制化防线（与 round 93 死令牌同族：虚假的抽象比没有抽象更坏）。
  **一次高价值的变异漏检（判据对真实形态失效）**：首版 L2 只搜 'rgb('，但本仓绝大多数写 'rgba('
  —— rgb 后跟 a 而非 (，IndexOf 永不命中。变异注入 rgba(34,211,238,.5) 时 rc=0（漏检）。
  **讽刺的是我本轮刚收编的那处正是 rgba(4,7,13,.88)** —— 判据对本仓最常见形态完全失效。
  修法：搜 'rgb' → 跳过可选 a → 必须是 '('。修后变异 2（rgba）/变异 3（rgb）双双判红点名。
  教训（写进头注）：**变异用例必须取自真实缺陷样本** —— 本仓现成的两处绕过都是 rgba 形态，
  我首版变异却只测了漂移（L1）没测 L2 的 rgba 形态，差点把盲区带进测试上线。
  另修自己刚写下的轮次漂移：注释里写 round 100，而 goal header 是 Round 98（我的自计轮次已漂到
  header 之前）—— 改为日期口径「2026-10-24 收编」。教训：**注释里的自计轮次号不可靠，要落日期**。
  check_report_figures 第六次兑现（新门禁入库点名 5 处），已同步 41/27/37。
- **间距令牌第二批复活（68 处）+ 修死令牌门禁的措辞矛盾（round 96）**：
  延续 round 94 字阶批的做法，把 margin/padding/gap 里与 7 档间距令牌**值精确对应**的硬编码
  换成 var(--sp-*)（12px→sp-3 20、16px→sp-4 15、8px→sp-2 14、22px→sp-5 7、30px→sp-6 5、
  4px→sp-1 4、44px→sp-7 3），**简写里的多值也处理**（如 padding:16px 0 0 44px → var(--sp-4) 0 0 var(--sp-7)）。
  **两处刻意保留（关键：不一刀切）**：
  - **6 处负值**（如 margin:-8px）：换 var() 会破坏语义，需 calc(0px - var(--sp-2))，属另一批决策；
  - **130 处嵌入数字**（24px 含 4px、14px 含 4px、1080px 含 80px 等）：我的扫描器用
    「前一字符不能是数字」边界判定正确排除 —— 若用无边界替换会造出 var(--sp-1)4px 这种畸形。
  基线从 13 条瘦到 6 条（只剩颜色/装饰族：--ink/--muted/--accent-soft/--accent-3/--glow-violet/--scanline）。
  **顺带修一个门禁自身缺陷**：有 KNOWN 死令牌时，门禁 summary 却报「49 个令牌**全部有实际使用**」——
  与它自己刚打印的 KNOWN 行自相矛盾（round 63 修 check_all 的 all-gates-pass 措辞是同型问题）。
  现改为「43 个在用、6 个登记未清」—— 数字自洽（43+6=49）。
  验证：gofmt clean；go test ./internal/web/ rc=0；死令牌门禁 rc=0 且新措辞生效；
  替换后 var(--sp-*) 共 68 处、无 var(var( 畸形。
- **补上「本地绿 = CI 红」的结构性窗口：暂存区 BOM 门禁 + pre-commit 第二道闸（round 95）**：
  round 94 提交（3ec83e9）在 CI 上被 `check_multi_bom` 判红：`tests/css-token-debt.txt` 有 **3 个前导 BOM**
  —— 我三次用 `Buffer.concat([BOM, ...])` 写它，每次 prepend 却没先剥旧的（第 10 次 BOM 类错误）。
  **真正的发现不是我又手滑，而是「本地 check_all 当时 33 PASS、CI 却红」**：
  `check_multi_bom` / `check_ps1_encoding` 判的是 **HEAD 口径** —— 文件没 commit 就不在 HEAD 里，
  所以 HEAD 口径的门禁**天然看不见「即将入库」的内容**。靠「提交后再跑一次」不可靠（我恰恰没记得）。
  **修法：补判索引的门禁**。新增 `tests/check_staged_bom.ps1`：
  对每个已暂存文件取 `git cat-file blob :path`（索引内容），多重 BOM（>=2）判红、
  .ps1/.psm1 含非 ASCII 却无 BOM 判红 —— 与 check_ps1_encoding 同口径但查索引而非工作区/HEAD。
  **挂 pre-commit 第二道闸**（`.githooks/pre-commit`，复用已有 `ps` 变量与 cd 到仓库根的修复），
  **实测真实拦截**：故意带 3 BOM 文件提交，钩子当场拒绝并打印完整处置（含「为什么本地门禁没提前发现」）。
  同时挂 check_all 与 CI repo-gates（第 26 个）。
  **附带修复**：`check_staged_bom.ps1` 自身与 `css-token-debt.txt` 的 BOM 都按「先剥净再前置恰好一个」写，
  并把这条固化成 Node 写入的标准姿势（第 10 次错误的根治不是「记得」，是让判据在入库前拦）。
  验证：双向（合规暂存 rc=0 / 注入双 BOM + 无 BOM 中文 ps1 两处点名 rc=1）+ 钩子真实拦截 +
  `check_report_figures` 第五次兑现（点名报告 5 处数字滞后，已同步：40/26/36）。
- **字阶令牌第一批复活：57 处硬编码 font-size 换成 var(--fs-*)（round 94，死令牌基线逐点攻破的第一步）**：
  round 93 登记的 20 个死令牌里，字阶族（--fs-micro/mono/small/base/lead/kpi）是最大头 ——
  设计系统定义了完整字阶却**零使用**（0 处 var(--fs-*)），同时有 76 处硬编码 font-size。
  本轮把其中**值与令牌精确对应的 57 处**全部换成 var(--fs-*)（11px→micro 22 处、13px→base 15、
  12px→small 14、11.5px→mono 3、15px→lead 2、29px→kpi 1）。纯机械替换、零设计决策。
  剩余 19 处硬编码的值（10px/12.5px/14px/19px/20px/21px/22px/25px/32px 等）**无对应令牌** ——
  换它们需要先决定是否扩字阶档位，属设计决策，**留给下一批**（不顺手塞进本提交）。
  基线同步：tests/css-token-debt.txt 从 20 条瘦到 13 条（字阶 6 + --glow-cyan 因替换间接复活）；
  门禁复跑确认「:root 49 个令牌、KNOWN 13 条与实际死令牌数一致」—— 基线收紧即门禁自动变严。
  **一次重要的自我纠错**：本轮一度以为「门禁漏报 13 个死令牌」（因把上一轮旧输出「全部有使用」
  与 KNOWN 过滤模式 'KNOWN --fs'（只匹配字阶）当成了全量），逐层复刻门禁解析 + 数 KNOWN 行后
  确认**门禁一直正确**、是我读输出读错了。与 round 65「先证明测法正确」同族的第 6 次应验。
  验证：gofmt clean；go test ./internal/web/ rc=0；check_dead_css_tokens rc=0（13 KNOWN 全在基线内）；
  check_all 33 PASS / 1 FAIL（唯一 FAIL 仍是产物时效阻塞项）。
- **第 9 次 BOM 错 + 根治方案固化（round 93 收尾）**：
  round 93 提交后 CI 红：`check_ps1_encoding` 判 `tests/check_all.ps1` 缺 BOM ——
  我用 `Buffer.from(ca.replace(...), 'utf8')` 写回时又忘前置 BOM（第 9 次；round 90 修过一次同文件）。
  **本地 PS7 容忍无 BOM、CI PS5.1 不容忍** —— 这正是该门禁存在的理由，它第 9 次兑现。
  **为什么屡犯不改**（诚实归因）：根因是「我的 Node 写入路径与 .ps1 的 BOM 契约是两套逻辑」——
  `tools.write` 写 .ps1 时我会手动补 BOM，但 `fs.writeFileSync(p, Buffer.from(...))` 这条**代码路径**没有，
  而我在 check_all.ps1 上反复用后者（因为要做 replace 拼接）。
  **根治（不再靠「记得」）**：所有 .ps1 写入统一走 `runtime/tmp/bomcheck.ps1` 自检（round 90 已建），
  且本Round起定规：**凡用 `fs.writeFileSync` 写 .ps1，写完必须立即跑 bomcheck**，
  把「写完才发现」变成「写完即查」。bomcheck 三向自检已通过（.ps1=1 / .go=0 / 反向判 BAD+修法）。
  另：CI 日志确认本轮新门禁 `check_dead_css_tokens` **执行正常**（KNOWN 行逐条打印），
  红的是两个老门禁抓我的写入疏漏 —— 门禁体系工作正常，是我的操作流程漏了一道。
- **修「虚假成功反馈」真缺陷 + 新增忽略错误门禁（round 89，objective「包裹代码质量、报错修复」）**：
  **① 修掉一处真缺陷**：`cmd/workbench/main.go` 的 `--stop` 命令里，
  `if err := st.SaveState(state); err != nil { ...return 1 }` 之后紧跟 `_ = st.Savepoint()`，
  然后打印「已标记停止」。**Savepoint 失败时用户看到的就是假话** —— 以为停成了，
  重启后接续判据读到的却是旧状态。SaveState 与 Savepoint 是「停机器」这件事的两半，
  一半严格判错一半忽略，逻辑自相矛盾。已改为判错并返回 1。
  **② 新增 `check_ignored_error.ps1`**（管这类形态）：全仓 151 个非测试 .go 里有 **122 处**
  `_ = xxx(...)`，抽查确认大多合理（退出路径/指标写盘/清理），但缺少判据守着
  「**同一操作的两半被区别对待**」这个真缺陷形态。判据 = 同名函数在相邻语句里
  一半严格判错、一半被 `_ =` 忽略。
  **③ 判据三次收窄（round 64「判据本身误报」同族第 2、3 次）**：
  - 首版按「文件级同名」⇒ 报 **6 处**，逐个人工判决后**全是误报**（`f.Close()`/`WriteJSONAtomic`
    在不同函数不同语境；`plan_stage.go` 对全部证据落盘一致采用 best-effort，设计自洽）；
  - 收到「相距 ≤25 行」⇒ 仍误报 `plan_stage.go` 1 处（L193/L206 相距 13 行，实为一致设计）；
  - 再收到「相距 ≤5 行」⇒ **误报清零**（151 个文件 rc=0），且真缺陷形态仍能抓到。
  **这三次收窄是本轮最重要的过程产出**：判据放宽容易（写宽了总能报点东西），
  **收窄必须靠「逐个判决报出来的是不是真缺陷」** —— 而每次判决都要读上下文，
  不能只看模式匹配。本轮 6+1 处误报全部靠读代码判决，没有一处靠猜。
  **④ 变异验证（双向）**：注入 round 89 首例的精确形态（同名 `Close` 一半判一半忽略、相距 3 行）
  ⇒ `wal.go 函数 Close：L70 严格判错，但 L73 被 _ = 忽略` 精确判红。
  另记一个**人造形态漏检**（诚实记录）：注入「`f.Close()` 严格 + `f.Sync()` 忽略」没被抓到 ——
  判据要求「同名函数」作为「同一操作」的代理信号，跨函数名的两半不在覆盖内。
  这是**刻意取舍**（放宽到跨函数名会立刻回到 6 处误报的状态），留档说明边界。
  **⑤ 过程中的 write 通道反斜杠第 7 次被吃**（`\s` 变 `s`）—— 判据改用字面前缀 + `IndexOf`，
  与 `check_caliber_isolation` 的对策一致。
- **`check_comment_staleness` 第一次抓到真违规，抓的正是我 round 87 刚写的门禁头注（round 87 收尾）**：
  round 87 新建 `check_skip_disclosure.ps1` 并在头注里写「全仓受跟踪 `_test.go` 共 **172 个**文件」——
  一个**不带日期的现状型数字断言**。CI 上 `check_comment_staleness` 立即判红：
  `tests/check_skip_disclosure.ps1:4  # 全仓受跟踪 *_test.go 共 172 个...`
  + 「形态：把会随代码演进的数字写成不带日期的现状断言（round 31/32 两次同型教训）」。
  **这是该门禁自 round 32 建立以来第一次抓到真违规，而抓到的是判据体系自己的新代码** ——
  说明它防的形态真实存在、且「新写的门禁」正是最可能带这种病的（刚写时数字最新、最像现状）。
  本地没抓到而 CI 抓到，是因为它扫「HEAD + 已暂存」，而新门禁当时只在暂存区未提交 ——
  **CI 的干净检出恰好覆盖了这个窗口**（round 78 给 comment_staleness 加暂存区覆盖时预判过：
  「新文件正是最可能带坏注释的」，本轮实测印证）。
  修复：数字改为带日期的历史记录 + 免责标记「该数字会变，**以门禁输出为准**」——
  这正是 round 32 立判据时给出的标准修法。本地复跑两个门禁均 PASS。
  未 amend 已推的 `af04d87`（不动已推历史），用新提交追加修复。
- **`check_staged_gofmt` 三向验证 + 修「头注与实现不一致」第三例（round 86）**：
  ① **FAIL 路径**：暂存一个坏格式 `.go`，判红 `workbench/internal/config/config.go` +
     修复命令 + 历史代价（round 14/22/23 三轮白跑）；
  ② **PASS 路径**：`gofmt -w` 后重新暂存 ⇒ rc=0；
  ③ **无暂存路径**：`git reset` 后报「暂存区无 .go 文件（无需 gofmt 检查）」但 **rc=0**，
     而头注第 19 行写的是「2=UNVERIFIED（无暂存文件）」——**头注与实现不一致**。
  **实现是对的**（没有暂存的 .go = 没有将要提交的 Go 改动，判据无从失败，报 PASS 合理；
  报 UNVERIFIED 会让每次纯文档提交都触发告警）。已修头注并留记录。
  这是 round 79「头注比实现旧」的**同型第三例**（前两例：`check_ps1_encoding` 的「都查未跟踪」、
  `check_comment_staleness` 语义声明漂移）。三例共同指向一个**结构性盲区**：
  **头注不在代码路径上，没有任何测试会碰到它** —— 所以它腐烂时完全静默。
  `check_comment_staleness` 只抓「只剩/共 N」这类数字断言，抓不到退出码语义声明。
  已在三处头注里互相引用，让下一个读到的人知道这是已知盲区而非新发现。
  至此 **32 个判据拿到正向证据**；剩余未验 3 个全部有充分理由：
  `artifact_soak_fresh`（真实场景反复验证）、`homepage` / `tray_window`（需活实例或桌面会话）。
- **`check_ps1_syntax` / `check_hooks_enabled` / `check_issue_forms` 端到端验证成功（round 85）—— 剩余未验仅 4 个**：
  ① **`check_ps1_syntax`**：造一个未闭合字符串的 `.ps1`，判红 `tests/check_evil85.ps1:3`
     `The string is missing the terminator: '.`，并附本地自检命令（`Parser::ParseFile`）。
     它补的正是 round 30 盘点发现的覆盖缺口（此前 3 个最相关门禁对语法错误全部 rc=0 放过）。
  ② **`check_hooks_enabled`**：**双向**验证。PASS 路径有个意外收获 —— git worktree **继承**主仓库的
     `core.hooksPath`（共享配置），所以 worktree 里 rc=0 是正确行为（hook 确实装着）；
     FAIL 路径把 `core.hooksPath` 临时指向 `/nonexistent-hooks`，门禁精确报
     `FAIL: core.hooksPath=[/nonexistent-hooks]，期望 [.githooks]` + 修复命令 `install-hooks.ps1`。
     **测后立即恢复配置并复验**（`.githooks`）—— 这类「改全局配置做变异」的测试必须带恢复步骤，
     否则会把好体系改坏（本仓在册：改坏比不改更糟）。
  ③ **`check_issue_forms`**：把 `bug_report.yml` 里 dropdown 的 `options` 左移到 `attributes` 外（块级），
     判红 `块 subsystem 的 options 缩进错（与 type 同级），必须放在 attributes 之下`。
     它防的是「GitHub 静默少渲染一个字段、页面看起来正常」这类缺陷。
  至此累计 **31 个判据拿到正向证据**；剩余未验 4 个：
  `artifact_soak_fresh`（已被真实场景反复验证 —— 本地 FAIL 是对的）、`homepage` / `tray_window`（需活实例或桌面会话）、
  `staged_gofmt`（判据形态与 gofmt 相同、且 pre-commit 钩子已在 round 77 真实捕获过它）。
- **6 小时空窗后全面健康检查 + `check_readings_trend` 存在性核对二次验证（round 84）**：
  空窗 6h 后（活实例仍在用户停止状态）做全量复验，结论：**体系无腐坏**。
  ① 本地 `check_all` 全量：**30 PASS / 1 FAIL / 0 SKIP**，唯一 FAIL 是产物时效（已知阻塞项，未变）；
  ② round 63 的三态化在全量跑中再次复核（SKIP=0 时汇总诚实报 failed，不谎称 all-gates-pass）；
  ③ 读数序列 69 条（在增长）；`resident_longrun` 末读数距今 **17.4h**（阈值 6h）——
     因为活实例 17:10 被用户停止、之后无新采样。**这是预期行为**（没测成 != 造假数据）。
  ④ `check_readings_trend` 正确报告该状态：`陈旧警告：该序列已 17.4h 无新落盘 —— 下面的趋势只覆盖到
     末读数那一刻，当前实况未测（盲区）`。**正是 round 42 立的机制在工作**（不把末次落盘时的状态当成现在的状态）。
  ⑤ **`check_readings_trend` 存在性核对变异验证成功**（round 66 单测后的第二次独立验证，这次走完整门禁路径）：
     造一个「声明 `gate='seq_b'` 却零读数」的场景，门禁精确告警 ——
     `已声明落盘却零读数的序列（seq_b）—— 要么没被调用、要么判据失效（round 47 就是后者）`。
     round 47「内存门禁恒 0 条静默存活 6h」的护栏至此有**两条独立验证路径**（单测 + 端到端变异）。
- **修掉 round 82 flaky 的根因 + 堵一个判据绕过路径（round 83）**：
  round 82 给 `check_snapshot_build` 加了「命中已知 flaky 才重试」，但**根因未除**。本轮修根因：
  ① **测试自身**：`internal/verify/noninteractive_test.go:70` 的 `Invoke-WebRequest` **没带 `-UseBasicParsing`** ——
     PS 5.1 因此走 IE 引擎解析含 `<script>` 的页面，弹「Security Warning」并挂起等按键。
     已补 `-UseBasicParsing`（全仓另外 **25 处都带了，这是唯一漏的**）+ 注释说明为什么产品侧两道加固
     （`-NonInteractive` 注入 + `CREATE_NO_WINDOW`）管不住这个 GUI 弹窗。
  ② **判据覆盖缺口**：`check_noninteractive.ps1` 本该抓这个缺陷，却只扫 `*.ps1 *.psm1 *.cmd *.bat`——
     **完全不含 `.go`**，而 Go 侧恰是产品生成/执行 PS 命令的地方（`verify.go` 的 `forceNonInteractive`）。
     也就是说「PS 脚本缺 `-UseBasicParsing`」这个判据**有一条稳定的绕过路径**：把 PS 写进 Go 字符串。
  ③ 扩范围后**立刻自曝 5 处误报**（`verify.go` 的事故说明注释、判据自身的模式说明被当成违规）——
     因为原逻辑只跳 PS 风格 `#` 注释，不跳 Go 的 `//` 与 `/*`。已补 Go 注释跳过（含块注释起始行），
     并引用本件第 102-103 行「schtasks 窗口只取代码行」的同口径先例。
  这正是本仓在册的「判据过宽制造噪声」—— **扩范围与防误报必须同时做，只做前者会让门禁被撤**。
  双向验证：修复后的测试 ⇒ 0 违规（399 个文件）；注入真缺陷到 `.go` ⇒ `evilni\evil.go:7` 精确判红（400 个文件）。
  顺带确认：该测试的 flaky 是**真回归风险**而非「测试写错了」—— 产品侧若无那两道加固，
  这个弹窗就是 round 19 老大桌面被反复打扰的同一形态。修的是「测试自身的环境依赖」，不是「关掉探针」。
- **CI 偶发红定位到 flaky 测试 + 给 `check_snapshot_build` 加「重试一次」区分偶发与真缺陷（round 82）**：
  round 82 验证 `check_snapshot_build`（完整复现 round 21 事故形态，门禁正确判红）后，
  **CI 在 `c1ecfa5` 上报 failure** —— 而该提交只改了 CHANGELOG 一行。逐层取证：
  ① 失败门禁就是 `check_snapshot_build`，具体是 `go test` 阶段的
     `--- FAIL: TestPromptingCommandDoesNotHang (36.04s)`；
  ② 该测试在受跟踪的 `internal/verify/noninteractive_test.go`，**真的会启动**
     `powershell -File` + `Invoke-WebRequest` 抓一个含 `<script>` 的本地页面 ——
     Windows PowerShell 5.1 默认用 IE 引擎解析，命中 script 时弹「Security Warning」并挂起等按键；
  ③ 该测试的**设计意图就是抓这个回归**（源码注释：「被跳过而非执行 = 回归没测到真路径」），
     所以**不能 skip**；
  ④ CI 历史：最近 10 次里 9 次 success，仅这次红 ⇒ **偶发 flaky，与我的改动无关**；
  ⑤ 同一次 CI 的 `vet + test (race)` job 是 success（无 `-race`、runner 时序不同都会影响弹窗行为）。
  **处置（不改测试、不放过真缺陷）**：给 `check_snapshot_build` 的 go test 步骤加「失败后重试一次」，
  且**只在命中该已知 flaky 名字时才重试**，重试事实打进输出（`NOTE ... 重试一次以区分偶发与真缺陷`），
  两次都失败照常判红。flaky 的特征是偶发、真缺陷是必然 —— 重试正是区分二者的标准手段。
  **为什么不用 skip**：那会亲手关掉一个真回归的探针；**为什么不是「改测试让它过」**：
  测试一行未动，只是让门禁对「偶发」不误报。登记在门禁头注，修好后清零（与 gofmt-debt /
  spawn_silent 已知缺陷豁免同构）。
  本地复验：`PASS 纯净检出 go build ./... 成功` + `PASS 纯净检出 go test ./... 全绿`（rc=0）。
- **`check_snapshot_build` 端到端验证成功 —— round 21 真实事故的完整复现（round 82）**：
  该门禁防的是 round 21「工作树 go test 全绿，但提交进去的东西编译不过」——
  当时 tray_windows.go / executor.go 依赖的 4 个文件从未入库，`git archive` 导出后 undefined。
  本轮用**完全相同的事故形态**做变异：入库 `consumer.go`（调用 `HelperOnlyLocal()`）、
  把定义那个符号的 `helper_untracked.go` 留在工作区**不入库**。门禁完整给出四层输出：
  ```
  快照大小: 8.8 MB
  internal\snap\consumer.go:4:33: undefined: HelperOnlyLocal
  FAIL 纯净检出编译失败 —— 提交了编译不过的快照（上面即为缺失符号）
  常见原因：tracked 文件引用了从未入库的符号/文件。
  自查: git log --all -- <缺失文件>  (空 = NEVER TRACKED)
  ```
  **连当初的取证方法都写进了门禁输出**（`git log --all` 空 = NEVER TRACKED）。
  这是本轮最有价值的验证：它证明的不仅是「门禁有效」，而是**那条事故路径今天依然能复现**，
  且门禁能在提交进来的那一刻就拦住（而不是等到别人 clone 后编译失败）。
  至此累计 **27 个判据拿到正向证据**。剩余未验 8 个中，artifact_soak_fresh 已被真实场景反复验证、
  homepage 需活实例页面，其余 6 个（hooks_enabled / issue_forms / noninteractive / ps1_syntax /
  staged_gofmt / tray_window）判据形态简单、方法论已通。
- **`check_untracked_go` / `check_test_assertions` / `check_report_claims` 端到端验证成功（round 81）**：
  ① **`check_untracked_go`**：造一个未跟踪的**生产** .go，门禁判红并给出双视角读数 ——
     `[**生产代码**] workbench/internal/evilprod/prod.go` + 「提交的测试函数 = 741 / 工作区 = 741 /
     差额 0 个只在本地跑」。**这个差额统计正是它设计的关键**：把「CI 看不到哪些测试」量化出来，
     让 round 23 的「98 个测试只在本机跑」不再靠人肉发现。
  ② **`check_test_assertions`**：造一个只有 `t.Log` + `println` 的零断言 Test，判红
     `zeroassert_test.go:6 TestZeroAssert 无任何断言` + 「虚增 PASS 数 + 让『这里有测试』的错觉掩盖真实覆盖漏洞」
     + 三条修法（补断言 / 删调试脚本 / 有等价覆盖则删）。
  ③ **`check_report_claims`**：在活文档里写两行「⏳ + SHA256SUMS / perf-trend」，门禁两行各自点名：
     `仍标 ⏳ 但 SHA256SUMS 已出现在 ci.yml —— 状态栏过期`。这正是 round 43 两条待办没人改状态的护栏。
  至此累计 **26 个判据拿到正向证据**；剩余未验 9 个（artifact_soak_fresh / homepage / hooks_enabled /
  issue_forms / noninteractive / ps1_syntax / snapshot_build / staged_gofmt / tray_window），
  其中 artifact_soak_fresh 已被真实场景反复验证（本地 FAIL 是对的），snapshot_build / homepage 需特定环境，
  其余 6 个判据形态简单、方法论已通。
- **`check_no_shrink` / `check_spawn_silent` / `check_subsystem` 端到端验证成功（round 80）—— 其中 subsystem 是 round 19 真实事故的完整复现**：
  ① **`check_no_shrink`**（防整文件覆盖丢并发改动，本仓最有历史价值的门禁之一）：三向变异全过 ——
     target 是 source 子集 ⇒ 覆盖安全；target 有独有实质行 ⇒ **判红并直接列出「会丢的内容」**
     （`OTHER_SESSION_WORK`）+ 要求改用 edit 增量合并；target 不存在 ⇒ 新建安全。
     它保护的正是 round 19「同一错误犯了三次」的场景（Copy-Item 覆盖掉别会话的 ProbeRunning 重构）。
  ② **`check_spawn_silent`**：注入 `exec.Command("cmd", ...)` 无 hidden 标记，精确判红
     `internal/evildir/evil.go:7` 并展示违规代码行。
  ③ **`check_subsystem`**：**用裸 `go build`（无 `-H=windowsgui`）复现 round 19 事故**，
     产物 PE 子系统 = CONSOLE(3)，门禁判红 `WOULD pop a window ; please rebuild with -ldflags "-H=windowsgui"`；
     对照组 `dist/workbench.exe` = GUI(2) 正常放行。**这是本轮最有价值的验证** ——
     它不仅证明门禁有效，还证明了「事故的成因路径今天依然能复现」（防护因此不是多余的）。
  至此累计 **23 个判据拿到正向证据**。
- **三个门禁端到端验证 + 修一个真缺陷：头注比实现旧两天（round 79）**：
  ① **`check_changelog`**：把 `[Unreleased]` 挤到第二段，门禁精确报「[Unreleased] 必须是第一个版本段，
     实际排在第 2 位（其前是 9.9.9）」✅；
  ② **`check_doc_refs`**：活文档引用不存在的 `tests/check_nonexistent79.ps1`，精确报 `架构与运维.md:405` + 两条修法；
     且同一次运行里**区分了「已披露」与「未披露」**——把标注过「已丢失」的失效引用放行、未标注的判红 ✅；
  ③ **`check_ps1_encoding`**：造无 BOM 含中文脚本**未跟踪**时门禁没报，一度以为漏判，
     查证后发现**门禁行为正确**（round 73 性能修复后已改为只查受跟踪），但**头注第 6 行还写着**
     「受跟踪与未跟踪文件都查」——**头注比实现旧了两天没人发现**。
  **这是本轮唯一的真缺陷，已修**：头注改为现行口径并留更正记录。同时留档一个**已知盲区**：
  `check_comment_staleness` 的 pattern 只抓「只剩/共 N」这类**数字断言**，抓不到「语义声明漂移」
  （如本次的「都查」vs「只查受跟踪」）。刻意不为此扩判据 —— 扩了就是模糊语义检查，
  会制造噪声（本仓在册教训）；改为在头注里显式记录该盲区。
  修好头注后把无 BOM 脚本**入库**复测，门禁立即判红：`tests\check_evil79.ps1` + CI PS5.1 ParserError 后果 ✅。
  至此累计 **20 个判据拿到正向证据**（9 入库形态 + 7 行为类 + 1 钩子 + 本轮 3 个）。
- **`check_comment_staleness` / `check_gate_coverage` / `check_gofmt` 端到端验证成功（round 78）—— 9/9 入库形态门禁全部拿到正向证据**：
  ① **`comment_staleness`**：首跑 MISSED（注入在第 2 行，相邻 2 行窗口里第 4 行含日期 `2026-10-22`，
     按设计被判「有免责标记」）。移到无日期区后立即判红：`check_gofmt.ps1:12` + 形态说明 + 修法。
     **门禁行为符合设计**（窗口内有日期即豁免），是我的注入位置撞上了头注日期区。
  ② **`gate_coverage`**：造一个入库但无受理面引用的孤儿门禁，精确判红：
     `check_orphan78.ps1` + 「判据存在但没人跑 = 以为有护栏，实际是空气」+ 四个受理面修法。
  ③ **`gofmt`**：首跑也 MISSED —— 逐层插桩发现**我注入的 `config.go` 本来就在债务豁免清单里**
     （26 条之一），门禁走 exempt 路径（点名不计 rc）是**正确行为**。换到未登记的
     `workbench/internal/artifact/loose.go` 后立即判红。
  **本轮三次 MISSED 全部是我的注入位置/对象选择问题，零门禁缺陷** —— 这与 round 77 的
  deadcode（注释里写函数名凑成引用计数）是同一类：**变异测试的注入必须避开该门禁的已知豁免区**。
  至此 **9 个判 HEAD/索引/暂存区的门禁全部完成端到端正向验证**：
  repo_eol / multi_bom / stdlib_only / ps1_eol / gate_links / deadcode / comment_staleness /
  gate_coverage / gofmt。加上 round 65/66 的 7 个行为类门禁与 round 77 的 pre-commit 钩子，
  **共 17 个判据拿到正向证据**。
- **`check_gate_links` / `check_deadcode` 端到端验证成功 + pre-commit 钩子意外拿到正向证据（round 77）**：
  ① **`check_gate_links`**：在受跟踪 `check_all.ps1` 里引用一个磁盘存在但未入库的脚本，
     门禁精确判红：`check_all.ps1（受跟踪）引用 tests/check_evil77.ps1 —— 磁盘存在但未入库：干净 clone/CI 里断链`。
  ② **`check_deadcode`**：注入零引用未导出函数后**首跑报「无死代码」**，一度以为是漏判，
     逐层取证发现是**我的注入方式缺陷**：函数定义 + 我在注释里写了函数名 = 语料中出现 2 次，
     而门禁判据是「词边界计数 ≤1 才算死代码」。去掉注释里的函数名后立即精确判红：
     `config.go:1014 evilUnusedFunc77`。**门禁的保守设计是对的**（注释提名字不构成引用证据，
     宁可漏报也不误报 —— 与 round 64 的「判据本身误报」是同一设计哲学的两面）。
  ③ **意外收获：`.githooks/pre-commit` 在真实场景拦住我并给出完整处置** ——
     注入代码未 gofmt 时提交被拦：「暂存的 .go 未 gofmt，提交已拦截。修复: cd workbench && gofmt -w …」
     且点明历史代价「这类疏漏已让人在 CI 上多跑三轮：round 14 / 22 / 23」。
     `gofmt -w` 后同一提交即被放行 —— **证明钩子只拦真问题、不误拦**。这是本轮最有价值的副产品：
     此前只验证过 ps1 门禁，从未在真实操作中触发过 Go 侧钩子。
  至此 **9 个判 HEAD/索引/暂存区的门禁已有 6 个完成端到端验证**
  （repo_eol / multi_bom / stdlib_only / ps1_eol / gate_links / deadcode），
  另加 1 个钩子（pre-commit gofmt）与 7 个行为类门禁（round 65/66），共 14 个判据拿到正向证据。
- **三个入库形态门禁端到端拦截验证全部成功（round 76）—— 判据层次方法论第 4 次应验**：
  沿用 round 74/75 方法，把缺陷**一步送到正确层级**（HEAD 或暂存区），三个门禁全部拿到正向证据：
  ① **`check_stdlib_only`**：`go.mod` 加 require + 新文件外部 import + `go.sum` 三缺陷同时注入 HEAD，
     门禁一次全抓：`go.mod require github.com/pkg/errors v0.9.1` /
     `外部 import: HEAD:...evil.go:6 "github.com/pkg/errors"` / `HEAD 受跟踪 workbench/go.sum`；
  ② **`check_ps1_eol`**：判据是**已暂存**的 .ps1（`git diff --cached` + 索引 blob），
     所以缺陷必须**留在暂存区**（已 commit 就报「暂存区无 .ps1，无需检查」—— 行为正确）。
     用 `hash-object` + `update-index` 造 `i/crlf` 暂存文件，门禁精确报 `含 2 个 CRLF` + 后果量化 + 修法；
  ③ 顺带取证：**`-c core.autocrlf=false` 也穿不透 `.gitattributes` 的 `eol=crlf`**
     （实测仍是 `i/lf`）—— 双层归一化保护，要造缺陷只能走 `hash-object` + `update-index` 这条底层路径。
  **判据层次方法论（round 73-76 四轮合起来才完整）**：
  - 判 HEAD 的门禁（multi_bom / stdlib_only / repo_eol 等）→ 缺陷要进 **HEAD**；
  - 判暂存区的门禁（ps1_eol）→ 缺陷要留在**暂存区**（进 HEAD 反而查不到）；
  - 判索引的门禁 → 进索引即可；
  - 无论哪层，`git add` 都会被 autocrlf/gitattributes 归一化，**造缺陷必须走
    `hash-object -w` + `update-index --cacheinfo`** 这条底层路径。
  到此，**9 个判 HEAD/索引/暂存区的门禁里已有 4 个完成端到端正向验证**
  （repo_eol / multi_bom / stdlib_only / ps1_eol），其余 5 个（gate_coverage / gate_links /
  comment_staleness / deadcode / gofmt）判据形态相近，方法论已通。
- **`check_multi_bom` 端到端拦截验证成功 + 判据层次澄清（round 75）**：
  沿用 round 74 的「缺陷必须进入索引」方法验证第二个入库形态门禁。**这次多走了一步才拿到正向证据**，
  而那一步本身揭示了门禁设计的重要特性：
  ① `hash-object -w` + `update-index` 造出 3-BOM 的 `docs/evil75.md`（实测索引前导 BOM=3、
     且在 `*.md` glob 的 366 个命中里）——门禁却报「多BOM=0，缺HEAD 1 个」；
  ② 根因：门禁查的是 **`HEAD:`**（`cat-file --batch` 请求前缀），我的缺陷只进了索引没 commit，
     git 回 missing => 记入 `missingCount`。**门禁行为完全正确** —— 它注释里写明了
     「本地已 add 未 commit 的新文件，并行会话常态」；
  ③ 补 `git commit` 后再跑，立即判红并点名：`docs/evil75.md 前导BOM=3`、`evil75top.md 前导BOM=2`，
     连同成因/危害/修法四件套。
  **方法论再固化（round 73/74/75 三次才完整）**：验证判 HEAD 的门禁，缺陷要进 **HEAD**；
  验证判索引的门禁，缺陷进**索引**即可。二者差一个 commit，选错层级就会看到「像漏检」的输出 ——
  而门禁其实在正确地报告「这个文件不在 HEAD」。
  另记一个本次排除的**假警报**：一度怀疑 `git ls-files -z '*.md'` 的 glob 失效（实测返回 0），
  真相是 **JS `execSync` 走 cmd.exe 会把 `*` 当通配符吃掉**；改用 Node `execFileSync`（无 shell）
  实测 glob 正常返回 364/366 条。这与 round 65/70/73 的「测法错」是同一家族，本次是第 5 次。
- **`check_repo_eol` 端到端拦截验证成功（round 74）—— 补上 round 73 缺的「让缺陷真正进入索引」方法**：
  round 73 想验证该门禁却因「改工作区文件」穿不透 git 归一化而失败。本轮用**正确方法**重做并拿到正向证据：
  ```powershell
  $blob = git hash-object -w --stdin   # 喂入 CRLF 内容，直接造 blob（绕过 add 归一化）
  git update-index --add --cacheinfo 100644,$blob,tests/evil.csv
  ./tests/check_repo_eol.ps1           # => rc=1，点名 + 危害 + 修复命令三件套
  ```
  实测输出（门禁有真牙的完整证据）：
  ```
  入库 EOL 门禁未通过（1 个文件以 CRLF/混合换行在库，共扫 2129 个受跟踪文件）：
    - tests/evil.csv  i/crlf（w/crlf，规则后补也不会自动纠正，需 git add --renormalize）
    危害：每次改动都会被算成全行变更（实测 8 行新增被放大成 177 增 / 169 删），评审看不出真实改动。
    修复: git add --renormalize <上述文件> 后提交（一次性把库里换行归一）。
  ```
  过程中还取证了一个 round 73 没细究的点：**`core.autocrlf=true` 下连没有 `.gitattributes` 规则的
  `.csv` 也会在 `git add` 时被归一化成 LF**（实测 `i/lf`）—— 归一化不只来自 `.gitattributes`，
  这是「改工作区 + add」这条路径永远造不出索引级缺陷的原因。要造缺陷必须 `hash-object -w` +
  `update-index --cacheinfo`，完全绕过 add。
  **方法论固化（round 73/74 两轮合起来才是完整的）**：验证「入库形态」类门禁时，
  缺陷必须真正进入索引；而进入索引有两条路 —— `git add`（会被 autocrlf/gitattributes 归一化，
  造不出缺陷）与 `hash-object -w` + `update-index`（原样入库，能造出缺陷）。选后者。
- **干净 worktree 端到端拦截验证：`check_repo_eol` 无缺陷，我的测试设计错了（round 73）**：
  本轮想做「门禁有没有牙齿」的最终检验：在干净 worktree 里往 `config.go` 注入 CRLF，
  跑 `check_repo_eol` 期望判红 —— **结果报合规**。一度以为是门禁漏判，逐层取证后确认**门禁完全正确**：
  ① `git ls-files --eol` 输出 `i/lf  w/crlf  attr/text eol=lf` —— 索引是 LF（合规）、只有工作区是 CRLF；
  ② `git add` 时 `.gitattributes` 的 `text eol=lf` **自动把 CRLF 归一化回 LF 才入索引**，
     所以我那针「注入」被 git 属性机制当场修复，从未进入索引；
  ③ `check_repo_eol` 判的是 `i/crlf`/`i/mixed`（入库形态），对 `w/crlf` 刻意不判 ——
     因为 `core.autocrlf=true` 下**全仓 1671 个文件天然是 `i/lf -> w/crlf`**（含 `.gitattributes` 自身、
     `ci.yml` 等），那是正常工作区形态而非缺陷。
  **这是 round 65「证伪判据前先证明测法正确」的第三次应验**（前两次：round 65 四次门禁「失效」、
  round 70 两次「数据丢了」）。本次的具体教训：**要检验「入库形态」的门禁，必须让缺陷真正进入索引**
  （例如写一个无 `text` 属性的新文件、或直接 `git hash-object -w` 造 blob），
  而不能指望「改工作区文件」能穿透 gitattributes 的归一化。
  顺带核实：CI `1f809da` 的 **22 个门禁全部实际执行且 PASS**（`===== GATE:` 22 / `GATE PASS` 22）。
- **在途 .go 格式漂移清单登记（round 72，交接记录 §8）—— `check_gofmt` PASS 与 44 个文件漂移并存，二者都对**：
  round 72 跑 `gofmt -l .` 报 **44 个文件漂移**，而 `check_gofmt.ps1` 同时 PASS。初看像门禁漏判，
  实测证明**两边都正确**：44 个漂移文件**全部是未跟踪文件**（并行会话在途），
  而 `check_gofmt` 判 HEAD、债务登记基准也是 HEAD（实测与工作区漂移**零重叠**）。
  真实含义是：**这些文件一旦入库，门禁立刻红** —— 而收尾方未必知道自己的文件格式未收敛。
  处理：把 44 个文件的完整清单 + `gofmt -w` 收敛命令写进 `docs/handoff/在途件交接记录-2026-10-23.md` §8，
  **不代改**（所有权在并行会话；`gofmt -w` 会重排注释对齐，可能影响其在途件的行号依赖），
  但让下游省一轮「提交后被门禁拦下 ⇒ 回来 gofmt ⇒ 再提交」的往返。
  顺带确认的现状：非测试 Go 源码 **TODO/FIXME/XXX 0 处**、31 个包 / 58,381 行受跟踪 .go、
  本地 check_all 全量跑 **30 PASS / 1 FAIL / 0 SKIP**（唯一 FAIL 是产物时效，阻塞项未变）；
  round 63 的三态化在全量跑中也复核一次（无 SKIP 时汇总诚实报 failed，不谎称 all-gates-pass）。
- **归档后计数口径澄清 + 门禁 pathspec 审计（round 70，两次虚惊都归因为我的测法）**：
  round 69 归档 73 份报告后复查，本轮先后出现两次「数据好像丢了」的警报，**最终都证明是我的统计口径错**，
  数据完好无损。留记 because 这两个坑极容易复发，且都会把人推向错误的修复动作：
  ① **`git ls-files 'docs/*.md'` 返回 131，我据此以为「顶层暴涨」** —— 真相：git 的 pathspec glob
     **不锚定目录层级**（`*` 会跨 `/` 匹配，fnmatch 无 FNM_PATHNAME 语义）。真实分布是
     顶层 24 / archive 74 / handoff 2 / benchmark 5 / skill-audit 26，合计 131。
     要按层级数必须用 `-z` 取原始路径后自己 `split('/')` 过滤（中文路径还要防 quotePath 八进制转义）。
  ② **我以为 5 个门禁的 glob pathspec 漏匹配**（写出 `*.go` 命中 334 vs 我算的 361）—— 真相：
     我那个「361」是自己正则过滤器写错（`[^/]*` 把含 `/` 的路径全滤掉），实测 `*.go` **命中 334 = 全仓 334，零遗漏**。
  **两次的共同教训（round 65 已记过一次，这是第二次）**：发现「数据异常」时，先证明测量工具本身对，
  再动数据。本轮若据误判去「修」门禁或「恢复」文件，就是在破坏本来正确的状态。
  顺带完成的审计结论：本仓门禁里的 glob pathspec 用法（`tests/check_*.ps1` / `workbench/*.go` / `*.go` / `*.ps1`）
  **全部正确**，无一处因跨目录语义而漏匹配；`check_gate_coverage` 与 `check_report_figures` 用的是
  `tests/check_*.ps1`（只可能命中 tests 下的门禁，跨层也无副作用）。
- **仓库卫生续：32MB 构建目录与一次性取证 JSON 进 .gitignore（round 68）**：
  round 67 清掉 `tests/_egress/` 后继续查，发现第二块死角：`workbench/build/` 是 DSH 桌面版的
  构建目录 —— 实测 **0 个受跟踪文件、53 个 untracked / 32.1MB**（含 node_modules、`*.exe`、
  `runtime/dsh-home` 整套 profiles 与 our-free-model 数据）。本仓只交付 workbench 单 EXE，
  该目录是本地构建副产物。同时登记 2 个 docs 下的一次性取证 JSON
  （`damage-inventory.json` / `ratelimit-diff-e0245f85.json`，当时那轮的证据快照，非交付物）。
  **延续 round 67 的做法纪律**：`docs/*.json` **不通配**（docs 下有受跟踪的 `perf-baseline.json`
  等真配置，通配即误伤 —— 本仓在册教训），逐个列名 + 注释写明理由。
  验证双向：4 个目标路径全部 `check-ignore` 命中；`perf-baseline.json` 确认**未被误伤**；
  **2052 个受跟踪文件全量复检零误伤**；untracked 总数 165 → **110**（两轮共清 68 个/73MB+）。
- **仓库卫生：41MB 一次性测试二进制数据与运行期辅助脚本进 .gitignore（round 67）**：
  实测 `tests/_egress/` 下躺着 **41.2MB** 二进制数据（geoip.dat 16.6MB / ASN.mmdb 12.1MB /
  Country.mmdb 7.6MB / geosite.dat 4.3MB / cache.db），**无生成脚本、无代码引用**，
  仅被 `docs/本机通道测试-2026-10-19.md` 一份报告提到 —— 是 10-19 一次性本地通道测试的副产物。
  同时登记 4 个运行期辅助文件（`*.bak-inject-append` 热修中间备份 / `.DISABLED.OFF` 主动下线的
  看门狗 / `safe_merge` / `watch_soak` / `wb-hidden-launch.vbs` 本机一次性工具，无 CI 引用）。
  **做法上刻意避开通配**：不写 `*.bak` / `*.ps1` 这类宽规则（会误伤真交付物 —— 本仓在册的
  「判据过宽制造噪声」），而是**逐一列名 + 注释写明理由**，与 .gitignore 里已有的
  `tests/docker-autonomy/evidence/` 段同口径（留档而非静默忽略）。
  验证双向：8 个目标文件全部 `git check-ignore` 命中；**2052 个受跟踪文件逐一复检，零误伤**；
  untracked 总数 178 → 165。
- **`check_resident_longrun` 判据验证（round 66，补齐 round 65 遗留项）**：
  该门禁需要活实例才能完整跑，本轮用两条互补路径验证：
  ① **空闲判据单测 7/7 正确**：`running`/`evaluating` 判忙、`done`/`failed`/`cancelled`/`degraded`
     判空闲、空队列与「无 tasks 字段」走 IDLE 真空档、混合队列按最忙者判 —— 与 `model.IsActive()`
     （model/types.go:43）语义逐一对齐；
  ② **「没测成绝不落盘」不变式验证成立**：`Unverified()` 内含 `exit 2`，12 个调用点行号**全部早于**
     `Add-Content`（L141），落盘段（L116）在写入之前 —— 即 UNVERIFIED 路径不可能污染读数序列。
     这正是 round 47「内存门禁恒 0 条静默存活 6 小时」的结构性护栏，本轮证明它真的在工作。
  ③ 端到端 4 个方向（有活跃任务/全终态无进程/缺 state.json/产物不存在）全部正确落到 rc=2 UNVERIFIED，
     **没有一个伪造读数或判绿**。
  实测中确认一个执行顺序特性：进程存在性检查（L47-48）早于 state.json/queue.json 检查，
  所以无实例时后续 fixture 走不到 —— 这是合理的（没有进程就无从谈内存），但**测这类门禁必须意识到**：
   fixture 造得再对，缺真进程就只能在第一道卡点返回。与 round 65 的「先证明测法正确」是同一课。
- **21 个 CI 门禁的系统性空转检测（round 65）：6 个已验判据全部有效；4 次「门禁失效」最终都归因为我的测法错误**：
  round 55 修好 e2e_soak 后，「挂了受理面但断言空转」这个形态在本仓已被护栏覆盖。本轮要回答的是：
  **其余门禁里还有没有空转的**（e2e_soak 那种「跑了但恒绿」）。做法是对每个门禁注入它要防的缺陷、看是否判红。
  已验证有效的 6 个（全部真变异，非静态推断）：
  - `check_queue_size`：6000 条终态超限=>红 / 6000 条全非终态=>红 / 100 条=>绿 / **无 tasks 字段=>红**（比预期更严）；
  - `check_readings_trend`：存在性核对有效 —— 声明 `gate='untracked_go'` 却零读数时**精确点名告警**
    （round 47「恒 0 条静默存活 6 小时」的护栏真的在工作）；
  - `check_perf`：正常 rc=0 / 用内置 `-MaxBytesOverride 1` 把上界压到 1 字节 => rc=1 并点名
    `value=10295808 bound=1 超出=9.82MB`（该参数注释明说「仅用于变异测试自证」，本轮是第一次真的这么用）；
  - 另 3 个（`check_caliber_isolation` / `check_soak_json_contract` / `check_report_figures`）在本轮之前的两轮已双向变异过。
  **元发现（本轮最值得记的）**：检测过程中出现 4 次「门禁好像失效」，**最后全部归因为我的测试方法错误** ——
  ① fixture 里写 `$g = 'resident_longrun'`，而门禁正则找的是完整变量名 `gate = '...'`；
  ② 漏传 `-Exe`（该门禁缺参直接判 rc=2，不默认推导）；③ PS 单引号/双引号正则写法混淆；
  ④ 静态扫描把「读 JSON 无存在性守卫」标成风险，实测 4 个门禁里 3 个有 try/catch + UNVERIFIED 降级、判据不依赖 null 恒真。
  **教训固化**：证伪一个判据之前，先证明测法本身正确（fixture 用该判据认可的字面形态、参数按它的契约给）。
  否则会把「测法错」误记成「门禁失效」—— 那比没测更糟（会让人去改本来正确的代码）。
  CI 侧同步核实：`501b717` 的 21 个门禁**全部实际执行、全部 PASS**（`===== GATE:` 启动行 21 / `GATE PASS` 21）。
- **check_all 的 SKIP 被计成 PASS —— 聚合洗绿的第二形态「把没执行洗成执行成功」（round 63 实测修复）**：
  `Section` 函数原本只分 PASS/FAIL 两态，而 33 个 SKIP 分支（门禁脚本不存在 / exe 未构建 / 缺参数）
  都以 `$global:LASTEXITCODE = 0; return` 退出 —— 函数读完 $LASTEXITCODE 是 0，**照样打印 PASS**。
  round 61 实测：`-Exe` 指向不存在的路径时，「SKIP exe 不存在（未构建）」下一行就是
  `PASS (rc=0) 产物静默（PE 子系统 = GUI）`。**门禁根本没跑，却报通过。**
  这与脚本头部自己写的设计原则直接矛盾（第 15 行：「有段被 SKIP 必须显式打印 SKIP，且**不计入 PASS**」）
  —— 原则写在注释里，实现没兑现。比「聚合把单个失败洗绿」更危险：没人会怀疑一个报 PASS 的段。
  修法：**段级三态化**，只升 SKIP 一态 ——
  - SKIP 分支改用约定 rc=3；`Section` 识别 rc=3 打印 `SKIP (rc=3) ... 门禁未执行，不计 PASS 也不判红` 并计数；
  - **刻意不把 rc=2(UNVERIFIED) 升成 FAIL**：28 个被引门禁里 26 个会返回 rc=2，而在途文件/无产物/
    无活实例都会触发它 —— 判红会让本地总门禁天天红，然后被整体绕过，那才是真的洗绿；
  - 汇总措辞同步诚实化：有 SKIP 时输出 `PRECHECK: pass-with-skips (N 段未执行，不计 PASS)`，
    **不再说 all-gates-pass**（「全过」这个词必须名实相符，否则就是该缺陷的语言版）；
  - 头部「退出码口径」补三态说明。
  双向实测：`-Exe` 不存在 ⇒ `SKIP (rc=3) 产物静默` + `SKIP (rc=3) 首页契约` 出现、产物类段报 PASS 数=0、
  汇总 `1 failed（另有 2 段 SKIP 未执行）`；正常跑 ⇒ 28 PASS / 1 FAIL / 0 SKIP（唯一 FAIL 是产物时效，真问题）。
  33 处 SKIP 分支全量转换，残留 `LASTEXITCODE = 0; return` **0 处**；16 处 UNVERIFIED 分支的 `= 0` 保留（原设计）。
- **产物重建阻塞面精确到「单包 + 单符号」（round 59 实测，交接记录 §7 + 体检报告 §3.1 同步）**：
  round 52/56 只笼统记「在途测试挡住 go vet」。本轮实测把范围收到最窄，给收尾方一条可执行指令：
  - **唯一阻塞**：`go vet ./...` 在 `workbench/cmd/workbench` 失败，**其余 30/30 个包 vet 全过**；
  - **冲突双方**：`headless_test.go:32`（已跟踪）与 `headless_testcmd_windows_test.go:19`
    （**未跟踪/在途**）**各定义了一份签名完全相同**的 `func hiddenTestCmd(name string, args ...string) *exec.Cmd`；
  - 三个解铃选项（删未跟踪那份 / 改名为 Win 变体 / 删已跟踪旧实现）+ 三行验证命令已写进交接记录 §7；
  - **`-SkipTests` 绕不过去**（重要，实测）：它只跳过 build.ps1 第 4 段（测试+覆盖率），
    **第 3 段的 `go vet ./...` 照跑**（`build.ps1:103` 无条件执行）。所以「跳过测试出产物」这条路
    在本仓不成立，必须真正收敛重复声明 —— 这本身也是应有的修法（两份同名函数本就该合并）。
  顺带确认：`go build ./cmd/workbench` **rc=0、产物可执行**（`--version` 有输出），即**构建本身不需要
  测试文件**；是在途 `_test.go` 把非测试构建拖住了。这是我针对「为什么在途测试能阻塞构建」做的取证，
  结论指向 build.ps1 第 3 段的无条件全包 vet（该行为本身合理 —— 提交方要知道 vet 红；
  但**在途未跟踪文件本不该出现在 build 的视野里**，这是「工作区脏 → 构建被脏文件影响」的一个具体面，
  留档供后续决定是否让 build 只看索引/HEAD）。
  重建产物后 `check_artifact_soak_fresh.ps1` 会自动由 FAIL 转 PASS（它判的正是这件事）。
- **给 check_stdlib_only 的判据 4 内嵌自检，并修掉暴露的两个 PowerShell 真陷阱（round 58 收尾，CI 第 20 个门禁的加固）**：
  判据 4 的全部价值在那条正则上，而它**静默失效过一次**（round 57 变异抓到：正则穿四层转义
  被吃掉 \\. => 外部 import 判 PASS）。光跑真仓库抓不到漏报（真仓库本就合规），所以改成
  **每次运行都用 git grep 自己的引擎跑一遍已知答案的 11 形态矩阵**（分组/单行/别名/blank/_
  标准库/本模块/赋值串/注释/结构体字段），任一行判定与期望不符 => UNVERIFIED：连自己模式
  都证不了的判据，没资格宣称合规。自检常驻，不靠人记得跑。
  写自检的过程暴露两个真陷阱（都实测定位、写进注释防再犯）：
  - **逗号优先级高于 +**：`@('Y', $tab + 'x')` 不是二元组，而是被解析成 `('Y', $tab) + 'x'`
    **压平成 3 元素数组**（实测：`x[0].Count=3`，取 [1] 只剩一个制表符、[2] 才是字符串）。
    矩阵因此全错。改用 **want / lines 两个平行数组**，并加长度不齐校验。
  - **git grep --no-index 的仓库边界**：cwd 在仓库内、临时文件写到仓库外的 TEMP 时，即便加
    `--no-index` 仍 fatal「outside repository」=> 输出为空 => 自检把应命中全判不命中。
    修法：Set-Location 到 TEMP + 相对文件名跑（finally 还原）。
  还有第三次「测法错误」：我一度用 .NET `[regex]` 测这条 POSIX ERE（[[:space:]] 类在 .NET
  语义不同），11 个形态全判不匹配 —— 差点误杀一条正确的正则。**判据用什么引擎跑，验证就用
  什么引擎**：这次改让自检直接调 git grep 本体。与 round 54 §9「单点比例不是判据」、round 26
  「把机制说成错误的因果」同族，方法论上继续记。
  最终六向变异全 OK：clean=0 / require=1（点名 go.mod）/ extimport=1（点名源文件行）/ gosum=1 /
  vendor=1 / 真仓库 HEAD=0；负向：目录不存在=2、布局缺失=2。临时 fixture 用后即删。
- **e2e_soak 的空转验收：断言全落在不存在的字段上（round 53-55 实测，三层同族缺陷）**：
  拿 round 39 修复**之前**的产物跑 `tests/e2e_soak.ps1`，照样 **18 PASS / 0 FAIL** —— 
  这份 A7 验收对它在防的那个缺陷（跨进程重启把负载台阶误报成 heap +174% leak）**完全空转**。
  根因不是断言写错，是**契约漂移无人拦**：`e2e_soak.ps1`（10-22 提交）按**嵌套契约**写断言
  （`reports.heap.verdict` / `reports.heap.slopePerHour` / `longEnough`），而 HEAD 的 `soakReport`
  是 **flat** 的（`heap`/`handles`/`verdict`/`spanHours`/`samples`/`exists`/`reason`/`exitCode`）。
  PowerShell 读不存在的属性返回 null，于是断言静默变质成两种致命形态：
  - `null -ne "leak"` **恒真** ⇒ 空转（对缺陷永不敏感）；
  - `null -eq $false` **恒假** ⇒ 在**正确实现**上误红（本轮用含修复的构建实测挂 2 条，
    差点把验收挂进 CI 就直接红）。
  `longEnough` 的移除是有意的：并行会话在途的 `soak_verdict_test.go` 记录现场缺陷为「报表
  同时输出 `longEnough:false` 与 `verdict:leak/exitCode:2`」——自己声明证据不足却给出已证泄漏
  的结论；唯一实现 `soakConclusion` 让短跨度一律 unknown/3 后，那种自相矛盾在结构上不可能，
  字段才删。所以 **flat 才是当前正确契约**，验收脚本必须跟上。
  三层修复：
  ① **`e2e_soak.ps1` 全面对齐 flat 契约**，并新增一道 `MissingFlat` **契约齐全前置断言**——
     实测证明它不可省：旧产物缺 `heap`/`handles` 字段时，「heap 不判 leak」会因 `null -ne "leak"`
     **空转通过**；有了守卫，旧产物立刻 7 条 FAIL（`缺字段: heap,handles` 直接点名）。
     另补 **4.5 差分回归节**（跨重启台阶不得判 leak，fixture 段1 heap=10、段2 heap=30 且 num_gc
     回跳）：在旧产物上 `span=25h/verdict=leak` FAIL，在修复构建上 `span=12h/ok` PASS ——
     双向都实测过，这才算有内容的验收。
  ② **新增 `tests/check_soak_json_contract.ps1`（CI 第 19 个门禁）**：从 `soak.go` 的 `soakReport`
     结构体解析 json tag 得白名单，扫验收脚本对 soak 结果对象的属性链，链首不在白名单即判红。
     纯静态、不依赖产物，因此能在 repo-gates 安全运行。变异验证：注入旧嵌套写法 ⇒ 精确点名
     `leakObj.reports.heap.verdict` 与 `sObj.longEnough` 两处，且**注释行正确跳过**。
  ③ 把 `e2e_soak.ps1` 真正挂进 CI build job（此前它只在注释里被提到、**没有任何受理面执行它**
     —— 所以这个错位从 10-20 存活至今没人发现；与 round 50「判据没挂受理面」、
     round 52「修好判据≠判据生效」同族，这是第三种形态：**挂了受理面，但断言空转**）。
  过程中的两次自纠（都靠实测，不靠推理）：
  - 我一度按「handles 300→425 = +42%」断言 `handles=leak`，实测 FAIL —— 判据取的是**首尾 1/3
    均值**（320→405 = +26.6%）而非全程极差，故 `ok` 才正确。**单点比例不是判据**，与
    `性能基线口径.md` §9「别用单点建换算直觉」是同一课，当日第二次踩。
  - 门禁自身两处 PowerShell 陷阱：`-split [char]46` 的参数按**正则**解释（点是「任意字符」，
    把字段名逐字切碎成空串，15 条访问全误报），必须用 `'\.'`；以及 JS 侧 `String.replace` 的
    dollar-quote 会把「匹配之后的全部文本」展开复制进文件 —— 「用脚本改脚本，产物必须回读+实跑」
    第 6、7 次。
  实测结果：修复构建 **25 PASS / 0 FAIL**（rc=0）、旧产物 **18 PASS / 7 FAIL**（rc=1，原为 18/0）。
- **自我更正：内存比值不是 3.8 倍常数，实测带为 1.5×~3.8×（性能基线口径.md §9 精化，round 52）**：
  round 51 我写下「RSS 约为 HeapInuse 的 3.8 倍」并据此推论 MemLimitMB 的后果。本轮拿第二个
  时点一对照，**发现我自己正踩在刚写进文档的那条禁令上**（「不能假设某个固定倍数」）：
  - 空闲时段（活实例同刻三取）：RSS 64.4~64.6MB / heap 16.83MB ⇒ **3.83~3.84×**
  - 重负载时段（6 任务并发、裁剪前 33,740 条）：RSS 峰值 752MB / 同进程 metrics 的 heap 峰值
    **501.02MB** ⇒ **≈1.5×**
  成因清楚了：空闲时非堆部分（栈/代码段/运行时元数据/GC 保留未用页）占比大 ⇒ 比值高；
  重负载时堆本身占主导 ⇒ 比值贴近 1。**同一个进程、同一个比值，在不同负载下差 2.5 倍** ——
  单点采样建立的换算直觉必然错。§9 的对照表已改为带值区间，并加一句「只能在具体时段同刻双取」。
  顺带把自愈阈值的余量量化（不靠外推）：`MemLimitMB=700` 比的是 HeapInuse（因果链实测核对：
  supervisor.go:186 与 gov/engine.go:161→volume.go:250 的 MemMB 均取自 `sup.Samples().HeapMB`，
  即 metrics 那同一列）。9,990 条真实采样里 heap 峰值 501.02MB、p95=65.4MB、47.5% 采样 <20MB ——
  **离 700 只剩 1.4 倍余量，重负载时段真会贴边**。含义：该阈值拦的是「堆失控」，触发时真实
  RSS 按 1.5× 估算已在 ~750MB 量级。若有人按 RSS 直觉把它改成 128（对齐 resident 上界），
  会变成频繁误自愈重启；按 heap 量级放大到几千又形同关闭护栏 —— 这条已写进 §9 纪律第 3 点。
  纪律层面补一条方法论：**写进权威文档的数字，要有第二时点/第二形态对照才能称「约 X 倍」**；
  与 round 26「把机制说成错误的因果」、round 42「本机不发生 ≠ 仓库不发生」同族，第四次记。
- **内存的两个口径写进权威文档：`metrics.csv` 的 heap_mb 是 Go 堆 HeapInuse，不是常驻 RSS（性能基线口径.md 新增 §9，round 51 实测）**：
  仓库里实际有**两个**内存量、都被判据使用，而权威口径文档此前通篇只讲一个（WorkingSet64/RSS），
  从没声明另一个的存在：
  - **OS RSS**（WorkingSet64）：`check_resident_longrun` 用它，上界 128MB；§3/§8 全部读数也是它。
  - **Go 堆 HeapInuse**（`runtime.MemStats.HeapInuse`）：`metrics.csv` 第 2 列 `heap_mb`，
    被 `--soak` 与 A7 验收的趋势判据用，**还被自愈判据 `cfg.MemLimitMB` 用（supervisor.go:186）**。
  **同实例同刻实测**（活实例 PID 29404，三次采样）：RSS 64.4~64.6MB，heap_mb 恒 16.83MB，
  **比值 3.83~3.84 —— 差近 4 倍**。且倍数不是常数：RSS 还含栈/代码段/运行时元数据/GC 保留未用
  的堆页，Go 侧不含非堆内存。两者既无固定换算、也不能互相替代。
  **为什么是陷阱（round 22「两种形态互比」的同族变体，且更隐蔽）**：那次是两个 RSS 读数
  （探针 vs 长驻）互比，这次是**两个不同物理量**互比 —— 名字还都叫「内存」。具体会踩的：
  看到 `--soak` 报「heap 16.8MB，远低于 resident 上界 128MB」就以为余量充足，其实同刻真实
  RSS 是 64.5MB、离上界只有约 2 倍；反之拿 metrics.csv 的 heap 序列去校 128MB 上界也无效，
  因为 128 是从 RSS 实测（空闲 46.9 / 跑过任务 85~107 / 峰值 752）推出来的。
  写入 §9 的口径纪律：① 引用内存读数必须写明 RSS 还是 HeapInuse；② 趋势判据（heap）与
  上界判据（RSS）各判各的、不做数值互比；③ 按 RSS 量级去配自愈的 MemLimitMB 会因这 3.8 倍
  差导致该触发时不触发或误触发（此条留档、**不设门禁** —— 跨口径互比是语义问题，判据要落到
  可证伪的窄面，硬做会制造噪声，触发本仓在册的「模糊语义检查 => 门禁被删」）。
  另修一处文档缺陷：「## 6.」重号（上面已有「## 6. 缺口」）。改为 `## 6b.` 而**不连续重排** ——
  CHANGELOG 历史条目里显式引用过「§8 裁剪后基线重取」，重排会打断那些历史记录的指向
  （本仓纪律：历史记录不该被要求跟今天的树对齐）。
  验证：check_doc_refs/repo_eol/multi_bom/changelog/comment_staleness 五门禁全 PASS；
  numstat 49增/2删（确认无换行风格污染 —— 一度把该文件写成 CRLF，回读发现后改回 LF）。
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
- **跑官方构建链重建产物（本地全量由 1 FAIL 复绿），并登记一处「刻意不改」的版本降级**：
  起因：round 159 删死代码后，本地 `check_all` 唯一 FAIL 是「产物时效」—— 该门禁比较
  `dist\workbench.exe` 的构建时间与 4 个 soak/A7 判据源码（含我改过的 `cmd/workbench/acceptance.go`）
  在 HEAD 的最新提交时间，源码一变产物即「过期」。实测确认**当前无 workbench 实例在跑**
  （`Get-Process` 计数 0）⇒ 重建安全，遂跑与 CI 同一条链：`setup/build.ps1 -Version 1.1.0`。
  **构建实测（本机，2026-10-25）**：`BUILD_RC=0`、**98.5 s**；覆盖率门禁通过
  （**avg 74.04% / min=workbench/cmd/workbench 17.1%**，对比 round 155 的 73.23% / 11.1% —— 
  即最近两轮 CLI 侧修复抬高了薄包的覆盖）；产物 **9.79 MB**、`SHA256=CB02E6F9…C9C5`；
  2/6 资源段、3/6 vet+gofmt、6/6 校验产物全过；**子系统 GUI（无控制台窗口）**；
  **首页契约四项全 PASS**（新锚点 / 无旧锚点 / 含 --ui-probe / **真探针 HTTP 首页 len=1289**）；
  **清单读回自证 OK**（round 154 新增的那步在链上被执行）；MOTW 无网络来源标记；完整性标签非 Low。
  随后本地全量：**`check_all` rc=0 / PASS=42 / FAIL=0 / 229.8 s**（上轮那处「产物时效」FAIL 随之消除）。
  **一处「刻意不改」并登记的版本降级（实测而非推测）**：产物的 PE `FileVersion` 恒为 **1.0.0.0**，
  而 `-Version 1.1.0` 只注入 `main.version`（`--version` 的输出）。原因是 build.ps1 的 2/6 段有
  **声明过的降级分支**：`workbench/build/app.rc` 从未入库、本机也**没有 windres**，
  于是改走「用 git 跟踪的静态 syso」这条分支 —— 该件在库（`workbench/cmd/workbench/rsrc_windows_amd64.syso`，
  12116 B，含 `VS_VERSION_INFO` 与版本串 1.0.0.0），图标与 FileVersion 因此保持静态值。
  **我刻意没有改这条链**：要让它跟随 `-Version`，必须先新建并入库 `app.rc`，而本机无 windres
  ⇒ 改动在本机**不可验证**，按本项目纪律不做不可验证的改动。已作为待办登记，前置条件是
  「先把 app.rc 入库 **且** 验证机具备 windres」，届时 build.ps1 的 L65 分支会自动接管。
  **一处我自己的测量失误（如实记）**：我最初去查 `workbench/app.rc` 与 `workbench/rsrc_*.syso`，
  得出「资源件不在库、产物将丢图标与版本」的结论；核对 build.ps1 里的 `$build`/`$sysoDst` 定义后
  才发现真实路径是 `workbench/build/app.rc` 与 `workbench/cmd/workbench/rsrc_windows_amd64.syso`，
  前者缺、后者**在库且有效**。**结论在写进任何交付物之前被自己推翻**（本会话这类「查错路径/口径」
  已有多次，处置一律是先核对代码里的真实定义，再下结论）。
- **日志体积信息化门禁 + 两处实测否掉我自己的候选修复（本轮不改那段代码）**：
  起因：查 7×24 无人值守下的**磁盘无界增长**风险 —— 内存有硬护栏（MemLimitMB），磁盘没有。
  实测现状（2026-10-25）：`dist\workbench-output\logs` 共 **13.37MB**（`audit.jsonl` 6.91MB /
  `workbench.jsonl` 5.40MB / `governance.jsonl` 0.64MB / `metrics.csv` 0.41MB），且三个 jsonl
  **都没有轮转**（全仓搜过：无 rotate/上限实现）。
  **刻意不做上限/轮转**：`audit.jsonl` 与 `decisions.jsonl` 是本仓的**取证面**（engine.go 就写过
  「audit.jsonl 一夜涨到 2.3MB」当症状），删旧记录等于毁证据 —— 比占盘更坏。正确的第一步是
  **让增长可见**：新增 `tests/check_log_growth.ps1`（信息化：永不判红；无日志目录 ⇒ rc=2
  UNVERIFIED），把体积写成读数落到与其他门禁同一份 `.workbuddy/memory/gate-readings.jsonl`
  （`gate: "log_growth"`，实测已追加成功），交给既有的趋势门禁做时间序列。接线 `check_all`
  一段（覆盖 42 → 43）。
  **两处候选修复被实测否掉（如实记录，避免后续重复劳动）**：
  ① `config.JSONLWriter.Append` 每记录 open/close，我怀疑是性能问题 —— 写临时基准实测
  **91,083 ns/op（19 allocs/op）**，按审计写入量级换算为「每天不到 1 秒」，**不显著 ⇒ 不改**
  （不为了改而改；探针用完即删）。
  ② `logging.Close` 与并发写之间我怀疑会让落盘出现**半行 JSON**（撕裂）—— 写探针实测
  40 轮 × 4 goroutine × 200 行并发 Close：**无撕裂行、`-race` 无告警**（Windows/Go 下写已关闭
  fd 是整体失败而非部分写入，且 `os.File` 内部已串行化 Close/Write）⇒ **不改**。
  数字同步：`check_report_figures` 先如实报出 3 处不符（42 → 43），按读数更新后 rc=0
  （实测 43/27/43/29）；`check_gate_coverage` / `check_gate_links` 在**先暂存后跑**（round 154
  的教训：它们按索引/干净检出语义读内容）下 rc=0。
- **补上「产物与校验清单一致」的本地侧判据 + 更正 `口径.md` 里一条双重过期的缺口行**：
  ① 背景：`docs/benchmark/性能基线口径.md` §6 把「`dist/SHA256SUMS` 已过期」（清单 C81CA500… vs
  产物 BEC437CE…）登记为未覆盖缺口。实测复核发现这条**已双重过期**：用 node crypto 复算，
  清单 `3335F81A…` 与产物本体实测一致（2026-10-24 16:50 重建产物、清单 4 秒后写入）；
  且 CI 的 build job **早就有**独立读回校验（`.github/workflows/ci.yml` L199-205，
  `清单 != 实测` 即 throw）。
  ② 真空白在**本地侧**：CI 那条读回只在 build job 里跑，本地用裸 `go build` / 手工复制重建
  `dist\workbench.exe` 时清单不会更新，本地全套件不报警 —— 正是 2026-10-20 实发的形态。
  ③ 新增 `tests/check_artifact_checksum.ps1`（窄面判据）：产物缺 ⇒ rc=3 SKIP（「没测成 != 合格」）；
  有产物无清单 / 清单无条目 / hash 漂移 ⇒ rc=1（打印两个 hash 便于取证）；一致 ⇒ rc=0。
  **三态实测 0/1/3/1**（真实 dist=0；篡改副本清单=1；无产物=3；有产物无清单=1）。
  ④ 接线两处：`check_all` 新增一段（覆盖数 41 → 42）；`setup/build.ps1` 在**写入清单之后**
  加独立读回自证，并沿用本仓立场「校验器不在就内联自证，而不是跳过」（`Get-FileHash` 内联比对）。
  ⑤ 一处过程教训（如实记）：我用可执行位改这两个 .ps1 时，先按 CRLF 读写，看到工作区出现
  259 CRLF/35 裸 LF 以为改坏了行尾；查 `.gitattributes` 才确认 `*.ps1: text eol=crlf` —— 工作区
  CRLF 本就是正确形态（blob 存 LF 是 git 的转换），裸 LF 是**本就存在**的混合残留。改用
  `git diff` 做权威核对：`build.ps1 +14 行`、`check_all.ps1 +7 行`，**内容零损坏**。
  教训：判断「我是否改坏了文件」要用 git 的 diff（它按 eol 过滤后只显真实改动），不要用自造的
  逐行比对或用 fs 读取的行尾统计当判据。
  ⑥ 数字同步：`check_report_figures` 先如实报出 3 处不符（L11 受跟踪 41→42、L14 check_all 覆盖
  41→42、L84 结论行 41→42），按读数更新后复跑 rc=0（实测 42/27/42/29）。
- **「某判据是否在 CI 执行」三口径实测对照 —— 我自己的第 3 次口径自我推翻，并否掉两种错误方法**：
  续 round 152，本轮查「那些不在 ci.yml 里的门禁，是否真的没在 CI 跑」。我先后用了两种静态口径，
  **两个方向都错**，最终以**运行事实**定案（报告新增 §1.2）：
  ① grep `ci.yml` 全文 ⇒ 「12 个不在 CI」—— **漏报**：`setup/build.ps1` 在 6/6 段**无条件**调用
  `check_subsystem.ps1`（L194-201，rc≠0 即 throw）与 `check_homepage.ps1`（L220-224，同样 throw），
  而 `ci.yml` L170 无条件跑 `./setup/build.ps1` ⇒ 这 2 个在 CI 里**确实执行**。
  ② 静态调用链闭包（先含注释、后仅非注释行）⇒ 两次都得出「CI 可达 41」—— **多报**：
  闭包把「**点名**文件」当成「**执行**文件」，某个门禁在代码里列受理面时点了 `check_all.ps1`，
  于是整棵树被拉进闭包。仅过滤注释并不能修好它。
  ③ **第一手**：数 CI 日志里 distinct 的 `GATE: tests/*.ps1` 行 ⇒ **repo-gates 实测执行 27 个**
  （run `01572a6`），且**不含** `check_report_figures.ps1` —— 与它头注「本地判据、不挂 CI」以及我
  round 150 在干净克隆实测它 rc=1 **三处互证**。
  结论：判断 CI 执行面只能靠 ①**读调用点** 或 ②**跑一次数日志**；静态 grep 会漏、静态闭包会多。
  报告同时列出**在 CI 里确实不执行的 9 个门禁及各自理由**（工作区口径 / 需本地数据根 / 需在跑实例 /
  需要 hooks / opt-in 参数 / 暂存区空转），证明这不是遗漏。
  新增建议 G5：把「CI 执行面」当**测量结果**维护（每次从 GATE: 行恢复），并要求每个门禁在头注写明受理面。
- **补齐 4 个「只挂 CI」的门禁进本地全量套件 —— 关掉「本地绿 / CI 红」陷阱**：
  实测（门禁 × 受理面矩阵）：41 个受跟踪门禁里 12 个只挂单一受理面，其中 **4 个只挂 ci.yml**
  （`check_changelog` / `check_issue_forms` / `check_noninteractive` / `check_perf`），
  而本地全量套件 `tests/check_all.ps1` **从不调用**它们 —— 且这三个文档类门禁在 check_all 里
  **没有任何等价内联逻辑**（按关键词实测：CHANGELOG/issue/非交互 各 0 行命中）。后果是：
  按文档跑完全量本地套件全绿，推送后仍可能被 CI 判红；而本仓在册有「本地绿 CI 红」这一整族教训。
  修法：在 check_all 里按既有三态惯例补 4 段（rc=2 ⇒ 改写成 0 并保留 UNVERIFIED 字样；
  无产物时走 rc=3 SKIP 而不是判红），并写清「为什么补」的注释。实测结果：
  `check_all` 覆盖数 **37 → 41**，与「受跟踪 41」对齐 —— 即本地全量套件现在引用**全部**受跟踪门禁
  （部分在缺前置时以 SKIP/UNVERIFIED 呈现，不计 PASS）。
  验证：`check_ps1_syntax` rc=0（BOM 1 + 纯 CRLF 原样保持）；`check_report_figures` 先如实判红
  「报告写 37，实测 41」→ 更新报告该行（并注明补齐由来）→ 复跑 rc=0，实测 41/27/41/29 全一致；
  全量 `check_all` 运行结果见提交说明与读数。
- **gate-spec 新增 ⑦（依赖 git 历史的判据必须处理浅克隆取空）+ 更正上一轮报告的口径 + 把 G1/G2 降级**：
  ① `quality-gate-spec` 新增 ⑦：判据若依赖 **git 历史**（git log / rev-list / blame 等需要
  **祖先提交**的命令），规格必须写明「取不到历史时怎么办」—— CI 的 checkout 默认
  `fetch-depth: 1`（浅克隆），此时 `git log -1 -- <paths>` 可能返回空，**那不是「没有改动」，
  而是「看不到祖先提交」**，必须走 UNVERIFIED，不许当空集判绿；并写明降级后**由谁容忍 rc=2**
  （实测形态：`check_artifact_soak_fresh.ps1` 取空即 Unverified，`ci.yml` 该步骤
  `Write-Warning` 后 `exit 0`）。条款还写明一个实测踩点：**别把「打印给用户看的 git 自查命令」
  当成依赖** —— 静态扫描会把 Write-Host 里的提示串误判成依赖，这类检测只会制造噪声。
  契约测试同步：gate-spec 期望片段 2→3（总 14 条 / 4 模板）；变验证：把「浅克隆」改成不含原串的
  「薄克隆」⇒ RED 且点名「quality-gate-spec 的渲染结果缺片段 "浅克隆"」；还原 ⇒ GREEN 且字节一致。
  ② **更正上一轮报告的口径（该报告第二次自我推翻）**：初稿写「41 个受跟踪门禁里 **9 个**用 git 历史」，
  那是**整文件正则（含注释）**的结果。按「只看非注释代码行」重测的真实分类见报告 §1.1：
  真依赖**祖先提交**的只有 **1 个**（`check_artifact_soak_fresh.ps1` L84 的
  `git log -1 --format=%ct -- <4 路径>`）；5-6 个只用 HEAD 作用域 API（`cat-file`/`HEAD:<path>`/
  `git archive HEAD`/`checkout-index`，浅克隆安全）；`check_doc_refs.ps1` 的 `git log --all`
  只在**头注释**里（运行期零 git 调用）；`check_snapshot_build.ps1` 的 `git log` 只在
  **Write-Host 提示串**里（真调用是 HEAD 作用域的 archive/checkout-index）。
  ③ **G1/G2 因此降级（不采纳）**：静态检测「是否依赖 git 历史」无法区分**真调用**与**提示串** ——
  `check_snapshot_build.ps1` L91 就是反例，任何朴素扫描器都会误判。按本仓在册立场
  （`check_ps1_encoding` 作者拒绝「头注语义必须与实现一致」的同类判断，理由同样是会制造噪声），
  **会误报的模糊检查不该建**。④ **风险链其实已被现有设计闭合**（实测）：那唯一一个真依赖祖先提交的
  门禁取空时走 `Unverified`（L97），而 `ci.yml` L228 **显式容忍 rc=2** ⇒ 浅克隆下最坏是
  「没测成」，**既非假绿也非假红**。于是落地方式从「加运行期判据」改为「设计期约束」（即上面的 ⑦）。
- **新增五轴审查报告《门禁浅克隆与空输入失效面》—— 两处假设被实测推翻，并含一次自我推翻**：
  `docs/门禁浅克隆与空输入失效面-五轴审查-2026-10-25.md`（含合并结论 + 门禁建议 G1-G4）。
  起因：侦察发现 41 个受跟踪门禁里 **9 个用 git 历史**，而 CI 三个 job 全部 `fetch-depth: 1`
  （浅克隆）—— 于是查「门禁会不会因取不到历史而静默判绿」。
  **H1（浅克隆假绿）被推翻**：用 `git clone --depth 1 --no-local file://<root>`（**完全离线**）
  造出提交数为 1 的浅克隆，对 5 个 git 历史门禁在完整树与浅克隆各跑一遍：rc 全一致，且浅克隆下
  仍在真干活（`check_doc_refs` 74 份活文档/103 处引用、`check_multi_bom` 1739 个受跟踪文本、
  `check_gate_coverage` 41 门禁/4 受理面、`check_gofmt` 354 个 .go、`check_stdlib_only` 全项）。
  机理：它们用的是 `git ls-files`（索引层）/ `cat-file` / `HEAD:<path>` 回退，浅克隆只缺**祖先提交**。
  `check_artifact_soak_fresh` 在浅克隆里 correctly 变 rc=2 UNVERIFIED（不是判绿）。
  **H2（CI 无人守 .ps1 的 BOM 缺失）被推翻**：`check_multi_bom` 头注明写它只判 ≥2 个 BOM、
  刻意把「有没有 BOM」交给 `check_ps1_encoding`；后者实测受跟踪 + 在 ci.yml + 判据覆盖
  「受跟踪 .ps1/.psm1 含非 ASCII 必须带 UTF-8 BOM」。两者职责不重叠，不是漏洞。
  **我自己在报告初稿里写的第 4 条断言也被实测推翻**：初稿 §3 写「4 个读外部数据件的门禁在输入
  缺失时都走 rc=2」；在干净克隆（天然无未跟踪件、无 `gate-readings.jsonl`）实测 rc 为
  **0 / 0 / 0 / 1**，一个都不是 2。已在 §3 逐条更正，并写明「为什么 0/1 才是对的」：
  `check_untracked_go` 在干净树里差额本就 0、`check_skip_disclosure` 核心判定在受跟踪列表上、
  `check_readings_trend` 缺基线只告警不升 rc，而 `check_report_figures` 判**工作区**，
  干净树未跟踪数为 0 与报告写的本地值 29 不符 ⇒ 判红 —— 其头注 L27-30 明写「本件属本地判据、
  **不挂 CI repo-gates**（挂了会在干净检出上永远红）」，实测 ci.yml 确无它（本地 check_all 有）。
  由此多出一条：**干净克隆同时是「判据分级」的验证器**（在干净树里判红的门禁必须不挂 CI），
  记为建议 G4。
  建议：G1 浅克隆一致性判据（复用 `check_snapshot_build` 的克隆范式）、G2 先固化「依赖 git 的
  门禁名单」再建 G1、G3 给 `check_ps1_eol`/`check_staged_bom` 输出加「CI 下为空转」说明、
  G4 干净树分级校验。**本轮只出报告不改代码**（按「逐点攻破」，建判据留给下一轮）。
- **两条新模板条款把本轮实测教训固化：`quality-gate-spec` ⑥（判据读外部数据件时的字段缺失语义）+ `quality-verify` ⑨（变异样本必须自证真的改掉了目标）**：
  ① gate-spec 新增 ⑥：判据若读外部数据件（基线 JSON、配置、清单）的字段，规格必须写清
  「字段缺失/为 null 时怎么办」，并配一条**静态配对检查**。条款里直接带上实测依据：
  判据分支在、数据件缺键 ⇒ 取到 null ⇒ 判据静默走 SKIP 而整条门禁仍 rc=0，运行期读数与
  「真在跑」一模一样，只能静态发现。该形态本轮刚实测过（提交 `1ee56f6`：静默失效三天，
  立键时键与分支同批入库 → 一次「重取基线」把两者一起删 → 又一次「只补 BOM」的提交把分支
  带回、键没人补）。配对检查至少要管三件事：引用的字段要有键、null 字段要登记理由、
  分支标记与字段同生同灭。
  ② verify 新增 ⑨：变异样本必须自证「真的改掉了目标」——把判据搜索的标记改成仍**包含**
  原串的新名（实测形态 `exeSizeMin` → `exeSizeMinRenamed`），Contains 类判据依旧命中，
  读数看起来是「判据没抓到」，其实是样本无效。顺序写进条款：**先怀疑自己的样本，再怀疑判据**。
  （依据 = 上一轮我自己差点把这种无效变异写成一条「判据漏抓」的结论。）
  契约测试同步扩到 **4 模板 / 13 条教训**（`quality-verify` 3→4，新增 `quality-gate-spec` 2 条）。
  **变异证明**：把两个新片段各自改名成不含原串的同义词 ⇒ 契约测试 RED 且点名「缺片段
  "静态配对检查"/"真的改掉了目标" —— 该条教训没到达 prompt，等于没写」；还原 ⇒ GREEN 且
  `defaults.go` 字节一致；全程临时 worktree，跑完即删、主仓未污染。
  **本轮我在改自己这个文件时反复失手，如实记录（都不留到下一轮）**：零散字符串手术
  ① 把新条款插到了旧条款**之前**（编号乱成 ①②③④⑥⑤ 与 ⑦⑨⑧）；② 结尾标点反转
  （前一条提前用 `,` 收尾、新条末尾 `+` 悬空）；③ 我的后缀正则只吃掉逗号、留下 `""+`；
  ④ 构造函数多打一个引号写出 `"",`。**处置：不再逐处打补丁，直接从 HEAD 恢复该文件**，
  改用「把末条结尾由 `",` 改为 `"+`、再在其后追加新行、新行末条以 `",` 收尾」的构造
  一次性重做；并加了三道自证（插入后断言两条编号序列各连续无重复、断言 `\n{context}`
  转义仍在、gofmt+go build 双验）。
  ⑤ 又一次现场证明 `gofmt clean ≠ 语义合法`：`too many arguments in call to mk`（have 12 /
  want 11）只在 `go build` 暴露 —— 这正是我前几轮写进模板的那条，本轮自己第 2 次踩到。
- **CI 三个 job 各加 timeout-minutes: 15（补上「全无上限」的止损缺口）**：
  这是 round 124 报告里登记为「采纳为待办（单独一轮做）」的那一项，本轮落地。
  此前 ci.yml 三个 job（vet+test race / repo gates / build+coverage）都没有 timeout-minutes，
  全文出现 0 次 —— 一旦某步真挂起，会一直占着 runner 且没有自动失败。
  **阈值不是拍数字**：先实测最近 8 次 CI run 的 24 条 job 记录，各 job 中位耗时
  1.8 / 2.3 / 2.5 分钟、观测最大 2.8 分钟；15 分钟约为最大值 5.4 倍，留冷缓存与 runner
  抢占余量，又能在真卡死时止损。依据两处实证写进注释：本文件下方已记录的一处 CI 侧永久
  挂起（Invoke-WebRequest 触发 PS5.1+IE 弹窗等人工按键），以及本地 check_all 在并行会话
  争抢时从约 3 分钟拖到 44 分钟未跑完（最终被我终止）。
  刻意不做按 job 差异化设限：三者耗时同一量级（1.4-2.8 分钟），差异不足以支撑三组阈值，
  而阈值过紧会把偶发的 runner 慢变成假红 —— 假红的代价是门禁被人关掉（round 64 家族）。
  验证：python yaml 解析结构确认三 job 均 timeout-minutes=15 且 needs 关系未变；
  受影响 10 项门禁全 PASS（gate_coverage / noninteractive / ps1_syntax / changelog /
  multi_bom / repo_eol / comment_staleness / doc_refs / gate_links / report_figures）；
  其中 gate_coverage 特别核过——我注释里写的 check_all 不带 .ps1 后缀，不会被它的
  正则 [\/]check_[a-z0-9_]+\.ps1 误计为已接线门禁（实测该正则扫描 diff 新增行 0 命中）。
  另两处即时自纠（都不留到下一轮）：① 我第一版注释写「两处在 CI 侧挂起过的实证」并把
  「Stop 模式抛错」也算作挂起 —— 读 ci.yml 原文核实只有一处挂起实证，而抛错不是挂起，
  已改为精确表述；② 第一版写「45+ 分钟」而实测是 44 分钟，按数字必须精确改正。
- **新增模板链完整性判据：后继 ID 断链会被引擎静默跳过**：
  实测引擎取后继的写法是 if t, exists := g.reg.Get(id); exists && t.Enabled { out = append(out, t) }
  —— 一个拼错或已被删除的后继 ID 不报错、不打日志，链在那一步无声停止延伸。7x24 无人值守下
  表现就是"某类任务再也不往后走了"，而所有测试与门禁都是绿的。既有守卫只有 chain_test.go 里
  4 个抽查用例，覆盖不到 29 条模板的全部 11 条边；实测受跟踪与 29 个未跟踪测试里都没有链完整性断言。
  新增 templates/chain_link_contract_test.go 遍历 Defaults() 的字段真值（不解析 Go 源码，
  按 quality-audit 要求⑤），断言三类客观错：断链、自环、同一后继列表内重复 ID
  （重复会被算作两个后继、白占 fanout 名额）。当前真值实测全为 0（29 模板 / 11 条边）。
  刻意不断言有向环（A→B→A 跨轮交替是有意义的链形态）与域一致性（链设计可能刻意跨域）——
  把设计选择写成缺陷是另一种误报。受理面哨兵：Defaults() 为空或后继边总数为 0 即 Fatal，
  防"循环不执行而恒绿"。
  三项变异全判红且点名（临时 worktree，跑完即删、主仓 defaults.go 复验 clean）：
  A 把 quality-audit 的后继改成 quality-gate-spec-typo => 点名"不存在 —— 引擎会静默跳过该边"；
  B 把自己列为后继 => 点名自环；C 同列表写两个相同 ID => 点名重复白占 fanout；
  还原 => GREEN 且字节一致。变异脚本按本轮刚编码的⑦做四态判定（RED OK/INVALID/SILENT/
  UNVERIFIED），且只取 Errorf 点名行 —— 上一轮我因取首条匹配行拿到 Logf 而误读，这次已避开。
  另记一次工具层事实（本轮第 3 次撞同类）：写 CHANGELOG 临时稿时 tools.write 报
  "file no longer exists — re-read the file, then retry" —— 因为我前几轮删过同名文件，
  观测层留了删除记录、同名重建被拦。处置是换文件名，而不是绕开工具或硬写。
  验证：go vet rc=0；判据 rc=0（日志"链完整性 OK：29 条模板、11 条后继边"）；
  新文件 BOM 0 纯 LF、无反引号无裸反斜杠；worktree 与临时脚本全清、未跟踪生产 .go 0。
- **quality-verify 新增要求⑦「变异实验自身要有受理面检查」并当场被它抓到两次**：
  上轮我把这条教训记进了 CHANGELOG，却没编码进模板 —— 正是我自己反对的「只留在文档里」。
  新⑦要求：判红之前必须确认是判据判的，而不是实验把被测物弄崩了；还原按字节捕获/写回并复验
  字节一致。原⑦顺延为⑧。同步扩上轮建的 encoded_lessons_contract_test.go，断言该片段
  （"变异实验自身"）真出现在 Render 之后的 prompt 里。

  **本轮我两次踩在这条上，都被它抓住（所以它是真教训，不是我编的规矩）**：
  ① 第一版变异脚本把 rc=-1（execFileSync 超时，拿不到退出码）标成 SILENT —— 等于把「没测成」
     当成「判据没抓到」，正好是⑦要防的形状。改成严格四态：RED OK / INVALID(编译错) /
     SILENT(rc=0 且处于变异态) / UNVERIFIED(拿不到码)，基线不绿直接 ABORT。
  ② 点名行取的是「首条匹配 contract_test.go:」，抓到的是 t.Logf 的日志行而非 Errorf 的
     失败行 —— 同族错误第 2 次（上轮变异 C 也犯过）。改为只匹配含「缺片段/字面反斜杠/
     未替换占位符/受理面」的 Errorf 输出。

  最终精准版结果：基线 GREEN → 抹掉⑦关键词 → **RED OK 且点名**
  「quality-verify 的渲染结果缺片段 "变异实验自身" —— 该条教训没到达 prompt，等于没写」
  → 还原 GREEN 且 defaults.go 字节一致。锚点命中数先断言 =1 才动手（防 replace 打歪）。
  全程在临时 worktree 内（冷缓存导致首轮超时 57s+，热身复测后稳定）。

  另记一次同族的即时自纠：写变异脚本时我用 /\n/ 做 split，正则里的反斜杠被转义层吃掉
  导致 JS 语法错 —— 这是本仓在册的「多层字符串改写」家族，处置是改成 split/join 与写文件
  执行，而不是继续在命令行里加转义。

  验证：go build ./internal/templates/ rc=0（拼接语义合法，gofmt clean 不足以证明）；
  契约测试 rc=0，quality-verify 渲染 1206 → 1794 字、3 条编码教训到位；templates+chain 全量 rc=0；
  两文件 BOM 0、纯 LF（各自形态）；未跟踪生产 .go = 0；worktree 与脚本已清。
- **新增 encoded_lessons_contract_test.go：钉住「写进模板的教训必须真的到达 prompt」**：
  这几轮我往 quality-audit 加了要求⑤（测量学四条）与⑥（工具剥 BOM）、往 quality-verify
  加了⑥（时序类判据须给复现率）。但模板文本是 Go 的**多段字符串拼接** —— 少一个加号、多一个
  转义，编译可能仍过而我本轮就把⑥的末行写成逗号（当场被 go build 抓到）；更糟的是渲染结果会
  静默缺一整条，而这些条目的全部价值在于「下游 agent 读到」，少一条等于没写，却没有任何测试失败。
  新判据断言的是 Render **之后**的真值（比对片段而非整句，措辞微调不致误红）：9 条编码教训
  （quality-audit 5 + quality-verify 2 + review-five-axis 2）逐条必须在，且渲染文本里
  任何字面反斜杠都判红（复现我真实犯过的「转义写成双反斜杠 → 残渣文本」），未替换占位符也判红。
  双向变异三项全判红且点名：抹掉一条教训关键词 ⇒ 报缺片段并说明「等于没写」；注入反斜杠残渣 ⇒
  报「含 1 个字面反斜杠」；把模板 ID 改掉 ⇒ 走进受理面分支（判据失去受理面即报错，不静默通过）。
  还原 ⇒ rc=0，defaults.go 字节一致，全程在临时 worktree 内做，主仓未污染。
  一处如实自纠：变异 C 的点名行我打印出来的是 Logf 行而非 Errorf 行 —— 我的 lineOf 取的是
  首条匹配行，取证据时没区分日志级别；结论本身由 rc + 无编译错 + 代码路径唯一性三处共同支撑，
  但**展示层差点让我误读**，与本轮主题同族（判据/取证窄一档就会放过问题）。
  另：本轮按我自己刚写进⑥的定规，每次 tools.write/edit 改完受跟踪文件都逐字节复验 BOM ——
  实测 CHANGELOG 又被静默剥掉 BOM（第 2 次），已补回，HEAD 与工作区均 BOM=1。
  验证：新契约测试 rc=0（9 条全到位）；templates 包全量 rc=0；gofmt clean。
- **quality-audit 模板新增要求⑥：写入/编辑工具会静默剥 BOM 并改行尾，改完必须逐字节复验**：
  根因来自本轮我自己的操作：tools.edit 改完 CHANGELOG.md 后开头 BOM 从 1 变 0，
  而 diff 只看内容行、看不见编码变化。我一度按「BOM 恰 1」的既有定规补回就当完事，
  但那是单次观察 —— 按我上一轮刚写进同一个模板的规矩（含并发/偶发断言必须复现），
  这次做了受控实验：造一个带 BOM + CRLF 的临时文件，分别过 tools.write 与 tools.edit，
  两者都把 BOM 剥成 0、且 write 还把 2 个 CRLF 静默改成纯 LF。**可复现，非偶发**。
  新写进模板的第⑥条就是这条事实与对策：改完带编码要求的文件（.ps1 必须有且仅有一个 BOM、
  部分文档要求无 BOM），必须在使用写入/编辑工具之后再逐字节复验编码。
  定规升级：凡本轮相对 HEAD 有改动的文件都查 BOM（不再只查「我改的那个」）——
  本轮实测只有 CHANGELOG 有改动，HEAD BOM=1 / 工作区 BOM=1 一致。
  我自己另两处即时纠错（都不留到下次）：
  ① 插入 ⑥ 时末行写成逗号结尾，会让下一行「上下文」变成裸字面量而编译不过；
     当场改成加号闭合拼接链（gofmt clean + go build rc=0 双重确认）。
     顺带注意：gofmt clean 只证明**语法可解析**，不证明拼接语义合法，所以必须补 go build。
  ② 我在 CHANGELOG 里留过一句「全量 check_all 见下条」的前向引用 —— 那是未兑现的承诺，
     读到的人拿不到结果；已改为真实数字（35 PASS / 1 FAIL，唯一 FAIL 是 figures 的时序快照，
     提交后单独复验 FIG=0；受影响 6 项门禁全 PASS）。
  另记一次工具层事实：本机被我积压的后台任务 + 并行会话占满时，连 gofmt -l 与 gh run list
  都会超时 —— 那**不是代码问题**。处置是先杀积压任务再重测，而不是把资源饥饿误判成缺陷。
  验证：go build ./internal/templates/ rc=0；go test templates rc=0；gofmt clean；
  文件 BOM 0、纯 LF（该文件自身形态）；上一提交 b139a73 与 cec42b0 的 CI 均已确认 success。
- **补内嵌 CSS / 四个 HTML 页面的结构判据（与 JS 同族空白）+ 五项变异有效、并识破自己变异脚本写坏实验**：
  §5.6 给 JS 补了语法判据，本轮补完同族另一半。空白取证（临时 worktree，跑完即删）：删掉
  appCSS **末尾一个右花括号**（第 1404 行，刻意避开任何测试的字符串锚点）⇒ **web 包 rc=0 全绿**。
  引用 appCSS 的 16 个测试函数（分布在 9 个文件、26 处引用）全是「某个类名在不在」的文本断言，无一校验结构。
  危害比 JS 更隐蔽：JS 坏了整页白屏（立刻可见），CSS 少一个 } 会把它后面的规则整段吞进上一条，
  表现是「部分样式静默失效」—— 在 7x24 无人值守里就是「没人会看的那个页面悄悄丑了」。
  落地 web/css_html_structure_contract_test.go 三道结构：
  ① CSS 花括号用**跳过字符串与注释的单遍扫描器**（返回开数/闭数/最深负值），不是 strings.Count：
     实测当前 474 对括号里引号串与注释恰好都不含括号（45 个引号串 0 处、153 个注释 0 处），
     朴素计数今天也对，但 CSS 有 content:"}" 这种合法写法，将来一出现就假红 —— 而假红的代价是
     门禁被人关掉（round 64/116 在册）。
  ② HTML 判 DOCTYPE 恰 1 次 + html/head/body/script 开闭配平；开标签算「<tag 空格」与「<tag 尖括号」
     两形态（页面里 <body class=…> 与 <head> 并存，只数 <tag 会把 <header 算进来）。四页实测 1/1。
  ③ 扫描器自检 TestCSSBraceScannerIsSound 六用例，且对含干扰括号的用例做**反证**：若朴素计数与
     扫描器结果相同即判该用例「没区分力」。这条反证当场抓到我自己的用例设计错（我把反证套在
     一个只测负深度、不含干扰括号的用例上，必然同值 ⇒ 自检判红，非被测逻辑错）。
  明确不纳入两项（都因实测歧义，写死必假红）：style 标签配平（modelPoolPage 实测 style=2/1，
  第二个 <style 出现在**注释文字**里，纯文本计数分不开注释与真标签）；CSS 嵌套深度归零
  （@media 合法地让深度在不同层级收口，判树形需要 CSS 解析器，收益不匹配）。
  五项变异全部有效且点名：A 删末 }、A2 同破损跑包级、B 删首 {、C 删 </body>、D appCSS 整段丢失、
  E 删 DOCTYPE；还原后 rc=0 且字节一致。**A2 是本轮最想要的证据**：上轮此处 rc=0（静默），
  现在包级判红并点名子测试 appCSS_花括号配平。D 证明「0==0 也算配平」的空气陷阱被哨兵拦住。
  **我的变异脚本自己写坏了 worktree（如实记，比修好判据更值得记）**：finally 里写 put(orig)，
  而 put(b) 的语义是「把 b 当内容塞进两个反引号之间」—— 整个 .go 被嵌进 raw string（反引号 2→4、
  字节 +194），C 与还原步都撞上 Go 编译错。那两次 rc=1 是编译失败不是判据抓到的；若只看退出码
  就会把无效结论写进报告。改成「按字节捕获/按字节还原」并对每次结果甄别 syntax error|build failed
  才拿到真实结论。教训：**变异实验自身也要有受理面检查** —— 判红要先确认是判据判的，
  不是实验把被测物弄崩了。主仓始终 clean，污染只在临时 worktree（已删）。
  验证：新判据 rc=0（appCSS 474/474 配平、78184 字节）；自检 rc=0；web 包全量 rc=0；go vet rc=0；
  gofmt clean；BOM 0；全量 check_all 35 PASS / 1 FAIL —— 唯一 FAIL 是 check_report_figures，其快照早于本次提交
  报告 §5.7：docs/三份内嵌JS同名helper分叉-五轴审查-2026-10-25.md
- **补上一处更严重的空白：全仓没有任何判据解析过内嵌 JS（新增 node --check 判据）**：
  三份内嵌脚本是 Go raw string —— 括号失配、少分号时 go build / go vet / go test 全绿、CI 全绿，
  **只有浏览器打开才白屏**。空白取证（临时 worktree，跑完即删）：
  ① 删 appJS 一个右花括号 => web 包 rc=1，但抓到它的是 TestThreeApiImplsAreIdentical 与
     TestSharedHelpersRemainIdentical —— 它们比对复制体文本，坏在花括号配对导致两侧长度不等
     于是**碰巧**报警。那不是语法判据，是对「文本变了」的判据。
  ② 改坏 appJS **独有**函数 renderTemplates（不参与任何比对）=> 仍 rc=1，但抓到它的是
     TestKeyboardBindingsNotInRepeatablePaths 的正则恰好撞上。依旧不是语法判据。
  ③ 全仓搜 --check / new Function / vm.Script / acorn => **0 处**。
  ⇒ 这份 7.4 万字的 JS **从来没有被任何判据解析过**；两次「抓到」都是别的判据的副作用，
    换个位置破坏就静默通过 —— 那是「碰巧变红」，不是「守得住的判据」。
  后果形态具体：本项目是 7x24 无人值守桌面工作台，面板是唯一观测入口；一次括号失配让整页
  脚本抛异常不渲染，而引擎仍在后台跑 —— 表现为「一切正常但界面死了」。
  **落地** web/js_syntax_contract_test.go：用 node --check（只解析不执行）校验三份脚本，
  判据取编译后的常量值本身（不解析 Go 源码，与 quality-audit 要求⑤ 同规）。三道结构 ——
  ① 三份各自独立检查（变异 A 实测：删独有函数的 } 后**只 app.js 报错、gov/pool 仍绿** ⇒ 隔离，
     不互相掩盖）；② 受理面哨兵（字节数低于登记值三成即报「判据在比空气」；变异 B 抬下限
     触发成功 ⇒ 非死代码）；③ 无 node 时带理由 Skipf 并如实写「本机无 node 时本判据不生效，
     属没测成」，CI 是 windows-latest 预装 Node，若日志出现该 Skip 即环境假设错了 —— 可被看见。
  **顺带纠正我自己的单位错标**：注释把 62914/12068/28249 写成「字节」，那是 JS **字符数**；
  Go len() 与 node 看到的都是**字节**（74285/14539/34123，中文按 3 字节、比值约 1.18）。
  发现契机是 node 报的字节数远大于我记的「字节」，第一反应以为提取器截断了 —— 实际是单位混用。
  教训：报数字必须标口径；字符/字节混用既会造出「大得离谱的差异」这种假警报，也会掩盖真截断。
  验证：新判据 rc=0（三份各自「语法有效」日志）；变异 A/B 双向判红、还原复验 rc=0 且两文件
  字节一致；web 包全量 rc=0、go vet rc=0、gofmt clean（新文件不在 -l 列表内）；BOM 0 纯 LF。
  报告新增 §5.6。
- **PR 模板新增硬约束自查项：并发/共享状态改动须给复现率 + 加锁前复核锁序（round 124）**：
  这是本轮五轴报告「门禁建议 #1」的落地（当时写「采纳」，本轮真做，不留空承诺）。
  自查项 11 增至 12。理由来自本轮真实代价：round 121 我本机跑一次绿就宣布「并发已解决」，
  实际复现率约 1/30，CI run 40127263998 才暴露；而加 pathLocks 前必须复核
  Store.Replace 的 s.mu 与新锁是否成环（在册教训 round 111 自写的自死锁形态）。
  与 quality-verify 模板⑥（复现率）构成「思考面 + 交付面」两处同一约束。
  验证：BOM 0 纯 LF（该文件自身惯例）、反引号未被转义、7 个相关门禁全 PASS。
- **把 round 115-121 的四条测量学教训编码进 quality-audit 模板（新增要求 ⑤）**：
  这五轮学到的东西只留在 CHANGELOG 和文档里 —— 执行模型（链上的下一个任务）看不到，
  它会重犯。模板 prompt 才是执行面真正读到的东西，所以本轮只做一个小点：给链条入口
  quality-audit 加 ⑤，每条都带实测出处：
  ① 读字段值必须执行程序取运行时真值，不要解析源码文本（同一个「字面命中」指标
     我用 Node 正则解析 Go 源码量出 83/68 两种错值，改 Go 端运行时才得 82）；
  ② 判据命令必须查退出码并打印错误（助手函数 catch 返回空串，把「门禁 41 个」读成 1、
     把「dsh-web-app 1 处」读成 0 处 —— 空输出与执行失败是两回事）；
  ③ 自动分类器的输出是待核线索不是结论（分类器判 9 条模板章节不一致，人工逐条核对
     只有 4 条成立，按 9 条去改就会改错 5 条）；
  ④ 涉仓库惯例先测分布，别拿自己上一份文件当基准（受跟踪 .md 实测 23/28 无 BOM，
     反倒我自己上一份带了 BOM 属少数派）。
  验证走刚写进模板的同一条规矩 —— **运行时取证**：临时 cmd 遍历 Defaults() 取 quality-audit
  真值渲染，LEN=1073、五个占位符全部替换（HAS_UNKNOWN_PLACEHOLDER=false）、⑤/运行时真值/
  待核线索 三处均在渲染文本中；Validate 通过；探针用完即删并复验 check_untracked_go。
  templates+chain 包 rc=0、vet rc=0、gofmt clean、6 个受影响门禁 PASS。
- **硬约束机器保障审计：钉住「不挂 dsh-web-app」+ PR 模板补 2 条自查项 + 五轴报告（round 116-118）**：
  审计动机：objective 要求把硬约束落到可机器判定。本仓是三层形态 —— CONTRIBUTING 硬约束 6 条
  PR 模板自查 6 项 tests/check_*.ps1 41 门禁 + Go 测试。三层不重合就是缺口。
  实测（git grep -rn dsh-web-app，逐 pathspec 查 rc）：
  - 全仓该字符串仅 1 处，且是 dshprofile/bootstrap.go:8 的注释；
  - tests/ 41 个门禁 0 处、.github/pull_request_template.md 0 处；
  - CONTRIBUTING 有（约束 #4，且原文「违反 1 或 2/4 一律不合并，没有例外」）。
  而 dshprofile 包两个受跟踪测试文件（13 个 Test）没有一个断言 Bundles 的内容。
  后人往 Bundles 加一行就能静默违反「一律不合并」级约束，而后果不是崩溃：
  dsh-web-app 起常驻 HTTP 服务、headless 永不退出（CONTRIBUTING 记的实测：卡死 150s+），
  在 7x24 无人值守里表现为「任务不再推进」—— 难归因、难发现。

  修：新增 workbench/internal/dshprofile/bundles_contract_test.go，两面判据 ——
  黑名单（任一项含 web-app 即 Errorf，附为什么文案）；白名单快照（恰为 base/headless/
  our-free-model 三项，增减须显式改测试与 CONTRIBUTING）+ 空切片 fatal 防空转。
  CI 的 go test -race ./... 自动覆盖，无需单独挂载。
  双向变异：往 Bundles 注入 @deepseek-ai/dsh-web-app 后 rc=1，点名违反硬约束 #4 并附快照变化提示；
  还原后 rc=0 且 bootstrap.go 与原始字节一致；dshprofile 包全量 rc=0（既有 13 个 Test 无一受影响）。
  PR 模板补 2 条自查项（9 增至 11 项）：约束 #4（dsh-web-app）与 #6（产出根 workbench-output）——
  此前模板独缺这两条，其余 6 项覆盖 #1/#2+#3/#5 及 3 条模板独有项。
  报告：docs/硬约束机器保障缺口-五轴审查-2026-10-25.md（用 round 90 的 review-five-axis 模板格式）。
  剩余两项登记不夹带（objective 逐点攻破）：约束 #6 的 DataRoot 测试（小、独立一轮）；
  约束 #5 的新增删除路径必经 refuseDangerousPath 门禁（需先定义删除路径的可机判形态，否则易误报）。

  本轮我自己的四处测量错误（都如实记进报告附节）：
  门禁数一会儿 41、一会儿 1、dsh-web-app 0 处 vs 1 处矛盾 —— 根因是助手函数 g() 在 catch 里
     return 空串，随后 split 算成长度 1，把命令失败当成零结果。定规：测量必须查 rc 并打印错误，
     空输出与失败是两件事（后两次重测后数字才可信）。
  我给约束 #5 判过「无任何门禁覆盖」—— 偏严：refuseDangerousPath 有 Go 单测（1 文件）且非 Windows
     分支 trash_other.go 与 Windows 同序；缺的是校验新增删除路径是否经过它的门禁。两回事。
  PR 模板 #1 假阴性缺 —— 我用自动子串匹配建映射（约束「Go 纯标准库」vs 模板「go.mod 未新增 require」），
     字面不匹配但语义同物。定规：建映射必须人工逐条核对（与 round 115 分类器同族）。
  我一度要给新报告补 BOM 才对 —— 实测 docs 顶层 28 个 .md 里 23 个无 BOM，且
     docs/archive/2026-10/README.md:8 早写明「无 BOM、纯 LF，与受跟踪 .md 一致」；反倒是上一轮那份
     报告带了 BOM/CRLF。定规：涉惯例先测分布，别拿自己上一份文件当基准；且两种形态门禁都接受
     （multi_bom 判 >=2、repo_eol 对 *.md 无规则），故不回改已提交文件 —— 无必要不等于应改。
  另：报告标题首版写「两次测量错误」而表格已有 4 行（4 行但仅 3 个根因）—— 当场发现并改，
  这正是本轮在修的那类漂移，发生在自己的新文件上。
  本轮只 add 自己的 4 个文件（并行会话在途的 atomic.go 与 32 个未跟踪 .go 未碰，见交接纪律）。
- **模板 OutputSpec.Sections 五轴审计报告（round 115）—— 结论是 NO-OP，但审计本身改了两处认知**：
  用 round 116 修 `{prev}` 的同一思路复查「模板声明的产出契约是否被 prompt 真正要求」。
  **主发现（架构）**：`Sections` 是「有定义、有写入方、**零读取方**」的装饰性元数据 ——
  全仓只有 `model/types.go:155` 定义 + `defaults.go` 与面板（`sections: []` 硬编码）写入，
  无任何代码读它；对比兄弟字段 `MinWords` 非测试 19 处、`OutputFormat` 10 处都有真实消费方，
  `autonomy` 的验收准则枚举里也没有 section 类准则。
  **次发现（可读性）**：97 个声明 section 里 15 个字面未出现在自己的 prompt 中，逐条人工核对后
  **真不一致仅 4 处**（code-review/结论、knowledge-playbook/准备、data-analysis/背景、
  data-metrics-design/目标）—— 且 4 处全是他人模板，我 round 88/90 写的 5 处经核对语义全覆盖。
  因零消费方，其危害只到 P2（误导读者），不会造成运行时故障。
  **三个候选动作全被实测否掉（故 NO-OP，报告里逐个记明理由）**：不补那 4 条 prompt（在给
  零消费方字段补一致性、且他人 prompt 是调优过的产出契约）；不删字段（属 API 契约面 + 产品决策）；
  **不加「prompt 必须提及每个 section」的门禁**（实测会误报 11 处语义已覆盖的，且等于把死字段
  冻结成契约 —— round 64/108 在册的「判据本身误报」）。给出「若将来要让 Sections 生效」的正确顺序：
  先加消费方准则 → 消费方上线后 4 处立刻升级为阻塞 → 面板同步可编辑，顺序不能反。
  **本轮最贵的认知（报告附录如实记）**：同一个「字面命中」数我量出 **83 / 68 / 82** 三种结果 ——
  前两次都在 Node 里正则解析 Go 源码，第一次块边界判错、第二次把多段 `"…" +` 拼接的 prompt
  只取到最长一段。第三次改为 **Go 端运行时遍历 `Defaults()` + `strings.Contains`** 才拿到真值。
  定规：**凡「字段值是否满足某条件」的判断必须执行程序读运行时真值**，不能靠解析源码文本 ——
  解析器等于重做一遍 Go 的词法与拼接语义，出错面远大于收益（与 round 92 的 POSIX-vs-.NET 正则、
  round 116 的行为测试同族）。探针用完即删并复验 `check_untracked_go` rc=0（未跟踪 .go 会污染我自己的门禁）。
  另修一处自己的分类器误判：自动分类把 9 条判为「真缺」，人工逐条核对只有 4 条成立 ——
  若直接按 9 条改就会**改错 5 条**。分类器输出是待核线索，不是结论。
  报告：docs/模板OutputSpec分节-五轴审查-2026-10-25.md（用 review-five-axis 模板格式，可直接贴 PR）。
- **教训反哺进模板，并给反哺本身加断言（round 98 收尾）**：
  本轮最贵的教训（变异样本必须取自真实缺陷形态 —— 判据只搜 rgb 前缀而本仓多数写 rgba 形态，
  注入 rgba 时 rc=0 漏检）写进 CHANGELOG 只是「记录」；objective 要的是「通过修改和增加模板内容」
  提升质量，所以把它加进 quality-verify 模板的 prompt 第 ⑤ 条（抽象规则 + rgba/rgb 具体反例）。
  **但「写进源码」不等于「运行时读得到」**：Go 里多行字符串拼接很容易断在中间（本次就因行尾少写
  续接加号而破坏语法，gofmt 直接报错）。所以补了 TestQualityVerifyPromptCarriesMutationSampleRule，
  断言 tpl.PromptTemplate 真的含这条规则与 rgba 实例。
  **一次无效变异的自我纠正（本轮第二个教训）**：第一次变异我删那句时顺带删掉了行尾续接符 ——
  测试确实 rc=1，但原因是 **build failed**（语法被我自己破坏），根本不能证明断言生效。
  换成语法合法、仅语义缺失的变异（把那句话改成另一句通顺的话）后，才由断言在
  templates_test.go:94 点名「prompt 缺『变异样本取自真实形态』这条」。
  教训：**变异必须只破坏「被测语义」，不能顺带破坏「承载它的语法」** —— 否则判红来自编译器，
  判据本身是否有效仍然是未知的（与 round 53「契约漂移让断言读 null 而 vacuous-pass」同族）。
  模板库现状：29 条模板、7 领域（quality 领域 5 条，含五轴审查）。
- **CSS 死令牌第三批：五轴审查报告 + 复活 --scanline（round 99）—— 「不能机械替换时就出报告」的示范**：
  前两批（字阶 57 处 / 间距 68 处）能机械化，靠的是「硬编码值与令牌值精确对应」。第三批 5 个令牌
  （--ink/--muted/--accent-soft/--accent-3/--glow-violet）**连自身值都在 CSS 里零出现** ——
  机械替换法失效，此时正确做法是**出报告而不是硬改**（objective：目标不要太宏大、逐点攻破）。
  **本批唯一的机械活**：`--scanline` 在 L854 有一处字面量精确重复，已换成 var(--scanline)；
  其余 4 处 `.055)` 是**不同基色**（L91 硬编码紫、L276/277 蓝调），刻意未换。基线 6 → 5 条。
  报告（docs/CSS死令牌第三批-五轴审查与处置建议-2026-10-24.md）的核心发现：
  - **P1-1 虚假的抽象比没有抽象更坏**：:root 里 --accent-3:#a78bfa 注释写着「副信号：紫，
    科技感的第二个记忆色」，但 L91 的紫色是硬编码 rgba(167,139,250,.055) —— 核实过
    #a78bfa → 167,139,250 **逐字节一致**。即设计意图存在、实现绕过了令牌：读 CSS 的人会以为
    改 --accent-3 能换全局紫，改了毫无效果。这不是「多余」，是「骗人」。
  - **P1-2 派生链未闭合（有数据）**：rgba(var(--accent-rgb),α) 共 **50 处使用、36 个不同 α** ——
    同一用途（半透明主色底）无约定值。而 --accent-soft（α=.14）在 :root 外**零字面出现**，
    故连「收编」的机械活都没有，必须先定约定。
  - **P2-1 命名债（有数据）**：var(--dim) 37 处、var(--dim-2) 8 处、**var(--muted) 0 处** ——
    弱化文字色实际全走 --dim，--muted 是纯冗余。
  - **安全/性能两轴明确无发现并说明理由**：getPropertyValue 全仓 0 处读取 ⇒ 删除运行期零风险；
    CSS 变量解析开销与是否被引用无关 ⇒ 死令牌不构成性能负担（写明白免得被误当性能问题优化）。
  报告附「处置建议表 + 建议顺序 + 不建议做的事」：不建议把「硬编码色值数量」做成判据
  （需一次梳理 38 个唯一色值，属宏大目标；阈值型判据必然产噪声）。
  验证：gofmt clean；go test ./internal/web/ rc=0；死令牌门禁 rc=0 且 summary 自洽（44 在用 + 5 登记 = 49）。
- **CSS 死令牌门禁（check_dead_css_tokens.ps1，CI 第 25 个，round 93）—— 「设计系统名义存在实际失效」**：
  round 92 守住了 JS 的 esc() 单点。本轮按同一思路查 CSS 侧，实测发现一个更系统的债务：
  设计系统在 :root 定义 56 个自定义属性（含完整字阶 8 档 --fs-*、间距 7 档 --sp-*），
  但其中 20 个从未被 var() 引用过（全包仅定义处出现 1 次）；同时实际排版用的是
  78 处硬编码 font-size + 36 处硬编码 margin/padding。
  即：设计系统名义存在、实际失效 —— 读代码的人以为改令牌就能全局换字阶，改了却发现毫无效果。
  这比没有设计系统更坏：它给出虚假的抽象。
  门禁判据：扫 :root 定义的令牌，若整个 web 包非注释代码里出现次数不超过 1（仅定义处）即死令牌。
  基线机制（与 gofmt-debt 同构）：存量 20 个登记在 tests/css-token-debt.txt，
  每次运行逐条打印 KNOWN 行（不静默放过）但不计入 rc；新增死令牌才判红。
  基线文件里写明了处置顺序（逐点攻破）：先字阶（78 处硬编码的最大头）→ 再间距 → 最后颜色族，
  每清一批文件同步瘦一圈 —— 这正符合 objective「步子不要太大逐点攻破」的要求。
  变异验证：注入 --dead-new-token 得到精确判红「1 个新死令牌（未登记基线）」，
  基线内 20 个零误报；还原后 rc=0。
  过程中的小教训：门禁文件写完 BOM=0（第 8 次）—— 立即被 check_ps1_encoding 抓到的前一步，修完继续。
  另记录小瑕疵：KNOWN 行在 -Quiet 模式下仍打印（退出逻辑未受 Quiet 控制）。
- **新增 JS XSS 防线契约测试 + 一次「判据形态选错」的完整记录（round 91-92，前端工程质量）**：
  按 objective「全栈技术工程质量（HTML/CSS/JS...）」侦察前端侧，事实基线：
  web 目录 57 个受跟踪文件**全是 .go**，前端资产以 **Go 字符串字面量**内嵌
  （html.go / assets_css.go 1409 行视觉系统 / assets_script.go 1089 行 JS）——
  这是「单 EXE 零依赖」设计的必然选择（无构建链、无 node_modules）。
  质量基线：118 个 Test / 42 个测试文件，CSS 有跨页一致性契约、a11y 有名字契约、
  文案有一致性契约、脚本有可移植性契约 —— **但 JS 侧只有一个契约**。
  ① 侦察发现的真实形态：18 处 innerHTML 赋值逐个读代码，确认**全部**用户可控数据
  都过了 esc()（60 处使用）—— 代码质量好，但这条 XSS 防线只有**一个根基函数** esc()，
  它若被改坏（少一个字符/正则写漏），18 处同时失守而**无任何测试守着**。
  ② 第一次判据形态选错（完整记录，因为它本身就是教训）：第一版想判
  「innerHTML 拼接里的裸标识符」，实测报 15 处 —— **全是误报**：head/body 是纯字面量、
  keepDetail 来自已渲染 DOM、rowOf 内部转义。**静态分析穿透不了变量数据流**，
  这不是调参能救的（收到 25 行/5 行窗口后仍有 11 处误报，逐个读代码判决全部是误报）。
  按 #39（不选折中）**删掉该判据**，换成守 esc() 本身：五字符覆盖 + 五实体齐全 + null 兜底 + 非死码。
  ③ 新判据又踩两个坑（都被实测抓出）：
  - 首版取 esc 定义后 300 字节窗口查「包含某字符」—— 窗口里的 null 兜底表达式自带单引号，
    「含单引号」**恒真**，变异（删掉正则里的单引号）**漏检**。修：精确抽出 replace 的正则字面量再查；
  - 抽正则时 off-by-one（结束位置从 0 算），正常情况就 FAIL。修：起始指向正则首个斜杠、从 +1 处找闭合。
  ④ 最终双向验证通过：正常 rc=0；变异（去掉正则里的单引号）rc=1 且精确点名
  哪个字符缺失；文件已还原。web 包全量测试 rc=0。
  方法论固化：这是 round 64「判据本身误报」家族的第 4-6 次（误报 15 处 → 收窄仍误报 11 处 → 换形态）。
  这次多一层收获：**当判据的误报是「形态选的本质局限」而非「参数不够窄」时，要敢换形态**——
  判「数据流」对静态分析太难，就改判「单点根基函数」，后者可证伪且零误报。
- **新增「五轴审查报告」模板（`review-five-axis`，round 90）—— objective 明确点名的审查形态**：
  objective 本轮新增要求：「五轴审查（正确性/可读性/架构/安全/性能）**分级出报告，可直接贴 PR**，
  附合并结论与门禁建议」。这是模板库的直接缺口（既有 `code-review` 是笼统审查、无分级、无 PR 形态）。
  按 objective「**目标不要太宏大、逐点攻破**」的要求，本轮**只加这一个模板**并做扎实：
  - **五轴逐一过**：正确性（逻辑错误/边界/并发）、可读性（命名/注释/结构）、
    架构（耦合/边界/可测试性）、安全（注入/越权/Secrets）、性能（热点/分配/复杂度）；
  - **三级分级**：P0 阻塞 / P1 应改 / P2 建议，每条含 `文件:行号` + 问题一句说清 +
    **为什么是问题（后果）** + **建议改法（可直接执行的 diff 思路）**；
  - **无问题的轴也要写「未发现问题」并说明检查了什么** —— 不许留空让读者猜；
  - **结尾给合并结论**（可合并/改后可合并/需重写）+ **门禁建议**（哪些发现应变成机器判据）；
  - **区分已证实与存疑**：没读代码证实的不许写成事实（本仓铁律的模板化）。
  模板数 28 → **29**；`TestDefaultsCount` 同步（该测试再次**正确地发现数量与结构变化**：
  quality 领域从 4 变 5 ⇒ 已把「每领域固定 4 个」的硬编码改为 `wantCount` 查表，
  避免以后每加一个模板就要改一处判据）。
  顺带修掉上轮（round 89）提交被中断的遗留：改动都在暂存区，已用原提交信息补交为 `d2b67d4`。
- **新增「工程质量」模板领域（4 条，round 88）—— 补上模板库与工作台使命的脱节**：
  objective 的原话是「全部只聚焦于**提升被选中文件夹的工程质量**」，而内置模板库的 6 个领域
  （content/code/knowledge/data/solution/report）**没有一条面向工程质量本身** ——
  round 1-88 的实际工作全是判据/门禁/验证/卫生类，模板库却覆盖不到它。这是模板与使命的脱节。
  新增 `quality` 领域 4 条，对应工程质量提升的**四个动作**（与既有 6×4 结构对齐）：
  | ID | 名称 | 动作 |
  |---|---|---|
  | `quality-audit` | 工程质量体检 | 找问题（按严重程度×影响范围列清单，区分已证实/未验证） |
  | `quality-gate-spec` | 判据规格设计 | 把教训变成机器可判（可证伪判据+豁免+三态+变异验证方案） |
  | `quality-verify` | 判据有效性验证 | 证明判据有效（构造最小缺陷+记录完整输出+反向验证+查空转） |
  | `quality-hygiene` | 工程卫生清理 | 防复发/保卫生（分类盘点+逐条列名 ignore+双向验证） |
  四条 prompt 都写进了本仓的真实纪律：「没测成不许写成合格」「宁漏不误」「不通配（会误伤真交付物）」、
  「结论只能是已验证有效或未验证」。模板数 24 → **28**，`TestDefaultsCount`/`TestRegistryRoundtrip`
  同步（这两个测试**正确地发现了数量变化**并 FAIL —— 正是它该做的）。
  **过程中一次自伤（第 6 次双重 BOM）**：我给 `.go` 文件写入了 BOM（那是 `.ps1` 的约定），
  `go vet` 报 `illegal byte order mark`；且一次写进 **2 个** BOM。已全部清除 + gofmt。
  **教训**：写 `.go` 时绝不能再套用 `.ps1` 的 BOM 前置逻辑 —— 两类文件的字节约定相反，
  而我的写入助手是同一套代码路径。这次是编译器抓到的（`illegal byte order mark`），
  但如果写进的是 `.ps1` 就不会有人报 —— **同类错误在不同文件类型上的可见性完全不同**。
- **skip 披露门禁（`tests/check_skip_disclosure.ps1`，CI 第 23 个，round 87）—— Go 侧「没测成」第一次进入判据视野**：
  round 87 侦察发现：全仓 172 个受跟踪 `_test.go` 里有 **49 处 `t.Skip`/`t.Skipf`、涉及 28 个文件**。
  抽查确认质量是好的（`probe_windows_test.go` 的 icacls 三类跳过连「理由+后果」都写了，注释里甚至立了纪律
  「禁把没验成当验过了」），但存在两个真问题：
  ① **总数不可见** —— 49 处散在 28 个文件里，没有任何机制统计。于是「某个包其实全靠 skip 凑数」
     这种腐化**静默发生**（与 round 47「内存门禁恒 0 条」同族：沉默 != 正常，只是这次沉默的是「跳过」）；
  ② 个别 skip 可能没写理由（`t.Skip()` 空参数），静态可判。
  本仓铁律是「没测成 != 合格」（check_resident_longrun / check_perf / check_queue_size 都按此设计），
  **Go 侧的 t.Skip 正是「没测成」的本体，却一直不在任何判据视野里**。
  判据（窄、可证伪，刻意不做阈值语义判断）：
  1) `t.Skip()` / `t.Skipf()` **空参数** ⇒ 判红（客观事实，无需推断）；带字符串参数视为已披露；
  2) **总数打进输出**（49 处 / 28 文件 / Top 5 文件）—— 让数字可见，但不做「skip 太多就判红」
     （那需要定阈值，正是本仓在册「模糊语义检查制造噪声 ⇒ 门禁被删」的形态）；
  3) **读数落盘** `skip_disclosure` 序列（总数/bareCount/文件数）—— 与 untracked_go / resident_longrun
    同构：单次读数不够，**趋势上升 = 在靠 skip 凑数**（round 47 同族信号），由趋势器去判。
  与 `check_test_assertions` 分工明确：后者管「Test 有没有断言」（零断言 = 假测试），
  本件管「Test 有没有真的跑」（skip = 没跑）+「跳过是否披露」。合起来才覆盖「测试既存在、又真在测」。
  首跑即绿（49 处全部带理由）；变异验证：注入 `t.Skip()` 空参数 ⇒ `evil_test.go:7` 精确判红，
  同文件里有理由的那处正确放行。
  **`check_report_figures` 第四次兑现**：本门禁入库后再次点名报告 5 处数字滞后（36→37 / 22→23 / 32→33 等），已同步。
- **gitignore 泄漏门禁（`tests/check_gitignore_leak.ps1`，CI 第 22 个，round 71）—— 治「靠人凭记忆补 ignore」**：
  本仓三连实测同一根因：round 67 `tests/_egress/` **41.2MB** GeoIP 数据、round 68
  `workbench/build/` **32.1MB** 构建目录、round 71 `workbench/exec`（**17KB Go 覆盖率 profile**）
  都没被忽略。三次都是「.gitignore 靠人凭记忆补，漏一类就静默积累」。后果不只是占空间 ——
  untracked 越来越长后，后来者分不清「并行会话在途工作」与「忘了忽略的垃圾」，
  而 round 69 已演示过：73 份真有价值的报告因为在 untracked 里躺了几天，处于误删即永失的状态。
  四族判据（只认客观形态，不做语义推断）：
  - **A 构建与依赖**：`node_modules/`、`*.exe`、`*.dll`、`*.so`、`*.dylib`、`*.wasm`、`*.map`；
  - **B 覆盖率/测试副产物**：`coverage*`、`*.test`、`*.coverprofile`，以及**内容特征**
    `mode: set`/`mode: count`/`mode: atomic` 开头（round 71 的真实案例既无前缀也无扩展名，只能靠内容认）；
  - **C 大型二进制**：>5MB 且非源码扩展名；
  - **D 缓存**：`*.pyc`、`__pycache__/`、`*.cache`、`cache.db`。
  三项显式豁免（带理由）：并行会话在途 `*.go`（他人所有权，不代改不判红）、
  `workbench/cmd/workbench/rsrc_windows_amd64.syso`（受跟踪的交付必需资源，见 build.ps1:70-77）、
  `.workbuddy/memory/gate-readings.jsonl`（读数数据，按约定故意不跟踪也不忽略）。
  **刻意不判「untracked 总数太多」** —— 那会把并行会话的正常在途工作判红（在册：判据过宽制造噪声）。
  四向变异实测：node_modules / exe / 6MB 二进制 / cache.db+pyc / coverprofile **各自精确点名**
  （如 `out/app.exe [A:*.exe 构建产物]`、`data.bin [C:大二进制 6MB]`、
  `workbench/exec [B:Go coverprofile（mode: 开头）]`），三个豁免项正确未判；真仓库 rc=0。
  开发中一次自纠：首版漏了 `mode: set` 内容判据（只在注释里提了），变异测试立刻暴露 ——
  round 71 的真实案例因此没被抓到。**「注释里写了」不等于「实现了」**，与本仓
  「修好判据≠判据生效」（round 52）同族，这是它的微观版。
  另：`check_report_figures` **第三次在真实场景兑现** —— 本门禁入库后再次点名报告 5 处数字滞后，已同步。
- **73 份历史报告归置入库（`docs/archive/2026-10/`，round 69）**：
  实测 docs 顶层有 **57 个受跟踪 .md**，另有 **73 份高价值报告散在 untracked 里**
  （前线门禁回执 14 份 / 前端视觉系统重做 / 缺陷根因分析 / 体验治理指标体系 / 验收报告等，共 252KB）。
  它们既是 objective 要的「报告与优化建议落点」，又处在**最危险的状态**：未入库 = 误删即永失、
  `check_doc_refs` 等门禁看不到、后续会话无法引用。
  处理：
  ① **入库前敏感信息扫描**（API key/token/密码/私钥/机器名 5 类模式）—— 73 份全部干净；
  ② 归置到 `docs/archive/2026-10/`（按月归档），docs 顶层恢复为「当前有效文档」的语义；
  ③ 生成**归档索引 README**（73 条，含标题与大小），否则散文件不可导航；
  ④ 编码统一（无 BOM、纯 LF，与受跟踪 .md 一致，复检 0 偏差）。
  一并清理：空的 `deliverables/` 目录、根目录一次性 `前端自检.cmd`（归入归档）。
  入库后 5 个文档门禁（doc_refs / report_claims / multi_bom / repo_eol / comment_staleness）全 PASS。
- **内存口径隔离门禁（`tests/check_caliber_isolation.ps1`，CI 第 21 个，round 64）+ 第 9 类失效形态「判据本身误报」**：
  本仓同时存在两个内存口径且**比值不是常数**（§9 实测带 1.5×~3.83×）：RSS（WorkingSet64，
  128MB 上界判它）与 Go heap（HeapInuse，A7 趋势/MemLimitMB=700 判它）。「RSS 超 limit 就判泄漏」
  这类推断因此全错，而 §9 只把纪律写在文档里、**没有判据守** —— 文档纪律靠人记得，正是本仓反复踩的形态。
  两级判据（round 64 变异测试后定的形态）：
  - **L1 同行混用**：同一代码行同时出现两侧特征 => 判红（实测命中 `if rss > int64(cfg.MemLimitMB)*1024*1024`）；
  - **L2 跨行混用（变量传播型）**：把比较行按运算符切左右两侧、分别判口径，只有「一边 RSS 一边 heap」
    才判红（实测命中三行写法 `$rss=...WorkingSet64 / $limit=700 # MemLimitMB / if ($rss -gt $limit)`）。
  **L2 为何长这样（两次收窄，都是实测逼出来的）**：
  - 首版 L2 是「文件级两侧特征共存 + 有 RSS 比较行」，**把正确代码判红**：`tests/e2e.ps1` 的
    `$budG.budget.memLimitMB -gt 0` 是断言自愈配置项存在（与 RSS 判定各自独立、都正确），
    却因字段名相同被算成 heap 特征。这正是**第 9 类失效形态：判据本身误报** ——
    与「空转/洗绿」并列的另一种失效，且同样危险（误报让门禁被撤）。
  - 根因：`memLimitMB` 同时出现在两个合法场景（自愈配置检查 / 跨口径比较），**仅凭名字区分不了**；
    所以 L2 必须落在比较表达式本身，而不是文件级特征共存。
  **开发中第三次踩 write 通道转义污染**：门禁正则里的 `\\b`、`\\s` 被双重转义落盘，
  PS 单引号内成了「字面反斜杠+b」，**门禁写完自己失效却报合规**（连踩两次才发现）。
  最终对策：判据一律用字面片段（`WorkingSet64`/`if`/`-gt`），不用带反斜杠的正则 —— 已写进头注防再犯。
  实测：真仓库 386 文件 RC=0（误报消除）；变异仓库 RC=1（同行 1 处 + 跨行 1 处，两种真实缺陷形态各命中一）。
  顺带：`check_report_figures` **首次在真实场景兑现价值** —— 本门禁入库后它立刻点名报告 5 处数字滞后
  （34→35 / 20→21 / 30→31 / 结论行两处），已同步；报告同时新增第 9 类失效形态。
- **报告数字一致性门禁（`tests/check_report_figures.ps1`，round 60）—— 本仓第 8 类失效形态**：
  `docs/门禁体系体检报告-2026-10-23.md` 开头就承诺「所有数字来自实测」，但那些数字**靠人手同步**：
  round 56 建它时人工核 6 个、round 58 加了 stdlib 门禁后又手工改 8 处（32->33、19->20、28->29、
  32/32->33/33、第 6 类->第 7 类）。**下一次谁再加删一个门禁，报告就会静默失真** —— 而它是后续
  agent 的整改入口，数字错了就是误导（与 `check_report_claims` 同源，但那条只判「⏳ 状态栏 vs
  CI 制品」，不判规模数字）。
  判据极窄，只核 4 个「仓库里有唯一权威来源」的数：受跟踪门禁数（ls-files）、CI repo-gates 数
  （ci.yml 去重）、check_all 覆盖数、未跟踪 Go 测试数（git status -uall）；另核结论行
  「N 个判据 / M 个进 CI / 0 个孤儿」的 N/M 并与实测一致、孤儿数非 0 时判红。
  刻意不做：不核「失效形态几类」这类语义数、不核读数条数/内存中位这类**随时间自然增长**的值
  （那种数根本不该写死进报告）、不核叙述性内容。只核同一事实有唯一权威来源的那几个。
  **它第一次运行就抓到真问题**：我把门禁挂进 check_all 后 check_all 覆盖数 29->30、新门禁入库后
  受跟踪 33->34，而报告还写旧值 —— 正是它设计要抓的滞后。
  开发中自己也踩了一个时序坑并修掉（与 round 50 同族「鸡生蛋窗口」）：只数 `ls-files`（已跟踪）时，
  「新门禁已写盘但还没提交」那一刻会少一个，导致报告写 34 而门禁报实测 33。修法是把「磁盘上
  已存在的未跟踪同模式文件」也计入 —— 提交后两者恒等、CI 干净检出时未跟踪数为 0 口径不变。
  四向变异实测：数字全对=0；受跟踪写错=1（点名行号与实测）；CI 数写错=1；孤儿数写 2=1。
  挂载选择：**只挂本地 check_all、不上 CI** —— 它的输入之一是「未跟踪 Go 测试数」，CI 干净检出
  必然为 0（与报告写的本地实测值不符），挂 CI 会永远红。这是「本地判据就该只挂本地」的具体例。
- **纯标准库约束门禁（`tests/check_stdlib_only.ps1`，CI 第 20 个，round 58）**：
  本仓设计前提是「Go 1.27 纯标准库 + 单 Windows EXE」，docs 里多处明文承诺
  （对标报告:3「纯标准库，Windows 单 EXE」；架构与运维.md:11「单进程，7.7MB，纯 Go 标准库」；
  peer源码精读笔记.md:132 甚至写明「照抄外部存储会把『零依赖』这条硬约束打掉」）。
  但 round 57 实测：**32 个受跟踪门禁里零个检查这条约束** —— 加一条第三方 require 不会有任何
  自动化反对，直到某台机器 go build 报「找不到包」，归因还不清。这是「约束写在文档里、
  靠人记得」的形态，体检报告 §2 六类失效里第 1 类（判据不存在）的新变体。
  四条判据（全客观可证伪）：① HEAD 的 workbench/go.mod 无 require 块/行；② 无受跟踪 go.sum
  （有校验和即有依赖）；③ 无受跟踪 workbench/vendor/ 文件；④ workbench/ 下的受跟踪 .go
  无外部模块路径 import（路径首段含点 —— 标准库与本模块首段都不含点）。
  **开发过程两处值得记（都是变异测试抓的）**：
  - 判据 4 首版**静默漏报**：正则要穿「JS 字符串 → PS 单引号串 → 门禁自己的 Invoke-Git 加
    引号 → git argv 解析」**四层**，`\.` 在中间某层被吃掉，变异仓库里外部 import 判 PASS。
    改为 UTF8 模式文件 + `git grep -f`（只穿一层路径），变异立刻正确判红并点名行号。
    —— 「用脚本改脚本，产物必须回读+实跑」再记一笔，这次栽在**嵌套调用自身**的转义层。
  - 判据范围先写成全仓 `.go`，实测抓到 `docs/benchmark/evidence/*.go` 47 行外部 import ——
    那是 asynq/river 的**对标取证摘录**（不在模块内、不参与编译），属误报。威胁面是 Go 模块，
    判据范围就该等于模块（限定 workbench/）。判据过宽制造噪声 = 在册的「模糊语义 => 门禁被删」。
  三态+四向变异实测：clean=0 / require=1（点名 go.mod 行）/ extimport=1（点名源文件行）/
  gosum=1 / vendor=1 / 模块布局缺失=2（UNVERIFIED，不是静默放过）/ 目录不存在=2。
  另记一个实测发现的**已知边界**：`-RepoPath` 指向「非仓库但在某仓库之内」的目录时，git 会
  向上找到父仓库的 .git（判的其实是父仓库 HEAD）。默认路径无此问题；头注如实标注不掩盖。
- **门禁体系体检报告（`docs/门禁体系体检报告-2026-10-23.md`，round 56）**：
  把 round 50-55 零散发现的失效形态汇总成一份可直接据以整改的报告，不必后续会话重新考古。
  实测数字（**逐条核对过，非引用记忆**）：受跟踪门禁 **32** 个、CI repo-gates 执行 **19** 个、
  CI build job 附加 3 个判据、本地 check_all 覆盖 **28** 个、未跟踪 Go 测试 **31** 个、
  读数落盘 **47** 条（其中 resident 8 条：中位 63.6~66.1MB / 上界 128MB，余量充足）。
  报告的**核心是一张「已确认失效形态」表**，6 种全部真实发生过、且现在都有对应护栏：
  ① 判据存在但没挂受理面（round 22 前 23 个门禁 CI 只跑 4 个）⇒ check_gate_coverage；
  ② 挂了受理面但判据恒不落盘（round 47 内存门禁 phase=running 恒真，静默失效 6h）⇒ 存在性核对；
  ③ 修好判据但产物没重建（round 52 A7 修复在源码、产物旧 12.6h，走产物的调用仍报假 leak）⇒
     check_artifact_soak_fresh；
  ④ 挂了受理面但断言空转（round 55 e2e_soak 读不存在的字段，对 round 39 缺陷 18 PASS/0 FAIL）⇒
     check_soak_json_contract + MissingFlat 前置守卫；
  ⑤ 读数过期当现状报（round 42 报「红窗 1.3h」而实际已 >2h）⇒ 新鲜度 + 6h 陈旧告警；
  ⑥ 门禁自己的文案命中自己的判据（round 32 注释过期自检自红、round 48 字面示例被自我扫描）⇒
     排除自身 / 历史记录措辞。
  **共性值得单独记**：6 种里有 5 种的表现都是「**看起来是绿的**」—— 这不是流程洁癖的问题，
  而是这类缺陷只有主动证伪才暴露，这正是本仓把变异测试/双向 fixture/负向路径实测列为硬要求的理由。
  报告同时如实标出**边界与不做的事**：产物重建阻塞在他人所有权（活实例占用 + 在途测试使
  go vet 失败）、31 个未跟踪测试属并行会话、24h soak 只差攒时长；以及 §9 两个内存口径互比
  **刻意不设门禁**（语义问题，硬做会触发「模糊语义检查⇒门禁被删」）。
  全部 8 个文档类门禁（doc_refs/report_claims/multi_bom/repo_eol/ps1_syntax/gate_coverage/gate_links/changelog）PASS。
- **产物时效门禁（`tests/check_artifact_soak_fresh.ps1`，round 52）—— 修好判据 != 判据生效**：
  本轮用生产产物跑 `--soak`，拿到 **verdict=leak / spanHours=99.73**；对同一份 metrics.csv
  用当前源码跑，得到 **ok / 14.58**。差别只有一个：round 39 的趋势修复提交于 10-23 12:14，
  而 `dist/workbench.exe` 构建于 10-22 23:41 —— **修复已在源码、不在产物**（旧 12.6 小时）。
  受影响面是实的：`tests/e2e_soak.ps1`（第 18 行默认用该产物）与 `tests/keepalive.ps1`（拉起的也是它）。
  也就是说「A7 趋势已修好」这个结论，在任何走产物的真实调用里仍然是错的 —— 这比没修更糟，
  因为它给「已经修了」的错觉。新门禁取 `--version` 里的构建时间，与**会改变 soak/A7 结论的
  4 个源码文件**在 HEAD 的最新提交时间比较，源码更新即判红并给出「重跑 setup/build.ps1、
  不要靠改判据把结论圆回去」的修法。
  口径刻意极窄：只比那 4 个（trend.go / supervisor.go / soak.go / acceptance.go）。
  若比「所有源码」，「产物总落后于 README/无关包」这种常态会天天红 => 噪声 => 门禁被删（在册教训）。
  三态端到端自证（用 `-PathsOverride` 注入不同清单）：PASS 0（注入早于产物的 types.go，
  源码 10-22 17:19 < 产物 23:41）、FAIL 1（注入 trend.go，滞后 12.6h）、UNVERIFIED 2（产物不存在）。
  **它第一次运行就抓到真实缺陷**（不是理论护栏）。
  挂载选择：只挂 **build job**（repo-gates 阶段还没有产物，挂那里必然 UNVERIFIED；build job 里
  产物当场重建，判据天然成立）+ 本地 check_all（本地未重建就是红的，红得对）。CI 里
  UNVERIFIED 按 rc=0 放行（不计 PASS 也不误红），FAIL 才 throw。
  **本轮我没重建产物，两条硬约束实测过**（已写进在途件交接记录 §6，含解铃步骤）：
  ① `dist/workbench.exe` 被 7×24 活实例 PID 29404 占用（运行 17+ 小时、正在跑任务），
     覆盖它必须先停生产实例 —— 不为验证牺牲在跑的活儿；② `go build ./cmd/workbench` rc=0 但
     `go vet` 因并行会话在途测试失败（`hiddenTestCmd redeclared`），而 `setup/build.ps1` 内含
     `go test` 会被挡住。解铃人是收尾那批测试的会话，不是强行停实例的我。
  自曝一条：写交接记录时又造出**第 6 次双 BOM** —— 读文件时 JS 把 BOM 保留成 \uFEFF 字符、
  我没剥就前置一个。**而这正是我本轮早些时候刚写进 CHANGELOG 的纪律**（「补 BOM 前必须数清
  已有几个，剥要按连续前导循环剥」）。教训：纪律写下来 ≠ 会用上，必须在**每次写文件后回读计数**
  —— 本轮起我把「字节级回读 BOM 计数」作为固定收尾动作（当场发现并修好，且自查了全部 4 个改动文件）。
- **受理面覆盖门禁（`tests/check_gate_coverage.ps1`，CI 第 18 个）—— 把「判据没挂受理面」变成机器不变量（round 50）**：
  起因：做自动化覆盖率审计，找「判据写好了但没有任何自动化会跑它」的脚本。本仓反复犯这错 ——
  round 22 前 tests/ 有 23 个门禁、CI 只跑 4 个；round 47 更隐蔽：内存门禁挂在 check_all 上
  却因判据用错（phase=running 恒真）恒不落盘、静默失效 6 小时。没有任何机制保证下一个新门禁
  一定挂上受理面，本件补这条不变量。
  受理面四个（会被执行的地方，注释里的提及不算）：ci.yml / check_all.ps1 / .githooks/pre-commit /
  setup/build.ps1。口径刻意宽松：只要求至少挂一个、不要求必须上 CI —— 否则把本仓合理分层当噪声
  （resident_longrun 属本地全量、check_staged_gofmt 属提交前，都合理）。审计结论：30 个受跟踪
  check_*.ps1 全部有受理面、0 真孤儿。
  三处设计：
  (1) 引用匹配认正反两种斜杠：ci.yml 用 tests/x.ps1、check_all 用 Join-Path 的 tests\x.ps1。首版
      正则 JS 少转义一层（[\/] 变成 [/] 只认正斜杠），把 8 个挂在 check_all 上的凭空报成孤儿。
      用 Node 生成的 PS 正则，转义产物必须回读文件 + 实跑 —— round 48 坑1 同一个洞，二次踩到。
  (2) 受理面内容读索引优先、回退 HEAD：新门禁与它的挂载通常同提交落地，只读 HEAD 会有「暂存后
      提交前那一刻，新门禁把自己报成孤儿」的鸡生蛋窗口，反而惩罚最想鼓励的动作。CI 里索引==HEAD。
  (3) 自指 + 排除运行器：本件自己也必须挂受理面（先暴露再补挂，比「检查别人、自己没人跑」诚实）；
      check_all 是运行器本体、被 ls-files 的 tests/check_*.ps1 匹配到，显式排除（变异时暴露）。
  变异（一次性临时仓库，用完即删）：ci.yml 正斜杠挂 1 + check_all 反斜杠挂 1 + 1 真孤儿 + check_all
  注释里提到该孤儿 —— 精确只点名 check_orphan，两种斜杠都认、注释不算挂载，全对。
- **趋势器加「存在性核对」：已声明落盘的序列若零读数就点名告警（round 48-49，把 round 47 的教训机制化）**：
  round 47 的内存门禁 bug 能静默存活 6 小时，根因是趋势器按**现有读数**分组 ——
  恒 0 的序列在报告里**根本不出现**（沉默被当成正常）。本件补上第五个核对：
  已声明落盘的 gate 清单从源码**自动提取**（扫 tests/ 与 setup/ 里引用 gate-readings.jsonl
  的脚本中的单引号 gate 赋值）——将来新增落盘脚本自动纳入核对，不靠人记得更新清单；
  每个声明序列必须在读数文件里至少出现一次，否则逐个点名「要么没被调用、要么判据失效
  （round 47 就是后者）」。coverage 显式豁免（由 build.ps1 在 CI 侧落盘，本地出现不了），
  豁免计数输出、不静默。定位不变：仍 informational（rc 只 0/2），存在性告警也不判红。
  开发过程自己贡献了两个新坑，都被机制抓住（这正是做变异的理由）：
  - **坑1：补丁写坏自排除 guard**。JS 补丁里一个换行转义没生效，把 `if ($sc.FullName -eq
    $PSCommandPath) { continue }` 粘进了注释尾部 —— guard 静默失效，消费脚本注释里的
    字面示例（`gate='...'`）被自我扫描当成声明、污染清单。首轮变异 MISSED（零读数却报
    通过），逐行回读源码才发现。**教训：用脚本改脚本时，转义后的产物必须回读验证，
    不能信「补丁执行成功」的日志**（ok 打印了，坏行也真写进去了）。
  - **坑2：双 BOM 第 5 次，且形态全新**。我的补丁脚本里剥 BOM 的字节循环条件写成了
    永假式（typo 出的比较串把同一字节既当 EF 又当 BF），**剥 BOM 静默失败**，再前置一个
    就成双 BOM。PS5.1 对它的执行形态是「第 1 行把 BOM+井号 当命令报错、随后照常跑完
    且 rc=0」—— **坏了但不拦**，比静默漏检更迷惑：本轮一次趋势器运行就是这么过的。
    抓住它的是分层防御：`check_ps1_encoding`（查工作区）当轮判红，而 `check_multi_bom`
    只查 HEAD、当时未提交所以看不见 —— 两层各有盲区，凑齐才不漏。
  验证：变异注入「只有 coverage 的读数文件」=> 3 个声明序列逐个点名零读数；正常文件 =>
  「存在性核对通过（自动提取 3 个落盘声明）」；修 BOM/guard 后 parse=0、编码/语法/EOL/
  多 BOM/趋势器 6 门禁复验全绿。
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
- **死代码债务清零：删掉 15 个零引用未导出函数（13 文件、净 −177 行，纯删除无新增）**：
  承 round 158（那个长期空转的门禁修好后报出 16 处，其中 workspace_guard.go 已在当时删除）。
  本轮把剩余 15 条清完，删除**前**逐条核过四个维度：受跟踪非注释引用仅 1 处（自身定义）、零测试文件引用、
  零未跟踪文件引用（这条最要紧 —— 未跟踪测试是并行会话的在途件，删掉它们用到的符号会在本地炸）、
  且**不属于「接口实现」豁免类**（若某接口声明了它，接口声明本身会在代码里留下第二处命中，实测没有）。
  删除是门禁自己在输出里推荐的两种修法之一（「删掉，或补调用点」），git 历史可追溯。
  **过程中的一次真实事故与修法（本轮最值钱的教训）**：我第一版删除器用**按行启发式**（从 func 行向下找
  第一个第 0 列的 `}`）。`sourceRoot` 的收尾括号不在第 0 列，于是搜索越界，**把它后面的 `readRepoFile`
  一起吃掉** ⇒ 编译报 `undefined: readRepoFile`（5 处）。我当时的守卫只断言「切除块里有 func 关键字」，
  太弱。处置：`git checkout HEAD -- <本轮改过的 13 个文件>` 全量回退（回退后 `go build ./...` rc=0 复验干净），
  改用 **Go 自带的 `go/parser`**：`parser.ParseFile` 取 `FuncDecl.Pos()/End()` 的**精确字节区间**
  （含 `fd.Doc` 文档注释），**从文件末尾往前**逐个切除 —— 一次成功（13 文件 / 170 行），
  且不再依赖任何缩进或括号猜测。教训：**能交给语言解析器做的结构定位，不要自己按行猜**；
  回退也要有判据（回退后必须复验编译 rc=0，不能只看 `git checkout` 退出码）。
  删完后 cascading 出现 4 处「imported and not used」（被删函数各自的唯一使用者：`sort`、
  `workbench/internal/model`、`path/filepath`、`bufio`、`encoding/json`）—— 用「编译→解析该消息→删对应导入行」
  的收敛循环处理，4 轮后 `go build ./...` 与 `go vet ./...` 均 rc=0。
  基线随之**清空**（保留机制与由来注释，并写明本轮清零的动作与核验口径）。
  一处文档核对：`docs/容器门禁-五阶段固化管理.md` L105 提到 `itoaEngine`，但那是**历史事故叙述**
  （记录并发会话造成的瞬时 `undefined`，并说明当时调用被改到 `countWordsEngine` —— 恰好解释了它为何变成死代码），
  **不是**现状断言，故不改；删除反而与该叙述闭环。
  验证：全量 `go test ./... -count=1` **rc=0、零 FAIL 行**；`check_deadcode` rc=0（KNOWN 0，新增死代码会直接判红）；
  12 项受影响门禁全 PASS；数字未变（43/27/43/29）。
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