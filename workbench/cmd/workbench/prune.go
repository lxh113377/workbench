package main

// prune.go -- 保留策略命令行入口（7x24 的磁盘兜底）。

// 为什么需要（2026-10-19 审计现场）：
// internal/retention 实现了三档保留策略（DSH_HOME 3 天 / workspace 30 天 / 任务目录 90 天），
// config 也接好了 RetentionConfig，但**没有 CLI、没有 API、没有定时触发** ——
// 对一个 7x24 长跑系统来说，这等于「磁盘只涨不回收」，几个月后必然撑爆。
// 逻辑写好了却没人调用，是最典型的「以为有、其实没有」。

// 用法：
//
//	--prune [--json] [--apply] [--dsh-home-days N] [--workspace-days N] [--task-dir-days N]
//
// 默认 **dry-run**（只统计不删除）；必须显式 --apply 才真的回收。
// 这条默认值是刻意的：删除是不可逆操作，宁可多一步确认，也不要误删用户资产。
//
// 退出码：0 = 执行成功（含 dry-run）；1 = 参数/布局错误。
import (
	"fmt"
	"os"
	"strings"

	"workbench/internal/config"
	"workbench/internal/retention"
	"workbench/internal/web"
)

type pruneOptions struct {
	JSON  bool
	Apply bool
	// TaskDirs 控制是否**回收任务目录本身**（默认 false）。
	//
	// 为什么默认关闭：项目硬约束与本包注释都写明「绝不删除任何产出」，
	// 而任务目录里含 final.md 这类成果资产。
	// 默认只回收 DSH_HOME 副本与 workspace（那两类不是成果）。
	// 但即使不回收，也**如实报告任务目录占用**，避免又一个「看起来有闸门实际没有」的黑箱。
	TaskDirs bool
	Poly     retention.Policy
	HavePoly bool
}

