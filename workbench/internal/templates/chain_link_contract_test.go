package templates

import "testing"

// TestTemplateChainHasNoBrokenLinks 钉住模板链的后继引用完整性。
//
// 为什么必须有（2026-10-25 实测）：引擎取后继的写法是
//
//	if t, exists := g.reg.Get(id); exists && t.Enabled { out = append(out, t) }
//
// 即一个**拼错或已被删除的后继 ID 会被静默跳过** —— 不报错、不打日志，
// 链在那一步无声停止延伸。7x24 无人值守下表现就是"某类任务再也不往后走了"，
// 而所有测试与门禁都是绿的。既有守卫只有 chain_test.go 里 4 个抽查用例，
// 覆盖不到 29 条模板的全部 11 条边；受跟踪与未跟踪测试里都没有链完整性断言（实测）。
//
// 判据只覆盖「客观错」的三类，且都是当前真值已满足的（实测 29 模板 / 11 条边：
// 断链 0、自环 0、同一后继列表内重复 0）：
//  1. 断链：后继 ID 不在模板集合里（静默丢边的直接成因）；
//  2. 自环：自己把自己列为后继（A→A 会在 MaxDepth 内反复衍生同一模板）；
//  3. 同一列表内重复 ID：会被引擎当成两个后继，白占 fanout 名额。
//
// 刻意不断言的两项（避免把设计选择写成缺陷）：
//   - 有向环（A→B→A）：跨轮交替是有意义的链形态，不是错；
//   - 域一致性（quality 链指向 content 之类）：链设计可能刻意跨域。
//
// 取的是 Defaults() 的字段真值，不解析 Go 源码 —— 见 quality-audit 要求 ⑤。
func TestTemplateChainHasNoBrokenLinks(t *testing.T) {
	all := Defaults()
	byID := make(map[string]tpl, len(all))
	for _, x := range all {
		if _, dup := byID[x.ID]; dup {
			t.Errorf("模板 ID %q 重复出现 —— Get() 只会命中其一，另一条静默失效", x.ID)
			continue
		}
		byID[x.ID] = x
	}
	edges := 0
	for _, x := range all {
		seen := make(map[string]bool, len(x.Chain.SuccessorTemplates))
		for _, id := range x.Chain.SuccessorTemplates {
			edges++
			if id == x.ID {
				t.Errorf("模板 %q 把自己列为后继（自环）", x.ID)
			}
			if _, ok := byID[id]; !ok {
				t.Errorf("模板 %q 的后继 %q 不存在 —— 引擎会静默跳过该边，链在此无声终止", x.ID, id)
			}
			if seen[id] {
				t.Errorf("模板 %q 的后继列表里 %q 重复 —— 会被算作两个后继，白占 fanout 名额", x.ID, id)
			}
			seen[id] = true
		}
	}
	// 受理面哨兵：若 Defaults() 或链字段被改坏成空，上面所有循环都不执行而判据恒绿。
	if len(all) == 0 {
		t.Fatal("Defaults() 返回空 —— 判据失去受理面")
	}
	if edges == 0 {
		t.Fatal("29 条模板的后继边总数为 0 —— 链配置整体丢失，判据在比空气")
	}
	t.Logf("链完整性 OK：%d 条模板、%d 条后继边，无断链/自环/重复", len(all), edges)
}
