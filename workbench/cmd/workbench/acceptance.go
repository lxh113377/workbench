package main

// acceptance.go 是「全部目标要求」的总验收出口。
//
// 动机（2026-10-19）：项目里分散着 selftest / soak / diagnose / governance 等各子系统
// 的判据，但没有任何一个命令能回答"目标 10 条要求整体到哪一步了"。人工核对文档
// 既慢又容易虚报。本命令把每条要求映射成**当场可实测**的检查项，输出三态：
//
//	pass   —— 有当场证据
//	fail   —— 有当场证据表明不满足
//	unverified —— 证据不足/依赖未满足（零数据不记 PASS）
//
// 退出码：0 = 全 pass；1 = 有 fail；3 = 有 unverified 但无 fail。
//
// 用法：
//
//	workbench.exe --acceptance [--json] [--verbose]

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"workbench/internal/config"
	"workbench/internal/supervisor"
	"workbench/internal/web"
)

// checkState 是三态判据的结果。
type checkState string

const (
	statePass       checkState = "pass"
	stateFail       checkState = "fail"
	stateUnverified checkState = "unverified"
)

// acceptanceItem 是一条要求的验收结果。
type acceptanceItem struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	State    checkState `json:"state"`
	Detail   string     `json:"detail"`
	Evidence []string   `json:"evidence,omitempty"`
}

// acceptanceReport 是总验收报告。
type acceptanceReport struct {
	Items    []acceptanceItem `json:"items"`
	Pass     int              `json:"pass"`
	Fail     int              `json:"fail"`
	Unknown  int              `json:"unverified"`
	Verdict  string           `json:"verdict"`
	ExitCode int              `json:"exitCode"`
}

func runAcceptanceCLI(args []string) int {
	asJSON := false
	verbose := false
	dataRoot := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--json":
			asJSON = true
		case "--verbose", "-v":
			verbose = true
		case "--data":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--data 需要一个数据根路径")
				return 2
			}
			i++
			dataRoot = args[i]
		case "--help", "-h":
			fmt.Println("用法: workbench.exe --acceptance [--json] [--verbose] [--data <数据根>]")
			fmt.Println("退出码: 0=全通过 / 1=存在 fail / 3=存在 unverified（未验证不得记 PASS）")
			return 0
		}
	}
	layout, err := config.ResolveLayout(dataRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 2
	}
	rep := buildAcceptanceReport(layout)
	if asJSON {
		fmt.Println(web.JSONString(rep))
	} else {
		printAcceptanceReport(rep, verbose)
	}
	return rep.ExitCode
}

// buildAcceptanceReport 汇总全部判据。所有检查都只读磁盘/配置，不启动实例。
func buildAcceptanceReport(layout config.Layout) acceptanceReport {
	items := []acceptanceItem{
		checkHomeMinimal(layout),
		checkModelPool(layout),
		checkSchedulerStrategies(layout),
		checkFailoverLog(layout),
		checkPoolConsole(layout),
		checkAutonomyBootstrap(layout),
		checkAutonomyClosedLoop(layout),
		checkTemplateCompat(layout),
		checkDesignDocs(layout),
		checkConfigIntegrity(layout),
		checkSoak(layout),
	}
	return aggregate(items)
}

// aggregate 把若干检查项聚合成总报告（三态口径，供 buildAcceptanceReport 与单测共用）。
func aggregate(items []acceptanceItem) acceptanceReport {
	rep := acceptanceReport{Items: items}
	for _, it := range items {
		switch it.State {
		case statePass:
			rep.Pass++
		case stateFail:
			rep.Fail++
		default:
			rep.Unknown++
		}
	}
	switch {
	case rep.Fail > 0:
		rep.Verdict, rep.ExitCode = "fail", 1
	case rep.Unknown > 0:
		rep.Verdict, rep.ExitCode = "unverified", 3
	default:
		rep.Verdict, rep.ExitCode = "pass", 0
	}
	return rep
}

