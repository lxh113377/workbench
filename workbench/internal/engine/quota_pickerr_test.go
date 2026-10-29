package engine

import (
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/modelroute"
)

// TestQuotaWaitBudgetAppliesOnPickErrPath 钉住「整池冷却时的退回也要走长预算」（round 125 的补漏）。
//
// 现场：额度耗尽把 36+ 条车道打进冷却后，任务是在「无可用车道」的 pickErr 路径上退回的，
// 本轮没有 ErrorKind ⇒ 只看 kind 的话 48 次长预算永远够不到（实测：30 次 quota 尝试、长预算日志 0 条、
// 判死仍 budget=4）。quotaLanesCooling() 从池账本取真值来补齐这条路径。
func TestQuotaWaitBudgetAppliesOnPickErrPath(t *testing.T) {
	e, st, _ := newTestEngine(t)
	id := "task-pickerr-quota"
	seedQueuedForPoolTest(t, e, id)
	if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Get(id)
	// 前置：没有任何 quota 冷却 ⇒ 默认预算生效（不得无脑放宽）
	if e.quotaLanesCooling() {
		t.Fatal("空池不应被判成额度耗尽冷却")
	}
	// 造一条「因额度耗尽而冷却」的车道（走 router 的真实记账口）
	r := modelroute.New([]config.ModelRoute{{Model: "lane-q", Provider: "our-free-model", Enabled: true}})
	r.CooldownByProvider("our-free-model", 600000000000, "quota_exhausted", "free usage limit reached")
	e.router = r // 同包测试直接注入（engine.router 是私有字段）
	if !e.quotaLanesCooling() {
		t.Fatal("有 quota 冷却车道时 quotaLanesCooling 必须为真")
	}
	// 关键：kind 为空（pickErr 路径就是这样）也应走长预算 ⇒ 超过默认预算仍存活
	for i := 1; i <= poolWaitBudget; i++ {
		if e.requeueForPoolWaitKind(id, before.ChainID, "") {
			t.Fatalf("第 %d 次就判死：pickErr 路径没吃到长等待预算", i)
		}
		if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := st.Get(id); !ok {
		t.Fatal("超默认预算后任务应仍存活")
	}
}
