package main

// soak_verdict.go — 长时压测（A7 / --soak）判据的**唯一实现**。
//
// 2026-10-19 现场缺陷（两份判据各自实现、只修一处即漂移）：
//   报表同时输出 `longEnough: false` 与 `verdict: leak / exitCode: 2` ——
//   「自己声明证据不足，却给出已证泄漏的结论」。
//   根因有两条：① 只把 ok 在短跨度下降级，漏掉 leak；② --soak 与总验收 A7
//   **各写了一遍**同一判据，修一处另一处仍是旧的。
//
// 收敛方式：把「趋势 × 跨度 ⇒ 结论」的四象限判定抽成两个纯函数，
// --soak CLI 与 A7 验收项都只调用它们；`soak_verdict_test.go` 直接钉住矩阵，
// 任何人再想在某调用点单独写一套都会被矩阵挡住。

// soakVerdictMinHours 是「有资格下结论」的最低跨度门槛（小时）。
//
// 24 小时的来历：A7 判据的前提是覆盖一个完整的日周期（含业务低峰），
// 不足 24h 的序列无法区分「预热爬坡」与「无界泄漏」。
const soakVerdictMinHours = 24.0

// soakConclusion 把「趋势判定 × 采样跨度」映射为三态结论与退出码。
//
// 返回 (verdict, exitCode)，取值约定：
//
//	verdict = "ok"       ⇒ exitCode 0   趋势正常且跨度足够
//	verdict = "leak"     ⇒ exitCode 2   检出泄漏趋势且跨度足够
//	verdict = "unknown"  ⇒ exitCode 3   跨度不足 / 趋势不可判定 / 输入非法
//
// 关键性质（本函数存在的全部理由）：**跨度不足时任何趋势都不给结论**。
// 「取不到数 ≠ 取到 0」—— 证据不足既不能记 PASS，也不能记 FAIL。
func soakConclusion(trend string, spanHours float64, minHours float64) (string, int) {
	// 输入非法一律 unknown：这里不能把空 trend / 负跨度当成某一种合法情形。
	if trend != "ok" && trend != "leak" && trend != "unknown" {
		return "unknown", 3
	}
	if spanHours < 0 {
		return "unknown", 3
	}
	// minHours 非法时退回默认门槛，绝不让「配置写坏」把门槛降到 0
	// （门槛为 0 等于任何跨度都足够 ⇒ 恰好复现 2026-10-19 的现场缺陷）。
	if minHours <= 0 {
		minHours = soakVerdictMinHours
	}
	if spanHours < minHours {
		return "unknown", 3
	}
	switch trend {
	case "leak":
		return "leak", 2
	case "ok":
		return "ok", 0
	default:
		return "unknown", 3
	}
}

// classifySoakVerdict 是 A7 验收项侧的同一判据，输出三态 checkState。
//
// 与 soakConclusion 的映射约定（`soak_verdict_test.go` 的 TestSoakSidesAgree 钉住）：
//
//	ok ⇒ pass    leak ⇒ fail    unknown ⇒ unverified
//
// 固定使用默认门槛 24h：A7 的要求写死在验收项标题里，不接受外部调参。
func classifySoakVerdict(trend string, spanHours float64) checkState {
	verdict, _ := soakConclusion(trend, spanHours, soakVerdictMinHours)
	switch verdict {
	case "ok":
		return statePass
	case "leak":
		return stateFail
	default:
		return stateUnverified
	}
}
