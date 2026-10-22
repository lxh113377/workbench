package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestBodyFontNotShrunkAtNarrowViewport 钉住「窄视口不得靠缩小正文字号来适配」。
//
// 一手实测（2026-10-22 WCAG 1.4.4 缩放审计）：`@media (max-width:480px){ body{font-size:14px} }`
// 把正文从 15px 降到 14px。问题在于窄视口有两种成因：① 手机（真小屏，14px 合理）；
// ② **用户缩放放大**（200%/400% 后视口同样变窄）。CSS 无法区分两者，而后者里
// 「为了看清而放大、字号反而变小」与 1.4.4 的意图正好相反。
// 实测对比：400% 缩放下 bodyFont 14px（修前）→ 15px（修后），零溢出零截断不变。
//
// 判据：CSS 里不得存在把 body 字号改小的媒体查询；窄视口适配只能靠布局，不能牺牲可读性。
func TestBodyFontNotShrunkAtNarrowViewport(t *testing.T) {
	// 找出所有「body{font-size:...}」形态（忽略注释：先剥行注释）
	var b strings.Builder
	for _, line := range strings.Split(appCSS, "\n") {
		if i := strings.Index(line, "/*"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	code := b.String()
	re := regexp.MustCompile(`body\s*\{[^}]*font-size:\s*([0-9.]+)px`)
	for _, m := range re.FindAllStringSubmatch(code, -1) {
		size := m[1]
		if size != "15" && size != "15.0" {
			t.Errorf("发现 body 字号覆盖 font-size:%spx —— 窄视口（含缩放放大）下不得缩小正文（WCAG 1.4.4）", size)
		}
	}
}

// TestZoomAuditContractRecorded 把「200%/400% 缩放实测结论」固化成判据锚点：
// 断言 CSS 里保留了窄屏适配的**布局**手段（卡片化/单列/换行），而不是字号手段。
// 这样若将来有人为了「塞得下」删掉卡片化改回固定表格，本判据会提醒。
func TestZoomAuditContractRecorded(t *testing.T) {
	need := []struct{ name, needle string }{
		{"窄屏表格卡片化（布局适配）", ".content table tbody{display:block}"},
		{"表格换行策略", "overflow-wrap"},
		{"正文基准字号令牌", "font:15px/1.6 var(--font-body)"},
	}
	for _, c := range need {
		if !strings.Contains(appCSS, c.needle) {
			t.Errorf("缺少「%s」（%q）—— 缩放/窄屏适配只能靠布局，不能靠缩字号", c.name, c.needle)
		}
	}
}
