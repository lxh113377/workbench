package retry

import (
	"testing"
)

// 运行期状态的归属判据（2026-10-22 新增）。
//
// 背景：Policy 原先把 warnMu sync.Mutex 内嵌在自身，而 Policy 在本仓是按值传递的
// （engine/helpers.go 构造字面量后交给 New）。于是「复制一份策略」同时复制了
// 一份告警表 —— Warnings() 读数随副本而异；go vet copylocks 也判红
// （retry.go:138 New / retry.go:179 赋值 / engine/helpers.go:37 调用 三处）。
// 改法：运行期状态收进 runtime *policyRuntime（指针），配置字段仍可按值传。
//
// 为什么必须有 Go 层判据：vet 只拦得住「结构体里再有锁」，拦不住
// 「把 runtime 改回值字段」或「New 里复用入参 runtime」这两类回退 ——
// 两种改法 vet 都是绿的，但告警表会重新分叉，读数又变得不可信。

// TestNewDoesNotInheritRuntimeState 钉死构造器语义：New 产出的是**新**策略，
// 不得携带入参策略的运行期告警。
//
// 旧实现（cp := p 连 warnings 切片一起复制）在这条上判红。
func TestNewDoesNotInheritRuntimeState(t *testing.T) {
	base := New(Policy{MaxRetries: 2, BackoffMs: []int{0}, Jitter: 0})
	_ = base.nextDelay(0) // 列表项非正 -> 登记告警
	if len(base.Warnings()) == 0 {
		t.Fatal("前置条件不成立：非法退避列表应登记告警")
	}
	fresh := New(*base)
	if got := fresh.Warnings(); len(got) != 0 {
		t.Fatalf("New 必须产出干净策略，却带上了入参的告警 %v（运行期状态被复用）", got)
	}
	if len(base.Warnings()) == 0 {
		t.Fatal("构造新策略不得清空原策略的告警")
	}
}

// TestValueCopySharesRuntimeState 钉死「按值复制策略不复制出第二份告警表」：
// cp := *p 之后在 cp 上登记的告警，p 必须看得到 —— 运行期状态只有一份。
//
// 与上一条不矛盾：New 是构造器（产出新对象、状态重建），按值复制是同一对象
// 的另一个视图（状态共享）。两条一起把「何时该分、何时该共享」钉死。
func TestValueCopySharesRuntimeState(t *testing.T) {
	p := New(Policy{MaxRetries: 2, BackoffMs: []int{0}, Jitter: 0})
	if len(p.Warnings()) != 0 {
		t.Fatal("前置条件不成立：干净策略不应有告警")
	}
	cp := *p
	_ = cp.nextDelay(0) // 在副本上触发告警
	if len(p.Warnings()) == 0 {
		t.Fatal("副本上登记的告警必须对原策略可见（按值复制不应复制出第二份告警表）")
	}
}

// TestZeroValuePolicyDoesNotPanic 覆盖 rt() 的兜底分支：
// 未经 New 构造的零值策略（外部直接写字面量 retry.Policy{...}）仍不得 panic。
//
// 只断言「不崩 + 返回值可用」，不断言具体延迟值：零值策略的 BaseDelay/MaxDelay
// 没经过 New 的默认值补齐，延迟语义不在本测试的判据范围内。
func TestZeroValuePolicyDoesNotPanic(t *testing.T) {
	p := &Policy{MaxRetries: 1, Jitter: 1}
	if got := p.Decide(Timeout, 0, ""); got.Action != ActionRetry {
		t.Fatalf("零值策略应仍能给出决策，got %s", got.Action)
	}
	if d := p.nextDelay(0); d <= 0 {
		t.Fatalf("零值策略算出的等待 %v 非正（热循环配方）", d)
	}
	if p.Warnings() == nil {
		t.Fatal("Warnings() 应返回非 nil 切片（便于调用方直接 range）")
	}
}
