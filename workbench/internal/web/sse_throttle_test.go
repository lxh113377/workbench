package web

import (
	"strings"
	"testing"
)

// onMessageBody 取出 es.onmessage 处理函数的**函数体**（大括号配对，非按行正则）。
// 为什么不用 grep 式单行匹配：本仓踩过的判据失效形态之一是"用通用词做锚"（见 round 135 记录），
// 而 JS 会被 gofmt 之外的手工排版改动，按行匹配极易假绿。
func onMessageBody(t *testing.T) string {
	t.Helper()
	i := strings.Index(appJS, "es.onmessage = function () {")
	if i < 0 {
		t.Fatal("找不到 es.onmessage 处理器 ⇒ 面板已不再消费 SSE？本判据需随之改写，不能静默放行")
	}
	rest := appJS[i:]
	open := strings.Index(rest, "{")
	if open < 0 {
		t.Fatal("处理器无函数体")
	}
	depth := 0
	for k := open; k < len(rest); k++ {
		switch rest[k] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[open+1 : k]
			}
		}
	}
	t.Fatal("处理器函数体大括号不配对（JS 被改坏了）")
	return ""
}

// TestSSEHandlersThrottleTopBarRefresh 钉住"顶栏刷新必须进 SSE 节流窗口"。
//
// 现场（round 152 前端/性能面实测）：2026-10-23 那次并发走查把 render() 收进了 400ms 窗口，
// 注释里自证「实测 SSE 峰值约 10 条/秒」且「事件频率 × 全量重建」是平方级浪费 —— 但**闸门只做了一半**：
// 旧实现里 refreshTop() 留在窗口之外、且写在页签判断之前。而 refreshTop() 是一次 /api/status 请求，
// 该接口按本包注释要拿**引擎写锁**（Phase/LastError 都持锁）。⇒ 顶栏轮询从设计的 5 秒一次
// 退化为最高 10 次/秒，全部打在引擎写锁上，并且停在任何页签都在打。
//
// 判据形式：结构不变量 —— refreshTop( 必须出现在 setTimeout( 之后**且在其回调内**（回调以 ", 400)" 收尾）。
// 为什么用顺序而不是计数请求数：本包没有 JS 运行时，跑不了真页面；而"谁在外层"这一事实
// 足以区分"每事件直调"（旧，必红）与"窗口内补跑一次"（新，绿）。开发期我用 Node 抽出旧/新两段代码实测：
// 旧版 refreshTop 在偏移 7、setTimeout 在 289（⇒ 每事件直调）；新版 refreshTop 在 479 且位于回调内。
func TestSSEHandlersThrottleTopBarRefresh(t *testing.T) {
	body := onMessageBody(t)
	ti := strings.Index(body, "refreshTop(")
	si := strings.Index(body, "setTimeout(")
	if ti < 0 {
		t.Fatal("处理器里已无 refreshTop 调用 ⇒ 顶栏不再随事件更新？语义变了需同步改本判据与注释")
	}
	if si < 0 {
		t.Fatal("处理器里已无节流窗口 ⇒ SSE 每条事件都会直接触发请求（本轮修的正是这个）")
	}
	if ti < si {
		t.Fatalf("refreshTop 在节流窗口之外（偏移 %d < %d）⇒ 每条 SSE 事件都会打一次带引擎写锁的 /api/status；"+
			"实测 SSE 峰值约 10 条/秒 ⇒ 顶栏轮询失控。必须把它移进 setTimeout 回调", ti, si)
	}
	// 进一步要求"确实在回调内"，而不是"排在后面的另一条语句"
	cb := body[si:]
	closeAt := strings.Index(cb, ", 400)")
	if closeAt < 0 {
		t.Fatal("找不到节流窗口的收尾（窗口时长被改动？本判据的 400ms 假设需同步更新）")
	}
	inner := strings.Index(cb[:closeAt], "refreshTop(")
	if inner < 0 {
		t.Fatal("refreshTop 不在节流回调内 ⇒ 仍是每事件直调，闸门无效")
	}
	// 语义守恒：顶栏要"任何页签都更新" ⇒ 置位不得在页签判断之后（否则停在 settings 就不刷新顶栏）
	guardAt := strings.Index(body, "if (name === 'overview' || name === 'queue')")
	setAt := strings.Index(body, "topPending = true")
	if guardAt < 0 || setAt < 0 {
		t.Fatalf("页签判断或顶栏置位被改写（guardAt=%d setAt=%d）⇒ 请核对语义后更新本判据", guardAt, setAt)
	}
	if setAt > guardAt {
		t.Fatalf("topPending 被移到页签判断之后（%d > %d）⇒ 停在非 overview/queue 页时顶栏不再刷新，语义回归", setAt, guardAt)
	}
}
