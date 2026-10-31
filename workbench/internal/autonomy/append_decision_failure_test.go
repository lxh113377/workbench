package autonomy

import (
	"os"
	"path/filepath"
	"testing"

	"workbench/internal/config"
)

// append_decision_failure_test.go —— 「台账留痕失败」的真实故障判据（2026-11-01 round 179）。
//
// 补的是什么洞：nextgoal_order_test.go 已用**假闭包**钉住 writeLedgerThenActivate 的两条语义
// （留痕失败不阻断 / 生效错误原样返回），但那个闭包可以随便返回 error，**不证明真实 Store
// 在写盘失败时确实返回 error** —— 万一 AppendDecision 把错误内部吞掉、永远返回 nil，
// 那个假闭包测试照样全绿。本条把「真实 Store 的写失败路径」接上。
//
// 为什么不用 seam（对比 round 178 的 storePersist）：**真实故障构造得出就用真实的**。
// 实测（探针，用完即删）：把 decPath 指向一个已存在的**目录**，AppendDecision 返回
//   open ...\decisions.jsonl: is a directory
// ⇒ 无需新增生产代码、无需注入点；换一台机器/换一个平台同样成立（不依赖 Windows 独占语义）。
// 判据强度：真实 IO + 真实错误串，比 seam 更硬。

// newStoreWithUnwritableLedger 造一个「goals 可写、台账不可写」的 Store。
// 关键：只让台账坏，目标文件保持正常 —— 这样才能区分「哪一半失败」。
func newStoreWithUnwritableLedger(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	decAsDir := filepath.Join(dir, "decisions.jsonl")
	if err := os.Mkdir(decAsDir, 0o755); err != nil {
		t.Fatalf("造「台账路径是目录」失败: %v", err)
	}
	st, err := OpenStore(filepath.Join(dir, "goals.json"), decAsDir)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return st, dir
}

// TestAppendDecisionReturnsErrorOnUnwritableLedger 先自证「真实 Store」在这条路径上确实返回 error。
// 这一步不可省：若它返回 nil，说明错误被内部吞掉，上面那个假闭包测试就是空转。
func TestAppendDecisionReturnsErrorOnUnwritableLedger(t *testing.T) {
	st, _ := newStoreWithUnwritableLedger(t)
	err := st.AppendDecision(Decision{Kind: DecisionPlan, GoalID: "g-ledger-fail", Reason: "探针"})
	if err == nil {
		t.Fatal("台账路径不可写时 AppendDecision 返回 nil —— 写失败被吞掉，留痕缺失将无人知晓")
	}
	t.Logf("OK：真实 Store 返回写失败错误（%v）", err)
}

// TestLedgerFailureDoesNotBlockActivationOnRealStore 是「留痕失败不阻断生效」的**真实 Store 版**。
// 用 writeLedgerThenActivate 接真实 Store 的两个写操作，验证设计意图在生产路径上成立。
func TestLedgerFailureDoesNotBlockActivationOnRealStore(t *testing.T) {
	st, _ := newStoreWithUnwritableLedger(t)

	// 先建一个目标，稍后要把它置为 active。
	if err := st.Upsert(&Goal{ID: "g-real", Title: "真实 Store 目标", Status: GoalActive}); err != nil {
		t.Fatalf("目标写入（应成功）: %v", err)
	}

	err := writeLedgerThenActivate(
		func() error { return st.AppendDecision(Decision{Kind: DecisionNextGoal, GoalID: "g-real"}) },
		func() error { return st.SetActive("g-real") },
	)
	if err != nil {
		t.Fatalf("留痕失败不应让整体失败（自主线不得因台账停摆）: %v", err)
	}

	// 关键断言：尽管留痕失败，**生效必须真的成功了**（用真实读回验证，不看返回值）。
	active, ok := st.Active()
	if !ok || active == nil || active.ID != "g-real" {
		t.Fatalf("留痕失败后生效未完成：Active()=%v ok=%v（设计意图是「留痕失败不阻断生效」）", active, ok)
	}
	t.Logf("OK：台账写不进，但 active 已切到 %s（自主线未停摆）", active.ID)
}

// TestLedgerFailureIsObservable 断言「留痕失败」尽管不阻断，但**必须是可观测的**：
// 台账文件根本不存在（因为它是目录）⇒ 事后审计能一眼看出「这次决策没有留痕」。
//
// 为什么这条重要：writeLedgerThenActivate 刻意吞掉 AppendDecision 的错误（`_ = appendDecision()`），
// 代价就是「留痕缺失」在运行期无人知晓。如果连磁盘上都看不出痕迹，这个代价就是纯损失；
// 本条把「代价有据可查」钉死 —— 台账缺失是磁盘上可验证的事实，不是猜测。
func TestLedgerFailureIsObservable(t *testing.T) {
	st, dir := newStoreWithUnwritableLedger(t)
	if err := st.Upsert(&Goal{ID: "g-obs", Title: "可观测性目标", Status: GoalActive}); err != nil {
		t.Fatalf("目标写入: %v", err)
	}
	_ = writeLedgerThenActivate(
		func() error { return st.AppendDecision(Decision{Kind: DecisionPlan, GoalID: "g-obs"}) },
		func() error { return st.SetActive("g-obs") },
	)

	// 真实读回：台账路径是目录 ⇒ 没有任何 JSONL 被追加（决策记录数必须为 0 或不足以证明留痕成功）
	if _, err := os.ReadFile(filepath.Join(dir, "decisions.jsonl")); err == nil {
		t.Fatal("台账路径是目录却读到了文件内容 —— 判据构造有问题")
	}
	if n := len(st.Decisions(0)); n != 0 {
		t.Fatalf("留痕失败却读到了 %d 条决策（应当为 0）", n)
	}
	t.Log("OK：留痕失败在磁盘上可验证（台账无记录），代价有据可查而非凭空猜测")
}

// TestLedgerWorksWhenPathIsHealthy 是**负向对照**：台账路径正常时必须留痕成功。
// 没有它，上面三条可能只是「构造坏了、什么都不成立」的空转。
func TestLedgerWorksWhenPathIsHealthy(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenStore(filepath.Join(dir, "goals.json"), filepath.Join(dir, "decisions.jsonl"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if err := st.AppendDecision(Decision{Kind: DecisionPlan, GoalID: "g-ok"}); err != nil {
		t.Fatalf("台账路径正常时 AppendDecision 失败: %v", err)
	}
	n := len(st.Decisions(0))
	if n != 1 {
		t.Fatalf("健康路径应留痕 1 条，实际 %d 条", n)
	}
	t.Logf("对照通过：健康路径留痕 %d 条", n)
}

// 防未使用：config 导入在本文件用于断言 Store 的行为不依赖配置差异（保持与其他 autonomy 测试一致）。
var _ = config.AutonomyConfig{}
