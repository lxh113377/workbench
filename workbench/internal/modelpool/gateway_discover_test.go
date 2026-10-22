package modelpool

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ────────────────────────────────────────────────────────────────
// 网关发现的回归判据（2026-10-20 · 老大实报）
//
// 用户可见症状：
//
//	网关错误：端口未监听: dial tcp 127.0.0.1:8326: connectex:
//	No connection could be made because the target machine actively refused it.
//
// 两个被钉死的缺陷（见 gateway_check.go 内注释）：
//  1. DiscoverGatewayVerified 的预算比较写成 time.Now().After(time.Now().Add(budget))，
//     恒假 ⇒ budget 从不生效，候选（base × key 笛卡尔积）全量试。
//  2. 同一 base 证明端口没监听后，仍为每个 api-key 重复拨号
//     （任务隔离 HOME 实测 225 份 key），把一次环境态放大成成片失败。
// ────────────────────────────────────────────────────────────────

// TestDiscoverBudgetIsHonored 预算必须真的中断循环（防回退成恒假比较）。
//
// 反假绿设计：20 个候选、每个 check 睡 70ms、预算 150ms。
// 若预算失效则至少拨 20 次 ≈ 1.4s；生效则约 2 次。用「拨号次数」而非仅测耗时，
// 避免 CI 抖动把判据变成 flakes。
func TestDiscoverBudgetIsHonored(t *testing.T) {
	var calls int32
	cands := make([]gatewayCandidate, 0, 20)
	for i := 0; i < 20; i++ {
		cands = append(cands, gatewayCandidate{base: fmt.Sprintf("http://127.0.0.1:%d/v1", 8000+i), key: "k", from: "test"})
	}
	check := func(base, key string) gatewayHealth {
		atomic.AddInt32(&calls, 1)
		time.Sleep(70 * time.Millisecond)
		// 全部「端口在监听但拒绝鉴权」：不进入 deadBases 短路，纯测预算。
		return gatewayHealth{Reachable: true, StatusCode: 401, Detail: "网关拒绝（HTTP 401）"}
	}
	started := time.Now()
	_, _ = discoverVerifiedWith(cands, 150*time.Millisecond, check)
	elapsed := time.Since(started)
	got := atomic.LoadInt32(&calls)
	if got >= 20 {
		t.Fatalf("预算未生效：拨号 %d 次 / 共 20 个候选（预算 150ms，单次 70ms）", got)
	}
	if elapsed > 900*time.Millisecond {
		t.Fatalf("预算未生效：耗时 %v（预算 150ms，单次 70ms）", elapsed)
	}
}

// TestDiscoverSkipsKeyLoopOnDeadBase 同一 base 端口没监听后，换 key 不再重复拨号。
//
// 修复前：1 个死 base × 30 份 key + 1 个可用 base ⇒ check 调 31 次。
// 修复后：死 base 只探 1 次，随后命中可用 base 即返回 ⇒ check 调 2 次。
func TestDiscoverSkipsKeyLoopOnDeadBase(t *testing.T) {
	// 可用 base 必须是真 HTTP 服务：check 通过后还要 listGatewayModels 打 /v1/models。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"our-free-model/x"}]}`)
	}))
	defer srv.Close()

	dead := "http://127.0.0.1:9/v1"
	live := srv.URL + "/v1"
	cands := []gatewayCandidate{}
	for i := 0; i < 30; i++ {
		cands = append(cands, gatewayCandidate{base: dead, key: fmt.Sprintf("k%d", i), from: "task-home"})
	}
	cands = append(cands, gatewayCandidate{base: live, key: "good", from: "env"})

	var deadCalls, liveCalls int32
	check := func(base, key string) gatewayHealth {
		if base == dead {
			atomic.AddInt32(&deadCalls, 1)
			return gatewayHealth{Detail: "本机 OFM 网关（127.0.0.1:9）未启动；已退回本地清单纳管，模型照常可调度"}
		}
		atomic.AddInt32(&liveCalls, 1)
		return gatewayHealth{Reachable: true, Authorized: true, StatusCode: 200}
	}
	gw, _ := discoverVerifiedWith(cands, 5*time.Second, check)
	if d := atomic.LoadInt32(&deadCalls); d != 1 {
		t.Fatalf("死 base 被重复拨号 %d 次（应为 1；修复前为 30）", d)
	}
	if l := atomic.LoadInt32(&liveCalls); l != 1 {
		t.Fatalf("可用 base 应只探 1 次，实际 %d", l)
	}
	if gw.BaseURL != live || gw.Key != "good" {
		t.Fatalf("应返回可用候选，实际 BaseURL=%q Key=%q", gw.BaseURL, gw.Key)
	}
	if len(gw.Models) != 1 || gw.Models[0] != "our-free-model/x" {
		t.Fatalf("应带出网关清单，实际 %v", gw.Models)
	}
}

// TestLocalGatewayDownHasActionableReason 本机网关未监听时，面板拿到的是可行动中文，
// 不再是 Go 的裸 dial 字符串（用户实报的那句）。
func TestLocalGatewayDownHasActionableReason(t *testing.T) {
	// 绑一个回环端口再关掉 ⇒ 该端口必然「主动拒绝」，且确定属于本机网关分支。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法绑定临时端口：%v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	base := fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	h := checkGateway(base, "k", 1500*time.Millisecond)
	if h.Reachable {
		t.Skipf("端口 %d 在关闭后仍可连（环境异常，跳过）", port)
	}
	if !strings.Contains(h.Detail, "未启动") {
		t.Errorf("本机网关未监听应给出可行动中文，实际 Detail=%q", h.Detail)
	}
	for _, raw := range []string{"dial tcp", "connectex", "connect: connection refused"} {
		if strings.Contains(h.Detail, raw) {
			t.Errorf("Detail 仍含 Go 裸错误 %q：%q", raw, h.Detail)
		}
	}
}

