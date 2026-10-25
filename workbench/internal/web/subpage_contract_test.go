package web

import (
	"strings"
	"testing"
)

// 子页面契约：与首页同一套视觉/交互基线（目标要求 2 + 3）。

// TestPoolPageTokenInjectionWithCssPercent 守护 2026-10-19 的真实事故：
// 占位符替换把 CSS 里的 `width: 100%` 当成了 %s，token 被塞进样式表，
// 结果页面上的 __WB_TOKEN__ 为空、所有 /api/pool 请求 401。
func TestPoolPageTokenInjectionWithCssPercent(t *testing.T) {
	page := modelPoolPageHTML("tok-abc")
	if strings.Contains(page, "100;tok-abc") || strings.Contains(page, "100%tok-abc") {
		t.Fatal("token 被注入进 CSS 的百分比里（占位符替换误命中 100%）")
	}
	if strings.Count(page, "tok-abc") != 3 {
		t.Fatalf("token 必须恰好出现 3 次（两个链接 + 注入脚本），实际 %d", strings.Count(page, "tok-abc"))
	}
	if !strings.Contains(page, `window.__WB_TOKEN__ = "tok-abc"`) {
		t.Fatal("token 未注入到 __WB_TOKEN__（前端拿不到令牌会全页 401）")
	}
	if !strings.Contains(page, "/admin?token=tok-abc") || !strings.Contains(page, "/?token=tok-abc") {
		t.Fatal("子页面必须保留「管理面板」「回首页」两个可达链接")
	}
	// 未替换掉的占位符残留 = 模板与参数数量不匹配，必须暴露。
	if strings.Count(page, "%s") != 0 {
		t.Fatalf("页面残留 %d 个未替换占位符", strings.Count(page, "%s"))
	}
}

// TestAdminAndGovernanceTokenInjection 三大子页面用同一套注入约定，防止只修一处。
func TestAdminAndGovernanceTokenInjection(t *testing.T) {
	for name, page := range map[string]string{"admin": adminHTML("tok-xyz"), "governance": governanceHTML("tok-xyz")} {
		// 2026-10-19 起两个子页面各多一个跨页入口（admin↔治理 / governance↔模型池），
		// token 注入次数随之由 3 变 4：2 个既有链接 + 1 个新增链接 + 1 段注入脚本。
		if strings.Count(page, "tok-xyz") != 4 {
			t.Fatalf("%s 页 token 注入次数应为 4（两个链接 + 一段注入脚本），实际 %d", name, strings.Count(page, "tok-xyz"))
		}
		if !strings.Contains(page, `window.__WB_TOKEN__ = "tok-xyz"`) {
			t.Fatalf("%s 页缺少 __WB_TOKEN__ 注入", name)
		}
		if strings.Count(page, "%s") != 0 {
			t.Fatalf("%s 页残留未替换占位符 %d 个", name, strings.Count(page, "%s"))
		}
	}
}

// TestIndexTemplatePlaceholderCountMatchesArgs 守护首页模板与 Sprintf 参数数量一致。
func TestIndexTemplatePlaceholderCountMatchesArgs(t *testing.T) {
	n := strings.Count(indexPage, "%s")
	// 2026-10-19 老大指令改版：入口链接从 1 个隐藏后门变为 3 个可见入口（admin/modelpool/
	// governance 各注入一次 token），占位符 6 → 8（state/hint/label/hint/token×3/script）。
	if n != 8 {
		t.Fatalf("首页模板应有 8 个占位符（state/hint/label/hint/token×3/script），实际 %d", n)
	}
	html := indexHTML("tok-1", "idle")
	if strings.Count(html, "%s") != 0 {
		t.Fatalf("indexHTML 输出残留占位符 %d 个（fmt 错配标记会泄露到页面）", strings.Count(html, "%s"))
	}
	if strings.Contains(html, "%!") {
		t.Fatal("首页出现 fmt 参数错配标记（页面会显示乱码）")
	}
}

// TestPagesDeclareFavicon 每个页面都要声明图标：浏览器默认会请求 /favicon.ico，
// 缺失会在 DevTools 留下 404 噪声，把「加载是否干净」的读数污染成假红。
func TestPagesDeclareFavicon(t *testing.T) {
	pages := map[string]string{"index": indexHTML("t", "idle"), "admin": adminHTML("t"), "governance": governanceHTML("t"), "pool": modelPoolPageHTML("t")}
	for name, page := range pages {
		if !strings.Contains(page, `rel="icon"`) || !strings.Contains(page, "/assets/favicon.svg") {
			t.Fatalf("%s 页未声明 favicon（会留下 404 请求）", name)
		}
	}
	if !strings.Contains(faviconSVG, "<svg") {
		t.Fatal("内嵌 favicon 必须是 SVG")
	}
}

// TestSubpagesShareUnifiedShell 三个子页面共用同一套顶栏/按钮/返回首页基线。
func TestSubpagesShareUnifiedShell(t *testing.T) {
	pages := map[string]string{"admin": adminHTML("t"), "governance": governanceHTML("t"), "pool": modelPoolPageHTML("t")}
	for name, page := range pages {
		for _, need := range []string{`class="topbar"`, `class="brand"`, `class="btn ghost"`, "回首页", `lang="zh-CN"`, `/assets/app.css`, "viewport"} {
			if !strings.Contains(page, need) {
				t.Fatalf("%s 页缺少统一外壳要素 %q", name, need)
			}
		}
		if strings.Contains(page, "%!") {
			t.Fatalf("%s 页出现 fmt 错配标记", name)
		}
	}
}

// TestNarrowScreenTablesCannotOverflowPage 守护 2026-10-19 实测到的窄屏缺陷：
// 390px 下「日志」标签的审计表把页面撑出横向滚动（scrollWidth 381 > clientWidth 390 边界）。
// 判据：窄屏表格必须固定列宽 + 任意位置断行，而不是靠列内容撑开。
func TestNarrowScreenTablesCannotOverflowPage(t *testing.T) {
	for _, need := range []string{"table-layout:fixed", "overflow-wrap:anywhere", "word-break:break-word"} {
		if !strings.Contains(appCSS, need) {
			t.Fatalf("窄屏表格缺少防溢出规则 %q（长文本会把页面撑出横向滚动）", need)
		}
	}
	// 首页与大屏不应受固定列宽影响：该规则必须包在窄屏媒体查询里。
	idx := strings.Index(appCSS, "table-layout:fixed")
	if idx < 0 {
		t.Fatal("缺少 table-layout:fixed")
	}
	before := appCSS[:idx]
	if !strings.Contains(before, "@media (max-width:720px)") {
		t.Fatal("table-layout:fixed 必须写在窄屏媒体查询内（否则会改变桌面宽度分配）")
	}
}

// TestSubpageResponsiveRules 子页面必须带窄屏重排规则（表格卡片化 / 容器横滚）。
func TestSubpageResponsiveRules(t *testing.T) {
	page := modelPoolPageHTML("t")
	for _, need := range []string{"@media (max-width:720px)", "table.pool-table thead { display: none; }", "data-label", ".pool-table-wrap"} {
		if !strings.Contains(page, need) && need != "data-label" {
			t.Fatalf("模型池页缺少窄屏规则 %q", need)
		}
	}
	if !strings.Contains(poolJS, `data-label=`) {
		t.Fatal("模型池行渲染必须带 data-label（窄屏卡片模式依赖它显示字段名）")
	}
}
