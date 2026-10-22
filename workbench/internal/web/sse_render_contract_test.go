package web

import (
	"strings"
	"testing"
)

// TestSSERenderIsThrottled 钉住「SSE 重渲染必须节流」。
//
// 一手实测（2026-10-23 并发时序走查，6 任务并跑现场）：SSE 峰值约 **10 条/秒**，
// 原实现每条 onmessage 都对 overview/queue 做全量 innerHTML 重渲染。
// 这是「事件频率 × 全量重建」的平方级浪费：当前内容量小所以长任务还是 0，
// 但内容一多必掉帧。合并到 400ms 窗口（窗口内多条事件只渲染最后一帧）。
func TestSSERenderIsThrottled(t *testing.T) {
	i := strings.Index(appJS, "es.onmessage = function ()")
	if i < 0 {
		t.Fatal("找不到 SSE onmessage 处理")
	}
	end := i + 700
	if end > len(appJS) {
		end = len(appJS)
	}
	body := appJS[i:end]
	if !strings.Contains(body, "renderTimer") {
		t.Error("SSE onmessage 缺少节流定时器 —— 10 条/秒 × 全量重建会持续吃主线程")
	}
	if !strings.Contains(body, "400") {
		t.Error("SSE 节流窗口缺失或变更（实测 400ms 足够跟上实时性）")
	}
}

// TestRenderPreservesScroll 钉住「重渲染必须保住滚动位置」。
//
// 一手实测缺陷：用户把「任务」页表格滚到 100px，SSE 重渲染把整个 .table-scroll
// 元素换掉（elemStillInDom=false），新节点 scrollTop 归 0 —— 列表每秒被弹回顶部十次。
// 30 次采样（200ms 间隔）修前 zeroCount=30（全 0）、修后 zeroCount=0 / nonZeroCount=30。
//
// 修法有两个**必须同时满足**的点，缺一无效（第一版就栽在这里）：
//
//	① 不能手动按 scrollHeight 夹紧 —— 刚换完 innerHTML 时布局未完成，
//	   scrollHeight 偏小 → maxTop 算成 0 → Math.min(top,0) 把位置夹成 0；
//	② 必须双 rAF 后再赋值 —— 第一帧完成布局，第二帧赋值才不被丢弃。
func TestRenderPreservesScroll(t *testing.T) {
	if !strings.Contains(appJS, "const snapshotScroll =") {
		t.Error("缺少 snapshotScroll（重渲染前快照滚动位置）")
	}
	if !strings.Contains(appJS, "const restoreScroll =") {
		t.Error("缺少 restoreScroll（重渲染后恢复滚动位置）")
	}
	// ① 不得出现按 scrollHeight 手动夹紧的写法（第一版失败根因）
	if strings.Contains(appJS, "const maxTop = Math.max(0, el.scrollHeight - el.clientHeight)") {
		t.Error("restoreScroll 又按 scrollHeight 手动夹紧了 —— 布局未完成时会把滚动位置夹成 0")
	}
	// ② 必须是双 rAF
	if !strings.Contains(appJS, "requestAnimationFrame(function () { requestAnimationFrame(apply); })") {
		t.Error("restoreScroll 不是双 rAF —— 单帧赋值会被布局丢弃")
	}
	// ③ render() 必须在渲染前后成对调用
	if !strings.Contains(appJS, "const scrollSnap = snapshotScroll()") {
		t.Error("render() 渲染前未快照滚动位置")
	}
	if !strings.Contains(appJS, "restoreScroll(scrollSnap)") {
		t.Error("render() 渲染后未恢复滚动位置")
	}
}
