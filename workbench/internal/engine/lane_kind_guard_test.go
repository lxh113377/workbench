package engine

import (
	"os"
	"strings"
	"testing"
)

// TestLaneCooldownKindNotHardcoded 钉住「车道冷却记账的 errKind 必须来自本轮分类」（round 125 · T78 第三处漏项）。
//
// 现场：429 分支把 CooldownByLane 的 errKind 硬编码成 rate_limited ⇒ 额度耗尽写进池账本的 lastErr 前缀
// 永远是 rate_limited: ⇒ 一切按 quota_exhausted: 前缀做的判定（Router.CountCoolingByKind、
// quotaLanesCooling）恒为 false，分级等待预算的第二级补漏**静默落空**（实测判据是「日志 0 条」而非报错）。
//
// 断言形态（两次自我纠正后定稿）：
//
//	· 记账调用的 errKind 实参必须是变量 laneKind，不得再写死常量；
//	· 必须存在「本轮 ErrorKind 为 quota_exhausted 时改写 laneKind」这条分支（三层链路里唯一区分两者的地方）；
//	· 为什么钉源文本：写死与传对在单测里行为差异只体现在「账本里存了什么字符串」，
//	  跑通 429→冷却→pickErr→预算整链要真执行器与池闸门时序，而回归点恰恰就是这一行实参。
func TestLaneCooldownKindNotHardcoded(t *testing.T) {
	src, err := os.ReadFile("engine.go")
	if err != nil {
		t.Fatalf("读 engine.go 失败: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "laneKind, \"429 rate limited\")") {
		t.Fatal("车道冷却的 errKind 又被写死：额度耗尽将无法在账本里被识别，分级等待预算静默失效")
	}
	if !strings.Contains(body, "laneKind = string(executor.KindQuotaExhausted)") {
		t.Fatal("缺少「本轮是额度耗尽则改写 laneKind」的分支：账本前缀会永远停在 rate_limited")
	}
	// 台账写入点同样不得写死（round 126 · T81 第一块砖）：此前限流路径的 AppendAttempt
	// 把 Kind 写死成 rate_limited，于是额度耗尽的尝试在台账里全被记成限流 ⇒
	// 面板「免费额度耗尽」这一类恒为 0，按 kind 做的统计/告警全部失真。
	// 普查结论（7 处台账 Kind 实参）：plan/done 是旁路名单里的台账种类、
	// cancelKind/kind/waitKind 都是变量 —— 字面量写死只有这一处，已修。
	if !strings.Contains(body, "Model: actualModel, Kind: waitKind,") {
		t.Fatal("限流台账的 Kind 又被写死：面板按 kind 的统计会失真（额度耗尽恒为 0）")
	}
	if strings.Contains(body, "Model: actualModel, Kind: string(executor.KindRateLimited),") {
		t.Fatal("限流台账仍在写死 rate_limited（应与分级预算共用同一个 waitKind）")
	}
	// 记账调用不得同时携带写死的 rate_limited 实参
	i := strings.Index(body, "e.router.CooldownByLane(lane,")
	if i < 0 {
		t.Fatal("锚点缺失（CooldownByLane 调用点被改名或删除？）")
	}
	arg := body[i : i+260]
	if strings.Contains(arg, "string(executor.KindRateLimited),") {
		t.Fatal("CooldownByLane 的 errKind 实参仍是硬编码常量")
	}
}
