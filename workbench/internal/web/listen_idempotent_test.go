package web

// listen_idempotent_test.go 守护「Listen 必须幂等」（2026-10-19 现场）。
//
// 现场：Server 有 mu + starting 两个字段，是为「防止并发/重复启动」准备的，
// 但 starting **从未被读或写**（全库只出现在结构体声明处）——
// 于是 Listen() 没有任何幂等保护：重复调用会 net.Listen 成功、
// 覆盖 s.ln/s.port、再起一个 Serve 循环 => 端口漂移 + 两个监听并存。
//
// 为什么值得修：
//   - main.go 与 ui_probe.go 都调用 Listen()，路径不止一条；
//   - 端口漂移会让 panel.url 指向错误的实例（现场「重试」事故的同源风险）；
//   - 双 Serve 会让面板响应时有时无。
//
// 判据：Listen() 第二次调用必须**不重新监听**，且端口保持不变。

import (
	"testing"
)

// TestListenIsIdempotent 断言：重复 Listen 不改变端口、不重复监听。
func TestListenIsIdempotent(t *testing.T) {
	s, _, _ := newTestServer(t)
	if err := s.Listen(); err != nil {
		t.Fatalf("首次 Listen 应成功: %v", err)
	}
	defer s.Close()
	p1 := s.Port()
	if p1 <= 0 {
		t.Fatal("首次 Listen 后端口应有效")
	}

	// 第二次调用：必须幂等（不换端口、不起第二个 Serve）
	if err := s.Listen(); err != nil {
		t.Fatalf("重复 Listen 不应报错（应幂等返回 nil）: %v", err)
	}
	p2 := s.Port()
	if p2 != p1 {
		t.Fatalf("重复 Listen 不得改变端口（会漂移出 panel.url 指向）：first=%d second=%d", p1, p2)
	}
}
