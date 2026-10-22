package web

import (
	"strings"
	"testing"
)

// TestEveryOpenTaskCallerHasContainer 钉住「每个调用 openTask 的页面都必须挂 #taskDetail」。
//
// 一手缺陷（2026-10-23 全链路走查，真实 8 个排队任务现场）：
// openTask 里是「拿到 box 为空就 return」的静默返回，而 #taskDetail 此前只在产出页渲染，
// 队列页没有该容器 => 队列页那 8 个「详情」按钮点了毫无反应、也没有任何提示。
// CDP 实测：点击后 taskDetail=null、paneLen 不变、零运行时错误 —— 这是最坏的一类故障：
// 按钮在、点了没反应、没有任何线索。
// 修：队列页渲染 #taskDetail 容器；openTask 的静默 return 改成 toast 明示。
func TestEveryOpenTaskCallerHasContainer(t *testing.T) {
	const marker = "<div id=\"taskDetail\">"
	count := strings.Count(appJS, marker)
	if count < 2 {
		t.Fatalf("只有 %d 处渲染 #taskDetail；调用 openTask 的两页（队列/产出）都必须有容器，否则另一页的按钮会静默失败", count)
	}
	idx := strings.Index(appJS, "async openTask(id)")
	if idx < 0 {
		t.Fatal("找不到 openTask")
	}
	body := appJS[idx:]
	if e := strings.Index(body, "async exportAll"); e > 0 {
		body = body[:e]
	}
	if strings.Contains(body, "if (!box) { return; }") {
		t.Error("openTask 的容器缺失分支仍是裸 return —— 会变成不可诊断的静默失败，必须 toast 明示")
	}
	if !strings.Contains(body, "详情面板缺失") {
		t.Error("openTask 容器缺失时没有明示报错")
	}
}

// TestQueueCarriesOpenDetail 钉住「队列页重渲染不吞掉已展开的详情」。
//
// 队列页是 SSE 重渲染目标（约 2.5 次/秒）。若重渲染时不带上已展开的详情内容，
// 用户点开的详情会在下一次事件到达时被抹掉 —— 与滚动丢失同型。
func TestQueueCarriesOpenDetail(t *testing.T) {
	idx := strings.Index(appJS, "async function renderQueue()")
	if idx < 0 {
		t.Fatal("找不到 renderQueue")
	}
	body := appJS[idx:]
	if e := strings.Index(body, "async function renderOutputs"); e > 0 {
		body = body[:e]
	}
	// 断言**实际取值逻辑**而非标识符存在性（本轮 Y3 变异暴露：把 keepDetail 改成 ''，
	// 标识符仍在、判据照样通过 —— 断言须能区分「真的读了旧内容」与「读了个空」）。
	if !strings.Contains(body, "openedDetail && openedDetail.innerHTML ? openedDetail.innerHTML : ''") {
		t.Error("renderQueue 未真正读取已展开详情的内容（重渲染会被 SSE 刷新抹掉）")
	}
	if !strings.Contains(body, "document.getElementById('taskDetail')") {
		t.Error("renderQueue 未定位旧的 #taskDetail 节点")
	}
	if !strings.Contains(body, "'<div id=\"taskDetail\">' + keepDetail + '</div>'") {
		t.Error("renderQueue 未把 keepDetail 带进新渲染的容器")
	}
}
