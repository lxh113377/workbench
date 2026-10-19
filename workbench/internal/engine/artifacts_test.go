package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

const tFence = "```"

// TestMaterializeArtifactsPass 覆盖 P3 正向路径：模型给出文件+验收命令，
// 工作台落盘真实文件并真实执行命令，通过则 Verified=true。
func TestMaterializeArtifactsPass(t *testing.T) {
	e, _, layout := newTestEngine(t)
	taskID := "task-art-pass"
	if err := e.store.Create(&model.Task{ID: taskID, Title: "产物测试", State: model.StateCandidate}); err != nil {
		t.Fatal(err)
	}
	taskDir := layout.TaskDir(taskID)
	if err := os.MkdirAll(filepath.Join(taskDir, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	ip := pickTestInterp(t)
	final := artifactsFinal(t, ip, "完成。", "hello.md", "# 你好\n")

	task, _ := e.store.Get(taskID)
	rep := e.materializeArtifacts(task, final, taskDir, config.DefaultConfig())
	if rep == nil {
		t.Fatal("应解析出产物并返回验收报告")
	}
	if !rep.Pass {
		t.Fatalf("验收应通过: %+v", rep)
	}
	// 文件真的落盘了吗
	got, err := os.ReadFile(filepath.Join(taskDir, "workspace", "hello.md"))
	if err != nil {
		t.Fatalf("产物文件未落盘: %v", err)
	}
	if !strings.Contains(string(got), "你好") {
		t.Fatalf("落盘内容不对: %q", string(got))
	}
	// 验收证据落盘了吗
	if _, err := os.Stat(filepath.Join(taskDir, "verify.json")); err != nil {
		t.Fatal("verify.json 未落盘（证据缺失）")
	}
	if _, err := os.Stat(filepath.Join(taskDir, "artifacts-index.json")); err != nil {
		t.Fatal("artifacts-index.json 未落盘")
	}
}

// TestMaterializeArtifactsFail 覆盖反向路径：命令不通过 → 报告 Pass=false 且带人读原因。
func TestMaterializeArtifactsFail(t *testing.T) {
	e, _, layout := newTestEngine(t)
	taskID := "task-art-fail"
	if err := e.store.Create(&model.Task{ID: taskID, Title: "产物失败", State: model.StateCandidate}); err != nil {
		t.Fatal(err)
	}
	taskDir := layout.TaskDir(taskID)
	_ = os.MkdirAll(filepath.Join(taskDir, "workspace"), 0o755)
	ip := pickTestInterp(t)
	fpath, fcontent, fcmd := ip.failScriptCase()
	manifestCmd, err := jsonMarshalLit(fcmd)
	if err != nil {
		t.Fatal(err)
	}
	final := "<<<ARTIFACTS>>>\n" + tFence + "manifest\n" +
		`{"files":[{"path":"` + fpath + `"}],"verify":[{"cmd":` + manifestCmd + `,"expectExit":0,"timeout":30}]}` +
		"\n" + tFence + "\n" + tFence + "file:" + fpath + "\n" + fcontent + tFence + "\n<<<END>>>\n"
	task, _ := e.store.Get(taskID)
	rep := e.materializeArtifacts(task, final, taskDir, config.DefaultConfig())
	if rep == nil {
		t.Fatal("应返回验收报告")
	}
	if rep.Pass {
		t.Fatal("命令退出码不符，必须判失败")
	}
	if !strings.Contains(rep.FailReason, "退出码 2") {
		t.Fatalf("失败必须因脚本真实退出码 2（而非启动失败——判据不许被偷换）: %q", rep.FailReason)
	}
	if rep.FailReason == "" {
		t.Fatal("失败必须带人读原因（返工 prompt 依赖它）")
	}
}

// TestMaterializeArtifactsUnclosedFence 覆盖截断检测：
// 围栏未闭合 → 结构缺陷 → 直接判失败，不进入真实执行。
func TestMaterializeArtifactsUnclosedFence(t *testing.T) {
	e, _, layout := newTestEngine(t)
	taskID := "task-art-trunc"
	if err := e.store.Create(&model.Task{ID: taskID, Title: "截断", State: model.StateCandidate}); err != nil {
		t.Fatal(err)
	}
	taskDir := layout.TaskDir(taskID)
	_ = os.MkdirAll(filepath.Join(taskDir, "workspace"), 0o755)
	final := "<<<ARTIFACTS>>>\n" + tFence + "manifest\n" +
		`{"files":[{"path":"a.py"}],"verify":[{"cmd":"python a.py","expectExit":0}]}` +
		"\n" + tFence + "\n" + tFence + "file:a.py\nprint(1)\nprint(2)\n" // 未闭合
	task, _ := e.store.Get(taskID)
	rep := e.materializeArtifacts(task, final, taskDir, config.DefaultConfig())
	if rep == nil || rep.Pass {
		t.Fatalf("围栏未闭合必须判失败: %+v", rep)
	}
	if !strings.Contains(rep.FailReason, "未闭合") {
		t.Fatalf("失败原因应指出截断: %q", rep.FailReason)
	}
}

// TestMaterializeArtifactsNoProtocol 覆盖旧格式：**内容足够长**但没有产物块 → 返回 nil
// （退化为纯文本归档，仍会被标记为未验收，不计入今日产出）。
//
// 2026-10-19 语义边界修正：
//   - 短且无产物块 → 失败报告（过程独白，见 TestMaterializeRejectsTinyOutput）；
//   - 长但无产物块 → nil（纯文本归档，历史兼容路径）。
func TestMaterializeArtifactsNoProtocol(t *testing.T) {
	e, _, layout := newTestEngine(t)
	taskID := "task-art-none"
	if err := e.store.Create(&model.Task{ID: taskID, Title: "无协议", State: model.StateCandidate}); err != nil {
		t.Fatal(err)
	}
	taskDir := layout.TaskDir(taskID)
	_ = os.MkdirAll(filepath.Join(taskDir, "workspace"), 0o755)
	task, _ := e.store.Get(taskID)
	// 内容足够长（远超 200 字门槛），因此走纯文本归档路径。
	long := strings.Repeat("这是一段没有产物块的纯文字内容，仅用于验证旧格式兼容路径。", 20)
	rep := e.materializeArtifacts(task, long, taskDir, config.DefaultConfig())
	if rep != nil {
		t.Fatalf("长内容但无产物块应返回 nil（走纯文本归档）: %+v", rep)
	}
}

// TestStatsExcludesDegraded 覆盖统计口径：降级任务不计入今日产出。
func TestStatsExcludesDegraded(t *testing.T) {
	e, st, _ := newTestEngine(t)
	mk := func(id string, state model.TaskState) {
		if err := st.Create(&model.Task{ID: id, Title: id, State: model.StateCandidate}); err != nil {
			t.Fatal(err)
		}
		if state != model.StateCandidate {
			// 走合法路径
			steps := map[model.TaskState][]model.TaskState{
				model.StateDegraded: {model.StateApproved, model.StateQueued, model.StateRunning, model.StateDegraded},
				model.StateDone:     {model.StateApproved, model.StateQueued, model.StateRunning, model.StateDone},
			}
			for _, to := range steps[state] {
				if _, err := st.Transition(id, to, "test", ""); err != nil {
					t.Fatalf("%s -> %s: %v", id, to, err)
				}
			}
		}
	}
	mk("d1", model.StateDone)
	mk("g1", model.StateDegraded)
	s := e.Stats()
	if s.DoneTasks != 1 {
		t.Fatalf("DoneTasks = %d, want 1（degraded 不计入）", s.DoneTasks)
	}
	if s.DegradedTasks != 1 {
		t.Fatalf("DegradedTasks = %d, want 1", s.DegradedTasks)
	}
	if s.TodayDone != 1 {
		t.Fatalf("TodayDone = %d, want 1", s.TodayDone)
	}
}
