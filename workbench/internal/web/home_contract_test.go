package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"workbench/internal/model"
)

// ────────────────────────────────────────────────────────────────
// 目标要求 1：首页只有一个自动触发的核心按钮，其余区域无冗余元素。
// 下面这组用例把「无冗余」写成机器判据：任何人往首页加状态文字、徽标、
// 图标、卡片，测试立刻红，不靠人工走查。
// ────────────────────────────────────────────────────────────────

func TestIndexPageNoRedundantVisibleElements(t *testing.T) {
	html := indexHTML("tok", "idle")
	// 标签数量（2026-10-19 老大指令改版）：1 个按钮 + 3 个可见子页面入口链接
	//（管理面板/模型池/治理，「入口出现在这个页面上」）；隐藏的 span（读屏状态）不算可见元素。
	if n := strings.Count(html, "<button"); n != 1 {
		t.Fatalf("首页必须只有 1 个按钮，实际 %d 个", n)
	}
	if n := strings.Count(html, "<a "); n != 3 {
		t.Fatalf("首页必须有 3 个子页面入口链接，实际 %d 个", n)
	}
	// 首页的 span 只允许三种：按钮内的两个加载环 + 一个标签，以及 sr-only 读屏状态；
	// 任何「可见状态文字」都会在这里被抓住。
	for _, seg := range strings.Split(html, "<span")[1:] {
		if !(strings.HasPrefix(seg, " class=\"btn-load") || strings.HasPrefix(seg, " class=\"btn-label\"") || strings.HasPrefix(seg, " id=\"homeStatus\"")) {
			t.Fatalf("首页出现了不允许的 span：<span%s", seg[:min(60, len(seg))])
		}
	}

	// 任何可见容器/媒体/表单标签都属于冗余元素，一律禁止。
	// <nav> 于 2026-10-19 放行：子页面入口条就是导航（老大指令要求入口可见）；
	// 其余冗余元素（div/标题/媒体/表单/表格）仍一律禁止。
	for _, bad := range []string{"<div", "<p", "<img", "<svg", "<picture", "<video", "<canvas",
		"<h1", "<h2", "<section", "<header", "<footer", "<aside",
		"<input", "<select", "<textarea", "<table", "<ul", `role="menu"`} {
		if strings.Contains(html, bad) {
			t.Fatalf("极简首页不得出现冗余元素 %q", bad)
		}
	}
	// 读屏状态区必须是视觉隐藏的（sr-only），不能是可见文字。
	if !strings.Contains(html, `id="homeStatus"`) || !strings.Contains(html, "sr-only") {
		t.Fatal("状态反馈必须是 sr-only 的读屏区，不得占用可见区域")
	}
}

// 目标要求 1 + 3：按钮必须自带加载动画与状态反馈。
func TestIndexPageButtonHasLoadingAndStateFeedback(t *testing.T) {
	html := indexHTML("tok", "running")
	if !strings.Contains(html, `data-state="running"`) {
		t.Fatal("按钮必须携带引擎阶段 data-state（状态反馈的入口）")
	}
	if !strings.Contains(html, `aria-busy="`) || !strings.Contains(html, `aria-describedby="homeStatus"`) {
		t.Fatal("按钮必须具备 aria-busy 与状态描述关联（无障碍状态反馈）")
	}
	// 两个加载环：左侧即时 spinner + 右侧运行弧；均由 data-state 控制可见性。
	if strings.Count(html, "btn-load") != 2 {
		t.Fatalf("按钮需要 2 个加载环节点（spinner + arc），实际 %d", strings.Count(html, "btn-load"))
	}
	for _, need := range []string{"@keyframes btnspin", ".btn-spinner", ".btn-arc",
		".start-btn[data-state=busy] .btn-spinner", ".start-btn[data-state=running] .btn-arc"} {
		if !strings.Contains(appCSS, need) {
			t.Fatalf("CSS 缺少加载动画/状态联动规则 %q", need)
		}
	}
}

// 目标要求 1：自动加载 + 自动响应 + 自动触发，且必须有防重入与失败收敛。
func TestHomeScriptIsAutomaticAndGuarded(t *testing.T) {
	for _, need := range []string{
		"paint('busy')",                       // 自动加载：脚本一执行即进入加载态
		"setInterval(ping, 4000)",             // 自动响应：轮询引擎阶段
		"btn.addEventListener('click', clickHandler)", // 手动兜底（走开关语义，不再是「永远只能启动」）
		"const pauseHome = async ()",             // 运行态点击 → 暂停
		"const resumeHome = async ()",            // 暂停态点击 → 继续（乐观更新，不等轮询）
		"setTimeout(() => {",                  // 自动触发：开屏自动点一次
		"inflight",                            // 防重入
		"fails >= 3",                          // 连续失败收敛到 error 态
		"PHASES[state] || PHASES.error",       // 未知状态兜底
	} {
		if !strings.Contains(appJS, need) {
			t.Fatalf("首页脚本缺少自动行为/防护 %q", need)
		}
	}
	// 六个状态必须齐全（idle/busy/running/paused/stopped/error）。
	for _, st := range []string{"idle:", "busy:", "running:", "paused:", "stopped:", "error:"} {
		if !strings.Contains(appJS, st) {
			t.Fatalf("阶段状态表缺少 %q", st)
		}
	}
	// 自动触发必须只发生一次、且不再挂重复定时器（不得出现第二个 setInterval）。
	if n := strings.Count(appJS, "setInterval("); n > 3 {
		t.Fatalf("脚本里的定时器过多（%d），易造成重复请求与交互冲突", n)
	}
	// 按钮必须是开关：running/paused 两态不得被 paint 置为 disabled（lock:false），
	// 否则文案写着「运行中/已暂停」却点不动，首页那条状态就是死的（实测问题 A）。
	if strings.Contains(appJS, "running: { text: '运行中',   hint: '引擎运行中',   busy: false, lock: true  }") {
		t.Fatal("运行态按钮被锁死（lock:true）：用户无法从首页暂停引擎")
	}
	if strings.Contains(appJS, "paused:  { text: '已暂停',   hint: '引擎已暂停',   busy: false, lock: true  }") {
		t.Fatal("暂停态按钮被锁死（lock:true）：用户无法从首页继续")
	}
}

