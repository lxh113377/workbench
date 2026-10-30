package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestAuditReportCitationsResolve 钉住「报告的每条代码位置引用都必须命中那一行」。
//
// 两轮现场叠加出来的判据：
//  1. round 130：报告写完用 node 逐条对账，10 条里 8 条行号是错的 —— 不是写错，而是我随后几轮
//     持续编辑 engine.go/manager.go，行号跟着漂而报告没跟着改。内容门禁（三节/证据/分级）结构上
//     看不出这种错：它只判「有没有 file:line 这种形状」，不判「对不对」。
//  2. round 132：我把这条判据写成「取最长 token 作锚」，于是**我自己 T85 改动造成的新漂移它看不见** ——
//     报告引 `if waits >= budget {` 在 :1826，我在它上方插了 13 行把它顶到 :1839，而 :1826 变成
//     `if budget > x.PoolWaitCeil {`；最长 token 是通用词 "budget" ⇒ 判据照样绿。
//     并行子代理用「逐字 + 全 token」的独立判据抓到了它 ⇒ 判据强度不足 = 判据不存在。
//     现在口径：整段逐字命中，或全部代码形态 token 命中（AND），两者都不满足即判红。
func TestAuditReportCitationsResolve(t *testing.T) {
	text := string(readAuditReport(t))
	matches := citationRe.FindAllStringSubmatch(text, -1)
	if len(matches) < 8 {
		t.Fatalf("只解析到 %d 条代码引用，口径可疑（判据可能被空集/正则失配蒙过）", len(matches))
	}
	for _, m := range matches {
		rel, lineStr, claim := m[1], m[2], m[3]
		line, aerr := strconv.Atoi(lineStr)
		if aerr != nil {
			t.Fatalf("行号解析失败 %q: %v", lineStr, aerr)
		}
		claim = cleanClaim(claim)
		p, err := filepath.Abs(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", rel, err)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("报告引用了不存在的文件：%s（%s:%d）", rel, rel, line)
			continue
		}
		lines := strings.Split(string(raw), "\n")
		if line < 1 || line > len(lines) {
			t.Errorf("行号越界：%s:%d（该文件共 %d 行）", rel, line, len(lines))
			continue
		}
		at := strings.TrimSpace(lines[line-1])
		if citationMatches(claim, at) {
			continue
		}
		real := -1
		for i, l := range lines {
			if citationMatches(claim, strings.TrimSpace(l)) {
				real = i + 1
				break
			}
		}
		t.Errorf("报告引用漂移：%s:%d 声称 %q，实际该行 %q；命中位置=%s:%d（改准或删除引用，不许放宽判据）",
			rel, line, claim, at, rel, real)
	}
}

// citationRe 抓「路径:行号 + 同行代码片段」。
var citationRe = regexp.MustCompile(`(?m)^\s*(?:workbench/)?((?:internal|docs|tests|setup)[^\s:]*\.go):(\d+)\s+(.+?)\s*$`)

// cleanClaim 去掉引用尾部的中文注解（"← 无 kind"、"（限频 60 秒…）"）与包裹反引号。
// 必须先剥：第一版没剥，把 "func logSupplyBlockedOnce() {（限频…" 整段当锚去比一行纯代码，
// 必然不等 ⇒ 误报漂移。那是判据自己的错，不是报告的错。
func cleanClaim(claim string) string {
	c := strings.TrimSpace(claim)
	if k := strings.Index(c, "\u2190"); k >= 0 {
		c = c[:k]
	}
	if k := strings.Index(c, "\uff08"); k >= 0 {
		c = c[:k]
	}
	return strings.TrimSpace(strings.Trim(c, "`"))
}

// citationMatches：整段逐字命中，或全部代码形态 token 命中（AND）。
// 没有代码 token 时（纯中文声称，如「连试 8 个模型」）只认逐字。
func citationMatches(claim, line string) bool {
	if claim == "" || line == "" {
		return false
	}
	if strings.Contains(line, claim) {
		return true
	}
	var toks []string
	for _, w := range strings.Fields(claim) {
		w = strings.Trim(w, "`.,;:()[]{}")
		if len(w) < 4 || !codeTokenRe.MatchString(w) {
			continue
		}
		toks = append(toks, w)
	}
	if len(toks) == 0 {
		return false
	}
	for _, w := range toks {
		if !strings.Contains(line, w) {
			return false
		}
	}
	return true
}

// TestCitationGateIsStrongEnough 内建负控：通用词单独命中**不算命中**。
// 这就是 round 132 我看不见自己 T85 漂移的确切原因 —— 把它写成测试，防止下次又退回去。
func TestCitationGateIsStrongEnough(t *testing.T) {
	const claim = "if waits >= budget {"
	// 我 T85 插入后 :1826 的真实内容（含通用词 budget，但不是声称的那行）
	decoy := "if budget > x.PoolWaitCeil {"
	if citationMatches(claim, decoy) {
		t.Fatal("判据仍把通用词命中当成命中 ⇒ 抓不到 T85 这类自我漂移（强度不足）")
	}
	if !citationMatches(claim, "if waits >= budget {") {
		t.Fatal("逐字命中必须成立（否则判据过严会误报）")
	}
	// 全 token AND：三个 token 都在同一行才算命中
	if !citationMatches("x.PoolWaits++", "x.PoolWaits++") {
		t.Fatal("自等行必须命中")
	}
}

// codeTokenRe 认标识符/简单表达式形态的词（长度>=4 且含代码特征）。
var codeTokenRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\[\]()=<>!&|*+/-]{3,}$`)
