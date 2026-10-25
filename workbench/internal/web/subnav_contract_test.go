package web

import (
	"strings"
	"testing"
)

// 子界面导航与状态回溯契约（2026-10-19 目标轮：子界面清单落地后的机器判据）。
//
// 背景：四个界面此前是「首页 → 隐藏后门 → 管理面板 → 模型池」单向可达，
// 治理控制台只能靠手输 URL 进；管理面板的页签换一个就丢状态（刷新/返回都回概览）。
// 2026-10-20 起页签从 10 个收敛到 5 个（总览/任务/产出/模板/设置），跨页入口收进「更多」菜单。
// 本组用例把「每个界面都能被其它界面点到」与「页签状态可回溯（hash 深链）」写成判据，
// 任何人删掉一个入口或把 hash 逻辑退回，测试立刻红。

// TestSubpagesAreMutuallyReachable 守护四界面全连通：
// admin 必须能到 治理/模型池/首页；governance 必须能到 管理面板/模型池/首页；
// modelpool 必须能到 管理面板/首页。
func TestSubpagesAreMutuallyReachable(t *testing.T) {
	admin := adminHTML("tok-1")
	for _, need := range []string{"/modelpool?token=tok-1", "/governance?token=tok-1", "/?token=tok-1"} {
		if !strings.Contains(admin, need) {
			t.Fatalf("管理面板缺少跨页入口 %q（子界面之间必须互相可达）", need)
		}
	}
	gov := governanceHTML("tok-1")
	for _, need := range []string{"/admin?token=tok-1", "/modelpool?token=tok-1", "/?token=tok-1"} {
		if !strings.Contains(gov, need) {
			t.Fatalf("治理控制台缺少跨页入口 %q", need)
		}
	}
	pool := modelPoolPageHTML("tok-1")
	for _, need := range []string{"/admin?token=tok-1", "/?token=tok-1"} {
		if !strings.Contains(pool, need) {
			t.Fatalf("模型池缺少跨页入口 %q", need)
		}
	}
}

// TestPanelScriptSupportsHashDeepLink 守护「状态可回溯」的实现：
// 点页签必须写入 hash；hashchange 必须能把界面收敛回 hash 指向的页签；
// 非法 hash 必须回落 overview（不能出现半个页面激活）。
func TestPanelScriptSupportsHashDeepLink(t *testing.T) {
	for _, need := range []string{
		"location.hash",                        // 读写 hash
		"window.addEventListener('hashchange'", // 返回/前进的收敛入口
		"activateTab(",                         // 唯一的激活函数
		"TAB_NAMES.indexOf(name) < 0",          // 非法页签名回落
		"pendingHash",                          // 自己写 hash 的那次不重复渲染
		"activateTab((location.hash || '').replace(/^#/, ''), { hash: false })", // 首次加载按 hash 收敛
	} {
		if !strings.Contains(appJS, need) {
			t.Fatalf("面板脚本缺少 hash 深链逻辑 %q（页签状态将无法回溯）", need)
		}
	}
	// 页签激活必须收敛到唯一函数：点页签处不得再自己写一遍 class 切换，
	// 否则会出现「pane 亮着但 hash 没写」的浅层跳转（状态不可回溯）。
	// 判据：`tab.classList.add('active')` 只允许出现在 activateTab 内部一次。
	if n := strings.Count(appJS, "tab.classList.add('active')"); n != 1 {
		t.Fatalf("页签 class 切换出现了 %d 处（应只在 activateTab 内 1 处），存在绕过 hash 的第二套切换", n)
	}
	if n := strings.Count(appJS, "tab.addEventListener('click'"); n != 1 {
		t.Fatalf("页签点击绑定出现 %d 处（应为 1 处且走 activateTab）", n)
	}
	// 四个子界面的 data-tab 必须都在 TAB_NAMES 白名单来源里（由 DOM 反查，不是硬编码）。
	if !strings.Contains(appJS, "document.querySelectorAll('.tab')") {
		t.Fatal("TAB_NAMES 白名单必须来自页面上的 .tab 元素，硬编码会与实际页签漂移")
	}
}

