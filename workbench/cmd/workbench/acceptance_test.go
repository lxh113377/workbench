package main

// acceptance_test.go 守护总验收判据的"三态口径"与几个易错的具体判据。
//
// 动机：总验收命令是给人看的最终结论，它自己不能虚报。这里把最容易写错的两处
// 钉死：(1) 三态聚合规则（有 fail 就是 fail；没有 fail 但有 unknown 就是
// unverified，绝不降级成 pass）；(2) indexPage 段截取（全文件计数会把管理面板
// 的 16 个按钮算进首页，导致误判 fail —— 本轮实际踩到）。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workbench/internal/config"
)

// TestAggregateVerdict 守护三态聚合：fail 优先，其次 unverified，全 pass 才 pass。
func TestAggregateVerdict(t *testing.T) {
	cases := []struct {
		name    string
		items   []acceptanceItem
		verdict string
		code    int
	}{
		{"全通过", []acceptanceItem{{State: statePass}, {State: statePass}}, "pass", 0},
		{"有未验证", []acceptanceItem{{State: statePass}, {State: stateUnverified}}, "unverified", 3},
		{"有失败", []acceptanceItem{{State: statePass}, {State: stateFail}}, "fail", 1},
		{"失败优先于未验证", []acceptanceItem{{State: stateUnverified}, {State: stateFail}}, "fail", 1},
		{"全未验证", []acceptanceItem{{State: stateUnverified}}, "unverified", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := aggregate(tc.items)
			if rep.Verdict != tc.verdict || rep.ExitCode != tc.code {
				t.Fatalf("聚合结果 = %s/%d，期望 %s/%d", rep.Verdict, rep.ExitCode, tc.verdict, tc.code)
			}
		})
	}
}

// TestIndexPageSectionIsolation 守护首页判据的截取范围：
// html.go 里有 indexPage / adminPage / governancePage 三个模板，
// 只有 indexPage 属于"极简首页"契约。
func TestIndexPageSectionIsolation(t *testing.T) {
	src := "package web\n\nconst indexPage = `<!DOCTYPE html>\n<button id=\"startBtn\">开始运行</button>\n`\n\nconst adminPage = `<button>暂停</button><button>继续</button><button>停止</button>`\n"
	idx := indexPageSection(src)
	if strings.TrimSpace(idx) == "" {
		t.Fatal("indexPageSection 必须能截出 indexPage 段")
	}
	if n := strings.Count(idx, "<button"); n != 1 {
		t.Fatalf("indexPage 段内应只有 1 个按钮，实际 %d（截取越界会把 adminPage 按钮算进来）", n)
	}
	if strings.Contains(idx, "暂停") {
		t.Fatal("截取范围越界：把 adminPage 的内容也算进来了")
	}
	// 找不到定义时返回空串（调用方据此判 fail，而不是 panic）
	if got := indexPageSection("package x\nvar y = 1\n"); got != "" {
		t.Fatalf("无 indexPage 定义时应返回空串，实际 %q", got)
	}
}

// TestAcceptanceItemStatesAreThreeValued 守护状态枚举只有三态（防止有人加第四态绕过口径）。
func TestAcceptanceItemStatesAreThreeValued(t *testing.T) {
	valid := map[checkState]bool{statePass: true, stateFail: true, stateUnverified: true}
	for _, st := range []checkState{statePass, stateFail, stateUnverified} {
		if !valid[st] {
			t.Fatalf("状态 %q 不在三态枚举内", st)
		}
	}
	if len(valid) != 3 {
		t.Fatalf("验收状态必须恰好三态，实际 %d 种", len(valid))
	}
}

// TestRepoRootHasRequiredAnchors 守护仓库根探测：必须有 docs + workbench 双锚点。
// （本轮实测：只看 docs 会在 dist 目录误判，只看 workbench 又会在命令目录误判。）
func TestRepoRootHasRequiredAnchors(t *testing.T) {
	root := repoRoot()
	if root == "" {
		t.Fatal("repoRoot 不得返回空串")
	}
	// 单测运行目录是 workbench/cmd/workbench，向上找应命中仓库根。
	if _, err := readRepoFile("workbench/internal/web/html.go"); err != nil {
		t.Fatalf("repoRoot(%s) 下读不到首页源码，探测逻辑有问题: %v", root, err)
	}
	if _, err := readRepoFile("docs/自主运行方案.md"); err != nil {
		t.Fatalf("repoRoot(%s) 下读不到方案文档，探测逻辑有问题: %v", root, err)
	}
}

