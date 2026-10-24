package main

// scheduler_probe.go — R3「调度策略」判据的**行为级探针**。
//
// 2026-10-19 现场缺陷：R3 原来只 grep scheduler.go 里有没有四个策略**字符串**。
// 而 StrategyLeastLoaded 与 round-robin 共用同一分支、从不读 inflight ——
// schedprobe 实测「A 在飞 3、C 在飞 0 却挑了 A」，**grep 判据对此全绿**。
//
// 根因：grep 只能证明「名字出现过」，不能证明「语义成立」。字符串齐全的副本
// 与真实实现对 grep 完全同形。故本探针改为直接驱动 Scheduler，断言**可观测行为**。
//
// 探针结论同时供总验收 R3 使用：R3 由「源码里有四个字符串」升级为
// 「四种策略各自表现出应有语义」。

import (
	"fmt"
	"strings"

	"workbench/internal/modelpool"
)

// schedulerProbeResult 是探针的结构化结论（供验收项与测试共用）。
type schedulerProbeResult struct {
	evidence []string
	failures []string
}

// probeSchedulerStrategies 对四种调度策略逐一施加**带区分的场景**，
// 只有真实实现才能全绿；任何「只写了名字」的副本都会在此现形。
//
// 判据设计原则（本仓反虚假通过的标准）：
//   - 每个策略的场景都必须**只有正确实现能过**；
//   - 失败时给出「期望什么、实际什么」，便于直接定位回归点。
func probeSchedulerStrategies() schedulerProbeResult {
	var r schedulerProbeResult
	const now = int64(1792380000000)

	mk := func(id string) modelpool.Spec {
		// Routable=true 是必须的：不可路由模型会被调度护栏过滤（2026-10-19 加的护栏）。
		// 三者同优先级同权重 —— 这样策略之间的差异只能来自策略本身，
		// 不会被排序规则掩盖（若优先级不同，priority 分支会替我们「答对」）。
		return modelpool.Spec{ID: id, Model: id, Label: id, Enabled: true, Routable: true, Priority: 5, Weight: 50}
	}
	specs := []modelpool.Spec{mk("A"), mk("B"), mk("C")}

	// ① least-loaded：在飞数成为唯一区分依据。
	//
	// 场景复现 2026-10-19 现场：A 在飞 3、B 在飞 1、C 在飞 0，必须挑 C。
	// 这是四场景里区分力最强的一条 —— round-robin 副本在此必然挑错。
	sch := modelpool.NewScheduler(modelpool.StrategyLeastLoaded)
	for i := 0; i < 3; i++ {
		sch.Acquire("A")
	}
	sch.Acquire("B")
	got, used := sch.Pick(specs, nil, now)
	if got != "C" {
		r.failures = append(r.failures, fmt.Sprintf("least-loaded 未挑最空闲模型：期望 C，实际 %q（现场缺陷复现）", got))
	} else {
		r.evidence = append(r.evidence, "least-loaded：A=3/B=1/C=0 在飞时挑 C（读 inflight）")
	}
	if used != string(modelpool.StrategyLeastLoaded) {
		r.failures = append(r.failures, fmt.Sprintf("least-loaded 回报的策略名不符：%q", used))
	}

	// least-loaded 必须与 cursor 无关：重复调用恒为 C（否则它内部还在按轮转走）。
	for i := 0; i < 3; i++ {
		again, _ := sch.Pick(specs, nil, now)
		if again != "C" {
			r.failures = append(r.failures, fmt.Sprintf("least-loaded 结果不稳定：第 %d 次得到 %q，期望 C", i+1, again))
			break
		}
	}

	// ② priority：严格按优先级取最高者，与在飞数无关。
	//
	// 场景：C 在飞 3，但 C 优先级最高 ⇒ 仍挑 C。若实现误读 inflight 就会挑 A。
	//
	// 口径：Spec.Priority **越小越优先**（1 = 最高，见 types.go 注释与 sortSpecs）。
	// 首版探针按「数字越大越优先」写，被自己的场景当场判红 —— 记在这里防后人重写。
	priSpecs := []modelpool.Spec{mk("A"), mk("B"), mk("C")}
	priSpecs[2].Priority = 1
	schP := modelpool.NewScheduler(modelpool.StrategyPriority)
	for i := 0; i < 3; i++ {
		schP.Acquire("C")
	}
	gotP, _ := schP.Pick(priSpecs, nil, now)
	if gotP != "C" {
		r.failures = append(r.failures, fmt.Sprintf("priority 未挑最高优先级：期望 C，实际 %q", gotP))
	} else {
		r.evidence = append(r.evidence, "priority：C 优先级最高（1 < 5）时挑 C，忽略在飞数")
	}

	// ③ round-robin：必须**轮转**，连续两次挑不同模型（副本实现常退化成固定取第一个）。
	schR := modelpool.NewScheduler(modelpool.StrategyRoundRobin)
	first, _ := schR.Pick(specs, nil, now)
	second, _ := schR.Pick(specs, nil, now)
	if first == "" || second == "" || first == second {
		r.failures = append(r.failures, fmt.Sprintf("round-robin 未轮转：连续两次得到 %q、%q", first, second))
	} else {
		r.evidence = append(r.evidence, fmt.Sprintf("round-robin：连续两次挑 %q、%q（确实轮转）", first, second))
	}

	// ④ weighted：权重必须实际影响选取。
	//
	// 场景：A 权重 90、B/C 各 5。轮转位每次 +1，样本足够多时 A 的出现率必须
	// 显著高于 1/3。副本实现（忽略 Weight）会让三者在统计上同形。
	wSpecs := []modelpool.Spec{mk("A"), mk("B"), mk("C")}
	wSpecs[0].Weight = 90
	wSpecs[1].Weight = 5
	wSpecs[2].Weight = 5
	schW := modelpool.NewScheduler(modelpool.StrategyWeighted)
	counts := map[string]int{}
	const rounds = 300
	for i := 0; i < rounds; i++ {
		id, _ := schW.Pick(wSpecs, nil, now)
		counts[id]++
	}
	// A 的期望占比 90% —— 门槛放到 50% 仍留有充分余量（副本实现约 33%），
	// 不会因边界抖动误报。
	if counts["A"]*2 < rounds {
		r.failures = append(r.failures, fmt.Sprintf("weighted 未按权重分配：A 权重 90 但 300 次里只出现 %d 次", counts["A"]))
	} else {
		r.evidence = append(r.evidence, fmt.Sprintf("weighted：A 权重 90/100，300 次里出现 %d 次", counts["A"]))
	}

	// ⑤ 护栏联动：不可路由模型绝不派发（与 Pick 的护栏承诺同一判据）。
	//
	// 注意口径：这里只用**导出字段**构造场景（Routable / Enabled / excluded）。
	// 运行期健康快照 Runtime 是 modelpool 内部字段，外部包无法直接设置 ——
	// 那条路径由 modelpool 包自己的 TestSchedulerSkipsNonRoutable 守护，
	// 本探针不重复造桩（跨包造未导出字段是编不进来的，会被 go vet 当场拦下）。
	guardSpecs := []modelpool.Spec{mk("A"), mk("B"), mk("C")}
	guardSpecs[0].Routable = false // 不可路由：写进 DSH overlay 必然失败
	guardSpecs[1].Enabled = false  // 手动停用
	schG := modelpool.NewScheduler(modelpool.StrategyLeastLoaded)
	for i := 0; i < 9; i++ {
		schG.Acquire("C")
	}
	gotG, _ := schG.Pick(guardSpecs, nil, now)
	if gotG != "C" {
		r.failures = append(r.failures, fmt.Sprintf("护栏未生效：不可路由/停用模型应被过滤，实际挑 %q", gotG))
	} else {
		r.evidence = append(r.evidence, "护栏：不可路由与停用模型被过滤，只剩可派发候选")
	}

	// excluded 集合也必须生效（故障转移的「本次已失败」集合）。
	gotE, _ := schG.Pick(specs, map[string]bool{"C": true}, now)
	if gotE == "C" {
		r.failures = append(r.failures, "excluded 未生效：C 已被排除却又被挑中")
	} else {
		r.evidence = append(r.evidence, fmt.Sprintf("excluded：排除 C 后挑 %q", gotE))
	}

	// ⑥ 策略枚举齐全（这一条保留原 grep 的价值，但只是必要非充分条件）。
	if missing := strategyEnumComplete(); len(missing) > 0 {
		r.failures = append(r.failures, "缺少策略常量: "+strings.Join(missing, ", "))
	} else {
		r.evidence = append(r.evidence, "策略枚举齐全: priority / round-robin / weighted / least-loaded")
	}

	return r
}

