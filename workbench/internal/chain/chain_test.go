package chain

import (
	"path/filepath"
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/templates"
)

// newTestGen 构造内存模板库 + 生成器（不落盘、不依赖用户配置）。
func newTestGen(t *testing.T, cfg config.ChainConfig) (*Generator, *templates.Registry) {
	t.Helper()
	reg, err := templates.Open(joinTemp(t, "templates.json"))
	if err != nil {
		t.Fatal(err)
	}
	return New(reg, cfg), reg
}

func joinTemp(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func mustTpl(t *testing.T, reg *templates.Registry, id string) model.Template {
	t.Helper()
	tpl, ok := reg.Get(id)
	if !ok {
		t.Fatalf("内置模板 %s 不存在", id)
	}
	return tpl
}

// ---------- RootTask ----------

func TestRootTaskFields(t *testing.T) {
	cfg := config.ChainConfig{Enabled: true, MaxDepth: 5, MinQuality: 4, ContextChars: 2000}
	g, reg := newTestGen(t, cfg)
	tpl := mustTpl(t, reg, "content-deep-dive")
	now := int64(1700000000)
	task := g.RootTask(tpl, now)
	if task == nil {
		t.Fatal("RootTask 返回 nil")
	}
	if !strings.HasPrefix(task.ID, "task-") {
		t.Fatalf("ID 前缀 = %q", task.ID)
	}
	if !strings.HasPrefix(task.ChainID, "chain-") {
		t.Fatalf("ChainID 前缀 = %q", task.ChainID)
	}
	if task.State != model.StateCandidate || task.Source != "template" {
		t.Fatalf("初始状态/来源错误: %s/%s", task.State, task.Source)
	}
	if task.TemplateID != tpl.ID || task.Domain != tpl.Domain || task.OutputFormat != tpl.OutputSpec.Format {
		t.Fatalf("模板字段未继承: %+v", task)
	}
	if task.CreatedAt != now || task.UpdatedAt != now {
		t.Fatalf("时间戳未透传: %d/%d", task.CreatedAt, task.UpdatedAt)
	}
	if task.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts = %d, want 3", task.MaxAttempts)
	}
	// 标题 = 模板名 · 主题；提示词必须已渲染（无残留占位符）
	if !strings.Contains(task.Title, "·") {
		t.Fatalf("标题未含主题分隔: %q", task.Title)
	}
	if strings.Contains(task.Prompt, "{topic}") || strings.Contains(task.Prompt, "{words}") {
		t.Fatalf("提示词占位符未渲染: %q", task.Prompt)
	}
}

func TestRootTaskIDsUnique(t *testing.T) {
	g, reg := newTestGen(t, config.ChainConfig{Enabled: true, MaxDepth: 5})
	tpl := mustTpl(t, reg, "code-tool")
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := g.RootTask(tpl, 0).ID
		if seen[id] {
			t.Fatalf("第 %d 次生成重复 ID %s", i, id)
		}
		seen[id] = true
	}
}

// ---------- Successor ----------

