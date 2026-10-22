package web

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ────────────────────────────────────────────────────────────────
// 模型池窄屏/触控契约（task-1 · pool-ui · 标识 pool-ui-touch-1）
//
// 两个实测缺陷写成机器判据，谁改回去测试立刻红：
//  1. ≤720px 卡片模式下每格字段名靠 td::before{content:attr(data-label)} 显示，
//     车道水位表的 5 个 td 曾经没有 data-label ⇒ 窄屏只剩裸数字；
//  2. 行内操作按钮是裸 <button>，项目 44px 触控基线（.btn{min-height:44px}）覆盖不到它，
//     判据要求媒体查询同时含 (pointer:coarse) 与 (max-width:720px)，且 min-height ≥ 44。
//
// 两个判据都抽出「可复用判读函数」，再由 TestPoolTouchContractRejectsBadSamples
// 用坏样本反证它们确实会红（防判据恒真式假绿）。
// ────────────────────────────────────────────────────────────────

// normWS 把连续空白折叠成单个空格：Go 字符串常量保留了作者排版，
// 判据只关心规则内容，不该被缩进/换行差异左右。
func normWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// sliceBetween 取 [from, to) 区间；缺失任一标记返回空串（判据会因空串直接失败，
// 不静默跳过 —— 段落被改名时必须红，而不是悄悄变绿）。
func sliceBetween(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return ""
	}
	rest := s[i+len(from):]
	j := strings.Index(rest, to)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// laneAudit 判读车道表：返回表头列名、缺 data-label 的列、以及裸 <td> 个数。
func laneAudit(lanes string) (headers, missing []string, bareTD int) {
	thead := sliceBetween(lanes, "<thead>", "</thead>")
	for _, p := range strings.Split(thead, "<th>")[1:] {
		if k := strings.Index(p, "</th>"); k >= 0 {
			headers = append(headers, strings.TrimSpace(p[:k]))
		}
	}
	for _, h := range headers {
		if !strings.Contains(lanes, `data-label="`+h+`"`) {
			missing = append(missing, h)
		}
	}
	return headers, missing, strings.Count(lanes, "<td>")
}

// poolActionMinHeight 判读操作按钮触控规则：返回 min-height 像素值。
// 第二个返回值为 false 表示「媒体查询或 .pool-row-actions button 规则不存在」。
func poolActionMinHeight(page string) (int, bool) {
	const mq = "@media (pointer:coarse), (max-width:720px){"
	i := strings.Index(page, mq)
	if i < 0 {
		return 0, false
	}
	rest := page[i+len(mq):]
	j := strings.Index(rest, ".pool-row-actions button")
	if j < 0 {
		return 0, false
	}
	seg := rest[j:]
	end := strings.Index(seg, "}")
	if end < 0 {
		return 0, false
	}
	k := strings.Index(seg[:end], "min-height:")
	if k < 0 {
		return 0, false
	}
	num := strings.TrimSpace(seg[k+len("min-height:"):])
	digits := ""
	for _, c := range num {
		if c < '0' || c > '9' {
			break
		}
		digits += string(c)
	}
	if digits == "" {
		return 0, false
	}
	px, err := strconv.Atoi(digits)
	if err != nil {
		return 0, false
	}
	return px, true
}

// ── 缺陷 1：车道水位表窄屏字段名 ──────────────────────────────
func TestPoolLaneCellsCarryDataLabel(t *testing.T) {
	lanes := sliceBetween(poolJS, "const renderLanes", "// ── 模型清单")
	if lanes == "" {
		t.Fatal("未能在 poolJS 中定位 renderLanes 段落（段落被改名/删除 ⇒ 判据必须红，不得静默跳过）")
	}
	headers, missing, bareTD := laneAudit(lanes)
	if len(headers) != 5 {
		t.Fatalf("车道表表头应为 5 列（车道/总数/可用/下线/停用），实际 %d 列：%v", len(headers), headers)
	}
	if len(missing) != 0 {
		t.Errorf("车道表 td 缺少 data-label %v（窄屏卡片模式下这几格显示无字段名的裸数字）", missing)
	}
	if bareTD != 0 {
		t.Errorf("车道表仍有 %d 个裸 <td>（无 data-label ⇒ 窄屏无字段名）", bareTD)
	}
	if !strings.Contains(lanes, `colspan="5"`) {
		t.Error("车道表空态行应保留 colspan=5（列数与表头一致）")
	}
}

// ── 缺陷 2：行内操作按钮 44px 触控基线 ────────────────────────
func TestPoolRowActionButtonsMeetTouchBaseline(t *testing.T) {
	px, ok := poolActionMinHeight(normWS(modelPoolPage))
	if !ok {
		t.Fatal("模型池页内 <style> 缺少 @media (pointer:coarse), (max-width:720px){ .pool-row-actions button{min-height:...} }（触屏/窄屏下操作按钮高度 <44px）")
	}
	if px < 44 {
		t.Fatalf("操作按钮 min-height=%dpx < 44px（项目触控基线）", px)
	}
	// 触控规则必须落在模型池页内样式（不得依赖 assets_css.go 的 .btn —— 裸 button 不在其内）。
	if strings.Contains(normWS(appCSS), ".pool-row-actions button") {
		t.Error("模型池专项规则不应写进全局 assets_css.go（写域属他人）；请留在 pool_page.go 页内 <style>")
	}
}

// ── 反假绿：判据自身必须能红 ────────────────────────────────
func TestPoolTouchContractRejectsBadSamples(t *testing.T) {
	// 坏样本 1：把真实车道段落的 data-label 全剥掉（= 修复前的形态），判读必须报缺列。
	lanes := sliceBetween(poolJS, "const renderLanes", "// ── 模型清单")
	if lanes == "" {
		t.Fatal("定位 renderLanes 失败，反证无法进行")
	}
	badLanes := regexp.MustCompile(` data-label="[^"]*"`).ReplaceAllString(lanes, "")
	if strings.Contains(badLanes, "data-label=") {
		t.Fatal("坏样本 1 构造失败（仍有 data-label 残留）")
	}
	headers, missing, bareTD := laneAudit(badLanes)
	if len(headers) != 5 {
		t.Fatalf("坏样本 1 的 thead 仍应可读（5 列），实际 %v", headers)
	}
	if len(missing) != 5 {
		t.Fatalf("坏样本 1 应被判为「5 列全缺 data-label」，实际缺 %v ⇒ 判据恒真（假绿）", missing)
	}
	if bareTD != 5 {
		t.Fatalf("坏样本 1 应含 5 个裸 <td>，实际 %d", bareTD)
	}

	// 坏样本 2：媒体查询只留 max-width（= 漏掉触屏笔记本这一档）。
	page := normWS(modelPoolPage)
	badPage := strings.Replace(page, "@media (pointer:coarse), (max-width:720px){", "@media (max-width:720px){", 1)
	if _, ok := poolActionMinHeight(badPage); ok {
		t.Fatal("坏样本 2 仍被判据判绿 ⇒ pointer:coarse 判据恒真（假绿）")
	}

	// 坏样本 3：媒体查询完好但 min-height 掉到 30px（= 低于触控基线）。
	badPage3 := regexp.MustCompile(`min-height:[0-9]+px`).ReplaceAllString(page, "min-height:30px")
	px, ok := poolActionMinHeight(badPage3)
	if !ok {
		t.Fatal("坏样本 3 构造失败（触控规则被误删）")
	}
	if px >= 44 {
		t.Fatalf("坏样本 3 的 min-height 应 < 44，实际 %d ⇒ 44px 阈值判据失效", px)
	}
}
