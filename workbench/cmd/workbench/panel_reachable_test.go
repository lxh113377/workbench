package main

import (
	"net"
	"strconv"
	"testing"
)

// TestPanelPortReachable 钉住本轮新增的就绪判据 helper（2026-11-01 round 181）。
//
// 为什么要单独测这个 helper：它是「面板真就绪」的唯一判据，若它恒真或恒假，
// 上面那条 flaky 修复就等于没修（本仓在册纪律：判据的部件也要有判据）。
//
// 三态用例（真实网listen，不是假端口）：
//
//	① 端口真在监听 ⇒ true；
//	② 端口没人听 ⇒ false；
//	③ 畸形串（无端口/非 URL）⇒ false，不许 panic。
func TestPanelPortReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("起监听失败: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port
	good := "http://127.0.0.1:" + strconv.Itoa(port) + "/panel"

	if !panelPortReachable(good) {
		t.Fatalf("端口在监听却判不可达: %s", good)
	}

	// ② 关掉监听后必须转 false —— 关键：证明它真的在探测，而不是「有 URL 形状就返回 true」。
	_ = ln.Close()
	if panelPortReachable(good) {
		t.Fatalf("端口已关闭却仍判可达（判据恒真，flaky 修复无效）: %s", good)
	}

	// ③ 畸形串不许 panic，且必须 false。
	for _, bad := range []string{"", "http://127.0.0.1", "not-a-url", "http://127.0.0.1:abc"} {
		if panelPortReachable(bad) {
			t.Fatalf("畸形串被误判为可达: %q", bad)
		}
	}
}
