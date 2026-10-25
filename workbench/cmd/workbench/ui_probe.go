package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"workbench/internal/config"
	"workbench/internal/engine"
	"workbench/internal/gov"
	"workbench/internal/logging"
	"workbench/internal/modelpool"
	"workbench/internal/store"
	"workbench/internal/supervisor"
	"workbench/internal/templates"
	"workbench/internal/web"
)

// ui_probe.go — 前端验收的确定性通道（无人工点击的机器判据）。
//
// 为什么需要它而不是直接开真实工作台：真实路径会在启动时探测执行通道、同步模型池清单，
// 在容器/CI 里不可复现。前端验收关心的是「页面契约 + 交互路径」，因此这里用同一套
// web.NewServer + engine.New 构造一个可复现的服务：内部数据落临时目录、日志静默、不开托盘。
//
// 用法：
//
//	workbench --ui-probe --dir <数据目录> --port <端口> --seconds <存活秒数> [--expect-idle]
//
// 退出码：0 探针正常退出 / 1 监听失败 / 2 参数错误。
func runUIProbeCLI(args []string) int {
	dir := ""
	port := 17890
	seconds := 120
	expectIdle := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dir", "--data":
			if i+1 < len(args) {
				dir = args[i+1]
				i++
			}
		case "--port":
			if i+1 < len(args) {
				v, err := parseIntArg(args[i+1])
				// 非法值必须**报错退出**（rc=2，见本文件头注的退出码约定），不许静默沿用默认端口：
				// 静默沿用会让「--port 写错」看起来像「参数生效了」，排查时找不到入口。
				// 0 也拒绝：端口 0 会让内核随机分配，与本探针「固定端口、可复现」的设计相悖。
				if err != nil || v <= 0 || v > 65535 {
					fmt.Fprintln(os.Stderr, "ui-probe: 无效端口: "+args[i+1])
					return 2
				}
				port = v
				i++
			}
		case "--seconds":
			if i+1 < len(args) {
				v, err := parseIntArg(args[i+1])
				// 同上：非法/非正秒数一律报错退出，不许静默沿用 120 秒默认值。
				if err != nil || v <= 0 {
					fmt.Fprintln(os.Stderr, "ui-probe: 无效秒数: "+args[i+1])
					return 2
				}
				seconds = v
				i++
			}
		case "--expect-idle":
			expectIdle = true
		}
	}
	if dir == "" {
		fmt.Fprintln(os.Stderr, "ui-probe: --dir 必填")
		return 2
	}
	layout, err := config.BuildLayout(dir, filepath.Join(dir, "workbench-output"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ui-probe: "+err.Error())
		return 1
	}
	if err := layout.EnsureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, "ui-probe: "+err.Error())
		return 1
	}
	// 探针固定端口，禁用浏览器自动打开与自启，避免污染宿主。
	seed := config.DefaultConfig()
	seed.Server.Port = port
	seed.Server.OpenBrowser = false
	seed.AutoStart = false
	seed.AutoResume = false
	seed.ResumeOnBoot = false
	seed.Pool.Enabled = false
	seed.Gov.Enabled = false
	seed.Supervisor.Enabled = false
	seed.Autonomy.Enabled = false
	raw, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(layout.ConfigFile, raw, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "ui-probe: "+err.Error())
		return 1
	}
	cstore, err := config.NewStore(layout.ConfigFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ui-probe: "+err.Error())
		return 1
	}
	reg, err := templates.Open(filepath.Join(layout.DataRoot, "templates.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ui-probe: "+err.Error())
		return 1
	}
	st, err := store.Open(layout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ui-probe: "+err.Error())
		return 1
	}
	defer func() { _ = st.Close() }()
	hub := web.NewHub(300)
	eng, err := engine.New(layout, cstore, st, reg, hub)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ui-probe: "+err.Error())
		return 1
	}
	eng.SetLogger(logging.Nop())
	eng.ReloadConfig()
	if expectIdle && eng.Phase() != "idle" {
		fmt.Fprintf(os.Stderr, "ui-probe: 期望初始阶段为 idle，实际 %s\n", eng.Phase())
		return 2
	}
	srv := web.NewServer(layout, cstore, eng, hub)
	// 子页面齐全性：治理与模型池也要接入，否则 /governance、/modelpool 在探针里是 503，
	// 测出来的「子页面完整」就是假的（面板的标签存在但接口不可达）。
	// 只注入不启动：探测循环会真的发网络请求，探针禁止消耗额度。
	sup := supervisor.New(layout, func() config.SupervisorConfig { return cstore.Get().Supervisor })
	srv.SetSupervisor(sup)
	if govStore, gerr := gov.Open(layout.GovDir); gerr == nil {
		srv.SetGovernance(gov.NewEngine(layout, govStore, eng, sup, func() config.GovConfig { return cstore.Get().Gov }))
	}
	poolStore := modelpool.NewStore(layout.DataRoot)
	srv.SetModelPool(modelpool.NewService(poolStore, nil, modelpool.ServiceConfig{
		Enabled:      false,
		ProbeSeconds: cstore.Get().Pool.ProbeSeconds,
		SyncSeconds:  cstore.Get().Pool.SyncSeconds,
		Strategy:     modelpool.Strategy(cstore.Get().Pool.Strategy),
		DSHHome:      layout.DSHHome,
	}))
	if err := srv.Listen(); err != nil {
		fmt.Fprintln(os.Stderr, "ui-probe: "+err.Error())
		return 1
	}
	fmt.Printf("UI_PROBE_READY %s token=%s phase=%s\n", srv.URL(), srv.Token(), string(eng.Phase()))
	flagFile := filepath.Join(dir, "ui-probe.url")
	_ = os.WriteFile(flagFile, []byte(srv.URL()+"\n"+srv.Token()+"\n"), 0o644)
	stop := make(chan os.Signal, 2)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case <-stop:
	case <-time.After(time.Duration(seconds) * time.Second):
	}
	_ = eng.Stop()
	srv.Close()
	fmt.Println("UI_PROBE_EXIT")
	return 0
}

// parseIntArg 是仅接受十进制整数的解析：**整串**必须是十进制整数（两侧空白先剥掉）。
//
// 为什么不用 fmt.Sscanf("%d")（2026-10-25 round 156 实测缺陷）：Sscanf 只取**前导**整数、
// 忽略尾部残留，于是 "12abc"→12、"1e3"→1、"12.5"→12 全都静默成功，最刺眼的是
// "0x10"→**0**（端口静默变成「内核随机分配」）—— 与上一行承诺的「拒绝半数字」正好相反，
// 而本函数当时**零测试**。strconv.Atoi 按整串解析，四类残留一律报错。
//
// 边界（按 2026-10-25 实测写，不按直觉写）：Atoi 接受**可选符号**，故 "-5" 与 "+5" 都照常返回；
// 「端口/秒数不许非正」是调用点的职责（见 --port / --seconds 分支），不在本层夹带语义。
func parseIntArg(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(s))
}
