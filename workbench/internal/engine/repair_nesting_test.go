package engine

import (
	"strings"
	"testing"

	"workbench/internal/model"
)

// TestRepairPromptDoesNotNestPreviousRound 钉住 round 136 · T90 的根因 A。
//
// 实测缺陷（agent 根 task-1793335397343-30a）：prompt.repair-1.md = 675KB → prompt.repair-2.md = 1,347KB，
// 同一任务内按轮数**翻倍**。链路：repairPrompt 把「### 原始任务」写成 task.Prompt，而 scheduleRepair
// 又把新 prompt 写回 task.Prompt ⇒ 第 2 轮的「原始任务」里装着第 1 轮的整份返工 prompt（含上一版产出）。
// 后果不只是磁盘：模型在同一 prompt 里看到同一份文件的两份副本，返工质量与成本同时变差。
func TestRepairPromptDoesNotNestPreviousRound(t *testing.T) {
	const original = "写一份带结论的报告：只谈缓存失效策略"
	// 第 1 轮：task.Prompt 已被上一轮换成返工 prompt（这就是病灶的输入形态）
	round1 := repairPrompt(&model.Task{ID: "t-nest", Prompt: original}, nil,
		&model.VerifyReport{Pass: false, FailReason: "退出码 9"}, 1)
	if !strings.Contains(round1, original) {
		t.Fatalf("第 1 轮 prompt 应含原始任务要求，实得片段=%q", nestHead(round1))
	}
	polluted := &model.Task{ID: "t-nest", Prompt: round1} // scheduleRepair 把 round1 写回了 task.Prompt
	round2 := repairPrompt(polluted, nil, &model.VerifyReport{Pass: false, FailReason: "退出码 9"}, 2)
	// 关键：没有 OriginalPrompt 时只能回退（旧任务行为不变）；一旦登记过就必须用登记值
	if !strings.Contains(round2, "## 返工说明（第 1 轮）") {
		t.Fatal("回退半边失效：无 OriginalPrompt 的旧任务应维持原行为（嵌 task.Prompt），否则是引入新缺陷")
	}
	registered := &model.Task{ID: "t-nest", Prompt: round1, OriginalPrompt: original}
	clean := repairPrompt(registered, nil, &model.VerifyReport{Pass: false, FailReason: "退出码 9"}, 2)
	if strings.Contains(clean, "## 返工说明（第 1 轮）") {
		t.Fatalf("第 2 轮嵌套了第 1 轮的返工 prompt ⇒ 体积按轮数翻倍（T90 病灶复发）；原始任务段=%q", nestHead(clean))
	}
	if !strings.Contains(clean, original) {
		t.Fatalf("登记过之后第 2 轮仍须带上原文，实得片段=%q", nestHead(clean))
	}
	// 体积侧兜住量级：不嵌套时不该出现"约两倍"的跳增
	if len(clean) > len(round1)+2000 {
		t.Fatalf("第 2 轮体积异常：clean=%d round1=%d ⇒ 疑似仍在累积", len(clean), len(round1))
	}
}

// TestOriginalPromptFallsBackForLegacyTasks 钉兼容半边：字段为空（旧任务）必须回退 task.Prompt；
// 把「原始任务」留空会让返工 prompt 丢掉任务本身 —— 那比维持旧缺陷更糟。
func TestOriginalPromptFallsBackForLegacyTasks(t *testing.T) {
	if got := originalPromptOf(nil); got != "" {
		t.Errorf("nil 任务应给空串，实得 %q", got)
	}
	if got := originalPromptOf(&model.Task{Prompt: "旧任务的原始要求"}); got != "旧任务的原始要求" {
		t.Errorf("无登记时应回退 task.Prompt，实得 %q", got)
	}
	if got := originalPromptOf(&model.Task{Prompt: "  ", OriginalPrompt: "真正原文"}); got != "真正原文" {
		t.Errorf("有登记时必须用登记值，实得 %q", got)
	}
	// 空白登记值不算登记（防把空白当原文 ⇒ 原始任务段变空）
	if got := originalPromptOf(&model.Task{Prompt: "唯一可用的要求", OriginalPrompt: "   "}); got != "唯一可用的要求" {
		t.Errorf("登记值为空白时应回退，实得 %q", got)
	}
}

// TestRepairRegistersOriginalPromptOnce 是源文本守卫：登记动作必须留在首轮。
// 为什么用源文本判据而不是行为判据：跑完整返工环要造 harness 状态（本轮已两次踩"凭手感造类型"），
// 而这里的不变量很简单也很关键 —— 「只有首轮、且当前无登记时」才写 OriginalPrompt；
// 若有人把 round==1 条件删掉，第 2 轮会用被污染的 prompt 覆盖掉真正的原文 ⇒ 病灶以另一种形态回来。
func TestRepairRegistersOriginalPromptOnce(t *testing.T) {
	for _, p := range []string{"repair_schedule.go"} {
		raw := readSourceOrFatal(t, p)
		if !strings.Contains(raw, "if round == 1 && strings.TrimSpace(t.OriginalPrompt)") {
			t.Errorf("%s 的原文登记不再限定首轮 ⇒ 后续轮次会用被污染的 prompt 覆盖原文（T90 另一种复发形态）", p)
		}
		if !strings.Contains(raw, "t.OriginalPrompt = task.Prompt") {
			t.Errorf("%s 不再登记原文 ⇒ repairPrompt 只能回退到被污染的 task.Prompt", p)
		}
	}
}

// nestHead 只为报错可读（截断），不参与判定。
func nestHead(s string) string {
	if i := strings.Index(s, "### 原始任务"); i >= 0 {
		e := i + 100
		if e > len(s) {
			e = len(s)
		}
		return s[i:e]
	}
	if len(s) > 100 {
		return s[:100]
	}
	return s
}
