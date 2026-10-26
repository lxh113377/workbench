// Command workbench 是「个人 AI 自动化工作台」的唯一入口。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"workbench/internal/autonomy"
	"workbench/internal/browser"
	"workbench/internal/config"
	"workbench/internal/dshprofile"
	"workbench/internal/engine"
	"workbench/internal/executor"
	"workbench/internal/gov"
	"workbench/internal/logging"
	"workbench/internal/model"
	"workbench/internal/modelpool"
	"workbench/internal/shortcut"
	"workbench/internal/single"
	"workbench/internal/store"
	"workbench/internal/supervisor"
	"workbench/internal/templates"
	"workbench/internal/tray"
	"workbench/internal/web"
	"workbench/internal/workspace"
)

// 这些变量在构建时由 -ldflags -X 注入。
var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-v":
			fmt.Println("workbench " + version + " (" + buildTime + ")")
			return
		case "--diagnose":
			os.Exit(runDiagnoseCLI())
		case "--status":
			os.Exit(runStatusCLI())
		case "--stop":
			os.Exit(runStopCLI())
		case "--quit":
			// 整体退出（2026-10-19 现场补齐）：产品此前**只能靠 taskkill 强杀** ——
			// tray 的「退出工作台」要 UI 自动化，signal 对 GUI 进程收不到，--stop 只停引擎。
			// 这条命令走与信号处理同一条优雅路径，让运维/自动化能无人工地让实例整体停下。
			os.Exit(runQuitCLI())
		case "--setup":
			os.Exit(runSetupCLI())
		case "--governance", "--gov":
			os.Exit(runGovernanceCLI(args[1:]))
		case "--workspace", "--ws":
			os.Exit(runWorkspaceCLI(args[1:]))
		case "--selftest-workspace":
			dir := ""
			if len(args) > 1 {
				dir = args[1]
			}
			os.Exit(runWorkspaceSelftest(dir))
		case "--autonomy", "--auto":
			os.Exit(runAutonomyCLI(args[1:]))
		case "--plan-selftest":
			// 双角色闭环的离线自检（plan_selftest.go）。
			//
			// 与 --autonomy selftest 同一水准：不依赖真实模型、不消耗额度，
			// 用桩执行器把"方案 → 执行 → 验收回灌"整条链路真实跑一遍。
			// 没有这条通道时，闭环是否还通只能靠跑真模型验证 —— 那意味着没人会天天验。
			os.Exit(runPlanSelftest())
		case "--model-pool", "--pool":
			os.Exit(runModelPoolCLI(args[1:]))
		case "--soak":
			os.Exit(runSoakCLI(args[1:]))
		case "--acceptance", "--ac":
			os.Exit(runAcceptanceCLI(args[1:]))
		case "--ui-probe":
			// 前端验收的确定性通道（ui_probe.go）。
			//
			// 事故（2026-10-19 19:17 实测，且 20:10 又复现一次）：这一格曾在并发改写
			// main.go 时**整段消失**，源码能编译、测试全绿，但主入口二进制跑 --ui-probe
			// 会**静默回落**成 GUI 启动（打印「工作台已在运行」后退出 0）。
			// 前端门禁因此拿不到确定性通道，整轮读数变成 unverified ——
			// 属于「没测成被当成没问题」，比断言失败更危险。
			// 回落路径由 TestMainDispatchKeepsUIProbeFlag 用真实二进制守住（见 main_dispatch_test.go）。
			os.Exit(runUIProbeCLI(args[1:]))
		case "--prune":
			os.Exit(runPruneCLI(args[1:]))
		case "--export-all":
			dir := ""
			if len(args) > 1 {
				dir = args[1]
			}
			os.Exit(runExportCLI(dir))
		}
	}
	if err := runApp(args); err != nil {
		fmt.Fprintln(os.Stderr, "workbench: "+err.Error())
		os.Exit(1)
	}
}

