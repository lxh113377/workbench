package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workbench/internal/artifact"
)

// TestAuditReportPassesRealGate 让**本轮那份五轴审查报告本体**过引擎里真实存在的报告门禁
// （reportGateProblems：三节 + 可核查证据 + 分级，report_gate.go:38）。
//
// 为什么必须写这条（round 129 · 我自己的判据事故）：我写完报告后跑了
//
//	go test -run TestReportGateOnThisAudit
//
// 得到末行 "PASS" 就声称"用引擎自身的门禁验收了这份报告" —— **那个 PASS 是假的**：
//
//	① 该测试名当时不存在 ⇒ go test 输出 "no tests to run" 但退出码 0 ⇒ PASS 来自空集；
//	② reportGateProblems 根本不在我当时读的文件里（我读的是 artifacts.go）。
//
// ⇒ 这正是我在报告「元发现」里写的那类错误（判据没作用在被检对象上却报绿）。
//
//	本条测试把那句空话换成真判据；配套负控见 TestAuditReportGateHasTeeth。
func TestAuditReportPassesRealGate(t *testing.T) {
	body := readAuditReport(t)
	problems := reportGateProblems("docs/工作空间工程质量-五轴审查-2026-10-30.md", body)
	if len(problems) > 0 {
		t.Fatalf("本轮交付的五轴审查报告未过自家门禁（三节/证据/分级）：\n  %s",
			strings.Join(problems, "\n  "))
	}
	// 落点门禁同样必须过（T74）：报告自称报告 ⇒ 必须落在 docs/ 下
	// 构造 artifact.File 时路径用仓库相对形态，与引擎交付时一致
	if loc := reportLocationProblems(reportFilesForGate(body)); len(loc) > 0 {
		t.Fatalf("报告落点不合格：%s", strings.Join(loc, "；"))
	}
}

// reportFilesForGate 把报告装成门禁函数期望的入参形态。
// 合规样本：路径在 docs/ 下 ⇒ reportLocationProblems 第一遍即放行。
func reportFilesForGate(body []byte) []artifact.File {
	return []artifact.File{{Path: "docs/工作空间工程质量-五轴审查-2026-10-30.md", Body: body}}
}

// readAuditReport 读报告本体。**缺文件即判红而不是 Skip**（判据必须作用在真实对象上；
// Skip 会让"文件被改名/删掉"退化成"测试通过"，那就是本轮假绿的复现形态）。
func readAuditReport(t *testing.T) []byte {
	t.Helper()
	// go test 的工作目录 = 包目录（internal/engine），而上跳三级才是**仓库根**：
	// 本仓结构是「仓库根 D:/workdesktop + 模块子目录 workbench/」，报告落在仓库根的 docs/ 下，
	// 不是模块内的 workbench/docs/（第一版按模块根算 ⇒ 读不到文件，判红得对，但原因错了）。
	p, err := filepath.Abs(filepath.Join("..", "..", "..", "docs", "工作空间工程质量-五轴审查-2026-10-30.md"))
	if err != nil {
		t.Fatalf("解析报告路径失败：%v", err)
	}
	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读不到被检报告 %s：%v —— 判据不许在空对象上过（round 129 的假绿正是这个形态）", p, err)
	}
	if len(strings.TrimSpace(string(body))) < 1000 {
		t.Fatalf("报告体量 %d 字节可疑（门禁判据要求内容级检查，不只看存在性）", len(body))
	}
	return body
}

