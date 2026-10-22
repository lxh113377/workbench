package web

import (
	"os"
	"path/filepath"
	"testing"

	"workbench/internal/model"
)

// fakeArtifactStore 只实现读取所需的两个方法，把任务目录指到临时目录，
// 让解析逻辑能脱离 Engine/Server 单测（含反向用例）。
type fakeArtifactStore struct{ root string }

func (f fakeArtifactStore) ReadTaskArtifact(taskID, name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(f.root, taskID, name))
}

func (f fakeArtifactStore) TaskDir(taskID string) string { return filepath.Join(f.root, taskID) }

func writeTaskFile(t *testing.T, root, taskID, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestReadTaskArtifactResolution 钉住三级回落的每一级，以及「读不到要如实报 false」。
//
// 一手代价（2026-10-22 前端走查实测）：详情接口与批量导出都硬编码读 final.md，
// 而产出文件名由 engine.choosePrimary 按格式与体积决定（实测样本是 article.md），
// 于是 outputBytes=5935 的真实产出在面板里显示成「（暂无产出）」（.doc 只有 6 个字符）。
func TestReadTaskArtifactResolution(t *testing.T) {
	root := t.TempDir()
	src := fakeArtifactStore{root: root}

	artPath := writeTaskFile(t, root, "t-primary", "article.md", "# 正文一")
	writeTaskFile(t, root, "t-primary", "final.md", "兜底名不该被选中")

	writeTaskFile(t, root, "t-fallback", "final.md", "# 正文二")

	writeTaskFile(t, root, "t-scan", "notes.txt", "短")
	writeTaskFile(t, root, "t-scan", "report.md", "# 最大的 markdown 正文")
	writeTaskFile(t, root, "t-scan", "events.jsonl", "这个更长但不是产出格式 xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")

	writeTaskFile(t, root, "t-stale", "final.md", "# 正文四")

	cases := []struct {
		name     string
		task     *model.Task
		wantBody string
		wantFrom string
		wantOK   bool
	}{
		{
			name:     "① OutputFile 绝对路径优先（真实形态：article.md）",
			task:     &model.Task{ID: "t-primary", OutputFile: artPath},
			wantBody: "# 正文一", wantFrom: "article.md", wantOK: true,
		},
		{
			name:     "② 无 OutputFile 时回落任务目录 final.md",
			task:     &model.Task{ID: "t-fallback"},
			wantBody: "# 正文二", wantFrom: "final.md", wantOK: true,
		},
		{
			name:     "③ 两者皆无时扫目录取最大 markdown（忽略非产出格式）",
			task:     &model.Task{ID: "t-scan"},
			wantBody: "# 最大的 markdown 正文", wantFrom: "report.md", wantOK: true,
		},
		{
			name:     "④ OutputFile 失效（被改名/清理）时仍能读到产出",
			task:     &model.Task{ID: "t-stale", OutputFile: filepath.Join(root, "t-stale", "gone.md")},
			wantBody: "# 正文四", wantFrom: "final.md", wantOK: true,
		},
		{
			name:   "⑤ 反向：目录不存在必须报 false（禁止把读不到当有产出）",
			task:   &model.Task{ID: "t-missing"},
			wantOK: false,
		},
		{
			name:   "⑥ 反向：nil task 不得 panic",
			task:   nil,
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, from, ok := readTaskArtifact(src, tc.task)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v，期望 %v（from=%q body=%q）", ok, tc.wantOK, from, body)
			}
			if !tc.wantOK {
				return
			}
			if body != tc.wantBody {
				t.Fatalf("body=%q，期望 %q", body, tc.wantBody)
			}
			if from != tc.wantFrom {
				t.Fatalf("from=%q，期望 %q", from, tc.wantFrom)
			}
		})
	}
}

// TestReadTaskArtifactRejectsEmptyFile 钉住「空文件不算产出」：
// 历史上曾出现过 events.jsonl 被空内容原子覆盖成 0 字节的事故，
// 若解析器把 0 字节当命中，面板又会显示成「（暂无产出）」却报 ok=true。
func TestReadTaskArtifactRejectsEmptyFile(t *testing.T) {
	root := t.TempDir()
	src := fakeArtifactStore{root: root}
	path := writeTaskFile(t, root, "t-empty", "article.md", "")
	body, from, ok := readTaskArtifact(src, &model.Task{ID: "t-empty", OutputFile: path})
	if ok {
		t.Fatalf("0 字节文件被判为命中（from=%q body=%q）——空产出不算产出", from, body)
	}
}