func runApp(args []string) error {
	silent := false
	for _, a := range args {
		switch a {
		case "--silent":
			silent = true
		case "--headless":
			// --headless 是 --silent 的显式子集（2026-10-21 老大指令「无头执行」）：
			// 两者都"不弹面板"，区别在语义来源 —— --silent 表达"服务化启动"，
			// --headless 表达"本次运行根本不该出现任何窗口"。
			// 保留两个名字是因为既有计划任务/守护脚本已经用 --silent 在跑，
			// 改名会静默失效（历史教训：快捷方式参数改动没同步 = 永远起不来）。
			silent = true
		}
	}

	layout, err := config.ResolveLayout("")
	if err != nil {
		return err
	}
	if err := layout.EnsureDirs(); err != nil {
		return err
	}

	// 文件独占锁 = 跨会话可靠的单实例（命名互斥体在 WMI/不同会话下不可靠）
	lock, alreadyRunning, err := single.Acquire(filepath.Join(layout.RuntimeDir, "workbench.lock"))
	if err != nil {
		return err
	}
	if alreadyRunning {
		// 面板地址只能用**精确**的实例自报地址来定位，不能用端口顺延扫描。
		//
		// 现场（2026-10-19 老大：快捷方式打开显示「重试」）：
		//   原实现 ProbeRunning(host, basePort, 10) 从 basePort 起连扫 10 个端口，
		//   任何监听成功的端口都被当成「工作台面板」。实测 17890 是主实例、
		//   17891 是压测副本（wbsoak26.exe），于是二次启动把浏览器指向了副本；
		//   副本一死 URL 成死端口，页面 fetch 全失败 -> 按钮落进 error 态「重试」。
		//   语义错误在于：端口号相邻 ≠ 同一实例。
		//
		// 现在只认两处权威来源：实例自写的 runtime/panel.url；其次配置里的精确端口。
		//
		// --silent 必须覆盖本分支（2026-10-19 实测事故）：守护进程/压测/自愈都会用
		// --silent 拉起自身，一旦它们撞上"已在运行"，原实现仍然会往桌面弹一个面板窗口 ——
		// 服务化启动污染桌面，正是老大反复反馈"一直在跳这个"的来源之一。
		if url := resolveRunningPanelURL(layout); url != "" {
			if shouldOpenPanel(silent) {
				// 先声明意图再动手：输出即契约，黑盒测试据此判定
				// 「服务化启动有没有污染桌面」，不依赖浏览器是否真的起来。
				fmt.Println(panelOpenedMarker + url)
				openPanel(url)
			}
			fmt.Println("工作台已在运行：" + url)
			return nil
		}
		preCfg, _ := config.NewStore(layout.ConfigFile)
		basePort := 17890
		if preCfg != nil {
			basePort = preCfg.Get().Server.Port
		}
		if p, ok := single.ProbeExactPort("127.0.0.1", basePort); ok {
			url := fmt.Sprintf("http://127.0.0.1:%d/", p)
			if shouldOpenPanel(silent) {
				fmt.Println(panelOpenedMarker + url)
				openPanel(url)
			}
			fmt.Println("工作台已在运行：" + url)
		} else {
			fmt.Println("工作台已在运行，但未能定位面板地址（panel.url 缺失且精确端口不通）")
		}
		return nil
	}
	defer lock.Release()

	cstore, err := config.NewStore(layout.ConfigFile)
	if err != nil {
		return err
	}
	reg, err := templates.Open(filepath.Join(layout.DataRoot, "templates.json"))
	if err != nil {
		return err
	}
	st, err := store.Open(layout)
	if err != nil {
		return err
	}
	// 关闭 WAL 文件句柄（P2-3）：退出前先 Savepoint 压缩，再由 engine.Stop 负责落盘。
	defer func() { _ = st.Close() }()
	// 结构化日志（P1-1）：统一落 logs/workbench.jsonl，GUI 模式不写终端。
	logger, logErr := logging.Open(logging.Options{
		Dir:     layout.LogsDir,
		Level:   logging.LevelInfo,
		Console: !silent,
	})
	if logErr != nil {
		logger = logging.Nop()
	}
	defer func() { _ = logger.Close() }()
	hub := web.NewHub(300)
	eng, err := engine.New(layout, cstore, st, reg, hub)
	if err != nil {
		return err
	}
	eng.SetLogger(logger)
	eng.ReloadConfig()

	// 模型池：把 OFM 渠道网关清单、实时探测、轮询调度与故障转移装配起来。
	// 池子是「可选增强」：任何一步失败都只告警，不阻断工作台启动。
	poolStore := modelpool.NewStore(layout.DataRoot)
	poolSvc := modelpool.NewService(poolStore, nil, modelpool.ServiceConfig{
		Enabled:      true,
		ProbeSeconds: cstore.Get().Pool.ProbeSeconds,
		SyncSeconds:  cstore.Get().Pool.SyncSeconds,
		Strategy:     modelpool.Strategy(cstore.Get().Pool.Strategy),
		DeepProbe:    cstore.Get().Pool.DeepProbe,
		DSHHome:      layout.DSHHome,
	})
	poolSvc.SetRouteSink(func(routes []config.ModelRoute) {
		if len(routes) == 0 {
			return
		}
		eng.ReloadModelRoutes(routes)
	})
	// 配置热更新时也以池子为准（否则每次保存设置都会把路由表冲回静态 models）。
	eng.SetPoolRoutes(func() []config.ModelRoute { return poolSvc.Manager().Routes() })
	// 用故障转移执行器包住真实执行器：一次尝试内换模型，用户无感。
	if inner, ok := eng.Runner().(*executor.Runner); ok {
		eng.SetRunner(modelpool.NewFailoverRunner(inner, poolSvc.Manager(), poolSvc.Scheduler()))
	}
	// 限流时暂停模型探测：冷却期间继续探测 25 个模型会自己制造流量，
	// 让 429 窗口无限延长（2026-10-19 现场实测）。engine 不 import modelpool，
	// 用回调解耦。
	eng.SetProbeSuppressor(func(seconds int, reason string) {
		poolSvc.SuppressProbing(seconds, reason)
	})
	poolSvc.Start(context.Background())
	defer poolSvc.Stop()
	hub.Emit(model.Event{Type: "ready", At: model.Now(), Msg: "模型池已启动", Data: map[string]any{"dir": poolStore.Dir()}})

	profileInfo, profErr := dshprofile.Bootstrap(layout)
	if profErr != nil {
		hub.Emit(model.Event{Type: "error", At: model.Now(), Msg: "执行通道探测失败: " + profErr.Error()})
	}

	srv := web.NewServer(layout, cstore, eng, hub)
	srv.SetModelPool(poolSvc)
	if err := srv.Listen(); err != nil {
		return err
	}
	defer srv.Close()

	if err := eng.Recover(); err != nil {
		hub.Emit(model.Event{Type: "error", At: model.Now(), Msg: "恢复状态失败: " + err.Error()})
	}

	// 优雅退出的唯一实现（2026-10-19）：信号与 /api/engine/quit 共用这一段，
	// 避免两条退出路径各自为政（例如一条落盘相位、另一条不落）。
	quitOnce := sync.Once{}
	gracefulQuit := func() {
		quitOnce.Do(func() {
			// 退出照常执行（不能因为落盘失败就把用户困在进程里），但失败必须留痕：
			// 相位没落上时，外部守护可能把「用户主动退出」当成异常停机再拉起。
			if err := eng.Stop(); err != nil {
				logger.At("quit").Warn("停机时状态落盘失败", "err", err.Error())
				hub.Emit(model.Event{Type: "warn", At: model.Now(), Msg: "停机时状态落盘失败: " + err.Error()})
			}
			srv.Close()
			os.Exit(0)
		})
	}
	// quitByUser 是「用户主动退出」的**唯一实现**（托盘「退出工作台」走这里）。
	//
	// 为什么必须落标记（2026-10-19 现场缺陷：托盘点「退出工作台」关不掉）：
	//
	//	gracefulQuit 只做 Stop→Close→Exit，**不表达「这是用户主动退出」**；
	//	而外部守护（tests/keepalive.ps1 + 计划任务 WorkBenchKeepAlive，每分钟一次）
	//	只看面板 data-state=stopped/idle 就判定「该拉起来」，于是：
	//	  点退出 → 进程结束、相位落 stopped → ≤60s 被重新拉起
	//	  → 新实例 POST /api/engine/start → 回到 running
	//	用户观感 = "关不掉"，且 7x24 语义被反转成"用户无权停机"。
	//
	// MarkUserQuit 落两个消费者都能读到的证据：state.json 的 userQuit 字段
	// 与 runtime/user-quit.flag；守护据此跳过拉起。清除点 = Engine.Start()
	// （用户点「启动引擎」/面板「开始」= 恢复运行 = 恢复正常守护语义）。
	//
	// 面板 /api/engine/quit 已走同一处理（internal/web/server.go handleEngineQuit），
	// 本条补齐托盘这条**不经 HTTP** 的入口。
	quitByUser := func() {
		if err := eng.MarkUserQuit(); err != nil {
			logger.At("tray").Warn("记录用户主动退出标记失败", "err", err)
			hub.Emit(model.Event{Type: "warn", At: model.Now(), Msg: "记录用户主动退出标记失败: " + err.Error()})
		}
		gracefulQuit()
	}
	stopSignal := make(chan os.Signal, 2)
	signal.Notify(stopSignal, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stopSignal
		gracefulQuit()
	}()
	// 让面板也能触发整体退出（--quit CLI 就是走这条路）。
	srv.SetQuitHook(gracefulQuit)

	state := st.State()
	cfg := cstore.Get()
	// 接续判据（2026-10-19 现场缺陷修正）：
	//
	// 原判据只看 (running|paused)，但**自愈停机会把 phase 落成 stopped** ——
	// 于是「资源自愈」重启后引擎永不恢复，7x24 只重启了壳。见 engine.AbortForSelfHeal。
	//
	// 现在加入 explicit 的 ResumeAfterRestart 标记（仅被动中断时置位）：
	//   - running/paused        : 进程被杀/断电等，恢复运行
	//   - resumeAfterRestart    : 自愈触发的重启，必须接续
	//   - 其它（含用户主动 stop）: 尊重用户意愿，保持停止
	shouldAutoStart := shouldAutoStart(cfg, state.Phase, state.ResumeAfterRestart)
	if shouldAutoStart {
		if err := eng.Start(); err != nil {
			hub.Emit(model.Event{Type: "error", At: model.Now(), Msg: "自动恢复运行失败: " + err.Error()})
		}
	}

	ensureShortcuts(layout, hub)

	// 资源治理与自愈：内存/句柄/goroutine 超阈值时自重启，保证 7x24 稳定
	sup := supervisor.New(layout, func() config.SupervisorConfig { return cstore.Get().Supervisor })
	sup.SetLogger(logger)
	srv.SetSupervisor(sup)
	sup.OnHeal(func(reason string) {
		hub.Emit(model.Event{Type: "warn", At: model.Now(), Msg: "资源自愈触发: " + reason})
		// 用 AbortForSelfHeal 而非 Stop：前者会落 ResumeAfterRestart 标记，
		// 保证重启后引擎真的接续干活（否则 7x24 只是重启了壳）。
		// 上报但不阻断：state 没落上仍要继续自愈重启（不重启才是 7×24 事故），
		// 但必须说出来 —— 否则运维以为 resumeAfterRestart 标记已经写上，实际新进程读的是旧 state。
		if err := eng.AbortForSelfHeal(reason); err != nil {
			logger.At("heal").Warn("自愈停机时状态落盘失败（仍将重启）", "err", err.Error())
			hub.Emit(model.Event{Type: "warn", At: model.Now(), Msg: "自愈停机时状态落盘失败（仍将重启）: " + err.Error()})
		}
		if err := sup.SelfHeal(reason); err != nil {
			hub.Emit(model.Event{Type: "error", At: model.Now(), Msg: "自愈重启失败: " + err.Error()})
			return
		}
		os.Exit(0)
	})
	sup.Start()
	defer sup.Stop()

	// 治理监控（契约 × 体量）：采样 → 预警/调整/联动 → 落盘。
	// 治理为 advisory 子系统：初始化失败只告警，不阻断主工作台（体量看守不得成为任务的闸门）。
	var govEngine *gov.Engine
	if govStore, gerr := gov.Open(layout.GovDir); gerr != nil {
		hub.Emit(model.Event{Type: "warn", At: model.Now(), Msg: "治理子系统初始化失败: " + gerr.Error()})
	} else {
		govEngine = gov.NewEngine(layout, govStore, eng, sup, func() config.GovConfig { return cstore.Get().Gov })
		if cstore.Get().Gov.Enabled {
			govEngine.Start()
			defer govEngine.Stop()
		}
		srv.SetGovernance(govEngine)
		hub.Emit(model.Event{Type: "ready", At: model.Now(), Msg: "治理监控已就绪", Data: map[string]any{"dir": layout.GovDir}})
	}
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for range t.C {
			// metrics.csv 是「趋势是否单调上升（泄漏）」判据的唯一数据源：这里静默失败会让下一轮
			// trend 判读看到「无新增样本」而误报健康 —— 比写盘本身更坏的正是「没写成功却看不出来」。
			// 仍不中断 ticker（下次继续写），但必须留下日志与事件。
			if err := sup.WriteCSV(layout.LogFile("metrics.csv")); err != nil {
				logger.At("metrics").Warn("指标采样落盘失败（趋势判据将缺样本）", "err", err.Error())
				hub.Emit(model.Event{Type: "warn", At: model.Now(), Msg: "指标采样落盘失败: " + err.Error()})
			}
		}
	}()

	hub.Emit(model.Event{Type: "ready", At: model.Now(), Msg: "工作台已就绪", Data: map[string]any{
		"url": srv.URL(), "port": srv.Port(), "version": version, "profile": profileInfo, "profileError": errText(profErr),
	}})

	if !silent {
		openPanel(srv.URL())
	}

	// reportEngineOp 上报托盘触发的引擎操作失败：写日志 + 发事件流（与 OnWarn 同一条通道）。
	reportEngineOp := func(op string, err error) {
		if err == nil {
			return
		}
		logger.At("tray").Warn("托盘"+op+"引擎失败", "err", err.Error())
		hub.Emit(model.Event{Type: "warn", At: model.Now(), Msg: "托盘" + op + "引擎失败: " + err.Error()})
	}

	trayDone := make(chan struct{})
	go func() {
		defer close(trayDone)
		tray.Run(tray.Options{
			Title:   "个人 AI 工作台",
			Tooltip: "个人 AI 工作台",
			Status:  func() string { return string(eng.Phase()) },
			// 托盘降级必须可见（2026-10-19 老大："状态栏也没有图标"）：注册失败时把
			// 原因写进日志与事件流，而不是静默少一个图标让人无从排查。
			OnWarn: func(msg string) {
				logger.At("tray").Warn(msg)
				hub.Emit(model.Event{Type: "warn", At: model.Now(), Msg: msg})
			},
			OnOpen: func() { openPanel(srv.URL()) },
			// 托盘是不经 HTTP 的入口。面板那条路径（handleEnginePause/Resume）会判错并回非 2xx；
			// 这里原先一律 _ = 吞掉 ⇒ 同一操作成了「一处报错、一处静默」。引擎侧已改为落盘失败即报错，
			// 故这里必须把失败说出来（只上报、不弹窗：托盘是可选增强，不得打断主流程）。
			OnStart:  func() { reportEngineOp("启动", eng.Start()) },
			OnPause:  func() { reportEngineOp("暂停", eng.Pause()) },
			OnResume: func() { reportEngineOp("恢复", eng.Resume()) },
			OnStop:   func() { reportEngineOp("停止", eng.Stop()) },
			// 「退出工作台」必须走 quitByUser（用户主动退出），不得裸调 gracefulQuit。
			//
			// 托盘是**不经 HTTP** 的退出入口：gracefulQuit 只落 stopped、不表达用户意图，
			// 于是外部守护（tests/keepalive.ps1，每分钟一次）看到 stopped 就把实例重新拉起。
			// 2026-10-19 现场实测：点「退出工作台」后 ≤60s 被拉起并自动 start，
			// 70s 后又被拉起一次 —— 用户观感「关不掉」。见 quitByUser 注释。
			OnQuit: quitByUser,
		})
	}()
	<-trayDone
	return nil
}

