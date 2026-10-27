package engine

import "workbench/internal/executor"

// attemptedModel 返回「这次尝试台账/健康度该记哪个模型」（round 44 · T27）。
//
// 为什么不能直接用引擎选出的 modelID：`FailoverRunner` 会在自己内部沿候选链换车道，真正执行的是链上那一条；
// 审计实测 3/3 抽查里 `attempts.jsonl` 记的 model 与同 attempt 的 `overlay.yml`（执行器按实际模型写）**全都不符**
// —— 于是 `RecordFailure(modelID)` 把失败记到没被执行的模型上，把好模型一起冷却，台账也不再是可用的审计面。
//
// 口径：执行器上报的 `res.Model` 优先；它为空（老执行器实现、或压根没跑到子进程，如 `ErrNoLane`）时回落
// 到引擎选出的 modelID —— 这条回落是刻意的：那时「没跑过任何模型」，记 selected 比记空更可诊断。
func attemptedModel(res *executor.Result, selected string) string {
	if res != nil && res.Model != "" {
		return res.Model
	}
	return selected
}
