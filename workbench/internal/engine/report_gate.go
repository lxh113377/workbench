package engine

import (
	"regexp"
	"strings"
)

// reportGateProblems 检查「报告类产物」的内容是否够格交付（round 65 · T66）。
//
// 为什么需要它（live 实测，2026-10-29）：两条通过验收的自主任务，verify 命令都是
//
//	pwsh -NoProfile -Command "if (Test-Path docs/五轴审查-*.md) { exit 0 } else { exit 1 }"
//
// ⇒ **文件存在即 pass**。同一批交付里：
//
//	· task-1793209625820-9240 的报告 103B，「## 证据」整节内容是「无。」   —— 判据说它合格；
//	· task-1793210078818-8614 的报告 1032B，含 file:app/server.py:128 + 可复跑命令 —— 也合格。
//
// 目标的验收要求是「五轴审查分级出报告，可直接贴 PR，附合并结论与门禁建议」，
// 而存在性判据分不出「写了」和「写对了」。本函数把 T59 的**建议**升级为**可执行最小门禁**。
//
// 口径（刻意保守，只挡明确不合格的）：
//  1. 只作用于 `docs/` 下的 `.md`（目标里"报告放指定地点"的那一类）；代码/配置产物不受影响；
//  2. 必须含三节标题，逐字：`## 结论` / `## 证据` / `## 门禁建议`；
//  3. 「证据」节必须给出 ≥1 条**可核查证据**：`文件:行号`（如 app/server.py:128）或可复跑命令
//     （pytest / go test / npm / python / pwsh / node / Select-String 等识别词）。
//
// 不做「字数下限」与「分级」硬性要求：前者是拍脑袋阈值，后者留待下一轮（先让最小门禁跑起来）。
func reportGateProblems(path string, body []byte) []string {
	if !isReportArtifact(path) {
		return nil
	}
	text := string(body)
	if !claimsReport(path, text) {
		return nil
	}
	var problems []string
	for _, sec := range reportSections {
		if !hasSection(text, sec) {
			problems = append(problems, "报告缺少小节："+sec)
		}
	}
	if hasSection(text, secEvidence) && !evidenceHasMarker(sectionBody(text, secEvidence)) {
		problems = append(problems, "「"+secEvidence+"」节没有任何可核查证据（需 文件:行号 或可复跑命令）")
	}
	return problems
}

const (
	secConclusion = "## 结论"
	secEvidence   = "## 证据"
	secGate       = "## 门禁建议"
)

var reportSections = []string{secConclusion, secEvidence, secGate}

// claimsReport 判断这份产物**是不是在自述为报告**。
//
// 为什么需要这一层（2026-10-29 实测事故）：docs/ 下的 .md 不都是报告 —— 项目的 README、说明、变更记录
// 同样是 .md；对它们套「三节 + 证据」是**误伤**。第一次实现就踩了：既有护栏测试
// TestMaterializeArtifactsRespectsGuard 的 fixture 正是 `docs/hello.md`（内容只有一行标题），
// 被新门禁判成「报告内容缺陷」，全仓回归随即变红。
//
// 判据（保守）：① 正文里**按行首**出现任一必备小节 ⇒ 它在自称报告；
//
//	② 或文件名含「报告 / 审查 / review」⇒ 形态上就是报告。
//
// 两者都不满足的普通文档一律放行 —— 门禁只挡「自称是报告却写不成报告」的那种。
func claimsReport(path, text string) bool {
	for _, h := range reportSections {
		if hasSection(text, h) {
			return true
		}
	}
	low := strings.ToLower(path)
	return strings.Contains(low, "报告") || strings.Contains(low, "审查") || strings.Contains(low, "review")
}

// isReportArtifact：报告类 = docs/ 下的 .md（目标指定的报告地点与格式）。
func isReportArtifact(path string) bool {
	p := strings.ToLower(strings.TrimSpace(path))
	if !strings.HasSuffix(p, ".md") {
		return false
	}
	return strings.HasPrefix(p, "docs/")
}

// hasSection：按**行首**匹配，避免正文里引用小节名被误当成结构（实测模型会写"见 ## 证据"）。
func hasSection(text, header string) bool {
	for _, ln := range strings.Split(text, "\n") {
		if strings.TrimSpace(ln) == header {
			return true
		}
	}
	return false
}

// sectionBody 取某节到下一个二级标题之间的正文。
func sectionBody(text, header string) string {
	lines := strings.Split(text, "\n")
	start := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) == header {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return ""
	}
	var b strings.Builder
	for _, ln := range lines[start:] {
		if strings.HasPrefix(strings.TrimSpace(ln), "## ") {
			break
		}
		b.WriteString(ln)
		b.WriteString("\n")
	}
	return b.String()
}

// fileLinePattern 匹配「文件:行号」（允许路径含 / \ . _ -）。
var fileLinePattern = regexp.MustCompile(`[A-Za-z0-9_\-./\\]+\.[A-Za-z0-9]+:[0-9]+`)

// commandMarkers 是可复跑命令的识别词（够宽以容纳实际写法，够窄以排除空话）。
var commandMarkers = []string{"pytest", "go test", "npm ", "pnpm ", "python ", "python3 ", "node ", "pwsh", "powershell", "select-string", "grep ", "curl ", "dotnet test", "cargo test"}

func evidenceHasMarker(section string) bool {
	if fileLinePattern.MatchString(section) {
		return true
	}
	low := strings.ToLower(section)
	for _, m := range commandMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}
