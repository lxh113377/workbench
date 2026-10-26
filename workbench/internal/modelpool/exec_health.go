package modelpool

// isExecutionFailureKind 判定「这条失败记录**不能被探测成功清除**」。
//
// 为什么需要（2026-10-26 round 15 实测缺陷）：ApplyProbe 的 StateHealthy 分支原先无条件
// `ConsecFail = 0; Success++; CooldownTo = 0` —— 探测只证明「端点可达」，却被当成「模型会产出」。
// 实测读数：deepseek-ai/deepseek-v4.1-flash 在多次**零产出**失败（stalled / bad_exit）之后，
// 健康账本仍是 `succ=17 fail=3 consec=0 healthy`（每轮探测把它复活），于是每个新任务的**首个路由**
// 永远落在实测跑不出东西的模型上，白白烧掉一次尝试。
//
// 口径：只有**端点连通性类**故障（不可达 / 缺凭据 / 端点坏）可由探测成功恢复；
// 执行侧失败（产出问题）与**出口级事实**（上游不服务该模型）只能由**执行成功**（MarkSuccess）清除。
//
// round 33 补充 `not_served`（T17②-b）：它按 model_unavailable 记时，45 s 后就被探测周期治回 healthy，
// 归因被抵消；把它纳入本集合后，「出口不服务该模型」不再被端点可达性翻案。
func isExecutionFailureKind(kind string) bool {
	switch kind {
	case "stalled", "bad_exit", "timeout", "upstream_failed":
		return true
	case notServedKind:
		return true
	}
	return false
}