// runWorkspaceCLI 是「AI 开发工作目录」的命令行出口（无人工点击也能配置）。
//
// 用法：
//
//	--workspace status                  打印当前模式/根/审计摘要
//	--workspace validate <dir>          只校验目录（不切换）
//	--workspace select <dir>            校验并切换为外部目录模式（写 config.json）
//	--workspace reset                   切回项目模式
//
// 退出码：0 成功 / 1 失败 / 2 参数错误。
func runWorkspaceCLI(args []string) int {
	layout, lerr := config.ResolveLayout("")
	if lerr != nil {
		fmt.Fprintln(os.Stderr, lerr)
		return 1
	}
	if err := layout.EnsureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cstore, err := config.NewStore(layout.ConfigFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取配置失败: "+err.Error())
		return 1
	}
	cfg := cstore.Get()
	mgr := workspace.New(layout.DataRoot, layout.Root, cfg.Workspace)
	audit, _ := workspace.OpenAudit(layout.WorkspaceAuditFile)
	mgr.SetAudit(audit)

	cmd := "status"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "status":
		snap := mgr.Snapshot()
		out, _ := json.MarshalIndent(map[string]any{
			"mode": snap.Mode, "root": snap.Root, "dataRoot": snap.DataRoot,
			"validation": snap.Validation, "entries": snap.Entries,
			"audit": snap.Audit, "auditPath": snap.AuditPath,
		}, "", "  ")
		fmt.Println(string(out))
		return 0
	case "validate":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: --workspace validate <绝对目录>")
			return 2
		}
		v, verr := mgr.Validate(args[1])
		out, _ := json.MarshalIndent(map[string]any{"ok": verr == nil, "validation": v, "error": errText(verr)}, "", "  ")
		fmt.Println(string(out))
		if verr != nil {
			return 1
		}
		return 0
	case "select":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: --workspace select <绝对目录>")
			return 2
		}
		v, verr := mgr.Validate(args[1])
		if verr != nil {
			out, _ := json.MarshalIndent(map[string]any{"ok": false, "validation": v, "error": verr.Error()}, "", "  ")
			fmt.Println(string(out))
			return 1
		}
		if _, uerr := cstore.Update(func(c *config.Config) {
			c.Workspace.Mode = workspace.ModeExternal
			c.Workspace.Root = v.Canonical
			c.Workspace.Since = model.Now()
		}); uerr != nil {
			fmt.Fprintln(os.Stderr, "写配置失败: "+uerr.Error())
			return 1
		}
		fmt.Println("已切换工作目录: " + v.Canonical)
		return 0
	case "reset":
		if _, uerr := cstore.Update(func(c *config.Config) {
			c.Workspace.Mode = workspace.ModeProject
			c.Workspace.Since = model.Now()
		}); uerr != nil {
			fmt.Fprintln(os.Stderr, "写配置失败: "+uerr.Error())
			return 1
		}
		fmt.Println("已切回项目模式")
		return 0
	default:
		fmt.Fprintln(os.Stderr, "未知子命令: "+cmd+"（可用: status/validate/select/reset）")
		return 2
	}
}

