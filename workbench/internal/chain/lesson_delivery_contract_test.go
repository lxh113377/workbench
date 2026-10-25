package chain

import (
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// TestLessonsReachTaskPromptThroughChain 钉住「要求真的到得了下游」这条链：模板正文 -> RootTask/Successor -> Task.Prompt。
//
// 为什么需要（2026-10-25 round 162）：本轮往 quality-audit 加了⑦、往 quality-verify 加了⑩；而 templates
// 侧已有的契约测试只调用 Render(PromptTemplate) —— 它证明「正文里有这句话」，**不证明**「这句话会出现在
// 交给模型的那条 Prompt 里」。中间还有两个会静默丢内容的环节，都不会让模板侧测试变红：
//
//	· 注册表启用位：模板 Enabled=false 时永远不会被选到 ⇒ 正文写得再对也是死字；
//	· 装配环节：RootTask 会填入 {context}/{prev}，Successor 还会注入父摘要与自评缺口，
//	  若哪天给 Prompt 加长度上限，**追加在正文末尾**的要求会第一个被截掉（本轮新条款正是追加在末尾）。
//
// 所以必须在 chain 侧按真实 Task.Prompt 再钉一次，且根任务与后继两条入口都要覆盖。
func TestLessonsReachTaskPromptThroughChain(t *testing.T) {
	cfg := config.ChainConfig{Enabled: true, MaxDepth: 5, MinQuality: 4, ContextChars: 2000}
	g, reg := newTestGen(t, cfg)
	wants := []struct {
		id    string
		frags []string
	}{
		{"quality-audit", []string{"运行时真值", "复验编码", "权威定义处", "否定性结论", "quotepath", "显示层转义"}},
		{"quality-verify", []string{"复现率", "变异实验自身", "真的改掉了目标", "双向变异证明", "去掉缺陷必须判绿", "先剥注释", "保留原词", "可见性规则", "按包限定计数", "修订版本", "声明什么口径", "覆盖率缺口不等于行为缺口", "制造假安全感", "正向（一个已知必然存在的串必须 True", "段数不是次数", "取错对象", "身份断言", "按块判定，不按单行判定", "对照组不成立的实验等于没做", "并发缺陷不得用", "同一通道的所有使用路径必须同锁", "判目标语言的行为必须用该语言", "先跑再写注释"}},
		{"quality-gate-spec", []string{"静态配对检查", "浅克隆", "挂受理面 ≠ 在该受理面上真能判", "虚增覆盖", "聚合器不得把非零退出码改写成", "绝不允许用归零", "第三种形态核销缺口", "任何在函数外写判决字样的分支都算缺陷"}},
		{"quality-hygiene", []string{"重新入索引", "先暂存后写", "显式 pathspec", "整个暂存区", "不要按惯例假定", "写前测、写后量", "误导读者做新的决策", "先复核它的状态是否还成立", "当时的证据快照", "现在断言", "字符串算出来的路径", "逐个文件 unlink", "工作副本的换行可能与入库内容", "必然改变字节"}},
		{"review-five-axis", []string{"合并结论", "门禁建议"}},
	}
	if len(wants) == 0 {
		t.Fatal("要求表为空 —— 判据在比空气")
	}
	now := int64(1700000000)
	parent := &model.Task{ID: "p1", ChainID: "c1", SessionID: "s1", Title: "上游 · 主题", Summary: "上游结论", Domain: "quality"}
	critique := &model.Critique{Quality: 8, Gaps: []string{"缺口"}}
	checked := 0
	for _, w := range wants {
		tpl := mustTpl(t, reg, w.id)
		if !tpl.Enabled {
			t.Errorf("模板 %s 未启用 ⇒ 它里面的要求下游永远读不到（写了等于没写）", w.id)
		}
		root := g.RootTask(tpl, now)
		if root == nil || strings.TrimSpace(root.Prompt) == "" {
			t.Fatalf("%s: 根任务 Prompt 为空", w.id)
		}
		succ := g.Successor(tpl, parent, critique, 1, now)
		if succ == nil || strings.TrimSpace(succ.Prompt) == "" {
			t.Fatalf("%s: 后继 Prompt 为空", w.id)
		}
		for _, f := range w.frags {
			checked++
			if !strings.Contains(root.Prompt, f) {
				t.Errorf("模板 %s 的要求片段 %q 没进**根任务** Prompt（正文被截断或装配漏项）", w.id, f)
			}
			if !strings.Contains(succ.Prompt, f) {
				t.Errorf("模板 %s 的要求片段 %q 没进**后继** Prompt", w.id, f)
			}
		}
	}
	if checked == 0 {
		t.Fatal("一个片段都没检查到 —— 判据在比空气")
	}
}
