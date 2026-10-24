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
	// now 注入排序时钟（毫秒）。生产走 model.Now，单测用它构造不同的等待档位；
	// 零值时 nowMs 退回 model.Now（见下）。
	now func() int64
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
	q.sortTasks(out)
	return out
}

// 防饿死 aging 参数（2026-10-25，对标 temporal IWRR 权重公平 + 调度理论 aging）：
//
// 缺陷：sortTasks 原是纯严格优先级（置顶 > 优先级 > FIFO）。7×24 自主循环持续产任务
// （链衍生、自主目标），低优任务在高优持续流入下可永久排队（饥饿）。生产优先级确实
// 异质（1-5：链用模板 PriorityWeight、自主目标固定 4、面板可设 1/5），非恒相等，故这是
// 真实结构缺陷而非理论担忧。
//
// 修法：有界 aging —— 有效优先级 = Priority + min(agingMaxBoost, 等待步数)。
// 有界是关键：当所有任务都比封顶更老时，全体获得相同 boost → 退化回严格优先级，
// 既修饥饿又不无谓打乱既有顺序（既有 TestPopRespectsPriorityAndFIFO 因此仍成立）。
const (
	agingStepMs   = int64(5) * 60 * 1000 // 每等 5 分钟，有效优先级 +1
	agingMaxBoost = 5                    // 封顶：够让 prio-1 追平并反超 fresh prio-5
)

// nowMs 返回排序基准时钟（毫秒）。生产走 model.Now；单测可注入 q.now 构造不同等待档位。
func (q *Queue) nowMs() int64 {
	if q.now != nil {
		return q.now()
	}
	return model.Now()
}

// effectivePriority = Priority + 按已等待时长的有界 aging 加成（负等待按 0 处理）。
func effectivePriority(t *model.Task, nowMs int64) int {
	wait := nowMs - t.CreatedAt
	if wait < 0 {
		wait = 0
	}
	boost := wait / agingStepMs
	if boost > agingMaxBoost {
		boost = agingMaxBoost
	}
	return t.Priority + int(boost)
}

// sortTasks 定义调度顺序：置顶 > 有效优先级（含防饿死 aging）> 创建时间早（FIFO）。
// 调用方（Pop/Snapshot）已持 q.mu，故直接读 q.nowMs()。
func (q *Queue) sortTasks(tasks []*model.Task) {
	nowMs := q.nowMs()
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		if ea, eb := effectivePriority(a, nowMs), effectivePriority(b, nowMs); ea != eb {
			return ea > eb
		}
		// 有效优先级相同：仍按原始优先级高者先（aging 只作追赶、不逆转同级内的既有偏序），
		// 再按创建时间早者先（FIFO）。
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
	q.sortTasks(list)
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
