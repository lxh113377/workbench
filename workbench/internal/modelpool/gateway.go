package modelpool

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ── OFM 渠道网关发现 ────────────────────────────────────────────
//
// 实测（2026-10-19）：our-free-model 的「白嫖渠道」把十三个账号渠道聚合成一个
// OpenAI 兼容网关（默认 127.0.0.1:8326），GET /v1/models 会广播全部车道：
//
//	our-free-model/<ofm-id>        → 免费/EAC 车道（DSH 的 our-free-model provider 认得）
//	our-free-model-region/<ofm-id> → 上游按地区拦截（保留可见但不该调度）
//	deepseek-official/*            → 渠道网关自有车道（只能用网关调用，DSH overlay 不认）
//
// 因此网关是「有哪些模型可用」最权威的实时来源：它已经替我们做完了清单合并。
// 本文件负责找到网关（地址 + 每个 DSH_HOME 各自的 api-key 文件）并拉取清单。

// Gateway 描述的是一次成功的网关发现。
type Gateway struct {
	BaseURL string   `json:"baseUrl"`
	Key     string   `json:"key"`     // 只在本进程内存中使用，API 只回显指纹
	KeyFrom string   `json:"keyFrom"` // 密钥来源（便于排查「为什么 401」）
	Models  []string `json:"models"`
	At      int64    `json:"at"`
	Error   string   `json:"error,omitempty"`
}

// gatewayCandidate 是一个候选的（地址, 密钥, 来源）。
type gatewayCandidate struct {
	base string
	key  string
	from string
}

// GatewayCandidates 枚举候选网关地址与密钥。
//
// 地址来源：OFM settings 的 forward（本机转发口，默认 18899）、渠道网关（默认 8326）、
// 渠道网关 relay（默认 18326）。密钥来源按可信度排序：
//  1. 显式环境变量 DSH_OPENAI_GATEWAY_API_KEY；
//  2. <DSH_HOME>/openai-gateway/api-key（本工作台自己那份）；
//  3. 各任务隔离 HOME 下的 api-key（executor 为每任务建独立 HOME，网关按 HOME 各发一张）；
//  4. ~/.dsh/openai-gateway/api-key（用户桌面端那份）。
//
// 第 3 条是实测关键：网关只有一个进程能绑 8326，但每个 DSH_HOME 都有自己的 key 文件，
// 只有「正在服务那个网关」的 HOME 的 key 才能通过校验 —— 所以必须逐个试并验证。
func GatewayCandidates(dshHome string) []gatewayCandidate {
	bases := []string{}
	add := func(b string) {
		b = strings.TrimSpace(b)
		if b == "" {
			return
		}
		for _, ex := range bases {
			if ex == b {
				return
			}
		}
		bases = append(bases, b)
	}

	var st settingsFile
	if dir := OFMDataDir(dshHome); dir != "" {
		_ = readJSON(filepath.Join(dir, "settings.json"), &st)
	}
	if st.Forward != nil && st.Forward.Enabled {
		host := firstNonEmpty(st.Forward.Host, "127.0.0.1")
		port := st.Forward.Port
		if port <= 0 {
			port = 18899
		}
		add("http://" + host + ":" + itoa(port) + "/v1")
	}
	if st.ChanGateway != nil && st.ChanGateway.Relay.Enabled && st.ChanGateway.Relay.Port > 0 {
		host := firstNonEmpty(st.ChanGateway.Relay.Host, "127.0.0.1")
		add("http://" + host + ":" + itoa(st.ChanGateway.Relay.Port) + "/v1")
	}
	// 渠道网关地址不受 settings 控制（固定在 8326 或环境变量覆盖）。
	port := "8326"
	if v := strings.TrimSpace(os.Getenv("DSH_OPENAI_GATEWAY_PORT")); v != "" {
		port = v
	}
	add("http://127.0.0.1:" + port + "/v1")

	keys := []gatewayCandidate{}
	if v := strings.TrimSpace(os.Getenv("DSH_OPENAI_GATEWAY_API_KEY")); v != "" {
		keys = append(keys, gatewayCandidate{key: v, from: "env DSH_OPENAI_GATEWAY_API_KEY"})
	}
	if dshHome != "" {
		p := filepath.Join(dshHome, "openai-gateway", "api-key")
		if k := readKeyFile(p); k != "" {
			keys = append(keys, gatewayCandidate{key: k, from: p})
		}
		// 任务隔离 HOME：按 mtime 倒序，最近用过的优先。
		keys = append(keys, taskHomeKeys(filepath.Join(dshHome, "tasks"))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".dsh", "openai-gateway", "api-key")
		if k := readKeyFile(p); k != "" {
			keys = append(keys, gatewayCandidate{key: k, from: p})
		}
	}
	out := make([]gatewayCandidate, 0, len(bases)*len(keys))
	for _, b := range bases {
		for _, k := range keys {
			out = append(out, gatewayCandidate{base: b, key: k.key, from: k.from})
		}
		// 无密钥也试一次：有些本机转发口开了匿名（或只校验 OPTIONS）。
		out = append(out, gatewayCandidate{base: b, from: "no-key"})
	}
	return out
}

// taskHomeKeys 扫描任务隔离 HOME 下的网关密钥（按 mtime 倒序）。
func taskHomeKeys(tasksDir string) []gatewayCandidate {
	ents, err := os.ReadDir(tasksDir)
	if err != nil {
		return nil
	}
	type item struct {
		path string
		mod  time.Time
	}
	var items []item
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(tasksDir, e.Name(), "openai-gateway", "api-key")
		if st, err := os.Stat(p); err == nil {
			items = append(items, item{path: p, mod: st.ModTime()})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.After(items[j].mod) })
	out := make([]gatewayCandidate, 0, len(items))
	for _, it := range items {
		if k := readKeyFile(it.path); k != "" {
			out = append(out, gatewayCandidate{key: k, from: it.path})
		}
	}
	return out
}

func readKeyFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// DiscoverGateway 逐个验证候选，返回第一个能列出模型的网关。
// 网络失败不 panic、不阻塞启动：超过整体预算就返回带 Error 的空结果。
func DiscoverGateway(dshHome string, budget time.Duration) Gateway {
	if budget <= 0 {
		budget = 8 * time.Second
	}
	cands := GatewayCandidates(dshHome)
	if len(cands) == 0 {
		return Gateway{Error: "没有可用的网关候选（未找到 OFM 数据目录与 api-key）", At: nowMs()}
	}
	client := &http.Client{Timeout: 4 * time.Second}
	deadline := time.Now().Add(budget)
	var lastErr string
	for _, c := range cands {
		if time.Now().After(deadline) {
			break
		}
		models, err := listGatewayModels(client, c.base, c.key)
		if err != nil {
			lastErr = c.base + ": " + err.Error()
			continue
		}
		return Gateway{BaseURL: c.base, Key: c.key, KeyFrom: c.from, Models: models, At: nowMs()}
	}
	return Gateway{Error: firstNonEmpty(lastErr, "所有网关候选均未通过验证"), At: nowMs()}
}

// listGatewayModels 调 GET /v1/models 并解析 id 列表。
func listGatewayModels(c *http.Client, base, key string) ([]string, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("X-API-Key", key)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, &gatewayError{code: resp.StatusCode, body: string(body)}
	}
	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(payload.Data))
	for _, d := range payload.Data {
		if id := strings.TrimSpace(d.ID); id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}

type gatewayError struct {
	code int
	body string
}

func (e *gatewayError) Error() string {
	return "HTTP " + itoa(e.code) + ": " + trim(e.body, 160)
}