func printAcceptanceReport(rep acceptanceReport, verbose bool) {
	mark := map[checkState]string{statePass: "[PASS]", stateFail: "[FAIL]", stateUnverified: "[UNVERIFIED]"}
	for _, it := range rep.Items {
		fmt.Printf("%-12s %s %s\n", mark[it.State], it.ID, it.Title)
		if it.Detail != "" {
			fmt.Printf("             %s\n", it.Detail)
		}
		if verbose {
			for _, ev := range it.Evidence {
				fmt.Printf("             · %s\n", ev)
			}
		}
	}
	fmt.Printf("\n合计：%d PASS / %d FAIL / %d UNVERIFIED\n", rep.Pass, rep.Fail, rep.Unknown)
	fmt.Printf("结论：%s（退出码 %d）\n", rep.Verdict, rep.ExitCode)
	if rep.Unknown > 0 {
		fmt.Println("提示：unverified 表示证据不足或依赖未满足，按项目铁律「零数据不记 PASS」不得计为通过。")
	}
}

// ── 各条判据 ──────────────────────────────────────────────────

// 要求1：首页只有一个开始按钮。判据：源码级契约（运行期 DOM 由 e2e 覆盖）。
func checkHomeMinimal(layout config.Layout) acceptanceItem {
	it := acceptanceItem{ID: "R1", Title: "极简首页：只有一个开始按钮，无配置项/菜单"}
	src, err := readRepoFile("workbench/internal/web/html.go")
	if err != nil {
		it.State, it.Detail = stateUnverified, "无法读取首页模板源码: "+err.Error()
		return it
	}
	// 只取 indexPage 那一段：同一个文件里还有 adminPage/governancePage，
	// 全文件计数会把管理面板的按钮也算进来（实测 16 个，导致误判 fail）。
	idx := indexPageSection(src)
	if idx == "" {
		it.State, it.Detail = stateFail, "html.go 中找不到 indexPage 模板定义"
		return it
	}
	buttons := strings.Count(idx, "<button")
	hasInput := strings.Contains(idx, "<input") || strings.Contains(idx, "<select") || strings.Contains(idx, "<nav")
	it.Evidence = []string{fmt.Sprintf("indexPage 段内 <button> 出现 %d 次", buttons)}
	if buttons == 1 && !hasInput {
		it.State = statePass
		it.Detail = "首页模板只含 1 个按钮，无 input/select/nav"
	} else {
		it.State = stateFail
		it.Detail = fmt.Sprintf("按钮数=%d，表单/导航=%v", buttons, hasInput)
	}
	return it
}

// 要求2：所有兼容模型接口统一纳管。判据：模型池账本存在且覆盖多条车道。
func checkModelPool(layout config.Layout) acceptanceItem {
	it := acceptanceItem{ID: "R2", Title: "统一模型队列：所有兼容接口纳入中心化调度"}
	poolPath := filepath.Join(layout.DataRoot, "models", "pool.json")
	data, err := os.ReadFile(poolPath)
	if err != nil {
		it.State, it.Detail = stateUnverified, "模型池账本不存在（实例尚未运行过）: "+poolPath
		return it
	}
	s := string(data)
	lanes := map[string]bool{}
	for _, lane := range []string{"\"free\"", "\"kilo\"", "\"eac\"", "\"custom\"", "\"aggregate\""} {
		if strings.Contains(s, "\"lane\": "+lane) || strings.Contains(s, "\"lane\":"+lane) {
			lanes[lane] = true
		}
	}
	specCount := strings.Count(s, "\"id\":")
	it.Evidence = []string{fmt.Sprintf("pool.json 大小 %d 字节，spec 条目约 %d", len(data), specCount)}
	it.Evidence = append(it.Evidence, fmt.Sprintf("覆盖车道 %d 条: %v", len(lanes), keysOf(lanes)))
	if len(lanes) >= 2 && specCount >= 3 {
		it.State = statePass
		it.Detail = fmt.Sprintf("已纳管约 %d 个模型，覆盖 %d 条车道", specCount, len(lanes))
	} else {
		it.State = stateFail
		it.Detail = fmt.Sprintf("模型条目或车道覆盖不足（条目≈%d，车道=%d）", specCount, len(lanes))
	}
	return it
}

