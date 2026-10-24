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
		if !strings.Contains(seg, p.guard+" = true") || !strings.Contains(seg, "finally {"+" "+p.guard+" = false") {
			t.Errorf("%s 的守卫必须置位并在 finally 里复位（否则一次异常就永久停摆）", p.head)
		}
		if !strings.Contains(seg, "timeoutMs:") {
			t.Errorf("%s 缺超时——守卫会因永久挂起而再也不复位", p.head)
		}
	}
	// 3) 不得新增定时器：轮询频率由既有 setInterval 承担（home_contract_test 另有上限断言）
	if n := strings.Count(appJS, "setInterval("); n != 2 {
		t.Errorf("appJS 的 setInterval 应为 2 处，实为 %d 处（守卫/超时不该引入新定时器）", n)
	}
}
