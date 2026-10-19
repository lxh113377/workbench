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
		case "--model-pool", "--pool":
			os.Exit(runModelPoolCLI(args[1:]))
		case "--soak":
			os.Exit(runSoakCLI(args[1:]))
		case "--acceptance", "--ac":
			os.Exit(runAcceptanceCLI(args[1:]))
		case "--ui-probe":
			os.Exit(runUIProbeCLI(args[1:]))
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
		if a == "--silent" {
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
		preCfg, _ := config.NewStore(layout.ConfigFile)
		basePort := 17890
		if preCfg != nil {
			basePort = preCfg.Get().Server.Port
		}
		if p, ok := single.ProbeRunning("127.0.0.1", basePort, 10); ok {
			openPanel(fmt.Sprintf("http://127.0.0.1:%d/", p))
			fmt.Println("工作台已在运行：" + fmt.Sprintf("http://127.0.0.1:%d/", p))
		} else {
			fmt.Println("工作台已在运行，但未能定位面板端口")
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

	stopSignal := make(chan os.Signal, 2)
	signal.Notify(stopSignal, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stopSignal
		_ = eng.Stop()
		srv.Close()
		os.Exit(0)
	}()

	state := st.State()
	cfg := cstore.Get()
	shouldAutoStart := cfg.AutoStart || (cfg.AutoResume && (state.Phase == model.PhaseRunning || state.Phase == model.PhasePaused))
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
		_ = eng.Stop()
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
			_ = sup.WriteCSV(layout.LogFile("metrics.csv"))
		}
	}()

	hub.Emit(model.Event{Type: "ready", At: model.Now(), Msg: "工作台已就绪", Data: map[string]any{
		"url": srv.URL(), "port": srv.Port(), "version": version, "profile": profileInfo, "profileError": errText(profErr),
	}})

	if !silent {
		openPanel(srv.URL())
	}

	trayDone := make(chan struct{})
	go func() {
		defer close(trayDone)
		tray.Run(tray.Options{
			Title:    "个人 AI 工作台",
			Tooltip:  "个人 AI 工作台",
			Status:   func() string { return string(eng.Phase()) },
			OnOpen:   func() { openPanel(srv.URL()) },
			OnStart:  func() { _ = eng.Start() },
			OnPause:  func() { _ = eng.Pause() },
			OnResume: func() { _ = eng.Resume() },
			OnStop:   func() { _ = eng.Stop() },
			OnQuit: func() {
				_ = eng.Stop()
				srv.Close()
				os.Exit(0)
			},
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

func openPanelOrPrint(layout config.Layout) {
	url := readRunningURL(layout)
	if url == "" {
		fmt.Println("工作台已在运行，但未能定位面板地址")
		return
	}
	openPanel(url)
	fmt.Println("工作台已在运行：" + url)
}

func openPanel(url string) {
	if err := browser.Open(url); err != nil {
		fmt.Println("请手动打开：" + url)
	}
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
	marker := filepath.Join(layout.RuntimeDir, "shortcut.ok")
	if _, err := os.Stat(marker); err == nil {
		return
	}
	if err := shortcut.Create(); err != nil {
		hub.Emit(model.Event{Type: "warn", At: model.Now(), Msg: "创建快捷方式失败: " + err.Error()})
		return
	}
	_ = os.WriteFile(marker, []byte(time.Now().Format(time.RFC3339)), 0o644)
	hub.Emit(model.Event{Type: "ready", At: model.Now(), Msg: "已创建桌面快捷方式"})
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
	_ = st.Savepoint()
	fmt.Println("已标记停止；若进程仍在运行，请通过面板或托盘退出。")
	return 0
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
