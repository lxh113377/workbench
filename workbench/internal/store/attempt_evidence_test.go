package store

import (
	"os"
	"strings"
	"testing"

	"workbench/internal/model"
)

// TestAttemptEvidenceFieldsRoundTrip 钉住「取证字段」能落盘并被读回（T15-④ / T21）。
//
// 动机：只靠 `error` 摘要排查时仍要翻任务目录；把 stderr 位置、回答体量、DSH 无头命令行写进 attempts.jsonl，
// 就能一眼分辨「子进程报错」「模型空产出」与「这一次到底经没经 DSH 无头控制」。
func TestAttemptEvidenceFieldsRoundTrip(t *testing.T) {
	s, layout := newTestStore(t)
	task := &model.Task{ID: "task-evidence", Title: "取证台账", State: model.StateCandidate}
	if err := s.Create(task); err != nil {
		t.Fatal(err)
	}
	cmd := "dsh.exe --expose-internals D:\\app\\desktop-cli.js --profile web-desktop --json -"
	s.AppendAttempt(AttemptRecord{
		TaskID: "task-evidence", Attempt: 0, Model: "m1", Kind: "no_artifact", Action: "fail",
		ElapsedMs: 1000, Error: "执行失败: no_artifact",
		StderrPath: "D:\\x\\stderr.txt", ProducedRunes: 37, DSHCommand: cmd,
	})
	recs, err := s.ListAttempts("task-evidence")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("attempts = %d, want 1", len(recs))
	}
	if recs[0].StderrPath != "D:\\x\\stderr.txt" {
		t.Fatalf("stderrPath 未回读：%+v", recs[0])
	}
	if recs[0].ProducedRunes != 37 {
		t.Fatalf("producedRunes 未回读：%+v", recs[0])
	}
	if recs[0].DSHCommand != cmd {
		t.Fatalf("dshCommand 未回读：%+v", recs[0])
	}
	// 人直接读台账文件时也要自证：三个键必须出现在 JSONL 里。
	data, err := os.ReadFile(layout.TaskFile("task-evidence", "attempts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"stderrPath", "producedRunes", "dshCommand", "web-desktop"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("attempts.jsonl 缺少 %s：%s", want, data)
		}
	}
	// 零产出形态：字段走 omitempty，读回仍是零值，语义一致。
	s.AppendAttempt(AttemptRecord{TaskID: "task-evidence", Attempt: 1, Model: "m2", Kind: "bad_exit", Action: "fail"})
	recs, err = s.ListAttempts("task-evidence")
	if err != nil || len(recs) != 2 {
		t.Fatalf("第二次读取失败：recs=%d err=%v", len(recs), err)
	}
	if recs[1].ProducedRunes != 0 || recs[1].StderrPath != "" || recs[1].DSHCommand != "" {
		t.Fatalf("零产出应读回零值：%+v", recs[1])
	}
}
