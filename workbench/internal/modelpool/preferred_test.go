package modelpool

import (
	"strings"
	"testing"
)

var threeLanes = []Spec{spec("a", 10, 10, StateHealthy), spec("b", 20, 10, StateHealthy), spec("c", 30, 10, StateHealthy)}

func joinChain(sch *Scheduler, specs []Spec) string {
	return strings.Join(sch.PickChain(specs, nowMs()), " ")
}

// ① 偏好命中者按偏好顺序前置，未命中者保持策略序。
func TestSchedulerPickChainPreferredOrder(t *testing.T) {
	sch := NewScheduler(StrategyPriority)
	sch.SetPreferredModels([]string{"c", "b"})
	if got := joinChain(sch, threeLanes); got != "c b a" {
		t.Fatalf("偏好 [c b] 应得 [c b a]，实际 [%s]", got)
	}
}

// ② 空/零值偏好 ⇒ 与改造前完全一致（绝不静默启用）。
func TestSchedulerPickChainEmptyPreferredUnchanged(t *testing.T) {
	for _, pref := range [][]string{nil, {}, {"  "}} {
		sch := NewScheduler(StrategyPriority)
		sch.SetPreferredModels(pref)
		if got := joinChain(sch, threeLanes); got != "a b c" {
			t.Fatalf("空偏好 %#v 应与改造前一致 [a b c]，实际 [%s]", pref, got)
		}
	}
}

// ③ 偏好组优先于 least-loaded；组内余位仍按负载升序（退路口径的取舍）。
func TestSchedulerPickChainPreferredBeatsLeastLoaded(t *testing.T) {
	sch := NewScheduler(StrategyLeastLoaded)
	sch.Acquire("a")
	sch.Acquire("a")
	sch.Acquire("b")
	sch.SetPreferredModels([]string{"b"})
	if got := joinChain(sch, threeLanes); got != "b c a" {
		t.Fatalf("偏好 [b] 应得 [b c a]，实际 [%s]", got)
	}
}

// ④ 不存在 id 不 panic；下线车道被过滤自然挡掉。
func TestSchedulerPickChainPreferredUnknownID(t *testing.T) {
	sch := NewScheduler(StrategyPriority)
	sch.SetPreferredModels([]string{"ghost", "down", "b"})
	specs := []Spec{spec("a", 10, 10, StateHealthy), spec("b", 20, 10, StateHealthy), spec("down", 1, 10, StateDown)}
	if got := joinChain(sch, specs); got != "b a" {
		t.Fatalf("不存在/下线偏好应被自然挡掉，实际 [%s]", got)
	}
}
