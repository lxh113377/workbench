# 会话记录 · 界面简化（ds 端 · 2026-10-20 07:2X-08:1X）

## 目标
老大当轮原话：**「页面乱七八糟的东西太多了,不够简洁,对用户不友好」**（goal-1171ee5f…）。

## 先量后改（诊断，不是猜）
旧版四页截图逐页走查 + 读源码，冗余是**信息过载**而非配色问题：
1. 管理面板 10 个页签平铺（概览/队列/产出/模板/自主/评估/日志/治理/工作目录/设置）；
2. 总览首屏 10 张 KPI 卡（含降级任务/被拒任务这类低频读数）→ 1414px 读数墙；
3. 次级信息 5 张卡全平铺（模型健康度/运行时指标/失败分布/最近错误/快捷操作）；
4. 模型池 9 列表格 + **常显 12 字段添加表单** + 车道表 + 日志 = 1228px；
5. 治理台 hero 下又一条 **banner 把同样的等级/契约/采样再念一遍**。

## 改法（五条统一原则）
页签收敛（10→5）/ 首屏只留 4 个真读数 / 次级信息一律 `<details class="fold">` 折叠 /
空态说人话 / 顶栏「更多」收纳跨页入口。

具体：
- html.go：adminPage 页签与面板重写；brand 改 `<a class="brand">`（保留 class="brand" 契约），
  topactions 加 btnMore + topmore 菜单；占位符 4 个。
- assets_script.go：整段管理面板 JS 重写 —— panes 只留 5 个；新增 `fold()`/`emptyHint()` 小件；
  renderOverview 4 KPI + status-line + 4 折叠；queue/outputs/templates 重排；
  renderSettings 合并评估/工作目录/审计（fillWorkspace/fillLogs）；新增 LEGACY_TABS 映射
  （logs/evaluate/workspace→settings，autonomy/governance→overview）；refreshTop 文案精简。
- assets_css.go：新增 .fold/.status-line/.empty-hint/.topmore/.brand 可点化/checkbox 命中区；
  治理折叠卡 .gov-card.foldcard 样式 + 窄屏适配。
- assets_pool.go / pool_page.go：9 列→6 列（延迟+备注并进模型名小字）、操作 6→4+「⋯」、
  添加表单折叠（高级参数再折）、车道表并入总览折叠、日志折叠、更多菜单。
- assets_gov.go / html.go：删重复 banner；无数据时给「点立即采样」说明；明细四卡 foldCard 折叠；
  治理台顶栏同步收敛为「刷新 + 立即采样 + 更多」（原来 5 个按钮 + 3 链接）。

## 判据同步（关键：简化不许回退，也不许把功能删掉）
- Go 新增 `TestAdminTabsStaySimplified`（页签清单双向钉死 + LEGACY_TABS 必须存在）、
  `TestTopbarHasSingleMoreMenu`；`TestAdminPageHasWorkspaceTab` 改写为
  `TestAdminPageHasWorkspaceSection`（工作目录不再是页签，但 fillWorkspace 能力必须在）。
- tests/ui：`ADMIN_TABS` 常量；tabs 契约加数量/清单双向校验；`LEGACY_EXPECT` + legacy 契约；
  cdp_probe 用例 19→20（新增 legacy-hash-admin）；assertions_selftest 新增 4 条反证。

## 实测回执
- go vet ./... rc=0；**go test ./... 27 包全 ok**。
- 候选件 frontend-gate：**PASS ok=20 fail=0 unverified=0**（回执-简洁化-r2）。
- design-audit 15 页：低对比 **0**（最低 5.29:1，首页 6.45:1）、焦点缺失 0、溢出 0。
- 发布件 v1.3.0 两次换件（备份 bak-pre-simple-* / bak-pre-simple2-*，第二次为治理顶栏统一）；活实例 17890 复验：
  四页 200 / 五资源 200 / tabs=5 / btnMore / .fold / LEGACY_TABS 全在；
  活实例 frontend-gate 也 **PASS ok=20 fail=0**（回执-发布件-简洁化）。

