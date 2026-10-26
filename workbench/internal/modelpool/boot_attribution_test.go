package modelpool

import (
	"testing"

	"workbench/internal/executor"
)

// TestBootAttributionKind 钉住「哪些 boot_failed 才算出口不服务该模型，以及该记什么类别」（T17②/T17②-b）。
//
// 实测形态（round 33 之前每个窗口都出现）：
//
//	dsh: SERVER: our free model does not serve "combo:auto-free" on this egress
//
// 修前它被跳过 ⇒ 不进冷却；按 model_unavailable 记又会被探测治回 ⇒ 归因被抵消。
func TestBootAttributionKind(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		detail   string
		wantKind string
		wantHit  bool
	}{
		{"不服务该模型（实测原文）", string(executor.KindBoot), `dsh: SERVER: our free model does not serve "combo:auto-free" on this egress`, notServedKind, true},
		{"大小写不敏感", string(executor.KindBoot), "upstream said the lane Does Not Serve this model", notServedKind, true},
		{"泛化的 Provider returned error 不归因", string(executor.KindBoot), "dsh: CLIENT_ERROR: Provider returned error", "", false},
		{"别的失败类别不归因", string(executor.KindStalled), "does not serve anything", "", false},
		{"空 detail 不归因", string(executor.KindBoot), "", "", false},
	}
	for _, c := range cases {
		gotKind, gotHit := bootAttributionKind(c.kind, c.detail)
		if gotHit != c.wantHit || gotKind != c.wantKind {
			t.Errorf("%s: bootAttributionKind = (%q,%v), want (%q,%v)", c.name, gotKind, gotHit, c.wantKind, c.wantHit)
		}
	}
	// 类别字符串必须与 executor 常量同一份来源（两处各写一遍必然漂移）。
	if notServedKind != string(executor.KindNotServed) {
		t.Fatalf("notServedKind=%q 必须等于 executor.KindNotServed=%q", notServedKind, executor.KindNotServed)
	}
}
