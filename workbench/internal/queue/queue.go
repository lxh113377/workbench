// Package queue 实现任务队列与调度：优先级 + 先进先出 + 并发信号量 + 每链串行。
package queue

import (
	"fmt"
	"sort"
	"sync"

	"workbench/internal/model"
)

// Queue 是内存队列。真正的持久化在 store 层，这里只维护"可执行候选"的顺序。
type Queue struct {
	mu        sync.Mutex
	items     map[string]*model.Task
	chainBusy map[string]bool // 链 ID -> 是否有任务在跑
}

// New 构造空队列。
func New() *Queue {
	return &Queue{items: map[string]*model.Task{}, chainBusy: map[string]bool{}}
}

// Len 返回队列长度。
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Push 把任务放入队列（同 ID 覆盖）。
func (q *Queue) Push(t *model.Task) {
	if t == nil || t.ID == "" {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	cp := *t
	q.items[t.ID] = &cp
}

// Remove 从队列移除。
func (q *Queue) Remove(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.items, id)
}

// Reset 用给定任务列表重建队列（启动恢复用）。
func (q *Queue) Reset(tasks []*model.Task) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = make(map[string]*model.Task, len(tasks))
	for _, t := range tasks {
		if t == nil || t.ID == "" {
			continue
		}
		cp := *t
		q.items[t.ID] = &cp
	}
}

// Snapshot 返回队列中的任务副本，按调度顺序排序。
func (q *Queue) Snapshot() []*model.Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]*model.Task, 0, len(q.items))
	for _, t := range q.items {
		cp := *t
		out = append(out, &cp)
	}
	sortTasks(out)
	return out
}

// sortTasks 定义调度顺序：置顶 > 优先级高 > 创建时间早（FIFO）。
func sortTasks(tasks []*model.Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		return a.CreatedAt < b.CreatedAt
	})
}

// Pop 取出下一个可执行任务。
//   - 跳过链上已有任务在跑的（每链串行，保证会话上下文顺序）
//   - 跳过不在 queued 状态的任务
//
// 返回 nil 表示当前无可执行任务。
func (q *Queue) Pop() *model.Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	list := make([]*model.Task, 0, len(q.items))
	for _, t := range q.items {
		list = append(list, t)
	}
	sortTasks(list)
	for _, t := range list {
		if t.State != model.StateQueued {
			continue
		}
		if t.ChainID != "" && q.chainBusy[t.ChainID] {
			continue
		}
		delete(q.items, t.ID)
		if t.ChainID != "" {
			q.chainBusy[t.ChainID] = true
		}
		cp := *t
		return &cp
	}
	return nil
}

// ReleaseChain 标记链空闲（任务结束时必须调用，否则该链会永久阻塞）。
func (q *Queue) ReleaseChain(chainID string) {
	if chainID == "" {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.chainBusy, chainID)
}

// BusyChains 返回当前占用的链数量。
func (q *Queue) BusyChains() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.chainBusy)
}

// Reorder 把任务优先级调整为新值（面板操作）。
func (q *Queue) Reorder(id string, priority int, pinned bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	t, ok := q.items[id]
	if !ok {
		return fmt.Errorf("任务不在队列中: %s", id)
	}
	t.Priority = priority
	t.Pinned = pinned
	return nil
}
