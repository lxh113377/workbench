package modelpool

import (
	"strings"

	"workbench/internal/executor"
)

// notServedKind 是「上游在这条出口上不服务该模型」的类别字符串（与 executor 常量同一份来源）。
const notServedKind = string(executor.KindNotServed)

// bootAttributionKind 判定一次 `boot_failed` 是否**明确**属于「这条出口没在服务该模型」，并给出应记的类别。
//
// 为什么需要（T17②，round 33 实测）：`combo:auto-free` / `combo:auto-main` / `combo:auto-vision` 在**每个**窗口都以
//
//	dsh: SERVER: our free model does not serve "combo:auto-free" on this egress
//
// 失败，却因为 `boot_failed` 不在「可归因到模型」的类别里被当成**任务级失败**跳过 ⇒ 不入健康度、不进冷却，
// 于是每个任务都要再白试它们一遍（每个约 4 s + 一次链式切换）。
//
// 判据刻意取得很窄：只认**上游原文里写明"不服务该模型"**这一类确定性描述（`does not serve`，大小写不敏感）；
// 泛化的 `Provider returned error` / `CLIENT_ERROR` **不在此列** —— 那可能是瞬时抖动，归因到车道会误杀健康车道。
//
// 返回的类别是 `not_served`（不是 `model_unavailable`）：后者可由探测恢复，前者是**出口级事实**，探测不得把它治回
// （round 33 实测：按 model_unavailable 记之后 45 s 就被探测周期治好 ⇒ 归因被抵消，见 T17②-b）。
func bootAttributionKind(kind, detail string) (string, bool) {
	if kind != string(executor.KindBoot) {
		return "", false
	}
	if !strings.Contains(strings.ToLower(detail), "does not serve") {
		return "", false
	}
	return notServedKind, true
}
