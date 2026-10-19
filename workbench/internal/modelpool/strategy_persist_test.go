package modelpool

import "testing"

// TestStrategySurvivesRestart 锁住需求2/5 的一条硬承诺：
// 面板与 CLI 切换的调度策略必须跨进程生效。
//
// types.go 里 file.Strategy 字段的注释原文写着「必须落盘：否则 CLI 里
// --model-pool strategy 只改内存，下一个进程（面板或工作台）又用回默认值，
// “可配置”就成了假象」—— 但 persist() 从不给该字段赋值、也没有任何地方读它，
// 实测 5 份真实 pool.json 全部没有 strategy 键。这条测试把承诺钉住。
func TestStrategySurvivesRestart(t *testing.T) {
	store := NewStore(t.TempDir())
	svc := NewService(store, []Spec{spec("a", 10, 100, StateUnknown)}, ServiceConfig{
		Enabled: true, ProbeSeconds: 3600, SyncSeconds: 3600, Strategy: StrategyPriority,
	})
	if err := svc.SetStrategy(StrategyWeighted); err != nil {
		t.Fatal(err)
	}
	if got := svc.Scheduler().Strategy(); got != StrategyWeighted {
		t.Fatalf("切换后内存态应为 weighted，实际 %s", got)
	}
	// 模拟进程重启：同一个 store 再建一个 Service（与 main.go 的构造路径一致）。
	restored := NewService(store, nil, ServiceConfig{
		Enabled: true, ProbeSeconds: 3600, SyncSeconds: 3600, Strategy: StrategyPriority,
	})
	if got := restored.Scheduler().Strategy(); got != StrategyWeighted {
		t.Fatalf("重启后策略应仍是 weighted（已落盘），实际 %s —— 面板切换没跨进程生效", got)
	}
}

// TestStrategyPersistedIntoPoolFile 直接看账本：pool.json 必须有 strategy 键。
func TestStrategyPersistedIntoPoolFile(t *testing.T) {
	store := NewStore(t.TempDir())
	svc := NewService(store, []Spec{spec("a", 10, 100, StateUnknown)}, ServiceConfig{
		Enabled: true, ProbeSeconds: 3600, SyncSeconds: 3600, Strategy: StrategyPriority,
	})
	if err := svc.SetStrategy(StrategyLeastLoaded); err != nil {
		t.Fatal(err)
	}
	f, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if f.Strategy != string(StrategyLeastLoaded) {
		t.Fatalf("pool.json 的 strategy 应为 least-loaded，实际 %q", f.Strategy)
	}
}

// TestLedgerStrategyBeatsConfigDefault 钉住优先级语义（副作用也要有据可查）：
// 面板/CLI 只写账本，config.json 是出厂默认，所以账本必须赢。
// 反过来说：手工编辑 config.json 改策略不会生效，必须走面板/CLI 入口。
func TestLedgerStrategyBeatsConfigDefault(t *testing.T) {
	store := NewStore(t.TempDir())
	first := NewService(store, []Spec{spec("a", 10, 100, StateUnknown)}, ServiceConfig{
		Enabled: true, ProbeSeconds: 3600, SyncSeconds: 3600, Strategy: StrategyPriority,
	})
	if err := first.SetStrategy(StrategyRoundRobin); err != nil {
		t.Fatal(err)
	}
	// 第二个进程即使传了不同的配置默认值，也要接住账本里的 round-robin。
	second := NewService(store, nil, ServiceConfig{
		Enabled: true, ProbeSeconds: 3600, SyncSeconds: 3600, Strategy: StrategyWeighted,
	})
	if got := second.Scheduler().Strategy(); got != StrategyRoundRobin {
		t.Fatalf("账本值应优先于配置默认，实际 %s", got)
	}
	// 而 config 仍能在「账本从未被切换过」时生效（首次启动路径）。
	fresh := NewService(NewStore(t.TempDir()), []Spec{spec("a", 10, 100, StateUnknown)}, ServiceConfig{
		Enabled: true, ProbeSeconds: 3600, SyncSeconds: 3600, Strategy: StrategyWeighted,
	})
	if got := fresh.Scheduler().Strategy(); got != StrategyWeighted {
		t.Fatalf("无落盘值时应沿用配置默认，实际 %s", got)
	}
}

// TestGarbageLedgerStrategyFallsBack 锁住「账本被手改成垃圾值」时的兜底：
// 宁可退回配置默认，也不能让 Strategy() 报出 Pick 不认的策略。
func TestGarbageLedgerStrategyFallsBack(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Save(&file{Specs: []Spec{spec("a", 10, 100, StateUnknown)},
		Health: map[string]*Health{}, Strategy: "no-such-strategy"}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(store, nil, ServiceConfig{
		Enabled: true, ProbeSeconds: 3600, SyncSeconds: 3600, Strategy: StrategyPriority,
	})
	if got := svc.Scheduler().Strategy(); got != StrategyPriority {
		t.Fatalf("非法落盘值应退回配置默认 priority，实际 %s", got)
	}
	// 且 Pick 仍能正常工作（不会拿到一个不认识的策略分支）
	if id, _ := svc.Scheduler().Pick(svc.mgr.Specs(), nil, nowMs()); id != "a" {
		t.Fatalf("兜底后调度应正常，实际 %q", id)
	}
}