// TestAuditReportGateHasTeeth 是本条判据的负控（#44 ⑧：交付件必须含负向对照，
// 否则「测试通过」可能只是恒绿或空集 —— 本轮我刚好就吃过一次这样的假绿）。
//
// 做法：拿**同一份报告**逐项抽掉门禁要求，要求每次都真的判红；
// 若某项抽掉后仍判绿 ⇒ 说明该判据对本报告根本没生效（等于没检查）。
func TestAuditReportGateHasTeeth(t *testing.T) {
	body := string(readAuditReport(t))
	const path = "docs/工作空间工程质量-五轴审查-2026-10-30.md"
	mutants := []struct {
		name string
		body string
	}{
		{"抽掉「## 门禁建议」整节", dropSection(body, "## 门禁建议")},
		{"抽掉「## 证据」整节", dropSection(body, "## 证据")},
		{"把证据里的 file:line 与命令全删", stripEvidenceMarkers(t, body)},
		{"把分级词全删", stripGrading(body)},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			if m.body == body {
				t.Fatal("变异未生效（原文里找不到可删的部分）：这条负控无法证明判据有牙")
			}
			if got := reportGateProblems(path, []byte(m.body)); len(got) == 0 {
				t.Fatalf("抽掉 %q 之后仍判绿 ⇒ 该判据对本报告没生效", m.name)
			}
		})
	}
	// 落点负控：同一份内容若落在仓库根（非 docs/）⇒ T74 必须判红
	rootFiles := []artifact.File{{Path: "工作空间工程质量-五轴审查-2026-10-30.md", Body: []byte(body)}}
	if got := reportLocationProblems(rootFiles); len(got) == 0 {
		t.Fatal("自称报告却落在根目录未被判红 ⇒ T74 落点门禁对本报告无牙")
	}
}

// dropSection 删掉某个二级标题小节（从该标题行到下一个二级标题或文件尾）。
func dropSection(text, sec string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), sec) {
			skipping = true
			continue
		}
		if skipping && strings.HasPrefix(strings.TrimSpace(l), "## ") {
			skipping = false
		}
		if !skipping {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// stripEvidenceMarkers 模拟「证据节只写空话」：删掉所有 file:line 与可复跑命令识别词。
//
// 关键：剥离**必须用门禁自己那两份判据源**（fileLinePattern / commandMarkers，report_gate.go:292-295），
// 而不是手抄一份关键词清单。第一版正是手抄漏了几个标记词，变异没把证据剥干净 ⇒
// 抽完之后证据节仍然"有标记" ⇒ 门禁正确地判绿 ⇒ 负控 FAIL。
// 那不是"判据无牙"，而是**变异不完整**——两种情况必须在报错文本里区分开，
// 否则下一次会把"我的变异写漏"误读成"门禁形同虚设"，往错误方向改代码。
// 因此下面除了剥离，还**自校验**："证据节真的一个标记都不剩"才交回给门禁判。
func stripEvidenceMarkers(t *testing.T, text string) string {
	t.Helper()
	out := fileLinePattern.ReplaceAllString(text, "（已剥离）")
	for _, m := range commandMarkers {
		out = replaceAllFold(out, m)
	}
	if sec := sectionBody(out, secEvidence); hasSection(out, secEvidence) && evidenceHasMarker(sec) {
		t.Fatalf("变异未剥离干净（不能据此说门禁无牙）：证据节仍残留标记。残留片段=%q", truncateForTest(sec, 160))
	}
	return out
}

// replaceAllFold 按大小写无关地替换（commandMarkers 存的是小写形态，而正文里可能是 "Select-String"）。
func replaceAllFold(text, needle string) string {
	low := strings.ToLower(text)
	var b strings.Builder
	from := 0
	for {
		j := strings.Index(low[from:], needle)
		if j < 0 {
			b.WriteString(text[from:])
			break
		}
		b.WriteString(text[from : from+j])
		b.WriteString("（已剥离）")
		from += j + len(needle)
	}
	return b.String()
}

// truncateForTest 只为报错可读性，不参与判定。
func truncateForTest(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// stripGrading 删掉全部分级词族，模拟"不分级"。
func stripGrading(text string) string {
	out := text
	for _, m := range severityMarkers {
		out = strings.ReplaceAll(out, m, "（略）")
	}
	return out
}
