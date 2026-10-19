package modelpool

import (
	"fmt"
	"sort"
	"strings"
)

// ── 网关清单 → 池子同步 ─────────────────────────────────────────
//
// 实测（2026-10-19，8326 /v1/models 共 28 个 id）三类命名：
//
//	our-free-model/<ofm-id>          DSH 的 our-free-model provider 直接认得，可调度
//	our-free-model-region/<ofm-id>   上游按地区拦截，保留可见但默认停用
//	deepseek-official/<model>        渠道网关自有车道，只能经网关 OpenAI 接口调用，
//	                                 不能喂给 DSH overlay（provider 不认），默认不可路由
//
// 同步策略：以网关清单为「有哪些」的权威，以池子已有条目为「怎么用」的权威 ——
// 用户改过的优先级/权重/启停/密钥一律保留，只补新增、只更新上游元信息。

// SyncResult 是一次同步的统计。
type SyncResult struct {
	Added   int      `json:"added"`
	Updated int      `json:"updated"`
	Skipped int      `json:"skipped"`
	AddedID []string `json:"addedIds"`
	// Revived 记录「原先自动停用、因网关重新广播而恢复」的模型。
	Revived []string `json:"revived"`
	At      int64    `json:"at"`
}

// SpecFromGatewayModel 把一个网关模型 id 翻译成池内 Spec。
func SpecFromGatewayModel(gw Gateway, modelID string) (Spec, bool) {
	id := strings.TrimSpace(modelID)
	if id == "" {
		return Spec{}, false
	}
	gwBase := gw.BaseURL
	s := Spec{
		BaseURL: gwBase, APIKey: gw.Key, Format: FormatChat, Enabled: true,
		Source: "gateway-sync", UpdatedAt: nowMs(),
	}
	switch {
	case strings.HasPrefix(id, "our-free-model-region/"):
		ofmID := strings.TrimPrefix(id, "our-free-model-region/")
		s.ID = ofmID
		s.Model = ofmID
		s.Label = "地区受限 " + ofmID
		s.Provider = "our-free-model"
		s.Lane = LaneFree
		s.Group = "地区受限（保留可见）"
		s.Tier = "fast"
		s.Priority = 900
		s.Weight = 5
		s.Enabled = false
		s.AutoDisabled = true
		s.Note = "上游对该地区不放行，已自动停用；换出口后可手动启用"
		s.Routable = true
	case strings.HasPrefix(id, "our-free-model/"):
		ofmID := strings.TrimPrefix(id, "our-free-model/")
		s.ID = ofmID
		s.Model = ofmID
		s.Label = "OFM " + ofmID
		s.Provider = "our-free-model"
		s.Lane = classifyOFMLane(ofmID)
		s.Group = laneLabel(s.Lane)
		s.Tier = "balanced"
		s.Priority = 100
		s.Weight = 50
		s.Routable = true
		s.Note = "经 OFM 渠道网关广播（DSH 原生车道）"
	case strings.HasPrefix(id, "deepseek-official/"):
		s.ID = id
		s.Model = id
		s.Label = "网关 " + strings.TrimPrefix(id, "deepseek-official/")
		s.Provider = "gateway"
		s.Lane = LaneAggregate
		s.Group = "网关专属车道"
		s.Tier = "quality"
		s.Priority = 800
		s.Weight = 10
		s.Enabled = false
		s.Routable = false
		s.Note = "只能经渠道网关 OpenAI 接口调用，DSH provider 不认，默认不参与任务调度"
	default:
		s.ID = id
		s.Model = id
		s.Label = id
		s.Provider = "gateway"
		s.Lane = LaneAggregate
		s.Group = "网关其他车道"
		s.Tier = "balanced"
		s.Priority = 500
		s.Weight = 20
		s.Routable = strings.Contains(id, "/")
		s.Note = "网关发现的未分类模型"
	}
	if s.Priority <= 0 {
		s.Priority = 100
	}
	if s.Weight <= 0 {
		s.Weight = 50
	}
	s.MaxFail = 3
	s.CooldownSec = 120
	return s, true
}

// classifyOFMLane 依据 OFM 模型 id 归车道：kilo 池的 id 形如 kilo-auto/free、
// nvidia/...:free；EAC 是 catalog.sealIds 那批（z-ai/、openai/、deepseek-ai/、moonshotai/）。
func classifyOFMLane(id string) Lane {
	lower := strings.ToLower(id)
	switch {
	case strings.HasPrefix(lower, "kilo-auto/"), strings.HasSuffix(lower, ":free"):
		return LaneKilo
	case strings.HasPrefix(lower, "opencode/"):
		return LaneFree
	}
	return LaneFree
}

// laneLabel 返回车道的中文显示名。
func laneLabel(l Lane) string {
	switch l {
	case LaneFree:
		return "OFM 免费"
	case LaneKilo:
		return "Kilo 免费池"
	case LaneEAC:
		return "EAC 协付"
	case LaneAggregate:
		return "聚合渠道"
	default:
		return "自定义"
	}
}

