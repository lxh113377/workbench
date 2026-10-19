package config

import (
	"os"
	"testing"
)

func TestDefaultConfigValid(t *testing.T) {
	c := DefaultConfig()
	if c.Executor.Concurrency < 1 {
		t.Fatal("default concurrency invalid")
	}
	if c.Eval.Threshold <= 0 {
		t.Fatal("default threshold invalid")
	}
	if len(c.Models) == 0 {
		t.Fatal("default models empty")
	}
}

func TestNormalizeClamps(t *testing.T) {
	c := DefaultConfig()
	c.Executor.Concurrency = 99
	c.Eval.Threshold = 999
	c.Chain.MaxDepth = 999
	c.Normalize()
	if c.Executor.Concurrency > 6 {
		t.Fatal("concurrency not clamped")
	}
	if c.Eval.Threshold > 100 {
		t.Fatal("threshold not clamped")
	}
	if c.Chain.MaxDepth > 20 {
		t.Fatal("chain depth not clamped")
	}
}

func TestConfigStorePersistence(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.json"
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(func(c *Config) { c.Executor.Concurrency = 4 }); err != nil {
		t.Fatal(err)
	}
	s2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Get().Executor.Concurrency; got != 4 {
		t.Fatalf("concurrency = %d, want 4", got)
	}
}

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/x.json"
	for i := 0; i < 5; i++ {
		if err := WriteJSONAtomic(path, map[string]int{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("file missing after atomic write")
	}
}

func TestNormalizeRetryPolicyDefaults(t *testing.T) {
	c := Config{}
	c.Normalize()
	if c.Executor.RetryPolicy.BaseDelayMs <= 0 {
		t.Fatalf("baseDelayMs = %d, want > 0", c.Executor.RetryPolicy.BaseDelayMs)
	}
	if c.Executor.RetryPolicy.MaxDelayMs < c.Executor.RetryPolicy.BaseDelayMs {
		t.Fatalf("maxDelayMs(%d) < baseDelayMs(%d)", c.Executor.RetryPolicy.MaxDelayMs, c.Executor.RetryPolicy.BaseDelayMs)
	}
	if c.Executor.ShutdownGraceSeconds <= 0 {
		t.Fatalf("shutdownGraceSeconds = %d, want > 0", c.Executor.ShutdownGraceSeconds)
	}
}

func TestNormalizeRetryPolicyClamps(t *testing.T) {
	c := Config{}
	c.Executor.RetryPolicy = RetryPolicyConfig{BaseDelayMs: 100, MaxDelayMs: 50, Jitter: 9}
	c.Executor.ShutdownGraceSeconds = 99999
	c.Normalize()
	if c.Executor.RetryPolicy.MaxDelayMs < c.Executor.RetryPolicy.BaseDelayMs {
		t.Fatalf("maxDelayMs 未被修正: %+v", c.Executor.RetryPolicy)
	}
	if c.Executor.RetryPolicy.Jitter < 0 || c.Executor.RetryPolicy.Jitter > 1 {
		t.Fatalf("jitter 未被修正: %v", c.Executor.RetryPolicy.Jitter)
	}
	if c.Executor.ShutdownGraceSeconds > 300 {
		t.Fatalf("shutdownGraceSeconds 未被修正: %d", c.Executor.ShutdownGraceSeconds)
	}
}

func TestGetCopiesRetryableKinds(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStore(dir + string(os.PathSeparator) + "config.json")
	if err != nil {
		t.Fatal(err)
	}
	c := st.Get()
	if len(c.Executor.RetryPolicy.RetryableKinds) == 0 {
		t.Fatal("默认配置应带 retryableKinds")
	}
	c.Executor.RetryPolicy.RetryableKinds[0] = "mutated"
	c2 := st.Get()
	if c2.Executor.RetryPolicy.RetryableKinds[0] == "mutated" {
		t.Fatal("Get() 返回的切片未深拷贝，外部修改污染了内存配置")
	}
}

func TestNormalizeMigratesAutonomyForOldConfig(t *testing.T) {
	// 模拟 v2 旧配置（schema=2，没有 autonomy 段）：Normalize 后必须整段补默认并默认启用。
	c := Config{SchemaVersion: 2}
	c.Normalize()
	if c.SchemaVersion < 3 {
		t.Fatalf("schemaVersion 应迁移到 3，实际 %d", c.SchemaVersion)
	}
	if !c.Autonomy.Enabled {
		t.Fatal("旧配置迁移后自主应默认启用")
	}
	if c.Autonomy.Budget.MaxTasksPerGoal <= 0 || c.Autonomy.Budget.MaxStages <= 0 {
		t.Fatalf("预算默认值未补齐: %+v", c.Autonomy.Budget)
	}
	if len(c.Autonomy.Bounds.AllowedDomains) == 0 {
		t.Fatal("领域白名单未补齐")
	}
}

func TestNormalizeKeepsExplicitAutonomyOff(t *testing.T) {
	// 显式关闭自主：迁移逻辑不得把它改回 true（用户选择优先）。
	c := Config{SchemaVersion: 3}
	c.Autonomy = DefaultAutonomyConfig()
	c.Autonomy.Enabled = false
	c.Normalize()
	if c.Autonomy.Enabled {
		t.Fatal("显式关闭 autononmy 后 Normalize 不应改回启用")
	}
}

func TestNormalizeClampsAutonomyBudget(t *testing.T) {
	c := DefaultConfig()
	c.Autonomy.Budget.MaxTasksPerGoal = 99999
	c.Autonomy.Budget.MaxStages = -3
	c.Autonomy.Budget.MinQuality = 42
	c.Autonomy.Budget.MaxRollbacks = 999
	c.Autonomy.Bounds.MaxTaskWords = 1 << 30
	c.Autonomy.Plan.Words = 1
	c.Normalize()
	if c.Autonomy.Budget.MaxTasksPerGoal > 200 || c.Autonomy.Budget.MaxTasksPerGoal <= 0 {
		t.Fatalf("maxTasksPerGoal 未钳制: %d", c.Autonomy.Budget.MaxTasksPerGoal)
	}
	if c.Autonomy.Budget.MaxStages > 50 || c.Autonomy.Budget.MaxStages <= 0 {
		t.Fatalf("maxStages 未钳制: %d", c.Autonomy.Budget.MaxStages)
	}
	if c.Autonomy.Budget.MinQuality > 10 || c.Autonomy.Budget.MinQuality < 1 {
		t.Fatalf("minQuality 未钳制: %d", c.Autonomy.Budget.MinQuality)
	}
	if c.Autonomy.Budget.MaxRollbacks > 20 {
		t.Fatalf("maxRollbacks 未钳制: %d", c.Autonomy.Budget.MaxRollbacks)
	}
	if c.Autonomy.Bounds.MaxTaskWords > 20000 {
		t.Fatalf("maxTaskWords 未钳制: %d", c.Autonomy.Bounds.MaxTaskWords)
	}
	if c.Autonomy.Plan.Words < 100 {
		t.Fatalf("plan.words 未钳制: %d", c.Autonomy.Plan.Words)
	}
}

// TestNormalizeRestoresZeroedAutonomy 守护「残缺配置不得让自主子系统静默停摆」。
//
// 现场缺陷（2026-10-19）：压测实例手工写 config.json 只填了 executor/supervisor/server，
// schemaVersion 已是 3，导致 normalizeAutonomy 的 fromSchema<3 分支不生效，
// autonomy 段整体为零值（Enabled=false），目标树 bootstrap 出空 goals.json，
// 自主子系统不发一声就停了 —— 而 --acceptance 判为"目标树结构不完整"（fail）。
//
// 判据：零值 autonomy 段必须被补成默认（Enabled=true + 预算/文案齐全）；
// 用户显式关闭（Enabled=false 但预算/文案非空）必须原样保留。
func TestNormalizeRestoresZeroedAutonomy(t *testing.T) {
	// 1) 残缺配置：schema=3 且 autonomy 段全空
	var c Config
	c.SchemaVersion = 3
	c.Executor.Concurrency = 2
	c.SchemaVersion = 3
	c.Normalize()
	if !c.Autonomy.Enabled {
		t.Fatal("零值 autonomy 段必须被补成默认（Enabled=true），否则自主子系统会静默停摆")
	}
	if c.Autonomy.Budget.MaxTasksPerGoal == 0 || c.Autonomy.Plan.RootTitle == "" {
		t.Fatalf("零值段必须补全预算与文案，实际: %+v", c.Autonomy)
	}

	// 2) 用户显式关闭：Enabled=false 但预算与文案非空 —— 必须保留关闭意图
	var c2 Config
	c2.SchemaVersion = 3
	c2.Autonomy.Enabled = false
	c2.Autonomy.Budget.MaxTasksPerGoal = 8
	c2.Autonomy.Plan.RootTitle = "我自己的目标"
	c2.Normalize()
	if c2.Autonomy.Enabled {
		t.Fatal("用户显式关闭自主（Enabled=false 且预算/文案非空）必须被保留")
	}
	if c2.Autonomy.Budget.MaxTasksPerGoal != 8 {
		t.Fatalf("用户自定义预算不得被覆盖，实际 %d", c2.Autonomy.Budget.MaxTasksPerGoal)
	}
}
