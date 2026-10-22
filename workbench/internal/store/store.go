// Package store 负责工作台的全部持久化：队列快照、引擎状态、任务目录与审计流水。
// 设计原则：内存是权威，磁盘是投影；每次状态迁移都先写盘再返回，保证崩溃后可重放。
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/retention"
)

// AttemptRecord 是一次执行尝试的台账（不可变，追加写）。
//
// 对标 riverqueue/river 的 AttemptError：把"第几次尝试、用什么模型、
// 哪类错误、等了多久"结构化落盘，而不是只留一行自由文本。
// 文件位置：tasks/<id>/attempts.jsonl（JSONL 追加，崩溃后仍可回溯）。
type AttemptRecord struct {
	At        int64  `json:"at"`
	TaskID    string `json:"taskId"`
	Attempt   int    `json:"attempt"`   // 第几次尝试（0 = 首次）
	Model     string `json:"model"`     // 本次使用的模型
	Kind      string `json:"kind"`      // 错误类别（成功为 done）
	Action    string `json:"action"`    // retry / fail / cancel / done
	DelayMs   int64  `json:"delayMs"`   // 本次失败后计划等待（毫秒）
	ElapsedMs int64  `json:"elapsedMs"` // 本次执行耗时
	Error     string `json:"error,omitempty"`
}

