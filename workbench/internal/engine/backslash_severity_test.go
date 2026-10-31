package engine

import (
	"testing"

	"workbench/internal/artifact"
)

// mustNormalizeForTest 复现生产链路：交付路径先过 artifact.SafePath（上游归一），再交给门禁。
// 判据必须打在「归一之后仍进门」这条真实链路上 —— 我上一版探针直接调谓词、喂未归一的字符串，
// 把 ./docs/ 误判成绕过面（本轮实测：SafePath 把 ./docs/x.md 归一成 docs/x.md，照样进门报问题）。
func mustNormalizeForTest(t *testing.T, p string) string {
	t.Helper()
	sp, err := artifact.SafePath(p)
	if err != nil {
		return "硬拒:" + err.Error()
	}
	return sp
}

// TestBackslashPathEntersContentGate 钉住 T108 第一半（round 155 独立复核 + Lead 亲跑对照）：
// 「内容门与落点门对同一个路径必须同答」。修前实测同一份自称报告的正文：
//
//	docs 反斜杠 报告.md（单个反斜杠）⇒ 内容门 isReportArtifact=false（整道内容检查跳过、问题 0），
//	  而 isDocsMarkdown=true ⇒ 落点门第一遍即 return nil（认定「报告已落在 docs/ 下」）⇒ 两道门同时失效；
//	同一份正文改成正斜杠 ⇒ 内容门报 4 个问题。差异**只来自分隔符** ⇒ 归因干净。
//
// 上游 artifact.SafePath 归一的是**双**反斜杠，单反斜杠原样放行；落盘侧 filepath.FromSlash 对
// 反斜杠是无操作 ⇒ 文件真会写进 docs 反斜杠目录，用户看得见、门禁看不见。
func TestBackslashPathEntersContentGate(t *testing.T) {
	BS := string([]byte{0x5c})
	body := "# 报告\n\n> 待填充\n"
	forms := []string{"docs/报告.md", "docs" + BS + "报告.md", "docs" + BS + "子" + BS + "报告.md"}
	for _, p := range forms {
		a := isReportArtifact(p)
		d := isDocsMarkdown(p)
		pr := len(reportGateProblems(p, []byte(body)))
		if a != d {
			t.Errorf("path=%q 内容门=%v 与落点门=%v 不一致 ⇒ 分隔符差异会让报告同时逃过两道门", p, a, d)
			continue
		}
		if !a {
			t.Errorf("path=%q 两道门都认为不是 docs markdown ⇒ 归一没生效", p)
			continue
		}
		if pr == 0 {
			t.Errorf("path=%q 已进门却报 0 问题 ⇒ 不合格正文被放行", p)
		}
		t.Logf("path=%-26q 内容门=%v 落点门=%v 问题数=%d ｜ SafePath 归一后=%q", p, a, d, pr, mustNormalizeForTest(t, p))
	}
}

// TestSeverityCodeNeedsWordBoundary 钉住 T108 第二半：P0-P3 是短码，必须按独立词判。
// 修前（复核者提出、Lead 复算确认）：severityMarkers 含裸 "P2" 且判定用 Contains ⇒ 一份通篇没分级、
// 只写了「采用 P2P 网络」的技术报告被判「已分级」⇒ 门禁假绿。同文件 :168-171 早就为「提高/其中/降低」
// 写过同样理由，只是当时只给了汉字尺度词、漏了短码。边界只挡 ASCII 字母数字 ⇒ 汉字/标点仍算分隔，
// 「P2级」「P2：」「(P2)」「P2/P3」「【P0】」照常命中：修的是假绿，不许顺手造成误伤。
func TestSeverityCodeNeedsWordBoundary(t *testing.T) {
	notGraded := []string{
		"本方案采用 P2P 网络做分发，不涉及缺陷分级。",
		"端口范围 P22 之外都放行。",
		"这是一段没有任何分级的普通描述。",
	}
	// 已知歧义（本轮**不假装解决**，登记 T109）：形如「变量名 p2 与 p20 冲突」的文本里 p2 是
	// **独立 token**（两侧都是空格）⇒ 任何词边界判据都区分不了"恰好叫 p2 的变量"与"分级 P2"。
	// 再收紧只能引入语义上下文（要求旁有 级/优先级/严重度/P0 共现），而那会误伤「P2：立即修复」
	// 这类合法写法 ⇒ 按「无实据不改行为」，本轮只修**粘连形态**（P2P/P22/1P2：明确假绿、零误伤），
	// 语义级歧义留给带现场样本的那一轮再定。
	for _, s := range notGraded {
		if hasSeverityGrading(s) {
			t.Errorf("被判已分级（假绿）：%q", s)
		}
	}
	graded := []string{
		"缺陷分级：P0 阻断、P1 严重、P2 一般。",
		"严重度：高",
		"优先级说明见下。",
		"[P0] 立即修复",
		"P2/P3 合并处理",
		"【P1】级缺陷两个",
		"定级 P2。",
	}
	for _, s := range graded {
		if !hasSeverityGrading(s) {
			t.Errorf("被判未分级（误伤）：%q", s)
		}
	}
	if !hasSeverityGrading("按 高 / 中 / 低 三档处置") {
		t.Error("「高 / 中 / 低」尺度写法被判未分级 ⇒ 改动破坏了既有分级识别")
	}
	if hasSeverityGrading("这是一段没有任何分级的普通描述") {
		t.Error("普通文本被判已分级 ⇒ 门禁名存实亡")
	}
}