func TestSuccessorInheritsChainAndCarriesContext(t *testing.T) {
	cfg := config.ChainConfig{Enabled: true, MaxDepth: 5, MinQuality: 4, ContextChars: 2000}
	g, reg := newTestGen(t, cfg)
	succTpl := mustTpl(t, reg, "content-outline-cards")
	parent := &model.Task{
		ID: "p1", ChainID: "c1", SessionID: "s1", Title: "深度长文 · 远程协作的隐性成本",
		Summary: "结论：异步文档优先。", Domain: "content",
	}
	critique := &model.Critique{Quality: 8, Tags: []string{"结构化"}, Gaps: []string{"缺案例"}}
	succ := g.Successor(succTpl, parent, critique, 1, 42)

	if succ.ChainID != parent.ChainID || succ.SessionID != parent.SessionID {
		t.Fatal("后继必须沿用父链与父会话")
	}
	if succ.ParentID != parent.ID || succ.ChainDepth != 1 {
		t.Fatalf("父子关系错误: parent=%s depth=%d", succ.ParentID, succ.ChainDepth)
	}
	if succ.Source != "chain" || succ.State != model.StateCandidate {
		t.Fatalf("来源/状态错误: %s/%s", succ.Source, succ.State)
	}
	if !strings.Contains(succ.Title, "基于《") {
		t.Fatalf("标题应指明递进来源: %q", succ.Title)
	}
	// 上下文注入：父结论与自评不足必须出现在提示词里
	if !strings.Contains(succ.Prompt, "异步文档优先") {
		t.Fatal("父任务摘要未注入后继提示词")
	}
	if !strings.Contains(succ.Prompt, "缺案例") {
		t.Fatal("自评缺口未注入后继提示词（链式递进失去依据）")
	}
	// 主题连贯：从父标题 "模板名 · 主题" 提取主题段
	if !strings.Contains(succ.Prompt, "远程协作的隐性成本") {
		t.Fatalf("主题未延续: %q", succ.Prompt)
	}
}

// ---------- SynthesisTask ----------

func TestSynthesisTaskFallbacks(t *testing.T) {
	g, reg := newTestGen(t, config.ChainConfig{Enabled: true, MaxDepth: 5, ContextChars: 2000})
	_ = reg
	parent := &model.Task{ID: "p9", ChainID: "c9", Domain: "data", Summary: "留存归因结论"}

	// domain 为空 → 继承父任务
	s := g.SynthesisTask(model.Idea{Title: "补充实验", Prompt: "设计 A/B 实验"}, parent, 2, 7)
	if s.Domain != "data" {
		t.Fatalf("domain 未继承父任务: %s", s.Domain)
	}
	// idea 自带 domain 优先
	s2 := g.SynthesisTask(model.Idea{Title: "x", Prompt: "y", Domain: "code"}, parent, 2, 7)
	if s2.Domain != "code" {
		t.Fatalf("idea.domain 应优先: %s", s2.Domain)
	}
	// 无产出要求措辞 → 自动补上交付约束
	s3 := g.SynthesisTask(model.Idea{Title: "z", Prompt: "随便写点什么"}, parent, 2, 7)
	if !strings.Contains(s3.Prompt, "产出") {
		t.Fatalf("提示词应补交付约束: %q", s3.Prompt)
	}
	if !strings.Contains(s3.Prompt, "留存归因结论") {
		t.Fatal("父摘要应注入合成任务")
	}
	// 空 domain 链（idea/parent 都没有）→ 兜底 content
	orphan := &model.Task{ID: "p0", ChainID: "c0"}
	s4 := g.SynthesisTask(model.Idea{Title: "t", Prompt: "产出内容"}, orphan, 1, 0)
	if s4.Domain != "content" {
		t.Fatalf("空 domain 应兜底 content: %s", s4.Domain)
	}
	if s4.Source != "synthesis" || s4.OutputFormat != "md" || s4.ChainDepth != 1 {
		t.Fatalf("合成任务字段错误: %+v", s4)
	}
}

// ---------- ShouldExtend ----------

func TestShouldExtendBoundaries(t *testing.T) {
	off, _ := newTestGen(t, config.ChainConfig{Enabled: false, MaxDepth: 5, MinQuality: 4})
	on, _ := newTestGen(t, config.ChainConfig{Enabled: true, MaxDepth: 5, MinQuality: 4})
	p := func(depth int) *model.Task { return &model.Task{ID: "x", ChainDepth: depth} }

	if off.ShouldExtend(p(0), nil) {
		t.Fatal("链关闭时不得延伸")
	}
	if on.ShouldExtend(nil, nil) {
		t.Fatal("父任务为 nil 不得延伸")
	}
	// 深度上限：MaxDepth=5 时 depth=4 的下一层是 5，已达上限 → 停
	if on.ShouldExtend(p(4), nil) {
		t.Fatal("达 MaxDepth 必须停")
	}
	if !on.ShouldExtend(p(3), nil) {
		t.Fatal("未达上限应继续")
	}
	// 低质终止：quality 在 (0, MinQuality) 区间
	if on.ShouldExtend(p(0), &model.Critique{Quality: 2}) {
		t.Fatal("低质自评必须终止延伸")
	}
	if !on.ShouldExtend(p(0), &model.Critique{Quality: 6}) {
		t.Fatal("合格自评应继续")
	}
	// Quality=0 表示自评降级（无有效评分）→ 不因此掐断链
	if !on.ShouldExtend(p(0), &model.Critique{Quality: 0}) {
		t.Fatal("降级自评（0 分）不应误杀链")
	}
}