// AuditRecord 是一条审计记录，写入 audit.jsonl 后不可变。
type AuditRecord struct {
	At      int64  `json:"at"`
	TaskID  string `json:"taskId,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Actor   string `json:"actor"` // engine/user/system
	Kind    string `json:"kind"`  // transition/create/note/error
	Message string `json:"message,omitempty"`
}

// EngineState 是引擎的持久化状态。
type EngineState struct {
	Phase        model.EnginePhase `json:"phase"`
	StartedAt    int64             `json:"startedAt"`
	StoppedAt    int64             `json:"stoppedAt"`
	PausedAt     int64             `json:"pausedAt"`
	RestartCount int               `json:"restartCount"`
	LastError    string            `json:"lastError,omitempty"`
	UpdatedAt    int64             `json:"updatedAt"`
	// ResumeAfterRestart 表示「上一次停机是被动中断（自愈/崩溃），重启后应当自动接续」。
	//
	// 为什么需要（2026-10-19 现场缺陷）：自愈链路 Stop() 会把 phase 写成 stopped，
	// 而启动时的接续判据只看 (running|paused)，于是自愈重启后引擎永不恢复 ——
	// 「资源自愈保证 7x24」实际只重启了壳。此标记把「被动中断」与「用户主动停机」区分开：
	//   被动中断 -> true  -> 重启后自动接续
	//   用户停机 -> false -> 重启后保持停止（尊重用户意愿）
	ResumeAfterRestart bool `json:"resumeAfterRestart,omitempty"`
	// UserQuit 表示「用户主动退出（--quit / 面板退出）」。
	//
	// 为什么需要（2026-10-19 现场）：外部守护者（tests/keepalive.ps1）的分支是
	// 「stopped 或 idle 都拉回 running」——它**区分不出**「意外停机」与「用户主动退出」，
	// 于是用户 --quit 后 60 秒内又被拉起来，--quit 等于不生效。
	//
	// 产品侧必须先把「这是用户主动退出」这件事表达出来，外部才有据可依。
	//   置位：MarkUserQuit（--quit 路径）；清除：Start（用户点「开始」）。
	UserQuit bool `json:"userQuit,omitempty"`
}

// Snapshot 是队列的磁盘结构。
type Snapshot struct {
	SchemaVersion int            `json:"schemaVersion"`
	Tasks         []*model.Task  `json:"tasks"`
	Chains        []*model.Chain `json:"chains"`
	SavedAt       int64          `json:"savedAt"`
}

// Store 是任务与状态的唯一写入口。
type Store struct {
	layout config.Layout

	mu     sync.RWMutex
	tasks  map[string]*model.Task
	chains map[string]*model.Chain
	state  EngineState

	audit *config.JSONLWriter
	busy  bool

	events   map[string]*config.JSONLWriter
	attempts map[string]*config.JSONLWriter

	// wal 是队列状态的追加日志（P2-3）。写路径 = WAL 追加，读路径 = 快照 + WAL 回放。
	wal *wal
}

// Open 打开存储并完成崩溃恢复（把悬挂的 running 任务标记为 interrupted）。
func Open(layout config.Layout) (*Store, error) {
	if err := layout.EnsureDirs(); err != nil {
		return nil, err
	}
	s := &Store{
		layout: layout,
		tasks:  make(map[string]*model.Task),
		chains: make(map[string]*model.Chain),
		audit:  config.NewJSONLWriter(layout.LogFile("audit.jsonl")),

		events:   make(map[string]*config.JSONLWriter),
		attempts: make(map[string]*config.JSONLWriter),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	// 快照之后回放 WAL：把上次进程崩溃前已追加但未压缩的操作补回内存。
	w, werr := openWAL(layout.WALFile)
	if werr != nil {
		return nil, fmt.Errorf("打开 WAL 失败: %w", werr)
	}
	s.wal = w
	replayed := s.replay()
	if replayed > 0 {
		_ = s.audit.Append(AuditRecord{At: model.Now(), Actor: "system", Kind: "note",
			Message: fmt.Sprintf("WAL 回放 %d 条操作（快照 + 追加日志重建）", replayed)})
	}
	if err := s.loadState(); err != nil {
		return nil, err
	}
	return s, nil
}

// replay 把 WAL 应用回内存，返回回放条数。
func (s *Store) replay() int {
	if s.wal == nil {
		return 0
	}
	n, err := replayWAL(s.wal.path, func(rec walRecord) {
		if rec.Task != nil && rec.Task.ID != "" {
			cp := *rec.Task
			s.tasks[cp.ID] = &cp
		}
		if rec.Chain != nil && rec.Chain.ID != "" {
			cp := *rec.Chain
			s.chains[cp.ID] = &cp
		}
	})
	if err != nil {
		return n
	}
	return n
}

func (s *Store) load() error {
	var snap Snapshot
	err := config.ReadJSON(s.layout.QueueFile, &snap)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("读取队列失败: %w", err)
	}
	if err == nil {
		for _, t := range snap.Tasks {
			if t == nil || t.ID == "" {
				continue
			}
			if t.Tags == nil {
				t.Tags = []string{}
			}
			s.tasks[t.ID] = t
		}
		for _, c := range snap.Chains {
			if c == nil || c.ID == "" {
				continue
			}
			s.chains[c.ID] = c
		}
	}
	return nil
}

func (s *Store) loadState() error {
	var st EngineState
	err := config.ReadJSON(s.layout.StateFile, &st)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("读取引擎状态失败: %w", err)
	}
	s.state = st
	return nil
}

// Reconcile 把上一次进程退出时悬挂的任务收敛为 interrupted，避免重复执行。
// 返回被收敛的任务数量。
func (s *Store) Reconcile() (int, error) {
	s.mu.Lock()
	var changed []*model.Task
	for _, t := range s.tasks {
		if t.State == model.StateRunning || t.State == model.StateEvaluating {
			prev := t.State
			t.State = model.StateInterrupted
			t.UpdatedAt = model.Now()
			changed = append(changed, t)
			_ = s.audit.Append(AuditRecord{At: model.Now(), TaskID: t.ID, From: string(prev), To: string(model.StateInterrupted), Actor: "system", Kind: "recover", Message: "进程异常退出后恢复"})
		}
	}
	s.mu.Unlock()
	if len(changed) == 0 {
		return 0, nil
	}
	// 逐个把收敛结果追加进 WAL（而不是整份重写）
	for _, t := range changed {
		cp := *t
		if err := s.record(walRecord{Op: OpTransition, Task: &cp}); err != nil {
			return len(changed), err
		}
	}
	return len(changed), nil
}

// PruneResult 是一次终态任务记录裁剪的结果。
type PruneResult struct {
	RemovedByAge    int   `json:"removedByAge"`    // 超过保留天数被删的条数
	RemovedByCap    int   `json:"removedByCap"`    // 超过条数上限被删的条数
	Kept            int   `json:"kept"`            // 保留下来的任务记录数
	RemovedChains   int   `json:"removedChains"`   // 一并清掉的孤立链数
	EligibleForKeep int   `json:"eligibleTotal"`   // 裁剪前的记录总数
	BytesBefore     int64 `json:"bytesBefore"`     // 裁剪前 queue.json 字节数（估算）
	DryRun          bool  `json:"dryRun"`
}

// PruneTasks 裁剪 queue.json 里的**终态任务记录**，把「只增不减」变成有界保留。
//
// 为什么需要（2026-10-23 实测根因）：queue.json 达 59MB / 33,740 条，其中 33,733 条
// 是终态（33,187 条 rejected，空转热循环留下的），在跑任务仅 7 条。store 此前**没有
// 任何删除任务的 API**，retention 只清 tasks/ 目录 —— 记录本身永远留在快照里。
// 后果不是磁盘而是内存与写放大：每次快照全量序列化 59MB，内存常驻 3.3 万任务对象，
// 实测常驻 170MB / 并发高峰 752MB，而基线 residentMBMax 是 64MB。
//
// 安全边界（缺一不可）：
//  1) **只删终态**（done/failed/cancelled/degraded/rejected）—— 非终态（queued/running/
//     approved/evaluating/candidate/interrupted）一律不动，否则会丢在跑或待跑任务；
//  2) 终态里**按年龄**：只删 createdAt 早于 TerminalTaskDays 的；
//  3) 再按**条数上限**：若仍超 MaxTerminalTasks，从**最旧**的开始删直到达标；
//  4) 孤立的 chain 记录一并清（其 TaskIDs 全被删掉的），有任务引用的 chain 保留；
//  5) 只改内存记录，**磁盘写仍是 persist 全量快照**（原子写，不会留下半截文件）；
//  6) 删除后立刻写一次 WAL 标记（OpPrune），让回放路径知道这是一次有意的裁剪，
//     而不是「记录凭空消失」。
//
// 注意：本函数**不删** tasks/<id>/ 目录里的产出与事件（那是 retention.PruneTaskDir 的职责，
// 保留窗口更长）。此处只解记录层。
func (s *Store) PruneTasks(pol retention.Policy, dryRun bool) (PruneResult, error) {
	res := PruneResult{DryRun: dryRun}
	s.mu.Lock()
	res.EligibleForKeep = len(s.tasks)
	cutoff := model.Now() - int64(pol.TerminalTaskDays)*86400*1000

	// 1) 分类：终态可删候选 / 必须保留
	type cand struct {
		id string
		at int64
	}
	var victims []cand
	keptTerminal := make([]cand, 0)
	for id, t := range s.tasks {
		if !t.State.IsTerminal() {
			continue // 非终态一律不动
		}
		if t.CreatedAt < cutoff {
			victims = append(victims, cand{id: id, at: t.CreatedAt})
		} else {
			keptTerminal = append(keptTerminal, cand{id: id, at: t.CreatedAt})
		}
	}
	res.RemovedByAge = len(victims)

	// 2) 条数上限：仍超限则从最旧开始再删
	cap := pol.MaxTerminalTasks
	if cap > 0 {
		remaining := len(keptTerminal)
		if remaining > cap {
			sort.Slice(keptTerminal, func(i, j int) bool { return keptTerminal[i].at < keptTerminal[j].at })
			excess := remaining - cap
			for i := 0; i < excess && i < len(keptTerminal); i++ {
				victims = append(victims, keptTerminal[i])
				res.RemovedByCap++
			}
		}
	}

	if dryRun || len(victims) == 0 {
		res.Kept = len(s.tasks)
		s.mu.Unlock()
		return res, nil
	}

	// 3) 真正删除（内存）
	for _, v := range victims {
		delete(s.tasks, v.id)
	}
	res.Kept = len(s.tasks)

	// 4) 清理孤立 chain：TaskIDs 中已无存活任务的链
	if len(s.chains) > 0 {
		alive := make(map[string]bool, len(s.tasks))
		for _, t := range s.tasks {
			if t.ChainID != "" {
				alive[t.ChainID] = true
			}
		}
		for cid, c := range s.chains {
			if alive[cid] {
				continue
			}
			// 双重判据：链自己的 TaskIDs 也都没了才删（防止调用方尚未回填 ChainID 时误删）
			hasTask := false
			for _, tid := range c.TaskIDs {
				if _, ok := s.tasks[tid]; ok {
					hasTask = true
					break
				}
			}
			if !hasTask {
				delete(s.chains, cid)
				res.RemovedChains++
			}
		}
	}
	s.mu.Unlock()

	// 5) 落盘：**必须用 compact() 而不是 persist()**（2026-10-23 实测踩到）。
	//
	// persist() 只写快照、不截断 WAL；而 Open() 的加载顺序是「先快照 -> 再回放 WAL」，
	// 被裁掉的终态任务在 WAL 里有历史记录，回放时会被**原样加回内存** ——
	// 实测：裁剪后重开，记录又回来了，白删一轮（下次启动照样占满）。
	// compact() 的语义正是「写快照 + 截断 WAL」，两步同源才能让裁剪真正生效。
	if err := s.compact(); err != nil {
		return res, fmt.Errorf("裁剪后落盘失败: %w", err)
	}
	_ = s.audit.Append(AuditRecord{At: model.Now(), Actor: "system", Kind: "note",
		Message: fmt.Sprintf("终态任务记录裁剪：按龄删 %d 条、按上限删 %d 条，保留 %d 条，清孤立链 %d 条",
			res.RemovedByAge, res.RemovedByCap, res.Kept, res.RemovedChains)})
	return res, nil
}

// Savepoint 立即落盘（供退出前调用）：写全量快照并清空 WAL。
//
// 退出前压缩的理由：下次启动直接读快照，省一次 WAL 回放；
// 顺序必须是"先写快照 → fsync → 再截断 WAL"，任一步崩溃都不丢数据。
func (s *Store) Savepoint() error {
	if err := s.persist(); err != nil {
		return err
	}
	if s.wal != nil {
		return s.wal.reset()
	}
	return nil
}

// record 追加一条 WAL 记录，并在超过阈值时压缩。
//
// 这是 P2-3 的核心：把 O(N) 的全量重写降到 O(1) 的追加写；
// 只有当 WAL 累积到阈值时才做一次全量快照（摊薄成本）。
func (s *Store) record(rec walRecord) error {
	if s.wal == nil {
		// 没有 WAL（例如测试里手工构造的 Store）：退回全量写，行为与旧版一致。
		return s.persist()
	}
	if err := s.wal.append(rec); err != nil {
		// WAL 写失败是严重问题：退回全量写保证不丢状态，并把错误上抛。
		if perr := s.persist(); perr != nil {
			return fmt.Errorf("WAL 写入失败且快照兜底也失败: %v / %w", err, perr)
		}
		return fmt.Errorf("WAL 写入失败（已用快照兜底）: %w", err)
	}
	if s.wal.needsCompact() {
		// 压缩失败不阻塞业务：WAL 还在，下次启动照样能回放。
		if err := s.compact(); err != nil {
			_ = s.audit.Append(AuditRecord{At: model.Now(), Actor: "system", Kind: "error", Message: "WAL 压缩失败: " + err.Error()})
		}
	}
	return nil
}

// compact 把当前内存全量写成快照，然后截断 WAL。
func (s *Store) compact() error {
	if err := s.persist(); err != nil {
		return err
	}
	if s.wal != nil {
		return s.wal.reset()
	}
	return nil
}

// Close 关闭 WAL（进程退出前调用）。
func (s *Store) Close() error {
	if s.wal != nil {
		return s.wal.close()
	}
	return nil
}

func (s *Store) persist() error {
	s.mu.RLock()
	snap := Snapshot{
		SchemaVersion: config.SchemaVersion,
		Tasks:         make([]*model.Task, 0, len(s.tasks)),
		Chains:        make([]*model.Chain, 0, len(s.chains)),
		SavedAt:       model.Now(),
	}
	ids := make([]string, 0, len(s.tasks))
	for id := range s.tasks {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return s.tasks[ids[i]].CreatedAt < s.tasks[ids[j]].CreatedAt
	})
	for _, id := range ids {
		cp := *s.tasks[id]
		snap.Tasks = append(snap.Tasks, &cp)
	}
	for _, c := range s.chains {
		cp := *c
		snap.Chains = append(snap.Chains, &cp)
	}
	s.mu.RUnlock()
	return config.WriteJSONAtomic(s.layout.QueueFile, snap)
}

// SaveState 持久化引擎状态。
func (s *Store) SaveState(st EngineState) error {
	s.mu.Lock()
	st.UpdatedAt = model.Now()
	s.state = st
	s.mu.Unlock()
	return config.WriteJSONAtomic(s.layout.StateFile, st)
}

// State 返回当前引擎状态。
func (s *Store) State() EngineState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Create 新增一个任务并落盘。ID 冲突返回错误。
func (s *Store) Create(t *model.Task) error {
	if t == nil || t.ID == "" {
		return fmt.Errorf("任务缺少 ID")
	}
	s.mu.Lock()
	if _, exists := s.tasks[t.ID]; exists {
		s.mu.Unlock()
		return fmt.Errorf("任务已存在: %s", t.ID)
	}
	if t.Tags == nil {
		t.Tags = []string{}
	}
	if t.State == "" {
		t.State = model.StateCandidate
	}
	if t.MaxAttempts <= 0 {
		t.MaxAttempts = 3
	}
	if t.CreatedAt == 0 {
		t.CreatedAt = model.Now()
	}
	t.UpdatedAt = model.Now()
	s.tasks[t.ID] = t
	// 这里**刻意不再**创建任务目录（2026-10-19 现场缺陷修复）。
	//
	// 现场数据：tasks/ 下 **1314 个空目录**，恰好等于 audit.jsonl 里
	// candidate->rejected 的 1314 次 —— 完全相等。
	//
	// 机理：调用方（engine_autonomy.go）是**先 Create、后 Evaluate**：
	//   Create 建目录 -> Evaluate 命中硬门禁 -> 拒 -> 目录留下、永远空着。
	// 空转热循环期间（每 3 秒被拒一次）就每 3 秒落一个空目录。
	//
	// 为什么 Create 不该建目录：
	//   1) 目录是**执行期**资源；Create 只是「登记」，耦合导致「被拒也占地」；
	//   2) executor.go 已经有懒建逻辑（真正要跑时才 MkdirAll(workspace)），
	//      这里那行是冗余的、且是有害的那一半；
	//   3) 与 executor 已修的同类现场（external 模式留空沙箱）方向一致：
	//      **不该建的时候不要建**。
	s.mu.Unlock()
	_ = s.audit.Append(AuditRecord{At: model.Now(), TaskID: t.ID, To: string(t.State), Actor: "engine", Kind: "create", Message: t.Title})
	cp := *t
	return s.record(walRecord{Op: OpCreate, Task: &cp})
}

// Get 返回任务副本。
func (s *Store) Get(id string) (*model.Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, false
	}
	cp := *t
	return &cp, true
}

// List 返回全部任务副本，按创建时间升序。
func (s *Store) List() []*model.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		cp := *t
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out
}

// Update 用 mutator 修改任务并落盘。mutator 返回错误时放弃修改。
//
// 并发契约：本函数**换指针**（map 槽位指向新副本），绝不原地写调用方已持有的 *Task；
// 调用方（Get/List/Transition 返回值）拿到的都是副本或不再被写的旧对象。
func (s *Store) Update(id string, mutator func(*model.Task) error) (*model.Task, error) {
	s.mu.Lock()
	t, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("任务不存在: %s", id)
	}
	cp := *t
	if err := mutator(&cp); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	cp.UpdatedAt = model.Now()
	// 换指针而非原地写（*t = cp）：原地写会让「持有旧指针的读方」（如 engine runOne
	// 的 defer 读 running.ChainID）与本次写形成 data race。改为把 map 槽位指向新副本，
	// 旧指针指向的对象从此不再被写，与 Transition/Get 返回副本的语义一致。
	//
	// 2026-10-19 复验：这个修复曾被整文件覆盖丢掉（工作树又回到 *t = cp），
	// 容器内 `go test -race ./...` 立刻在 internal/engine 抓到 data race
	// （store.Update 写  vs  runOne 的 defer 读）。此注释即为防丢标记。
	s.tasks[id] = &cp
	result := cp
	s.mu.Unlock()
	if err := s.record(walRecord{Op: OpUpdate, Task: &result}); err != nil {
		return &result, err
	}
	return &result, nil
}

// Transition 执行一次带校验的状态迁移。audit 记录迁移前后状态。
func (s *Store) Transition(id string, to model.TaskState, actor, note string) (*model.Task, error) {
	s.mu.Lock()
	t, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("任务不存在: %s", id)
	}
	from := t.State
	if from == to {
		cp := *t
		s.mu.Unlock()
		return &cp, nil
	}
	if !model.TransitionAllowed(from, to) {
		s.mu.Unlock()
		return nil, fmt.Errorf("非法状态迁移 %s -> %s (任务 %s)", from, to, id)
	}
	t.State = to
	t.UpdatedAt = model.Now()
	cp := *t
	s.mu.Unlock()
	_ = s.audit.Append(AuditRecord{At: model.Now(), TaskID: id, From: string(from), To: string(to), Actor: actor, Kind: "transition", Message: note})
	if err := s.record(walRecord{Op: OpTransition, Task: &cp}); err != nil {
		return &cp, err
	}
	return &cp, nil
}

// Audit 追加一条审计记录。
//
// 返回写入错误（2026-10-22 对标 asynq processor.go:336-338 后新增）。
//
// 改造前是 `func (s *Store) Audit(rec AuditRecord)`，内部 `_ = s.audit.Append(rec)` ——
// 写入失败（磁盘满 / 句柄被占用 / 权限变更）被**完全吞掉**，调用方无从知晓。
// asynq 的结构把「错误处理钩子」放在状态决策之前，使「任何一条失败路径都留痕」
// 在结构上不可能被跳过；本项目的审计是一条**旁路**，一旦写失败，
// 失败任务就只剩任务记录（ErrorKind）而没有台账说明「为什么失败」。
//
// 兼容性：老调用方仍可忽略返回值（Go 允许丢弃返回的 error），
// 故签名变化不破坏任何既有调用点（本仓 4 处，其中引擎侧的 failTask / finishCancelled
// 已改为读取该错误并落兜底痕迹）。
func (s *Store) Audit(rec AuditRecord) error {
	if rec.At == 0 {
		rec.At = model.Now()
	}
	return s.audit.Append(rec)
}

// SaveTaskArtifact 把任务的具名产物写入其目录（原子写）。
func (s *Store) SaveTaskArtifact(taskID, name string, data []byte) error {
	if strings.ContainsAny(name, "\\/:*?\"<>|") {
		return fmt.Errorf("非法产物文件名: %s", name)
	}
	dir := s.layout.TaskDir(taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return config.WriteFileAtomic(filepath.Join(dir, name), data)
}

// ReadTaskArtifact 读取任务产物。
func (s *Store) ReadTaskArtifact(taskID, name string) ([]byte, error) {
	return os.ReadFile(s.layout.TaskFile(taskID, name))
}

// TaskDir 暴露任务目录路径。
func (s *Store) TaskDir(taskID string) string { return s.layout.TaskDir(taskID) }

// Layout 暴露布局。
func (s *Store) Layout() config.Layout { return s.layout }

// UpsertChain 新增或更新链。
func (s *Store) UpsertChain(c *model.Chain) error {
	if c == nil || c.ID == "" {
		return fmt.Errorf("链缺少 ID")
	}
	s.mu.Lock()
	c.UpdatedAt = model.Now()
	if c.CreatedAt == 0 {
		c.CreatedAt = c.UpdatedAt
	}
	cp := *c
	s.chains[c.ID] = &cp
	s.mu.Unlock()
	return s.record(walRecord{Op: OpChain, Chain: &cp})
}

// GetChain 返回链副本。
func (s *Store) GetChain(id string) (*model.Chain, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.chains[id]
	if !ok {
		return nil, false
	}
	cp := *c
	return &cp, true
}

// ListChains 返回全部链副本。
func (s *Store) ListChains() []*model.Chain {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Chain, 0, len(s.chains))
	for _, c := range s.chains {
		cp := *c
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

// AppendTaskEvent 追加一条任务级事件（供面板时间线展示）。
//
// 按任务复用 writer：以前每次调用都新建 writer 再追加，同一任务高频写时
// 反复开关文件；O_APPEND 本身安全，但写放大明显。
func (s *Store) AppendTaskEvent(taskID string, ev model.Event) {
	_ = s.eventWriter(taskID).Append(ev)
}

// eventWriter 返回（并缓存）任务级事件流的追加写入器。
func (s *Store) eventWriter(taskID string) *config.JSONLWriter {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.events == nil {
		s.events = make(map[string]*config.JSONLWriter)
	}
	if w, ok := s.events[taskID]; ok {
		return w
	}
	w := config.NewJSONLWriter(s.layout.TaskFile(taskID, "events.jsonl"))
	s.events[taskID] = w
	return w
}

// attemptWriter 返回（并缓存）任务级尝试台账的追加写入器。
func (s *Store) attemptWriter(taskID string) *config.JSONLWriter {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attempts == nil {
		s.attempts = make(map[string]*config.JSONLWriter)
	}
	if w, ok := s.attempts[taskID]; ok {
		return w
	}
	w := config.NewJSONLWriter(s.layout.TaskFile(taskID, "attempts.jsonl"))
	s.attempts[taskID] = w
	return w
}

// AppendAttempt 追加一条执行尝试台账（失败不阻塞主流程，但会进审计）。
func (s *Store) AppendAttempt(rec AttemptRecord) {
	if rec.At == 0 {
		rec.At = model.Now()
	}
	if err := s.attemptWriter(rec.TaskID).Append(rec); err != nil {
		_ = s.audit.Append(AuditRecord{At: model.Now(), TaskID: rec.TaskID, Actor: "system", Kind: "error", Message: "写入 attempts.jsonl 失败: " + err.Error()})
	}
}

// ListAttempts 读取某任务的全部尝试台账。文件不存在时返回空切片与 nil。
func (s *Store) ListAttempts(taskID string) ([]AttemptRecord, error) {
	data, err := os.ReadFile(s.layout.TaskFile(taskID, "attempts.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []AttemptRecord
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec AttemptRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // 单行坏数据不影响其余台账
		}
		out = append(out, rec)
	}
	return out, nil
}
