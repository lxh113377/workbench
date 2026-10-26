package modelpool

import (
	"strings"

	"workbench/internal/executor"
)

// bootFailureAttributable 判定一次 `boot_failed` 是否**明确**属于「这条车道没在服务该模型」。
//
// 为什么需要（T17②，round 33 实测）：`combo:auto-free` / `combo:auto-main` / `combo:auto-vision` 在**每个**窗口都以
//
//	dsh: SERVER: our free model does not serve "combo:auto-free" on this egress
//
// 失败，却因为 `boot_failed` 不在「可归因到模型」的类别里被当成**任务级失败**跳过 ⇒ 不入健康度、不进冷却，
// 于是每个任务都要再白试它们一遍（每个约 4 s + 一次链式切换）。
//
// 判据刻意取得很窄：只认**上游原文里写明"不服务该模型"**这一类确定性描述（`does not serve`）；
// 泛化的 `Provider returned error` / `CLIENT_ERROR` **不在此列** —— 那可能是瞬时抖动，归因到车道会误杀健康数量。
func bootFailureAttributable(kind, detail string) bool {
	if kind != string(executor.KindBoot) {
		return false
	}
	return strings.Contains(strings.ToLower(detail), "does not serve")
}
