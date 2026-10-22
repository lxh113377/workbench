package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestControlsHaveAccessibleName 钉住「每个表单控件都有可访问名称」。
//
// 一手缺陷（2026-10-23 Awwwards 四维验收，WCAG 1.3.1/4.1.2）：
// 三处 `<label>领域</label><select id=...>` 与一处无 label 的 strategySel ——
// 视觉上标签就在旁边，但 label 未用 for/id 关联也未包裹控件，屏幕阅读器读不到控件名。
// 实测修前 4 处缺名（浏览器 unlabeled 判据不报是因为它只查 aria-label/title，不查 label 关联）。
// 修：三处补 for/id，strategySel 补 aria-label。
func TestControlsHaveAccessibleName(t *testing.T) {
	srcs := map[string]string{"app.js": appJS, "pool.js": poolJS}
	// ① 裸 <label>文字</label><select id=X> 这种「相邻未关联」形态一律判红
	badForm := regexp.MustCompile(`<label>([^<]+)</label><select id="([A-Za-z0-9_]+)"`)
	for name, src := range srcs {
		for _, m := range badForm.FindAllStringSubmatch(src, -1) {
			t.Errorf("%s: <label>%s</label> 未与 <select id=%s> 关联（缺 for）—— 屏幕阅读器读不到控件名", name, m[1], m[2])
		}
	}
	// ② 每个 <select id=X> 必须满足之一：有 aria-label / 被 <label> 包裹 / 有 for=X 的 label
	selRe := regexp.MustCompile(`<select[^>]*id="([A-Za-z0-9_]+)"[^>]*>`)
	for name, src := range srcs {
		for _, m := range selRe.FindAllStringSubmatch(src, -1) {
			id := m[1]
			tag := m[0]
			if strings.Contains(tag, "aria-label") {
				continue
			}
			if strings.Contains(src, "for=\""+id+"\"") {
				continue
			}
			// 被 label 包裹：找该 select 之前最近的 <label> 是否未闭合
			i := strings.Index(src, tag)
			if i > 0 {
				before := src[:i]
				lastOpen := strings.LastIndex(before, "<label")
				lastClose := strings.LastIndex(before, "</label>")
				if lastOpen > lastClose {
					continue // 被包裹
				}
			}
			t.Errorf("%s: <select id=%s> 缺可访问名称（须 aria-label / 被 label 包裹 / 有 for 关联）", name, id)
		}
	}
}