// ---------- SuccessorTemplates ----------

func TestSuccessorTemplates(t *testing.T) {
	g, _ := newTestGen(t, config.ChainConfig{Enabled: true, MaxDepth: 5})
	// content-deep-dive 的内置后继链 = 要点卡片化 + 口播脚本
	out := g.SuccessorTemplates(&model.Task{TemplateID: "content-deep-dive"})
	if len(out) != 2 {
		t.Fatalf("内置链长度 = %d, want 2", len(out))
	}
	ids := map[string]bool{out[0].ID: true, out[1].ID: true}
	if !ids["content-outline-cards"] || !ids["content-script"] {
		t.Fatalf("后继模板不符: %v", ids)
	}
	if g.SuccessorTemplates(&model.Task{TemplateID: "no-such-tpl"}) != nil {
		t.Fatal("未知模板应返回 nil")
	}
	if g.SuccessorTemplates(nil) != nil {
		t.Fatal("nil 父任务应返回 nil")
	}
	// 无后继配置的模板 → 空列表
	if len(g.SuccessorTemplates(&model.Task{TemplateID: "content-faq"})) != 0 {
		t.Fatal("无后继链的模板应返回空")
	}
}

// ---------- buildContext（纯函数）----------

func TestBuildContextShapes(t *testing.T) {
	// nil 父任务
	if got := buildContext(nil, nil, 100); got != "" {
		t.Fatalf("nil 父应得空上下文: %q", got)
	}
	// 全空字段 → 占位文案（不能把空串注入提示词）
	got := buildContext(&model.Task{Title: "t"}, nil, 2000)
	if strings.TrimSpace(got) != "前序任务：t" {
		t.Fatalf("最小上下文形态不符: %q", got)
	}
	// 摘要 + 标签 + 缺口都在
	c := &model.Critique{Tags: []string{"A", "B"}, Gaps: []string{"缺数据"}}
	got = buildContext(&model.Task{Title: "t", Summary: "结论X"}, c, 2000)
	for _, want := range []string{"结论X", "A、B", "缺数据"} {
		if !strings.Contains(got, want) {
			t.Fatalf("上下文缺 %q: %q", want, got)
		}
	}
	// 截断：maxChars 必须生效（超长摘要不炸后继提示词）
	huge := &model.Task{Title: "t", Summary: strings.Repeat("长", 5000)}
	got = buildContext(huge, nil, 200)
	if len(got) > 260 {
		t.Fatalf("上下文未按 maxChars 截断: %d", len(got))
	}
}

// ---------- randPicker（自研随机源，必须守住边界）----------

func TestRandPickerBounds(t *testing.T) {
	r := newRandPicker()
	for _, n := range []int{0, 1, 2, 7, 100} {
		for i := 0; i < 200; i++ {
			v := r.Intn(n)
			if v < 0 || v >= n && n > 0 {
				t.Fatalf("Intn(%d) = %d 越界", n, v)
			}
		}
	}
}

