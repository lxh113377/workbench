package engine

import (
	"os"
	"strings"
	"testing"

	"workbench/internal/artifact"
	"workbench/internal/model"
)

// TestDeliverableKindGate 钉住 T88：被要求交报告（prompt 含引擎注入的 marker）却没交任何 .md ⇒ 不算通过。
//
// 触发器只用 marker，绝不用任务标题词族 —— 实测词族命中 1,120 条而真缺陷只有 5 条（收益≈0、误杀面巨大）。
// 判定只看结构（有没有 .md），不看内容：内容是 T66/T67 的活，越权就会重演 T73 把容器桩判红那次事故。
func TestDeliverableKindGate(t *testing.T) {
	mk := model.DeliverableGateMarker
	if mk == "" {
		t.Fatal("marker 常量为空 ⇒ 触发器永久失效（门会永远沉默，比误杀更危险）")
	}
	cases := []struct {
		name    string
		prompt  string
		files   []artifact.File
		wantHit bool
	}{
		{"要求交报告 + 只交脚本（现场 3 条 pass=true 的确切形态）",
			"目标X\n" + mk + "（硬性）\n1. ...", []artifact.File{{Path: "explore.py"}}, true},
		{"要求交报告 + 交了 .md ⇒ 放（内容好坏归 T66/T67）",
			mk, []artifact.File{{Path: "docs/wu-zhou-shen-cha-xing-neng.md"}}, false},
		{"要求交报告 + .md 不在 docs/ ⇒ 本门不拦（落点是 T74 的职责，不越权重复判）",
			mk, []artifact.File{{Path: "bao-gao.md"}}, false},
		{"容器 E2E 形态（result.py + 占位 result.md）⇒ 必放：最该放过的真实样本（T73 教训）",
			mk, []artifact.File{{Path: "result.py"}, {Path: "result.md"}}, false},
		{"大小写不敏感：REPORT.MD 也算交了 .md",
			mk, []artifact.File{{Path: "REPORT.MD"}}, false},
		{"没被要求交报告 + 只交代码 ⇒ 放（非报告类模板与文章类不受影响）",
			"写一篇深度长文，约 8000 字", []artifact.File{{Path: "main.go"}}, false},
		{"空 prompt ⇒ 放（防御：不给无信息的任务编造要求）",
			"", []artifact.File{{Path: "a.py"}}, false},
		{"要求交报告但一个产物都没有 ⇒ 拦（另一种结构性失败）",
			mk, nil, true},
		{"marker 在正文中段（真实注入在尾部，但判据不依赖位置）",
			"前文\n\n" + mk + "（硬性，与引擎判据同口径）\n1. 交付物是文件", []artifact.File{{Path: "m.py"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deliverableKindProblems(tc.prompt, tc.files)
			if tc.wantHit && len(got) == 0 {
				t.Fatalf("该拦未拦：prompt=%q files=%v", tc.prompt, tc.files)
			}
			if !tc.wantHit && len(got) > 0 {
				t.Fatalf("该放未放（误杀）：%v ｜ prompt=%q files=%v", got, tc.prompt, tc.files)
			}
		})
	}
}

// TestDeliverableGateSingleSource 防止 T88 长成"第三份私有清单"（本会话同族病）：
// 注入侧两个包必须引用同一个 marker 常量，不许把标题写成字面量 —— 否则改注入侧时判定侧不知道。
// 被检文件只有 defaults.go / planner.go，不含本测试文件（守卫自己必须能搜到字面量才成立）。
func TestDeliverableGateSingleSource(t *testing.T) {
	lit := "## 交付物门禁"
	for _, p := range []string{"../templates/defaults.go", "../autonomy/planner.go"} {
		raw := readSourceOrFatal(t, p)
		if !strings.Contains(raw, "model.DeliverableGateMarker") {
			t.Errorf("%s 不再引用 model.DeliverableGateMarker ⇒ 注入侧与判定侧脱钩，T88 会永久沉默", p)
		}
		if strings.Contains(raw, lit) {
			t.Errorf("%s 里又出现门禁标题字面量 ⇒ 必须走 model.DeliverableGateMarker（同族病的标准形态）", p)
		}
	}
}

// readSourceOrFatal 读同仓源码做对照：判据必须作用在真实文件上，读不到判红而非跳过。
func readSourceOrFatal(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读不到 %s：%v", p, err)
	}
	return string(b)
}