// TestDeadGatewayErrorIsNotRawDial 全部候选死掉时，Gateway.Error 也必须是可行动原因。
func TestDeadGatewayErrorIsNotRawDial(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法绑定临时端口：%v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	cands := []gatewayCandidate{{base: fmt.Sprintf("http://127.0.0.1:%d/v1", port), key: "k", from: "test"}}
	gw, h := discoverVerifiedWith(cands, 2*time.Second, func(base, key string) gatewayHealth {
		return checkGateway(base, key, 1200*time.Millisecond)
	})
	if h.Reachable {
		t.Skip("端口未被拒绝，环境异常")
	}
	if strings.Contains(gw.Error, "dial tcp") || strings.Contains(gw.Error, "connectex") {
		t.Errorf("Gateway.Error 泄漏裸 dial 字符串：%q", gw.Error)
	}
	if !strings.Contains(gw.Error, "未启动") {
		t.Errorf("Gateway.Error 应为可行动原因，实际 %q", gw.Error)
	}
}

// TestSyncNowLocalGatewayDownLogsActionableReason 端到端钉住用户可见读数：
// 8326 没监听时，model-status.jsonl 里的 gateway-fail 原因必须是可行动中文，
// 不能再是 Go 的裸 dial 字符串（老大实报的那句），且必须回退 catalog 数据面。
func TestSyncNowLocalGatewayDownLogsActionableReason(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法绑定临时端口：%v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	t.Setenv("DSH_OPENAI_GATEWAY_PORT", fmt.Sprintf("%d", port))
	t.Setenv("DSH_OPENAI_GATEWAY_API_KEY", "test-key")

	root := t.TempDir()
	store := NewStore(root)
	svc := NewService(store, nil, ServiceConfig{
		Enabled: true, ProbeSeconds: 3600, SyncSeconds: 3600,
		Strategy: StrategyPriority, DSHHome: root,
	})
	// 造一份最小 OFM 数据面，证明网关不可达时会退回 catalog 纳管。
	ofmDir := filepath.Join(root, "our-free-model")
	if err := os.MkdirAll(ofmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cat := "{ " + "\"version\":1,\"entries\":[\"m1\",\"m2\"] }"
	if err := os.WriteFile(filepath.Join(ofmDir, "catalog.json"), []byte(cat), 0o644); err != nil {
		t.Fatal(err)
	}
	svc.SyncNow("startup")

	// 断言 1：状态日志里的失败原因可行动、无裸 dial。
	lines := store.TailLog(50)
	var failLine string
	for _, l := range lines {
		if strings.Contains(l, "gateway-fail") {
			failLine = l
		}
	}
	if failLine == "" {
		t.Fatalf("应记录一次 gateway-fail，实际日志：%v", lines)
	}
	if strings.Contains(failLine, "dial tcp") || strings.Contains(failLine, "connectex") {
		t.Errorf("gateway-fail 原因仍是 Go 裸错误：%s", failLine)
	}
	if !strings.Contains(failLine, "未启动") {
		t.Errorf("gateway-fail 原因应为可行动中文：%s", failLine)
	}

	// 断言 2：回退数据面生效 —— 模型照常纳管（不因网关没起就整池空转）。
	if specs := svc.Manager().Specs(); len(specs) == 0 {
		t.Error("网关不可达时应退回 catalog 数据面纳管模型，实际池子为空")
	}
}

// TestDiscoverDoesNotProbeEveryKeyForDeadBase 生产路径等价性：
// GatewayCandidates 在本机只有 1 个 base，但任务隔离 HOME 会有很多 key。
// 用真实候选（含临时 DSH_HOME）验证 dead base 只拨一次。
func TestDiscoverDoesNotProbeEveryKeyForDeadBase(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法绑定临时端口：%v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	t.Setenv("DSH_OPENAI_GATEWAY_PORT", fmt.Sprintf("%d", port))

	root := t.TempDir()
	tasks := filepath.Join(root, "tasks")
	// 造 25 个任务隔离 HOME，各一把 api-key ⇒ 候选 25 份。
	for i := 0; i < 25; i++ {
		d := filepath.Join(tasks, fmt.Sprintf("task-%02d", i), "openai-gateway")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "api-key"), []byte(fmt.Sprintf("k%d", i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cands := GatewayCandidates(root)
	if len(cands) < 25 {
		t.Skipf("候选不足 25（实际 %d），跳过该等价性检查", len(cands))
	}

	suffix := fmt.Sprintf(":%d/v1", port)
	var dials int32
	check := func(base, key string) gatewayHealth {
		if strings.HasSuffix(base, suffix) {
			atomic.AddInt32(&dials, 1)
		}
		return gatewayHealth{Detail: "本机 OFM 网关未启动；已退回本地清单纳管，模型照常可调度"}
	}
	_, _ = discoverVerifiedWith(cands, 5*time.Second, check)
	if got := atomic.LoadInt32(&dials); got != 1 {
		t.Fatalf("死 base 应只拨 1 次，实际 %d 次（%d 份 key 被逐个重试）", got, len(cands))
	}
}
