package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIgnoredErrorInventoryPassesGate 钉住「docs/忽略错误分类盘点-2026-11-01.md」能过本仓报告门禁。
//
// 为什么需要它：那份盘点文档在 docs/ 下、正文含「结论/证据/门禁建议」三节标题 ⇒ claimsReport 命中，
// 必须满足三节齐全 + 证据可核查 + 已分级。若以后有人往里补内容却漏了某节，这条会立刻红。
// （三节齐全 + 证据可核查 + 已分级）。它是 docs/ 下的 .md 且文件名含「盘点」，
// 但 claimsReport 会扫三节标题 —— 若命中就受判据约束，必须真过。
func TestIgnoredErrorInventoryPassesGate(t *testing.T) {
	const rel = `docs/忽略错误分类盘点-2026-11-01.md`
	var path string
	d, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		cand := filepath.Join(d, filepath.FromSlash(rel))
		if _, err := os.Stat(cand); err == nil {
			path = cand
			break
		}
		d = filepath.Dir(d)
	}
	if path == "" {
		t.Fatalf("找不到被检文档 %s —— 找不到≠通过", rel)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读不到: %v", err)
	}
	probs := reportGateProblems(rel, body)
	if len(probs) > 0 {
		t.Fatalf("盘点文档过不了报告门禁，%d 条: %v", len(probs), probs)
	}
	t.Logf("OK：%s（%d 字节）通过报告门禁三节+证据+分级判据", path, len(body))
}