func runPruneCLI(args []string) int {
	opt := pruneOptions{}
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json":
			opt.JSON = true
		case a == "--data" && i+1 < len(args):
			// 数据根覆盖（与 --acceptance/--soak 同口径）：由 dataRootFromArgs 统一消费。
			i++
		case a == "--apply":
			opt.Apply = true
		case a == "--dry-run":
			opt.Apply = false
		case a == "--task-dirs":
			// 显式开启「连任务目录一起回收」——默认关闭，见 TaskDirs 注释。
			opt.TaskDirs = true
		case a == "--help" || a == "-h":
			printPruneUsage()
			return 0
		case (a == "--dsh-home-days" || a == "--workspace-days" || a == "--task-dir-days") && i+1 < len(args):
			i++
			// 必须用**整串**解析（2026-10-25 round 157 实测缺陷）：此前用 fmt.Sscan(args[i], &v)，
			// 它对尾部残留一律不报错 —— "3abc"→3、"3.5"→3，最意外的是 "0x10"→**16**（十六进制被当成合法值）。
			// 这三个数字是**保留天数**，且 --apply 不可逆：解错一天，删的就是另一个年龄的数据。
			// 统一走本包 parseIntArg（与 --port/--seconds 同一实现，避免第二套语义）。
			v, err := parseIntArg(args[i])
			if err != nil || v <= 0 {
				fmt.Fprintln(os.Stderr, "无效的天数: "+args[i])
				return 1
			}
			opt.HavePoly = true
			switch a {
			case "--dsh-home-days":
				opt.Poly.DSHHomeDays = v
			case "--workspace-days":
				opt.Poly.WorkspaceDays = v
			case "--task-dir-days":
				opt.Poly.TaskDirDays = v
			}
		default:
			fmt.Fprintln(os.Stderr, "未知参数: "+a)
			printPruneUsage()
			return 1
		}
	}
	layout, err := config.ResolveLayout(dataRootFromArgs(args))
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	poly := retention.DefaultPolicy()
	// 用户配置优先；命令行再覆盖。
	if cstore, cerr := config.NewStore(layout.ConfigFile); cerr == nil {
		poly = cstore.Get().Retention
	}
	if opt.HavePoly {
		if opt.Poly.DSHHomeDays > 0 {
			poly.DSHHomeDays = opt.Poly.DSHHomeDays
		}
		if opt.Poly.WorkspaceDays > 0 {
			poly.WorkspaceDays = opt.Poly.WorkspaceDays
		}
		if opt.Poly.TaskDirDays > 0 {
			poly.TaskDirDays = opt.Poly.TaskDirDays
		}
	}
	// 三档保留策略：
	//   1) DSH_HOME 任务副本（非成果，默认回收）
	//   2) 任务 workspace（明细副本，默认回收）
	//   3) 任务目录本身（**含 final.md 等成果资产；默认只统计不回收**，需 --task-dirs 显式开启）
	h := retention.PruneDSHHome(layout.DSHHome, poly, !opt.Apply)
	t := retention.PruneTaskWorkspaces(layout.TasksDir, poly, !opt.Apply)
	// 无论是否开启回收，都如实统计任务目录占用 —— 让人看到「这里在涨」。
	tdCount, tdBytes, tdExpired := retention.TaskDirUsage(layout.TasksDir, poly)
	var td retention.Result
	td.DryRun = !opt.Apply
	if opt.TaskDirs {
		td = retention.PruneTaskDir(layout.TasksDir, poly, !opt.Apply)
	}
	out := map[string]any{
		"dataRoot":      layout.DataRoot,
		"dshHomePath":   layout.DSHHome,
		"tasksRoot":     layout.TasksDir,
		"dryRun":        !opt.Apply,
		"taskDirsOptIn": opt.TaskDirs,
		"policy":        poly,
		"dshHome":       h,
		"taskWorkspace": t,
		"taskDir": map[string]any{
			"reclaimedDirs": td.ReclaimedDirs,
			"skippedDirs":   td.SkippedDirs,
			"freedBytes":    td.FreedBytes,
			"totalCount":    tdCount,
			"totalBytes":    tdBytes,
			"expiredCount":  tdExpired,
			"optIn":         opt.TaskDirs,
			"errors":        td.Errors,
		},
		"freedBytes":    h.FreedBytes + t.FreedBytes + td.FreedBytes,
		"reclaimedDirs": h.ReclaimedDirs + t.ReclaimedDirs + td.ReclaimedDirs,
		"skippedDirs":   h.SkippedDirs + t.SkippedDirs + td.SkippedDirs,
	}
	// 回收失败的可读解释（2026-10-19 现场）：
	//
	// SHFileOperationW 在「从 workspace 启动」的进程里会返回 0x78
	// （ERROR_CALL_NOT_IMPLEMENTED，"lookup key not found in any active activation context"）。
	// 实测判别：同一份 EXE 字节放到 workspace 外执行就成功 —— 变量在**启动环境**，不在代码。
	// 因此这里不把它当成致命错误，而是明确告知「未执行成功 + 可能原因」，
	// 让读报表的人能区分「真失败」与「环境限制」。
	allErrs := append(append(append([]string{}, h.Errors...), t.Errors...), td.Errors...)
	if len(allErrs) > 0 && opt.Apply {
		out["errors"] = allErrs
		out["hint"] = "有目录未能回收。若错误含 0x78（ERROR_CALL_NOT_IMPLEMENTED），" +
			"通常是从受限/workspace 环境启动导致 shell 激活上下文不可用；" +
			"请在正常桌面会话（非沙箱）中重试。"
	}
	if opt.JSON {
		fmt.Println(web.JSONString(out))
		return 0
	}
	mode := "dry-run（未删除任何东西）"
	if opt.Apply {
		mode = "apply（已执行回收）"
	}
	fmt.Printf("保留策略: DSH_HOME %d 天 / workspace %d 天 / 任务目录 %d 天\n", poly.DSHHomeDays, poly.WorkspaceDays, poly.TaskDirDays)
	fmt.Printf("模式：%s\n", mode)
	fmt.Printf("DSH_HOME 副本：回收 %d 个，跳过 %d 个，释放 %.1f MB\n", h.ReclaimedDirs, h.SkippedDirs, float64(h.FreedBytes)/1e6)
	fmt.Printf("任务 workspace：回收 %d 个，跳过 %d 个，释放 %.1f MB\n", t.ReclaimedDirs, t.SkippedDirs, float64(t.FreedBytes)/1e6)
	// 任务目录始终如实报告（即便未开启回收）——「有闸门」必须伴随「看得见」。
	fmt.Printf("任务目录（含成果资产）：共 %d 个 / %.1f MB，其中已过保留期 %d 个\n",
		tdCount, float64(tdBytes)/1e6, tdExpired)
	if opt.TaskDirs {
		fmt.Printf("  已按 --task-dirs 回收 %d 个，释放 %.1f MB\n", td.ReclaimedDirs, float64(td.FreedBytes)/1e6)
	} else {
		fmt.Println("  未回收（默认保护成果资产）；如需连任务目录一起回收，请加 --task-dirs")
	}
	for _, e := range allErrs {
		fmt.Println("  非致命错误: " + e)
	}
	if len(allErrs) > 0 && opt.Apply {
		fmt.Println("  提示：0x78 = ERROR_CALL_NOT_IMPLEMENTED（shell 激活上下文不可用）。")
		fmt.Println("       实测从受限/workspace 环境启动必现；同一二进制在正常桌面会话中可正常回收。")
		fmt.Println("       这不是代码缺陷，请在正常桌面会话（非沙箱）中重试。")
	}
	if !opt.Apply {
		fmt.Println("提示：这是预览。要真正回收请加 --apply。")
	}
	return 0
}

func printPruneUsage() {
	fmt.Println("用法: workbench.exe --prune [--json] [--apply] [--dry-run] [--dsh-home-days N] [--workspace-days N] [--task-dir-days N]")
	fmt.Println("默认 dry-run（只统计不删除）；--apply 才执行真实回收。")
}

// _ 保持 strings 引用（用法文本里将来可能用到），避免未使用导入。
var _ = strings.TrimSpace
