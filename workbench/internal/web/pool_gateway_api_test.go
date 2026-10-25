package web

import (
	"net"
	"net/http"
	"strconv"
	"testing"
)

// TestPoolAPIReportsGatewayReachability 钉住面板中性措辞所依赖的读数字段。
//
// 背景（2026-10-20 老大实报）：总览卡把「8326 端口没监听」渲染成红字
// 「网关错误：端口未监听: dial tcp ...」—— 那是 DSH 桌面端起停的正常环境态，
// 已被 catalog 回退兜住。前端要把它降级成中性提示，前提是 /api/pool 的
// gateway 对象必须真的带上 reachable/authorized。少一个字段，前端就会走
// 「网关错误」红色分支，修复等于没做。
func TestPoolAPIReportsGatewayReachability(t *testing.T) {
	// 找一个确定被拒绝的回环端口（先绑后放）。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法绑定临时端口：%v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	t.Setenv("DSH_OPENAI_GATEWAY_PORT", strconv.Itoa(port))

	srv, pool := newTestPoolServer(t)
	// 真跑一次同步，让 service 记录本轮网关健康读数（端口确定被拒绝）。
	pool.SyncNow("test")

	code, body := call(t, srv, http.MethodGet, "/api/pool", "")
	if code != http.StatusOK {
		t.Fatalf("status code=%d body=%v", code, body)
	}
	gw, okf := body["gateway"].(map[string]any)
	if !okf {
		t.Fatal("/api/pool 响应必须含 gateway 对象（面板措辞依赖它）")
	}
	for _, key := range []string{"reachable", "authorized", "error", "statusCode", "detail"} {
		if _, has := gw[key]; !has {
			t.Errorf("gateway 缺少字段 %q —— 前端分级措辞与排查都会退化成盲判", key)
		}
	}
	if gw["reachable"] != false {
		t.Errorf("端口被拒绝时 gateway.reachable 应为 false，实际 %v", gw["reachable"])
	}
	if gw["authorized"] != false {
		t.Errorf("端口被拒绝时 gateway.authorized 应为 false，实际 %v", gw["authorized"])
	}
}
