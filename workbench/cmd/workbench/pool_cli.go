package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"workbench/internal/config"
	"workbench/internal/modelpool"
)

// runModelPoolCLI 是模型池的命令行出口（无需启动 GUI 即可运维）。
//
// 用法:
//
//	--model-pool status               # 列出模型、状态、优先级、可用数（JSON）
//	--model-pool sync                 # 立刻从 OFM 渠道网关同步清单
//	--model-pool probe                # 立刻跑一轮可用性探测
//	--model-pool routes               # 打印投影出的调度路由表（config.ModelRoute 形态）
//	--model-pool log [N]              # 最近 N 条状态日志（默认 30）
//	--model-pool reset <modelID>      # 解除该模型的冷却与计数
//	--model-pool enable  <modelID>    # 启用
//	--model-pool disable <modelID>    # 停用
//	--model-pool strategy <name>      # 切换调度策略
//	--model-pool aggregate            # 自填端点的聚合车道对账（无可用退出码 2）
//
// 退出码：0 成功 / 1 命令错误 / 2 池内没有可用模型（供脚本判定）。
func runModelPoolCLI(args []string) int {
	layout, err := config.ResolveLayout("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	if err := layout.EnsureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	store := modelpool.NewStore(layout.DataRoot)
	svc := modelpool.NewService(store, nil, modelpool.ServiceConfig{
		Enabled: true, ProbeSeconds: 180, SyncSeconds: 600,
		Strategy: modelpool.StrategyPriority, DSHHome: layout.DSHHome,
	})
	cmd := "status"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "status":
		st := svc.Status()
		// 脱敏后再打印：Manager.Specs() 带明文 APIKey（调度与探测要用），
		// 但 CLI 输出会进终端、脚本管道与日志文件 —— 密钥绝不能流到那里。
		// 与面板 /api/pool 同一口径（web.redactSpec）。
		st["specs"] = modelpool.RedactSpecs(svc.Manager().Specs())
		b, _ := json.MarshalIndent(st, "", "  ")
		fmt.Println(string(b))
		if n, okf := st["available"].(int); okf && n == 0 {
			return 2
		}
		return 0
	case "sync":
		res := svc.SyncNow("cli")
		fmt.Printf("同步完成：新增 %d / 更新 %d / 跳过 %d\n", res.Added, res.Updated, res.Skipped)
		if len(res.Revived) > 0 {
			fmt.Println("自动恢复：" + strings.Join(res.Revived, ", "))
		}
		return 0
	case "probe":
		sum := svc.ProbeNow()
		fmt.Printf("探测完成：共 %d，可用 %d，下线 %d，未知 %d\n", sum.Total, sum.OK, sum.Down, sum.Unknown)
		return 0
	case "routes":
		mgr := svc.Manager()
		mgr.NotifyRoutes()
		routes := mgr.Routes()
		b, _ := json.MarshalIndent(routes, "", "  ")
		fmt.Println(string(b))
		if len(routes) == 0 {
			return 2
		}
		return 0
	case "log":
		n := 30
		if len(args) > 1 {
			// 必须整串解析**且检查错误**（2026-10-25 round 157 实测缺陷）：此前是
			// fmt.Sscanf(args[1], "%d", &n) 且**返回值被丢弃** —— "5abc" 静默当 5，
			// "abc" 静默沿用默认 30，"0x10" 在 %d 下解析成 **0** ⇒ LogTail(0) 打印 0 行、rc=0，
			// 用户以为拿到了日志，实际是静默空操作。
			v, err := parseIntArg(args[1])
			if err != nil || v <= 0 {
				fmt.Fprintln(os.Stderr, "无效的条数: "+args[1])
				return 1
			}
			n = v
		}
		lines := svc.LogTail(n)
		for _, l := range lines {
			fmt.Println(l)
		}
		return 0
	case "reset", "enable", "disable":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: --model-pool "+cmd+" <modelID>")
			return 1
		}
		id := args[1]
		var e error
		switch cmd {
		case "reset":
			e = svc.Manager().Restore(id, "cli")
		case "enable":
			e = svc.Manager().SetEnabled(id, true, "cli")
		default:
			e = svc.Manager().SetEnabled(id, false, "cli")
		}
		if e != nil {
			fmt.Fprintln(os.Stderr, e.Error())
			return 1
		}
		fmt.Println("已完成：" + cmd + " " + id)
		return 0
	case "strategy":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: --model-pool strategy priority|round-robin|weighted|least-loaded")
			return 1
		}
		if e := svc.SetStrategy(modelpool.Strategy(args[1])); e != nil {
			fmt.Fprintln(os.Stderr, e.Error())
			return 1
		}
		fmt.Println("调度策略已切换为 " + args[1])
		return 0
	case "lane":
		// 一键启用/一键停用某个免费池（老大 2026-10-22 令）。
		// 用法: --model-pool lane <free|kilo|eac|aggregate|custom> <enable|disable>
		// 按分组名: --model-pool lane group OFM免费... <enable|disable>（需精确分组名时用 HTTP API）
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "用法: --model-pool lane <free|kilo|eac|aggregate|custom> <enable|disable>")
			return 1
		}
		laneArg := args[1]
		action := args[2]
		enabled := true
		switch action {
		case "enable":
		case "disable":
			enabled = false
		default:
			fmt.Fprintln(os.Stderr, "动作只能是 enable|disable，实际 "+action)
			return 1
		}
		res, e := svc.Manager().SetPoolEnabled(laneArg, "", enabled, "cli")
		if e != nil {
			fmt.Fprintln(os.Stderr, e.Error())
			return 1
		}
		svc.Manager().NotifyRoutes()
		toggleAction := "启用"
		if !enabled {
			toggleAction = "停用"
		}
		fmt.Printf("已一键%s「%s」：改动 %d 个模型（%d 个已是目标状态）\n", toggleAction, laneArg, len(res.Affected), res.Skipped)
		return 0
	case "purge-gateway":
		// 清理网关专属/网关其它车道存量（同步时会自动做，这里供人工/脚本立即执行）。
		res := svc.Manager().PurgeGatewayLanes("cli")
		if len(res.Removed) == 0 {
			fmt.Println("网关专属/其它车道已无存量条目")
			return 0
		}
		fmt.Printf("已清理 %d 条：%s\n", len(res.Removed), strings.Join(res.Removed, ", "))
		return 0
	case "aggregate":
		// 聚合车道对账：自填兼容端点能否真被 DSH 调用（providerId 是否在插件清单里、
		// 密钥是否齐、线协议是否匹配），以及将要写进 settings.json 的 aggregate 段。
		// 用途：接线前后各跑一次，用退出码判断「有没有真能用的自填端点」。
		view := svc.AggregateView()
		b, _ := json.MarshalIndent(view, "", "  ")
		fmt.Println(string(b))
		if !view.RegistryLoaded {
			fmt.Fprintln(os.Stderr, "provider 清单未加载："+view.Error)
			return 2
		}
		if !view.Enabled {
			return 2 // 没有可用自填端点（或全部被拒），供脚本判定
		}
		return 0
	case "help", "-h", "--help":
		fmt.Println(modelPoolUsage)
		return 0
	default:
		fmt.Fprintln(os.Stderr, "未知子命令: "+cmd)
		fmt.Println(modelPoolUsage)
		return 1
	}
}

const modelPoolUsage = `模型池 CLI（多接口统一纳管 + 自动探测 + 故障转移）
  --model-pool status            列出全部模型/状态/优先级（JSON；无可用模型退出码 2）
  --model-pool sync              立刻从 OFM 渠道网关同步清单
  --model-pool probe             立刻跑一轮可用性探测
  --model-pool routes            打印投影出的调度路由表
  --model-pool log [N]           最近 N 条状态日志（默认 30）
  --model-pool reset <id>        解除冷却并重置计数
  --model-pool enable|disable <id>
  --model-pool strategy <name>   priority | round-robin | weighted | least-loaded
  --model-pool lane <lane> <enable|disable>
                             一键启用/停用整个池（free|kilo|eac|aggregate|custom）
  --model-pool purge-gateway   清理网关专属/其它车道存量（只留 eac/kilo/ofm 等免费车道）
  --model-pool aggregate         自填端点的聚合车道对账（无可用退出码 2）`

var _ = time.Now
