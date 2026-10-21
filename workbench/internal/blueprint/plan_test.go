package blueprint

// blueprint 包的契约测试。
//
// 覆盖三条硬约束（每条都对应一个现场会真出问题的失效形态）：
//  1. 解析要能吃下"带寒暄的输出"（免费车道小模型从不只输出 JSON）；
//  2. 不安全路径 / 非法解释器必须被 Normalize 丢弃（否则执行模型据此落盘会写穿沙箱）；
//  3. RenderTask 必须把验收命令带进执行指令（否则方案→执行这一跳断了，闭环失效）。

import (
	"strings"
	"testing"
)

func TestParseStrictJSON(t *testing.T) {
	raw := `{"approach":"先造骨架再补数据",` +
		`"deliverables":[{"path":"out/report.md","what":"结论与依据"}],` +
		`"steps":[{"order":1,"do":"写骨架","done":"文件存在"},{"order":2,"do":"补数据"}],` +
		`"verify":[{"cmd":"pwsh -NoProfile -Command exit 0","expectExit":0}],` +
		`"risks":["别写成作文"]}`
	bp, problems, err := Parse(raw)
	if err != nil {
		t.Fatalf("严格 JSON 必须解析成功: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("合法方案不应产生问题清单: %v", problems)
	}
	if bp.Approach == "" || len(bp.Deliverables) != 1 || len(bp.Steps) != 2 || len(bp.Verify) != 1 {
		t.Fatalf("字段解析不完整: %+v", bp)
	}
	if bp.Steps[0].Order != 1 || bp.Steps[1].Order != 2 {
		t.Fatalf("步骤序号应被规范为 1..n: %+v", bp.Steps)
	}
}

func TestParseToleratesChatter(t *testing.T) {
	raw := "好的，这是我的方案：\n```json\n" +
		`{"approach":"分两步走","deliverables":[{"path":"a.md","what":"x"}],` +
		`"steps":[{"do":"做A"}],"verify":[{"cmd":"python a.py","expectExit":0}]}` +
		"\n```\n希望有帮助。"
	bp, problems, err := Parse(raw)
	if err != nil {
		t.Fatalf("带寒暄的输出必须能截取解析: %v", err)
	}
	if bp == nil || len(bp.Steps) != 1 {
		t.Fatalf("截取后方案不完整: %+v", bp)
	}
	found := false
	for _, p := range problems {
		if strings.Contains(p, "截取") {
			found = true
		}
	}
	if !found {
		t.Fatalf("截取降级必须记录问题清单: %v", problems)
	}
}

func TestParseEmptyFails(t *testing.T) {
	if _, _, err := Parse("   "); err == nil {
		t.Fatal("空输出必须报错（不能拿空方案去编排执行）")
	}
	if _, _, err := Parse("这是一段纯文字，没有任何 JSON"); err == nil {
		t.Fatal("无 JSON 必须报错")
	}
	if _, _, err := Parse(`{"approach":""}`); err == nil {
		t.Fatal("空方案（无交付/步骤/验收）必须报错")
	}
}

func TestNormalizeRejectsUnsafePathsAndInterpreters(t *testing.T) {
	bp := &Blueprint{
		Approach: "x",
		Deliverables: []Deliverable{
			{Path: "../../etc/passwd"},
			{Path: "C:/Windows/system32/x.exe"},
			{Path: "/abs/path.md"},
			{Path: "ok/file.md"},
		},
		Verify: []VerifyCmd{
			{Cmd: "rm -rf /", ExpectExit: 0},
			{Cmd: "python ok.py", ExpectExit: 0},
			{Cmd: "", ExpectExit: 0},
		},
	}
	problems := bp.Normalize()
	if len(bp.Deliverables) != 1 || bp.Deliverables[0].Path != "ok/file.md" {
		t.Fatalf("越界交付物必须被丢弃，只剩相对安全路径: %+v", bp.Deliverables)
	}
	if len(bp.Verify) != 1 || bp.Verify[0].Cmd != "python ok.py" {
		t.Fatalf("非法解释器与空命令必须被丢弃: %+v", bp.Verify)
	}
	if len(problems) < 4 {
		t.Fatalf("每个丢弃都要留问题记录，实际 %d 条: %v", len(problems), problems)
	}
}

func TestRenderTaskCarriesPlanToExecutor(t *testing.T) {
	bp := &Blueprint{
		Approach:     "先骨架后内容",
		Deliverables: []Deliverable{{Path: "out/a.md", What: "结论"}},
		Steps:        []Step{{Order: 1, Do: "写骨架", Done: "文件存在"}},
		Verify:       []VerifyCmd{{Cmd: "python check.py", ExpectExit: 0, Why: "证明可运行"}},
		Risks:        []string{"不要输出占位"},
	}
	out := RenderTask(bp, "标题", "原始需求正文")
	for _, want := range []string{"out/a.md", "写骨架", "python check.py", "expectExit=0", "不要输出占位", "原始需求正文", "先骨架后内容"} {
		if !strings.Contains(out, want) {
			t.Fatalf("执行指令缺少 %q:\n%s", want, out)
		}
	}
	if RenderTask(nil, "t", "p") != "p" {
		t.Fatal("无方案时必须原样回退到原始 prompt")
	}
}

func TestPlanPromptInjectsFailureEvidence(t *testing.T) {
	p := PlanPrompt("项目目标X", "标题Y", "任务Z", 800, "verify 退出码 1：python a.py 报 SyntaxError")
	for _, want := range []string{"项目目标X", "标题Y", "任务Z", "SyntaxError", "上一轮失败证据", "重新"} {
		if !strings.Contains(p, want) {
			t.Fatalf("方案提示词缺少 %q", want)
		}
	}
	if strings.Contains(p, "省略") && !strings.Contains(p, "禁止写【省略】") {
		t.Fatal("提示词应明确禁止占位而不是自己使用占位")
	}
	first := PlanPrompt("", "t", "p", 0, "")
	if strings.Contains(first, "上一轮失败证据") {
		t.Fatal("首轮方案不应包含失败证据段（无中生有会让模型去解决不存在的问题）")
	}
}

func TestSummaryLine(t *testing.T) {
	if got := (*Blueprint)(nil).SummaryLine(); got != "无方案" {
		t.Fatalf("nil 方案摘要 = %q", got)
	}
	bp := &Blueprint{Approach: "思路", Steps: []Step{{Do: "a"}}, Verify: []VerifyCmd{{Cmd: "python a.py"}}}
	if !strings.Contains(bp.SummaryLine(), "模型方案") {
		t.Fatalf("摘要应标明方案来源: %s", bp.SummaryLine())
	}
	bp.Degraded = true
	if !strings.Contains(bp.SummaryLine(), "规则兜底") {
		t.Fatalf("退化方案必须显式标注: %s", bp.SummaryLine())
	}
}
