package engine

import "workbench/internal/model"

// completionVerdict 判定一次「执行器成功返回」的产出该落哪个终态（round 46 · T28）。
//
// 为什么需要（审计实锤）：`materializeArtifacts` 只在回答里**有产物块**时才返回报告；
// 于是 `verifyRep == nil` 意味着「模型压根没按协议交东西」。修前这种情况直接落 `done`（verified=false），
// 造成「0 个 artifacts-index / 0 个 workspace，却有 done 记录」的虚报完成。
// 口径：
//   - 报告为 nil  ⇒ `degraded`，理由写明「无产物块」（与「交了但没过」区分，便于台账/面板一眼分辨）；
//   - 报告 pass=false ⇒ `degraded`，理由「产出未通过验收」（既有语义）；
//   - 报告 pass=true  ⇒ `done`。
func completionVerdict(rep *model.VerifyReport) (state model.TaskState, note string) {
	switch {
	case rep == nil:
		return model.StateDegraded, "无产物块：模型未按产物协议交付，已降级"
	case !rep.Pass:
		return model.StateDegraded, "产出未通过验收，已降级"
	default:
		return model.StateDone, "执行成功"
	}
}
