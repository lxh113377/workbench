package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAuditReportGradedPassesOwnGate 自证：本轮交付的五轴审查报告（分级版）必须通过本仓
// 报告门禁 reportGateProblems（三节齐全 + 证据可核查 + 已分级）。
//
// 为什么单独成一个文件（不并进 audit_report_gate_test.go）：那个文件是 2026-10-30 已入库件，
// 里面已有一个 readAuditReport helper；本轮我一度把它整个重写导致 helper 被删、包编不过
// （见 DECOMPOSE round 176 记录），教训是**改已入库文件前先查它是否受跟踪**（git ls-files）。
//
// 路径解析不写死层级：从 cwd 向上逐级找含 docs/ 的那一层，找不到即 Fail（不是 Skip）——
// 「找不到当通过」是本仓在册的假绿形态（round 175 的空集 PASS 同族）。
func findRepoDoc(rel string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	d := cwd
	for i := 0; i < 6; i++ {
		cand := filepath.Join(d, filepath.FromSlash(rel))
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
		d = filepath.Dir(d)
	}
	return "", os.ErrNotExist
}

func TestAuditReportGradedPassesOwnGate(t *testing.T) {
	const rel = `docs/五轴审查-分级-2026-11-01.md`
	path, err := findRepoDoc(rel)
	if err != nil {
		t.Fatalf("找不到被检报告 %s —— 找不到≠通过（判据必须作用在真实对象上）", rel)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读不到报告: %v", err)
	}
	probs := reportGateProblems(rel, body)
	if len(probs) > 0 {
		t.Fatalf("本次审查报告自身过不了自家门禁，%d 条问题: %v", len(probs), probs)
	}
	t.Logf("OK %s（%d 字节）通过 reportGateProblems 三节+证据+分级判据", path, len(body))
}

// containsAnySub 是本地小工具（不引入第三方库，符合纯标准库约束）。
func containsAnySub(probs []string, sub string) bool {
	for _, p := range probs {
		if strings.Contains(p, sub) {
			return true
		}
	}
	return false
}

// TestAuditReportGateNegControls 负向对照：证明「三节+证据+分级」判据有牙。
// 三条各削掉一个要件，必须都判红；没有这三条，上面的 PASS 只是恒绿。
func TestAuditReportGateNegControls(t *testing.T) {
	base := []byte("# 测试报告\n\n## 结论\n建议合并。\n\n## 证据\n- workbench/internal/engine/engine.go:983 超长函数\n- 可复跑命令：go vet ./...\n\n## 门禁建议\n1. 拆函数。\n\n分级：P0 无 / P1 三项 / P2 四项。\n")
	if p := reportGateProblems("docs/负控-合格样本.md", base); len(p) != 0 {
		t.Fatalf("合格样本被判红（判据过严会误伤真实报告）: %v", p)
	}
	noSev := []byte("# 测试报告\n\n## 结论\n建议合并。\n\n## 证据\n- engine.go:983\n\n## 门禁建议\n1. 拆函数。\n")
	if p := reportGateProblems("docs/负控-未分级.md", noSev); !containsAnySub(p, "未分级") {
		t.Fatalf("缺分级未被判红（判据无牙）: %v", p)
	}
	noEv := []byte("# 测试报告\n\n## 结论\n建议合并。\n\n## 证据\n无。\n\n## 门禁建议\n1. 拆函数。\n\n分级：P1。\n")
	if p := reportGateProblems("docs/负控-无证据.md", noEv); !containsAnySub(p, "证据") {
		t.Fatalf("缺可核查证据未被判红（判据无牙）: %v", p)
	}
	noSec := []byte("# 测试报告\n\n## 结论\n建议合并。\n\n## 证据\n- engine.go:983\n\n分级：P1。\n")
	if p := reportGateProblems("docs/负控-缺节.md", noSec); !containsAnySub(p, "缺少小节") {
		t.Fatalf("缺整节未被判红（判据无牙）: %v", p)
	}
	t.Logf("三条负控全部判红 ⇒ 判据有牙")
}
