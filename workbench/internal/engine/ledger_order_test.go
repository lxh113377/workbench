package engine

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/retry"
	"workbench/internal/store"
)

// 台账与状态生效的顺序契约（2026-10-22 对标 asynq processor.go:336-338）。
//
// 失效形态（改造前沿用）：failTask 先 store.Update（迁状态）再 store.Audit，
// 存在一个窗口「状态已是 failed，而台账查不到为什么」；
// 更糟的是 Update 返回错误时只 emit 一条瞬时事件（不落盘），
// 任务停在 running 且没有任何结构化痕迹 —— 既不重试也不统计，只会静静躺着。
//
// 判据强度说明（本轮两次踩到）：顺序必须用**注入闭包断言调用次序**
//（沿用本仓 autonomy/nextgoal_order_test.go 的既有范式），不靠时间戳比较 ——
// 两次写常常落在同一毫秒内，时间戳判据会把反序实现也判绿。
// 第一版测试在测试里重写了一遍判据逻辑，结果变异一回退照样全绿。

// TestWriteLedgerThenTransitionOrder 直接断言调用次序（不依赖时间/IO）。
func TestWriteLedgerThenTransitionOrder(t *testing.T) {
	var calls []string
	ledgerErr, stateErr := writeLedgerThenTransition(
		func() error { calls = append(calls, "ledger"); return nil },
		func() error { calls = append(calls, "state"); return nil },
	)
	if ledgerErr != nil || stateErr != nil {
		t.Fatalf("无错误路径不应返回错误: ledger=%v state=%v", ledgerErr, stateErr)
	}
	if len(calls) != 2 || calls[0] != "ledger" || calls[1] != "state" {
		t.Fatalf("顺序必须是 先留痕后生效，实际 %v", calls)
	}
}

// TestWriteLedgerThenTransitionSemantics 钉死两条失效语义：
//   - 留痕失败**不阻断**状态生效（任务必须能到达终态，否则会永久悬挂）；
//   - 两个错误都原样返回给调用方（由调用方决定如何兜底留痕）。
func TestWriteLedgerThenTransitionSemantics(t *testing.T) {
	ledgerErr := errors.New("disk full")
	var calls []string
	lErr, sErr := writeLedgerThenTransition(
		func() error { calls = append(calls, "ledger"); return ledgerErr },
		func() error { calls = append(calls, "state"); return nil },
	)
	if !errors.Is(lErr, ledgerErr) {
		t.Fatalf("留痕错误必须原样返回，got %v", lErr)
	}
	if sErr != nil {
		t.Fatalf("留痕失败不得阻断状态生效，got state=%v", sErr)
	}
	if len(calls) != 2 {
		t.Fatalf("留痕失败后仍须尝试状态生效: %v", calls)
	}

	stateErr := errors.New("cannot persist")
	_, sErr = writeLedgerThenTransition(
		func() error { return nil },
		func() error { return stateErr },
	)
	if !errors.Is(sErr, stateErr) {
		t.Fatalf("状态错误必须原样返回，got %v", sErr)
	}
}

// TestFailTaskWritesLedgerAndKind 是端到端判据：
// failTask 之后，任务记录必须带真实错误类别，**且**审计台账里必须有对应记录。
// 两面同源，任一缺失都判红（改造前 Update 失败路径两者都缺）。
func TestFailTaskWritesLedgerAndKind(t *testing.T) {
	e, st, layout := newTestEngine(t)
	seedRunning(t, st, "task-ledger")

	e.failTask("task-ledger", errors.New("上游 502"), retry.Upstream)

	got := mustGet(t, st, "task-ledger")
	if got.ErrorKind != string(retry.Upstream) {
		t.Fatalf("ErrorKind = %q, want %q（类别必须落到任务记录）", got.ErrorKind, string(retry.Upstream))
	}
	if got.State != model.StateFailed {
		t.Fatalf("state = %s, want failed", got.State)
	}
	audit := readAuditText(t, layout)
	if !strings.Contains(audit, "["+string(retry.Upstream)+"]") {
		t.Fatalf("审计台账缺少带类别的失败记录，实际内容:\n%s", audit)
	}
}

