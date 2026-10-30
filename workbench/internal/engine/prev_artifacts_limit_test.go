package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadPrevArtifactsRespectsLimit 是对"根因 B"的取证式测试（round 136）。
// 我只从代码读出「limit 是累计口径」，按本会话纪律不能拿代码阅读当结论 —— 这条测试
// 把两种可能行为钉成一种：越过累计阈值后是否还继续吸收后续文件。
// 本轮只确证行为，不改行为；确证完才谈得上要不要加单文件上限。
func TestReadPrevArtifactsRespectsLimit(t *testing.T) {
	dir := t.TempDir()
	const limit = 2000
	// 一个远超 limit 的大文件 + 三个小文件（big.md 字典序在前，先被读到）
	huge := strings.Repeat("x", limit*5)
	if err := os.WriteFile(filepath.Join(dir, "big.md"), []byte(huge), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"s1.md", "s2.md", "s3.md"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("small-content-"+n), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := readPrevArtifacts(dir, limit)
	if len(got) == 0 {
		t.Fatal("一个产物都没读到：测不到点上（先修测试，别改实现）")
	}
	total := 0
	for _, f := range got {
		total += len(f.Body)
	}
	t.Logf("读回 %d 个产物，合计 %d 字节（limit=%d）", len(got), total, limit)
	// 关键：越过 limit 后不得继续把后续文件全吸进来（否则返工 prompt 体积随工作区文件数增长）
	if len(got) > 2 {
		t.Fatalf("limit 没起到截断后续文件的作用：读到 %d 个产物，合计 %d 字节（limit=%d）", len(got), total, limit)
	}
}
