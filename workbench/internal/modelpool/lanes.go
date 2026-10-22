package modelpool

import (
	"fmt"
	"sort"
	"strings"
)

// ── 车道（池）批量开关 与 非免费车道清理 ──────────────────────────
//
// 老大 2026-10-22 令，两件事同属一处：
//  1. 「增加模型池各个免费池的一键启用一键停用功能」—— 面板按池（OFM 免费 /
//     Kilo 免费池 / EAC 协付 / 聚合渠道 / 自定义 / 地区受限）逐个批量开关，
//     不用一个个模型点。
//  2. 「将网关专属车道和网关其它车道去掉，只留 eac/kilo/ofm 等免费车道」——
//     这两类是渠道网关自有广播形态（deepseek-official/*、deepseek-account/*、
//     codearts/*、buddy/* …），provider 不认、写进 overlay 必失败；同步入口已
//     经不收（sync.go 的 default 分支返回 false），本文件的 PurgeGatewayLanes
//     负责把历史账本里已存在的存量条目摘掉（实测本机 pool.json 残留 20 条）。
//
// 判据边界：只清 provider=="gateway" 或分组名命中这两类车道字样的条目。
// 用户自己添加的聚合端点（lane=aggregate、source=user/panel、provider 非 gateway）
// 属于「兼容接口纳管」功能，绝不被误删。

// gatewayLaneGroups 是历史遗留的网关自有车道分组名（含「其它/其他」两种写法）。
var gatewayLaneGroups = map[string]bool{
	"网关专属车道": true,
	"网关其他车道": true,
	"网关其它车道": true,
}

// isGatewayLaneSpec 判断一个 Spec 是否属于要下线的网关自有车道。
func isGatewayLaneSpec(sp Spec) bool {
	if strings.TrimSpace(sp.Provider) == "gateway" {
		return true
	}
	return gatewayLaneGroups[strings.TrimSpace(sp.Group)]
}

// PurgeResult 是一次存量清理的结果。
type PurgeResult struct {
	Removed []string `json:"removed"`
	At      int64    `json:"at"`
}

// PurgeGatewayLanes 从池子里删除网关专属/网关其它车道的全部条目（含健康度）。
// 幂等：没有命中项时不落盘、不写日志。
func (m *Manager) PurgeGatewayLanes(actor string) PurgeResult {
	res := PurgeResult{At: nowMs()}
	m.mu.Lock()
	kept := make([]Spec, 0, len(m.specs))
	removedIDs := make([]string, 0, 8)
	for _, sp := range m.specs {
		if isGatewayLaneSpec(sp) {
			removedIDs = append(removedIDs, sp.ID)
			continue
		}
		kept = append(kept, sp)
	}
	if len(removedIDs) > 0 {
		m.specs = kept
		for _, id := range removedIDs {
			delete(m.health, id)
		}
		m.syncSpecStateLocked()
	}
	m.mu.Unlock()
	if len(removedIDs) == 0 {
		return res
	}
	sort.Strings(removedIDs)
	res.Removed = removedIDs
	m.persist()
	m.log(logRecord{Kind: "crud", State: "purge-gateway-lane",
		Reason: fmt.Sprintf("清理网关专属/其它车道存量 %d 条（只保留 eac/kilo/ofm 等免费车道）", len(removedIDs)),
		Actor:  actor})
	m.NotifyRoutes()
	return res
}

// PoolToggle 是一次「一键启用/一键停用」的结果。
type PoolToggle struct {
	Lane     Lane     `json:"lane,omitempty"`
	Group    string   `json:"group,omitempty"`
	Enabled  bool     `json:"enabled"`
	Affected []string `json:"affected"`
	// Skipped 是本来就在目标状态、没动的条目数（面板回显「其中 N 个已是该状态」）。
	Skipped int   `json:"skipped"`
	At      int64 `json:"at"`
}

// SetPoolEnabled 一键启用（enabled=true）或一键停用（enabled=false）一个池。
//
// lane 与 group 至少给一个。**group 优先**（面板的分组卡就是「池」的单位：
// 组名如「OFM 免费」/「Kilo 免费池」/「EAC 协付」/「聚合渠道」/「自定义」/
// 「地区受限（保留可见）」）；只给 lane 时按车道匹配（CLI/脚本用法）。
//
// 为什么不能取交集：面板表单允许把自填端点选成任意 lane（如 aggregate），
// 而 Upsert 对空 Group 一律落「自定义」—— 交集会把这类真实存在的池判成
// 「没有模型」，按钮点了报错。卡片既然按 group 分桶，开关就必须按 group 命中。
//
// 手动批量开关要赢过上游自动判定：命中条目一律清除 AutoDisabled 标记。
// 否则「用户一键停用」会被下一轮网关同步按「仍在广播」自动恢复（revive 的判据
// 正是 !Enabled && AutoDisabled），按钮就成了摆设 —— 这与单模型开关同一语义。
func (m *Manager) SetPoolEnabled(lane, group string, enabled bool, actor string) (PoolToggle, error) {
	res := PoolToggle{Enabled: enabled, At: nowMs()}
	target := Lane(strings.TrimSpace(lane))
	groupKey := strings.TrimSpace(group)
	if target == "" && groupKey == "" {
		return res, fmt.Errorf("必须指定车道或分组")
	}
	switch target {
	case "", LaneFree, LaneKilo, LaneEAC, LaneAggregate, LaneCustom:
	default:
		return res, fmt.Errorf("未知车道: %s（可选 free/kilo/eac/aggregate/custom）", string(target))
	}
	m.mu.Lock()
	affected := make([]string, 0, 16)
	skipped := 0
	matched := 0
	for i := range m.specs {
		sp := &m.specs[i]
		if groupKey != "" {
			if strings.TrimSpace(sp.Group) != groupKey {
				continue
			}
		} else if sp.Lane != target {
			continue
		}
		matched++
		if sp.Enabled == enabled {
			skipped++
			if sp.AutoDisabled {
				sp.AutoDisabled = false
				sp.UpdatedAt = nowMs()
			}
			continue
		}
		sp.Enabled = enabled
		sp.AutoDisabled = false
		sp.UpdatedAt = nowMs()
		affected = append(affected, sp.ID)
		h := m.health[sp.ID]
		if h == nil {
			h = &Health{ID: sp.ID}
			m.health[sp.ID] = h
		}
		if enabled {
			if h.State == StateDisabled {
				h.State = StateUnknown
			}
			h.CooldownTo = 0
			h.ConsecFail = 0
		} else {
			h.State = StateDisabled
		}
	}
	if matched == 0 {
		m.mu.Unlock()
		return res, fmt.Errorf("该池没有模型（车道=%s 分组=%s）", string(target), groupKey)
	}
	m.syncSpecStateLocked()
	m.mu.Unlock()
	res.Lane = target
	res.Group = groupKey
	res.Skipped = skipped
	res.Affected = affected
	m.persist()
	action := "启用"
	state := "enable-pool"
	if !enabled {
		action = "停用"
		state = "disable-pool"
	}
	label := firstNonEmpty(groupKey, string(target))
	m.log(logRecord{Kind: "crud", State: state,
		Reason: fmt.Sprintf("一键%s「%s」：改动 %d 个模型（其中 %d 个已是目标状态）", action, label, len(affected), skipped),
		Actor:  actor})
	m.NotifyRoutes()
	return res, nil
}
