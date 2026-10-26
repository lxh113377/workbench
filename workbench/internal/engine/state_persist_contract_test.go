package engine

// state_persist_contract_test.go -- 钉住「相位落盘失败时不得向用户宣布成功」（2026-10-26 round 246 实测定位）。
//
// 为什么需要（不是风格偏好）：engine.go 里 SaveState 共 7 处，L435/L461 严格判错，L417/L475/L566/L601/L617
// 用 _ = 吞掉。Pause()/Resume()/stop() 的签名都是 error，却在落盘失败时照样 emitEvent（引擎已暂停/
// 已恢复/已停止）并 return nil。而 Start() 的注释（engine.go:408-409）自己写明「重启后的接续判据
// shouldAutoStart 读的是**磁盘** state.json」⇒ 磁盘没落上时 7×24 重启后相位回到旧值：
// 用户以为暂停/停机了，实例却自己跑起来。这不是理论路径 —— SaveState 的错误来自文件 I/O。
//
// 注入方式（不依赖 config 的私有 seam、不改生产结构）：把 layout.StateFile 指到**普通文件之下**的路径
// ⇒ WriteFileAtomic 的第一步 os.MkdirAll 必失败（ENOTDIR），跨平台确定、与权限/杀软/占用无关。
// 首个用例是注入探针：先证明 SaveState 真的报错，否则下面全是空转（本仓在册的 vacuous pass 家族）。
//
// 判据只用**可观测事实**（返回 err、事件条数、QueueFile 是否仍在写），不比对错误文案 —— 那会把实现细节
// 焊进测试，改一句措辞就假红。

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/store"
	"workbench/internal/templates"
)

// captureEmitter 记录引擎发出的事件，用于断言「失败时不得宣布成功」。
type captureEmitter struct {
	msgs []string
}

func (c *captureEmitter) Emit(ev model.Event) {
	c.msgs = append(c.msgs, ev.Msg)
}

func (c *captureEmitter) countOf(msg string) int {
	n := 0
	for _, m := range c.msgs {
		if m == msg {
			n++
		}
	}
	return n
}

func newBrokenStateEngine(t *testing.T) (*Engine, config.Layout, *captureEmitter) {
	t.Helper()
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	// 注入：StateFile 落在一个普通文件之下 ⇒ 写它的 MkdirAll 必失败
	blocker := filepath.Join(layout.DataRoot, "state-blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	layout.StateFile = filepath.Join(blocker, "state.json")
	cstore, err := config.NewStore(layout.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := templates.Open(layout.DataRoot + string(os.PathSeparator) + "templates.json")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(layout)
	if err != nil {
		t.Fatalf("注入不该让 store 打不开（loadState 应容忍 state.json 缺失）: %v", err)
	}
	cap := &captureEmitter{}
	e, err := New(layout, cstore, st, reg, cap)
	if err != nil {
		t.Fatal(err)
	}
	e.SetRunner(&noopTestRunner{})
	e.ReloadConfig()
	// 与 newTestEngine 同款收尾（否则 TempDir 在 Windows 上因 WAL 句柄/在途写假红）
	t.Cleanup(func() {
		if !e.WaitInflight(5 * time.Second) {
			t.Log("在途后台工作 5s 未排空（继续收尾）")
		}
	})
	t.Cleanup(func() { _ = st.Close() })
	return e, st.Layout(), cap
}

// 探针：注入必须真的让落盘失败 —— 否则本文件所有判据都是空转。
func TestBrokenStateFileInjectionIsReal(t *testing.T) {
	_, lay, _ := newBrokenStateEngine(t)
	if err := config.WriteJSONAtomic(lay.StateFile, map[string]int{"a": 1}); err == nil {
		t.Fatal("注入失效：写 StateFile 竟然成功，后续判据会是 vacuous pass")
	}
}

func TestPauseReportsStatePersistFailure(t *testing.T) {
	e, _, cap := newBrokenStateEngine(t)
	e.mu.Lock()
	e.phase = model.PhaseRunning
	e.mu.Unlock()
	if err := e.Pause(); err == nil {
		t.Fatal("落盘失败时 Pause 仍报成功：面板显示已暂停而磁盘仍是 running，7×24 重启后会自己跑起来")
	}
	if n := cap.countOf("引擎已暂停"); n != 0 {
		t.Fatalf("落盘失败后仍发出「引擎已暂停」%d 条（应为 0：不得宣布未落地的成功）", n)
	}
}

func TestResumeReportsStatePersistFailure(t *testing.T) {
	e, _, cap := newBrokenStateEngine(t)
	e.mu.Lock()
	e.phase = model.PhasePaused
	e.mu.Unlock()
	if err := e.Resume(); err == nil {
		t.Fatal("落盘失败时 Resume 仍报成功")
	}
	if n := cap.countOf("引擎已恢复"); n != 0 {
		t.Fatalf("落盘失败后仍发出「引擎已恢复」%d 条（应为 0）", n)
	}
}

// 停机这条还要钉住另一半语义：state 落盘失败**不得阻断** Savepoint（队列快照仍要尽力落盘），
// 所以修法必须是「记录并合并错误」而不是提前 return。QueueFile 是否出现就是可观测证据。
func TestStopReportsFailureButStillSavepoints(t *testing.T) {
	e, lay, cap := newBrokenStateEngine(t)
	e.mu.Lock()
	e.phase = model.PhaseRunning
	e.mu.Unlock()
	if err := e.Stop(); err == nil {
		t.Fatal("落盘失败时 Stop 仍报成功")
	}
	if n := cap.countOf("引擎已停止"); n != 0 {
		t.Fatalf("落盘失败后仍宣布「引擎已停止」%d 条（应为 0）", n)
	}
	if _, err := os.Stat(lay.QueueFile); err != nil {
		t.Fatalf("Savepoint 被 state 失败阻断了（QueueFile 不存在: %v）—— 停机时队列快照必须仍尽力落盘", err)
	}
}
