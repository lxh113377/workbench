package modelpool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// ── 实时可用性探测 ───────────────────────────────────────────────
//
// 探测分两级（对标 k8s 的 liveness / readiness）：
//  1. 端点级连通性（GET /v1/models）：判断地址+凭据是否可用，代价极小；
//  2. 模型级可用性（POST 最小补全）：只有端点通、且模型声明要求时才做，
//     避免把额度浪费在探测上。默认关闭模型级探测，面板可一键触发。
//
// 结果写回 Health 并追加 model-status.jsonl；Prober 本身不决定调度，
// 由 Manager 依据 Health 决定是否禁用/恢复，保证「探测」与「策略」解耦。

// ProbeResult 是一次探测的结果。
type ProbeResult struct {
	Model     string `json:"model"`
	OK        bool   `json:"ok"`
	State     State  `json:"state"`
	LatencyMs int64  `json:"latencyMs"`
	Error     string `json:"error,omitempty"`
	Kind      string `json:"kind,omitempty"`
	At        int64  `json:"at"`
}

// Prober 并发探测模型池。
type Prober struct {
	client *http.Client
	// timeout 是单次探测的总超时。
	timeout time.Duration
	// modelProbe 为 true 时做模型级最小补全探测。
	modelProbe bool
	// KeyResolver 在本机网关鉴权失败时重新解析可用密钥。
	//
	// 现场事实（2026-10-19 07:43）：8326 的属主随 DSH 任务进程生灭而轮换，
	// 每个任务的隔离 HOME 各有一把 api-key；缓存下来的那把随时可能失效。
	// 因此鉴权失败不能直接判模型下线，必须先让上层重新解析密钥再试一次。
	KeyResolver func(base string) string
}

// NewProber 构造探测器。timeout <= 0 时用 12 秒。
func NewProber(timeout time.Duration) *Prober {
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	return &Prober{
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        16,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     60 * time.Second,
				// 本机网关与第三方端点都可能走代理，这里只在环境变量显式配置时使用默认代理。
			},
		},
		timeout: timeout,
	}
}

// SetModelProbe 设置是否做模型级探测。
func (p *Prober) SetModelProbe(on bool) { p.modelProbe = on }

