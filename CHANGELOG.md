# Changelog

> 本项目所有值得一提的变更都记在这里。
> 格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
> 版本号遵循[语义化版本](https://semver.org/lang/zh-CN/)。
>
> 版本与产物对应关系以 `dist/SHA256SUMS` 与 `workbench.exe --version` 的实测输出为准。

## [Unreleased]

> 规则：未发布的变更一律记在本节；发版时把本节整体提升为新的 `## [x.y.z] - YYYY-MM-DD`，
> 并立刻补一个空的 `## [Unreleased]` 回顶（CI 的 changelog 门禁会校验这一条）。

### Fixed
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