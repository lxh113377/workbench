// Command ledgercheck 审计**尝试台账的归因一致性**：拿每行的 error 文本用运行期分类器复算 kind，
// 与台账记录的 kind 对比，把不一致聚类打印。
//
// 为什么是独立小工具而不是塞进引擎（round 145 · T101）：它要读的是**生产台账**（agent 数据根），
// 不是线上服务的一部分；放进引擎等于让运行期多背一个只在排查时用的东西。仓内已有 assertionprobe /
// schedprobe 同类先例 —— 判据做成能复跑的外部命令。
//
// 用法: ledgercheck -root <数据根> [-since 2026-10-30T23:35:00+08:00] [-strict] [-json]
//
//	· 不带 -since 时看全部行（历史残留会一并列出，供考古）；
//	· 带 -since 时只看该时刻之后的行 —— **回归口径**：修完分类规则后，新行不应再有同类不一致；
//	· -strict：窗口内出现任何不一致即退出码 2（供自动化判定）；省略则总是 0（只读报告）。
//
// 退出码: 0 = 扫描完成（或 -strict 下无不一致）; 2 = -strict 下存在不一致; 1 = 用法/IO 错误。
// 末行固定为机读行: LEDGER-ATTRIBUTION=OK 或 LEDGER-ATTRIBUTION=MISMATCH judged=<n> mismatched=<n>
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"workbench/internal/ledgeraudit"
)

func main() {
	root := flag.String("root", os.Getenv("WORKBENCH_ROOT"), "数据根目录（含 tasks/ 与 logs/）")
	since := flag.String("since", "", "只看该时刻之后的行（RFC3339，如 2026-10-30T23:35:00+08:00）")
	strict := flag.Bool("strict", false, "窗口内出现不一致即退出码 2")
	asJSON := flag.Bool("json", false, "以 JSON 输出报告")
	flag.Parse()
	if *root == "" {
		fmt.Fprintln(os.Stderr, "缺少 -root（或环境变量 WORKBENCH_ROOT）⇒ 无法定位台账")
		os.Exit(1)
	}
	var sinceMs int64
	if *since != "" {
		t, err := time.Parse(time.RFC3339, *since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "-since 解析失败: %v\n", err)
			os.Exit(1)
		}
		sinceMs = t.UnixMilli()
	}
	rep, err := ledgeraudit.Scan(*root, sinceMs, 10)
	if err != nil {
		fmt.Fprintf(os.Stderr, "扫描失败: %v\n", err)
		os.Exit(1)
	}
	if *asJSON {
		out, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(out))
	} else {
		fmt.Printf("扫描根: %s（since=%s）\n", *root, orNone(*since))
		fmt.Printf("  行数=%d ｜ 文本可判定=%d ｜ 不一致=%d\n", rep.Rows, rep.Judged, rep.Mismatched)
		for _, g := range rep.GroupsSorted() {
			fmt.Printf("    %4d  %s\n", rep.Groups[g], g)
		}
		for _, s := range rep.Samples {
			when := time.UnixMilli(s.At).Format("01-02 15:04:05")
			fmt.Printf("    [样本] %s %s kind=%s 应为 %s ｜ %.60s\n", when, s.TaskID, s.Kind, s.Want, s.Error)
		}
	}
	if rep.Mismatched == 0 {
		fmt.Printf("LEDGER-ATTRIBUTION=OK judged=%d\n", rep.Judged)
		return
	}
	fmt.Printf("LEDGER-ATTRIBUTION=MISMATCH judged=%d mismatched=%d\n", rep.Judged, rep.Mismatched)
	if *strict {
		os.Exit(2)
	}
}

func orNone(s string) string {
	if s == "" {
		return "全部"
	}
	return s
}