// ProbeAll 并发探测全部启用的模型，返回逐个结果。
func (p *Prober) ProbeAll(specs []Spec) []ProbeResult {
	out := make([]ProbeResult, len(specs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6) // 最多 6 路并发，避免把本机网关打满
	for i := range specs {
		i := i
		s := specs[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = p.Probe(s)
		}()
	}
	wg.Wait()
	return out
}

// Probe 探测单个模型。
func (p *Prober) Probe(s Spec) ProbeResult {
	started := time.Now()
	res := ProbeResult{Model: s.ID, At: nowMs()}
	defer func() { res.LatencyMs = time.Since(started).Milliseconds() }()

	base, err := p.baseURL(s)
	if err != nil {
		res.State, res.Kind, res.Error = StateDown, "bad_endpoint", err.Error()
		return res
	}
	// 本机网关的 Key：优先用调用方显式传入的（DiscoverGatewayVerified 已验证过），
	// 没有再回落到 ~/.dsh 下的密钥文件。
	// 现场缺陷（2026-10-19 07:42）：无条件用 ~/.dsh 那份覆盖，而真正服务 8326 的
	// 是某个任务隔离 HOME 的 key ⇒ 每轮探测都 401，28 个模型全被判 unknown。
	key := s.APIKey
	if isLocalGateway(base) {
		if key == "" {
			if k := readGatewayKey(); k != "" {
				key = k
			}
		}
		// 本机网关没起来 / 拒绝鉴权都属于「环境未就绪」，不是模型判死。
		// 关键：这类模型仍然能被 DSH 的 our-free-model provider 直接调度（不经过 8326），
		// 所以保持 unknown 留在路由表里；如果一律判 down，调度器会整体停摆（现场实测）。
		if !gatewayReachable(p.client, base) {
			res.State, res.Kind = StateUnknown, "gateway_off"
			res.Error = "本机 OFM 网关未启动（模型本身未判死）"
			return res
		}
		if _, code, err := p.headJSON(base, key); err == nil && (code == http.StatusUnauthorized || code == http.StatusForbidden) {
			// 密钥轮换：让上层重新解析一把可用的，再试一次。
			if p.KeyResolver != nil {
				if k := p.KeyResolver(base); k != "" && k != key {
					key = k
					code2, body2, err2 := p.doJSON(http.MethodGet, strings.TrimRight(base, "/")+"/models", key, nil)
					if err2 == nil && code2 == http.StatusOK {
						// ⚠️ 必须走 modelIDVariants（2026-10-29 全池黑障事故）：网关按渠道给 id 加前缀，
						// 本处原先只比对裸 `s.Model`（如 z-ai/glm-5.3-flash），而清单里是 `our-free-model/z-ai/glm-5.3-flash` ⇒
						// 一律判 not_listed。本机实测：38/38 车道全灭、整池 30 分钟黑障循环，任务一次都没发出去。
						// 主路径（下方 probeEndpoint 的清单检查）早就用了 variants，这条轮换重试路径漏了 —— 同一判据两处实现。
						if listed, known := modelListed(body2, modelIDVariants(s)...); known && !listed {
							res.State, res.Kind = StateDegraded, "not_listed"
							res.Error = "网关清单里没有该模型（渠道被关闭或模型下线）"
							return res
						}
						res.OK, res.State = true, StateHealthy
						return res
					}
				}
			}
			res.State, res.Kind = StateUnknown, "gateway_unauthorized"
			res.Error = "本机网关拒绝鉴权（api-key 与当前网关不匹配）"
			return res
		}
	}
	code, body, err := p.doJSON(http.MethodGet, strings.TrimRight(base, "/")+"/models", key, nil)
	if err != nil {
		res.State, res.Kind = StateDown, classifyProbeErr(err)
		res.Error = trim(err.Error(), 300)
		return res
	}
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		res.State, res.Kind = StateDown, "missing_credential"
		res.Error = fmt.Sprintf("鉴权失败 HTTP %d: %s", code, trim(string(body), 200))
		return res
	case code == http.StatusNotFound:
		// 有些兼容端点不实现 /models；退化为模型级探测或视作端点存活。
		if p.modelProbe {
			return p.probeModel(s, base, key, started)
		}
		res.OK, res.State = true, StateHealthy
		return res
	case code >= 500:
		res.State, res.Kind = StateDown, "upstream_failed"
		res.Error = fmt.Sprintf("上游故障 HTTP %d: %s", code, trim(string(body), 200))
		return res
	case code >= 400:
		res.State, res.Kind = StateDegraded, "client_error"
		res.Error = fmt.Sprintf("HTTP %d: %s", code, trim(string(body), 200))
		return res
	}
	if p.modelProbe {
		return p.probeModel(s, base, key, started)
	}
	// 端点 200 只证明「服务活着」；再看清单里到底有没有这个模型（网关会按渠道启停广播）。
	// 网关会按车道给 id 加前缀（our-free-model/<id>），所以三种形态都要认。
	if listed, known := modelListed(body, modelIDVariants(s)...); known && !listed {
		res.State, res.Kind = StateDegraded, "not_listed"
		res.Error = "网关清单里没有该模型（渠道被关闭或模型下线）"
		return res
	}
	res.OK, res.State = true, StateHealthy
	return res
}

// modelIDVariants 返回一个模型在网关清单里可能出现的全部 id 形态。
//
// 实测（2026-10-19 07:45）：网关把免费/EAC 车道广播成 our-free-model/<id>、
// 地区受限成 our-free-model-region/<id>、网关自有车道保持 deepseek-official/<m> 原样。
// 只比对裸 id 会 100% 误判成「清单里没有」。
func modelIDVariants(s Spec) []string {
	base := firstNonEmpty(s.Model, s.ID)
	out := []string{base}
	for _, p := range []string{"our-free-model/", "our-free-model-region/"} {
		if !strings.HasPrefix(base, p) {
			out = append(out, p+base)
		}
	}
	out = append(out, s.ID)
	return out
}

// modelIDVariants 返回一个模型在网关清单里可能出现的全部 id 形态。
//
// 实测（2026-10-19 07:45）：网关把免费/EAC 车道广播成 our-free-model/<id>、
// 地区受限成 our-free-model-region/<id>、网关自有车道保持 deepseek-official/<m> 原样。
// 只比对裸 id 会 100% 误判成「清单里没有」。

// modelListed 判断 /v1/models 的响应体里是否列出了指定模型。
// known=false 表示响应体不是 OpenAI 清单形状（无法判断，交给上层其它信号）。
func modelListed(body []byte, ids ...string) (listed bool, known bool) {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := jsonUnmarshalLoose(body, &payload); err != nil || payload.Data == nil {
		return false, false
	}
	set := map[string]bool{}
	for _, d := range payload.Data {
		set[d.ID] = true
	}
	for _, id := range ids {
		if id != "" && set[id] {
			return true, true
		}
	}
	return false, true
}