// 要求3：轮询/优先级/负载均衡策略。
//
// 判据演进（2026-10-19 现场缺陷 → 2026-10-24 升级为行为级）：
//
// v1 只 grep scheduler.go 里有没有四个策略字符串 —— 而 StrategyLeastLoaded
// 曾与 round-robin 共用同一分支、从不读 inflight，实测「A 在飞 3、C 在飞 0
// 却挑了 A」，grep 判据对此全绿。字符串齐全的副本与真实实现对 grep 同形。
//
// v2 改为直接驱动 Scheduler 施加带区分的场景（见 scheduler_probe.go）：
// 只有真实实现能全绿。原 grep 降级为「枚举齐全」这一条必要非充分判据保留。
func checkSchedulerStrategies(layout config.Layout) acceptanceItem {
	it := acceptanceItem{ID: "R3", Title: "自动轮询调度：优先级/轮询/加权/最少负载"}
	pr := probeSchedulerStrategies()
	it.Evidence = append(it.Evidence, pr.evidence...)
	if len(pr.failures) > 0 {
		it.State = stateFail
		it.Detail = "调度策略行为探针未过: " + strings.Join(pr.failures, "; ")
		return it
	}
	// 探针全绿后再确认配置侧可读（面板能切换才有意义）。
	poolPath := filepath.Join(layout.DataRoot, "models", "model-pool.json")
	if _, err := os.Stat(poolPath); err != nil {
		it.State, it.Detail = stateUnverified, "模型池账本不存在（实例尚未运行过）: "+poolPath
		return it
	}
	it.State = statePass
	it.Detail = "四种调度策略均通过行为探针（least-loaded 读在飞数 / priority 取最高 / round-robin 轮转 / weighted 按权重），可在 /modelpool 控制台切换"
	return it
}

// 要求4/5：实时检测 + 自动故障转移 + 全量留痕。
// 判据：model-status.jsonl 里同时存在 probe/failure/switch 三类记录。
func checkFailoverLog(layout config.Layout) acceptanceItem {
	it := acceptanceItem{ID: "R4R5", Title: "实时检测 + 自动故障转移 + 状态/切换日志"}
	logPath := filepath.Join(layout.DataRoot, "models", "model-status.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		it.State, it.Detail = stateUnverified, "状态日志不存在: "+logPath
		return it
	}
	s := string(data)
	counts := map[string]int{}
	for _, kind := range []string{"probe", "probe-cycle", "failure", "switch", "success", "recover"} {
		n := strings.Count(s, "\"kind\":\""+kind+"\"")
		counts[kind] = n
	}
	it.Evidence = append(it.Evidence, fmt.Sprintf("probe=%d, probe-cycle=%d, failure=%d, switch=%d",
		counts["probe"], counts["probe-cycle"], counts["failure"], counts["switch"]))
	hasProbe := counts["probe"]+counts["probe-cycle"] > 0
	hasSwitch := counts["switch"] > 0
	if hasProbe && hasSwitch {
		it.State = statePass
		it.Detail = fmt.Sprintf("探测 %d 次、切换 %d 次，过程完整留痕",
			counts["probe"]+counts["probe-cycle"], counts["switch"])
	} else if hasProbe {
		it.State = stateUnverified
		it.Detail = "有探测记录但尚无实际切换样本（未触发过故障转移，不能宣称无感切换已实证）"
	} else {
		it.State = stateUnverified
		it.Detail = "状态日志存在但无探测记录（可能是空池或刚启动）"
	}
	return it
}

