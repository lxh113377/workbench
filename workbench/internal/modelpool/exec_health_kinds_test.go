package modelpool

import "testing"

// TestIsExecutionFailureKind 是这条判据的**首个测试**（round 72 · T75）。
//
// 它决定「ApplyProbe 的健康分支能否把一条失败翻案」（manager.go:651）；此前无测试，
// 于是 `boot_failed` 漏在集合外整整若干轮：探测一轮端点可达就把它治回 healthy（现场：beam-free 48/48 boot_failed 反复被选中）。
//
// 判据要**双向**钉住：执行侧/出口级失败必须不能被探测翻案；结构性故障（不可达/缺凭据）必须仍能由探测恢复 ——
// 只钉一边的判据会把「翻案逻辑整体失效」这种反向缺陷放过去。
func TestIsExecutionFailureKind(t *testing.T) {
	cannotBeCured := []string{"stalled", "bad_exit", "timeout", "upstream_failed", "boot_failed", notServedKind}
	for _, k := range cannotBeCured {
		if !isExecutionFailureKind(k) {
			t.Errorf("执行侧/出口级失败 %q 不得被探测翻案（否则每轮白烧一次尝试）", k)
		}
	}
	curAbleByProbe := []string{"gateway_off", "gateway_unauthorized", "unreachable", "not_listed", "model_rejected", ""}
	for _, k := range curAbleByProbe {
		if isExecutionFailureKind(k) {
			t.Errorf("结构性故障 %q 必须仍能被探测恢复（否则车道永远回不来）", k)
		}
	}
}
