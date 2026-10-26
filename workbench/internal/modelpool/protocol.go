package modelpool

import (
	"strings"

	"workbench/internal/executor"
	"workbench/internal/model"
)

// RequiresArtifacts 判定任务的提示词是否**明确要求产出物块**。
//
// 判据取「提示词里有没有 `<<<ARTIFACTS>>>` 协议块」：引擎的 appendProtocol 只给**执行阶段**的任务加这段，
// 方案阶段（只输出一个 JSON 对象）没有。用提示词自描述作判据，而不是猜任务 id 后缀，
// 这样引擎改阶段命名也不会失配（round 21 · T18）。
func RequiresArtifacts(prompt string) bool {
	return strings.Contains(prompt, "<<<ARTIFACTS>>>")
}

// requireArtifactsFor 组合「要不要产物块」的两条判据（执行器上报优先，提示词自描述兜底）。
//
// 为什么需要兜底（round 21 实测）：协议是**执行器**拼进 prompt 的，所以 `task.Prompt` 里没有 `<<<ARTIFACTS>>>` ——
// 只按提示词判时 `no_artifact` 一次都没触发（实测 count=0）。执行器上报（`Result.ArtifactsRequired`）才是第一判据；
// 保留提示词判据是为了将来协议改由上层拼时不失配。
func requireArtifactsFor(res *executor.Result, task *model.Task) bool {
	// 方案阶段永远不要求产物块 —— 这条必须放在最前面（round 24 实测回归）：
	// 执行器对**所有**阶段都会把产物协议拼进 prompt，于是它上报的 ArtifactsRequired 在方案阶段也是 true，
	// 结果方案阶段返回的合法 JSON 被判成 no_artifact（实测 `方案调用失败: 执行失败: no_artifact`）。
	if task != nil && task.Phase == model.PhasePlan {
		return false
	}
	if res != nil && res.ArtifactsRequired.Load() {
		return true
	}
	if task == nil {
		return false
	}
	return RequiresArtifacts(task.Prompt)
}

// noArtifactKind 是「任务要求产物、模型却只给了过程独白」的失败类别。
//
// 与 bad_exit 的区别：bad_exit 记的是「没有最终答案」（任务/提示词层面），
// 本类别记的是「有答案但没有产物块」—— 实测该形态与**车道能力**强相关（只吐自有协议的车道永远交不出成品），
// 因此 modelAttributable 认它（见 failover.go）。
// 取值直接来自 executor 的类别常量：判据在 failover 层，但类别字符串必须与执行器同一份（两处各写一遍必然漂移）。
const noArtifactKind = string(executor.KindNoArtifact)
