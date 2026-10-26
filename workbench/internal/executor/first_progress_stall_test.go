package executor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFirstProgressSecondsClamps 钉住首进展预算的取值口径（base/3，夹在 [30,120]）。
func TestFirstProgressSecondsClamps(t *testing.T) {
	cases := []struct{ base, want int }{
		{0, 120}, // 缺省 base=420 ⇒ 140 ⇒ 上限 120
		{60, 30}, // 20 ⇒ 下限 30
		{90, 30},
		{180, 60},
		{420, 120}, // 140 ⇒ 上限 120
		{3600, 120},
	}
	for _, c := range cases {
		if got := FirstProgressSeconds(c.base); got != c.want {
			t.Errorf("FirstProgressSeconds(%d) = %d, want %d", c.base, got, c.want)
		}
	}
}

// TestIsEarlyStall 钉住「零进展」判定：预算内不判；到点且既无事件也无产出才判；
// 有过事件或有过产出都不判（交给 IsStalled 按「最近进展」口径）。
//
// 为什么需要（round 13 实测）：一次自动任务在计划阶段跑了 452,399 ms 才被回收，而这期间一步都没推进；
// 对「零进展」形态等满 StallSeconds 只是烧配额。本判据就是那条「早回收早轮换」的机器口径。
func TestIsEarlyStall(t *testing.T) {
	start := int64(1_000_000)
	budgetMs := int64(FirstProgressSeconds(420)) * 1000

	if IsEarlyStall(start, start, "", 420, start+budgetMs-1) {
		t.Error("预算未到不该判零进展")
	}
	if !IsEarlyStall(start, start, "", 420, start+budgetMs) {
		t.Error("到点且零进展应判")
	}
	if IsEarlyStall(start, start+1, "", 420, start+budgetMs*2) {
		t.Error("有过事件不该按零进展判（应走 IsStalled 的最近进展口径）")
	}
	if IsEarlyStall(0, 0, "", 420, start+budgetMs) {
		t.Error("startedMs 未知时不得判")
	}

	// 有过产出文件（扩展名在 outputExtensions 白名单内）也不该按零进展判。
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsEarlyStall(start, start, dir, 420, start+budgetMs) {
		t.Error("已有产出文件时不该按零进展判")
	}
}

// TestIsSubstantiveEvent 钉住「握手帧不算实质进展」这条口径。
//
// 为什么必须钉（round 14 实测）：DSH 子进程一启动就吐 status 握手帧，而 consume 对每条事件都刷新活跃度，
// 于是「启动后零进展」的提前回收一次都没触发（尝试跑满 317 s+）。把「哪类事件才算起步」写成判据，
// 才能防止它再次退化回「握手即进展」。
func TestIsSubstantiveEvent(t *testing.T) {
	cases := []struct {
		name string
		ev   Event
		want bool
	}{
		{"turn_start 握手", Event{Type: "status", Phase: "turn_start"}, false},
		{"step_start 握手", Event{Type: "status", Phase: "step_start"}, false},
		{"step_end", Event{Type: "status", Phase: "step_end"}, true},
		{"status 带 usage", Event{Type: "status", Phase: "step_start", Usage: &Usage{OutputTokens: 1}}, true},
		{"text", Event{Type: "text", Text: "x"}, true},
		{"thinking", Event{Type: "thinking", Text: "x"}, true},
		{"final", Event{Type: "final", Text: "x"}, true},
		{"tool_call", Event{Type: "tool_call"}, true},
		{"error", Event{Type: "error"}, true},
		{"session", Event{Type: "session"}, false},
		{"未知类型", Event{Type: "weird"}, false},
	}
	for _, c := range cases {
		if got := IsSubstantiveEvent(c.ev); got != c.want {
			t.Errorf("%s: IsSubstantiveEvent = %v, want %v", c.name, got, c.want)
		}
	}
}