// 要求6：隐藏式可配置模型管理界面。判据：控制台路由 + 增删改 API 都在源码中。
func checkPoolConsole(layout config.Layout) acceptanceItem {
	it := acceptanceItem{ID: "R6", Title: "隐藏式模型管理界面：增删模型 + 调整优先级"}
	serverSrc, err1 := readRepoFile("workbench/internal/web/server.go")
	apiSrc, err2 := readRepoFile("workbench/internal/web/pool_api.go")
	if err1 != nil || err2 != nil {
		it.State, it.Detail = stateUnverified, "无法读取 web 源码"
		return it
	}
	hasPage := strings.Contains(serverSrc, "\"/modelpool\"")
	hasReorder := strings.Contains(serverSrc, "\"/api/pool/reorder\"")
	hasModels := strings.Contains(serverSrc, "\"/api/pool/models\"")
	hasUpsert := strings.Contains(apiSrc, "Upsert")
	hasDelete := strings.Contains(apiSrc, "MethodDelete")
	it.Evidence = append(it.Evidence,
		fmt.Sprintf("控制台路由=%v, 模型 CRUD=%v, 优先级调整=%v", hasPage, hasModels && hasUpsert && hasDelete, hasReorder))
	if hasPage && hasReorder && hasModels && hasUpsert && hasDelete {
		it.State = statePass
		it.Detail = "/modelpool 控制台支持增删改与优先级排序；首页入口为隐藏式后门"
	} else {
		it.State = stateFail
		it.Detail = "模型管理界面能力不完整"
	}
	return it
}

// 自主1：动态任务规划。判据：目标树存在且带机器可判的验收标准。
func checkAutonomyBootstrap(layout config.Layout) acceptanceItem {
	it := acceptanceItem{ID: "A1", Title: "动态任务规划：基于项目目标推导阶段任务"}
	goalsPath := layout.AutonomyFile("goals.json")
	data, err := os.ReadFile(goalsPath)
	if err != nil {
		it.State, it.Detail = stateUnverified, "目标树不存在（自主子系统未运行过）"
		return it
	}
	s := string(data)
	hasCriteria := strings.Contains(s, "\"criteria\"")
	hasStages := strings.Contains(s, "\"stages\"")
	decPath := layout.AutonomyFile("decisions.jsonl")
	decData, _ := os.ReadFile(decPath)
	planCount := strings.Count(string(decData), "\"kind\":\"plan\"")
	it.Evidence = append(it.Evidence,
		fmt.Sprintf("goals.json %d 字节，decisions.jsonl %d 字节", len(data), len(decData)),
		fmt.Sprintf("plan 决策 %d 条", planCount))
	if hasCriteria && hasStages && planCount > 0 {
		it.State = statePass
		it.Detail = fmt.Sprintf("目标树含机器可判验收标准，已产生 %d 条规划决策", planCount)
	} else if hasCriteria && hasStages {
		it.State = stateUnverified
		it.Detail = "目标树存在但尚无规划决策"
	} else {
		it.State = stateFail
		it.Detail = "目标树结构不完整"
	}
	return it
}

// 自主2：闭环（目标完成 → 自动推导下一目标）。
// 判据：decisions.jsonl 里出现过 goalDone 与 nextGoal。
func checkAutonomyClosedLoop(layout config.Layout) acceptanceItem {
	it := acceptanceItem{ID: "A2", Title: "闭环自主执行：验收通过后自动推导下一目标"}
	decPath := layout.AutonomyFile("decisions.jsonl")
	data, err := os.ReadFile(decPath)
	if err != nil {
		it.State, it.Detail = stateUnverified, "决策台账不存在"
		return it
	}
	s := string(data)
	kinds := map[string]int{}
	for _, k := range []string{"plan", "accept", "rollback", "goalDone", "nextGoal", "block"} {
		kinds[k] = strings.Count(s, "\"kind\":\""+k+"\"")
	}
	it.Evidence = append(it.Evidence, fmt.Sprintf("plan=%d accept=%d rollback=%d goalDone=%d nextGoal=%d block=%d",
		kinds["plan"], kinds["accept"], kinds["rollback"], kinds["goalDone"], kinds["nextGoal"], kinds["block"]))
	// 代码路径自检（离线、可复跑）：把"闭环代码跑得通"与"通道今天是否出力"分开报。
	// 现场背景（2026-10-19）：上游 429 整池冷却时真实运行打不出 goalDone/nextGoal，
	// 但自主闭环的代码路径是通的 —— 只读运行数据会把"通道不给力"误记成"功能没做"。
	st := runAutonomySelfTest(false)
	selfOK := st.Fail == 0 && st.Pass >= 10
	it.Evidence = append(it.Evidence, fmt.Sprintf("离线闭环自检: %d PASS / %d FAIL", st.Pass, st.Fail))
	for _, ln := range st.Lines {
		it.Evidence = append(it.Evidence, ln)
	}

	switch {
	case kinds["goalDone"] > 0 && kinds["nextGoal"] > 0:
		it.State = statePass
		it.Detail = fmt.Sprintf("已完成目标 %d 次、自动推导下一目标 %d 次（真实通道实证）", kinds["goalDone"], kinds["nextGoal"])
	case kinds["accept"] > 0:
		it.State = stateUnverified
		it.Detail = fmt.Sprintf("已有 %d 次自主验收；代码闭环自检 %s，但真实通道尚未出现 goalDone/nextGoal", kinds["accept"], selfCheckWord(selfOK))
	case selfOK:
		it.State = stateUnverified
		it.Detail = "代码闭环自检全绿（规划/验收/回退/下一目标/持久化），但真实通道尚无验收记录（未验证，不记 PASS）"
	default:
		it.State = stateFail
		it.Detail = "代码闭环自检未通过（自主闭环本身有问题）"
	}
	return it
}

