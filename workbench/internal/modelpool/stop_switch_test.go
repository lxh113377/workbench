package modelpool

import (
	"os"
	"strings"
	"testing"
)

// TestStopSwitchSet 钉住「哪些失败类别换模型无意义」（round 131 · T84）。
//
// 现场（2026-10-30 实测，agent 根）：quota_exhausted 的「尝试结束」一批 34 次 ——
// 那是同一次任务在池内轮转的结果。failover.go 早就有「限流时停止换模型」的拦截，
// 因为它的注释记录过一次事故：连试 8 个模型、24 秒打空整池、限流窗口拖到 50+ 分钟；
// 但那道拦截当时写的是裸字符串 kind == rate_limited，新增 quota_exhausted 时没人回来改，
// 于是同一个失效模式在新类别上原样复发（本会话同族第 7 次：同一契约多处实现，只改了一处）。
func TestStopSwitchSet(t *testing.T) {
	want := map[string]bool{
		"rate_limited":    true,
		"quota_exhausted": true,
	}
	for k, v := range want {
		if got := laneStopSwitch(k); got != v {
			t.Errorf("laneStopSwitch(%s) = %v，应为 %v", k, got, v)
		}
	}
	// 反向：能力级/结构性失败必须继续换模型（换一条车道正是正确处置，拦住会饿死可用车道）
	for _, k := range []string{"not_served", "no_artifact", "transport_failed", "stream_cut", "bad_exit", "timeout", "stalled", "boot_failed", "model_unavailable", "missing_credential"} {
		if laneStopSwitch(k) {
			t.Errorf("laneStopSwitch(%s) 应为 false", k)
		}
	}
	if laneStopSwitch("bogus_kind") {
		t.Error("未知类别不该被当成限流族")
	}
}

// TestFailoverUsesTableNotLiteral 是源文本守卫：停止切换判据必须走表，
// 不许再出现「拿 kind 与常量硬比」的形态（否则第 8 次同族事故仍会复发）。
func TestFailoverUsesTableNotLiteral(t *testing.T) {
	src, err := os.ReadFile("failover.go")
	if err != nil {
		t.Fatalf("读不到 failover.go：%v（判据失去对照面，判红而非跳过）", err)
	}
	body := string(src)
	if !strings.Contains(body, "if laneStopSwitch(kind) {") {
		t.Fatal("failover 的停止切换判据又不走表了，新增限流族类别时会漏改这里")
	}
	if strings.Contains(body, "if kind == string(executor.KindRateLimited) {") {
		t.Fatal("failover 里又出现把 rate_limited 当唯一停止条件的硬编码，本轮修掉的正是它")
	}
}
