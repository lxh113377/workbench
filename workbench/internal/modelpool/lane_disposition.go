package modelpool

import (
	"strings"

	"workbench/internal/executor"
)

// laneDisposition 是「某个失败类别进入车道健康账本后应如何处置」的一条决策记录（round 125 · T80）。
//
// 为什么要收成一张表（这是本会话第 5 次同族事故的根因治理）：此前这些决策散落在三处裸字符串分支里 ——
//
//	· manager.go：kind == no_artifact 一击即冷却 600s；kind == not_served 一击即冷却 1800s；
//	  kind == "model_unavailable" || "missing_credential" || "unreachable" || "bad_endpoint" 直接长冷却；
//	· exec_health.go：另一份「探测不得翻案」的 kind 列表；
//	· boot_attribution.go / failover.go：又两份 kind 判定。
//
// 三次线上事故都是同一形态：新增/改名一个类别时只改了其中一处，另一处的判定**静默失效**
// （最隐蔽的一次是记账把 errKind 写死 ⇒ 下游按前缀查找恒 false ⇒ 面板上完全看不出来）。
//
// 表本身不是目的，lane_disposition_test.go 的**总覆盖守卫**才是：
// 凡 executor 侧新增一个 Kind，要么在这里显式登记处置，要么进 engineOnlyKinds 并写明为什么不经车道账本；
// 漏决策必红 —— 把「记得改三处」变成交付门禁。
type laneDisposition struct {
	// oneStrike = true 表示这不是抖动而是出口级/能力级事实：连续失败阈值压到 1，第一次就冷却。
	oneStrike bool
	// minCooldownSec 是该类别冷却的下限（0 = 沿用 spec 的 cooldownSec）。
	// 数量级要有依据：no_artifact 600s（协议不合规不会因几秒而变好）、not_served 1800s（出口级事实）。
	minCooldownSec int
	// longCooldown = true 表示结构性错误（端点不可达/缺凭据/模型不存在）直接判死并长冷却，不等攒够次数。
	longCooldown bool
	// probeCannotCure = true 表示「端点可达」不构成反证，探测成功不得把它翻回 healthy
	//（只能由真实执行成功或冷却到期的半开恢复清除）。这一列就是 exec_health 里那份列表的来源。
	probeCannotCure bool
	// note 记录「为什么是这个处置」，让下一个改代码的人不必重新考古。
	note string
}

const (
	// 探测侧类别（classifyProbeErr 产出，不在 executor 的 Kind 枚举里）。
	probeKindUnreachable = "unreachable"
	probeKindBadEndpoint = "bad_endpoint"
)

// laneDispositions 是唯一事实源。缺条目 ⇒ lane_disposition_test.go 判红。
var laneDispositions = map[string]laneDisposition{
	string(executor.KindNoArtifact): {
		oneStrike: true, minCooldownSec: 600,
		note: "协议不合规是能力级事实：反复试只会重复 no_artifact（round 22 实测一次尝试内连拒 3 条车道）",
	},
	string(executor.KindNotServed): {
		oneStrike: true, minCooldownSec: 1800, probeCannotCure: true,
		note: "出口级事实：这条出口不服务该模型，与端点连通性无关（T17②-b；探测翻案曾把归因抵消掉）",
	},
	string(executor.KindModelDead):  {longCooldown: true, note: "模型不存在：结构性，等也没用"},
	string(executor.KindMissingKey): {longCooldown: true, note: "缺凭据：环境性不可恢复，重试只烧额度"},
	probeKindUnreachable:            {longCooldown: true, note: "端点不可达：结构性"},
	probeKindBadEndpoint:            {longCooldown: true, note: "端点坏（4xx 常量形态）：结构性"},
	string(executor.KindStalled):    {probeCannotCure: true, note: "零产出：端点可达不代表它会产出（round 15 实测被探测反复复活）"},
	string(executor.KindExit):       {probeCannotCure: true, note: "无最终答案：产出问题，探测不是反证"},
	string(executor.KindTimeout):    {probeCannotCure: true, note: "超时：可能是车道慢，不让探测抹掉计数"},
	string(executor.KindUpstream):   {probeCannotCure: true, note: "上游失败：执行侧事实"},
	string(executor.KindBoot):       {probeCannotCure: true, note: "启动失败：成因在该车道装载/启动，GET /v1/models 200 不是反证（T75，beam-free 48/48 被反复选中）"},
	// 显式登记的「默认处置」类：会进车道账本但没有特殊策略（攒够 maxFail 才冷却、探测可翻案）。
	string(executor.KindRateLimited):    {note: "瞬时限流：等短窗口即可，由 engine 侧按车道冷却处理，不在这里加长"},
	string(executor.KindQuotaExhausted): {note: "额度耗尽：同样进车道账本但按 rate_limited 短冷却；长尺度由 engine 的等待预算分级承担（T78）"},
	string(executor.KindTransport):      {note: "网络瞬断（fetch failed/ETIMEDOUT 等）：默认处置，探测可翻案 —— 与 isExecutionFailureKind 原集合一致（本表逐项等价）"},
	string(executor.KindStreamCut):      {note: "流被切断：多为上游抖动，默认处置，探测可翻案（同上，等价性由 lane_disposition_test 钉住）"},
}

// engineOnlyKinds 是「设计上不经车道健康账本」的类别：它们是引擎自己的终态判定或任务级失败，
// 归因到某条模型会冷却无辜车道。列在这里并要求写明理由 —— 让「不进表」成为一个**决策**而不是遗忘。
var engineOnlyKinds = map[string]string{
	string(executor.KindCancelled):        "用户取消：调用方意图，不是模型故障",
	string(executor.KindNoLane):           "整池此刻不可用：环境事实，不归属单条车道",
	string(executor.KindPoolWaitCapped):   "环境等待封顶判死：重试预算耗尽的产物",
	string(executor.KindTaskPanic):        "引擎内 panic：确定性缺陷，重投必再炸",
	string(executor.KindTransitionFailed): "本地状态机/磁盘问题，与车道无关",
	string(executor.KindCrashLoop):        "崩溃循环：重投会把进程再弄死",
	string(executor.KindVerifyFailed):     "验收失败：由返工环负责，不归车道冷却",
	string(executor.KindReplan):           "重规划：由重规划环负责",
	string(executor.KindUnknown):          "兜底类别：不该被当作某种车道处置来对待",
}

// dispositionOf 取某类别的车道处置；没有登记时返回 ok=false（调用方走默认路径，由测试保证不会出现）。
func dispositionOf(kind string) (laneDisposition, bool) {
	d, ok := laneDispositions[strings.TrimSpace(kind)]
	return d, ok
}

// laneProbeCannotCure 回答「探测成功不得把这条失败翻案」（exec_health 的唯一来源）。
func laneProbeCannotCure(kind string) bool {
	d, ok := dispositionOf(kind)
	return ok && d.probeCannotCure
}

// laneOneStrike 给出「是否一击即冷却」及其冷却下限（manager 的唯一来源）。
// 未登记时返回 false/0，由 lane_disposition_test 的总覆盖守卫保证这种情况不会出现。
func laneOneStrike(kind string) (oneStrike bool, minCooldownSec int) {
	d, ok := dispositionOf(strings.TrimSpace(kind))
	if !ok {
		return false, 0
	}
	return d.oneStrike, d.minCooldownSec
}

// laneLongCooldown 回答「这是结构性错误吗，是否直接长冷却不等攒次数」（manager 的唯一来源）。
func laneLongCooldown(kind string) bool {
	d, ok := dispositionOf(strings.TrimSpace(kind))
	return ok && d.longCooldown
}
