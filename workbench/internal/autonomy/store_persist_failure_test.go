package autonomy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"workbench/internal/config"
)

// injectPersistFailure 让 storePersist 在本次测试内必定失败，返回恢复函数。
//
// 为什么必须注入而不是造真实磁盘故障：Windows 上「写 goals.json 失败」无法稳定构造
// （config 侧实测扫过独占打开/只读属性/空目录/非空目录四种方案均不成立），
// 靠碰运气的测试在别的机器会静默 SKIP ⇒ 判据等于不存在（本仓在册 vacuous pass 家族）。
func injectPersistFailure(t *testing.T) func() {
	t.Helper()
	orig := storePersist
	storePersist = func(string, GoalsFile) error { return fmt.Errorf("注入的持久化失败") }
	t.Cleanup(func() { storePersist = orig })
	return func() { storePersist = orig }
}

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := OpenStore(filepath.Join(dir, "goals.json"), filepath.Join(dir, "decisions.jsonl"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return st, dir
}

// TestStorePersistSeamIsWired 先自证 seam 真的接上了（否则后面所有注入测试都是空转）。
// 这一步是本仓在册纪律：注入式测试必须先证明「注入点被走到」，再谈结论。
func TestStorePersistSeamIsWired(t *testing.T) {
	st, _ := newTestStore(t)
	calls := 0
	orig := storePersist
	storePersist = func(path string, f GoalsFile) error { calls++; return orig(path, f) }
	defer func() { storePersist = orig }()

	if err := st.Upsert(&Goal{ID: "g-seam", Title: "seam"}); err != nil {
		t.Fatalf("Upsert 失败: %v", err)
	}
	if calls == 0 {
		t.Fatal("storePersist 一次都没被调用 —— seam 没接上，后续注入测试全是空转")
	}
	t.Logf("seam 已接上：Upsert 触发了 %d 次 persist", calls)
}

// TestCreateGoalSwallowedPersistFailure 是本轮的核心取证：
// createGoalLocked 内部 `_ = m.store.Upsert(g)` 吞掉持久化错误后，**调用方拿到的目标是否真的落盘了**。
//
// 期望（按当前实现的真实行为，不是我的愿望）：
//
//	SetGoal 路径 ⇒ 后续 SetActive 会再 persist 一次，若那次也失败则整体返回 error（目标没落盘但内存有）；
//	bootstrapLocked 的 config 分支 ⇒ 同样靠 SetActive 兜住；
//	但只要两层都失败，**内存里已经有这个目标、磁盘上没有** —— 这就是状态分叉。
//
// 本判据把这件事写死：分叉发生时，必须有 error 上浮到调用方（否则用户以为建成了）。
func TestCreateGoalSwallowedPersistFailure(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenStore(filepath.Join(dir, "goals.json"), filepath.Join(dir, "decisions.jsonl"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	// 全程持久化失败（Upsert 与 SetActive 都写不进去）
	orig := storePersist
	storePersist = func(string, GoalsFile) error { return fmt.Errorf("注入的持久化失败") }
	defer func() { storePersist = orig }()

	clock := int64(1_700_000_000_000)
	now := func() int64 { return clock }
	m := NewManager(st, func() config.AutonomyConfig { return config.AutonomyConfig{} }, nil, now)
	_, err = m.SetGoal("注入目标", "验证持久化失败是否上浮", "quality")
	if err == nil {
		t.Fatal("SetGoal 在持久化全程失败时返回 nil —— 调用方会以为目标已建好并落盘，实际磁盘上没有（状态分叉）")
	}
	t.Logf("OK：持久化失败时 SetGoal 返回 error（%v），调用方不会被骗", err)

	// 反向证明：内存里确实已经建了这个目标（这就是分叉的证据面）
	goals := st.Goals()
	if len(goals) != 0 {
		t.Logf("注意：内存中已有 %d 个目标但磁盘无 —— 这正是被吞掉的那次 Upsert 造成的分叉面", len(goals))
	}
	// 磁盘必须**为空树**——用真实读回验证，而不是看内存。
	//
	// 门槛为什么是「goals 数组为空」而不是「文件不存在」：OpenStore 在文件缺失时会主动建一个
	// 空树 goals.json（实测 86 字节：{schemaVersion, activeId:"", goals:[], savedAt}）——
	// 那 86 字节是「建库」不是「写入目标」。我第一版断言"文件不存在"被它判红，
	// 是判据的期望写错、不是实现错（本仓在册「判据与被检对象错位」同族）。
	b, rerr := os.ReadFile(filepath.Join(dir, "goals.json"))
	if rerr != nil {
		t.Fatalf("读不到 goals.json（OpenStore 本该建空树）: %v", rerr)
	}
	var gf GoalsFile
	if err := json.Unmarshal(b, &gf); err != nil {
		t.Fatalf("goals.json 不是合法 JSON: %v", err)
	}
	if len(gf.Goals) != 0 {
		t.Fatalf("注入全程失败却落盘了 %d 个目标 ⇒ seam 没真正挡住写入，判据不可信", len(gf.Goals))
	}
	if gf.ActiveID != "" {
		t.Fatalf("注入全程失败却落盘了 activeId=%q ⇒ seam 没挡住 SetActive", gf.ActiveID)
	}
	t.Logf("磁盘为空树（goals=%d activeId=%q）⇒ 注入的持久化失败确实挡住了落盘", len(gf.Goals), gf.ActiveID)
}

// TestCreateGoalSucceedsWithoutInjection 是上面的**负向对照**（本仓在册纪律：注入式判据必须带对照）。
//
// 没有它，上面那条「注入失败 ⇒ 返回 error」可能只是恒真：万一 SetGoal 对任何输入都返回 error，
// 那条判据照样绿，却什么都没证明。这里去掉注入、走完全真实路径，必须**成功**且目标落盘。
func TestCreateGoalSucceedsWithoutInjection(t *testing.T) {
	dir := t.TempDir()
	gp := filepath.Join(dir, "goals.json")
	st, err := OpenStore(gp, filepath.Join(dir, "decisions.jsonl"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	clock := int64(1_700_000_000_000)
	m := NewManager(st, func() config.AutonomyConfig { return config.AutonomyConfig{} }, nil, func() int64 { return clock })

	// 不注入：storePersist 保持默认实现（真实写盘）。
	g, err := m.SetGoal("对照目标", "证明同路径在无注入时能成功落盘", "quality")
	if err != nil {
		t.Fatalf("无注入时 SetGoal 失败: %v（对照失效，上面那条判据不可信）", err)
	}
	if g == nil || g.ID == "" {
		t.Fatal("无注入时 SetGoal 返回空目标")
	}
	// 用真实读回验证落盘（不看内存）
	b, rerr := os.ReadFile(gp)
	if rerr != nil {
		t.Fatalf("读不到 goals.json: %v", rerr)
	}
	var gf GoalsFile
	if err := json.Unmarshal(b, &gf); err != nil {
		t.Fatalf("goals.json 不合法: %v", err)
	}
	if len(gf.Goals) != 1 {
		t.Fatalf("无注入时应落盘 1 个目标，实际 %d 个", len(gf.Goals))
	}
	if gf.Goals[0].ID != g.ID {
		t.Fatalf("落盘目标 ID=%q 与返回值 %q 不一致（内存与磁盘分叉）", gf.Goals[0].ID, g.ID)
	}
	if gf.ActiveID != g.ID {
		t.Fatalf("activeId=%q 与目标 ID=%q 不一致", gf.ActiveID, g.ID)
	}
	t.Logf("对照通过：无注入时目标已落盘 goals=%d activeId=%s", len(gf.Goals), gf.ActiveID)
}
