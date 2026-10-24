package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"workbench/internal/config"
	"workbench/internal/supervisor"
	"workbench/internal/web"
)

// soak.go — 长时压测命令行入口（A7「7×24 长期稳定」的机器判据）。
//
// 用法： workbench.exe --soak [--json] [--min-hours N] [csv路径]
//
// 三态退出码（与总验收同一套纪律）：
//
//	0 = ok       采样足以判定且无泄漏趋势
//	2 = leak     检出堆/句柄单调上升
//	3 = unknown  采样不足或跨度不够（「取不到数 ≠ 取到 0」，不得记 PASS）
func runSoakCLI(args []string) int {
	return runSoakWith(args, dataRootFromArgs(args))
}

// runSoakWith 是带数据根的显式形式（测试可直接指定根）。
func runSoakWith(args []string, dataRoot string) int {
	asJSON := false
	minHours := 24.0
	pathArg := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			asJSON = true
		case "--min-hours":
			if i+1 < len(args) {
				if v, err := strconv.ParseFloat(args[i+1], 64); err == nil {
					minHours = v
				}
				i++
			}
		default:
			if !strings.HasPrefix(args[i], "--") {
				pathArg = args[i]
			}
		}
	}
	layout, err := config.ResolveLayout(dataRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 3
	}
	csvPath := pathArg
	if csvPath == "" {
		csvPath = layout.LogFile("metrics.csv")
	}
	res := buildSoakReport(csvPath, minHours)
	if asJSON {
		fmt.Println(web.JSONString(res))
	} else {
		printSoakReport(res)
	}
	return res.ExitCode
}

// soakReport 是 --soak 的机器可读结论。
type soakReport struct {
	Exists   bool    `json:"exists"`
	Path     string  `json:"path"`
	Samples  int     `json:"samples"`
	SpanHour float64 `json:"spanHours"`
	Heap     string  `json:"heap"`
	Handles  string  `json:"handles"`
	Verdict  string  `json:"verdict"`
	Reason   string  `json:"reason"`
	ExitCode int     `json:"exitCode"`
}

// buildSoakReport 读取 metrics.csv 并给出三态结论。
func buildSoakReport(csvPath string, minHours float64) soakReport {
	r := soakReport{Path: csvPath, Verdict: "unknown"}
	points, skipped, err := supervisor.ReadMetricsCSV(csvPath)
	if err != nil {
		r.Reason = "metrics.csv 不可读（未运行过）: " + err.Error()
		r.ExitCode = 3
		return r
	}
	r.Exists = true
	r.Samples = len(points)
	_ = skipped
	heapRep := supervisor.AnalyzeTrend(points, "heap", 0.25)
	handlesRep := supervisor.AnalyzeTrend(points, "handles", 0.30)
	r.SpanHour = heapRep.SpanMin / 60.0
	r.Heap = heapRep.Verdict
	r.Handles = handlesRep.Verdict
	// 判据走唯一实现（soak_verdict.go）：与总验收 A7 同一套四象限。
	// 改造前这里另写一遍 switch —— 正是 2026-10-19「两处实现漂移」现场缺陷的一半。
	trendVerdict, _ := supervisor.SoakVerdict([]supervisor.TrendReport{heapRep, handlesRep})
	verdict, code := soakConclusion(trendVerdict, r.SpanHour, minHours)
	r.Verdict = verdict
	r.ExitCode = code
	switch code {
	case 2:
		r.Reason = "检出泄漏趋势（堆 " + heapRep.Verdict + " / 句柄 " + handlesRep.Verdict + "）"
	case 0:
		r.Reason = "已覆盖要求的时长且无泄漏趋势"
	case 3:
		if r.SpanHour >= minHours {
			r.Reason = "跨度已够但趋势未能判定：" + heapRep.Reason
		} else {
			r.Reason = "采样跨度 " + fmt.Sprintf("%.2f", r.SpanHour) + " 小时，未覆盖要求的 " + fmt.Sprintf("%.0f", minHours) + " 小时（不足以宣称跑满，也不构成失败）"
		}
	}
	return r
}

// printSoakReport 输出人读版。
func printSoakReport(r soakReport) {
	fmt.Println("=== 长时压测判定 ===")
	fmt.Println("metrics: " + r.Path)
	fmt.Printf("采样: %d 点，跨度 %.2f 小时\n", r.Samples, r.SpanHour)
	fmt.Println("堆趋势: " + r.Heap + "；句柄趋势: " + r.Handles)
	fmt.Println("结论: " + r.Verdict + " — " + r.Reason)
}

// dataRootFromArgs 从 --data 参数取数据根（与主命令口径一致）。
func dataRootFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "--data" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
