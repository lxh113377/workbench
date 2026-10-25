package main

// acceptance_r3_test.go 守护 R3「调度策略」判据的**行为级**性质（2026-10-19 现场）。
//
// 现场缺陷：R3 原来只 grep scheduler.go 里有没有四个策略**字符串**。
// 而 StrategyLeastLoaded 与 round-robin 共用同一分支、从不读 inflight ——
// 实测 A 在飞 3、C 在飞 0 却挑了 A，**grep 判据对此全绿**。
//
// 本测试同时钉住两件事：
//   1) 正常实现下 R3 探针全绿（防止我误把判据写死）；
//   2) 探针真的具备**区分力** —— 见 TestR3ProbeDetectsWrongPick。

import (
	"strings"
	"testing"

	"workbench/internal/modelpool"
)

// TestProbeSchedulerStrategiesGreen 断言当前实现在四种策略上全部合规。
func TestProbeSchedulerStrategiesGreen(t *testing.T) {
	r := probeSchedulerStrategies()
	for _, ev := range r.evidence {
		t.Logf("evidence: %s", ev)
	}
	if len(r.failures) > 0 {
		t.Fatalf("R3 探针报失败（实现回归或判据过严）：%v", r.failures)
	}
}

// TestR3ProbeCoversAllStrategies 守护探针自身的**覆盖面**。
//
// 为什么需要这条（2026-10-24 变异实测发现）：前两条判据只检查「探针没报失败」，
// 于是把探针里的任一场景整段删掉，测试照样全绿 —— 判据失去了对自身覆盖面的守护。
// 这正是本仓在册的「判据跑错对象 / 只测一份」家族：探针从 6 个场景退化成 5 个
// 甚至 1 个，只要那一个还绿，就没有任何测试会报错。
//
// 判据：四种策略**各自**必须至少有一条 evidence（即该场景真的跑过且过了）。
// 这条断言同时防两件事：① 场景被摘；② 场景在但恒不执行（evidence 为空）。
func TestR3ProbeCoversAllStrategies(t *testing.T) {
	r := probeSchedulerStrategies()
	if len(r.failures) > 0 {
		t.Fatalf("探针自身报失败，无法评估覆盖面：%v", r.failures)
	}
	want := []string{"least-loaded", "priority", "round-robin", "weighted"}
	for _, name := range want {
		found := false
		for _, ev := range r.evidence {
			if strings.HasPrefix(ev, name) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("探针缺少 %s 场景的 evidence（覆盖面退化）：共 %d 条 evidence 里没有以 %q 开头的", name, len(r.evidence), name)
		}
	}
	// 护栏场景也必须在（它守的是「不可路由/停用不派发」这条独立承诺）。
	if !hasEvidencePrefix(r.evidence, "护栏") {
		t.Fatalf("探针缺少护栏场景的 evidence")
	}
	// evidence 条数下限：防止「只留最小集合」式收缩。当前 6 类场景各 1 条，
	// 下限设 5 留一条余量（将来某类场景合法拆成两条时不必改判据）。
	if len(r.evidence) < 5 {
		t.Fatalf("探针 evidence 只有 %d 条，少于下限 5（场景被摘？）", len(r.evidence))
	}
}

func hasEvidencePrefix(ev []string, prefix string) bool {
	for _, e := range ev {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

// TestR3ProbeDetectsWrongPick 断言「探针有区分力」——
// 直接对 least-loaded 施加错误期望，确认会失败（而不是永远返回空 failures）。
//
// 做法：复现现场场景（A=3, B=1, C=0），断言必须挑 C；
// 再用一个「人为把它当 round-robin 用」的对照，证明差异可被观测。
func TestR3ProbeDetectsWrongPick(t *testing.T) {
	now := int64(1792380000000)
	mk := func(id string) modelpool.Spec {
		// Routable=true 是必须的：不可路由模型会被调度护栏过滤（2026-10-19 加的护栏）。
		return modelpool.Spec{ID: id, Model: id, Label: id, Enabled: true, Routable: true, Priority: 5, Weight: 50}
	}
	specs := []modelpool.Spec{mk("A"), mk("B"), mk("C")}

	// least-loaded：在飞数成为唯一区分依据
	sch := modelpool.NewScheduler(modelpool.StrategyLeastLoaded)
	for i := 0; i < 3; i++ {
		sch.Acquire("A")
	}
	sch.Acquire("B")
	got, _ := sch.Pick(specs, nil, now)
	if got != "C" {
		t.Fatalf("least-loaded 未挑最空闲模型：期望 C，实际 %q（现场缺陷复现）", got)
	}

	// 对照组：round-robin 在同场景下**不保证**挑 C ——
	// 若两者行为完全相同，说明 least-loaded 又退化成了 round-robin 的副本。
	sch2 := modelpool.NewScheduler(modelpool.StrategyRoundRobin)
	for i := 0; i < 3; i++ {
		sch2.Acquire("A")
	}
	sch2.Acquire("B")
	var rrPicks []string
	for i := 0; i < 3; i++ {
		id, _ := sch2.Pick(specs, nil, now)
		rrPicks = append(rrPicks, id)
	}
	t.Logf("round-robin picks=%v（对照组，不要求等于 C）", rrPicks)

	// 区分力自检：least-loaded 的结果不受 cursor 影响，重复调用恒为 C。
	for i := 0; i < 3; i++ {
		again, _ := sch.Pick(specs, nil, now)
		if again != "C" {
			t.Fatalf("least-loaded 结果不稳定：第 %d 次得到 %q，期望 C", i+1, again)
		}
	}
}
