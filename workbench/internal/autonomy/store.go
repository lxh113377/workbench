package autonomy

// 本文件实现「目标树与决策台账的持久化」：goals.json（当前状态，原子写）
// + decisions.jsonl（追加写，不可变）。
//
// 为什么拆两个文件：
//   - goals.json 是"现在是什么状态"，需要整体原子替换，崩溃后不出现半截目标树；
//   - decisions.jsonl 是"为什么变成这样"，必须只追加、可回溯、可审计——
//     自主决策的黑箱问题是这类系统最大的信任缺口，台账是唯一的解释面。

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"workbench/internal/config"
)

// GoalsSchemaVersion 是 goals.json 的结构版本（结构变更时 +1 并写迁移）。
const GoalsSchemaVersion = 1

// GoalsMax 是目标树保留上限：超出后归档最老的已完成/放弃目标，防止文件无限膨胀。
const GoalsMax = 60

// DecisionsKeepLines 是决策台账保留行数上限（超出后截断最旧一半）。
const DecisionsKeepLines = 20000

// Store 是自主目标与决策的持久化仓储。
type Store struct {
	mu        sync.Mutex
	goalsPath string
	decPath   string
	file      GoalsFile
}

// OpenStore 打开（或初始化）目标仓储。
//
// 容错约定：goals.json 不存在 => 建空文件；损坏 => 备份为 .corrupt-<ts> 后重建空树，
// 不阻断工作台启动（自主子系统是增强项，不能让损坏文件拖垮主流程）。
func OpenStore(goalsPath, decisionsPath string) (*Store, error) {
	s := &Store{goalsPath: goalsPath, decPath: decisionsPath}
	if err := os.MkdirAll(filepath.Dir(goalsPath), 0o755); err != nil {
		return nil, fmt.Errorf("创建自主目标目录失败: %w", err)
	}
	var f GoalsFile
	err := config.ReadJSON(goalsPath, &f)
	switch {
	case err == nil:
		// ok
	case errors.Is(err, os.ErrNotExist):
		f = GoalsFile{SchemaVersion: GoalsSchemaVersion}
	default:
		// 损坏：备份后重建，绝不让一个坏文件阻断启动。
		bak := fmt.Sprintf("%s.corrupt-%d", goalsPath, nowMs())
		_ = os.Rename(goalsPath, bak)
		f = GoalsFile{SchemaVersion: GoalsSchemaVersion}
	}
	if f.SchemaVersion <= 0 {
		f.SchemaVersion = GoalsSchemaVersion
	}
	if f.Goals == nil {
		f.Goals = []*Goal{}
	}
	// 归一化：历史文件/手改文件可能缺 slices，统一补空数组，保证 API 返回 [] 而不是 null。
	for _, g := range f.Goals {
		if g == nil {
			continue
		}
		if g.Criteria == nil {
			g.Criteria = []Criterion{}
		}
		if g.Stages == nil {
			g.Stages = []Stage{}
		}
		if g.Evidence == nil {
			g.Evidence = []Evidence{}
		}
	}
	s.file = f
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

// nowMs 返回毫秒时间戳（本包不依赖 model 的时间函数，保持纯逻辑可测）。
func nowMs() int64 { return time.Now().UnixMilli() }

// persistLocked 原子写 goals.json（调用方已持锁）。
func (s *Store) persistLocked() error {
	s.file.SchemaVersion = GoalsSchemaVersion
	s.file.SavedAt = nowMs()
	return config.WriteJSONAtomic(s.goalsPath, s.file)
}

// Goals 返回目标列表副本（按创建时间升序）。
func (s *Store) Goals() []*Goal {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Goal, 0, len(s.file.Goals))
	for _, g := range s.file.Goals {
		if g != nil {
			out = append(out, cloneGoal(g))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

// ActiveID 返回当前活跃目标 ID。
func (s *Store) ActiveID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.ActiveID
}

// Get 按 ID 取目标副本。
func (s *Store) Get(id string) (*Goal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range s.file.Goals {
		if g != nil && g.ID == id {
			return cloneGoal(g), true
		}
	}
	return nil, false
}

// Active 返回当前活跃目标副本（无活跃目标时返回 nil,false）。
func (s *Store) Active() (*Goal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range s.file.Goals {
		if g != nil && g.ID == s.file.ActiveID {
			return cloneGoal(g), true
		}
	}
	return nil, false
}

// Upsert 写入/替换一个目标并落盘（按 ID 匹配）。
func (s *Store) Upsert(g *Goal) error {
	if g == nil || g.ID == "" {
		return fmt.Errorf("目标缺少 ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for i, cur := range s.file.Goals {
		if cur != nil && cur.ID == g.ID {
			s.file.Goals[i] = cloneGoal(g)
			found = true
			break
		}
	}
	if !found {
		s.file.Goals = append(s.file.Goals, cloneGoal(g))
	}
	s.enforceCapLocked()
	return s.persistLocked()
}

// SetActive 设置活跃目标（空串表示没有活跃目标）。
func (s *Store) SetActive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.file.ActiveID = id
	return s.persistLocked()
}

// enforceCapLocked 控制目标数量上限：超过 GoalsMax 时把最老的终态目标归档掉（只保留最近 GoalsMax 个）。
func (s *Store) enforceCapLocked() {
	if len(s.file.Goals) <= GoalsMax {
		return
	}
	sort.SliceStable(s.file.Goals, func(i, j int) bool {
		return s.file.Goals[i].CreatedAt < s.file.Goals[j].CreatedAt
	})
	// 只裁剪终态目标，活跃目标永不裁剪。
	excess := len(s.file.Goals) - GoalsMax
	out := make([]*Goal, 0, len(s.file.Goals))
	for _, g := range s.file.Goals {
		if excess > 0 && g != nil && g.ID != s.file.ActiveID && g.Status != GoalActive {
			excess--
			continue
		}
		out = append(out, g)
	}
	s.file.Goals = out
}

// ── 决策台账 ─────────────────────────────────────────────────────

// AppendDecision 追加一条决策记录（JSONL，只追加）。
func (s *Store) AppendDecision(d Decision) error {
	if d.At == 0 {
		d.At = nowMs()
	}
	if d.Actor == "" {
		d.Actor = "autonomy"
	}
	buf, err := json.Marshal(d)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.decPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.decPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(buf, '\n')); err != nil {
		return err
	}
	return nil
}

// Decisions 读取最近 limit 条决策（新→旧），limit<=0 时返回全部。
func (s *Store) Decisions(limit int) []Decision {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.decPath)
	if err != nil {
		return []Decision{}
	}
	defer f.Close()
	var all []Decision
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var d Decision
		if err := json.Unmarshal(line, &d); err != nil {
			continue
		}
		all = append(all, d)
	}
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	// 反转成新→旧
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	if all == nil {
		return []Decision{}
	}
	return all
}

// cloneGoal 深拷贝目标（切片必须复制，避免外部修改缓存）。
func cloneGoal(g *Goal) *Goal {
	if g == nil {
		return nil
	}
	cp := *g
	cp.Criteria = append([]Criterion{}, g.Criteria...)
	cp.Stages = append([]Stage{}, g.Stages...)
	cp.Evidence = append([]Evidence{}, g.Evidence...)
	return &cp
}
