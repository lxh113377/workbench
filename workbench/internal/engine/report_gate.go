package engine

import (
	"bytes"
	"fmt"
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
//     （pytest / go test / npm / python / pwsh / node / Select-String 等识别词）；
//  4. （round 66 · T67 新增）必须**分级**：全文出现任一最小分级标记即合格 ——
//     严重度 / P0-P3 / 高中低优先级。目标验收语是「五轴审查**分级**出报告，可直接贴 PR」，
//     而 T66 当时只做三节 + 证据最小集，并在注脚明写「分级留待下一轮」，本轮补上这一欠项。
//
// 为什么只判「含标记」而不判条数/字数：分级写法很多（表格、行内、清单），但「一份分级报告必然
// 写下严重度或优先级这个词族」是硬事实；字数与条数是拍脑袋阈值（T66 同一注脚反对过），
// 会把合格报告判红 —— 那种门禁最后一定被人删掉。
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
	// 分级判据（round 66 · T67）：不分级的缺陷清单没法取舍，等于没审 —— 目标要求「分级出报告、
	// 可直接贴 PR」，所以这里挡的是**完全没有分级**的产出，而不是排版。
	if !hasSeverityGrading(text) {
		problems = append(problems, "报告未分级（需 严重度 / P0-P3 / 高中低优先级 任一标记；目标是「五轴审查分级出报告，可直接贴 PR」）")
	}
	return problems
}

// codeArtifactMinBytes 是**代码类产物**的最小体量（round 69 · T73）。
//
// 现场（2026-10-29 黑障解除后第一条 pass）：`task-5412543998c15235` 的产物 `m.py` 只有 **4 字节**，
// verify 是 `python m.py` ⇒ 解释器能启动就 exit 0 ⇒ 判 pass。T66 的报告门禁只覆盖 `docs/*.md`，
// 代码产物仍是「跑得起来就算过」。
//
// 阈值取 16 的依据（**三个真实数据点**，不是拍脑袋）：
//
//	· 4 字节的 `m.py`（占位/截断形态）必须挡下；
//	· 40 字节的容器 E2E 桩 `result.py`（`print("hello from workbench container e2e")`）必须放过 ——
//	  第一版取 64 直接把这个确定性桩判红、把 Autonomy Container E2E 打挂（本轮实测事故）；
//	· 97 字节的 `check_env.py`（本仓里程碑交付物，真实可用的小脚本）必须放过。
//
// 只按体量挡最明显的占位；「代码是否真有用」交给 verify 命令，不在这一层妄判。
// 教训：阈值的上界由**最该放过的真实样本**决定，不是由最该挡下的样本决定 —— 只盯下界会打挂既有验收。
const codeArtifactMinBytes = 16

// codeArtifactExts 是代码类产物扩展名（数据/配置类如 .json/.yaml/.csv 不设体量下限 —— 它们本来就可能很小）。
var codeArtifactExts = []string{".py", ".js", ".ts", ".tsx", ".jsx", ".go", ".sh", ".ps1", ".sql", ".rb", ".java", ".rs", ".c", ".cpp"}

func isCodeArtifact(path string) bool {
	low := strings.ToLower(strings.TrimSpace(path))
	for _, e := range codeArtifactExts {
		if strings.HasSuffix(low, e) {
			return true
		}
	}
	return false
}

// codeArtifactProblems 检查代码产物的最小实质（T73）。
func codeArtifactProblems(path string, body []byte) []string {
	if !isCodeArtifact(path) {
		return nil
	}
	n := len(bytes.TrimSpace(body))
	if n < codeArtifactMinBytes {
		return []string{fmt.Sprintf("代码产物过小（%d 字节 < %d）：不是可交付实现，而是占位或截断", n, codeArtifactMinBytes)}
	}
	return nil
}

// severityMarkers 是分级标记的词族 —— 命中任意一个即认为报告分了级。
//
// 覆盖面刻意宽（表格、行内、清单都认）：要挡的是「压根没分级」，不是「排版不对」。
var severityMarkers = []string{"严重度", "严重等级", "优先级", "高危", "中危", "低危", "P0", "P1", "P2", "P3", "p0", "p1", "p2", "p3"}

// gradingScaleWords 是「高中低」这一档分级尺度的三个取值。
var gradingScaleWords = []string{"高", "中", "低"}

// hasSeverityGrading 判断报告是否真的分了级（round 66 · T67）。
//
// 两条路任一成立即合格：
//
//	① 词族命中：严重度 / 优先级 / 高危 / P0-P3 …
//	② 「高」「中」「低」三个尺度词**都**以独立词出现（前一个字符不是汉字）。
//
// 为什么第 ② 条要判「三个齐全」且要求独立词：裸 Contains(高) 会被「提高」、Contains(中)
// 会被「其中」、Contains(低) 会被「降低」命中 —— 那等于任何中文报告都自动合格（假绿，
// 比判红危险得多，因为它对外宣称「刚加了一道分级门禁」）。三词齐全 + 独立词边界，
// 才与「这是一套分级尺度」这件事同构（对标本仓 ㉑二：判据要与它所判的那个世界一致）。
func hasSeverityGrading(text string) bool {
	for _, m := range severityMarkers {
		if strings.Contains(text, m) {
			return true
		}
	}
	for _, w := range gradingScaleWords {
		if !hasStandaloneWord(text, w) {
			return false
		}
	}
	return true
}

// hasStandaloneWord 判断 word 是否以独立词形态出现在 text 里：命中位置的**前一个字符**
// 不能是汉字（挡「提高/其中/降低」这类构词），后一个字符也不能是汉字（挡「高频/中央/低危」
// 这类词组 —— 低危已由词族单独覆盖，这里不必放进来）。
func hasStandaloneWord(text, word string) bool {
	from := 0
	for {
		i := strings.Index(text[from:], word)
		if i < 0 {
			return false
		}
		at := from + i
		before, after := byte(0), byte(0)
		if at > 0 {
			before = text[at-1]
		}
		if at+len(word) < len(text) {
			after = text[at+len(word)]
		}
		// 汉字在 UTF-8 里首字节 >= 0xE0；只需看相邻字节是否落在汉字区即可判「是否成词」。
		if !isHanByte(before) && !isHanByte(after) {
			return true
		}
		from = at + len(word)
	}
}

// isHanByte 判断一个字节是否可能是中文字符的一部分（UTF-8 三字节序列的首字节 0xE4-0xE9，
// 以及后续续字节 0x80-0xBF —— 邻位只要落在这些区间就说明该词与汉字连写，不是独立词）。
func isHanByte(b byte) bool {
	return b >= 0x80
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
