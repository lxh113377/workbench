package web

import (
	"strings"
	"testing"
)

// functionBody 按大括号配对抽取 `name` 的函数体（比固定字节窗口可靠：注释增删不该改变判据语义）。
// 找不到即判红 —— 本会话登记过的"判据空转"形态：锚点失配时若静默返回空串，测试会一直判绿。
func functionBody(t *testing.T, name string) string {
	t.Helper()
	i := strings.Index(appJS, name)
	if i < 0 {
		t.Fatalf("appJS 里找不到 %s ⇒ 结构已变，本判据必须同步改写，不许静默放行", name)
	}
	rest := appJS[i:]
	o := strings.Index(rest, "{")
	if o < 0 {
		t.Fatalf("%s 没有函数体", name)
	}
	depth := 0
	for k := o; k < len(rest); k++ {
		switch rest[k] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[o+1 : k]
			}
		}
	}
	t.Fatalf("%s 的函数体大括号不配对（JS 被改坏）", name)
	return ""
}

// TestSSEFallbackRendersWhenStreamDown 钉住 T107（round 154 前端面）：
// 「实时流断了，窗格必须还有人刷，且降级要可见」。
//
// 一手取证（本轮 grep 证实，不是推测）：改前面板里
//
//	· 主体窗格的持续刷新**只有 SSE 一个驱动源**（es.onmessage 的节流窗口）；
//	· 顶栏有 setInterval(refreshTop, 5000) 兜底；
//	· 全仓 appJS 内 onerror / readyState 出现次数 = 0。
//
// ⇒ 流一断的表现是"顶栏数字还在动、内容冻结"：一种看起来活着的**静默陈旧**，
//
//	比报错更难查（用户以为在实时刷新，实际看的是最后一次事件时刻的快照）。
//
// 判据要同时钉住"接得上"和"不越界"：兜底范围必须与 SSE 窗口一致（只接 overview/queue）。
// 给 outputs/templates/settings 加轮询会改语义（它们本就按需渲染）并凭空多打接口。
func TestSSEFallbackRendersWhenStreamDown(t *testing.T) {
	body := functionBody(t, "async function refreshTop()")
	// 进门断言：抽取成功且体量合理（防空函数体蒙过后面所有 Contains）
	if len(body) < 600 {
		t.Fatalf("refreshTop 函数体只抽到 %d 字节 ⇒ 抽取或结构有问题，后续断言不可信", len(body))
	}
	cases := []struct {
		name string
		need string
		why  string
	}{
		{"在检查流状态", "readyState", "不看 readyState 就无从知道流断了（此前全仓零处检查）"},
		{"以 OPEN 为健康态", "EventSource.OPEN", "CONNECTING 是首连接正常态，必须显式区分"},
		{"有连续计数", "sseDownStreak", "单次未 OPEN 不能就接管，需连续周期计数"},
		{"阈值防误伤", ">= 3", "首连接/瞬时抖动不得触发降级（阈值至少 3 个 5 秒周期）"},
		{"接管时真的重画窗格", "render(nm)", "只 toast 不接管 = 内容仍会冻结"},
		{"降级对用户可见", "toast(", "静默降级会让陈旧冒充正常（与顶栏停摆自证同一原则）"},
	}
	for _, c := range cases {
		if !strings.Contains(body, c.need) {
			t.Errorf("refreshTop 缺少 %s（%q）—— %s", c.name, c.need, c.why)
		}
	}
	// 范围守恒：兜底只接 overview/queue，不得给按需渲染的窗格加轮询
	if !strings.Contains(body, "nm === 'overview' || nm === 'queue'") {
		t.Errorf("兜底范围未与 SSE 窗口一致（应只接 overview/queue）")
	}
	for _, forbidden := range []string{"renderOutputs(", "renderTemplates(", "renderSettings("} {
		if strings.Contains(body, forbidden) {
			t.Errorf("refreshTop 里直调了 %s ⇒ 越界改语义：这些窗格本就按需渲染，不该被 5 秒轮询驱动", forbidden)
		}
	}
}
