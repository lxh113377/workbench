package web

import (
	"strings"
	"testing"
)

// TestOutputsPageUsesStateFilter 钉住「产出页按状态取数，不拉全量再前端过滤」。
//
// 一手缺陷（2026-10-22 数据规模实测，真实库存 33728 任务）：产出页原写法是
// /api/tasks?limit=3000 取最新 3000 条，再在浏览器里 filter(state === done)。
// 但最新窗口被 2919 条 rejected 挤满，窗口内 done = 0 —— 产出页显示「还没有产出」，
// 而库存实际有 86 份完成品。用户会以为系统坏了，且这种缺陷只在数据量长大后出现，
// 小数据回归测试永远测不到。修法：用后端本就支持的 ?state= 过滤（86 条 54ms，更准更快）。
func TestOutputsPageUsesStateFilter(t *testing.T) {
	if !strings.Contains(appJS, "/api/tasks?state=done") {
		t.Error("产出页未按 state=done 取数 —— 数据量大时会被 rejected 挤出窗口，显示 0 份产出")
	}
	if !strings.Contains(appJS, "/api/tasks?state=degraded") {
		t.Error("产出页未按 state=degraded 取数")
	}
	// 回归保险：不得再出现「裸 limit 取全量再过滤」的旧写法
	if strings.Contains(appJS, "const res = await api('/api/tasks?limit=3000')") {
		t.Error("产出页退回旧的「拉 limit=3000 全量再前端过滤」写法")
	}
}

// TestQueuePageFetchesOwnStatus 钉住「队列页自己取 status」。
//
// 一手缺陷：队列页头部「今日产出」读 cache.status.stats.todayDone，
// 而 cache.status 只在 renderOverview() 里赋值 —— 用户直接从 #queue 进页面时
// overview 从未渲染，cache.status 永远 undefined，「今日产出」恒显示 0（实测实际 8）。
// CDP 实测：进入队列页后 1.2/2.5/4.0/6.0s 四次采样 cacheStatus 均为 false。
func TestQueuePageFetchesOwnStatus(t *testing.T) {
	idx := strings.Index(appJS, "async function renderQueue()")
	if idx < 0 {
		t.Fatal("找不到 renderQueue")
	}
	body := appJS[idx:]
	if end := strings.Index(body, "async function renderOutputs"); end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "/api/status") {
		t.Error("队列页未自己取 /api/status —— 直接进 #queue 时「今日产出」会恒显示 0")
	}
	if !strings.Contains(body, "cache.status = statusRes") {
		t.Error("队列页取了 status 却没写进 cache.status（头部仍读旧值）")
	}
}