// runWorkspaceSelftest 在指定目录跑一遍"产物落盘 + 真实验收"链路（离线，不需要模型）。
//
// 用途：当模型通道被限流（429）时，仍可证明"文件创建→写入用户目录→执行验证"的
// 代码通路可用；也可作为部署后的冒烟自检。
func runWorkspaceSelftest(dir string) int {
	layout, err := config.ResolveLayout("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := layout.EnsureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cstore, err := config.NewStore(layout.ConfigFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取配置失败: "+err.Error())
		return 1
	}
	reg, err := templates.Open(filepath.Join(layout.DataRoot, "templates.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开模板库失败: "+err.Error())
		return 1
	}
	st, err := store.Open(layout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开存储失败: "+err.Error())
		return 1
	}
	defer st.Close()
	eng, err := engine.New(layout, cstore, st, reg, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "构造引擎失败: "+err.Error())
		return 1
	}
	if strings.TrimSpace(dir) != "" {
		if _, verr := eng.SelectWorkspaceDir(dir); verr != nil {
			fmt.Fprintln(os.Stderr, "工作目录校验失败: "+verr.Error())
			return 1
		}
	}
	rep, info := eng.SelfTestArtifactPipeline(eng.Workspace().Root())
	out, _ := json.MarshalIndent(info, "", "  ")
	fmt.Println(string(out))
	if rep == nil || !rep.Pass {
		fmt.Println("[verify] FAIL")
		return 1
	}
	fmt.Println("[verify] PASS")
	return 0
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// panelOpenedMarker 是「已把面板推到桌面」的输出标记。
//
// 为什么要一行输出（2026-10-19 现场）：--silent 的语义是"服务化启动，不弹面板"，
// 但旧实现只在**首次启动**分支尊重它，"已在运行"分支无条件弹窗 —— 结果是守护/
// 压测/自愈在桌面上反复弹面板。这类缺陷靠"看代码"极易漏，必须能被黑盒判据抓住：
// 约定凡是**真的要弹面板**就先打这一行，测试据此断言 --silent 下不出现。
const panelOpenedMarker = "已打开面板："

