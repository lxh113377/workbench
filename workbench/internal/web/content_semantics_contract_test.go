package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestTablesHaveAccessibleName 钉住「数据表格必须有可访问名称」。
//
// 一手缺陷（2026-10-23 Awwwards 内容维度验收）：静态提取 12 个 <table>，
// **12/12 全无 caption/aria-label**。读屏器只会念「表格 4 列 3 行」，说不清是什么数据。
// 其中 5 个有邻近 h3/h4 标题（可间接推断），7 个完全没有 —— 对后者语义完全丢失。
// 修：为所有数据表补 aria-label（用「这张表是什么」的中文短语）。
func TestTablesHaveAccessibleName(t *testing.T) {
	srcs := map[string]string{"app.js": appJS, "pool.js": poolJS}
	re := regexp.MustCompile(`<table([^>]*)>`)
	for name, src := range srcs {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			attrs := m[1]
			if strings.Contains(attrs, "aria-label") || strings.Contains(attrs, "aria-labelledby") {
				continue
			}
			// 允许显式标注为纯表现型（无语义数据）的表
			if strings.Contains(attrs, `role="presentation"`) {
				continue
			}
			t.Errorf("%s: <table%s> 缺 aria-label —— 读屏器只能念行列数，说不清是什么数据", name, attrs)
		}
	}
}

// TestDecorativeSpansAreHidden 钉住「纯装饰元素必须 aria-hidden」。
//
// 一手缺陷：4 处纯视觉装饰无 aria-hidden ——
// · `.dot` 状态点 ×3（旁边文字已说「引擎 运行中 / 排队 6」，颜色只是视觉强化）
// · `.stall-icon` ×1（内容就是 `!`，读屏器会把感叹号念出来，而旁边 <b> 已有完整说明）
// 这些元素的语义是**重复**的，读屏器念出来只会增加噪音。
func TestDecorativeSpansAreHidden(t *testing.T) {
	srcs := map[string]string{"app.js": appJS, "pool.js": poolJS, "gov.js": govJS}
	// 纯装饰 class 名单（状态点 / 卡壳图标 / 视觉分隔符）
	decor := []string{`class="dot `, "stall-icon", `class="sep"`}
	for name, src := range srcs {
		for _, d := range decor {
			i := 0
			for {
				idx := strings.Index(src[i:], d)
				if idx < 0 {
					break
				}
				start := i + idx
				end := start + 200
				if end > len(src) {
					end = len(src)
				}
				// 注意：**不能**按 ">" 截断取标签 —— 拼接式 HTML 里常有三元运算符
				// （如 'on' : 'off') + '" aria-hidden="true"），其「> 0」的 > 会提前截断，
				// 导致明明加了 aria-hidden 也判成缺（本轮实测踩到）。
				// 改为在固定窗口内直接查找：装饰元素的开标签必在 class 之后 200 字符内闭合。
				seg := src[start:end]
				if !strings.Contains(seg, "aria-hidden") {
					t.Errorf("%s: 装饰元素 %q 缺 aria-hidden —— 读屏器会念出无意义内容", name, d)
				}
				i = start + len(d)
				if i >= len(src) {
					break
				}
			}
		}
	}
}
