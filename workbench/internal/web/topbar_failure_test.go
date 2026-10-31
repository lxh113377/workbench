package web

import (
	"strings"
	"testing"
)

// silentCatchPattern 是"把失败静默吞掉"的字面形态。
const silentCatchPattern = "catch (e) { /* ignore */ }"

// catchClause 按大括号配对抽出 code 里第一个 catch 子句（含花括号内全部内容）。
// 抽不到就判红 —— 本会话登记过的"判据空转"形态：锚点失配时静默返回空串会让测试一直判绿。
func catchClause(t *testing.T, code string) string {
	t.Helper()
	i := strings.Index(code, "catch (e) {")
	if i < 0 {
		t.Fatal("代码里没有 catch 子句 ⇒ 结构已变（或抽取退化），本判据必须同步改写，不许静默放行")
	}
	rest := code[i:]
	o := strings.Index(rest, "{")
	depth := 0
	for k := o; k < len(rest); k++ {
		switch rest[k] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[:k+1]
			}
		}
	}
	t.Fatal("catch 子句大括号不配对（JS 被改坏）")
	return ""
}

// TestTopBarFailureIsVisible 钉住 T110（round 156 同源走查复算、round 158 落地）：
// 「顶栏刷新自身失败时，必须让用户看见，且不能把陈旧值冒充当前值」。
//
// 一手取证：refreshTop 原先是 try{...} catch (e) { /* ignore */ } —— /api/status 持续失败
// （引擎重启、token 换了、端口变了）时顶栏**冻结在最后一次的数值上**且零标记。
// 讽刺处：round 154 我在同一个函数里补了两半（finally 的流状态接管 + 降级 toast），
// 立的规就是「陈旧不能冒充正常」，却漏掉了请求失败这条路径 —— 同一函数三条失败路径两明一暗。
// 同文件 ping 早有 fails>=3 收敛，说明"失败要计数并可见"在本文件是既有惯例，refreshTop 是唯一例外。
//
// 判据形态说明（本轮刚登记的教训用在自己身上）：
//
//	① 必须先 stripLineComments 再判 —— 我在这段代码的**注释里**原样引用了
//	  "catch (e) { /* ignore */ }" 来说明修了什么；若按整份文本 Contains，注释会把修复
//	  判成"缺陷仍在"（同族：round 154 我的注释里的 setInterval( 字样被计数器当成真新增）。
//	  下面用反向对照把这件事钉成断言，而不是只写在注释里。
//	② 复用 no_dialog_contract_test.go 的 stripLineComments（它自带 TestStripLineCommentsIsSound
//	  四项自检），不在这里再写一个更难保证正确的版本。
func TestTopBarFailureIsVisible(t *testing.T) {
	body := functionBody(t, "async function refreshTop()")
	code := stripLineComments(body)
	if len(code) < 200 {
		t.Fatalf("剥注释后只剩 %d 字节 ⇒ 抽取退化，后续断言全是空气", len(code))
	}
	t.Run("静默catch已消失", func(t *testing.T) {
		if strings.Contains(code, silentCatchPattern) {
			t.Error("refreshTop 仍有静默 catch ⇒ /api/status 持续失败时顶栏冻结在最后成功值且零标记")
		}
	})
	t.Run("失败被计数且成功时归零", func(t *testing.T) {
		if !strings.Contains(code, "topFailStreak++") {
			t.Error("缺失败计数 ⇒ 无法区分瞬时抖动与持续失败")
		}
		ri := strings.Index(code, "topFailStreak = 0")
		ci := strings.Index(code, "catch (e) {")
		if ri < 0 {
			t.Error("成功路径未归零 ⇒ 恢复一次后仍永久报警")
		} else if ri > ci {
			t.Error("topFailStreak = 0 落在 catch 之后 ⇒ 不是成功路径归零，报警会粘滞")
		}
	})
	t.Run("阈值防单次误报", func(t *testing.T) {
		c := catchClause(t, code)
		if !strings.Contains(c, "topFailStreak === 2") && !strings.Contains(c, ">= 2") {
			t.Error("catch 内无连续失败阈值 ⇒ 单次网络抖动就报警，制造噪音（同函数流状态那半也是连续 3 周期才接管）")
		}
	})
	t.Run("降级对用户可见", func(t *testing.T) {
		c := catchClause(t, code)
		if !strings.Contains(c, "toast(") {
			t.Error("catch 里没有 toast ⇒ 静默降级，用户看不到")
		}
		if !strings.Contains(c, "textContent") {
			t.Error("catch 里没有改写顶栏文本 ⇒ 陈旧数值继续冒充当前值（本判据要挡的核心形态）")
		}
	})
	t.Run("反向对照：注释里的旧代码文本不得判成缺陷", func(t *testing.T) {
		// 把"缺陷字面量"只放进注释，判据必须仍然通过 —— 证明它判的是代码，不是文本。
		sample := "  // 修前形态是 " + silentCatchPattern + " —— 说明用\n" + code
		if !strings.Contains(sample, silentCatchPattern) {
			t.Fatal("对照样本没带上注释里的字面量 ⇒ 这条反向对照本身失效（空对照）")
		}
		if strings.Contains(stripLineComments(sample), silentCatchPattern) {
			t.Error("剥注释后仍能搜到该字面量 ⇒ stripLineComments 没生效或被绕过，判据会被注释骗（本轮实测过的形态）")
		}
		if strings.Contains(sample, silentCatchPattern) && !strings.Contains(code, silentCatchPattern) {
			t.Log("对照成立：整份文本能搜到、剥注释后搜不到 ⇒ 本判据免疫注释文本")
		}
	})
}