// shouldOpenPanel 判断本次启动要不要把面板推到桌面。
//
// 唯一授权来源 = 没带 --silent。两处调用点都必须经它，禁止各自判断
// （历史上正是"一处判断、另一处漏判"造成服务化启动弹窗）。
func shouldOpenPanel(silent bool) bool { return !silent }

func openPanel(url string) {
	// 无头开关兜底（2026-10-21 老大指令「不要给我跳出来窗口，无头执行」）：
	// 即便调用点漏判 silent，只要环境要求无头，就**绝不**拉起浏览器。
	// 双层防护的理由：shouldOpenPanel 是"代码里的决定"，环境变量是"运行期的强制"，
	// 后者覆盖前者 —— 测试/CI/无人值守不该因为某处分支漏判而污染桌面。
	if headlessEnv() {
		return
	}
	if err := browser.Open(url); err != nil {
		fmt.Println("请手动打开：" + url)
	}
}

// resolveRunningPanelURL 返回「可以放心交给浏览器」的面板地址；没有则返回空串。
//
// 权威来源只有一处：实例自写的 runtime/panel.url。
//
// 为什么必须校验存活（2026-10-19 现场）：残留的 panel.url 会指向已经死掉的端口，
// 直接把老大送进「页面 fetch 全失败 -> 按钮落 error 态」的死页。
// 因此这里要求地址**当前真的能 2xx 打开**，否则视为不可用、返回空串，
// 让调用方退回到「精确端口探测」这条更保守的路径。
func resolveRunningPanelURL(layout config.Layout) string {
	url := readRunningURL(layout)
	if url == "" {
		return ""
	}
	if !panelAlive(url) {
		return "" // 写入者已死，盘上的地址不可信
	}
	return url
}

