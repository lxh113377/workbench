package web

import (
	"strings"
	"testing"
)

// stripLineComments 剥掉行注释（含整行与行尾），但**不**把字符串字面量里的
// 「//」当注释（如 'http://'）。判据要区分「注释里提到 confirm()」与「真的调用 confirm()」，
// 否则要么误报（本仓注释大量出现「取代原生 confirm()」字样），要么漏报。
func stripLineComments(src string) string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		inSingle, inDouble := false, false
		cut := -1
		for i := 0; i < len(line); i++ {
			c := line[i]
			switch {
			case inSingle:
				if c == '\'' {
					inSingle = false
				}
			case inDouble:
				if c == '"' {
					inDouble = false
				}
			case c == '\'':
				inSingle = true
			case c == '"':
				inDouble = true
			case c == '/' && i+1 < len(line) && line[i+1] == '/':
				cut = i
				i = len(line)
			}
			if cut >= 0 {
				break
			}
		}
		if cut >= 0 {
			line = line[:cut]
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// TestStripLineCommentsIsSound 自检判据本身：既不能误报，也不能漏报。
// （判据若恒真或恒假，比没有判据更危险 —— 它会给出假绿。）
func TestStripLineCommentsIsSound(t *testing.T) {
	if strings.Contains(stripLineComments("  // 取代原生 confirm()\n"), "confirm(") {
		t.Error("注释里的 confirm( 没被剥掉 ⇒ 主判据会对注释误报")
	}
	if !strings.Contains(stripLineComments("  var u = 'http://x/confirm('\n"), "confirm(") {
		t.Error("字符串里的 // 被当成注释 ⇒ 主判据会漏报真调用")
	}
	if !strings.Contains(stripLineComments("  if (!confirm('x')) { return; } // 旧的\n"), "confirm(") {
		t.Error("真调用被剥掉了 ⇒ 主判据形同虚设")
	}
	if strings.Contains(stripLineComments("  run(); // 收尾 alert('x')\n"), "alert(") {
		t.Error("行尾注释里的 alert( 没被剥掉")
	}
}

// TestNoNativeBlockingDialogs 钉住「三个脚本包都不得使用原生阻塞弹窗」。
//
// 一手理由（2026-10-22 走查）：项目偏好是「禁弹窗、就地展开」，而 confirm()/alert()
// 是系统灰框 —— 阻塞主线程、不可样式化、在暗色 HUD 界面里脱离设计体系。
// 破坏性操作改走两段式就地确认（armConfirm），错误提示改走 toast。
func TestNoNativeBlockingDialogs(t *testing.T) {
	pkgs := map[string]string{"app.js": appJS, "pool.js": poolJS, "gov.js": govJS}
	for name, src := range pkgs {
		code := stripLineComments(src)
		for _, bad := range []string{"confirm(", "alert(", "prompt("} {
			if strings.Contains(code, bad) {
				t.Errorf("%s 仍在使用原生阻塞弹窗 %q（改 armConfirm / toast）", name, bad)
			}
		}
	}
}

// TestArmedConfirmWiredEverywhere 钉住「每个脚本包自带 armConfirm 且真的被调用」：
// 三页是独立脚本包，helper 不跨页可见（round 14 的 toast 回归一手教训），
// 且只定义不调用等于没改 —— 断言取**调用点**而非标识符。
func TestArmedConfirmWiredEverywhere(t *testing.T) {
	for name, src := range map[string]string{"app.js": appJS, "pool.js": poolJS} {
		if !strings.Contains(src, "const armConfirm =") {
			t.Errorf("%s 缺少本包自带的 armConfirm 定义", name)
		}
		if !strings.Contains(src, "armConfirm(") {
			t.Errorf("%s 定义了 armConfirm 却没有调用点", name)
		}
		if !strings.Contains(src, "disarmArm") {
			t.Errorf("%s 缺少回退通道（超时/Escape/点别处都必须能否定待确认态）", name)
		}
	}
}
