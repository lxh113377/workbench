package web

import (
	"strings"
	"testing"
)

// TestPollersGuardedAndBounded 钉住「轮询器必须有重入守卫且与超时配套」。
//
// 一手缺陷（2026-10-24 代码级取证，非推测）：
//
//	handleStatus 里 engine.Phase() 与 LastError() 都要 e.mu.Lock()（engine.go:623/630），
//	而 Start() 全程持 e.mu（engine.go:396）且 bootstrapAutonomy 内含模型调用 ——
//	点「启动」时面板每 4s(ping) / 5s(refreshTop) 一次的 /api/status 会排队等锁；
//	api() 又没有超时，于是 ① 一次卡顿就堆一串在途请求，② 旧响应后到会把顶栏/按钮
//	状态**往回拨**（paint 被过期数据覆盖）。
//
// 为什么守卫与超时必须**配套**（这条最容易被后人拆掉一半）：
//
//	只加守卫不加超时 => 一次永久挂起让 busy 永不复位 => 轮询**永久停摆**，比原缺陷更糟。
//	只加超时不加守卫 => 超时值若大于间隔仍会叠加。
//	所以本测试同时断言两者，并断言超时后 promise 必然 settle（见行为测试）。
//
// 为什么超时是**可选参数**而不是全局默认：
//
//	/api/pool/probe 是合法慢接口 —— probeSpecs 串行遍历每个 base（service.go:297），
//	单模型 client 超时 10s（service.go:56），整体可达 N×10s。一刀切会把能用的功能改坏。
//
// guardResetInFinally 判断 `guard = false` 是否出现在 finally 子句**内部**（按大括号配对抽取）。
// 抽取失败一律判 false（宁可判红，不静默放行）。
func guardResetInFinally(t *testing.T, seg, guard string) bool {
	t.Helper()
	i := strings.Index(seg, "finally {")
	if i < 0 {
		return false
	}
	rest := seg[i:]
	o := strings.Index(rest, "{")
	depth := 0
	for k := o; k < len(rest); k++ {
		switch rest[k] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.Contains(rest[o:k], guard+" = false")
			}
		}
	}
	return false
}

// codeSetIntervalCalls 统计**非注释行**里的 setInterval( 调用次数。
func codeSetIntervalCalls(js string) int {
	n := 0
	for _, line := range strings.Split(js, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") {
			continue
		}
		n += strings.Count(t, "setInterval(")
	}
	return n
}

func TestPollersGuardedAndBounded(t *testing.T) {
	// 1) api() 支持可选超时，且不传时不建 AbortController（保持旧行为）
	a := strings.Index(appJS, "const api = async")
	if a < 0 {
		t.Fatal("找不到 api() 定义")
	}
	body := appJS[a:]
	if e := strings.Index(body, "const ESC"); e > 0 {
		body = body[:e]
	}
	if !strings.Contains(body, "AbortController") {
		t.Error("api() 缺 AbortController —— 请求可能永久挂起")
	}
	if !strings.Contains(body, "timeoutMs ?") {
		t.Error("api() 的超时必须是可选的（probe 等合法慢接口不能被一刀切）")
	}
	if !strings.Contains(body, "delete rest.timeoutMs") {
		t.Error("timeoutMs 必须从 fetch 参数里剥离，否则会成为非法 fetch option")
	}
	// 2) 两个高频轮询器都要有守卫 + 显式超时
	type poller struct {
		head  string
		guard string
		end   string
	}
	pollers := []poller{
		{head: "const ping = async", guard: "pingBusy", end: "const start = async"},
		{head: "async function refreshTop()", guard: "topBusy", end: "const btnPause"},
	}
	for _, p := range pollers {
		i := strings.Index(appJS, p.head)
		if i < 0 {
			t.Fatalf("找不到轮询函数 %q", p.head)
		}
		seg := appJS[i:]
		if e := strings.Index(seg, p.end); e > 0 {
			seg = seg[:e]
		}
		if !strings.Contains(seg, "if ("+p.guard+") { return; }") {
			t.Errorf("%s 缺重入守卫 %s —— 慢响应会叠加在途请求", p.head, p.guard)
		}
		if !strings.Contains(seg, p.guard+" = true") {
			t.Errorf("%s 的守卫没有置位", p.head)
		}
		// 复位必须发生在 finally **块内**：原来断的是单行字面量 `"finally { topBusy = false"`，
		// 于是把 finally 写成多行（块内第一件事仍是复位）就会判红 —— 那是排版不是缺陷。
		// 这里改为抽取 finally 子句再判"其中含复位"，语义**更严**：单行、多行都要求真的在 finally 内，
		// 而把复位移出 finally（异常路径不复位）依旧判红（已做负控）。不是放宽。
		if !guardResetInFinally(t, seg, p.guard) {
			t.Errorf("%s 的守卫必须置位并在 finally 里复位（否则一次异常就永久停摆）", p.head)
		}
		if !strings.Contains(seg, "timeoutMs:") {
			t.Errorf("%s 缺超时——守卫会因永久挂起而再也不复位", p.head)
		}
	}
	// 3) 不得新增定时器：轮询频率由既有 setInterval 承担（home_contract_test 另有上限断言）
	//
	// 口径修正（round 154）：原来数的是全文 `setInterval(` 出现次数 —— 我往注释里写了
	// 「而顶栏另有 setInterval(refreshTop, 5000) 兜底」就被算成一处新定时器，判据报了假红。
	// 本意是「不许新增真实定时器」，所以改成只数**真实调用**（剥掉注释行后统计），
	// 这样注释怎么写都不影响判据，而真加一个 setInterval 仍会被抓到（已做负控）。
	if n := codeSetIntervalCalls(appJS); n != 2 {
		t.Errorf("appJS 的真实定时器应为 2 处，实为 %d 处（守卫/超时不该引入新定时器）", n)
	}
}
