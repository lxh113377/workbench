package executor

// ClassifyTextForAudit 用**运行期同一份分类规则**给一段上游错误文本定性（审计与判据用）。
//
// 为什么导出而不是在审计工具里另写一张规则表（round 145 · T101）：
// 分类**次序**本身就是本仓最容易分叉的东西 —— 仅本会话就撞到三次次序/归属错位
// （T97 验收路径误分类、T98 记录面误归因、T100 额度被降级），而"同一契约多处实现"在本仓
// 已累计吃过 T57/T61/T72/T80/T84/T86 的亏。审计若自带一份规则副本，改了运行期规则而审计不动，
// 扫描就会给出与产品不一致的结论 —— 那正是审计最不该有的性质。
//
// 为什么传 &Result{} 而不是 nil：classify 末尾有 `case res.ExitCode != 0`，nil 会直接 panic
// （历史台账里的文本没有结构化字段，只能走 stderr 通道；这正是它需要被导出的原因）。
func ClassifyTextForAudit(text string) Kind {
	return classify(text, &Result{})
}
