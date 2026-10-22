// Command schedprobe 对四种调度策略做**真实行为**验证，并输出 JSON 供总验收 R3 判读。
//
// 为什么需要（2026-10-19 现场）：R3 原来只是 grep scheduler.go 里有没有
// StrategyPriority/RoundRobin/Weighted/LeastLoaded 这四个**字符串**。
// 那是「源码文本存在性」判定，不是「调度行为正确性」判定 ——
// 策略被写死成永远返回第一个模型，grep 依然全绿。
// 这与 -H=windowsgui 那次的教训同源：**能在产物/行为上验的，就不要在源码文本上验。**
//
// 退出码: 0 = 四种策略行为全部符合语义; 1 = 有策略行为不符
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"workbench/internal/modelpool"
)

type specIn struct {
	ID       string `json:"id"`
	Priority int    `json:"priority"`
	Weight   int    `json:"weight"`
}

// toSpecs 构造候选：注意 ID 才是调度器认的键（Model 是上游模型名）。
func toSpecs(in []specIn) []modelpool.Spec {
	out := make([]modelpool.Spec, 0, len(in))
	for _, s := range in {
		out = append(out, modelpool.Spec{
			ID: s.ID, Model: s.ID, Label: s.ID, Enabled: true,
			// Routable 必须为真：不可路由模型会被调度护栏过滤（2026-10-19 加的护栏）。
			// 少了这个字段，探针只会得到空结果 —— 那不是「护栏坏了」，是探针构造不合规。
			Routable: true,
			Priority: s.Priority, Weight: s.Weight,
		})
	}
	return out
}

func main() {
	now := time.Now().UnixMilli()
	base := []specIn{{"A", 1, 10}, {"B", 5, 50}, {"C", 9, 90}}
	result := map[string]any{}
	var failed []string

	// 1) priority：priority 最小者应被持续选中
	{
		sch := modelpool.NewScheduler(modelpool.StrategyPriority)
		picked := map[string]int{}
		for i := 0; i < 6; i++ {
			id, _ := sch.Pick(toSpecs(base), nil, now)
			picked[id]++
		}
		result["priority"] = picked
		if picked["A"] != 6 {
			failed = append(failed, fmt.Sprintf("priority 语义不符：期望 A x6，实际 %v", picked))
		}
	}

	// 2) round-robin：多次挑选必须覆盖 >=2 个模型
	{
		sch := modelpool.NewScheduler(modelpool.StrategyRoundRobin)
		picked := map[string]int{}
		for i := 0; i < 30; i++ {
			id, _ := sch.Pick(toSpecs(base), nil, now)
			picked[id]++
		}
		result["roundRobin"] = picked
		if len(picked) < 2 {
			failed = append(failed, fmt.Sprintf("round-robin 未轮转：%v", picked))
		}
	}

	// 3) weighted：高权重的 C 应显著多于低权重的 A
	{
		sch := modelpool.NewScheduler(modelpool.StrategyWeighted)
		picked := map[string]int{}
		for i := 0; i < 300; i++ {
			id, _ := sch.Pick(toSpecs(base), nil, now)
			picked[id]++
		}
		result["weighted"] = picked
		if picked["C"] <= picked["A"] {
			failed = append(failed, fmt.Sprintf("weighted 未体现权重：C=%d A=%d", picked["C"], picked["A"]))
		}
	}

	// 4) least-loaded（核心判据）：同优先级同权重，唯一差别是在飞数。
	//    A=3、B=1、C=0 => 必须挑 C。
	{
		sch := modelpool.NewScheduler(modelpool.StrategyLeastLoaded)
		eq := []specIn{{"A", 5, 50}, {"B", 5, 50}, {"C", 5, 50}}
		for i := 0; i < 3; i++ {
			sch.Acquire("A")
		}
		sch.Acquire("B")
		id, _ := sch.Pick(toSpecs(eq), nil, now)
		result["leastLoaded"] = map[string]any{"inflightA": 3, "inflightB": 1, "inflightC": 0, "picked": id}
		if id != "C" {
			failed = append(failed, fmt.Sprintf("least-loaded 未按在飞数选择：期望 C，实际 %q", id))
		}
	}

	// 5) 未知策略必须报错（不得静默降级）
	{
		sch := modelpool.NewScheduler(modelpool.StrategyPriority)
		err := sch.SetStrategy(modelpool.Strategy("no-such-strategy"))
		result["unknownRejected"] = err != nil
		if err == nil {
			failed = append(failed, "未知策略未被拒绝（静默降级）")
		}
	}

	result["ok"] = len(failed) == 0
	result["failures"] = failed
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
	if len(failed) > 0 {
		os.Exit(1)
	}
}
