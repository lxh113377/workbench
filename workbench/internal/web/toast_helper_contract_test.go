package web

import (
	"strings"
	"testing"
)

// TestGovScriptDefinesItsOwnToast 钉住「治理台脚本必须自带 toast 定义」。
//
// 一手回归（2026-10-22）：把采样失败的 alert 换成 toast 时漏了 gov.js 的 helper ——
// 三页（app.js / pool.js / gov.js）是**各自独立**的脚本包，某一页的定义对另一页不可见。
// 前端门禁 interact-governance 当场判红 `reject: toast is not a function`（真实运行时错误），
// 本判据把它固化为构建期判据：一旦 govJS 里出现 toast( 调用，就必须能找到 const toast =。
func TestGovScriptDefinesItsOwnToast(t *testing.T) {
	if !strings.Contains(govJS, "toast(") {
		t.Skip("治理台当前不使用 toast（若将来启用，本判据自动生效）")
	}
	if !strings.Contains(govJS, "const toast =") {
		t.Fatal("gov.js 调用了 toast() 却没有定义它 —— 运行时抛 'toast is not a function'（三页各自独立，必须各留一份）")
	}
	// 反向：其它两页也各自必须有定义（同样的跨页可见性陷阱）
	for name, src := range map[string]string{"app.js": appJS, "pool.js": poolJS} {
		if strings.Contains(src, "toast(") && !strings.Contains(src, "const toast =") {
			t.Errorf("%s 调用 toast() 但未定义", name)
		}
	}
}