// 自主3：固定模板兼容。判据：模板库仍有默认条目。
func checkTemplateCompat(layout config.Layout) acceptanceItem {
	it := acceptanceItem{ID: "A3", Title: "固定模板链路兼容（自主关闭可回落）"}
	// 注意判据口径：内置 24 条模板是**代码内建**（internal/templates/defaults.go），
	// 不落 templates.json —— 该文件只存用户自定义模板，没写过就不存在。
	// 因此判据必须是"源码里的内置模板定义数"，而不是"磁盘上有没有 templates.json"。
	// （改造前判据写成读磁盘文件，导致所有干净实例都误判 unverified。）
	src, err := readRepoFile("workbench/internal/templates/defaults.go")
	if err != nil {
		it.State, it.Detail = stateUnverified, "无法读取内置模板源码: "+err.Error()
		return it
	}
	// 模板条目在源码里形如 mk("content-deep-dive", ...)，共 24 条。
	// 注意不要用 strings.Count(src, "ID:") —— 那只在 mk 闭包的 tpl{ID: ...} 里出现 1 次。
	n := strings.Count(src, "\tmk(")
	userPath := filepath.Join(layout.DataRoot, "templates.json")
	userData, userErr := os.ReadFile(userPath)
	userCount := 0
	if userErr == nil {
		userCount = strings.Count(string(userData), "\"id\":")
	}
	it.Evidence = append(it.Evidence,
		fmt.Sprintf("defaults.go 内建模板条目: %d", n),
		fmt.Sprintf("用户模板文件: %v（%d 条）", userErr == nil, userCount))
	if n >= 20 {
		it.State = statePass
		it.Detail = fmt.Sprintf("内建模板 %d 条 + 用户模板 %d 条（自主关闭后回落此链路）", n, userCount)
	} else {
		it.State = stateFail
		it.Detail = fmt.Sprintf("内建模板条目过少（%d），兼容性存疑", n)
	}
	return it
}

// 自主4（文档部分）：改造方案 + 实现逻辑 + 验证标准。
func checkDesignDocs(layout config.Layout) acceptanceItem {
	it := acceptanceItem{ID: "A4-DOC", Title: "改造方案 / 模块实现逻辑 / 量化验证标准文档"}
	required := []string{
		"docs/自主运行方案.md",
		"docs/六项要求验收矩阵.md",
		"docs/长时压测作业手册.md",
	}
	var missing []string
	for _, rel := range required {
		if _, err := readRepoFile(rel); err != nil {
			missing = append(missing, rel)
		}
	}
	it.Evidence = append(it.Evidence, "要求文档: "+strings.Join(required, ", "))
	if len(missing) == 0 {
		it.State = statePass
		it.Detail = "三份核心文档齐全（方案/矩阵/压测手册）"
	} else {
		it.State = stateFail
		it.Detail = "缺少文档: " + strings.Join(missing, ", ")
	}
	return it
}

