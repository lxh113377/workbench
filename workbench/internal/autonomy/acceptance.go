package autonomy

// 本文件实现「自主验收」：任务完成后，不依赖人工判断，由机器按目标级验收标准
// 判定"这一步是否算完成、这个目标是否达成"。
//
// 设计原则：
//   - 只用既有台账（任务状态、自评质量、产出字数、领域）计算，不额外跑模型，
//     所以验收本身零成本、零延迟、可重放；
//   - 每条标准都要能指出"现在值是多少、阈值是多少、差多少"（Current/Threshold/Detail），
//     避免出现"验收不通过但不知道为什么"的黑箱；
//   - 验收结论只做建议（Passed），目标状态迁移与预算扣减由 Manager 统一决策，
//     保证"判定"与"动作"分离，便于单测。

import (
	"fmt"
	"strings"

	"workbench/internal/model"
)

// AcceptanceInput 是一次自主验收的输入。
type AcceptanceInput struct {
	Goal  *Goal
	Tasks []*model.Task // 本目标关联的全部任务（含刚完成的那条）
	Now   int64
}

// AcceptanceResult 是一次自主验收的结果。
type AcceptanceResult struct {
	Passed     bool        `json:"passed"`
	Criteria   []Criterion `json:"criteria"`   // 回写 current/passed 后的标准
	TotalWords int         `json:"totalWords"` // 阶段产出总字数
	AvgQuality float64     `json:"avgQuality"` // 有自评任务的均分
	DoneCount  int         `json:"doneCount"`  // 已完成阶段任务数
	// VerifiedCount 是已通过阶段中"真实执行验收通过"的数量（P3 接线）。
	// 与 DoneCount 区分：done 只表示流程跑完，verified 才表示产物真能跑。
	VerifiedCount int     `json:"verifiedCount"`
	VerifiedRatio float64 `json:"verifiedRatio"`
	Distinct      int     `json:"distinct"` // 覆盖领域数
	Reason        string  `json:"reason"`
}

// Evaluate 对目标做一次完整验收：逐条计算标准现值并判定是否全部通过。
func Evaluate(in AcceptanceInput) AcceptanceResult {
	res := AcceptanceResult{}
	goal := in.Goal
	if goal == nil {
		res.Reason = "目标为空，无法验收"
		return res
	}
	// 只统计属于本目标的已完成任务（按 stages 里记录的 taskId 关联）。
	taskByID := map[string]*model.Task{}
	for _, t := range in.Tasks {
		if t != nil {
			taskByID[t.ID] = t
		}
	}
	var qSum float64
	var qN int
	domains := map[string]bool{}
	for _, st := range goal.Stages {
		if !st.Passed {
			continue
		}
		res.DoneCount++
		res.TotalWords += st.Words
		if st.Quality > 0 {
			qSum += float64(st.Quality)
			qN++
		}
		if t, ok := taskByID[st.TaskID]; ok {
			if t.Domain != "" {
				domains[t.Domain] = true
			}
			// 任务表必须传进来，否则这里的 Verified 永远读不到（现场缺陷：c-verified 恒 0）。
			if t.Verified {
				res.VerifiedCount++
			}
		}
	}
	if qN > 0 {
		res.AvgQuality = float64(int(qSum/float64(qN)*100+0.5)) / 100
	}
	res.Distinct = len(domains)
	if res.DoneCount > 0 {
		res.VerifiedRatio = float64(int(float64(res.VerifiedCount)/float64(res.DoneCount)*100+0.5)) / 100
	}

	crits := make([]Criterion, 0, len(goal.Criteria))
	for _, c := range goal.Criteria {
		crits = append(crits, evalCriterion(c, res))
	}
	res.Criteria = crits
	// 全部通过才算通过；人工判据（manual）不参与自动判定。
	res.Passed = true
	var failed []string
	for _, c := range crits {
		if c.Kind == CriterionManual {
			continue
		}
		if !c.Passed {
			res.Passed = false
			failed = append(failed, fmt.Sprintf("%s（现 %s / 需 %s）", c.Desc, fmtNum(c.Current), fmtNum(c.Threshold)))
		}
	}
	if res.Passed {
		res.Reason = fmt.Sprintf("全部 %d 条验收标准通过（阶段 %d 个 / 均分 %.2f / 总字数 %d）",
			len(crits), res.DoneCount, res.AvgQuality, res.TotalWords)
	} else {
		res.Reason = "验收未通过：" + strings.Join(failed, "；")
	}
	return res
}

