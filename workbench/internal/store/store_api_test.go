package store

// store_api_test.go -- 核心持久化 API 的契约判据（2026-10-23 补）。
//
// 为什么必须有：逐函数覆盖率扫描显示本包 12 个函数 0% 覆盖，其中三条是
// **会被引擎与 Web 层直接调用**的核心 API，各自的失效形态都不会让编译失败：
//   ① SaveTaskArtifact 的文件名校验 —— 若放行 `..\` 或 `:`，产物可写到任务目录之外
//      （路径穿越 / NTFS 备用流写入），这是安全面问题而不是功能问题；
//   ② Update 的写时复制语义 —— 若退回原地写 `*t = cp`，与「持有旧指针的读方」
//      形成 data race（本仓 2026-10-19 实测被 -race 抓到过，且修复曾被整文件覆盖丢掉）；
//   ③ UpsertChain/GetChain 的副本语义 —— 若返回内部指针，调用方改它就会污染台账
//      （链的 ChainDepth/Fanout 一旦被外部改坏，后继任务生成会失控）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	layout, err := config.BuildLayout(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(layout)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestSaveTaskArtifactRejectsIllegalNames 守护**路径穿越**这条安全判据。
// 非法字符表一旦漏项，产物就能落到任务目录之外：
//
//	`../x` 向上穿越、`a:b` 写 NTFS 备用数据流、`a/b` 子目录写入。
func TestSaveTaskArtifactRejectsIllegalNames(t *testing.T) {
	s := openTestStore(t)
	bad := []string{
		"../escape.md",
		"a/b.md",
		`a\b.md`,
		"evil:stream",
		"q?.md",
		"star*.md",
		"quote\".md",
		"lt<.md",
		"gt>.md",
		"pipe|.md",
	}
	for _, name := range bad {
		if err := s.SaveTaskArtifact("task-x", name, []byte("x")); err == nil {
			t.Fatalf("非法产物名 %q 必须被拒绝（否则可写出任务目录外 / 备用数据流）", name)
		}
	}
	// 反向：任务目录外不得出现任何文件（真写出去才算穿越）。
	root := s.Layout().DataRoot
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if e.Name() == "escape.md" {
				t.Fatal("非法名竟然真的写出去了（路径穿越）")
			}
		}
	}
}

// TestSaveTaskArtifactRoundTrip 正向判据：合法名可写可读、内容逐字节一致。
func TestSaveTaskArtifactRoundTrip(t *testing.T) {
	s := openTestStore(t)
	payload := []byte("# 产物\n\n内容与换行都要保真\r\n")
	if err := s.SaveTaskArtifact("task-1", "out.md", payload); err != nil {
		t.Fatalf("合法名写入失败: %v", err)
	}
	got, err := s.ReadTaskArtifact("task-1", "out.md")
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("产物内容不保真: got %q want %q", got, payload)
	}
	// 文件落在 layout.TaskDir(taskID) 下，位置契约不能被改。
	want := filepath.Join(s.Layout().TaskDir("task-1"), "out.md")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("产物未落在任务目录下（%s）: %v", want, err)
	}

	// 缺失产物必须返回错误而不是空内容（空内容会被当成「写成功了但是空的」）。
	if _, err := s.ReadTaskArtifact("task-1", "missing.md"); err == nil {
		t.Fatal("读取不存在的产物必须报错，不得返回空内容冒充成功")
	}
}

// TestUpdateReturnsNewPointerAndKeepsOldStable 守护**写时复制**语义。
//
// 失效形态（本仓 2026-10-19 容器内 -race 实测）：退回 `*t = cp` 原地写，
// 会让「持有旧指针的读方」（engine runOne 的 defer 读 running.ChainID）
// 与本次写形成 data race；而修复曾被整文件覆盖丢掉。
// 判据：Update 后，**之前取到的旧指针内容不得变化**。
// 判据载体说明（2026-10-23 实测修正）：第一版用 `Get` 拿「旧指针」——**测不出来**，
// 因为 `Get`/`List`/`Transition` 都返回副本，原地写对内无法观测（MUT-A 首轮 MISSED）。
// 真正可观测的载体是 **`Create` 的入参指针**：`Create` 直接把调用方的 `t` 存进 map
// （`s.tasks[t.ID] = t`），所以调用方手里的指针**就是内部指针**。
// 若 `Update` 退回原地写 `*t = cp`，调用方持有的对象会被改写 —— 这正是 2026-10-19
// 容器内 `-race` 抓到「store.Update 写 vs runOne defer 读」的形态。
func TestUpdateReturnsNewPointerAndKeepsOldStable(t *testing.T) {
	s := openTestStore(t)
	task := &model.Task{ID: "t-cow", Title: "原标题", State: model.StateCandidate}
	if err := s.Create(task); err != nil {
		t.Fatal(err)
	}
	// 调用方指针 = 内部指针（Create 的存储语义），这是可观测载体。
	oldTitle := task.Title

	updated, err := s.Update("t-cow", func(x *model.Task) error { x.Title = "新标题"; return nil })
	if err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	if updated.Title != "新标题" {
		t.Fatalf("返回值应含新内容，got %q", updated.Title)
	}
	// 核心判据：调用方持有的指针（= 老内部指针）**不得**被改写。
	// 若这里判红，说明 Update 退回原地写 —— 与持有该指针的读方 data race。
	if task.Title != oldTitle {
		t.Fatalf("调用方/旧内部指针被就地改写（%q -> %q）—— 写时复制契约破了",
			oldTitle, task.Title)
	}
	// mutator 报错时不得落任何改动。
	if _, err := s.Update("t-cow", func(x *model.Task) error { x.Title = "不该生效"; return os.ErrInvalid }); err == nil {
		t.Fatal("mutator 报错必须向上返回错误")
	}
	if got, _ := s.Get("t-cow"); got.Title != "新标题" {
		t.Fatalf("mutator 报错时改动不得生效，got %q", got.Title)
	}
}

