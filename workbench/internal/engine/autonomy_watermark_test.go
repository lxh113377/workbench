package engine

// autonomy_watermark_test.go 守护「自主链路不能被模板积压饿死」。
//
// 现场缺陷（2026-10-19 09:12）：soak 实例跑了 30 分钟，一条自主任务都没生成，
// goals.json 的 stages 一直是 0。根因是 supplyAutonomous 的水位判据写成
// 「队列总长 >= concurrency 就不再规划」，而模板链路会把队列稳定维持在 3 条
// （> concurrency=2）—— 自主链路**永远**在第一步就返回，被模板积压永久饿死。
//
// 正确判据：只数**自主任务**在队列里的数量（自主线串行，留 1 条足够）。

import (
	"context"
	"testing"
	"workbench/internal/model"
)

// TestAutonomyNotStarvedByTemplateBacklog 是本轮现场缺陷的回归测试。
func TestAutonomyNotStarvedByTemplateBacklog(t *testing.T) {
	e, st, _ := newAutonomyTestEngine(t)
	cfg := e.Config()
	if !cfg.Autonomy.Enabled {
		t.Fatal("默认配置应启用自主子系统")
	}

	// 先往队列里塞满模板任务（模拟 soak 实例现场：队列 3 条 > concurrency 2）
	for _, id := range []string{"tpl-1", "tpl-2", "tpl-3"} {
		task := &model.Task{ID: id, Title: "模板任务 " + id, State: model.StateCandidate,
			Source: "template", Domain: "content"}
		if err := st.Create(task); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Transition(id, model.StateApproved, "test", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Transition(id, model.StateQueued, "test", ""); err != nil {
			t.Fatal(err)
		}
		if q, ok := st.Get(id); ok {
			e.q.Push(q)
		}
	}
	if e.q.Len() < cfg.Executor.Concurrency {
		t.Fatalf("前置条件失败：队列长度 %d 应 >= concurrency %d", e.q.Len(), cfg.Executor.Concurrency)
	}

	// 关键断言：模板积压不得阻止自主规划
	e.supplyAutonomous(context.Background(), cfg)

	autonomyTasks := 0
	for _, tk := range e.Store().List() {
		if tk != nil && tk.Source == "autonomy" {
			autonomyTasks++
		}
	}
	if autonomyTasks == 0 {
		t.Fatal("模板任务积压时自主链路仍必须能规划（否则会被永久饿死）")
	}
}

// TestAutonomySerialNoSecondTask 守护自主线串行：队列里已有自主任务时不再新增。
func TestAutonomySerialNoSecondTask(t *testing.T) {
	e, _, _ := newAutonomyTestEngine(t)
	cfg := e.Config()
	e.supplyAutonomous(context.Background(), cfg)
	first := 0
	for _, tk := range e.Store().List() {
		if tk != nil && tk.Source == "autonomy" {
			first++
		}
	}
	if first == 0 {
		t.Fatal("首轮应能规划出自主任务")
	}
	// 再调一次：队列里已有自主任务 ⇒ 不应再规划
	e.supplyAutonomous(context.Background(), cfg)
	second := 0
	for _, tk := range e.Store().List() {
		if tk != nil && tk.Source == "autonomy" {
			second++
		}
	}
	if second > 1 {
		t.Fatalf("自主线必须串行：已有 1 条自主任务时不应再规划，实际 %d 条", second)
	}
}