// probeModel 做一次最小补全探测（max_tokens=1）。仅在 SetModelProbe(true) 时启用。
func (p *Prober) probeModel(s Spec, base, key string, started time.Time) ProbeResult {
	res := ProbeResult{Model: s.ID, At: nowMs()}
	payload := map[string]any{
		"model":      firstNonEmpty(s.Model, s.ID),
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
	}
	code, body, err := p.doJSON(http.MethodPost, strings.TrimRight(base, "/")+"/chat/completions", key, payload)
	res.LatencyMs = time.Since(started).Milliseconds()
	if err != nil {
		res.State, res.Kind = StateDown, classifyProbeErr(err)
		res.Error = trim(err.Error(), 300)
		return res
	}
	if code < 400 {
		res.OK, res.State = true, StateHealthy
		return res
	}
	res.State = StateDegraded
	res.Kind = "model_rejected"
	res.Error = fmt.Sprintf("HTTP %d: %s", code, trim(string(body), 240))
	return res
}

func (p *Prober) baseURL(s Spec) (string, error) {
	raw := strings.TrimSpace(s.BaseURL)
	if raw == "" {
		return "", fmt.Errorf("未配置 BaseURL")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("BaseURL 非法: %s", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("BaseURL 只支持 http/https: %s", raw)
	}
	return raw, nil
}

func (p *Prober) doJSON(method, endpoint, key string, payload any) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return 0, nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "workbench-modelpool/1.0")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("X-API-Key", key)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	// 限制响应大小，防止异常端点把内存打满。
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	return resp.StatusCode, raw, nil
}

// jsonUnmarshalLoose 宽松解析（形状不符时返回错误，由调用方决定是否降级）。
func jsonUnmarshalLoose(b []byte, v any) error {
	if len(b) == 0 {
		return fmt.Errorf("空响应体")
	}
	return json.Unmarshal(b, v)
}

// jsonUnmarshalLoose 宽松解析（形状不符时返回错误，由调用方决定是否降级）。

// headJSON 只取状态码（不解析响应体），用于区分「端口通」「鉴权失败」两种环境态。
// 注意：key 必须由调用方传入（调用方持有本轮解析出的可用密钥）；
// 早期版本这里自作主张去读 ~/.dsh 的 api-key，而那把往往已过期，
// 导致每个模型每轮都被误判成「网关拒绝鉴权」（2026-10-19 07:44 现场）。
func (p *Prober) headJSON(base, key string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		return nil, 0, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("X-API-Key", key)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	return raw, resp.StatusCode, nil
}

// headJSON 只取状态码（不解析响应体），用于区分「端口通」「鉴权失败」两种环境态。
// 注意：key 必须由调用方传入（调用方持有本轮解析出的可用密钥）；
// 早期版本这里自作主张去读 ~/.dsh 的 api-key，而那把往往已过期，
// 导致每个模型每轮都被误判成「网关拒绝鉴权」（2026-10-19 07:44 现场）。

// ProbeSummary 汇总一轮探测结果（面板与日志用）。
type ProbeSummary struct {
	At      int64         `json:"at"`
	Total   int           `json:"total"`
	OK      int           `json:"ok"`
	Down    int           `json:"down"`
	Unknown int           `json:"unknown"`
	Results []ProbeResult `json:"results"`
}

// Summarize 把结果收敛成摘要（按失败数降序，便于面板先看红的）。
func Summarize(results []ProbeResult) ProbeSummary {
	sum := ProbeSummary{At: nowMs(), Total: len(results), Results: results}
	for _, r := range results {
		switch r.State {
		case StateDown:
			sum.Down++
		case StateUnknown:
			sum.Unknown++
		default:
			if r.OK {
				sum.OK++
			} else {
				sum.Down++
			}
		}
	}
	sort.SliceStable(sum.Results, func(i, j int) bool {
		if sum.Results[i].OK != sum.Results[j].OK {
			return !sum.Results[i].OK
		}
		return sum.Results[i].Model < sum.Results[j].Model
	})
	return sum
}

func classifyProbeErr(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return "timeout"
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "no such host"),
		strings.Contains(msg, "actively refused"), strings.Contains(msg, "connectex"):
		return "unreachable"
	default:
		return "network_error"
	}
}

func isLocalGateway(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// gatewayReachable 判断本机网关端口是否在监听（不消耗模型额度）。
func gatewayReachable(c *http.Client, base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	host := u.Host
	addr := host
	if !strings.Contains(addr, ":") {
		addr = host + ":80"
	}
	conn, err := (&netDialer{}).Dial(addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
