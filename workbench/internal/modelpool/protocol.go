package modelpool

import "strings"

// RequiresArtifacts 判定任务的提示词是否**明确要求产出物块**。
//
// 判据取「提示词里有没有 `<<<ARTIFACTS>>>` 协议块」：引擎的 appendProtocol 只给**执行阶段**的任务加这段，
// 方案阶段（只输出一个 JSON 对象）没有。用提示词自描述作判据，而不是猜任务 id 后缀，
// 这样引擎改阶段命名也不会失配（round 21 · T18）。
func RequiresArtifacts(prompt string) bool {
	return strings.Contains(prompt, "<<<ARTIFACTS>>>")
}

// noArtifactKind 是「任务要求产物、模型却只给了过程独白」的失败类别。
//
// 与 bad_exit 的区别：bad_exit 记的是「没有最终答案」（任务/提示词层面），
// 本类别记的是「有答案但没有产物块」—— 实测该形态与**车道能力**强相关（只吐自有协议的车道永远交不出成品），
// 因此 modelAttributable 认它（见 failover.go）。
const noArtifactKind = "no_artifact"
