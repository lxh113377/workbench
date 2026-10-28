package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCollectOutputsSkipsScratch 钉住产物收集不吞别的写者的草稿（round 61 · T60）。
//
// live 实锤：任务产物目录里出现 Lead 的 runtime/tmp/commitmsg-t59.md，sha256 与工作空间同名文件一致 ⇒
// collectOutputsSince 把「窗口内变更」当成了「本任务产出」。产物集合决定有没有产出与返工提示词，
// 所以这不只是脏数据，而是能让零产出任务凭空变绿的口子。
func TestCollectOutputsSkipsScratch(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"docs/审查报告.md":            "# 报告",
		"src/main.go":             "package main",
		"runtime/tmp/helper.ps1":  "Write-Host 1",
		"runtime/tmp/sub/deep.md": "深一层也要跳过",
		"xtmp/rel-abc/x.go":       "package x",
	}
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, names, err := collectOutputsSince(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"docs/审查报告.md", "src/main.go"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("项目内产出必须被收集，缺 %q（实收 %v）", want, names)
		}
	}
	for rel := range files {
		if rel == "docs/审查报告.md" || rel == "src/main.go" {
			continue
		}
		if _, ok := got[rel]; ok {
			t.Fatalf("代理/工具草稿不得进产物集合：%q（实收 %v）", rel, names)
		}
	}
}
