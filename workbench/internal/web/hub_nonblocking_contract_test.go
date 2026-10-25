package web

// hub_nonblocking_contract_test.go -- 钉住 Hub.Emit 的**非阻塞契约**（2026-10-26 round 185-186）。
//
// 为什么需要（实测依据，不是猜的）：
//   引擎在**持锁期间**调用 emit —— Start 从 L396 的 defer Unlock 罩住整个函数体，
//   Pause / Resume / 停机路径同样在锁内 emitEvent。而 Hub.Emit 的注释承诺
//   「非阻塞：慢客户端丢帧而不阻塞引擎」，此前**只是注释，没有任何测试守住**。
//
//   round 185 探针实测：用与 Hub 同构的非阻塞 emitter 时，Start() 全程 2-6 ms、
//   Phase() 读数被阻塞 0-3 ms ⇒ 当下**不是**性能缺陷（故本轮不改 Start 的锁范围，
//   也不谎称修了瓶颈）。但一旦有人把 select/default 改成阻塞发送、或给客户端加
//   同步落盘，引擎的 mu 就会被**一个卡住的 SSE 客户端拖死**：面板读数停更、停机挂住，
//   而没有任何测试会失败。这正是本仓在册的「注释承诺 ≠ 实现」族，且发生在锁内。
//
// 判据（三条都用超时判定，绝不允许 hang）：
//   ① 无订阅者时 Emit 立即返回；② 慢客户端（订阅后不排空）不得阻塞 Emit，且真被丢帧；
//   ③ 存在灌满的慢客户端时，快客户端不得被饿死。

import (
	"errors"
	"testing"
	"time"

	"workbench/internal/model"
)

// emitWithTimeout 在独立 goroutine 里调 Emit，超时即返回错误。
// 为什么必须有这一层：若 Emit 真的阻塞，直接调用会让测试挂到全局 test timeout
// （实测第一轮就是这样：panic: test timed out after 45s）—— hang 不是可读的判红，
// 还烧 CI 时间。包一层超时才能把「阻塞」转成明确的失败信息。
func emitWithTimeout(h *Hub, ev model.Event, d time.Duration) error {
	done := make(chan struct{})
	go func() {
		h.Emit(ev)
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(d):
		return errors.New("Emit 阻塞超过 " + d.String())
	}
}

func TestHubEmitNeverBlocksWhenClientStalls(t *testing.T) {
	h := NewHub(300)
	// ① 无订阅者
	if err := emitWithTimeout(h, model.Event{Type: "orphan"}, time.Second); err != nil {
		t.Fatalf("无订阅者时 Emit 也必须立即返回: %v", err)
	}
	// ② 慢订阅者：订阅后一条都不读（Subscribe 通道缓冲 64）
	ch, unsub := h.Subscribe()
	defer unsub()
	const n = 1000
	for i := 0; i < n; i++ {
		if err := emitWithTimeout(h, model.Event{Type: "tick", At: int64(i)}, 500*time.Millisecond); err != nil {
			t.Fatalf("第 %d 次 Emit 被慢客户端阻塞（契约：丢帧而不阻塞）: %v", i+1, err)
		}
	}
	// 排空慢客户端通道，统计实收条数（缓冲上限 64，应远小于 n）
	got := 0
drain:
	for {
		select {
		case <-ch:
			got++
		default:
			break drain
		}
	}
	if got >= n {
		t.Fatalf("慢客户端收到全部 %d 条 —— 丢帧逻辑没生效（缓冲上限 64）", got)
	}
	t.Logf("慢客户端实收 %d/%d（其余丢帧，符合「不阻塞引擎」契约）", got, n)
}

func TestHubEmitSlowClientDoesNotStarveFastClient(t *testing.T) {
	h := NewHub(300)
	_, unsubSlow := h.Subscribe()
	defer unsubSlow()
	fast, unsubFast := h.Subscribe()
	defer unsubFast()
	// 先把第一个（慢）客户端的通道灌满 —— 这里也必须走 emitWithTimeout：
	// 否则变异（去掉 default 丢帧）时本测试会 hang 到全局 timeout，判红不可读。
	for k := 0; k < 200; k++ {
		if err := emitWithTimeout(h, model.Event{Type: "fill"}, time.Second); err != nil {
			t.Fatalf("灌满阶段第 %d 次 Emit 已阻塞: %v", k+1, err)
		}
	}
	// 快客户端正常排空：不得因为有个慢客户端而收不到
	const want = 50
	for i := 0; i < want; i++ {
		if err := emitWithTimeout(h, model.Event{Type: "real", At: int64(i)}, 500*time.Millisecond); err != nil {
			t.Fatalf("第 %d 次 Emit 被慢客户端拖住: %v", i+1, err)
		}
	}
	got := 0
	deadline := time.After(2 * time.Second)
collect:
	for got < want {
		select {
		case <-fast:
			got++
		case <-deadline:
			break collect
		}
	}
	if got == 0 {
		t.Fatal("慢客户端把快客户端饿死了：快客户端一条都没收到")
	}
	t.Logf("存在灌满的慢客户端时，快客户端仍收到 %d/%d", got, want)
}
