package web

// hub_closed_channel_contract_test.go -- 钉住「取消订阅时不得向已关闭通道发送」（2026-10-26 round 217，CI 实测）。
//
// 为什么需要（不是假想的竞态，是 CI 抓到的真崩溃）：
//   纯净检出门禁 check_snapshot_build.ps1 跑 go test ./... 时崩在
//     panic: send on closed channel
//     workbench/internal/web.(*Hub).Subscribe.func1()  hub.go:37
//   根因：Subscribe 起的回放 goroutine 往 ch 发送，**不持任何锁**；而 cancel 在 h.mu 内
//   delete + close(ch)。两者没有同步关系 ⇒ 取消落在"回放在途"的窗口里就向已关闭通道发送。
//   同一个 close 对 Emit 是安全的（Emit 持锁、且先 delete 再 close），唯独漏了回放这条路。
//
// 对 7×24 无人值守进程的后果：panic 发生在后台 goroutine，**没有 recover 能兜住**，整个进程退出；
// 触发条件是"浏览器关掉 SSE 页面而 ring 里还有几十条待回放事件"——日常就会发生，不是理论场景。
//
// 判据（绝不允许 hang）：
//   ① 大量「订阅→少量消费→立即取消」的轮次内不得 panic（panic 会直接崩测试进程，即判红）；
//   ② 全部取消后不得残留订阅者（顺带钉住泄漏，因为修复只该改变同步方式，不该改变清理语义）。

import (
	"sync"
	"testing"
	"time"

	"workbench/internal/model"
)

func TestHubCancelDuringBacklogReplayMustNotPanic(t *testing.T) {
	h := NewHub(300)
	// 先把 ring 灌满，使每次 Subscribe 都会起一个"有一堆待发事件"的回放 goroutine
	for i := 0; i < 300; i++ {
		h.Emit(model.Event{Type: "seed", At: int64(i)})
	}
	const rounds = 4000
	var wg sync.WaitGroup
	for r := 0; r < rounds; r++ {
		ch, cancel := h.Subscribe()
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 消费几条让缓冲腾出空间，使回放仍在发送中；随后立刻取消
			for k := 0; k < 4; k++ {
				select {
				case <-ch:
				case <-time.After(20 * time.Millisecond):
				}
			}
			cancel()
		}()
	}
	wg.Wait()
	if got := h.Clients(); got != 0 {
		t.Fatalf("全部取消后仍残留 %d 个订阅者", got)
	}
	t.Logf("%d 轮「订阅→消费→取消」无 panic、无残留订阅者", rounds)
}

// 反向对照：正常消费完再取消也不该 panic（防修复把正常路径改坏）
func TestHubNormalUnsubscribeStillStopsDelivery(t *testing.T) {
	h := NewHub(50)
	ch, cancel := h.Subscribe()
	h.Emit(model.Event{Type: "a"})
	select {
	case ev := <-ch:
		if ev.Type != "a" {
			t.Fatalf("期望事件 a，实收 %q", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("订阅后 Emit 的事件没送达")
	}
	cancel()
	// 取消后通道应已关闭：再读必须立刻返回 open=false
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("取消后仍能读到事件（通道未关闭，SSE 处理器不会退出）")
		}
	case <-time.After(time.Second):
		t.Fatal("取消后通道未关闭：读取阻塞，SSE 长连接会挂住")
	}
	if got := h.Clients(); got != 0 {
		t.Fatalf("取消后残留 %d 个订阅者", got)
	}
}
