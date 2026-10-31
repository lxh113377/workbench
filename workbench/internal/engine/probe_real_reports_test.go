package engine

import "testing"

// TestReportGateRejectsRealPlaceholderForms 把 round 151 在真实现场产物上量到的三种
// "看起来像交付、实际不是报告"的形态固化成回归（正文取现场节选，形态逐字保留）。
//
// 现场依据（agent 根 tasks/*/artifacts-index.json + 仓库根 docs/ 的三份产物）：
//
//	· docs/五轴审查-正确性.md  103 B —— 三节齐全，但「证据」节内容是「无。」      ⇒ 2 个问题
//	· docs/五轴审查-缺陷报告.md 1032 B —— 有 file:line 与可复跑命令，但通篇不分级 ⇒ 1 个问题
//	· docs/五轴审查-复盘覆盖度.md 305 B —— 「待填充」占位，连三节都没有            ⇒ 4 个问题
//
// 三者都曾被当成"报告级交付"记账；本判据确保它们再也不能蒙混过关。
func TestReportGateRejectsRealPlaceholderForms(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		wantAt int
	}{
		{"三节齐全但证据为无", "## 结论\n暂无发现真实性错误。\n\n## 证据\n无。\n\n## 门禁建议\n继续保持项目规范。\n", 2},
		{"有证据但未分级", "## 结论\n存在缺陷。\n\n## 证据\n`app/server.py:128` 用 endswith 判类型；可复跑 `python -m pytest tests/ -k upload`。\n\n## 门禁建议\n补白名单校验。\n", 1},
		{"待填充占位（连三节都没有）", "# 复盘：对照原目标检查覆盖度与遗漏\n\n> 状态：待填充（本次未能取得仓库读取工具，故不输出不可复核结论）。\n", 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const path = "docs/五轴审查-x.md"
			// 先断"进了门"——上一版探针我把路径写成 ../../../docs/... 导致整道门被跳过、
			// 读出"问题数=0 ⇒ 判合格"的假绿（空集读成 PASS，本会话登记过的形态）。⇒ 进门必须先自证。
			if !isReportArtifact(path) {
				t.Fatal("探针构造不合规：路径没进门禁，测的是空气")
			}
			if !claimsReport(path, tc.body) {
				t.Fatalf("自称报告判定为 false ⇒ 该形态被静默放行（body 前 30 字=%q）", tc.body[:30])
			}
			got := reportGateProblems(path, []byte(tc.body))
			if len(got) < tc.wantAt {
				t.Fatalf("问题数 %d < 期望 %d ⇒ 门禁放松了：%v", len(got), tc.wantAt, got)
			}
		})
	}
}