// TestAdminTabsStaySimplified 钉住 2026-10-20 的信息架构简化结果。
//
// 老大原话「页面乱七八糟的东西太多了，不够简洁」——管理面板的 10 个页签是主因：
// 概览/队列/产出/模板/自主/评估/日志/治理/工作目录/设置 平铺，一屏 10 个入口没有主次。
// 收敛为 5 个：总览 / 任务 / 产出 / 模板 / 设置；自主与治理合并进总览，
// 评估与工作目录/审计日志合并进设置。旧深链由 LEGACY_TABS 映射，老书签不失效。
// 本用例是「不许再加回去」的机器闸门：页签清单双向钉死。
func TestAdminTabsStaySimplified(t *testing.T) {
	html := adminHTML("tok")
	want := []string{"overview", "queue", "outputs", "templates", "settings"}
	for _, name := range want {
		if !strings.Contains(html, "data-tab=\""+name+"\"") {
			t.Fatalf("管理面板缺少页签 %q（5 个主入口一个都不能少）", name)
		}
	}
	// 用 data-tab= 计数（class="tab 会把 .tabs 与 .tabpane 一并算进来，读数会假高）。
	if n := strings.Count(html, `data-tab="`); n != len(want) {
		t.Fatalf("管理面板页签数量回退：期望 %d 个，实际 %d（页面又变乱了）", len(want), n)
	}
	for _, gone := range []string{"autonomy", "evaluate", "logs", "governance", "workspace"} {
		if strings.Contains(html, "data-tab=\""+gone+"\"") {
			t.Fatalf("已合并的页签 %q 又出现了（信息架构回退，老大要的是简洁）", gone)
		}
	}
	// 老书签不能变成死路：旧页签名必须有落点。
	for _, legacy := range []string{"logs:", "evaluate:", "workspace:", "autonomy:", "governance:"} {
		if !strings.Contains(appJS, legacy) {
			t.Fatalf("旧深链映射缺少 %q（老的 #logs / #workspace 书签会掉回总览）", legacy)
		}
	}
}

// TestTopbarHasSingleMoreMenu 钉住顶栏「六个按钮 + 三个跨页链接」的收敛结果：
// 主操作用直接可见（暂停/继续/停止），跨页入口收进「更多」，不再横向霸占半条顶栏。
func TestTopbarHasSingleMoreMenu(t *testing.T) {
	html := adminHTML("tok")
	for _, need := range []string{`id="btnMore"`, `id="topMore"`, "更多"} {
		if !strings.Contains(html, need) {
			t.Fatalf("顶栏缺少「更多」菜单要素 %q", need)
		}
	}
	if n := strings.Count(html, `class="btn ghost"`); n > 4 {
		t.Fatalf("顶栏幽灵按钮过多（%d 个），应把跨页入口收进「更多」", n)
	}
}

// TestTemplatesLibraryIsSearchableAndFolded 守护「模板库不再一次铺 24 行」。
//
// 老大反馈「页面乱七八糟」时，模板库是最长的一页（1902px，24 行全平铺、没有搜索）。
// 判据：① 必须有搜索框（tplSearch）；② 未搜索时默认只渲染前 8 条 + 「全部展开」入口；
// ③ 搜索必须真的参与过滤（不能只是摆个输入框）。
func TestTemplatesLibraryIsSearchableAndFolded(t *testing.T) {
	for _, need := range []string{
		"tplSearch", "tplShowAll", "hit.slice(0, 8)", "tplQuery.trim().toLowerCase()",
		"tplMore()", "indexOf(q) >= 0",
	} {
		if !strings.Contains(appJS, need) {
			t.Fatalf("模板库缺少「可搜索 + 默认折叠」要素 %q（页面又会变成 24 行平铺）", need)
		}
	}
}