// TestCondemnPathWritesAudit 钉死本轮补齐的缺口：
// 崩溃救援判死路径**改造前完全没有审计**（只在内存 emit 一条瞬时事件）。
// 判死是不可逆的终态决策，必须有可追溯的台账。
func TestCondemnPathWritesAudit(t *testing.T) {
	e, st, layout := newTestEngine(t)
	id := "task-condemn"
	seedRunning(t, st, id)
	if _, err := st.Transition(id, model.StateInterrupted, "test", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Update(id, func(x *model.Task) error {
		x.CrashRescues = maxCrashRescues
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if nt := e.rescueInterrupted(mustGet(t, st, id)); nt != nil {
		t.Fatal("额度耗尽后不应再入队")
	}
	final := mustGet(t, st, id)
	if final.State != model.StateFailed {
		t.Fatalf("state = %s, want failed", final.State)
	}
	if final.ErrorKind != crashRescueKind {
		t.Fatalf("ErrorKind = %q, want %q", final.ErrorKind, crashRescueKind)
	}
	// 判据必须按 **kind** 区分，不能只看文案：
	// store.Transition 自己也会把 note 写进审计（kind=transition），
	// 而 note 与本次新增的审计文案高度重合 —— 只看"有没有『判死』字样"的话，
	// 把新增的 Audit 调用整段删掉，Transition 的审计照样能让判据变绿。
	// 本轮实测：第一版正是这样被一条"去掉关键词"的变异放过的。
	if !hasAuditKind(layout, "error", "判死") {
		t.Fatalf("判死路径必须留一条 kind=error 的审计（改造前为零审计），实际审计:\n%s", readAuditText(t, layout))
	}
}

// TestAppendAuditOrEventAlwaysLeavesTrace 验证兜底通道：
// 「至少一处留痕」是结构性保证，不是尽力而为。
// 正常路径下审计面写成功即返回 true；审计面不可写时退到任务事件流。
func TestAppendAuditOrEventAlwaysLeavesTrace(t *testing.T) {
	e, st, layout := newTestEngine(t)
	seedRunning(t, st, "task-fallback")

	ok := e.appendAuditOrEvent(store.AuditRecord{At: model.Now(), TaskID: "task-fallback",
		Actor: "engine", Kind: "error", Message: "测试留痕"}, "task-fallback", "测试留痕")
	if !ok {
		t.Fatal("appendAuditOrEvent 必须保证至少一处留痕成功")
	}
	audit := readAuditText(t, layout)
	if !strings.Contains(audit, "测试留痕") {
		// 审计面没写成，则必须落到任务事件流。
		ev := layout.TaskFile("task-fallback", "events.jsonl")
		data, err := os.ReadFile(ev)
		if err != nil {
			t.Fatalf("审计面与事件流两面都没有痕迹: %v", err)
		}
		if !strings.Contains(string(data), "测试留痕") {
			t.Fatalf("事件流兜底痕迹内容不符: %s", string(data))
		}
	}
}

// TestCleanFailurePathHasNoFallbackNoise 是**反向判据**：
// 正常失败路径不得产生任何 fallback 痕迹，否则告警会变成噪音，
// 真正需要关注的"留痕写不进去"反被淹没。
func TestCleanFailurePathHasNoFallbackNoise(t *testing.T) {
	e, st, layout := newTestEngine(t)
	seedRunning(t, st, "task-clean")
	e.failTask("task-clean", errors.New("普通失败"), retry.Upstream)
	audit := readAuditText(t, layout)
	if strings.Contains(audit, "ledger-fallback") || strings.Contains(audit, "state-update-failed") {
		t.Fatalf("正常失败路径不得出现兜底痕迹，实际:\n%s", audit)
	}
}

// ── 脚手架 ────────────────────────────────────────────────────

// seedRunning 造一条处于 running 的任务（failTask 的入口前提）。
func seedRunning(t *testing.T, st *store.Store, id string) {
	t.Helper()
	if err := st.Create(&model.Task{ID: id, Title: id, State: model.StateCandidate, MaxAttempts: 3}); err != nil {
		t.Fatal(err)
	}
	for _, to := range []model.TaskState{model.StateApproved, model.StateQueued, model.StateRunning} {
		if _, err := st.Transition(id, to, "test", ""); err != nil {
			t.Fatalf("%s: %v", to, err)
		}
	}
}

func mustGet(t *testing.T, st *store.Store, id string) *model.Task {
	t.Helper()
	got, ok := st.Get(id)
	if !ok {
		t.Fatalf("任务不存在: %s", id)
	}
	return got
}

// hasAuditKind 在审计 JSONL 里查一条 kind 匹配且 Message 含 needle 的记录。
//
// 为什么必须按 kind 过滤（而不是子串搜全文）：Transition 与 Audit 都往同一个
// 文件写，Transition 的 Message 就是迁移说明文本，与业务审计文案高度重合；
// 不做 kind 过滤会把"迁移顺带记的那条"当成"专门的失败审计"，
// 使判据对"删掉专用审计"这类回归失效（本轮实测踩到）。
func hasAuditKind(layout config.Layout, wantKind, needle string) bool {
	data, err := os.ReadFile(filepath.Join(layout.LogsDir, "audit.jsonl"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec.Kind == wantKind && strings.Contains(rec.Message, needle) {
			return true
		}
	}
	return false
}

// readAuditText 读审计台账全文（不存在时返回空串，交由判据报错）。
func readAuditText(t *testing.T, layout config.Layout) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(layout.LogsDir, "audit.jsonl"))
	if err != nil {
		return ""
	}
	return string(data)
}
