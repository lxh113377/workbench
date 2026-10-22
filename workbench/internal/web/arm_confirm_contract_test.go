package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestArmConfirmCallSitesPassHostElement 钉住「每个 armConfirm 调用点都必须真的传进宿主元素」。
//
// 一手回归（2026-10-22 本轮自查抓到）：模型池「⋯」菜单里的删除项调用
// `onRowAction(act, id, d)` 时漏了第 4 个参数 btn，而 armConfirm 当时写的是
// `if (!btn) { run(); return; }` —— 于是那条路径**静默跳过确认直接删除**，
// 比改造前（原生 confirm 挡着）更危险。破坏性操作的确认链一旦断在半路，
// 界面看起来完全正常，只有数据没了。
//
// 判据两条：① armConfirm 必须 fail-closed（拿不到宿主就中止，绝不 run）；
// ② onRowAction 的两个调用点（行内按钮 / ⋯ 菜单项）都必须传元素。
func TestArmConfirmCallSitesPassHostElement(t *testing.T) {
	if !strings.Contains(poolJS, "const armConfirm =") {
		t.Fatal("pool.js 缺少 armConfirm 定义")
	}
	// ① fail-closed：!btn 分支里不得出现 run()
	helper := poolJS[strings.Index(poolJS, "const armConfirm ="):]
	helper = helper[:strings.Index(helper, "\n  };")+3]
	if strings.Contains(helper, "if (!btn) { run()") || strings.Contains(helper, "if (!btn) {run()") {
		t.Errorf("armConfirm 未 fail-closed（!btn 时直接 run = 静默跳过确认）：\n%s", helper)
	}
	if !strings.Contains(helper, "if (!btn)") {
		t.Error("armConfirm 缺少 !btn 保护分支")
	}
	// ② 调用点必须传元素：行内按钮传 btn，菜单项传 a
	if !strings.Contains(poolJS, "onRowAction(btn.getAttribute('data-act'), btn.getAttribute('data-id'), d, btn)") {
		t.Error("行内按钮调用点未传宿主按钮（第 4 参）")
	}
	if !strings.Contains(poolJS, "onRowAction(act, id, d, a)") {
		t.Error("⋯ 菜单调用点未传菜单项元素 —— 该路径会因 armConfirm fail-closed 而永远删不掉")
	}
	// ③ 删除项不得提前收起弹层（armed 态必须可见、可再点）
	menu := poolJS[strings.Index(poolJS, "a[data-more]"):]
	menu = menu[:strings.Index(menu, "});")+3]
	if !strings.Contains(menu, "if (act !== 'del') { pop.hidden = true; }") {
		t.Errorf("⋯ 菜单未对删除项保持展开（armed 态无处可点）：\n%s", menu)
	}
}

// TestAppArmConfirmRequiresHostElement 同型判据（主面板）：三页各自独立，
// 每页的 armConfirm 都要 fail-closed。
func TestAppArmConfirmRequiresHostElement(t *testing.T) {
	start := strings.Index(appJS, "const armConfirm =")
	if start < 0 {
		t.Fatal("app.js 缺少 armConfirm 定义")
	}
	end := strings.Index(appJS[start:], "\n  };")
	helper := appJS[start : start+end+5]
	if strings.Contains(helper, "if (!btn) { run()") {
		t.Errorf("app.js armConfirm 未 fail-closed：\n%s", helper)
	}
	if !strings.Contains(helper, "if (!btn) { return; }") {
		t.Errorf("app.js armConfirm 缺少 fail-closed 分支：\n%s", helper)
	}
}

// TestTemplateButtonsPassThis 钉住内联 onclick 必须把按钮自身传进确认型动作。
// 少了 `,this`，armConfirm 拿不到宿主 → 操作被 fail-closed 中止（功能静默失效）。
func TestTemplateButtonsPassThis(t *testing.T) {
	re := regexp.MustCompile(`onclick="WB\.(deleteTemplate|resetTemplates|wsReset)\([^"]*"`)
	for _, m := range re.FindAllString(appJS, -1) {
		if !strings.HasSuffix(m, ",this)") && !strings.Contains(m, "this") {
			t.Errorf("确认型动作未传 this（按钮自身）：%s", m)
		}
	}
	if re.FindAllString(appJS, -1) == nil {
		t.Fatal("一个确认型动作调用点都没找到 —— 判据失去受理面")
	}
}
