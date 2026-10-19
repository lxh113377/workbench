package modelpool

import (
	"context"
	"errors"
	"sync"
	"testing"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
)

// recordingRunner 记录每次调用的模型 ID，可让指定模型失败。
type recordingRunner struct {
	mu       sync.Mutex
	attempts []string
	failFor  map[string]bool
}

func (r *recordingRunner) UpdateConfig(c config.ExecutorConfig) {}

func (r *recordingRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	r.mu.Lock()
	r.attempts = append(r.attempts, modelID)
	fail := r.failFor[modelID]
	r.mu.Unlock()
	if fail {
		return &executor.Result{ErrorKind: string(executor.KindModelDead), ExitCode: 1}, errors.New("模型不可用（模拟）")
	}
	return &executor.Result{SessionID: "s", FinalText: "ok from " + modelID, ExitCode: 0}, nil
}

func (r *recordingRunner) Inner() executor.RunnerContract { return nil }

func (r *recordingRunner) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.attempts...)
}

// TestFailoverChainCoversLanes 是老大诉求的守护测试：
// 增加可支持的模型，一个挂了另一个顶上。
//
// 判据：
//  1. 前 4 个主力模型（free/eac 车道）全部失败时，第 5 个必须被调用；
//  2. 第 5 个必须是另一条车道的模型（跨车道接力，不被 IP 封禁一锅端）；
//  3. 只要有一个成功，上层看到的就是成功（用户无感）。
func TestFailoverChainCoversLanes(t *testing.T) {
	store := NewStore(t.TempDir())
	mgr := NewManager(store, []Spec{
		laneSpec("m1", 1, 100, "free"),
		laneSpec("m2", 2, 60, "free"),
		laneSpec("m3", 3, 40, "eac"),
		laneSpec("m4", 4, 50, "eac"),
		laneSpec("m5", 12, 30, "kilo"),
		laneSpec("m6", 25, 30, "kilo"),
	})
	sch := NewScheduler(StrategyPriority)

	// 前 4 个全部失败（模拟 IP 封禁打光 free/eac 车道）
	rec := &recordingRunner{failFor: map[string]bool{"m1": true, "m2": true, "m3": true, "m4": true}}
	fr := NewFailoverRunner(rec, mgr, sch)

	task := &model.Task{ID: "t-chain", Title: "接力链验证", State: model.StateQueued}
	res, err := fr.Run(context.Background(), task, "m1")
	if err != nil {
		t.Fatalf("第 5 个模型（kilo 车道）应顶上，却整体失败: %v", err)
	}
	if res == nil || res.FinalText == "" {
		t.Fatal("上层应看到成功结果")
	}

	seen := rec.seen()
	if len(seen) < 5 {
		t.Fatalf("至少应尝试 5 个模型，实际 %d: %v", len(seen), seen)
	}
	if seen[4] != "m5" {
		t.Fatalf("第 5 次应切到 kilo 车道 m5，实际 %q（链: %v）", seen[4], seen)
	}
}

// TestFailoverStopsAtFirstSuccess 接力到第一个成功就停，不浪费额度。
func TestFailoverStopsAtFirstSuccess(t *testing.T) {
	store := NewStore(t.TempDir())
	mgr := NewManager(store, []Spec{
		laneSpec("a", 1, 100, "free"),
		laneSpec("b", 2, 60, "kilo"),
		laneSpec("c", 3, 30, "kilo"),
	})
	sch := NewScheduler(StrategyPriority)
	rec := &recordingRunner{failFor: map[string]bool{"a": true}}
	fr := NewFailoverRunner(rec, mgr, sch)

	task := &model.Task{ID: "t-stop", Title: "首个成功即停", State: model.StateQueued}
	if _, err := fr.Run(context.Background(), task, "a"); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.seen()); n != 2 {
		t.Fatalf("首个成功即停，应只调 2 次，实际 %d: %v", n, rec.seen())
	}
}

// laneSpec 造一个指定车道的健康模型。
func laneSpec(id string, pri, weight int, lane Lane) Spec {
	s := spec(id, pri, weight, StateHealthy)
	s.Lane = lane
	return s
}