// TestUpsertChainAndGetChainCopySemantics 守护链台账的副本语义。
// 返回内部指针的后果：调用方改 ChainDepth/Fanout 会污染台账，后继生成失控。
func TestUpsertChainAndGetChainCopySemantics(t *testing.T) {
	s := openTestStore(t)
	if err := s.UpsertChain(nil); err == nil {
		t.Fatal("nil 链必须报错")
	}
	if err := s.UpsertChain(&model.Chain{}); err == nil {
		t.Fatal("缺 ID 的链必须报错（否则无法索引）")
	}

	c := &model.Chain{ID: "chain-1", Topic: "根主题", MaxDepth: 3, Depth: 1}
	if err := s.UpsertChain(c); err != nil {
		t.Fatalf("UpsertChain 失败: %v", err)
	}
	got, ok := s.GetChain("chain-1")
	if !ok {
		t.Fatal("GetChain 未命中")
	}
	if got.CreatedAt == 0 || got.UpdatedAt == 0 {
		t.Fatal("UpsertChain 必须补齐 CreatedAt/UpdatedAt（台账时序依赖它）")
	}
	if got.MaxDepth != 3 || got.Depth != 1 {
		t.Fatalf("链字段未保真: maxDepth=%d depth=%d", got.MaxDepth, got.Depth)
	}

	// 改返回值不得影响内部台账。
	got.Depth = 99
	again, _ := s.GetChain("chain-1")
	if again.Depth != 1 {
		t.Fatalf("GetChain 必须返回副本（外部改返回值污染了台账）: depth=%d", again.Depth)
	}
	// 改入参同样不得影响已存副本（UpsertChain 存的是副本）。
	c.MaxDepth = 77
	again2, _ := s.GetChain("chain-1")
	if again2.MaxDepth != 3 {
		t.Fatalf("UpsertChain 必须存副本（外部改入参污染了台账）: depth=%d", again2.MaxDepth)
	}

	if _, ok := s.GetChain("nope"); ok {
		t.Fatal("不存在的链必须返回 false")
	}
}

// TestAuditStampsTimeAndPersists 守护审计 API。
// Audit 是「任何失败路径都留痕」的落地函数（2026-10-22 改造为返回 error）。
// 判据：At 未填时自动补当前时间；返回 error 供调用方做兜底（不得静默吞）。
func TestAuditStampsTimeAndPersists(t *testing.T) {
	s := openTestStore(t)
	if err := s.Audit(AuditRecord{Kind: "error", TaskID: "t-1", Actor: "engine", Message: "why"}); err != nil {
		t.Fatalf("Audit 应成功: %v", err)
	}
	data, err := os.ReadFile(s.Layout().LogFile("audit.jsonl"))
	if err != nil {
		t.Fatalf("审计文件未落盘: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("审计记录为空（写了但没落盘）")
	}
	if !strings.Contains(string(data), "\"at\"") {
		t.Fatalf("审计记录必须带 at 时间戳（时序取证依赖它）: %s", string(data))
	}
}

// TestSaveStateAndStateRoundTrip 守护引擎状态持久化。
func TestSaveStateAndStateRoundTrip(t *testing.T) {
	s := openTestStore(t)
	st := s.State()
	st.Phase = model.PhaseRunning
	if err := s.SaveState(st); err != nil {
		t.Fatalf("SaveState 失败: %v", err)
	}
	if got := s.State(); got.Phase != model.PhaseRunning {
		t.Fatalf("State 未反映刚保存的状态: %q", got.Phase)
	}
	if s.State().UpdatedAt == 0 {
		t.Fatal("SaveState 必须刷新 UpdatedAt（面板按它判新鲜度）")
	}
	// 落盘校验：重新打开应读回同一状态。
	reopened, err := Open(s.Layout())
	if err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	if got := reopened.State(); got.Phase != model.PhaseRunning {
		t.Fatalf("重启后状态未持久化: %q", got.Phase)
	}
}
