package modelpool

// preferFirst 把 preferred 移到候选链首位（仅当它已在链中且不在首位）；其余元素相对顺序不变。
//
// 为什么需要（T17①，2026-10-26 round 19 里程碑的下一步）：failover 的候选链首由引擎传入的 modelID 决定，
// 而该 modelID 来自池子的加权顺序 —— 同一任务里刚产出成功的模型常常排在链尾，甚至不在链上，
// 于是执行阶段会从头再试一遍最弱车道（实测：计划在 dots-studio/… 成功后，执行又去试 nemotron→boot_failed、
// glm-5.3→早回收，白烧约 2.5 分钟）。
//
// 边界（保守）：只做**链内重排**，不新增候选 —— 因此不会把冷却/下线/被排除的模型拉回来；
// preferred 为空或不在链中时原样返回。
func preferFirst(order []string, preferred string) []string {
	if preferred == "" || len(order) < 2 {
		return order
	}
	idx := -1
	for i, id := range order {
		if id == preferred {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return order
	}
	out := make([]string, 0, len(order))
	out = append(out, order[idx])
	out = append(out, order[:idx]...)
	out = append(out, order[idx+1:]...)
	return out
}
