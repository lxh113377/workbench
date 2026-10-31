package autonomy

import (
	"os"
	"strings"
	"testing"

	"workbench/internal/model"
)

// TestAutonomyGateCarriesPlacementClause 钉住另一处注入点（自主规划）也带落点条款，且与 model 单点逐字一致。
//
// 为什么两边都要有：探查脚本污染来自**两条产出路径** —— 模板任务（templates）与自主任务（autonomy 规划器）。
// 只约束一边，另一边照样把 explore.ps1 / nul 写进被审查的项目根。
// 判据取 autonomyDeliverableGate 的**运行时值**（同包常量），不是读 planner.go 猜。
func TestAutonomyGateCarriesPlacementClause(t *testing.T) {
	if !strings.Contains(autonomyDeliverableGate, model.GatePlacementClause) {
		t.Error("autonomy 侧交付物门禁缺落点条款 ⇒ 自主任务会把探查脚本写进被审查项目根（templates 侧已约束即为分叉）")
	}
	if !strings.Contains(autonomyDeliverableGate, "交付物不在此列") {
		t.Error("autonomy 侧未写明交付物例外 ⇒ 模型可能把报告写进临时目录，反被 T88 判红")
	}
	if !strings.Contains(autonomyDeliverableGate, model.DeliverableGateMarker) {
		t.Error("autonomy 侧缺门禁标题 ⇒ 判定侧（deliverable_kind_gate）会认为任务没被要求交报告")
	}
	src, err := os.ReadFile("planner.go")
	if err != nil {
		t.Fatalf("读 planner.go 失败：%v", err)
	}
	if n := strings.Count(string(src), "探查用的临时脚本与重定向输出"); n > 0 {
		t.Errorf("planner.go 里出现条款正文（%d 处）⇒ 已退化为两处各写一份；正文只应存在于 model/deliverable_gate.go", n)
	}
	if !strings.Contains(string(src), "+ model.GatePlacementClause +") {
		t.Fatal("planner.go 不再引用 model.GatePlacementClause ⇒ 单点被绕过")
	}
	t.Logf("autonomy 侧条款齐全（门禁标题 + 落点条款 + 交付物例外），且引用单点常量")
}