// TestFormLabelsAvoidJargon 守护「非技术用户看得懂」这条用户诉求。
// 老大不是技术人员（behavior_core #25③）：模型池/设置页里的 Tier、BaseURL、线协议、
// DSH profile 这类行话必须配白话说明，不能只给英文术语。
func TestFormLabelsAvoidJargon(t *testing.T) {
	labels := map[string]string{
		"模型池": strings.Join([]string{poolJS}, ""),
		"设置页": appJS,
	}
	for page, src := range labels {
		for _, bad := range []string{">Tier<", "线协议<select", "DSH profile</td>"} {
			if strings.Contains(src, bad) {
				t.Errorf("%s 仍有未解释的行话 %q（老大不是技术人员）", page, bad)
			}
		}
	}
	for _, want := range []string{"接口类型", "用途偏好", "线路来源", "OpenAI 标准对话"} {
		if !strings.Contains(poolJS, want) {
			t.Fatalf("模型池表单缺少白话标签 %q", want)
		}
	}
	if !strings.Contains(appJS, "运行配置") {
		t.Fatal("设置页「DSH profile」未改成白话「运行配置」")
	}
}

// TestTabsCannotBeSquashedByFlex 守护 2026-10-20 实测缺陷：页签条被压成 0 高度。
//
// 老大截图：「管理面板」标题下那条页签只剩半截、像被切掉。实测读数 tabs.h = 0，
// 原因是 body.admin 是 flex column，而 .tabs 带 overflow-x:auto —— 按 flexbox 规范，
// overflow 非 visible 的 flex item 自动最小尺寸为 0，于是被内容挤扁，46px 的页签溢出叠到卡片上。
// 判据：.tabs 必须显式声明 flex:0 0 auto 且 min-height ≥ 44px（覆盖 auto 最小值）。
func TestTabsCannotBeSquashedByFlex(t *testing.T) {
	// 取 .tabs 规则块（到下一个 } 为止），只在该块内断言，避免匹配到别处的同名属性。
	i := strings.Index(appCSS, ".tabs{")
	if i < 0 {
		t.Fatal("找不到 .tabs 规则（选择器被改名 ⇒ 判据必须红，不得静默通过）")
	}
	rest := appCSS[i:]
	end := strings.Index(rest, "}")
	if end < 0 {
		t.Fatal(".tabs 规则未闭合")
	}
	rule := rest[:end]
	for _, need := range []string{"flex:0 0 auto", "min-height:46px"} {
		if !strings.Contains(rule, need) {
			t.Fatalf(".tabs 缺少 %q —— flex column 会把它压成 0 高度（页签只剩半截）", need)
		}
	}
	// 反证：把修复项从规则里剥掉，判据必须能发现。
	broken := strings.ReplaceAll(strings.ReplaceAll(rule, "flex:0 0 auto", ""), "min-height:46px", "")
	for _, need := range []string{"flex:0 0 auto", "min-height:46px"} {
		if strings.Contains(broken, need) {
			t.Fatalf("反证失败：剥掉 %q 后仍能匹配（判据恒真）", need)
		}
	}
}

// TestQueuePageHasActionWhenEmpty 守护任务页「空态不摆两张空卡」。
//
// 老大二轮截图：任务页三张卡里两张只写着「没有」，真正能做的「加任务」还藏在折叠里，
// 一屏看下来像坏了。判据：全空时表单必须直接展开（不折叠），且不再渲染两张空卡。
func TestQueuePageHasActionWhenEmpty(t *testing.T) {
	for _, need := range []string{"const nothing = q.length === 0 && active.length === 0", "if (nothing) {"} {
		if !strings.Contains(appJS, need) {
			t.Fatalf("任务页缺少「全空分支」%q（又会出现两张空卡叠在一起）", need)
		}
	}
	// 空态分支里表单必须是直接拼接（不是 fold 包起来）——「能做的事」不该再点一次才看到。
	i := strings.Index(appJS, "if (nothing) {")
	if i < 0 {
		t.Fatal("找不到 nothing 分支")
	}
	seg := appJS[i:]
	if e := strings.Index(seg, "} else {"); e > 0 {
		seg = seg[:e]
	}
	if strings.Contains(seg, "fold('") {
		t.Fatal("空态里的加任务表单被折叠了（老大要的是「此刻正好能做这件事」）")
	}
	if !strings.Contains(seg, "addBody") {
		t.Fatal("空态分支没有渲染加任务表单")
	}
}