## 顺手修的门禁假绿隐患
门禁回执出现「用例数=20/19」：探针新增 legacy-hash-admin，但 frontend-gate.mjs 的
EXPECTED_CASES 还是 19。它是「结果集完整」的唯一判据，写小了 = 允许漏跑用例仍 PASS
（与此前 cdp_probe 无条件 status:"ok" 同类，只是发生在门禁层）。
修：常量 19→20 + 启动前自检（从探针源码数 cases，与常量不符即 exit 3 硬停，不给结论）。
双向验证：常量改 19 → rc=3；正常 → 自检通过 + PASS。

## 踩坑
1. **Go 原始字符串必须成对**：用 Node fs 重写 assets_script.go 时漏了结尾反引号，
   `go vet` 报 "raw string literal not terminated"。教训：脚本重写内嵌模板后**第一件事是 go vet**，
   不要等测试。
2. `strings.Count(html, "class=\"tab")` 会把 `.tabs` 与 `.tabpane` 一起算进来（读数假高 10），
   页签计数要用 `data-tab="`。
3. `<a class="brand">` 改成 `<button class="brand">` 会直接打红
   `TestSubpagesShareUnifiedShell`（它钉的是 `class="brand"` 子串）；用 `<a>` 保留语义又不破契约。
4. design-audit 第一次跑出的「3 处低对比 2.07:1」是 `.status-line .sep` 的分隔点「·」——
   装饰符也算文本节点。改 `--dim-2` 后归零。审计器**把装饰符也计入**这一点要有预期。
5. 审计器读的是**二进制内嵌的 CSS**：改 CSS 后必须重新 build 再审计，否则读到旧值
   （本轮 r2 那次读数没变就是抓到这个）。

## 追加：非技术用户视角（v1.3.1）
「东西太多」之外，「看不懂」也是不友好（#25③ 老大不是技术人员）：
- 线协议→接口类型（OpenAI 标准对话/Responses/Anthropic 格式）、Tier→用途偏好（括号讲清代价）、
  车道→线路来源、BaseURL→接口地址、DSH profile→运行配置（一般不用改）。
- 模板库 24 行全平铺 → 默认只显示价值先验最高 8 个 + 「还有 N 个全部展开」+ 搜索框（250ms 去抖）。
- 新增两条契约：TestTemplatesLibraryIsSearchableAndFolded、TestFormLabelsAvoidJargon
  （后者反向禁 ">Tier<"/"线协议<select"/"DSH profile</td>"，防止行话回潮）。
- 发布件 v1.3.1（SHA256 01185FC4…9DB3），活实例 17890 复验 + 门禁 20/20 PASS。

## 二轮反馈（v1.3.2）：两个真缺陷
老大截图（真实窗口 1040×613）指出任务页空 + 页签像被切掉。取证后不是观感问题：

**A. 页签条被 flex 压成 0 高度（真渲染 bug）**
CDP 实测 `.tabs` height=0，而 .tab 高 46px ⇒ 页签溢出叠到卡片上。
根因：body.admin 是 flex column + .tabs 带 overflow-x:auto ⇒ **按规范 overflow 非 visible 的
flex item 自动最小尺寸=0**，被挤扁；滚动时 z-index 4 < 吸顶栏 20 更明显。
修：.tabs 加 flex:0 0 auto + min-height:46px。改前 tabs.h=0 → 改后 46（同探针同分辨率）。
判据：TestTabsCannotBeSquashedByFlex（只取 .tabs{} 块 + 剥离反证）。
排查同类：.content 无 overflow；.table-scroll/.logbox 在 block 卡片内；只有 .tabs 同时满足两个条件。

**B. 任务页空态摆两张空卡**
三张卡两张只写「没有」，能做的事（加任务）还折叠着。改为全空时一张卡 = 状态摘要 +
一句解释 + 直接摊开的表单；有任务时才回到表格 + 折叠表单。
判据：TestQueuePageHasActionWhenEmpty。

实测：go test ./internal/web 绿；frontend-gate 20/20 PASS；发布件 v1.3.2
（SHA256 9286B468…1CF3）；活实例复验 .tabs 规则与空态分支都在。

## 产物
- 文档 `docs/界面简化-信息架构收敛-2026-10-20.md`
- CHANGELOG [Unreleased] / Changed 条目
- 截图 `dist/ui-shots/simple-release/`（18 张）、`dist/ui-shots/simple-r1/`、审计 `audit-simple-r4/`
- 发布件 `dist/workbench.exe` v1.3.0
