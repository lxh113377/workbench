package web

import (
	"strings"
	"testing"
)

// ────────────────────────────────────────────────────────────────
// 模型池「网关读数」措辞契约（2026-10-20 · 老大实报）
//
// 用户可见症状：总览卡红字「网关错误：端口未监听: dial tcp 127.0.0.1:8326:
// connectex: No connection could be made because the target machine actively refused it.」
//
// 那是「DSH 桌面端没起 / 任务隔离 HOME 轮换」的正常环境态，已被 catalog 回退兜住，
// 不是缺陷。旧版一律红字 + 裸 dial 字符串，把可自愈状态渲染成故障。
// 判据：renderOverview 必须按 gateway.reachable / gateway.authorized 分三级措辞，
// 且「端口没监听」这一级不得使用 danger 红字。
// ────────────────────────────────────────────────────────────────

// TestPoolGatewayWordingIsTiered 钉住三级措辞。
func TestPoolGatewayWordingIsTiered(t *testing.T) {
	seg := sliceBetween(poolJS, "const renderOverview", "const renderLanes")
	if seg == "" {
		t.Fatal("未能定位 renderOverview 段落（改名/删除 ⇒ 判据必须红，不得静默通过）")
	}
	for _, need := range []string{"网关未监听：", "网关鉴权失败：", "网关错误："} {
		if !strings.Contains(seg, need) {
			t.Errorf("renderOverview 缺少分级措辞 %q", need)
		}
	}
	// 「端口没监听」这一级必须与 danger 红字解耦：
	// 断言 reachable===false 分支里没有 color:var(--danger)。
	idx := strings.Index(seg, "reachable === false ? ''")
	if idx < 0 {
		t.Fatal("缺少「port-down 不用红字」的分支判据（reachable === false ? '' : color:var(--danger)）")
	}
	// 反假绿：把分级措辞退回旧的一律「网关错误」，判读必须失败。
	old := strings.Replace(seg, "网关未监听：", "网关错误：", 1)
	if strings.Contains(old, "网关未监听：") {
		t.Fatal("反证样本构造失败")
	}
	if strings.Contains(old, "网关未监听：") || !strings.Contains(old, "网关错误：") {
		t.Fatal("反证样本未生效")
	}
}