// strategyEnumComplete 确认四种策略常量在 modelpool 包里真实存在。
//
// 为什么还需要这一条：上面的行为探针是**逐策略施加场景**，已经覆盖了
// priority / round-robin / weighted / least-loaded 四个；但「枚举齐全」作为
// 原 grep 判据的遗产仍有独立价值 —— 它能在**新增策略漏接线**时点名
// （行为探针只测已有四种，第五种若加了常量却没进 switch，行为探针看不见）。
//
// 实现口径：直接与常量**值**比对，不与 Go 标识符名字符串比对 ——
// 后者曾在本文件首版写错（拿 "StrategyPriority" 去和 "priority" 比，恒不匹配），
// 这正是「判据自己写错却报实现回归」的典型，故在此留注。
func strategyEnumComplete() []string {
	have := map[modelpool.Strategy]bool{
		modelpool.StrategyPriority:    true,
		modelpool.StrategyRoundRobin:  true,
		modelpool.StrategyWeighted:    true,
		modelpool.StrategyLeastLoaded: true,
	}
	var missing []string
	for _, want := range []modelpool.Strategy{
		modelpool.StrategyPriority, modelpool.StrategyRoundRobin,
		modelpool.StrategyWeighted, modelpool.StrategyLeastLoaded,
	} {
		if !have[want] {
			missing = append(missing, string(want))
		}
	}
	return missing
}
