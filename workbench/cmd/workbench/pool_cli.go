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
			fmt.Sscanf(args[1], "%d", &n)
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
  --model-pool strategy <name>   priority | round-robin | weighted | least-loaded`

var _ = time.Now
