package autonomy

// 本文件收纳自主子系统的纯展示格式化（CLI 与日志共用，避免 cmd 层重复拼串）。

import (
	"fmt"
	"strings"
)

// FormatPlanResult 把一次动态规划结果格式化成人类可读文本。
func FormatPlanResult(out map[string]any) string {
	var b strings.Builder
	if goal, ok := out["goal"].(*Goal); ok && goal != nil {
		b.WriteString("【当前目标】" + goal.Title + "（" + string(goal.Status) + "）\n")
		b.WriteString("意图：" + goal.Intent + "\n")
		b.WriteString(fmt.Sprintf("阶段：%d ／ 进度：%d%% ／ 回退：%d\n\n", len(goal.Stages), goal.ProgressPercent(), goal.Rollbacks))
	}
	planned, _ := out["planned"].(bool)
	if !planned {
		b.WriteString("本轮无可规划草案：" + fmt.Sprint(out["reason"]) + "\n")
		return b.String()
	}
	d, _ := out["draft"].(Draft)
	b.WriteString("【动态规划结果】\n")
	b.WriteString("标题：" + d.Title + "\n")
	b.WriteString("领域：" + d.Domain + " ／ 目标字数：" + fmt.Sprint(d.Words) + "\n")
	b.WriteString("规划依据：" + d.Rationale + "\n")
	b.WriteString("Prompt：\n" + d.Prompt + "\n")
	return b.String()
}
