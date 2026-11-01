package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEvidenceFabricationCasebookPassesGate 钉住「取证-报告证据虚构-2026-11-01.md」过本仓交付物内容判据
// （三节齐全 + 证据可核查 + 已分级）。
//
// 为什么需要它（2026-11-01 round 184-185 自查抓到的）：这份文档文件名含「报告」⇒ claimsReport 命中，
// 于是它自己就在 reportGateProblems 的管辖范围内；而我初版按「说明文档」的结构写，漏了三节 ——
// 即「写了反虚构判据，自己的取证文档却不合格」。判据必须首先作用于产出它的工具链自身，
// 否则它对外宣称的标准是空的（本仓在册形态：判据与被检对象错位）。
//
// 路径解析从 cwd 上跳找仓库根，找不到 ⇒ Fatalf 而不是 Skip（「找不到」不等于「通过」）。
func TestEvidenceFabricationCasebookPassesGate(t *testing.T) {
	const rel = "docs/取证-报告证据虚构-2026-11-01.md"
	var path string
	d, err := os.Getwd()
	if err != nil {
		t.Fatalf("取工作目录失败: %v", err)
	}
	for i := 0; i < 6; i++ {
		cand := filepath.Join(d, filepath.FromSlash(rel))
		if _, e := os.Stat(cand); e == nil {
			path = cand
			break
		}
		d = filepath.Dir(d)
	}
	if path == "" {
		t.Fatalf("找不到被检文档 %s（从 %s 起向上 6 级）—— 找不到≠通过", rel, "cwd")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读不到 %s: %v", path, err)
	}
	if probs := reportGateProblems(rel, body); len(probs) > 0 {
		t.Fatalf("取证文档自身过不了交付物判据，%d 条: %v", len(probs), probs)
	}
	t.Logf("OK %s（%d 字节）通过 reportGateProblems：三节 + 可核查证据 + 分级", path, len(body))
}
