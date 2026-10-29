package engine

import (
	"os"
	"strings"
	"testing"
)

// TestReportGateProblems 钉住报告内容门禁的判据（round 65 · T66）。
//
// 用例全部来自现场形态：stub 是 2026-10-29 真实通过验收的那份（「## 证据」只写了「无。」），
// substantive 是同一批里那份带 file:line + 可复跑命令的。
func TestReportGateProblems(t *testing.T) {
	stub := "# 五轴审查-正确性\n\n## 结论\n暂无发现真实性错误。\n\n## 证据\n无。\n\n## 门禁建议\n继续保持项目规范。\n"
	// 前提更新（round 66 · T66→T67，不是「改断言迁就实现」）：这份样本是 T66 当时的「实质报告」
	// 基准；T67 把目标验收语里的「五轴审查**分级**出报告」变成硬判据 ⇒ 原样本（三节齐 + 证据足
	// 但**完全没分级**）按新口径必须判红。这里把它升级成真正可贴 PR 的形态（带严重度），
	// 并新增一条用例钉住「原来那份现在会被拒」，防止口径变更被静默吸收。
	substantive := "# 五轴审查-缺陷报告\n\n## 结论\n上传类型只按后缀判断，存在任意文件执行风险。\n\n" +
		"## 证据\n- 严重度：高 ｜ file:app/server.py:128\n- 可复跑命令: `pytest -q tests/test_upload_security.py`\n\n" +
		"## 门禁建议\n1. 引入 MIME 校验\n2. CI 增加该测试\n"
	ungradedComplete := "# 五轴审查-缺陷报告\n\n## 结论\n有问题。\n\n" +
		"## 证据\n- file:app/server.py:128\n- 可复跑命令: `pytest -q t.py`\n\n## 门禁建议\n修\n"
	missing := "# 报告\n\n## 结论\n有问题。\n"
	proseOnly := "# 报告\n\n本报告结构见 ## 证据 一节（正文里提到小节名，不算真的有小节）。\n"

	cases := []struct {
		name    string
		path    string
		body    string
		wantSub string // 期望命中的问题关键字；空串表示应当没有问题
	}{
		{"stub 报告（证据节只有「无。」）必须被判不合格", "docs/五轴审查-正确性.md", stub, "证据"},
		{"带 file:line 且已分级的实质报告必须通过", "docs/五轴审查-缺陷报告.md", substantive, ""},
		// T67 新增三条分级判据用例：缺分级要拒、词族命中要放行、构词假阳性不得放行（防假绿）。
		{"三节齐、证据足但完全没分级 ⇒ 判不合格（T67 的核心欠项）", "docs/五轴审查-缺陷报告.md", ungradedComplete, "未分级"},
		{"P0 形态算已分级", "docs/x-审查.md", gradedVariant("P0"), ""},
		{"提高/其中/降低 这类构词不得当成已分级（假绿负控）", "docs/x-审查.md", gradedVariant("提高吞吐量，其中降低延迟"), "未分级"},
		{"缺小节要指出缺哪节", "docs/x.md", missing, "缺少小节"},
		{"正文提到小节名不算结构（文件名自述报告，故仍受门禁管）", "docs/审查报告-y.md", proseOnly, "缺少小节"},
		{"docs/ 下的普通文档不受报告门禁约束（本次事故的形态，防回归）", "docs/hello.md", "# 外部目录成功\n", ""},
		{"非 docs/ 下的 md 不管（避免误伤项目文档）", "src/readme.md", stub, ""},
		{"非 .md 产物不管", "docs/tool.py", stub, ""},
		{"docs/ 下的代码产物不管", "docs/check.py", "print(1)\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reportGateProblems(tc.path, []byte(tc.body))
			if tc.wantSub == "" {
				if len(got) > 0 {
					t.Fatalf("不该有问题，实得 %v", got)
				}
				return
			}
			joined := strings.Join(got, "；")
			if !strings.Contains(joined, tc.wantSub) {
				t.Fatalf("应命中 %q，实得 %v", tc.wantSub, got)
			}
		})
	}
}

// gradedVariant 造一份「三节齐 + 证据足」的报告，把分级位置填成 grade 文本。
// 用于成对验证分级判据：填真尺度（P0 / 严重度：高）应放行，填构词假阳性应判红。
func gradedVariant(grade string) string {
	return "# 审查报告\n\n## 结论\n结论。\n\n## 证据\n- file:a.py:1\n\n## 门禁建议\n修\n\n" + grade + "\n"
}

// TestReportGateWired 是接线守卫：内容门禁必须挂在 materializeArtifacts 里，且在真实执行之前。
//
// 为什么钉源文本：materializeArtifacts 需要真实任务目录/配置/日志，单测驱动它等于重造调用方；
// 而这次的回归风险正是「函数写了但没接上」（本仓已多次出现：T36/T42/T41/T50/T64 接线问题）。
func TestReportGateWired(t *testing.T) {
	src, err := os.ReadFile("artifacts.go")
	if err != nil {
		t.Fatalf("读 artifacts.go 失败: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "reportGateProblems(f.Path, f.Body)") {
		t.Fatal("报告内容门禁没有挂在 materializeArtifacts 的产物循环里")
	}
	if !strings.Contains(body, "报告内容缺陷：") {
		t.Fatal("判失败时必须用可区分的前缀（报告内容缺陷），否则台账里与结构缺陷混淆")
	}
	call := strings.Index(body, "reportGateProblems(f.Path, f.Body)")
	run := strings.Index(body, "vrep := verify.Run(")
	if call < 0 || run < 0 {
		t.Fatalf("锚点缺失（call=%d run=%d）", call, run)
	}
	if call > run {
		t.Fatal("内容门禁必须在**真实执行验收命令之前**：明显不合格就别浪费一次真实执行")
	}
}
