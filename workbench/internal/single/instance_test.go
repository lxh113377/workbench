package single

import (
	"fmt"
	"net"
	"strconv"
	"testing"
)

// maxUsablePort 是 TCP 端口号上界（65535）。
const maxUsablePort = 65535

// portTrioFits 判断「以 base 为主实例端口」时，本测试需要的三元组是否都落在合法端口号内：
// 需要 base-1（隐式，向下顺延会扫到）与 base+1（被查邻居）、base+2（副本）都不越界。
// 取 base+2 为最紧约束；下界取 1024 避开知名端口段（ephemeral 端口实际远高于此，写出来只为让边界可测）。
//
// 为什么抽成纯函数并单测（2026-11-01 CI 事故）：本测试原先直接 Listen(base+2)，而 base 由 OS 随机分配，
// Windows 动态端口范围含 65534/65535 ⇒ 撞上就 listen tcp: address 65536: invalid port（CI 实测
// instance_test.go:38 报此错、internal/single 包 FAIL）。那是**测试自身的端口算术越界**，与被测行为无关；
// 而我把原来的宽容分支改成 t.Fatalf 之后，它会偶发把 CI 打红。边界逻辑写成纯函数才有判据可钉。
func portTrioFits(base int) bool {
	return base >= 1024 && base+2 <= maxUsablePort
}

// TestPortTrioFits 表驱动钉住上界边界（含事故值 65534/65535）。
func TestPortTrioFits(t *testing.T) {
	cases := []struct {
		base int
		want bool
	}{
		{65533, true},  // base+2 = 65535，恰好可用
		{65534, false}, // base+2 = 65536，越界：CI 事故形态
		{65535, false},
		{49152, true}, // Windows 动态端口下界，典型可用值
		{1023, false}, // 下界排除知名端口段
	}
	for _, c := range cases {
		if got := portTrioFits(c.base); got != c.want {
			t.Fatalf("portTrioFits(%d) = %v，期望 %v", c.base, got, c.want)
		}
	}
	t.Logf("上界边界判定正确（65533 可用 / 65534、65535 越界）")
}

// listenMainAndReplica 取一组满足判据需求的端口三元组：base 在监听、base+1 空闲、base+2 作为副本在监听。
// 失败就换下一个随机 base 重试（越界与端口被占都走重试，不 Fatalf —— 那是环境噪声，不是被测行为的问题）。
// 返回的 listener 由调用方 Close；重试过程中已建的会先关掉，不泄漏。
func listenMainAndReplica() (main, replica net.Listener, base int, err error) {
	for attempt := 0; attempt < 64; attempt++ {
		m, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			return nil, nil, 0, e
		}
		p := m.Addr().(*net.TCPAddr).Port
		if !portTrioFits(p) {
			_ = m.Close()
			continue
		}
		// base+1 必须真的空闲：探针占一下再放开（否则邻居检查会因别人占用而假绿/假红）
		probe, pe := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p+1)))
		if pe != nil {
			_ = m.Close()
			continue
		}
		_ = probe.Close()
		r2, re := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p+2)))
		if re != nil {
			_ = m.Close()
			continue
		}
		return m, r2, p, nil
	}
	return nil, nil, 0, fmt.Errorf("64 次重试都没凑齐 base/base+1 空闲/base+2 三元组（环境端口拥挤）")
}

// TestProbeExactPortDoesNotScanNeighbors 守护 2026-10-19 现场缺陷：
// 端口探测**不得**顺延扫描邻居端口，否则会把压测副本当成主实例面板，
// 浏览器指向副本一死后，首页自动响应连续失败 → 按钮落进 error 态（用户看到「重试」）。
//
// 判据为什么必须这样排布（2026-10-31 变异实验实测出的豁口与修法）：
//
//	旧版把「base+2 起监听」放在**邻居检查之后** ⇒ 检查 base+1 时上方还是空的，
//	「从 base+1 向上连扫 10 个端口」这版历史缺陷照样返回 false ⇒ 测试仍 PASS（变异存活）。
//	现在先凑齐三元组再检查，让 base+1 的**上下两侧都有监听**：
//	  · 向上顺延变异扫 base+1→base+2 会命中副本 ⇒ 本条 t.Fatal 咬死；
//	  · 向下顺延变异扫 base+1→base 会命中主实例 ⇒ 同一条也咬死。
//	一个检查同时杀两个方向，靠的是「被查点两侧都有监听、而两侧都不该被当成实例」。
func TestProbeExactPortDoesNotScanNeighbors(t *testing.T) {
	ln, ln2, base, err := listenMainAndReplica()
	if err != nil {
		t.Fatalf("准备端口三元组失败: %v", err)
	}
	defer func() { _ = ln.Close() }()
	defer func() { _ = ln2.Close() }()

	// 前置自证：两个监听都真的在（否则后面的 false 断言可能只是「谁都没监听」）。
	if p, ok := ProbeExactPort("127.0.0.1", base); !ok || p != base {
		t.Fatalf("主实例端口应精确命中自身，实得 p=%d ok=%v（前置条件不成立）", p, ok)
	}
	if p, ok := ProbeExactPort("127.0.0.1", base+2); !ok || p != base+2 {
		t.Fatalf("副本端口应精确命中自身，实得 p=%d ok=%v（前置条件不成立）", p, ok)
	}

	// 核心断言：base+1 自身没监听，上下两侧各有监听（base 与 base+2），
	// 任何方向的顺延都会命中其中之一 ⇒ 必须返回 false，否则就是「把邻居当成实例」。
	if p, ok := ProbeExactPort("127.0.0.1", base+1); ok {
		t.Fatalf("不得把邻居端口 %d 当成工作台实例（探测结果 %d —— 说明存在向上或向下顺延）", base+1, p)
	}
}