// 配置文案完整性：防止「问号化」的配置把自主决策锚点废掉而系统静默继续跑。
//
// 现场缺陷（2026-10-19）：dist 实例 config.json 的 rootTitle/rootIntent 变成
// 一整串 "?"（外部客户端用 ASCII 编码 PUT /api/settings），而这两个字段会被
// planner 原样拼进每个自主任务 Prompt（"项目核心目标：?????????"）。
// 系统不会报错、不会失败，只是从此产出的内容都失去方向 —— 典型的静默失效。
func checkConfigIntegrity(layout config.Layout) acceptanceItem {
	it := acceptanceItem{ID: "CFG", Title: "配置文案完整性：目标意图不得被编码破坏"}
	if _, err := os.Stat(layout.ConfigFile); err != nil {
		it.State, it.Detail = stateUnverified, "配置文件不存在（未运行过）"
		return it
	}
	var cfg config.Config
	if err := config.ReadJSON(layout.ConfigFile, &cfg); err != nil {
		it.State, it.Detail = stateFail, "配置文件无法解析: "+err.Error()
		return it
	}
	beforeTitle, beforeIntent := cfg.Autonomy.Plan.RootTitle, cfg.Autonomy.Plan.RootIntent
	cfg.Normalize()
	it.Evidence = append(it.Evidence,
		fmt.Sprintf("磁盘上 rootTitle=%q", beforeTitle),
		fmt.Sprintf("磁盘上 rootIntent=%.40q", beforeIntent))
	// Normalize 会把损坏文案回落默认；若磁盘值与归一化值不同且磁盘值含问号串 ⇒ 曾被破坏。
	corruptedOnDisk := strings.Contains(beforeTitle, "???") || strings.Contains(beforeIntent, "???")
	repaired := cfg.Autonomy.Plan.RootTitle != beforeTitle || cfg.Autonomy.Plan.RootIntent != beforeIntent
	switch {
	case !corruptedOnDisk:
		it.State = statePass
		it.Detail = "配置文案未被编码破坏"
	case corruptedOnDisk && repaired:
		it.State = stateUnverified
		it.Detail = "磁盘上仍是损坏值，但运行期会回落默认（下次保存即修复）；建议重启一次实例完成磁盘修复"
	default:
		it.State = stateFail
		it.Detail = "配置文案已被编码破坏且无法自动修复（自主任务 Prompt 会注入问号）"
	}
	return it
}

