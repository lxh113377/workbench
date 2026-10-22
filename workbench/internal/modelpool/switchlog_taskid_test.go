package modelpool

// switchlog_taskid_test.go 守护「切换日志必须能归属到任务」（2026-10-19 现场）。
//
// 现场：model-status.jsonl 里 57 条 kind=switch 记录**全部没有 taskId**，
// 于是排查时无法重建「某个任务经历了哪条切换链」——
// 恰好违背需求5「完整记录模型调用状态、切换日志，便于后续问题排查」。
//
// 缺陷三层：
//   ① 调用方（FailoverRunner）**传了** TaskID：transitionRecord{TaskID: task.ID, ...}
//   ② AppendSwitch **没有转发**：m.log(logRecord{Kind:..., From:..., To:..., Reason:...})
//   ③ logRecord 结构体**根本没有 TaskID 字段**
// => 数据在第二层被丢掉，且没有测试覆盖。
//
// 判据：写入 switch 日志时必须带上 taskId，且落盘后能从日志里读回。

import (
	"strings"
	"testing"
)

// TestSwitchLogCarriesTaskID 断言：AppendSwitch 落盘的记录带 taskId。
func TestSwitchLogCarriesTaskID(t *testing.T) {
	store := NewStore(t.TempDir())
	m := NewManager(store, []Spec{spec("a", 10, 100, StateUnknown), spec("b", 20, 50, StateUnknown)})

	m.AppendSwitch(transitionRecord{
		TaskID: "task-abc-123", From: "a", To: "b",
		Reason: "模型失败自动切换（upstream_failed）",
	})

	logs := store.TailLog(50)
	var sw string
	for _, l := range logs {
		if strings.Contains(l, `"kind":"switch"`) {
			sw = l
		}
	}
	if sw == "" {
		t.Fatalf("应有一条 switch 记录，实际日志: %v", logs)
	}
	if !strings.Contains(sw, `"taskId":"task-abc-123"`) {
		t.Fatalf("switch 日志必须带 taskId（否则无法归属到任务、无法重建切换链）；实际: %s", sw)
	}
	// 回归：原有字段不能丢
	for _, want := range []string{`"from":"a"`, `"to":"b"`, `"reason":`} {
		if !strings.Contains(sw, want) {
			t.Fatalf("回归：switch 日志应保留 %s；实际: %s", want, sw)
		}
	}
}
