// Package web 提供工作台的本地 HTTP 服务：极简首页、管理面板、JSON API 与 SSE。
package web

import (
	"sync"
	"time"

	"workbench/internal/model"
)

// Hub 是 SSE 事件总线。
type Hub struct {
	mu      sync.RWMutex
	clients map[chan model.Event]struct{}
	ring    []model.Event
	maxRing int
}

// NewHub 构造事件总线，ring 保留最近事件供新连接回放。
func NewHub(ringSize int) *Hub {
	if ringSize <= 0 {
		ringSize = 200
	}
	return &Hub{clients: map[chan model.Event]struct{}{}, maxRing: ringSize}
}

// Subscribe 订阅事件流；返回通道与取消函数。
func (h *Hub) Subscribe() (chan model.Event, func()) {
	ch := make(chan model.Event, 64)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	backlog := append([]model.Event(nil), h.ring...)
	h.mu.Unlock()
	// 回放必须在 h.mu 内进行：Emit 持锁发送、cancel 持锁 close，三者互斥才保证
	//「不向已关闭通道发送」。发送一律带 default（非阻塞），故不会出现「持锁等慢客户端」
	//——那正是 round 185-186 用超时判据钉住的 Emit 非阻塞契约。
	go func() {
		for _, ev := range backlog {
			h.mu.Lock()
			_, alive := h.clients[ch] // 已取消 ⇒ 通道可能已 close，必须停手
			if alive {
				select {
				case ch <- ev:
				default:
					alive = false // 缓冲满：丢剩余回放，绝不持锁空等
				}
			}
			h.mu.Unlock()
			if !alive {
				return
			}
		}
	}()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.clients[ch]; ok {
			delete(h.clients, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// Emit 广播一条事件（非阻塞：慢客户端丢帧而不阻塞引擎）。
func (h *Hub) Emit(ev model.Event) {
	if ev.At == 0 {
		ev.At = time.Now().UnixMilli()
	}
	h.mu.Lock()
	h.ring = append(h.ring, ev)
	if len(h.ring) > h.maxRing {
		h.ring = h.ring[len(h.ring)-h.maxRing:]
	}
	for ch := range h.clients {
		select {
		case ch <- ev:
		default:
		}
	}
	h.mu.Unlock()
}

// Recent 返回最近事件快照。
func (h *Hub) Recent(n int) []model.Event {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if n <= 0 || n > len(h.ring) {
		n = len(h.ring)
	}
	return append([]model.Event(nil), h.ring[len(h.ring)-n:]...)
}

// Clients 返回当前连接数。
func (h *Hub) Clients() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