// A7：7x24 长期稳定。判据复用 internal/supervisor 的趋势分析：
//   - 跨度 ≥24h 且 堆内存/句柄都无泄漏趋势  => pass
//   - 任一维度判定泄漏                      => fail
//   - 跨度 <24h 或采样不足                  => unverified（不代表稳定，也不代表不稳定）
func checkSoak(layout config.Layout) acceptanceItem {
	it := acceptanceItem{ID: "A7", Title: "7x24 长期稳定：metrics 覆盖 ≥24h 且无泄漏趋势"}
	csvPath := layout.LogFile("metrics.csv")
	points, skipped, err := supervisor.ReadMetricsCSV(csvPath)
	if err != nil {
		it.State, it.Detail = stateUnverified, "metrics.csv 不可读（未运行过）: "+err.Error()
		return it
	}
	if len(points) < supervisor.MinSoakSamples {
		it.State, it.Detail = stateUnverified, fmt.Sprintf("采样不足（%d 点，至少需 %d）", len(points), supervisor.MinSoakSamples)
		return it
	}
	heapRep := supervisor.AnalyzeTrend(points, "heap", 0.25)
	handlesRep := supervisor.AnalyzeTrend(points, "handles", 0.30)
	spanHours := heapRep.SpanMin / 60.0
	it.Evidence = append(it.Evidence,
		fmt.Sprintf("%s：%d 个采样点（跳过坏行 %d），跨度 %.2f 小时", csvPath, len(points), skipped, spanHours),
		fmt.Sprintf("堆内存趋势 %s（斜率 %.2f MB/小时）", heapRep.Verdict, heapRep.SlopePerHour),
		fmt.Sprintf("句柄趋势 %s（斜率 %.2f 个/小时）", handlesRep.Verdict, handlesRep.SlopePerHour))

	// 判据走唯一实现（soak_verdict.go），与 --soak CLI 同一套四象限。
	// 改造前这里另写一遍 switch，与 soak.go 的实现漂移过（2026-10-19 现场缺陷）。
	verdict, _ := supervisor.SoakVerdict([]supervisor.TrendReport{heapRep, handlesRep})
	it.State = classifySoakVerdict(verdict, spanHours)
	switch it.State {
	case stateFail:
		it.Detail = fmt.Sprintf("检出泄漏趋势（堆 %s / 句柄 %s，跨度 %.2f 小时）", heapRep.Verdict, handlesRep.Verdict, spanHours)
	case statePass:
		it.Detail = fmt.Sprintf("已覆盖 %.2f 小时，堆内存与句柄均无泄漏趋势", spanHours)
	default:
		// unverified 要把「为什么没结论」讲清楚：跨度不够与趋势不可判定是两回事。
		if spanHours >= soakVerdictMinHours {
			it.Detail = fmt.Sprintf("已覆盖 %.2f 小时但趋势未能判定：堆 %s", spanHours, heapRep.Reason)
		} else {
			it.Detail = fmt.Sprintf("采样跨度 %.2f 小时，未覆盖要求的 %.0f 小时（当前不足以宣称已跑满，也不构成失败）", spanHours, soakVerdictMinHours)
		}
	}
	return it
}

// ── 工具函数 ──────────────────────────────────────────────────

// repoRoot 返回源码根（工作台从 dist 或源码目录运行都能找到文档/源码）。
//
// 判据用「docs/自主运行方案.md + workbench/internal/web/html.go」双命中：
// 只看 docs 会在 dist/ 目录误判（dist 下没有 docs），只看 workbench 又会在
// 单独拷贝的命令目录误判。两个同时命中才是真正的仓库根。
func repoRoot() string {
	if v := strings.TrimSpace(os.Getenv("WB_REPO_ROOT")); v != "" {
		return v
	}
	candidates := make([]string, 0, 8)
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for i := 0; i < 5; i++ {
			candidates = append(candidates, dir)
			dir = filepath.Dir(dir)
		}
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
		// 从 cwd 往上找
		dir := wd
		for i := 0; i < 5; i++ {
			dir = filepath.Dir(dir)
			candidates = append(candidates, dir)
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "docs", "自主运行方案.md")); err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(c, "workbench", "internal", "web", "html.go")); err != nil {
			continue
		}
		return c
	}
	// 未命中返回空串（而不是退回 cwd/「.」）。
	//
	// 现场（2026-10-19 Docker 干净容器实测）：真实部署形态是「只有 EXE、没有源码树」，
	// 改造前这里退回 cwd，于是基于源码的判据（R1/R3/R6/A3/A4-DOC）全都去读一个
	// 不存在的路径 ⇒ 报 FAIL。那不是"要求不满足"，是"这项判据在当前形态下取不到数" ——
	// 按项目铁律应当记 UNVERIFIED。空串让调用方能把这两种情况分开。
	return ""
}

// readRepoFile 读取仓库相对路径的文件。
func readRepoFile(rel string) (string, error) {
	root := repoRoot()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// indexPageSection 截取 html.go 里 indexPage 模板常量的正文（到下一个 const 为止）。
func indexPageSection(src string) string {
	marker := "const indexPage = " + "`"
	i := strings.Index(src, marker)
	if i < 0 {
		return ""
	}
	rest := src[i+len(marker):]
	if j := strings.Index(rest, "`\n\nconst "); j >= 0 {
		return rest[:j]
	}
	if j := strings.Index(rest, "`"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// selfCheckWord 把自检结果翻成短语（报告可读）。
func selfCheckWord(ok bool) string {
	if ok {
		return "全绿"
	}
	return "有问题"
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
