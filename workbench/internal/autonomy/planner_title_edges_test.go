package autonomy

// planner_title_edges_test.go 补 planner 标题链路的两个未覆盖分支（2026-10-26 round 189）。
//
// 依据（实测）：用 -coverprofile 逐块查 planner.go，发现两个 hits=0 的分支 ——
// firstLine 的多行分支 与 normTitle 的空串分支。两条都不是装饰：标题链路（composeTitle /
// rootGoalTitle / duplicated）都以它们为基座，而生产里 goal.Title 来自模型输出、
// 带换行是常态 —— 多行分支不测，等于把「标题里带换行会怎样」留成未知。
//
// 方法教训（本轮真实发生）：我先用 grep 在测试里找 composeTitle 一词就下结论「零测试覆盖」；
// 实际 planner_title_test.go 通过公开 API p.Plan(...) 间接覆盖了它，并断言了焦点保留与 rune
// 上限。grep 到函数名只能证明「没人直接调用」，不能证明「没被守护」—— 这正是模板
// quality-audit ⑦ 与 verify ⑨ 要防的形态；改用覆盖率取真值后，缺口才是上面那两个分支。
//
// 另两条如实记录（写在测试里，免得后人重踩）：
//   1) duplicated 的判重口径是**焦点段相同才算重复**（目标前缀相同不算）—— 见 focusPart 注释；
//      我第一版按「前缀相同即重复」写断言，被实现判红，是**我的期望错**，不是缺陷。
//   2) rootGoalTitle 的注释说「首段为空（畸形标题）时退回整串」，但它先过 firstLine，
//      而 firstLine 会 TrimSpace 整串 —— 前导空白已被吃掉，Index(` · `) > 0 时首段必非空，
//      所以该退路**不可达**。本测试只钉可达行为，不给不可达分支写假断言。

import (
	"testing"
)

// TestFirstLineKeepsOnlyFirstLine 钉住多行输入只取第一行（此前 hits=0 的分支）。
func TestFirstLineKeepsOnlyFirstLine(t *testing.T) {
	multi := "  第一行标题\n第二行是正文\n第三行\n  "
	if got := firstLine(multi); got != "第一行标题" {
		t.Fatalf("多行输入必须只取第一行且两端去空白，实际 %q", got)
	}
	// 单行输入不受影响（不该因为不含换行而改变内容）
	if got := firstLine("单行标题"); got != "单行标题" {
		t.Fatalf("单行输入应原样返回，实际 %q", got)
	}
}

// TestRootGoalTitleRealBranches 钉住 rootGoalTitle 的两条可达分支。
func TestRootGoalTitleRealBranches(t *testing.T) {
	// 多行 + 带分隔符：先取第一行，再取根段（第二行里的分隔符不该被看到）
	multi := "提升工作台质量\n说明：略\n · 不该被取到"
	if got := rootGoalTitle(multi); got != "提升工作台质量" {
		t.Fatalf("多行标题的根段应取第一行的首段，实际 %q", got)
	}
	// 无分隔符：原样返回（历史标题大多如此）
	if got := rootGoalTitle("单一标题"); got != "单一标题" {
		t.Fatalf("无分隔符应原样返回，实际 %q", got)
	}
}

// TestNormTitleEmptyBranch 钉住空串分支（此前 hits=0），并连带 duplicated 的空标题语义。
func TestNormTitleEmptyBranch(t *testing.T) {
	blanks := []string{"", "   ", "\n"}
	for _, in := range blanks {
		if got := normTitle(in); got != "" {
			t.Fatalf("空白标题归一化后必须为空串，输入 %q 实际 %q", in, got)
		}
	}
	// 空标题不得被判重：否则一条空标题会把该方向永久挡死
	if duplicated("", []string{"已有任务"}) {
		t.Fatal("空标题不得命中去重 —— 会把该方向永久挡死")
	}
	// recent 里的空串必须被跳过（不 panic、不影响其余判定）
	if duplicated("全新标题", []string{""}) {
		t.Fatal("recent 全为空串时不得判重")
	}
	// 判重口径：焦点段相同才算重复（目标前缀相同不算）
	if !duplicated("提升工作台质量 · 复盘", []string{"另一目标 · 复盘"}) {
		t.Fatal("焦点段相同必须判重（这是「同一阶段反复产出」的挡刀点）")
	}
	if duplicated("提升工作台质量 · 复盘", []string{"提升工作台质量 · 覆盖面"}) {
		t.Fatal("仅前缀相同、焦点不同不得判重 —— 否则阶段推进会被自己挡住")
	}
}
