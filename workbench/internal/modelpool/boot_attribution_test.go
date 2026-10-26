package modelpool

import (
	"testing"

	"workbench/internal/executor"
)

// TestBootFailureAttributable 钉住「哪些 boot_failed 才算车道结构性不可用」（T17②）。
//
// 实测形态（round 33 之前每个窗口都出现）：
//
//	dsh: SERVER: our free model does not serve "combo:auto-free" on this egress
//
// 它被跳过 ⇒ 不进冷却 ⇒ 每个任务继续白试。
func TestBootFailureAttributable(t *testing.T) {
	cases := []struct {
		name   string
		kind   string
		detail string
		want   bool
	}{
		{"不服务该模型（实测原文）", string(executor.KindBoot), `dsh: SERVER: our free model does not serve "combo:auto-free" on this egress`, true},
		{"大小写不敏感", string(executor.KindBoot), "upstream said the lane Does Not Serve this model", true},
		{"泛化的 Provider returned error 不归因", string(executor.KindBoot), "dsh: CLIENT_ERROR: Provider returned error", false},
		{"别的失败类别不归因", string(executor.KindStalled), "does not serve anything", false},
		{"空 detail 不归因", string(executor.KindBoot), "", false},
	}
	for _, c := range cases {
		if got := bootFailureAttributable(c.kind, c.detail); got != c.want {
			t.Errorf("%s: bootFailureAttributable = %v, want %v", c.name, got, c.want)
		}
	}
}
