package evaluate

import (
	"strings"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/model"
)

// TestFuzzyGateUsesTitleAndWindow 钉住模糊判据的两条修正（round 45 · T32，独立排查实锤）：
//
//  1. 只比标题：比全量 title+prompt 时，长脚手架模板的常量段把相似度顶到 0.92~0.997
//     （9 条模板 859 条永久误杀），量的其实是模板常量而不是主题变量；
//  2. 同口径加时间窗：否则「同标题不同 prompt」仍会被永久封杀（队列饿死换个入口复发）。
func TestFuzzyGateUsesTitleAndWindow(t *testing.T) {
	cfg := config.DefaultConfig().Eval
	cfg.ExactDupWindowMinutes = 60
	now := time.Now().UnixMilli()

	// ① 同模板不同主题（标题只差主题段）⇒ 窗内也不得被判「高度相似」。
	ev := New(cfg)
	seed := &model.Task{ID: "seed1", CreatedAt: now - 60_000, Title: "工程质量体检 · 构建与测试链路", Prompt: "你是一名工程质量体检专员。请对当前工作空间的项目做一次体检……（长脚手架常量段）"}
	ev.Observe(seed, 80, "d", 400)
	other := &model.Task{ID: "other1", CreatedAt: now, Title: "工程质量体检 · 工作空间项目最近一次改动", Prompt: "你是一名工程质量体检专员。请对当前工作空间的项目做一次体检……（长脚手架常量段）"}
	if got := ev.gates(Input{Task: other}, now); containsSimReason(got) {
		t.Fatalf("同模板不同主题不得被判高度相似（标题只差主题段），实际 reasons=%v", got)
	}

	// ② 同标题不同 prompt ⇒ 窗内仍拦（模糊判据仍有效）。
	ev2 := New(cfg)
	seed2 := &model.Task{ID: "seed2", CreatedAt: now - 60_000, Title: "同一个标题", Prompt: "需求 A"}
	ev2.Observe(seed2, 80, "d", 400)
	twin := &model.Task{ID: "twin", CreatedAt: now, Title: "同一个标题", Prompt: "需求 B（完全不同）"}
	if got := ev2.gates(Input{Task: twin}, now); !containsSimReason(got) {
		t.Fatalf("窗内同标题应被判高度相似，实际 reasons=%v", got)
	}

	// ③ 窗外同标题 ⇒ 不再拦（与精确指纹同口径，避免永久封杀）。
	ev3 := New(cfg)
	seed3 := &model.Task{ID: "seed3", CreatedAt: now - 2*60*60*1000, Title: "老标题", Prompt: "需求 A"}
	ev3.Observe(seed3, 80, "d", 400)
	twin3 := &model.Task{ID: "twin3", CreatedAt: now, Title: "老标题", Prompt: "需求 B（完全不同）"}
	if got := ev3.gates(Input{Task: twin3}, now); containsSimReason(got) {
		t.Fatalf("窗外同标题不得再被判高度相似，实际 reasons=%v", got)
	}
}

func containsSimReason(reasons []string) bool {
	for _, r := range reasons {
		if strings.Contains(r, "高度相似") {
			return true
		}
	}
	return false
}
