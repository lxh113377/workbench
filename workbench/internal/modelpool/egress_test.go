package modelpool

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestEgressKeyNormalizesTopology 钉住「出口归一」的口径（round 132 · P0-2 更正）。
//
// 现场实测（agent 根 models/pool.json）：池内 38 条 spec 的 baseUrl 全部是
// http://127.0.0.1:8326/v1 —— 走 https 的车道数 = 0。也就是说"上游怎么连、证书信不信
// 得过"是**一个出口**上的事，不是 38 条独立网络路径。这一点原先只写在 failover.go 的
// 注释里，而那条注释的归因前提（"同池其他车道的网络路径不同"）在本部署下是假的。
func TestEgressKeyNormalizesTopology(t *testing.T) {
	const gw = "http://127.0.0.1:8326/v1"
	ref := egressKey(Spec{ID: "ref", BaseURL: gw}, gw)
	// 前五条与显式那条必须同出口
	for _, tc := range []struct {
		name string
		s    Spec
		base string
	}{
		{"显式 baseUrl 与内置网关同址", Spec{ID: "a", BaseURL: gw}, gw},
		{"空 baseUrl 走内置网关 ⇒ 必须归到同一出口", Spec{ID: "b", BaseURL: ""}, gw},
		{"大小写差异不该分裂出口", Spec{ID: "c", BaseURL: "HTTP://127.0.0.1:8326/V1"}, gw},
		{"末尾斜杠不该分裂出口", Spec{ID: "d", BaseURL: "http://127.0.0.1:8326/v1/"}, gw},
		{"路径后缀不该分裂出口（同 host 即同出口）", Spec{ID: "e", BaseURL: "http://127.0.0.1:8326/v1/chat"}, gw},
	} {
		if got := egressKey(tc.s, tc.base); got != ref {
			t.Errorf("%s：出口 key=%q，应与 %q 相同", tc.name, got, ref)
		}
	}
	// 空 baseUrl 且拿不到网关地址时给"未知出口"，且不许与真实出口相等：
	// 宁可漏判共用，也不可将不同出口误合成一个（误合成的危害更大）。
	unk := egressKey(Spec{ID: "f", BaseURL: ""}, "")
	if unk != "(unknown)" {
		t.Errorf("无网关地址时应给未知出口，实得 %q", unk)
	}
	if unk == ref {
		t.Fatal("未知出口被误合成真实出口")
	}
	// 真实不同出口必须区分开，否则"共用一个出口"这条判据毫无意义。
	for _, u := range []string{"https://api.kilo.ai/v1", "http://127.0.0.1:9999/v1", "https://eac.example.com/v1"} {
		if egressKey(Spec{ID: "x", BaseURL: u}, gw) == ref {
			t.Errorf("%s 被误归一到本机网关出口", u)
		}
	}
}

// TestSharesOneEgressOnRealPoolShape 用实测拓扑样本断言：38 条同址车道 ⇒ 一个出口。
// 把现场事实钉成判据，而不是让它继续以注释形式存在（注释会漂，测试不会）。
func TestSharesOneEgressOnRealPoolShape(t *testing.T) {
	const gw = "http://127.0.0.1:8326/v1"
	specs := make([]Spec, 0, 38)
	for i := 0; i < 38; i++ {
		specs = append(specs, Spec{ID: fmt.Sprintf("m-%02d", i), BaseURL: gw})
	}
	if got := len(egressGroups(specs, gw)); got != 1 {
		t.Fatalf("38 条同址车道应归为 1 个出口，实得 %d", got)
	}
	if !sharesOneEgress(specs, gw) {
		t.Fatal("sharesOneEgress 应为 true：本机池就是这个形状")
	}
	// 样本不足不许报真（单条谈不上"共用"）
	if sharesOneEgress(specs[:1], gw) {
		t.Error("只有 1 条车道时不该报共用出口")
	}
	// 掺进第二个真实出口后必须变成多出口，否则拓扑变化不可见
	mixed := append(append([]Spec{}, specs...), Spec{ID: "other", BaseURL: "https://api.kilo.ai/v1"})
	if sharesOneEgress(mixed, gw) {
		t.Error("存在第二个出口时仍报共用 ⇒ 该判据失效")
	}
}

