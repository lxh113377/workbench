package modelpool

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ── OFM 数据面发现 ───────────────────────────────────────────────
//
// our-free-model 把「有哪些模型」写进 catalog.json，把「哪些不可用」写进
// availability.json，把「用户隐藏了谁 / 开了哪些聚合渠道」写进 settings.json。
// 本文件只读这些文件，不改它们（插件是唯一写者），把结果转成池子的候选模型。
//
// 路径来源：默认 <用户目录>/.dsh/our-free-model；工作隔离 DSH_HOME 下的副本
// （<DSHHome>/our-free-model）优先，这样池子看到的是工作台真正在用的那份账号态。

// OFMDataDir 返回 OFM 数据目录（工作隔离副本优先）。
func OFMDataDir(dshHome string) string {
	if strings.TrimSpace(dshHome) != "" {
		p := filepath.Join(dshHome, "our-free-model")
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".dsh", "our-free-model")
}

// catalogFile 对应 OFM catalog.json 的字段子集。
type catalogFile struct {
	Version int      `json:"version"`
	At      int64    `json:"at"`
	Entries []string `json:"entries"`
	SealIDs []string `json:"sealIds"`
	Kilo    []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Channel       string `json:"channel"`
		Vision        bool   `json:"vision"`
		Reasoning     bool   `json:"reasoning"`
		ContextWindow int    `json:"contextWindow"`
		MaxOutput     int    `json:"maxOutput"`
	} `json:"kiloRows"`
}

// availabilityFile 对应 OFM availability.json 的字段子集。
type availabilityFile struct {
	Version int   `json:"version"`
	At      int64 `json:"at"`
	Results map[string]struct {
		State     string `json:"state"`
		Detail    string `json:"detail"`
		LatencyMs int64  `json:"latencyMs"`
		At        int64  `json:"at"`
	} `json:"results"`
}

// settingsFile 对应 OFM settings.json 的字段子集。
type settingsFile struct {
	HiddenModelIDs []string `json:"hiddenModelIds"`
	HideFreeModels bool     `json:"hideFreeModels"`
	HideEACModels  bool     `json:"hideEacModels"`
	Forward        *struct {
		Enabled bool   `json:"enabled"`
		Host    string `json:"host"`
		Port    int    `json:"port"`
	} `json:"forward"`
	ChanGateway *struct {
		Relay struct {
			Enabled bool   `json:"enabled"`
			Host    string `json:"host"`
			Port    int    `json:"port"`
			Key     string `json:"key"`
		} `json:"relay"`
	} `json:"chanGateway"`
}

// Discovery 是一次数据面发现的完整结果。
type Discovery struct {
	Dir          string            `json:"dir"`
	CatalogAt    int64             `json:"catalogAt"`
	Availability map[string]string `json:"availability"` // 模型 -> state
	Candidates   []Spec            `json:"candidates"`
	Warnings     []string          `json:"warnings"`
	// GatewayBase 是 OFM 本机渠道网关的 OpenAI 兼容地址（如 http://127.0.0.1:8326/v1）。
	GatewayBase string `json:"gatewayBase"`
	GatewayKey  string `json:"-"` // 内存中间态，只用于构造候选 Spec 的 APIKey。
	// 原 json:"gatewayKey" 是明文标签：Discovery 若被整体序列化就外流密钥
	// （且它嵌套 Candidates[].Spec，Marshal 也会从嵌套层漏）。
	// 由 secret_serialize_test.go 的反射守卫钉住。
}

