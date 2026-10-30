package ledgeraudit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestScanFindsAttributionMismatches 用 fixture 钉住扫法本身（round 145 · T101）。
//
// 为什么必须有这条：扫法是我在两轮里定位三处缺陷（T98/T100 与一处 no_artifact）的工具 ——
// 工具自己判错，会把"没有缺陷"说成"有缺陷"或反过来。fixture 覆盖三类行：
//
//	· 历史误归因行（boot_failed + does not serve 原文）⇒ 必须被检出；
//	· 额度原文却记成 rate_limited ⇒ 必须被检出；
//	· 一致行 / 非失败类别（plan、done）/ 复算不出的文本 ⇒ 不计入分子也不计入分母。
func TestScanFindsAttributionMismatches(t *testing.T) {
	root := t.TempDir()
	writeRows(t, root, "task-a", []map[string]any{
		{"at": 1, "taskId": "task-a", "kind": "boot_failed", "error": `dsh: SERVER: our free model does not serve "combo:auto-free" on this egress`},
		{"at": 2, "taskId": "task-a", "kind": "rate_limited", "error": `RATE_LIMIT: Sign up for free to continue and explore 500 other models. Takes 2 minutes to sign up.`},
		{"at": 3, "taskId": "task-a", "kind": "rate_limited", "error": `dsh: RATE_LIMIT: too many requests; retry later`},
		{"at": 0, "taskId": "task-a", "kind": "plan", "error": "方案模型无输出"},
		{"at": 0, "taskId": "task-a", "kind": "done", "error": ""},
		{"at": 4, "taskId": "task-a", "kind": "bad_exit", "error": "任务无最终答案"},
	})
	rep, err := Scan(root, 0, 8)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Rows != 6 {
		t.Fatalf("应读到 6 行，实得 %d", rep.Rows)
	}
	// 可判定行 = 前 3 条文本可判 + bad_exit（\"任务无最终答案\" 判不出 ⇒ 不计）⇒ 3 条
	if rep.Judged != 3 {
		t.Fatalf("可判定行应为 3（plan/done 跳过、判不出的不计），实得 %d", rep.Judged)
	}
	if rep.Mismatched != 2 {
		t.Fatalf("应检出 2 条不一致，实得 %d（%v）", rep.Mismatched, rep.Groups)
	}
	if rep.Groups["boot_failed → 应为 not_served"] != 1 {
		t.Fatalf("第一簇应命中 1 条，实得 %v", rep.Groups)
	}
	if rep.Groups["rate_limited → 应为 quota_exhausted"] != 1 {
		t.Fatalf("第二簇应命中 1 条，实得 %v", rep.Groups)
	}
}

// sinceMs 是回归窗口：只看某个修复时刻之后的行（历史残留不该污染回归读数）。
func TestScanHonoursSinceWindow(t *testing.T) {
	root := t.TempDir()
	writeRows(t, root, "task-b", []map[string]any{
		{"at": 100, "taskId": "task-b", "kind": "boot_failed", "error": `our free model does not serve "x" on this egress`},
		{"at": 1000, "taskId": "task-b", "kind": "boot_failed", "error": `our free model does not serve "y" on this egress`},
	})
	rep, err := Scan(root, 500, 8)
	if err != nil {
		t.Fatal(err)
	}
	// Rows 仍数全部行（读数需要知道样本量），Judged/Mismatched 只看窗口内
	if rep.Rows != 2 || rep.Judged != 1 || rep.Mismatched != 1 {
		t.Fatalf("窗口口径错：Rows=%d Judged=%d Mismatched=%d，want 2/1/1", rep.Rows, rep.Judged, rep.Mismatched)
	}
}

func writeRows(t *testing.T, root, taskID string, rows []map[string]any) {
	t.Helper()
	dir := filepath.Join(root, "tasks", taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b []byte
	for _, r := range rows {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b = append(b, line...)
		b = append(b, '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "attempts.jsonl"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}
