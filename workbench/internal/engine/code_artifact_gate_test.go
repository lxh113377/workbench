package engine

import (
	"os"
	"strings"
	"testing"
)

// TestCodeArtifactMinSubstance 钉住代码产物的最小实质（round 69 · T73）。
//
// 现场：黑障解除后第一条 pass 的产物是 `m.py` **4 字节**，verify 是 `python m.py` ⇒ 解释器能启动就 exit 0。
// 阈值依据是两个真实数据点：4 字节的占位脚本必须挡；97 字节的 check_env.py（里程碑交付物）必须放过。
func TestCodeArtifactMinSubstance(t *testing.T) {
	cases := []struct {
		name, path, body, wantSub string
	}{
		{"4 字节占位脚本必须挡", "m.py", "pass", "代码产物过小"},
		{"97 字节真实小脚本必须放过", "check_env.py", strings.Repeat("# c\n", 24) + "print(1)\n", ""},
		// 容器 E2E 的确定性桩（40 字节）——第一版 64 字节下限把它判红、把 Autonomy Container E2E 打挂。
		// 这条用例就是那次事故的回归护栏：阈值必须放过**最该放过的真实样本**。
		{"容器 E2E 桩 result.py（40 字节）必须放过", "workspace/result.py", "print(\"hello from workbench container e2e\")\n", ""},
		{"数据文件不设体量下限", "data.json", "{}", ""},
		{"报告不受这条管（由报告门禁管）", "docs/x.md", "短", ""},
		{"沙箱相对路径同样生效", "workspace/tool.go", "package main", "代码产物过小"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := codeArtifactProblems(tc.path, []byte(tc.body))
			joined := strings.Join(got, "；")
			if tc.wantSub == "" {
				if len(got) > 0 {
					t.Fatalf("不该有问题，实得 %v", got)
				}
				return
			}
			if !strings.Contains(joined, tc.wantSub) {
				t.Fatalf("应命中 %q，实得 %v", tc.wantSub, got)
			}
		})
	}
}

// TestCodeArtifactGateRunsAfterVerifyOnlyOnPass 是**顺序与条件**守卫（T73 的关键取舍）。
//
// 本轮被既有契约测试当场纠正过一次：体量门禁若放在真实验收**之前**，就会把
// 「失败必须因脚本真实退出码 2」偷换成「产物过小」；若不分「已通过」就会把失败任务的真实原因也盖掉。
// 这两条语义只有源文本顺序能低成本钉住（要驱动真实验收需构造成套 fixture）。
func TestCodeArtifactGateRunsAfterVerifyOnlyOnPass(t *testing.T) {
	src, err := os.ReadFile("artifacts.go")
	if err != nil {
		t.Fatalf("读 artifacts.go 失败: %v", err)
	}
	body := string(src)
	callIdx := strings.Index(body, "codeArtifactProblems(f.Path, f.Body)...")
	runIdx := strings.Index(body, "vrep := verify.Run(")
	if callIdx < 0 || runIdx < 0 {
		t.Fatalf("锚点缺失（call=%d run=%d）", callIdx, runIdx)
	}
	if callIdx < runIdx {
		t.Fatal("体量门禁必须在真实验收**之后**：放在之前会把「失败因真实退出码」偷换成「产物过小」")
	}
	// 精确锚点（不能只找 "if out.Pass {" —— 文件末尾的日志分支也有同名判断，会误判有牙）：
	// 体量门禁必须紧跟在 `if out.Pass {` 之后、且用 "交付物实质不足：" 这个可区分的前缀。
	if !strings.Contains(body, "if out.Pass {\n\t\tvar tiny []string") {
		t.Fatal("体量门禁必须只降级**已通过**的产物（失败任务的真实原因不得被盖掉）—— 锚点：if out.Pass { var tiny []string")
	}
	if !strings.Contains(body, "交付物实质不足：") {
		t.Fatal("降级原因必须用可区分前缀（交付物实质不足），否则与「产物结构缺陷/报告内容缺陷」混淆")
	}
}
