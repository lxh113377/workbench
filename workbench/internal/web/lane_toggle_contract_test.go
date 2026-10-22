package web

import (
	"strings"
	"testing"
)

// ────────────────────────────────────────────────────────────────
// 模型池「各免费池一键启用/一键停用」+「去掉网关专属/网关其它车道」契约
//（老大 2026-10-22 令：增加模型池各个免费池的一键启用一键停用功能，
//  将网关专属车道和网关其它车道去掉，只留 eac/kilo/ofm 等免费车道）
//
// 分工：后端行为（SetPoolEnabled / PurgeGatewayLanes）由 modelpool 包
// 与 pool_api_test.go 覆盖；本件钉的是「用户看得见、点得到」——
// 分组卡真的带一键按钮、真的打到 /api/pool/lane、名册里不再有网关两条车道。
// 判据写成可复用判读函数，再由坏样本反证它确实会红（沿用
// pool_touch_contract_test.go 的 laneAudit 范式，禁恒真式假绿）。
// ────────────────────────────────────────────────────────────────

var gatewayLaneNames = []string{"网关专属车道", "网关其他车道", "网关其它车道"}
var freePoolKeeps = []string{"OFM 免费", "Kilo 免费池", "EAC 协付", "聚合渠道", "自定义"}

// groupOrderAudit 判读分组名册：返回「缺失的应保留池」与「不该出现的网关车道」。
func groupOrderAudit(orderLine string) (missing, stale []string) {
	for _, k := range freePoolKeeps {
		if !strings.Contains(orderLine, "'"+k+"'") {
			missing = append(missing, k)
		}
	}
	for _, g := range gatewayLaneNames {
		if strings.Contains(orderLine, g) {
			stale = append(stale, g)
		}
	}
	return missing, stale
}

// oneClickAudit 判读分组一键开关：返回缺失要素清单。
func oneClickAudit(js string) []string {
	want := []string{
		`data-gact="genable"`, `data-gact="gdisable"`,
		"一键启用", "一键停用", "/api/pool/lane", "const onGroupAction =",
	}
	var miss []string
	for _, w := range want {
		if !strings.Contains(js, w) {
			miss = append(miss, w)
		}
	}
	return miss
}

// firstLine 取 s 中 marker 所在行（含换行截断）。
func firstLine(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	rest := s[i:]
	if j := strings.Index(rest, "\n"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func TestPoolGroupOrderHasNoGatewayLanes(t *testing.T) {
	line := firstLine(poolJS, "const GROUP_ORDER =")
	if line == "" {
		t.Fatal("未能定位 GROUP_ORDER 定义（改名/删除 ⇒ 判据必须红，不得静默跳过）")
	}
	missing, stale := groupOrderAudit(line)
	if len(stale) > 0 {
		t.Errorf("分组名册仍含已下线的网关车道 %v（老大令：只留 eac/kilo/ofm 等免费车道）", stale)
	}
	if len(missing) > 0 {
		t.Errorf("分组名册缺少免费池 %v", missing)
	}
	// 一键开关按 lane 兜底，车道映射名册必须与后端 Lane 常量一致。
	laneLine := firstLine(poolJS, "const GROUP_LANE =")
	if laneLine == "" {
		t.Fatal("未能定位 GROUP_LANE 定义（一键开关按车道兜底的映射）")
	}
	for _, v := range []string{"free", "kilo", "eac", "aggregate", "custom"} {
		if !strings.Contains(laneLine, "'"+v+"'") {
			t.Errorf("GROUP_LANE 缺少车道映射 %q", v)
		}
	}
}

func TestPoolGroupCardsCarryOneClickButtons(t *testing.T) {
	if miss := oneClickAudit(poolJS); len(miss) > 0 {
		t.Errorf("pool.js 缺少分组一键开关要素 %v", miss)
	}
	// 停用整池是破坏性动作（该组模型立刻退出调度），必须两段式确认。
	seg := sliceBetween(poolJS, "const onGroupAction =", "// ── 添加/编辑")
	if seg == "" {
		t.Fatal("未能定位 onGroupAction 段落（判据失去受理面）")
	}
	if !strings.Contains(seg, "armConfirm(btn") {
		t.Error("一键停用未接两段式确认（一次误点即整池退出调度）")
	}
}

// TestPoolOneClickContractRejectsBadSamples 反假绿：退回改造前形态，判读必须报红。
func TestPoolOneClickContractRejectsBadSamples(t *testing.T) {
	// 坏样本 1：塞回「网关专属车道」⇒ stale 必须命中。
	line := firstLine(poolJS, "const GROUP_ORDER =")
	if line == "" {
		t.Fatal("定位 GROUP_ORDER 失败，反证无法进行")
	}
	bad := strings.Replace(line, "'自定义'", "'自定义', '网关专属车道'", 1)
	if !strings.Contains(bad, "网关专属车道") {
		t.Fatal("坏样本 1 构造失败")
	}
	_, stale := groupOrderAudit(bad)
	if len(stale) != 1 || stale[0] != "网关专属车道" {
		t.Fatalf("坏样本 1 应被判出网关车道，实际 %v ⇒ 判据恒真（假绿）", stale)
	}
	// 坏样本 2：抹掉一键按钮要素 ⇒ oneClickAudit 必须报缺。
	stripped := strings.ReplaceAll(poolJS, `data-gact="genable"`, "")
	miss := oneClickAudit(stripped)
	if len(miss) == 0 {
		t.Fatal("坏样本 2 仍被判绿 ⇒ 一键开关判据恒真")
	}
	// 坏样本 3：把免费池之一删掉 ⇒ missing 必须命中。
	badOrder := strings.Replace(line, "'Kilo 免费池',", "", 1)
	m2, _ := groupOrderAudit(badOrder)
	if len(m2) == 0 || m2[0] != "Kilo 免费池" {
		t.Fatalf("坏样本 3 应报缺 Kilo 免费池，实际 %v", m2)
	}
}
