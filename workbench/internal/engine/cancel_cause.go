package engine

// cancel_cause.go —— 把「为什么被取消」做成可机读的具名事实（issue #11 · 对标 river context.Cause）。
//
// 现场（生产实测，2026-10-29，台账 tasks/*/attempts.jsonl）：
//   2 行 kind=unknown / action=cancel / error="启动 DSH 失败: context canceled"
//   （executor.go:427 用 fmt.Errorf("启动 DSH 失败: %w", err) 包装，包装层不认识 context 错误，
//    于是 executor 的具名 KindCancelled 在这一条路径上根本没机会被赋上。）
// ⇒ 真相是「取消」，对外呈现却是「未知错误」：排查者会去查"为什么未知"，
//   而正确答案是"这是停机/用户终止的正常结果，本就不该算失败"。
//   与已修的 B17 同一形态：**语义在两层各说各话**（动作面说 cancel，类别面说 unknown）。
//
// 为什么不能靠 error 文本判：context.Canceled 本身是歧义的 —— 用户取消、停机、
// 阶段超时、父 ctx 被回收都可能是它。river 的做法是给第二维：context.WithCancelCause
// 的 cause（job_executor.go:590-597 isSoftStopCancelError 就是 cause ∧ err 双条件），
// 把「客户端停机」从「失败」里精确摘出去。asynq 同向：停机走 requeue，
// 既不写 ErrorMsg 也不吃重试次数（processor.go:238-241 + rdb.go:598-619）。

import (
	"context"
	"errors"

	"strings"

	"workbench/internal/retry"
)

// 两个具名 cause：谁取消的、为什么取消。
// 用包内哨兵而不是字符串匹配 —— 字符串会随包装层漂移，哨兵走 errors.Is 穿透 %w。
var (
	// causeEngineShutdown：引擎停机（优雅停服的宽限期兜底 / e.cancel）——不是任务的问题。
	causeEngineShutdown = errors.New("engine shutdown")
	// causeUserCancel：面板/CLI 显式终止该任务（CancelTask）——同样不是任务的问题。
	causeUserCancel = errors.New("user cancel requested")
)

// taskCanceler 是 taskCancels 里存的取消入口。
//
// 为什么不直接存 context.CancelCauseFunc：sync.Map 取出来要做类型断言，断言失败时
// **静默不取消**（无 panic、无日志，「终止」按钮当场变空操作）—— 这是最坏失效形态。
// 包成具名类型后：① 存取两侧只有一个形状；② 调用方必须显式给 cause（编译器逼着区分
// 「用户终止」与「引擎停机」，不许再传 nil）；③ 由 cancel_cause_contract_test.go 的
// 源文本不变量钉住「存的类型 == 断言的类型」，把这类静默失效变成必红。
type taskCanceler struct {
	// cause 是 context.WithCancelCause 返回的取消函数（字段名不能叫 cancel：
	// 那会与下面的方法同名，Go 直接编译错 —— 本文件第一版就踩了）。
	cause context.CancelCauseFunc
}

// Cancel 以**具名 cause** 取消该任务的执行上下文。cause 必须是本文件声明的两个哨兵之一：
// 传 nil 等于主动放弃归因（退回 issue #11 修掉的形态），因此显式拒绝。
func (t taskCanceler) Cancel(cause error) {
	if t.cause == nil || cause == nil {
		return
	}
	t.cause(cause)
}

// classifyCancellation 判断「这次结束到底是不是取消」，并给出具名类别与人类可读文案。
//
// 判定次序（次序本身就是结论，不能调换）：
//
//	① cause 命中具名哨兵 ⇒ 直接命名（最可信：是我们自己写进去的意图）；
//	② cause 缺失但用户意图标记在 ⇒ 仍判取消（用户点了终止，worker 却回了句泛错）；
//	③ ctx 已取消但两者皆无 ⇒ 判取消并标注来源未知（好过 unknown）；
//	④ 只剩 DeadlineExceeded ⇒ **不**算取消（那是超时，属任务执行问题，要照常计失败）。
//
// 返回 cancelled=false 时 kind/text 无意义，调用方走原有失败路径。
func classifyCancellation(ctx context.Context, userRequestedCancel bool, kind retry.Kind) (retry.Kind, string, bool) {
	cause := context.Cause(ctx)
	switch {
	case errors.Is(cause, causeEngineShutdown):
		return retry.Cancelled, "引擎停机取消（非任务问题，不计车道失败）", true
	case errors.Is(cause, causeUserCancel):
		return retry.Cancelled, "用户取消（面板/CLI 终止该任务）", true
	case errors.Is(cause, context.DeadlineExceeded):
		// 超时不是取消：保留原类别，交回失败路径（否则会吞掉一次真实失败）。
		return kind, "", false
	case errors.Is(cause, context.Canceled):
		// 有 cause 但不是我们写的两个具名 cause ⇒ 上游/外部取消，仍属取消而非任务失败。
		return retry.Cancelled, "执行上下文被取消（非任务问题）", true
	}
	// ② cause 还没落到（例如取消与收尾竞态：先写了 cancelling 标记、ctx 尚未可见）
	//    但用户意图已在 ⇒ 以意图为准。这正是「别让下游泛错顶掉真相」的落点。
	if userRequestedCancel {
		return retry.Cancelled, "用户取消（面板/CLI 终止该任务）", true
	}
	// ③ 兜底：没有任何具名信息，但 ctx 确实已取消（旧实现到这里就落 unknown）。
	if ctx != nil && ctx.Err() != nil {
		if kind == retry.Cancelled {
			return retry.Cancelled, "执行上下文被取消（非任务问题）", true
		}
		return retry.Cancelled, "执行上下文已取消（原因未具名）", true
	}
	// ④ executor 自己已把它判成具名取消（res.ErrorKind=cancelled）：认，但不覆写文案。
	if kind == retry.Cancelled {
		return retry.Cancelled, "执行上下文被取消（非任务问题）", true
	}
	return kind, "", false
}

// causeSuffix 把原始错误作为**尾注**附在具名归因之后，而不是让它顶掉归因。
//
// 为什么仍要留下原文：包装层泛错（"启动 DSH 失败: context canceled"）里带着
// 「死在哪一步」的信息量，全丢了排查会失去线索；但它**不能**当主因 —— 主因是取消。
// 形态：具名归因（原始错误: xxx），与 ledger 里 [kind] 前缀配合即可双向定位。
func causeSuffix(raw error) string {
	if raw == nil {
		return ""
	}
	s := strings.TrimSpace(raw.Error())
	if s == "" {
		return ""
	}
	return "（原始错误: " + s + "）"
}