// writeMetrics 生成一份 metrics.csv（每小时一个点，共 n 点）。
// 注意：必须写到 layout.DataRoot 下的 logs/，与 checkSoak 读的位置一致。
func writeMetrics(t *testing.T, dataRoot string, n int, heapBase, heapStep float64) string {
	t.Helper()
	logsDir := filepath.Join(dataRoot, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(logsDir, "metrics.csv")
	var b strings.Builder
	b.WriteString("at,heap_mb,alloc_mb,goroutine,handles,num_gc\n")
	for i := 0; i < n; i++ {
		at := int64(1792300000000) + int64(i)*3600000
		heap := heapBase + float64(i)*heapStep
		b.WriteString(fmt.Sprintf("%d,%.2f,%.2f,20,%d,10\n", at, heap, heap/2, 300+i))
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestCheckSoakNeverPassesWithout24h 守护 A7 的诚实性：
// 采样不足 24 小时时，哪怕趋势完全平稳也必须是 unverified，不得判 pass。
func TestCheckSoakNeverPassesWithout24h(t *testing.T) {
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	writeMetrics(t, layout.DataRoot, 5, 20, 0) // 5 小时、平稳
	it := checkSoak(layout)
	if it.State == statePass {
		t.Fatalf("采样仅 5 小时不得判 pass（零数据/不足时长不记 PASS），实际 detail=%q", it.Detail)
	}
	if it.State != stateUnverified {
		t.Fatalf("采样不足且无泄漏时应 unverified，实际 %s（%s）", it.State, it.Detail)
	}
}

// TestCheckSoakPassesOnFullDayFlat 守护正向判据：满 24h 且平稳 => pass。
func TestCheckSoakPassesOnFullDayFlat(t *testing.T) {
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	writeMetrics(t, layout.DataRoot, 26, 20, 0) // 25 小时、平稳
	it := checkSoak(layout)
	if it.State != statePass {
		t.Fatalf("满 24h 且平稳应 pass，实际 %s（%s；evidence=%v）", it.State, it.Detail, it.Evidence)
	}
}

// TestCheckSoakFailsOnLeak 守护反向判据：满 24h 但内存持续上升 => fail。
func TestCheckSoakFailsOnLeak(t *testing.T) {
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	writeMetrics(t, layout.DataRoot, 26, 10, 2) // 25 小时、每小时 +2MB
	it := checkSoak(layout)
	if it.State != stateFail {
		t.Fatalf("检出泄漏必须 fail，实际 %s（%s；evidence=%v）", it.State, it.Detail, it.Evidence)
	}
}

// TestCheckSoakUnverifiedWhenNoData 守护零数据口径：没有 metrics.csv ⇒ unverified（不是 fail）。
func TestCheckSoakUnverifiedWhenNoData(t *testing.T) {
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if it := checkSoak(layout); it.State != stateUnverified {
		t.Fatalf("无采样应 unverified，实际 %s", it.State)
	}
}

// TestCheckConfigIntegrityDetectsMojibake 守护「配置文案被编码破坏」必须被检出。
//
// 现场缺陷（2026-10-19）：dist 实例 config.json 的 rootTitle/rootIntent 变成
// 一整串 "?"，成因是外部客户端用 ASCII 编码 PUT /api/settings。这两个字段会被
// planner 原样拼进每个自主任务 Prompt，等于把自主决策的锚点废掉，而系统静默继续跑。
func TestCheckConfigIntegrityDetectsMojibake(t *testing.T) {
	// 1) 磁盘上是损坏值 ⇒ 不得 pass（要么 unverified 修复中，要么 fail 不可修）
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	bad := config.DefaultConfig()
	bad.Autonomy.Plan.RootTitle = "?????????"
	bad.Autonomy.Plan.RootIntent = "????????????????????,?????????"
	if err := config.WriteJSONAtomic(layout.ConfigFile, bad); err != nil {
		t.Fatal(err)
	}
	it := checkConfigIntegrity(layout)
	if it.State == statePass {
		t.Fatalf("磁盘配置含问号串时不得判 pass，实际 detail=%q", it.Detail)
	}

	// 2) 正常配置 ⇒ pass
	dir2 := t.TempDir()
	layout2, err := config.BuildLayout(dir2, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := layout2.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteJSONAtomic(layout2.ConfigFile, config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	if it2 := checkConfigIntegrity(layout2); it2.State != statePass {
		t.Fatalf("正常配置应 pass，实际 %s（%s）", it2.State, it2.Detail)
	}

	// 3) 没有配置文件 ⇒ unverified（不是 fail）
	dir3 := t.TempDir()
	layout3, err := config.BuildLayout(dir3, "")
	if err != nil {
		t.Fatal(err)
	}
	if it3 := checkConfigIntegrity(layout3); it3.State != stateUnverified {
		t.Fatalf("无配置应 unverified，实际 %s", it3.State)
	}
}