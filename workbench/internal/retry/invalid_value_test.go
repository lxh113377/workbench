package retry

import (
	"strings"
	"testing"
	"time"
)

// 策略非法值兜底（2026-10-22 对标 river job_executor.go:563-571）。
//
// river 的做法：策略返回「早于当前时间」的非法值时 WarnContext 并回落到默认策略。
// 本项目把同一语义拆成两层：
//   构造期 —— New() 静默修正矛盾配置并把原因登记进 Fixes（可观测）;
//   运行期 —— nextDelay() 算出非正等待时回落默认指数退避并登记 Warnings。
// 两层都必须在：只修不留痕等于没修（运维看不出配置写坏了），
// 只留痕不修正等于让系统按坏值跑（热循环）。

// TestNewRecordsMaxDelayBelowBaseDelay 验证跨字段矛盾被登记且被修正。
func TestNewRecordsMaxDelayBelowBaseDelay(t *testing.T) {
	p := New(Policy{MaxRetries: 3, BaseDelay: 10 * time.Second, MaxDelay: 2 * time.Second, Jitter: 0})
	if p.MaxDelay < p.BaseDelay {
		t.Fatalf("MaxDelay(%v) 仍小于 BaseDelay(%v)：跨字段矛盾未被修正", p.MaxDelay, p.BaseDelay)
	}
	if len(p.Fixes) == 0 {
		t.Fatal("矛盾配置必须登记 Fixes，否则运维看不出配置写坏了")
	}
	var found bool
	for _, f := range p.Fixes {
		if f.Field == "MaxDelayMs" {
			found = true
			if !strings.Contains(f.Reason, "小于基准") {
				t.Fatalf("修正原因应说明矛盾方向，got %q", f.Reason)
			}
		}
	}
	if !found {
		t.Fatalf("Fixes 应含 MaxDelayMs 项，got %+v", p.Fixes)
	}
}

// TestNewRecordsInvalidBackoffListItems 验证退避列表里的非正数被登记。
func TestNewRecordsInvalidBackoffListItems(t *testing.T) {
	p := New(Policy{MaxRetries: 3, BackoffMs: []int{5000, 0, -1}, Jitter: 0})
	var idx0, idx2 bool
	for _, f := range p.Fixes {
		switch f.Field {
		case "RetryBackoffMs[1]":
			idx0 = true
		case "RetryBackoffMs[2]":
			idx2 = true
		}
	}
	if !idx0 || !idx2 {
		t.Fatalf("退避列表第 1/2 项的非法值必须登记，got %+v", p.Fixes)
	}
}

// TestNewRecordsJitterOutOfRange 验证抖动比例越界被登记。
func TestNewRecordsJitterOutOfRange(t *testing.T) {
	for _, tc := range []struct{ in, want float64 }{
		{-0.5, 0},
		{1.5, 1},
	} {
		p := New(Policy{MaxRetries: 1, BaseDelay: time.Second, Jitter: tc.in})
		if p.Jitter != tc.want {
			t.Fatalf("Jitter %v 应被钳到 %v，got %v", tc.in, tc.want, p.Jitter)
		}
		var found bool
		for _, f := range p.Fixes {
			if f.Field == "Jitter" {
				found = true
			}
		}
		if !found {
			t.Fatalf("Jitter 越界必须登记，got %+v", p.Fixes)
		}
	}
}

// TestNextDelayNeverCollapsesOnInvalidList 是运行期兜底的核心契约：
// 退避列表被写坏（全 0/负）时，等待**不得**塌到 1ms（那是热循环配方），
// 必须回落到可信的默认指数退避，并登记 Warnings。
func TestNextDelayNeverCollapsesOnInvalidList(t *testing.T) {
	p := New(Policy{MaxRetries: 5, BackoffMs: []int{0, -1, 0}, Jitter: 0})
	for attempt := 0; attempt < 3; attempt++ {
		d := p.nextDelay(attempt)
		if d < time.Second {
			t.Fatalf("第 %d 次重试等待 %v：非法列表下塌到了秒级以下（热循环风险）", attempt, d)
		}
	}
	warns := p.Warnings()
	if len(warns) == 0 {
		t.Fatal("运行期兜底必须登记 Warnings，否则修正不可观测")
	}
}