// Discover 只读扫描 OFM 数据面，产出候选模型（不落盘、不改插件文件）。
func Discover(dshHome string) Discovery {
	out := Discovery{Dir: OFMDataDir(dshHome), Availability: map[string]string{}}
	if out.Dir == "" {
		out.Warnings = append(out.Warnings, "未找到 our-free-model 数据目录")
		return out
	}
	if err := readJSON(filepath.Join(out.Dir, "catalog.json"), &catalogFile{}); err != nil {
		_ = err
	}
	var cat catalogFile
	if err := readJSON(filepath.Join(out.Dir, "catalog.json"), &cat); err != nil {
		out.Warnings = append(out.Warnings, "catalog.json 不可读: "+err.Error())
	}
	var av availabilityFile
	if err := readJSON(filepath.Join(out.Dir, "availability.json"), &av); err == nil {
		out.CatalogAt = av.At
		for id, r := range av.Results {
			out.Availability[id] = r.State
		}
	} else {
		out.Warnings = append(out.Warnings, "availability.json 不可读: "+err.Error())
	}
	var st settingsFile
	hidden := map[string]bool{}
	if err := readJSON(filepath.Join(out.Dir, "settings.json"), &st); err == nil {
		for _, id := range st.HiddenModelIDs {
			hidden[id] = true
		}
		if st.Forward != nil {
			host := st.Forward.Host
			if host == "" {
				host = "127.0.0.1"
			}
			port := st.Forward.Port
			if port <= 0 {
				port = 18899
			}
			out.GatewayBase = "http://" + host + ":" + itoa(port) + "/v1"
		}
	} else {
		out.Warnings = append(out.Warnings, "settings.json 不可读: "+err.Error())
	}

	// 内置网关：OFM 渠道网关默认 127.0.0.1:8326，Key 由宿主写入 ~/.dsh/openai-gateway/api-key。
	if out.GatewayBase == "" {
		out.GatewayBase = "http://127.0.0.1:8326/v1"
	}
	out.GatewayKey = readGatewayKey()

	specs := map[string]Spec{}
	add := func(s Spec) {
		if s.ID == "" || hidden[s.ID] {
			return
		}
		if _, exists := specs[s.ID]; exists {
			return
		}
		s.Label = firstNonEmpty(s.Label, s.ID)
		s.Source = firstNonEmpty(s.Source, "ofm-catalog")
		s.UpdatedAt = nowMs()
		specs[s.ID] = s
	}

	// 1) sealed 车道（EAC 协付）：catalog.sealIds 是「本机可用的付费渠道模型 id」。
	for _, id := range cat.SealIDs {
		add(Spec{
			ID: id, Label: "EAC " + id, Provider: "our-free-model", Lane: LaneEAC,
			Model: id, Format: FormatChat, Tier: "balanced", Group: "EAC 协付",
			Priority: 20, Weight: 60, MaxFail: 3, CooldownSec: 120,
			BaseURL: out.GatewayBase, APIKey: out.GatewayKey,
		})
	}
	// 2) 匿名免费车道：catalog.entries。
	for _, id := range cat.Entries {
		add(Spec{
			ID: id, Label: "OFM " + id, Provider: "our-free-model", Lane: LaneFree,
			Model: id, Format: FormatChat, Tier: "fast", Group: "OFM 免费",
			Priority: 60, Weight: 40, MaxFail: 4, CooldownSec: 90,
			BaseURL: out.GatewayBase, APIKey: out.GatewayKey,
		})
	}
	// 3) Kilo 免费池：catalog.kiloRows（携带上下文窗口/视觉能力）。
	//
	// 注意 provider 必须写 our-free-model：Kilo 车道的模型在 DSH 里同样由 OFM provider
	// 承载（模型选择器里归 OFM 分组），写 "kilo" 会让路由表与真实 provider 对不上。
	for _, k := range cat.Kilo {
		add(Spec{
			ID: k.ID, Label: firstNonEmpty(k.Name, "Kilo "+k.ID), Provider: "our-free-model", Lane: LaneKilo,
			Model: k.ID, Format: FormatChat, Tier: "fast", Group: "Kilo 免费池",
			Priority: 80, Weight: 30, MaxFail: 4, CooldownSec: 90,
			Vision: k.Vision, ContextWindow: k.ContextWindow, MaxOutput: k.MaxOutput,
			BaseURL: out.GatewayBase, APIKey: out.GatewayKey,
		})
	}
	// 候选按优先级 + id 排序，保证面板与调度顺序稳定可复现。
	for _, s := range specs {
		s.Enabled = true
		// catalog 里的条目都来自 OFM 车道（免费 / Kilo / EAC），DSH 的 our-free-model
		// provider 直接认得，因此默认可调度。漏设这个字段会让「可调度」变成 0，
		// 面板明明有 35 个模型却只显示 4 条能进队列（2026-10-19 07:53 现场）。
		s.Routable = true
		// availability 里被上游明确判死的模型先禁用，但不写死：
		// 标记 AutoDisabled，等网关清单证明它还在广播时自动恢复。
		switch out.Availability[s.ID] {
		case "unavailable", "region-blocked":
			s.Enabled = false
			s.AutoDisabled = true
			s.Note = "上游判定不可用（网关广播后可自动恢复）"
		}
		out.Candidates = append(out.Candidates, s)
	}
	sort.Slice(out.Candidates, func(i, j int) bool {
		a, b := out.Candidates[i], out.Candidates[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		return a.ID < b.ID
	})
	return out
}

// DiscoverOFMOnly 只读 availability.json，返回「模型 -> 上游判定状态」。
// 插件自己会逐模型探测（探测周期由 probeIntervalMinutes 控制），这里直接复用它的结论，
// 避免工作台再对同一批免费模型发一遍探测请求（烧额度且拖慢）。
func DiscoverOFMOnly(dir string) map[string]string {
	out := map[string]string{}
	var av availabilityFile
	if err := readJSON(filepath.Join(dir, "availability.json"), &av); err != nil {
		return out
	}
	for id, r := range av.Results {
		out[id] = r.State
	}
	return out
}

// DiscoverOFMOnly 只读 availability.json，返回「模型 -> 上游判定状态」。
// 插件自己会逐模型探测（探测周期由 probeIntervalMinutes 控制），这里直接复用它的结论，
// 避免工作台再对同一批免费模型发一遍探测请求（烧额度且拖慢）。

// readGatewayKey 读取 OFM 网关的 API Key（宿主写在本机 0600 文件里）。
func readGatewayKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	cands := []string{
		filepath.Join(home, ".dsh", "openai-gateway", "api-key"),
	}
	for _, p := range cands {
		if b, err := os.ReadFile(p); err == nil {
			if k := strings.TrimSpace(string(b)); k != "" {
				return k
			}
		}
	}
	return ""
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return jsonUnmarshal(b, v)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

var _ = time.Now
