package engine

import (
	"sort"
	"strings"

	"workbench/internal/config"
)

// orphanConfiguredModels 找出「手写在 config.Models 里的车道 id」在模型池投影清单里不存在的那些（round 124 · T79）。
//
// 为什么必须有这条守卫（我自己造的事故，2026-10-29 round 72）：我把 config.Models 的 id 从裸 id 改成
// 带渠道前缀的形态（例如 our-free-model/kilo-auto/free），而池内 spec 与池健康账本的键都是裸 id
// （kilo-auto/free）⇒ 那 6 条路线在池里没有任何对应条目：
//
//	· 取不到 tier/weight/maxFail/cooldownSec，健康度也看不到（实测这些车道的池状态是 (none)）；
//	· 于是**失败记不到任何车道**，冷却与退避全部失效 ⇒ 被反复选中、每次几秒就空转一轮。
//
// 实测代价：01:28 那一小时 629 次尝试 0 成功（not_served 166 / transport_failed 436）；
// 把 id 改回池内真存在的裸 id 后，同时段降到 8 次尝试/8 分钟，且全部落在有 spec 的车道上。
//
// 口径（三条，都是被现场逼出来的）：
//  1. 只做「id 是否存在」的精确比对，**不猜前缀、不做模糊匹配** —— 猜前缀正是上一版的病根：
//     网关清单要不要前缀、池账本要不要前缀，是两套不同的键，不能混推（网关要、池账本不要）。
//  2. poolIDs 为空 ⇒ 返回 nil：模型池尚未同步（首次启动/池被禁用）时**无信息不判**，
//     否则会把「全部配置路线」误报成孤儿，制造新的假警。
//  3. 结果去重并排序：同一 id 重复配置只报一次，且日志可逐字复算（不随 map 迭代顺序漂移）。
func orphanConfiguredModels(configured []config.ModelRoute, poolIDs []string) []string {
	if len(configured) == 0 || len(poolIDs) == 0 {
		return nil
	}
	have := make(map[string]bool, len(poolIDs))
	for _, id := range poolIDs {
		if s := strings.TrimSpace(id); s != "" {
			have[s] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, r := range configured {
		m := strings.TrimSpace(r.Model)
		if m == "" || have[m] || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// checkConfiguredLaneIDs 核对「手写 config.Models ⊆ 模型池投影清单」，孤儿只告警、不拒启动。
//
// 为什么在启动与热更新两处都调：这两处正是 router 路由表被换成 config.Models 的时刻
// （池为空时 ReloadConfig 会回落到配置清单），事故就发生在这条回落路径上。
//
// 处置选择：只告警。7×24 无人值守（#29/#31）下，多一条没 spec 的路线是「可观测的坏路线」，
// 不该把整台引擎关在门外；但必须留下可复算的 WARN —— 否则就是本轮那种
// 「一小时内 629 次空转、面板上还一切正常」的静默失血。
func (e *Engine) checkConfiguredLaneIDs(cfg config.Config) {
	pool := e.poolRoutes()
	ids := make([]string, 0, len(pool))
	for _, r := range pool {
		ids = append(ids, r.Model)
	}
	bad := orphanConfiguredModels(cfg.Models, ids)
	if len(bad) == 0 {
		return
	}
	e.log.Warn("配置车道 id 在模型池里不存在（失败将记不到车道、冷却失效）",
		"orphans", strings.Join(bad, ","), "count", len(bad))
}
