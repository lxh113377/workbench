package templates

import (
	"strings"
	"testing"
)

// TestEncodedLessonsSurviveRender 钉住「写进模板的教训必须真的到达 prompt」。
//
// 为什么需要（2026-10-25 round 122-137）：这几轮我往 quality-audit 加了要求⑤（测量学四条）、
// 往 quality-verify 加了要求⑥（时序类判据须给复现率）、又加了⑥（写入工具会剥 BOM）。
// 但模板文本是 Go 的**多段字符串拼接** —— 少一个加号、多一个转义，编译可能仍过（我本轮
// 就把⑥的末行写成逗号，当场被 go build 抓到），而渲染结果会静默缺一整条。
// 更糟的是这些条目的价值全在「下游 agent 读到」，少一条等于没写，却没有任何测试会失败。
//
// 判据取 Render 之后的真值（不比对源码文本）：读字段值必须执行程序取运行时真值 ——
// 这条本身就是要求⑤ 的第一条，用来自查正好。
//
// 反向也要守住：渲染文本里出现字面反斜杠，就是拼接转义写坏（本轮真发生过一次，
// 我把行尾转义写成双反斜杠，渲染出「仍绿）。\」这种残渣，而探针查的是「双反斜杠」
// 于是假绿 —— 判据比缺陷窄）。所以这里查「任何反斜杠」。
func TestEncodedLessonsSurviveRender(t *testing.T) {
	byID := map[string]tpl{}
	for _, x := range Defaults() {
		byID[x.ID] = x
	}
	// 每条：模板 ID + 必须出现在渲染结果里的片段（片段取该要求的特征词，不取整句以免措辞微调即红）
	wants := []struct {
		id  string
		env []string
	}{
		{"quality-audit", []string{
			"运行时真值", // ⑤ 第一条：读字段值取运行时真值
			"退出码",   // ⑤ 第二条：命令须查 rc 并打印错误
			"待核线索",  // ⑤ 第三条：分类器输出不是结论
			"先测分布",  // ⑤ 第四条：涉惯例先测分布
			"复验编码",  // ⑥：写入/编辑工具会剥 BOM，改完逐字节复验
		}},
		{"quality-verify", []string{
			"复现率",     // ⑥：时序类判据必须给复现率
			"变异实验自身",  // ⑦：变异实验自身要有受理面检查（判红先排除编译错）
			"没测成",     // ⑧：结论只能是已验证有效或未验证
			"真的改掉了目标", // ⑨：变异样本必须自证真的改掉了目标（防无效变异伪装成判据漏抓）
		}},
		{"quality-gate-spec", []string{
			"静态配对检查", // ⑥：判据读外部数据件时必须配静态配对检查
			"字段缺失",   // ⑥：规格必须写清字段缺失/为 null 时怎么办
			"浅克隆",    // ⑦：依赖 git 历史的判据必须处理浅克隆取空
		}},
		{"review-five-axis", []string{
			"合并结论", "门禁建议", // 交付面承诺：报告须含合并结论与门禁建议
		}},
	}
	for _, w := range wants {
		got, ok := byID[w.id]
		if !ok {
			t.Errorf("模板 %s 不在 Defaults() 里 —— 判据失去受理面", w.id)
			continue
		}
		if err := Validate(got); err != nil {
			t.Errorf("%s Validate 失败: %v", w.id, err)
			continue
		}
		out := Render(got.PromptTemplate, RenderVars{
			Topic: "主题", Words: "95", Context: "上下文", Date: "2026-10-25", Prev: "前序",
		})
		for _, frag := range w.env {
			if !strings.Contains(out, frag) {
				t.Errorf("%s 的渲染结果缺片段 %q —— 该条教训没到达 prompt，等于没写（多段字符串拼接被改坏时就是这样静默丢的）", w.id, frag)
			}
		}
		if n := strings.Count(out, string(rune(92))); n != 0 {
			t.Errorf("%s 渲染结果含 %d 个字面反斜杠 —— 拼接转义写坏，prompt 里会出现残渣文本", w.id, n)
		}
		for _, ph := range []string{"{topic}", "{words}", "{context}", "{date}", "{prev}"} {
			if strings.Contains(out, ph) {
				t.Errorf("%s 渲染后仍有未替换占位符 %s", w.id, ph)
			}
		}
		t.Logf("%s 渲染 %d 字，编码教训 %d 条全部到位", w.id, len(out), len(w.env))
	}
}