// TestProbeCycleCarriesEgress 钉住「拓扑进低频摘要」这条可观测性不是空头：
// probe-cycle 摘要必须同时带车道数与出口数 —— 只有车道数就看不出"是不是同一个出口"。
func TestProbeCycleCarriesEgress(t *testing.T) {
	src := readOwnManagerSource(t)
	if !containsAll(src, "egressCount := len(egressGroups(m.specs, m.gatewayBase))", "探测面 %d 条车道 → %d 个网关端点") {
		t.Fatal("probe-cycle 摘要不再携带端点数 ⇒ 拓扑重新不可见（P0-2 的教训）")
	}
	// 措辞必须自带适用面（T87）：写「共用一个出口」会让人以为整机只有一条出网路，
	// 而这里算的只是探测面 —— 执行面在 dsh 侧。上一轮我就被自己的文案误导过。
	if strings.Contains(src, "\"｜池 %d 条车道共用 %d 个出口\"") {
		t.Fatal("摘要措辞退回到「共用一个出口」⇒ 未标明适用面，会再次诱导跨面嫁接")
	}
	// 真正的不变量是**顺序**：egressCount 计算 → 恰好一次 Unlock → probe-cycle 日志。
	// 即"在锁内算、在锁外写日志"（m.log 落在 Unlock 之后，届时若还去读 m.specs 就是数据竞争）。
	// 判据写法教训（本轮实测）：上一版我用 strings.Index(src, "m.mu.Unlock()")，那是**全文件第一个**
	// Unlock（manager.go 有 16 个方法各有自己的 Unlock），于是拿到的是无关位置 ⇒ 误判成违规。
	// ⇒ 断言"两个锚点之间"的事件数，而不是拿文件级首次命中当证据。
	// Go 的 const 只能是常量表达式，strings.Index 这类调用必须用 :=（上一版写成 const ⇒ 编译失败）。
	egr := strings.Index(src, "egressCount := len(egressGroups")
	cyc := strings.Index(src, "Kind: \"probe-cycle\"")
	if egr < 0 || cyc < 0 || cyc < egr {
		t.Fatalf("锚点缺失或顺序颠倒：egr=%d cyc=%d", egr, cyc)
	}
	unlocks := 0
	for i := egr; i < cyc; i++ {
		if strings.HasPrefix(src[i:], "m.mu.Unlock()") {
			unlocks++
		}
	}
	if unlocks != 1 {
		t.Fatalf("egressCount 与 probe-cycle 日志之间应有 1 次 Unlock（锁内算、锁外写），实得 %d", unlocks)
	}
}

// readOwnManagerSource / containsAll 是同包测试的小工具（判据需要对照源码本身）。
// TestTopologyPremiseNotAssumedInComments 守住一条纪律：拓扑是**算出来的事实**，不许再以注释假设出现。
func TestTopologyPremiseNotAssumedInComments(t *testing.T) {
	// 上一版 failover.go 在这里断言「同池其他车道的网络路径不同」，而本机实测是 38 条车道共用
	// 一个明文回环出口 —— 拿假前提做的归因会把出口级故障摊成整池故障。⇒ 该措辞已从注释中摘除，
	// 事实来源改成 egress.go + 本文件；若有人把这句假设写回去（它读起来很顺，很容易复发），这条判据判红。
	// 注意这是**源文本守卫**：它不判行为（行为未变，本轮明确不改归因，见下方注释），只防止错误前提重新成为依据。
	for _, f := range []string{"failover.go", "manager.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读不到 %s：%v（判据失去对照面，判红而非跳过）", f, err)
		}
		if strings.Contains(string(b), "同池其他车道的网络路径不同") {
			_ = strings.Contains
			t.Errorf("%s 又出现「同池其他车道的网络路径不同」这句未核实的拓扑假设 ⇒ 必须改用 egressKey/egressGroups 算出来的事实", f)
		}
	}
}

func containsAll(s string, subs ...string) bool {
	for _, x := range subs {
		if !strings.Contains(s, x) {
			return false
		}
	}
	return true
}

func readOwnManagerSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("manager.go")
	if err != nil {
		t.Fatalf("读不到 manager.go：%v（判据失去对照面，判红而非跳过）", err)
	}
	return string(b)
}

// containsAll 逐条检查源码片段是否全部存在。
// 命名教训：上一版我在同包新增了 indexOf，与 pool_test.go:810 的既有定义重名 ⇒
// "redeclared in this block" 编译失败。⇒ 同包加 helper 前必须先 grep 同名，别凭手感造。