// evalCriterion 计算单条标准的现值与通过与否。
func evalCriterion(c Criterion, res AcceptanceResult) Criterion {
	switch c.Kind {
	case CriterionTaskDone:
		c.Current = float64(res.DoneCount)
	case CriterionMinQuality:
		c.Current = res.AvgQuality
	case CriterionMinWords:
		c.Current = float64(res.TotalWords)
	case CriterionDistinctDomains:
		c.Current = float64(res.Distinct)
	case CriterionVerified:
		c.Current = float64(res.VerifiedCount)
	case CriterionVerifiedRatio:
		c.Current = res.VerifiedRatio
	case CriterionManual:
		c.Detail = "人工判据：需人工确认后才计入完成"
		return c
	default:
		c.Detail = "未知判据类型，按未通过处理"
		c.Passed = false
		return c
	}
	c.Passed = c.Current >= c.Threshold
	if c.Passed {
		c.Detail = fmt.Sprintf("达到阈值（%s ≥ %s）", fmtNum(c.Current), fmtNum(c.Threshold))
	} else {
		c.Detail = fmt.Sprintf("未达阈值（%s < %s，还差 %s）", fmtNum(c.Current), fmtNum(c.Threshold), fmtNum(c.Threshold-c.Current))
	}
	return c
}

// CountWords 统计中英文混合字数（导出给 Engine 记录阶段字数）。
//
// 口径：中文按字计（每个汉字 1），连续英文/数字串算 1 个词，标点与空白不计。
// 这样 1000 汉字 ≈ 1000 字，与模板 OutputSpec.MinWords 口径一致。
func CountWords(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n := 0
	inWord := false
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			inWord = false
		case isCJK(r):
			n++
			inWord = false
		case isPunct(r):
			inWord = false
		default:
			if !inWord {
				n++
				inWord = true
			}
		}
	}
	return n
}

// isCJK 判断是否为中日韩文字。
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0xF900 && r <= 0xFAFF) || (r >= 0x3040 && r <= 0x30FF)
}

// isPunct 判断是否为常见中英文标点（不计入字数）。
func isPunct(r rune) bool {
	switch r {
	case '，', '。', '！', '？', '；', '：', '、', '（', '）', '【', '】', '《', '》',
		'“', '”', '‘', '’', '—', '…', ',', '.', '!', '?', ';', ':', '(', ')', '[', ']',
		'"', '\'', '`', '-', '_', '/', '\\', '|', '*', '#', '>', '<', '=':
		return true
	}
	return false
}

// fmtNum 把 float64 格式化成"最多两位小数、去掉尾零"的短串。
func fmtNum(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

// QualityVerdict 是单阶段质量判定：是否可计入目标进度。
//
// 与目标级验收不同，这里是"这一步有没有走歪"的即时判定，用于触发回退：
//   - 任务成功但自评低于 MinQuality => 不计入阶段进度（Rollback）；
//   - 任务成功且质量达标 => 计入阶段。
type QualityVerdict struct {
	Pass       bool   `json:"pass"`
	Quality    int    `json:"quality"`
	MinQuality int    `json:"minQuality"`
	Reason     string `json:"reason"`
}

// JudgeStage 判定一个阶段是否通过。
func JudgeStage(t *model.Task, minQuality int) QualityVerdict {
	v := QualityVerdict{Quality: qualityOf(t), MinQuality: minQuality}
	if t == nil {
		v.Reason = "任务不存在"
		return v
	}
	if minQuality <= 0 {
		minQuality = 6
		v.MinQuality = minQuality
	}
	if v.Quality >= minQuality {
		v.Pass = true
		v.Reason = fmt.Sprintf("自评 %d/10 ≥ 下限 %d", v.Quality, minQuality)
		return v
	}
	v.Reason = fmt.Sprintf("自评 %d/10 < 下限 %d，本阶段不计入目标进度", v.Quality, minQuality)
	return v
}
