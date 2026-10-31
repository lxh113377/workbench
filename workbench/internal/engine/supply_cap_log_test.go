package engine

import (
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/retry"
)

// TestEnvBurnEvidenceScope 钉住 T104 的**口径**（round 148，现场：201 条计数任务里 200 条是环境性失败）。
//
// 判据必须与 tasksToday() 完全一致 —— 一旦这里多算/少算，读数就会与"是否命中上限"的产品判据分叉
// （2026-10-21 那次"两条链同时以为满了"的同类缺陷）。所以四条边界都要钉：
//
//	· 被拒草案不计；跨天不计；done/queued 不算"烧掉的额度"（只数 failed）；
//	· cancelled 虽属 Retryable 类别，但是**用户意图**不是环境故障 ⇒ 必须剔除；
//	· 任务侧失败（verify_failed 等不可重试类别）不计入环境性。
func TestEnvBurnEvidenceScope(t *testing.T) {
	now := time.Now().UnixMilli()
	start := now - 3600_000
	mk := func(id string, state model.TaskState, kind string, created int64) *model.Task {
		return &model.Task{ID: id, Title: id, State: state, ErrorKind: kind, CreatedAt: created}
	}
	tasks := []*model.Task{
		mk("a", model.StateFailed, string(retry.Transport), start),    // 环境性 ⇒ 计入
		mk("b", model.StateFailed, string(retry.Transport), start),    // 环境性 ⇒ 计入（topKind 主因）
		mk("c", model.StateFailed, string(retry.NotServed), start),    // 环境性 ⇒ 计入
		mk("d", model.StateFailed, string(retry.VerifyFailed), start), // 任务侧 ⇒ 不计环境性
		mk("e", model.StateCancelled, string(retry.Cancelled), start),
		mk("i", model.StateFailed, string(retry.Cancelled), start),          // 取消 ⇒ 不计环境性
		mk("f", model.StateRejected, string(retry.Transport), start),        // 被拒草案 ⇒ counted 也不计
		mk("g", model.StateDone, "", start-7200_000),                        // 跨天 ⇒ 全不计
		mk("h", model.StateFailed, string(retry.Transport), start-7200_000), // 跨天的环境失败 ⇒ 不计
	}
	counted, env, top := envBurnEvidence(tasks, start)
	if counted != 6 {
		t.Fatalf("counted 应为 6（排除 rejected 与跨天），实得 %d", counted)
	}
	if env != 3 {
		t.Fatalf("envFailures 应为 3（transport×2 + not_served；verify_failed 与 failed+cancelled 都不算），实得 %d", env)
	}
	if top != string(retry.Transport) {
		t.Fatalf("topKind 应为出现最多的 transport_failed，实得 %q", top)
	}
	// topKind 的确定性：同计数时按 kind 字典序，不能让 map 随机序决定"今天的主因"
	tie := []*model.Task{
		mk("x", model.StateFailed, string(retry.Upstream), start),
		mk("y", model.StateFailed, string(retry.Transport), start),
	}
	_, _, topTie := envBurnEvidence(tie, start)
	if topTie != string(retry.Transport) {
		t.Fatalf("同计数时应按 kind 字典序定主因（可复算），实得 %q", topTie)
	}
	for i := 0; i < 20; i++ {
		if _, _, again := envBurnEvidence(tie, start); again != topTie {
			t.Fatalf("主因读数不稳定：第 %d 次得到 %q，want %q", i, again, topTie)
		}
	}
	// 空数据不编造主因
	if _, e2, tp2 := envBurnEvidence(nil, start); e2 != 0 || tp2 != "" {
		t.Fatalf("空输入应给 0/空主因，实得 %d/%q", e2, tp2)
	}
}

// TestDailyCapBurnAlertIsRateLimited 钉住"决策点自己发声"且**限频**：
// 现场是一次风暴后 8.5 小时里两条供货链每 3 秒 return 一次、零日志；修法必须发声，
// 但也不能反过来把每小时 1200 条刷进日志（那会淹没信号 —— 本会话已被日志噪音反噬多次）。
func TestDailyCapBurnAlertIsRateLimited(t *testing.T) {
	e, st, _ := newTestEngine(t)
	cap := &captureLogs{}
	e.SetLogger(newCaptureLogger(t, cap))
	if err := st.Create(&model.Task{ID: "burn-1", Title: "烧额度", State: model.StateCandidate, MaxAttempts: 3, OutputFormat: "code"}); err != nil {
		t.Fatal(err)
	}
	for _, to := range []model.TaskState{model.StateApproved, model.StateQueued, model.StateRunning, model.StateFailed} {
		if _, err := st.Transition("burn-1", to, "test", ""); err != nil {
			t.Fatalf("%s: %v", to, err)
		}
	}
	if _, err := st.Update("burn-1", func(tk *model.Task) error {
		tk.ErrorKind = string(retry.Transport)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	start := time.Now().Add(-time.Hour).UnixMilli()
	e.logDailyCapBurn(cfg, start)
	n1 := len(cap.find("额度已被环境性失败烧穿"))
	if n1 != 1 {
		t.Fatalf("首次命中应记一条告警，实得 %d（0 = 静默停机未修）", n1)
	}
	// 同一窗口内重复调用必须被限频吞掉（tick 每 3 秒会反复命中）
	for i := 0; i < 5; i++ {
		e.logDailyCapBurn(cfg, start)
	}
	if n2 := len(cap.find("额度已被环境性失败烧穿")); n2 != 1 {
		t.Fatalf("限频未生效：重复调用后告警数 = %d，want 1", n2)
	}
}
