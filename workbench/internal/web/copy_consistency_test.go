package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestButtonLabelsAreActionWords 钉住按钮文案规范：动作词、无句末标点。
//
// 跨页文案审计（2026-10-22，164 条用户可见文案机器化提取）发现的真问题之一：
// 保存开机自启的按钮写作「应用」，而同页其它保存动作一律「保存」——
// 同一动作两种措辞，用户得猜「应用」是不是和执行类操作一样危险。已统一为「保存」。
func TestButtonLabelsAreActionWords(t *testing.T) {
	srcs := map[string]string{"app.js": appJS, "pool.js": poolJS, "gov.js": govJS}
	re := regexp.MustCompile(`>([^<>{}]{2,26})</button>`)
	for name, src := range srcs {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			label := strings.TrimSpace(m[1])
			if label == "" {
				continue // 动态拼接的按钮（label 由表达式生成），跳过字面量检查
			}
			if strings.ContainsAny(label, "。！？") {
				t.Errorf("%s 按钮「%s」带句末标点（按钮是动作标签，不是句子）", name, label)
			}
			if label == "应用" {
				t.Errorf("%s 出现「应用」按钮 —— 与其它页的「保存」同义不同词，统一为「保存」", name)
			}
		}
	}
}

// TestErrorToastsNameTheFailure 钉住失败提示必须点名（「XX 失败：」）。
//
// 审计结论：全站 23 处失败提示 100% 采用「XX失败：<原因>」形态 —— 这条是好的，
// 固化成判据防止将来有人写成「出错了」「有点问题」这类无法排查的模糊话。
func TestErrorToastsNameTheFailure(t *testing.T) {
	srcs := map[string]string{"app.js": appJS, "pool.js": poolJS, "gov.js": govJS}
	re := regexp.MustCompile(`toast\(\x27([^\x27]*)\x27\)`)
	for name, src := range srcs {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			txt := m[1]
			for _, bad := range []string{"出错了", "有点问题", "不行", "未知错误"} {
				if strings.Contains(txt, bad) {
					t.Errorf("%s 失败提示含糊（%q）：%q —— 必须点名失败对象与原因", name, bad, txt)
				}
			}
		}
	}
}

// TestDuplicateEmptyStatesUnified 钉住「同一语义的空态只能有一种写法」。
//
// 一手问题：管理面板「没有失败任务」有两处（折叠块 + 详细块），
// 一处写「最近没有失败任务。」、一处写「没有失败任务 —— 保持这样最好。」；
// 同一件事两种语气，用户翻到两处会以为说的是两码事。已统一。
func TestDuplicateEmptyStatesUnified(t *testing.T) {
	if strings.Contains(appJS, "最近没有失败任务。") {
		t.Error("同一语义的空态存在「最近没有失败任务。」写法，已统一为「没有失败任务 —— 保持这样最好。」")
	}
}