// TestBothPathsFillPrev 钉住「{prev} 在首轮与后继两条路径都不能渲染成空洞」。
//
// 一手缺陷（2026-10-25 实测，用探针跑真实 RenderVars）：RootTask 只填 Topic/Words/Context/Date，
// 不填 Prev；而 Successor 填的是 model.Truncate(parent.Summary, 400) —— 父任务无摘要时同样是空串。
// 实测渲染结果：「… ｜ 前序  ｜ 上下文 …」（前序后面直接是分隔符）。
// 而 Context 在首轮是有兜底文案的（首轮任务，无前序上下文）—— 同一批变量里一个有兜底一个没有，
// 是不一致而非设计。
//
// 为什么不能拿「内置 29 条模板 0 处使用 {prev}」当免责（运行时统计过，确实 0）：
// 模板库支持用户导入（Registry.ImportJSON / Upsert），而 Validate 只拒绝未知占位符 ——
// {prev} 在 allowedVars 白名单里，用户写 {prev} 完全合法、能入库、会渲染成空洞。
// 空洞不只是难看：模型看到「前序 」无法区分「真的没有前序」与「数据丢了」，会自行编造上下文。
//
// 不测「父任务为 nil」：生产路径不可达（engine.go 的 extendChain 在调用前已先解引用 parent），
// 且 Successor 后半段还有 5 处 parent.X 无守卫 —— 那是独立的结构性改动，不属本轮主张（已登记）。
// 本轮只钉真实可达的缺陷：{prev} 在首轮与「父任务有但摘要为空」两条路径渲染成空洞。
func TestBothPathsFillPrev(t *testing.T) {
	cfg := config.ChainConfig{Enabled: true, MaxDepth: 5, MinQuality: 4, ContextChars: 2000}
	g, _ := newTestGen(t, cfg)
	// 哨兵用方括号：若 {prev} 渲染成空串，结果里会出现 "P[]"。
	// （反例教训：首版哨兵用竖线 P{prev}|，空值时渲染成 P| 与分隔符撞形，
	//   断言永不命中 —— 把缺陷态也判成绿。变异测试（还原旧码重跑）才抓出这个空转。）
	tpl := model.Template{
		ID: "prev-probe", Name: "占位符探针", Domain: "quality",
		PromptTemplate: "T[{topic}]W[{words}]C[{context}]D[{date}]P[{prev}]",
		Params:         model.TemplateParams{ModelTier: "balanced", MaxMinutes: 10, Tags: []string{"x"}},
		OutputSpec:     model.OutputSpec{Format: "md", Sections: []string{"正文"}, MinWords: 100, MinBytes: 200},
		PriorityWeight: 3, ValuePrior: 80, CostPrior: 50, CooldownMinutes: 60, DiversityGroup: "quality",
		Chain: model.ChainSpec{MaxDepth: 5, Fanout: 2},
	}
	if err := templates.Validate(tpl); err != nil {
		t.Fatalf("含 {prev} 的模板应通过校验（{prev} 在 allowedVars 白名单里）: %v", err)
	}
	const hole = "P[]"
	runs := []struct {
		name string
		fn   func() string
	}{
		{"RootTask（首轮）", func() string { return g.RootTask(tpl, 1700000000).Prompt }},
		{"Successor（父任务无摘要）", func() string {
			q := &model.Task{ID: "p1", ChainID: "c1", Title: "父", Domain: "quality"}
			return g.Successor(tpl, q, nil, 1, 1700000001).Prompt
		}},
		{"Successor（父任务有摘要）", func() string {
			q := &model.Task{ID: "p2", ChainID: "c2", Title: "父2", Summary: "真摘要内容", Domain: "quality"}
			return g.Successor(tpl, q, nil, 1, 1700000003).Prompt
		}},
	}
	for _, r := range runs {
		got := r.fn()
		if strings.Contains(got, hole) {
			t.Errorf("%s 渲染出空的 {prev}（出现 %q）—— 必须给显式兜底文案：让模型能区分「真无前序」与「数据丢了」%q", r.name, hole, got)
			continue
		}
		if strings.Contains(got, "{") || strings.Contains(got, "}") {
			t.Errorf("%s 有未替换的占位符残留: %q", r.name, got)
		}
	}
}
