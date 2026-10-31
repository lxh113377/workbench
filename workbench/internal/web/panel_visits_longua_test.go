package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// edgeHeadlessUA 是本轮端到端实测里真实出现过的形态（Edge 无头，UA 长度 >80 字节且
// HeadlessChrome 落在第 84 字节之后）—— 这个位置关系不是编的：round 167 我用仓库自带的
// CDP 探针（tests/ui/cdp_probe.mjs，起真实 Edge）跑 /modelpool 与 /governance 时，
// 日志行给出 human=false 而 /api/status 的 panelVisits 给出 firstHuman=true ⇒ 同一事实两个答案。
//
// 为什么必须有这条判据：既有单测一律用 chromeUA（约 88 字节、标记在 80 之前）做样本，
// 截断后仍含 HeadlessChrome 之类关键字 ⇒ 抓不到"截断导致重算分叉"这一类；
// curl / node 探针的 UA 又短到不会触发截断。只有真实浏览器长 UA 才暴露 —— 这正是端到端验证的价值。
const edgeHeadlessUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) HeadlessChrome/142.0.0.0 Safari/537.36 Edg/142.0.0.0"

// TestLongHeadlessUAAgreesAcrossBothReadouts 钉住：同一个 UA，日志行与快照必须同答，且无头浏览器必须判机器。
func TestLongHeadlessUAAgreesAcrossBothReadouts(t *testing.T) {
	if len(edgeHeadlessUA) <= 80 {
		t.Fatalf("样本 UA 只有 %d 字节，没超过清洗上限 80 ⇒ 复现不了真实场景，本判据会空转", len(edgeHeadlessUA))
	}
	if !strings.Contains(edgeHeadlessUA[80:], "Headless") {
		t.Fatalf("Headless 标记不在第 80 字节之后 ⇒ 样本失真（当前第 80 字节后为 %q）", edgeHeadlessUA[80:])
	}
	// 前置：完整 UA 本身必须被识别为机器（否则下面比什么都无意义）
	if !isAutomatedUA(edgeHeadlessUA) {
		t.Fatal("完整 Edge 无头 UA 没被判成自动件 ⇒ 名单本身漏了，先修名单")
	}
	srv, sink := newVisitServer(t)
	rec := httptest.NewRecorder()
	srv.handleModelPoolPage(rec, getAs("/modelpool", edgeHeadlessUA))
	got := sink.find(visitMsg)
	if len(got) != 1 {
		t.Fatalf("应有 1 条访问日志，实得 %d ⇒ 进门断言失败，后续比对是空气", len(got))
	}
	logHuman, _ := got[0].Fields["human"].(bool)
	snap, _ := srv.visits.snapshot()["/modelpool"].(map[string]any)
	if snap == nil {
		t.Fatal("快照里没有 /modelpool ⇒ 计数没落上，无法比对")
	}
	snapHuman, _ := snap["firstHuman"].(bool)
	// 两条独立断言：一致 + 都必须是 false（一致但都判成"有人"同样是最坏结果）
	if logHuman != snapHuman {
		t.Errorf("同一 UA 两个读法答案分叉：日志 human=%v 而快照 firstHuman=%v ⇒ 快照在被截断的 UA 上重算", logHuman, snapHuman)
	}
	if snapHuman {
		t.Errorf("无头浏览器被快照记成 firstHuman=true ⇒ 面板会对使用者谎称「有真人打开过界面」")
	}
	if logHuman {
		t.Errorf("日志记成 human=true ⇒ 名单漏判无头浏览器")
	}
	// 顺带钉截断本身：入库的 firstUA 只可能是截断形态，所以「存结论、不重算」是必须的
	if ua, _ := snap["firstUA"].(string); len(ua) > 80 {
		t.Errorf("入库 UA 长 %d 超过上限 80 ⇒ 清洗失效，日志注入面回来了", len(ua))
	}
	t.Logf("两个读法一致（log=%v / snap=%v），UA 入库长度 %d（原 %d）⇒ 结论在 note 时固化，snapshot 不再重算",
		logHuman, snapHuman, len(snap["firstUA"].(string)), len(edgeHeadlessUA))
}
