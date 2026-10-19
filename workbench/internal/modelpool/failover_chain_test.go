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

// timeoutRunner 模拟「模型超时不返回」：Result.TimedOut=true 且无 ErrorKind。
// 这是需求3 明确点名的触发条件之一（不可用 / 超时 / 报错），
// 此前只测了 model_unavailable 与 upstream_failed 两类，超时这条没有故障转移级证据。
type timeoutRunner struct {
	mu       sync.Mutex
	attempts []string
	timeout  map[string]bool
}

func (r *timeoutRunner) UpdateConfig(config.ExecutorConfig) {}

func (r *timeoutRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	r.mu.Lock()
	r.attempts = append(r.attempts, modelID)
	hit := r.timeout[modelID]
	r.mu.Unlock()
	if hit {
		// 执行器超时路径：不返回 error，只把 TimedOut 置真（与 executor.FinalizeResult 一致）
		return &executor.Result{TimedOut: true, ExitCode: -1}, nil
	}
	return &executor.Result{SessionID: "s", FinalText: "ok from " + modelID, ExitCode: 0}, nil
}

func (r *timeoutRunner) Inner() executor.RunnerContract { return nil }

func (r *timeoutRunner) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.attempts...)
}

// TestFailoverSwitchesOnTimeout 锁住需求3 的「超时」触发路径：
// 主模型超时必须立刻换下一个模型顶上，且对上层表现为成功（需求4 无感）。
func TestFailoverSwitchesOnTimeout(t *testing.T) {
	store := NewStore(t.TempDir())
	mgr := NewManager(store, []Spec{
		laneSpec("slow", 1, 100, "free"),
		laneSpec("fast", 2, 60, "kilo"),
	})
	sch := NewScheduler(StrategyPriority)
	run := &timeoutRunner{timeout: map[string]bool{"slow": true}}
	fr := NewFailoverRunner(run, mgr, sch)

	task := &model.Task{ID: "t-timeout", Title: "超时接力验证", State: model.StateQueued}
	res, err := fr.Run(context.Background(), task, "slow")
	if err != nil {
		t.Fatalf("超时后应由 fast 顶上并整体成功，实际 err=%v", err)
	}
	if res == nil || res.FinalText == "" {
		t.Fatalf("上层必须看到成功产出（需求4 无感），实际 %+v", res)
	}
	if res.TimedOut {
		t.Fatal("返回给上层的结果不得仍是超时态")
	}
	seen := run.seen()
	if len(seen) != 2 || seen[0] != "slow" || seen[1] != "fast" {
		t.Fatalf("应先试 slow 再切 fast，实际 %v", seen)
	}
	// 超时属可归因到模型的失败：slow 必须被记失败并进入冷却
	if h := healthOf(t, mgr, "slow"); h.Failure == 0 || h.ConsecFail == 0 {
		t.Fatalf("超时模型应被记失败，实际 %+v", h)
	}
	// 且状态日志留下可追溯的切换记录
	found := false
	for _, l := range store.TailLog(50) {
		if contains(l, "\"kind\":\"switch\"") && contains(l, "\"from\":\"slow\"") && contains(l, "\"to\":\"fast\"") {
			found = true
		}
	}
	if !found {
		t.Fatal("状态日志应记录 slow→fast 的超时切换")
	}
}

// TestFailoverAllTimeoutKeepsFailureFact 锁住兜底反面：全部超时时，
// 交回引擎的东西必须携带「超时」这一失败事实，绝不得伪装成成功。
//
// 契约措辞要说准：不是「error 必须非空」。执行器的超时路径本就是
// Result{TimedOut:true} + error==nil（见 executor.FinalizeResult），
// 引擎在 runOne 里用 res.TimedOut 分支判失败并 RecordFailure。
// 我第一版把断言写成 err==nil 即失败，是照抄了 failover.go 里
// 一句不准确的旧注释（"把最后一个错误抛回引擎"）—— 已连带补正该注释。
func TestFailoverAllTimeoutKeepsFailureFact(t *testing.T) {
	store := NewStore(t.TempDir())
	mgr := NewManager(store, []Spec{laneSpec("s1", 1, 100, "free"), laneSpec("s2", 2, 60, "kilo")})
	sch := NewScheduler(StrategyPriority)
	run := &timeoutRunner{timeout: map[string]bool{"s1": true, "s2": true}}
	fr := NewFailoverRunner(run, mgr, sch)
	res, _ := fr.Run(context.Background(), &model.Task{ID: "t-all"}, "s1")
	if res == nil {
		t.Fatal("必须把最后一次的执行结果交回引擎（带失败事实），不得返回空")
	}
	if !res.TimedOut {
		t.Fatalf("交回的结果必须保留超时事实，否则引擎会误判成功：%+v", res)
	}
	if res.FinalText != "" {
		t.Fatalf("超时不得带产出，否则被当成成功：%q", res.FinalText)
	}
	// 引擎侧同构判定：res.TimedOut 为真即判失败（这里直接复算，锁住契约两端一致）
	if res.TimedOut == false || res.ExitCode == 0 {
		t.Fatal("引擎守卫条件失效")
	}
	if n := len(run.seen()); n != 2 {
		t.Fatalf("应试过 2 个模型后放弃，实际 %d: %v", n, run.seen())
	}
	// 两个超时模型都必须被记失败（否则下次还会选中它们）
	for _, id := range []string{"s1", "s2"} {
		if h := healthOf(t, mgr, id); h.Failure == 0 || h.ConsecFail == 0 {
			t.Fatalf("%s 超时后应被记失败，实际 %+v", id, h)
		}
	}
}
