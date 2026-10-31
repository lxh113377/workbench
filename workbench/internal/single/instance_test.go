package single

import (
	"net"
	"strconv"
	"testing"
)

// TestProbeExactPortDoesNotScanNeighbors 守护 2026-10-19 现场缺陷：
// 端口探测**不得**顺延扫描邻居端口，否则会把压测副本当成主实例面板，
// 浏览器指向副本一死后，首页自动响应连续失败 → 按钮落进 error 态（用户看到「重试」）。
//
// 判据为什么必须这样排布（2026-10-31 变异实验实测出的豁口与修法）：
//
//	旧版把「base+2 起监听」放在**邻居检查之后** ⇒ 检查 base+1 时上方还是空的，
//	「从 base+1 向上连扫 10 个端口」这版历史缺陷照样返回 false ⇒ 测试仍 PASS（变异存活）。
//	现在把 ln2 提到前面，让 base+1 的**上方真有监听**：
//	  · 向上顺延变异扫 base+1→base+2 会命中副本 ⇒ 本条 t.Fatal 咬死；
//	  · 向下顺延变异扫 base+1→base 会命中主实例 ⇒ 同一条也咬死。
//	一个检查同时杀两个方向，靠的是"被查点的两侧都有监听、而两侧都不该被当成实例"。
func TestProbeExactPortDoesNotScanNeighbors(t *testing.T) {
	// 在 base 上起一个假监听，代表「主实例」。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer func() { _ = ln.Close() }()
	base := ln.Addr().(*net.TCPAddr).Port

	if _, ok := ProbeExactPort("127.0.0.1", base); !ok {
		t.Fatal("精确端口命中失败（主实例应被识别）")
	}

	// 邻居端口（+2）也起一个监听，代表「压测副本」。**必须早于下面的邻居检查** ——
	// 顺序就是这条判据的杀伤力所在（见函数头注释），所以这里不做成"后面再补"。
	ln2, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(base+2)))
	if err != nil {
		t.Fatalf("副本监听失败（该检查失去意义）: %v", err)
	}
	defer func() { _ = ln2.Close() }()

	// base+1 自身没监听：上下两侧各有一个监听端口（base 与 base+2），
	// 任何方向的顺延都会命中其中之一 ⇒ 必须返回 false，否则就是"把邻居当成实例"。
	if p, ok := ProbeExactPort("127.0.0.1", base+1); ok {
		t.Fatalf("不得把邻居端口 %d 当成工作台实例（探测结果 %d —— 说明存在向上或向下顺延）", base+1, p)
	}
	// 同理：副本端口必须命中自己，且不能被解释成主实例（精确性另一面）。
	if p, ok := ProbeExactPort("127.0.0.1", base+2); !ok || p != base+2 {
		t.Fatalf("副本端口应精确命中自身，实得 p=%d ok=%v（期望 p=%d）", p, ok, base+2)
	}
	// 反向守护：base 在副本存在时仍须精确命中（不得被邻居劫持）。
	if p, ok := ProbeExactPort("127.0.0.1", base); !ok || p != base {
		t.Fatalf("主实例端口应精确命中自身，实得 p=%d ok=%v", p, ok)
	}
}
