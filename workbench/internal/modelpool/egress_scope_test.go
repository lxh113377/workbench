package modelpool

import (
	"os"
	"strings"
	"testing"

	"workbench/internal/executor"
)

// TestEgressScopedOnlyUpgradesFingerprintHits 钉住 T86 第一档的边界：
// 只有「上游正文写明出口不通」才升为出口级，其余一律保持车道级处置。
//
// 取证依据（两条独立通路一致，见报告 E12 与两份 xtmp 复核件）：
//
//	· 证书链类与「他车道同窗成功」共存率 = 0/1799 = 0.0% ⇒ 出口级；
//	· stream_cut 类共存率 = 41/91 = 45.1% ⇒ 多数是车道级（实测 z-ai/glm-5.3 报流断时另一条车道正常产出）。
//
// ⇒ 一刀切会把 21~33 条健康车道一起熔掉：下面反向半边就是防这个。
func TestEgressScopedOnlyUpgradesFingerprintHits(t *testing.T) {
	trans := string(executor.KindTransport)
	cut := string(executor.KindStreamCut)
	cases := []struct {
		name   string
		kind   string
		detail string
		want   bool
	}{
		{"transport + 自签证书全文 ⇒ 出口级", trans, "dsh: TRANSPORT: model request failed: fetch failed - self signed certificate; if the root CA is installed locally, try running Node.js with --use-system-ca", true},
		{"transport + DEPTH_ZERO 错误码 ⇒ 出口级", trans, "TRANSPORT: ... (DEPTH_ZERO_SELF_SIGNED_CERT)", true},
		{"大小写不敏感", trans, "Self Signed Certificate", true},
		// 反向（最关键）：同为网络类但正文没有出口级指纹 ⇒ 不许升档
		{"transport + 对端超时 ⇒ 保持车道级", trans, "dsh: TRANSPORT: fetch failed: ETIMEDOUT 10.0.0.1:443", false},
		{"stream_cut 普通流断 ⇒ 保持车道级", cut, "our free model closed the stream before its finish token, without answering", false},
		{"stream_cut 带证书字样也升档（同一指纹判别，不因 Kind 例外）", cut, "self signed certificate", true},
		// 别的 Kind 不参与正文分档：not_served 有自己更窄的既有判据，限流族走 stopSwitch
		{"not_served 带证书字样也不走这条路", string(executor.KindNotServed), "self signed certificate", false},
		{"rate_limited 不参与正文分档", string(executor.KindRateLimited), "self signed certificate", false},
		{"空正文不升档", trans, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := egressScoped(tc.kind, tc.detail); got != tc.want {
				t.Fatalf("egressScoped(%s, %q) = %v，应为 %v", tc.kind, tc.detail, got, tc.want)
			}
		})
	}
}

// TestEgressScopeIsTableDriven 防止 T86 又长成"第二处私有清单"（本会话同族病，第 8 次预防）。
// 全部 inline 读文件：不新增包级 helper —— 上一轮我在同包新增 indexOf 撞上既有定义，redeclared 编译失败。
func TestEgressScopeIsTableDriven(t *testing.T) {
	b, err := os.ReadFile("egress_scope.go")
	if err != nil {
		t.Fatalf("读不到 egress_scope.go：%v（判据失去对照面，判红而非跳过）", err)
	}
	body := string(b)
	if strings.Contains(body, "executor.KindTransport") || strings.Contains(body, "executor.KindStreamCut") {
		t.Fatal("egressScoped 里又出现直接比 Kind 常量 ⇒ 与 laneDispositions 形成两份清单，新增类别必漏一处")
	}
	if !strings.Contains(body, "laneScopeByFingerprint(kind)") {
		t.Fatal("egressScoped 不再问表 ⇒ 层面判定失去单一事实源")
	}
	for _, k := range []string{"transport_failed", "stream_cut"} {
		if !laneScopeByFingerprint(k) {
			t.Errorf("表应把 %s 标为需按正文定层面（scopeByFingerprint）", k)
		}
	}
	// 默认保守：其余类别不按正文升档（漏判只多试几条车道，误判会熔掉整池健康车道）
	for _, k := range []string{"rate_limited", "quota_exhausted", "not_served", "bad_exit", "timeout", "unknown"} {
		if laneScopeByFingerprint(k) {
			t.Errorf("%s 不该被标为按正文定层面", k)
		}
	}
}