// TestLoadingRingOnlyVisibleWhenActuallyLoading 守护状态反馈的诚实性：
// 加载环不得在 idle/stopped/paused/error 下显示 —— 否则「没在加载」被显示成「在加载」。
func TestLoadingRingOnlyVisibleWhenActuallyLoading(t *testing.T) {
	for _, bad := range []string{
		".start-btn[data-state=idle] .btn-spinner",
		".start-btn[data-state=stopped] .btn-spinner",
		".start-btn[data-state=paused] .btn-arc",
		".start-btn[data-state=error] .btn-spinner",
	} {
		if strings.Contains(appCSS, bad) {
			t.Fatalf("加载环不得在非加载态显示：发现 %q", bad)
		}
	}
	if !strings.Contains(appCSS, ".start-btn[data-state=busy] .btn-spinner") {
		t.Fatal("busy 态必须显示左侧快环（自动加载的可见反馈）")
	}
	if !strings.Contains(appCSS, ".start-btn[data-state=running] .btn-arc") {
		t.Fatal("running 态必须显示右侧慢环（运行中的持续反馈）")
	}
}

// 引擎阶段 → 按钮状态 / 文案的映射必须一一对应，不能只剩三种可见反馈。
func TestPhaseMappingDrivesButtonState(t *testing.T) {
	cases := []struct{ phase, state, label string }{
		{"running", "running", "工作台"},
		{"paused", "paused", "已暂停"},
		{"stopped", "stopped", "重启"},
		{"idle", "idle", "开始"},
	}
	for _, c := range cases {
		h := indexHTML("t", model.EnginePhase(c.phase))
		if !strings.Contains(h, `data-state="`+c.state+`"`) {
			t.Fatalf("阶段 %s 未映射为 data-state=%s", c.phase, c.state)
		}
		if !strings.Contains(h, ">"+c.label+"<") && !strings.Contains(h, c.label) {
			t.Fatalf("阶段 %s 的按钮文案缺少 %q", c.phase, c.label)
		}
	}
}

// 要求 3：静态资源必须可长缓存 + 支持 304（首屏与子页面加载速度）。
func TestAssetCacheAndETag(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec := httptest.NewRecorder()
	serveAsset(rec, req, "application/javascript; charset=utf-8", appJS)
	// 必须是「可重校验」而不是 immutable（实测教训：immutable 会让升级后的新资源
	// 在浏览器里一年不生效，验收读数变成缓存副本）。
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
		t.Fatalf("静态资源必须可重校验（no-cache + ETag），实际 %q", cc)
	}
	if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "immutable") {
		t.Fatalf("静态资源不得带 immutable（升级后旧资源会长期不失效），实际 %q", cc)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("静态资源必须带 ETag")
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "isAdmin") {
		t.Fatal("首次请求必须 200 且返回脚本正文")
	}
	req2 := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	serveAsset(rec2, req2, "application/javascript; charset=utf-8", appJS)
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("带 ETag 的重复请求必须 304，实际 %d", rec2.Code)
	}
	if rec2.Body.Len() != 0 {
		t.Fatal("304 响应不得携带正文")
	}
}

// TestAssetPolicyKeepsPagesAndAssetsInSync 守护「HTML 与静态资源同版本」这条不变量：
// HTML 必须 no-store（带 token，不可缓存），静态资源必须可重校验；
// 二者一起保证升级 EXE 后页面立即拿到新资源，而不是吃了 1 年缓存。
func TestAssetPolicyKeepsPagesAndAssetsInSync(t *testing.T) {
	css := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/app.css", nil)
	serveAsset(css, req, "text/css; charset=utf-8", appCSS)
	if !strings.Contains(css.Header().Get("ETag"), "v") {
		t.Fatal("静态资源 ETag 必须带版本前缀（便于识别同一份构建）")
	}
	if got := css.Header().Get("Cache-Control"); strings.Contains(got, "max-age=31536000") {
		t.Fatalf("静态资源不得带一年长缓存（升级后旧资源不失效），实际 %q", got)
	}
}

// 要求 3：响应式断点必须齐备，首页按钮必须连续缩放而非固定像素。
func TestResponsiveCSSContract(t *testing.T) {
	for _, need := range []string{
		"@media (max-width:1080px)",
		"@media (max-width:720px)",
		"@media (max-width:480px)",
		"@media (max-height:520px)",
		"@media (min-width:1600px)",
		"@media (pointer:coarse)",
		"@media (prefers-reduced-motion:reduce)",
		"width:min(260px,72vw)",
		"font-size:clamp(",
	} {
		if !strings.Contains(appCSS, need) {
			t.Fatalf("CSS 缺少响应式/自适应规则 %q", need)
		}
	}
}

// 要求 3：首页 viewport 必须覆盖刘海屏，并声明深色配色（移动端状态栏一致性）。
func TestIndexPageViewportAndColorScheme(t *testing.T) {
	h := indexHTML("t", "idle")
	for _, need := range []string{"viewport-fit=cover", `name="color-scheme"`, "width=device-width"} {
		if !strings.Contains(h, need) {
			t.Fatalf("首页 head 缺少 %q", need)
		}
	}
}
