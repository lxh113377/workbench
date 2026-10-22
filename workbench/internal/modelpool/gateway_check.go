package modelpool

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ── 真实网关探测（修正「TCP 通 ≠ HTTP 通」的误判） ──────────────
//
// 现场缺陷（2026-10-19 07:39 实测）：8326 端口有监听但 HTTP 返回 401，
// 旧逻辑用「TCP 能连上」判网关可用 ⇒ 立刻把 4 个模型全判 down 并进冷却。
// 正确判据必须是「HTTP 层能列出模型」，TCP 只用于区分「端口没开」与「端口开了但拒绝」。

// gatewayHealth 是一次网关健康检查的结果。
type gatewayHealth struct {
	Reachable  bool   `json:"reachable"`  // 端口有监听
	Authorized bool   `json:"authorized"` // 带密钥能列模型
	StatusCode int    `json:"statusCode"`
	Detail     string `json:"detail"`
}

// checkGateway 探测本机网关：先 TCP，再带密钥打 /v1/models。
func checkGateway(base, key string, timeout time.Duration) gatewayHealth {
	out := gatewayHealth{}
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || u.Host == "" {
		out.Detail = "BaseURL 非法: " + base
		return out
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	conn, derr := net.DialTimeout("tcp", host, timeout)
	if derr != nil {
		// 本机网关「没在监听」是最高频的环境态（DSH 桌面端起停、任务隔离 HOME 轮换），
		// 且已被 SyncNow 的 catalog 回退兜住 —— 不是缺陷。
		// 把 Go 的裸 dial 错误原样抛给用户，只会得到
		// 「网关错误：端口未监听: dial tcp 127.0.0.1:8326: connectex: ...」这种
		// 看不懂也没法行动的读数（2026-10-20 老大实报）。这里换成可行动的中文原因。
		if isLocalGateway(base) {
			out.Detail = "本机 OFM 网关（" + host + "）未启动；已退回本地清单纳管，模型照常可调度"
		} else {
			out.Detail = "端口未监听: " + trim(derr.Error(), 160)
		}
		return out
	}
	_ = conn.Close()
	out.Reachable = true
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		out.Detail = err.Error()
		return out
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("X-API-Key", key)
	}
	resp, err := client.Do(req)
	if err != nil {
		out.Detail = "HTTP 探测失败: " + trim(err.Error(), 160)
		return out
	}
	defer resp.Body.Close()
	out.StatusCode = resp.StatusCode
	out.Authorized = resp.StatusCode == http.StatusOK
	if !out.Authorized {
		out.Detail = fmt.Sprintf("网关拒绝（HTTP %d），密钥可能来自别的 DSH_HOME", resp.StatusCode)
	}
	return out
}

// MergeCandidates 把 Discover 出来的候选模型并入池子：
// 已存在的保留用户设置（优先级/权重/启停），新加入的按候选默认值落库。
// 与 SyncFromGateway 的区别是数据源不同（catalog vs 网关），语义一致。
func MergeCandidates(m *Manager, candidates []Spec, actor string) SyncResult {
	res := SyncResult{At: nowMs()}
	for _, spec := range candidates {
		if existing, _, found := m.Get(spec.ID); found {
			// catalog 是 OFM 车道的权威来源：它说可调度就必须可调度。
			// 早期版本遗留的 routable=false 记录会永远卡住（现场 31/35 全被卡）。
			if spec.Routable && !existing.Routable {
				existing.Routable = true
				if _, err := m.UpsertPreserve(existing, actor); err == nil {
					res.Updated++
				}
				continue
			}
			res.Updated++
			continue
		}
		if _, err := m.Upsert(spec, actor); err != nil {
			res.Skipped++
			continue
		}
		res.Added++
		res.AddedID = append(res.AddedID, spec.ID)
	}
	m.RecordSync(res, Gateway{})
	return res
}

// DiscoverGatewayVerified 是 DiscoverGateway 的严格版：只有 HTTP 层能列出模型才算成功。
// 返回的 Gateway.Error 会区分「端口没开 / 拒绝鉴权 / 网络错误」，便于面板给出可操作提示。
func DiscoverGatewayVerified(dshHome string, budget time.Duration) (Gateway, gatewayHealth) {
	if budget <= 0 {
		budget = 8 * time.Second
	}
	return discoverVerifiedWith(GatewayCandidates(dshHome), budget, func(base, key string) gatewayHealth {
		return checkGateway(base, key, 3*time.Second)
	})
}

// discoverVerifiedWith 是 DiscoverGatewayVerified 的可注入内核（check 可替换，便于计数验证）。
//
// 两个现场缺陷在这里被钉死（2026-10-20）：
//  1. 预算比较写成了 time.Now().After(time.Now().Add(budget)) —— 右边恒 > 左边，
//     循环条件恒假，budget 形同虚设（整个函数只剩「候选全试一遍」一条路）。
//  2. 候选是 base × key 的笛卡尔积，而本机网关只有一个 base；任务隔离 HOME 会积累
//     200+ 份 api-key。旧逻辑在「端口没开」时仍逐个 key 重复拨同一个端口，
//     把一次环境态放大成成片的 gateway-fail 噪音。同一个 base 一旦证明端口没监听，
//     后面的候选只换 key 不换端口 —— 直接跳过。
func discoverVerifiedWith(cands []gatewayCandidate, budget time.Duration, check func(base, key string) gatewayHealth) (Gateway, gatewayHealth) {
	if budget <= 0 {
		budget = 8 * time.Second
	}
	deadline := time.Now().Add(budget)
	var last gatewayHealth
	deadBases := map[string]bool{}
	for _, c := range cands {
		if time.Now().After(deadline) {
			break
		}
		if deadBases[c.base] {
			continue
		}
		health := check(c.base, c.key)
		if !health.Reachable {
			// 端口未监听：记一次读数，后续同 base 的候选不再拨号。
			deadBases[c.base] = true
			last = health
			continue
		}
		if !health.Authorized {
			last = health
			continue
		}
		models, err := listGatewayModels(&http.Client{Timeout: 4 * time.Second}, c.base, c.key)
		if err != nil {
			last = gatewayHealth{Reachable: true, Detail: trim(err.Error(), 200)}
			continue
		}
		return Gateway{BaseURL: c.base, Key: c.key, KeyFrom: c.from, Models: models, At: nowMs()}, health
	}
	gw := Gateway{At: nowMs()}
	switch {
	case last.Reachable && last.StatusCode > 0:
		gw.Error = "网关端口在监听但拒绝请求（" + last.Detail + "）"
	case last.Reachable:
		gw.Error = "网关端口在监听但不可用（" + last.Detail + "）"
	default:
		gw.Error = firstNonEmpty(last.Detail, "没有可用的网关候选")
	}
	return gw, last
}