// readRunningURL 从 runtime 目录读取当前实例写入的面板地址。
func readRunningURL(layout config.Layout) string {
	data, err := os.ReadFile(filepath.Join(layout.RuntimeDir, "panel.url"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func ensureShortcuts(layout config.Layout, hub *web.Hub) {
	// 守卫（2026-10-19 现场）：只有正式安装位置的实例才允许写快捷方式。
	// 并发会话跑隔离 e2e 时，副本实例（temp\wb-ws-e2e-*\wbws.exe）曾把桌面
	// 「个人AI工作台」改写到临时目录；这里在调用侧再挡一道，双重保险。
	if !shortcut.Guard() {
		return
	}
	// 幂等性从"跑过一次就永远跳过"改成"每次启动都校验并修复"。
	//
	// 为什么改（2026-10-19 现场）：原实现用 runtime\shortcut.ok 做一次性标记，
	// 于是当年写坏的快捷方式（带着 --silent 参数）**永远不会被自动纠正** ——
	// 老大双击后 app 在后台跑起来但桌面无窗口，看起来就是"点不开"，
	// 而且重启多少次都不会自己好。现在每次启动都让 Create() 校验并重写一遍
	// （Create 自身带 TargetPath + Arguments 回读校验），代价是一次短命的 powershell 调用。
	if err := shortcut.Create(); err != nil {
		hub.Emit(model.Event{Type: "warn", At: model.Now(), Msg: "创建快捷方式失败: " + err.Error()})
		return
	}
	marker := filepath.Join(layout.RuntimeDir, "shortcut.ok")
	_ = os.WriteFile(marker, []byte(time.Now().Format(time.RFC3339)), 0o644)
}

// ── CLI 子命令 ─────────────────────────────────────────────────

func runDiagnoseCLI() int {
	layout, err := config.ResolveLayout("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	info, profErr := dshprofile.Bootstrap(layout)
	out := map[string]any{"version": version, "buildTime": buildTime, "layout": layout, "profile": info}
	if profErr != nil {
		out["profileError"] = profErr.Error()
	}
	if cstore, err := config.NewStore(layout.ConfigFile); err == nil {
		out["config"] = cstore.Get()
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	if profErr != nil {
		return 1
	}
	return 0
}

func runStatusCLI() int {
	layout, err := config.ResolveLayout("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	st, err := store.Open(layout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = st.Close() }()
	state := st.State()
	tasks := st.List()
	done, failed := 0, 0
	for _, t := range tasks {
		switch t.State {
		case model.StateDone:
			done++
		case model.StateFailed, model.StateTimeout:
			failed++
		}
	}
	out := map[string]any{"phase": state.Phase, "total": len(tasks), "done": done, "failed": failed, "updatedAt": state.UpdatedAt, "url": readRunningURL(layout)}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	return 0
}

func runStopCLI() int {
	layout, err := config.ResolveLayout("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// 跨进程盲写的风险（2026-10-19 现场缺陷）：
	//
	// 本命令在**另一个进程**里自开 store，只能看到磁盘快照，看不到运行中实例的内存相位。
	// 改造前它无条件把 stopped 写盘 —— 于是内存仍在 running 时，磁盘被写坏成 stopped，
	// 且因为 Engine.Start() 幂等分支不落盘，这个分歧**永久化**。
	// 后果：重启后的接续判据 shouldAutoStart 读磁盘 => 误判为「用户停过」=> 不自启 => 7x24 断链。
	//
	// 修法：写盘前先看面板/进程是否仍活着。
	//   活着的实例 => 磁盘相位代表不了内存，**只发停机请求、不写 phase**；
	//   无实例      => 才是真正的停机，可安全落盘 stopped。
	alive := panelAlive(readRunningURL(layout))
	if alive {
		// 仍在运行：交给运行中的实例自己优雅停机（它会正确落盘）。
		if err := requestPanelStop(layout); err != nil {
			fmt.Println("已请求停机，但面板未确认：" + err.Error())
			return 1
		}
		fmt.Println("已请求运行中的工作台优雅停机（未改写磁盘相位，避免与内存分歧）。")
		return 0
	}
	st, err := store.Open(layout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = st.Close() }()
	state := st.State()
	state.Phase = model.PhaseStopped
	state.StoppedAt = model.Now()
	if err := st.SaveState(state); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// 2026-10-24 round 89：Savepoint 失败不再忽略。它是重启后的恢复点 ——
	// 写失败时上面那句「已标记停止」就是虚假成功反馈：用户以为停成了，
	// 重启后接续判读到的却是旧状态（与 SaveState 同属「停机器」这件事的两半）。
	// 判据：失败是否会让已打印的成功消息变成假话；会 => 必须判，不能 _ = 忽略。
	if err := st.Savepoint(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("已标记停止；若进程仍在运行，请通过面板或托盘退出。")
	return 0
}

// runQuitCLI 让运行中的实例**整体退出**（进程结束，不只是引擎停）。
//
// 为什么需要（2026-10-19 现场缺陷）：
//
//	产品此前**没有任何程序化退出入口**：
//	  - tray 菜单「退出工作台」是 WM_COMMAND 1006，需要 UI 自动化；
//	  - signal.Notify(os.Interrupt/SIGTERM) 对无控制台的 GUI 进程收不到；
//	  - --stop 只停引擎、不停进程（那是它的正确语义，但换二进制时不够用）。
//	=> 运维/自动化想重启实例只能 taskkill 强杀，违反「零人工干预」。
//
// 实现方式：走与 main() 里信号处理**同一条**优雅路径 ——
//
//	面板发 POST /api/engine/quit，运行中的实例收到后自行 eng.Stop -> srv.Close -> 退出。
//	自己不擅自写磁盘相位（相位由**运行中的实例**在优雅退出时落盘，语义唯一）。
//
// 退出码：0 = 已请求（实例会自己退出）/ 1 = 无实例或请求失败。
func runQuitCLI() int {
	layout, err := config.ResolveLayout("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !panelAlive(readRunningURL(layout)) {
		fmt.Println("没有运行中的实例（面板不可达），无需退出。")
		return 1
	}
	if err := requestPanelQuit(layout); err != nil {
		fmt.Println("已请求退出，但面板未确认：" + err.Error())
		return 1
	}
	fmt.Println("已请求运行中的工作台整体退出（优雅路径，相位由实例自行落盘）。")
	return 0
}

// requestPanelQuit 请求运行中的实例整体退出。
func requestPanelQuit(layout config.Layout) error {
	url := readRunningURL(layout)
	if url == "" {
		return fmt.Errorf("未能定位面板地址")
	}
	page, err := httpGet(url)
	if err != nil {
		return err
	}
	token := extractWBTokens(page)
	if token == "" {
		return fmt.Errorf("页面未暴露 token")
	}
	return httpPostJSON(strings.TrimRight(url, "/")+"/api/engine/quit", token, map[string]any{})
}

// requestPanelStop 让运行中的实例优雅停机（面板 /api/engine/stop，需要实例自己的 token）。
func requestPanelStop(layout config.Layout) error {
	url := readRunningURL(layout)
	if url == "" {
		return fmt.Errorf("未能定位面板地址")
	}
	page, err := httpGet(url)
	if err != nil {
		return err
	}
	token := extractWBTokens(page)
	if token == "" {
		return fmt.Errorf("页面未暴露 token")
	}
	return httpPostJSON(strings.TrimRight(url, "/")+"/api/engine/stop", token, map[string]any{})
}

// runGovernanceCLI 是治理子系统的命令行出口（只读优先，写动作必须显式子命令）。
//
// 用法：
//
//	--governance [status|sample|recompute|checklist|json]   只读总览/采样/重算/考核/JSON
//	--governance sample                                     采一次样并输出总览
//	--governance transition <id> <stage> [actor] [note]      契约阶段迁移（写）
//	--governance budget                                     打印生效阈值
func runGovernanceCLI(args []string) int {
	layout, err := config.ResolveLayout("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := layout.EnsureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	st, err := gov.Open(layout.GovDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "治理数据打开失败: "+err.Error())
		return 1
	}
	cstore, cerr := config.NewStore(layout.ConfigFile)
	govCfg := config.DefaultConfig().Gov
	if cerr == nil {
		govCfg = cstore.Get().Gov
	}
	if len(args) > 0 && args[0] == "budget" {
		fmt.Println(web.JSONString(map[string]any{"budget": st.Budget(), "defaults": gov.DefaultBudget()}))
		return 0
	}
	if len(args) > 2 && args[0] == "transition" {
		id, stage := args[1], gov.ContractStage(args[2])
		actor, note := "cli", ""
		if len(args) > 3 {
			actor = args[3]
		}
		if len(args) > 4 {
			note = args[4]
		}
		c, terr := st.TransitionContract(id, stage, actor, note, "")
		if terr != nil {
			fmt.Fprintln(os.Stderr, "阶段迁移失败: "+terr.Error())
			return 1
		}
		st.Recompute(time.Now())
		fmt.Printf("契约 %s 已迁移到 %s（操作人 %s，留痕 %d 条）\n", c.ID, c.Stage, actor, len(c.Trail))
		return 0
	}
	eng := gov.NewEngine(layout, st, gov.SilentSource{}, nil, func() config.GovConfig { return govCfg })
	var sum gov.Summary
	mode := "status"
	if len(args) > 0 {
		mode = args[0]
	}
	switch mode {
	case "sample", "recompute", "checklist":
		sum = eng.RunOnce(time.Now())
	case "json":
		sum = eng.Summary()
		fmt.Println(web.JSONString(sum))
		return govExitCode(sum)
	default:
		sum = st.Summary(time.Now())
	}
	if mode == "checklist" {
		fmt.Println(web.JSONString(sum.Compliance))
		return govExitCode(sum)
	}
	fmt.Print(gov.FormatSummary(sum))
	return govExitCode(sum)
}

// govExitCode 把治理等级映射为退出码：0 绿 / 1 黄 / 2 红 / 3 未验证。
// 未验证绝不返回 0 —— 零数据不得记 PASS。
func govExitCode(sum gov.Summary) int {
	switch sum.Level {
	case gov.LevelOK:
		return 0
	case gov.LevelWarn:
		return 1
	case gov.LevelRed:
		return 2
	}
	return 3
}

func runSetupCLI() int {
	// 诚实反馈（2026-10-19）：守卫会静默跳过非正式位置的副本。改造前这里无条件
	// 打印"已创建/更新"，导致在临时副本里跑 --setup 也报成功，掩盖了"其实什么都没写"。
	// 现在先看守卫，跳过时明确告知原因与正确做法。
	if !shortcut.Guard() {
		fmt.Println("已跳过：当前 EXE 不是正式安装位置（需名为 workbench.exe 且同目录有 runtime/ 或 workbench-output/）")
		fmt.Println("正确做法：在安装目录运行 dist\\workbench.exe --setup（或首次正常启动一次，会自动创建）")
		return 0
	}
	if err := shortcut.Create(); err != nil {
		fmt.Fprintln(os.Stderr, "创建快捷方式失败: "+err.Error())
		return 1
	}
	fmt.Println("已创建/更新桌面与开始菜单快捷方式")
	return 0
}

func runExportCLI(dir string) int {
	layout, err := config.ResolveLayout("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	st, err := store.Open(layout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = st.Close() }()
	out := dir
	if out == "" {
		out = layout.ExportsDir
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	n := 0
	for _, t := range st.List() {
		if t.State != model.StateDone {
			continue
		}
		data, err := st.ReadTaskArtifact(t.ID, "final.md")
		if err != nil {
			continue
		}
		name := sanitizeFileName(t.Title) + ".md"
		if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
			continue
		}
		n++
	}
	fmt.Printf("已导出 %d 篇产出一 %s\n", n, out)
	return 0
}

// runAutonomyCLI 是「自主运行」子系统的命令行出口（无 GUI 也能验收）。
//
// 用法：
//
//	--autonomy status            打印当前目标/阶段/进度/最近决策（JSON）
//	--autonomy plan [--json]     只做一次动态规划（不执行模型），打印推导出的任务草案
//	--autonomy decisions [N]     打印决策台账（默认 30 条）
//	--autonomy goal <title> <intent> [domain]   人工设置项目目标（写）
//	--autonomy selftest          无外部依赖地跑一遍闭环自检（PASS/FAIL，退出码 0/1）
//
// 设计动机：自主链路的核心价值是"目标 → 阶段任务 → 验收 → 下一目标"，
// 这条链不应该只有跑满模型才看得见。plan 与 selftest 让它可离线验证。
func runAutonomyCLI(args []string) int {
	layout, err := config.ResolveLayout("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := layout.EnsureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cstore, err := config.NewStore(layout.ConfigFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取配置失败: "+err.Error())
		return 1
	}
	ast, err := autonomy.OpenStore(layout.AutonomyFile("goals.json"), layout.AutonomyFile("decisions.jsonl"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "自主数据打开失败: "+err.Error())
		return 1
	}
	mgr := autonomy.NewManager(ast, func() config.AutonomyConfig { return cstore.Get().Autonomy }, nil, nil)
	mode := "status"
	if len(args) > 0 {
		mode = args[0]
	}
	switch mode {
	case "selftest":
		return autonomySelftest()
	case "decisions":
		limit := 30
		if len(args) > 1 {
			limit = atoiOr(args[1], 30)
		}
		fmt.Println(web.JSONString(map[string]any{"decisions": mgr.Decisions(limit)}))
		return 0
	case "goal":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "用法: --autonomy goal <标题> <目标意图> [领域]")
			return 2
		}
		domain := ""
		if len(args) > 3 {
			domain = args[3]
		}
		g, gerr := mgr.SetGoal(args[1], args[2], domain)
		if gerr != nil {
			fmt.Fprintln(os.Stderr, "设置目标失败: "+gerr.Error())
			return 1
		}
		fmt.Println(web.JSONString(g))
		return 0
	}
	// 先确保有活跃目标（与引擎启动时行为一致）
	g, berr := mgr.Bootstrap()
	if berr != nil {
		fmt.Fprintln(os.Stderr, "目标初始化失败: "+berr.Error())
		return 1
	}
	if mode == "plan" {
		recent := autonomyRecentTitles(layout)
		draft, goal, ok := mgr.NextTask(recent, nil)
		out := map[string]any{"planned": ok, "goal": goal, "draft": draft}
		if !ok {
			out["reason"] = "当前无可规划草案（目标已完成/阻塞/预算耗尽或草案被边界拒绝），引擎会回落到固定模板链路"
		}
		if len(args) > 1 && args[1] == "--json" {
			fmt.Println(web.JSONString(out))
			return 0
		}
		fmt.Println(autonomy.FormatPlanResult(out))
		return 0
	}
	// status
	sum := map[string]any{
		"enabled":   cstore.Get().Autonomy.Enabled,
		"active":    g,
		"goals":     mgr.Goals(),
		"decisions": mgr.Decisions(10),
	}
	fmt.Println(web.JSONString(sum))
	return 0
}

// atoiOr 把字符串转 int，失败时返回默认值。
func atoiOr(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}

// autonomyRecentTitles 从任务台账取最近已完成标题（防重复规划）。
func autonomyRecentTitles(layout config.Layout) []string {
	st, err := store.Open(layout)
	if err != nil {
		return nil
	}
	defer func() { _ = st.Close() }()
	all := st.List()
	out := make([]string, 0, 20)
	for i := len(all) - 1; i >= 0 && len(out) < 20; i-- {
		if all[i] != nil && all[i].State == model.StateDone {
			out = append(out, all[i].Title)
		}
	}
	return out
}

// autonomySelftest 在临时目录上跑一遍「目标 → 规划 → 验收 → 回退 → 下一目标」闭环。
//
// 判据（全部 PASS 才返回 0）：
//  1. 首次 bootstrap 创建带验收标准的 active 目标；
//  2. 动态规划能从目标推导出任务草案（标题/正文非空、领域在白名单内）；
//  3. 越界草案（含"人工登录"等）被边界拒绝；
//  4. 高质量阶段计入进度并记 accept 决策；
//  5. 低质量阶段回退且不计入进度；
//  6. 验收达标后目标自动完成并推导下一目标（闭环续跑）；
//  7. 重开存储后目标与阶段轨迹仍完整（持久化）。
func autonomySelftest() int {
	dir, err := os.MkdirTemp("", "wb-autonomy-selftest-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "创建临时目录失败: "+err.Error())
		return 1
	}
	defer os.RemoveAll(dir)
	pass, fail := 0, 0
	check := func(name string, cond bool, detail string) {
		if cond {
			fmt.Printf("  [PASS] %s\n", name)
			pass++
		} else {
			fmt.Printf("  [FAIL] %s %s\n", name, detail)
			fail++
		}
	}
	cfg := config.DefaultAutonomyConfig()
	cfg.Budget.MaxStages = 4
	cfg.Budget.MinQuality = 6
	goalsPath := filepath.Join(dir, "goals.json")
	decPath := filepath.Join(dir, "decisions.jsonl")
	ast, oerr := autonomy.OpenStore(goalsPath, decPath)
	if oerr != nil {
		fmt.Fprintln(os.Stderr, "打开存储失败: "+oerr.Error())
		return 1
	}
	clock := int64(1_700_000_000_000)
	now := func() int64 { clock += 1000; return clock }
	mgr := autonomy.NewManager(ast, func() config.AutonomyConfig { return cfg }, nil, now)

	// 1) bootstrap
	g, berr := mgr.Bootstrap()
	check("1. 创建带验收标准的 active 目标", berr == nil && g != nil && g.IsOpen() && len(g.Criteria) > 0, fmt.Sprint(berr))

	// 2) 动态规划
	draft, planGoal, ok := mgr.NextTask(nil, nil)
	check("2. 从项目目标推导出阶段任务", ok && draft.Title != "" && draft.Prompt != "" && planGoal != nil, fmt.Sprintf("ok=%v", ok))
	allowed := false
	for _, d := range cfg.Bounds.AllowedDomains {
		if d == draft.Domain {
			allowed = true
		}
	}
	check("2b. 草案领域在边界白名单内", allowed, draft.Domain)

	// 3) 边界拒绝
	bad := autonomy.Draft{Title: "登录后填报", Prompt: "请登录网站并填写表单后提交，等待审批结果。", Domain: "content"}
	br := autonomy.ValidateDraft(bad, cfg)
	check("3. 需要人工介入的草案被边界拒绝", !br.OK, fmt.Sprintf("%+v", br.Violations))

	// 4) 高质量阶段计入并记 accept
	if g != nil {
		g.Criteria = []autonomy.Criterion{{ID: "c-done", Desc: "完成 2 个阶段", Kind: autonomy.CriterionTaskDone, Threshold: 2}}
		_ = mgr.Store().Upsert(g)
	}
	good := &model.Task{ID: "st-1", Title: "阶段一：立框架", Critique: &model.Critique{Quality: 8}}
	out1 := mgr.CompleteStage(g.ID, good, 800, nil, nil)
	check("4. 高质量阶段验收通过并计入进度", !out1.RolledBack && out1.Verdict.Pass, out1.Verdict.Reason)

	// 5) 低质量阶段回退
	badTask := &model.Task{ID: "st-bad", Title: "低质产出", Critique: &model.Critique{Quality: 3}}
	outBad := mgr.CompleteStage(g.ID, badTask, 100, nil, nil)
	check("5. 低质量阶段回退且不计入进度", outBad.RolledBack, outBad.Verdict.Reason)

	// 6) 达标后闭环：目标完成 + 自动下一目标
	good2 := &model.Task{ID: "st-2", Title: "阶段二：成文", Critique: &model.Critique{Quality: 9}}
	out2 := mgr.CompleteStage(g.ID, good2, 900, nil, nil)
	check("6. 目标验收通过后自动完成", out2.GoalCompleted, out2.Acceptance.Reason)
	check("6b. 自动推导并激活下一目标", out2.NextGoalID != "", "nextGoalId 为空")
	next, hasNext := mgr.Active()
	check("6c. 下一目标处于 active 且不同于已完成目标", hasNext && next != nil && next.ID == out2.NextGoalID && next.ID != g.ID, "")

	// 7) 持久化
	ast2, oerr2 := autonomy.OpenStore(goalsPath, decPath)
	check("7. 重开存储后目标树可恢复", oerr2 == nil && len(ast2.Goals()) >= 2, fmt.Sprint(oerr2))
	decs := mgr.Decisions(100)
	kinds := map[autonomy.DecisionKind]bool{}
	for _, d := range decs {
		kinds[d.Kind] = true
	}
	check("7b. 决策台账覆盖 规划/验收/回退/目标完成/下一目标", kinds[autonomy.DecisionPlan] && kinds[autonomy.DecisionAccept] && kinds[autonomy.DecisionRollback] && kinds[autonomy.DecisionGoalDone] && kinds[autonomy.DecisionNextGoal], fmt.Sprint(kinds))

	fmt.Printf("RESULT: %d PASS / %d FAIL\n", pass, fail)
	if fail > 0 {
		return 1
	}
	return 0
}
func sanitizeFileName(s string) string {
	r := strings.NewReplacer("\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_", "\n", " ", "\r", " ")
	out := r.Replace(s)
	if len([]rune(out)) > 60 {
		out = string([]rune(out)[:60])
	}
	return out
}

// shouldAutoStart 是「进程启动时是否自动开跑」的**唯一**判据（纯函数，便于单测）。
//
// 现场缺陷（2026-10-19）：原判据只看 (running|paused)，而自愈停机把 phase 落成 stopped，
// 于是「资源自愈保证 7x24」重启后引擎永不恢复 —— 只重启了壳，不恢复干活。
//
// 判据矩阵：
//
//	AutoStart=true                     => 启动（开机自启语义，无条件）
//	AutoResume=true 且 running/paused  => 启动（进程被杀/断电后恢复）
//	AutoResume=true 且 resumeMarked    => 启动（自愈触发的被动中断，必须接续）
//	其它（含用户主动 stop 的 stopped）  => 不启动（尊重用户意愿）
func shouldAutoStart(cfg config.Config, phase model.EnginePhase, resumeMarked bool) bool {
	if cfg.AutoStart {
		return true
	}
	if !cfg.AutoResume {
		return false
	}
	if phase == model.PhaseRunning || phase == model.PhasePaused {
		return true
	}
	// 自愈重启：phase 已是 stopped，但标记说明「停机前确实在跑」，必须接续。
	return resumeMarked && phase == model.PhaseStopped
}
