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
	Reachable bool   `json:"reachable"` // 端口有监听
	Authorized bool  `json:"authorized"` // 带密钥能列模型
	StatusCode int   `json:"statusCode"`
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
		out.Detail = "端口未监听: " + trim(derr.Error(), 160)
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
	cands := GatewayCandidates(dshHome)
	var last gatewayHealth
	for _, c := range cands {
		if time.Now().After(time.Now().Add(budget)) {
			break
		}
		health := checkGateway(c.base, c.key, 3*time.Second)
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
