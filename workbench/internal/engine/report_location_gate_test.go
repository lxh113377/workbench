package engine

import (
	"os"
	"strings"
	"testing"

	"workbench/internal/artifact"
)

// TestReportLocationProblems 钉住「自称报告的交付物必须落在 docs/ 下」（round 71 · T74）。
//
// 现场：报告内容门禁（T66）只作用于 `docs/` 下的 .md ⇒ 只要把同一份报告写到根目录，它就整条绕开门禁。
// 目标验收语是「报告与优化建议**放置于指定地点**」，所以落点本身是判据。
//
// ⚠️ 第一版按 `task.OutputFormat == "md"` 一刀切，把**文章/长文类**任务也逼进 docs/，
// 当场打红 TestClosedLoopThroughRunOne 与 TestCompleteTaskDoneOnVerifyPass 两处既有契约 ⇒ 改为**内容判定**
// （复用 claimsReport：正文含三节任一标题，或文件名含 报告/审查/review）。
func TestReportLocationProblems(t *testing.T) {
	mk := func(p, body string) artifact.File { return artifact.File{Path: p, Body: []byte(body)} }
	cases := []struct {
		name  string
		files []artifact.File
		want  bool
	}{
		{"根目录含三节标题（自称报告）必须挡", []artifact.File{mk("m.md", "## 结论\n\n正文")}, true},
		{"文件名含「报告」必须挡", []artifact.File{mk("报告.md", "随便写点什么")}, true},
		{"文件名含「审查」必须挡", []artifact.File{mk("code-审查.md", "内容")}, true},
		{"子目录但不是 docs/ 也要挡", []artifact.File{mk("out/review.md", "内容")}, true},
		{"反斜杠路径的 docs/ 认", []artifact.File{mk(`docs\五轴审查-正确性.md`, "## 结论")}, false},
		{"docs/ 下报告 + 根目录报告 ⇒ 落点合格", []artifact.File{mk("报告.md", "内容"), mk("docs/五轴审查.md", "## 结论")}, false},
		{"文章/长文类（无报告标记）不误伤", []artifact.File{mk("a.md", "# Abstract\n\n本文……")}, false},
		{"闭环验证的 loop.md 不误伤", []artifact.File{mk("loop.md", "# 闭环产出\n\n该文件由闭环验证生成。")}, false},
		{"代码产物不受落点管束", []artifact.File{mk("tool.py", "print(1)")}, false},
		{"非 md 报告名也不管", []artifact.File{mk("报告.json", "{}")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reportLocationProblems(tc.files)
			if tc.want && len(got) == 0 {
				t.Fatalf("应判落点问题（files=%v）", tc.files)
			}
			if !tc.want && len(got) > 0 {
				t.Fatalf("不该判问题，实得 %v", got)
			}
		})
	}
}

// TestReportLocationGateRunsAfterVerifyOnlyOnPass 是顺序与条件守卫（与 T73 同规矩）。
//
// 放真实验收之前 ⇒ 会把「失败因脚本真实退出码」偷换成「落点不符」；
// 不分「已通过」⇒ 失败任务的真实原因同样会被盖掉。
func TestReportLocationGateRunsAfterVerifyOnlyOnPass(t *testing.T) {
	src, err := os.ReadFile("artifacts.go")
	if err != nil {
		t.Fatalf("读 artifacts.go 失败: %v", err)
	}
	body := string(src)
	locIdx := strings.Index(body, "if loc := reportLocationProblems(res.Files); len(loc) > 0 {")
	runIdx := strings.Index(body, "vrep := verify.Run(")
	if locIdx < 0 || runIdx < 0 {
		t.Fatalf("锚点缺失（loc=%d run=%d）", locIdx, runIdx)
	}
	if locIdx < runIdx {
		t.Fatal("落点门禁必须在真实验收**之后**（否则会把失败原因偷换成落点不符）")
	}
	// 必须落在 out.Pass 分支内：找它之前最近的 `if out.Pass {`，再确认中间**没有**外层块闭合行。
	//
	// 为什么用「行级精确判定」而不是括号计数：`out.Pass` 块内还嵌着 `if len(tiny) > 0 { ... }`，
	// 数 `}` 会把内层闭合也算进去 ⇒ 判据自己误报（本轮第一版就是这么红的）。
	// 外层块闭合的标志是**恰好一个 tab 的 `}`**（内层是 `\t\t}`），行级比对才与结构同构。
	prefix := body[:locIdx]
	lastPass := strings.LastIndex(prefix, "if out.Pass {")
	if lastPass < 0 {
		t.Fatal("落点门禁必须写在 `if out.Pass {` 分支内（失败任务的真实原因不得被盖掉）")
	}
	for _, ln := range strings.Split(prefix[lastPass:], "\n") {
		if ln == "\t}" {
			t.Fatal("落点门禁被写到了 out.Pass 块**之外**（失败任务的真实原因会被盖掉）")
		}
	}
	if !strings.Contains(body, "交付物落点不符：") {
		t.Fatal("降级原因必须用可区分前缀「交付物落点不符：」")
	}
}
