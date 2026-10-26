package modelpool

// isExecutionFailureKind 判定「这条失败记录是**执行侧产出问题**，而不是端点不可达」。
//
// 为什么需要（2026-10-26 round 15 实测缺陷）：ApplyProbe 的 StateHealthy 分支原先无条件
// `ConsecFail = 0; Success++; CooldownTo = 0` —— 探测只证明「端点可达」，却被当成「模型会产出」。
// 实测读数：deepseek-ai/deepseek-v4.1-flash 在多次**零产出**失败（stalled / bad_exit）之后，
// 健康账本仍是 `succ=17 fail=3 consec=0 healthy`（每轮探测把它复活），于是每个新任务的**首个路由**
// 永远落在实测跑不出东西的模型上，白白烧掉一次尝试。
//
// 口径：结构性故障（不可达 / 缺凭据 / 端点坏）可由探测成功恢复；执行侧失败只能由
// **执行成功**（MarkSuccess）清除。
func isExecutionFailureKind(kind string) bool {
	switch kind {
	case "stalled", "bad_exit", "timeout", "upstream_failed":
		return true
	}
	return false
}
