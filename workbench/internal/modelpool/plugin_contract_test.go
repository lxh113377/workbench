package modelpool

// 跨组件契约测试：把 ProjectAggregate 产出的 aggregate 段喂给**真实 OFM 插件代码**，
// 断言插件 buildAggregateCatalog 生成的行与我们投影出的可路由映射逐一对应。
//
// 为什么值得单独一层：executor.taskHome 的接线还没做（Lead 在途文件），
// 所以「池里的自填端点能否被 DSH 真调用」此前只有两类证据 ——
// 我们的 Go 单测（只证明我们按自己的理解生成 JSON），
// 以及 agg_e2e.ps1 的端到端（证明插件能服务，但要起真 DSH、慢且依赖桌面会话）。
// 中间缺的正是「字段形状是否被插件接受」这一层：插件是 JSON 的消费方，
// 它说行才算数。字段名或枚举值一旦在插件侧改名，本测试立刻翻红，
// 而不是等接线上线后表现为「静默改道免费车道」。
//
// 依赖：本机 node + 已安装 dsh-our-free-model + tests/aggregate 下的 harness。
// 任一缺失即 Skip（不假通过，也不在无插件的 CI/容器里判红）。

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// harnessRelPath 是插件契约 harness 相对仓库根的位置。
const harnessRelPath = "tests/aggregate/plugin_contract_harness.mjs"

// pluginRow 是 harness 回报的一行目录条目。
type pluginRow struct {
	ID         string `json:"id"`
	Channel    string `json:"channel"`
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
	Format     string `json:"format"`
	Wire       string `json:"wire"`
}

// askPlugin 用真实插件代码解析我们生成的 aggregate 段。
func askPlugin(t *testing.T, section map[string]any) (rows []pluginRow, chatURL string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skipf("本机无 node，跳过跨组件契约测试：%v", err)
	}
	pluginDir := OFMPluginDir("")
	if pluginDir == "" {
		t.Skip("本机未安装 dsh-our-free-model 插件，跳过跨组件契约测试")
	}
	// 从包目录向上找仓库根（workbench/internal/modelpool -> 仓库根）
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(root, filepath.FromSlash(harnessRelPath))
	if _, err := os.Stat(harness); err != nil {
		t.Skipf("harness 不在盘（%s），跳过：%v", harnessRelPath, err)
	}
	buf, err := json.Marshal(section)
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "aggregate.json")
	if err := os.WriteFile(cfg, buf, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", harness, cfg, pluginDir).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("harness 失败：%s", string(ee.Stderr))
		}
		t.Fatalf("node 调用失败：%v", err)
	}
	var parsed struct {
		Rows    []pluginRow `json:"rows"`
		ChatURL string      `json:"chatUrl"`
		Error   string      `json:"error"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("解析 harness 输出失败：%v\nraw=%s", err, string(out))
	}
	if parsed.Error != "" {
		t.Fatalf("插件解析我们的配置时报错（契约破坏）：%s", parsed.Error)
	}
	return parsed.Rows, parsed.ChatURL
}

// TestPluginAcceptsOurAggregateProjection 是需求1 的契约锁：
// 我们投影出的 provider/model 必须被插件原样认下，且请求地址就是我们的 BaseURL。
func TestPluginAcceptsOurAggregateProjection(t *testing.T) {
	registry := reg() // 复用 aggregate_test.go 的 fixture provider 表
	specs := []Spec{
		customSpec("open_router/openrouter/free", "http://127.0.0.1:18432/v1", "sk-user", LaneCustom),
	}
	proj := ProjectAggregate(specs, registry)
	if !proj.Enabled {
		t.Fatalf("前置不成立：投影未启用，拒绝=%+v", proj.Rejections)
	}
	rows, chatURL := askPlugin(t, proj.AggregateSection())
	if len(rows) != 1 {
		t.Fatalf("插件应认出 1 行，实际 %d：%+v（我们的映射 %+v）", len(rows), rows, proj.ModelIDs)
	}
	// 核心断言：插件产出的行 id 必须与我们给 overlay 的复合 id 完全一致。
	// 不一致意味着接线后 DSH 拿这个 id 去查 catalog 会查不到 —— 静默回退免费车道。
	want := proj.ModelIDs["open_router/openrouter/free"]
	if rows[0].ID != want {
		t.Fatalf("id 契约破坏：插件给 %q，我们投影给 %q", rows[0].ID, want)
	}
	if rows[0].Channel != "aggregate" {
		t.Fatalf("channel 应为 aggregate（决定走 postAggregateStreamed 而非免费车道），实际 %q", rows[0].Channel)
	}
	// 请求地址必须打到用户自填的端点，而不是插件 registry 的默认公网地址。
	if chatURL != "http://127.0.0.1:18432/v1/chat/completions" {
		t.Fatalf("chatUrl 未指向用户 BaseURL，实际 %q", chatURL)
	}
}

// TestPluginRejectsWhatWeReject 锁住两侧一致：我们判死的 provider，
// 插件也不会产出行（否则说明我们的拒绝清单漏了东西）。
func TestPluginRejectsWhatWeReject(t *testing.T) {
	registry := reg()
	specs := []Spec{
		customSpec("not-a-real-provider/whatever", "http://127.0.0.1:1/v1", "sk", LaneCustom),
	}
	proj := ProjectAggregate(specs, registry)
	if proj.Enabled {
		t.Fatal("未知 provider 不该被投影")
	}
	rows, _ := askPlugin(t, proj.AggregateSection())
	if len(rows) != 0 {
		t.Fatalf("我们已拒绝，插件却仍产出行（两侧判据不一致）：%+v", rows)
	}
}
