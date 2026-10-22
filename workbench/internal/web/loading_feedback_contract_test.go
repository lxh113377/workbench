package web

import (
	"strings"
	"testing"
)

// TestSlowBackendHasLoadingAffordance 钉住「慢后端下界面必须表明正在读，且失败不得留在读取态」。
//
// 一手实测（2026-10-22，CDP 限速 1.2s）：切到模板页首帧 `pane.innerHTML` 长度 = 0 ——
// 整片空白，用户无从判断「正在读」还是「坏了」；治理台「立即采样」按钮在 +0/+700/+3700ms
// 三帧读数完全一致（文案与 disabled 全程不变）。判据卷把这条列为跨篇必测：
// 「慢网 / 资源失败态（Loading 兜底不得永驻）」—— 后半句要求失败时**替换**占位。
//
// 断言取**调用点**而非标识符（前轮 M-C 教训：只查标识符会让「摘掉调用那一步」静默通过）。
func TestSlowBackendHasLoadingAffordance(t *testing.T) {
	jsChecks := []struct{ name, needle string }{
		{"加载占位 helper 定义", "const loadingPane ="},
		{"失败态 helper 定义", "const failPane ="},
		{"切页签时调用占位（调用点，非仅定义）", "loadingPane(name);"},
		{"取数失败时替换占位（调用点）", "failPane(name,"},
		{"占位骨架样式", "skeleton"},
	}
	for _, c := range jsChecks {
		if !strings.Contains(appJS, c.needle) {
			t.Errorf("主面板缺少「%s」：%q", c.name, c.needle)
		}
	}
	if !strings.Contains(appCSS, ".skeleton{") || !strings.Contains(appCSS, "@keyframes skeletonSweep") {
		t.Error("CSS 缺少 .skeleton / skeletonSweep（骨架不可见等于没有占位）")
	}
	// 采样按钮 busy：断言赋值语句本身
	for _, needle := range []string{"govSampleBtn.disabled = true", "govSampleBtn.textContent = '采样中…'", "govSampleBtn.disabled = false"} {
		if !strings.Contains(govJS, needle) {
			t.Errorf("治理台采样按钮缺少 busy 反馈：%q", needle)
		}
	}
}

// TestNoNativeModalDialogs 钉住「前端不得使用原生 alert()」：
// 原生弹窗阻塞界面、且不属于本设计体系（暗色 HUD 里蹦出系统灰框）。
// 面板反馈一律走 toast / 就地展开。
//
// 注：`confirm()` 属**破坏性操作确认**，暂不在本判据范围（另立待办）——
// 本判据只锁「错误提示不得弹原生框」这条已修完的面。
func TestNoNativeModalDialogs(t *testing.T) {
	for name, src := range map[string]string{"治理台": govJS, "模型池": poolJS, "主面板": appJS} {
		if strings.Contains(src, "alert(") {
			t.Errorf("%s 仍使用原生 alert()（应改为 toast）", name)
		}
	}
}