// TestNextDelayJitterCollapseFallsBackToBaseDelay 验证抖动塌陷的兜底。
//
// Jitter=1 时 spread 等于 base，抖动公式 base-spread+rand*spread 的取值域是 [0, base)，
// rand 取到接近 0 时结果会塌到 0 或负数。旧实现在这里只钳到 1ms（热循环配方），
// 现在必须回落到 BaseDelay 并登记告警。
func TestNextDelayJitterCollapseFallsBackToBaseDelay(t *testing.T) {
	p := New(Policy{MaxRetries: 5, BackoffMs: []int{5000}, BaseDelay: 5 * time.Second, Jitter: 1})
	var sawFallback bool
	for i := 0; i < 3000; i++ {
		d := p.nextDelay(0)
		// 合法抖动的下界是 base*(1-Jitter) = 0，但兜底阈值取 base/2：
		// 低于半个基准周期的等待就是热循环配方，必须被兜住。
		if d < 2500*time.Millisecond {
			t.Fatalf("抖动塌陷后等待 %v 低于基准 5s 的一半：仍会退化成热循环", d)
		}
	}
	for _, w := range p.Warnings() {
		if strings.Contains(w, "抖动后") {
			sawFallback = true
		}
	}
	if !sawFallback {
		t.Fatal("抖动塌陷兜底必须登记 Warnings")
	}
}
// TestWarningsDeduplicated 验证同一条告警不重复累计（nextDelay 被高频调用）。
func TestWarningsDeduplicated(t *testing.T) {
	p := New(Policy{MaxRetries: 5, BackoffMs: []int{0, 0, 0}, Jitter: 0})
	for i := 0; i < 50; i++ {
		_ = p.nextDelay(0)
	}
	if n := len(p.Warnings()); n != 1 {
		t.Fatalf("同一告警应去重成 1 条，got %d 条: %v", n, p.Warnings())
	}
}

// TestWarningsIsCopy 验证返回值是副本（调用方改不动内部状态）。
func TestWarningsIsCopy(t *testing.T) {
	p := New(Policy{MaxRetries: 3, BackoffMs: []int{0}, Jitter: 0})
	_ = p.nextDelay(0)
	got := p.Warnings()
	if len(got) > 0 {
		got[0] = "MUTATED"
	}
	again := p.Warnings()
	for _, w := range again {
		if w == "MUTATED" {
			t.Fatal("Warnings 必须返回副本，内部状态被外部改动了")
		}
	}
}

// TestCleanConfigProducesNoFixesOrWarnings 是**反向判据**：
// 合法配置不得产生任何 Fixes/Warnings，否则「配置修正」会变成噪音，
// 真正需要关注的修正反被淹没。
func TestCleanConfigProducesNoFixesOrWarnings(t *testing.T) {
	p := New(Policy{MaxRetries: 3, BackoffMs: []int{5000, 20000, 60000}, Jitter: 0.2})
	if len(p.Fixes) != 0 {
		t.Fatalf("合法配置不应有 Fixes，got %+v", p.Fixes)
	}
	for attempt := 0; attempt < 5; attempt++ {
		_ = p.nextDelay(attempt)
	}
	if len(p.Warnings()) != 0 {
		t.Fatalf("合法配置不应有 Warnings，got %v", p.Warnings())
	}
}

// TestDecideStillWorksWithInvalidList 验证非法列表不影响三分决策语义
// （兜底只改延迟值，不改 action）。
func TestDecideStillWorksWithInvalidList(t *testing.T) {
	p := New(Policy{MaxRetries: 2, BackoffMs: []int{0, 0}, Jitter: 0})
	if got := p.Decide(Timeout, 0, ""); got.Action != ActionRetry {
		t.Fatalf("action = %s, want retry", got.Action)
	}
	if got := p.Decide(Timeout, 2, ""); got.Action != ActionFail {
		t.Fatalf("次数耗尽应 fail，got %s", got.Action)
	}
	if got := p.Decide(MissingKey, 0, ""); got.Action != ActionFail {
		t.Fatalf("不可重试类别应 fail，got %s", got.Action)
	}
}