// SyncFromGateway 用网关清单增量更新池子，返回统计。
//
// 规则：
//   - 新模型按 SpecFromGatewayModel 的默认值加入；
//   - 已存在模型只更新 BaseURL/APIKey（网关地址可能变）与上游来源备注，
//     保留用户设置的 Enabled/Priority/Weight/MaxFail/CooldownSec/Tier/Group；
//   - 网关里消失的模型不删除（可能只是网关临时没广播），只记 skipped。
func SyncFromGateway(m *Manager, gw Gateway, actor string) SyncResult {
	res := SyncResult{At: nowMs()}
	if len(gw.Models) == 0 {
		return res
	}
	// 统一忽略 case 重复的 id。
	seen := map[string]bool{}
	for _, id := range gw.Models {
		if seen[id] {
			continue
		}
		seen[id] = true
		spec, ok := SpecFromGatewayModel(gw, id)
		if !ok {
			res.Skipped++
			continue
		}
		existing, _, found := m.Get(spec.ID)
		if !found {
			if _, err := m.Upsert(spec, actor); err != nil {
				res.Skipped++
				continue
			}
			res.Added++
			res.AddedID = append(res.AddedID, spec.ID)
			continue
		}
		// 只更新网关相关的易变字段。
		existing.BaseURL = gw.BaseURL
		if gw.Key != "" {
			existing.APIKey = gw.Key
		}
		if existing.Note == "" {
			existing.Note = spec.Note
		}
		// 自动恢复：网关此刻仍在广播该模型，说明上游判定已过期（availability.json
		// 是插件按探测周期写的旧快照）。只恢复「当初是我们自动停用的」那些，
		// 用户手动停用的绝不擅自打开。
		if !existing.Enabled && existing.AutoDisabled {
			existing.Enabled = true
			existing.AutoDisabled = false
			existing.Note = "网关重新广播，已自动恢复启用"
			res.Revived = append(res.Revived, existing.ID)
		}
		// 自动恢复：网关此刻仍在广播该模型，说明上游判定已过期（availability.json
		// 是插件按探测周期写的旧快照）。只恢复「当初是我们自动停用的」那些，
		// 用户手动停用的绝不擅自打开。
		//
		// 顺序很重要：UpsertPreserve 会以「磁盘上的当前值」为准保留 Enabled，
		// 所以必须在写入之后再调 SetEnabled(true)，否则恢复动作会被自己覆盖掉。
		revive := !existing.Enabled && existing.AutoDisabled
		if revive {
			existing.AutoDisabled = false
			existing.Note = "网关重新广播，已自动恢复启用"
		}
		if _, err := m.UpsertPreserve(existing, actor); err != nil {
			res.Skipped++
			continue
		}
		if revive {
			if err := m.SetEnabled(existing.ID, true, actor); err == nil {
				res.Revived = append(res.Revived, existing.ID)
			}
		}
		res.Updated++
	}
	sort.Strings(res.AddedID)
	m.RecordSync(res, gw)
	return res
}

// UpsertPreserve 是给同步用的更新入口：不重置用户显式设置的字段。
func (m *Manager) UpsertPreserve(s Spec, actor string) (Spec, error) {
	cur, _, ok := m.Get(s.ID)
	if !ok {
		return m.Upsert(s, actor)
	}
	// 保留用户语义字段。
	// AutoDisabled 例外：它由同步逻辑显式控制（恢复时置 false），不能被旧值覆盖。
	// Enabled 的保留策略（2026-10-19 修正）：默认保留磁盘现值（用户手动开关优先），
	// 但当调用方显式要求「打开」而现值是关闭时，必须是同步层的自动恢复动作
	// —— 否则恢复会被这一行自己抹掉（现场：revive 后 Enabled 仍是 false）。
	if !(s.Enabled && !cur.Enabled) {
		s.Enabled = cur.Enabled
	}
	s.Priority = cur.Priority
	s.Weight = cur.Weight
	s.MaxFail = cur.MaxFail
	s.CooldownSec = cur.CooldownSec
	if s.Tier == "" {
		s.Tier = cur.Tier
	}
	if s.Group == "" {
		s.Group = cur.Group
	}
	if s.Source == "" || s.Source == "gateway-sync" {
		s.Source = cur.Source
	}
	if s.Label == "" {
		s.Label = cur.Label
	}
	return m.Upsert(s, actor)
}

// RecordSync 记录一次同步结果（面板展示「上次同步」）。
func (m *Manager) RecordSync(res SyncResult, gw Gateway) {
	m.mu.Lock()
	m.lastSync = res
	m.gatewayBase = firstNonEmpty(gw.BaseURL, m.gatewayBase)
	hasKey := gw.Key != ""
	m.gatewayKeySet = hasKey
	m.mu.Unlock()
	m.log(logRecord{Kind: "sync", State: "gateway", Reason: fmt.Sprintf("网关 %s 广播 %d 个模型：新增 %d / 更新 %d / 跳过 %d", gw.BaseURL, len(gw.Models), res.Added, res.Updated, res.Skipped)})
}

// LastSync 返回最近一次同步统计。
func (m *Manager) LastSync() SyncResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastSync
}

// GatewayStatus 返回网关连接状态（面板顶部胶水信息）。
func (m *Manager) GatewayStatus() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return map[string]any{
		"base": m.gatewayBase, "hasKey": m.gatewayKeySet, "lastSync": m.lastSync,
	}
}
