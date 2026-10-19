package modelpool

// ── OFM provider registry 加载与聚合车道视图 ─────────────────────
//
// 聚合车道投影必须拿插件的 registry.json 校验 providerId：OFM 的
// parseProviderModelId 用 BY_ID.has(providerId) 判生死，未知 id 的行根本不会被
// 服务。所以「池里有个自定义端点」和「DSH 真能调用它」之间差的不是网络，
// 而是一次清单对账 —— 本文件负责那次对账。
//
// 插件目录定位与 dshprofile.bootstrap 的挂载来源保持一致（工作台 profile 优先，
// 其次用户目录的 desktop/web profile），并且只读不写：插件是它自己文件的唯一写者。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// registryRelPath 是 registry.json 相对插件根的路径（插件里由 src/providers/index.js
// 以 REGISTRY_FILE 读取，这里对齐同一份文件）。
const registryRelPath = "src/providers/registry.json"

// pluginDirName 是 OFM 插件在 profile node_modules 下的目录名。
const pluginDirName = "dsh-our-free-model"

// workbenchProfileName 是工作台专用 profile 名。
//
// 不直接引 dshprofile.ProfileName 是为了避免 modelpool → dshprofile 的依赖边
// （dshprofile 属执行侧引导，池子只做清单与调度）。两处常量必须一致，
// 由 registry_test.go 的 TestProfileNameMatchesBootstrap 钉住。
const workbenchProfileName = "web-desktop"

// OFMPluginDir 按候选顺序定位已安装的 OFM 插件目录，找不到返回空串。
//
// 顺序刻意把「工作台隔离 HOME」放最前：执行器真正跑的是这份 profile，
// 用它校验才不会与运行期行为不一致。
func OFMPluginDir(dshHome string) string {
	candidates := []string{}
	if h := strings.TrimSpace(dshHome); h != "" {
		for _, profile := range []string{workbenchProfileName, "desktop", "web", "headless"} {
			candidates = append(candidates, filepath.Join(h, "profiles", profile, "node_modules", pluginDirName))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, profile := range []string{"desktop", "web", workbenchProfileName, "headless"} {
			candidates = append(candidates, filepath.Join(home, ".dsh", "profiles", profile, "node_modules", pluginDirName))
		}
	}
	for _, c := range candidates {
		if st, err := os.Stat(filepath.Join(c, filepath.FromSlash(registryRelPath))); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// registryProviderFile 是 registry.json 的一条记录（只取投影用得上的字段）。
type registryProviderFile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Auth struct {
		Kind string `json:"kind"`
	} `json:"auth"`
	Format  string `json:"format"`
	BaseURL string `json:"baseUrl"`
	Local   bool   `json:"local"`
}

// LoadProviderRegistry 读取插件 registry 并返回 id → 约束的索引。
//
// 第三个返回值是实际读取的文件路径（面板与 CLI 要展示来源，避免「读到哪份清单」
// 成为不可知信息）。找不到或解析失败必须返回 error —— 调用方要把它当「无法校验」
// 处理，而不是当「空清单」放行：空清单会让所有投影被 ProjectAggregate 整体拒绝
// （宁可不通，不可静默改道）。
func LoadProviderRegistry(dshHome string) (map[string]ProviderInfo, string, error) {
	dir := OFMPluginDir(dshHome)
	if dir == "" {
		return nil, "", fmt.Errorf("未找到已安装的 %s 插件（无法校验 providerId，聚合车道保持关闭）", pluginDirName)
	}
	path := filepath.Join(dir, filepath.FromSlash(registryRelPath))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, path, fmt.Errorf("读取 provider registry 失败: %w", err)
	}
	var rows []registryProviderFile
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, path, fmt.Errorf("解析 provider registry 失败: %w", err)
	}
	out := make(map[string]ProviderInfo, len(rows))
	for _, r := range rows {
		id := strings.TrimSpace(r.ID)
		if id == "" {
			continue
		}
		out[id] = ProviderInfo{
			ID: id, Name: strings.TrimSpace(r.Name),
			Auth: strings.TrimSpace(r.Auth.Kind), Format: strings.TrimSpace(r.Format),
			BaseURL: strings.TrimSpace(r.BaseURL), Local: r.Local,
		}
	}
	if len(out) == 0 {
		return nil, path, fmt.Errorf("provider registry 为空（%s）", path)
	}
	return out, path, nil
}

// ── 清单缓存 ──────────────────────────────────────────────────────
//
// 面板 /api/pool 每几秒轮询一次，而 registry.json 有 120KB+。每次都读盘会让
// 「看一眼聚合车道」变成常态化的 IO 开销；但缓存又不能无过期 ——
// DSH 插件升级会换清单（新增/移除 provider），过期后必须自动跟上。
// 取 10 分钟：与池子自身 10 分钟的同步周期同量级，插件升级半小时内必然反映。

const registryCacheTTL = 10 * time.Minute

type registryCacheEntry struct {
	registry map[string]ProviderInfo
	path     string
	err      error
	at       time.Time
}

var (
	registryCacheMu sync.Mutex
	registryCache   = map[string]registryCacheEntry{}
)

// cachedProviderRegistry 是 LoadProviderRegistry 的带缓存版本（按 dshHome 分键）。
//
// 失败也缓存：插件未安装时每次轮询都去 stat 一圈候选目录毫无意义。
// 但失败缓存的存活时间减半，好让「刚装上插件」这种情况较快自愈。
func cachedProviderRegistry(dshHome string) (map[string]ProviderInfo, string, error) {
	key := strings.TrimSpace(dshHome)
	now := time.Now()
	registryCacheMu.Lock()
	defer registryCacheMu.Unlock()
	if hit, okf := registryCache[key]; okf {
		ttl := registryCacheTTL
		if hit.err != nil {
			ttl = registryCacheTTL / 2
		}
		if now.Sub(hit.at) < ttl {
			return hit.registry, hit.path, hit.err
		}
	}
	registry, path, err := LoadProviderRegistry(dshHome)
	registryCache[key] = registryCacheEntry{registry: registry, path: path, err: err, at: now}
	return registry, path, err
}

// InvalidateProviderRegistryCache 在插件目录可能变化时（同步/引导后）清缓存。
func InvalidateProviderRegistryCache() {
	registryCacheMu.Lock()
	registryCache = map[string]registryCacheEntry{}
	registryCacheMu.Unlock()
}

// AggregateForDSHHome 是接线用的便捷入口：一次调用拿到可落盘的 aggregate 段
// 与「哪些池内模型真的可路由」。
//
// 调用方是组合根 cmd/workbench，**不是** internal/executor：executor 不能 import
// 本包（本包 failover.go 已 import executor，反向依赖会 import cycle not allowed）。
// 接线由 main.go 取到返回值后经 executor.SetAggregateSource 注入闭包，
// executor.taskHome 只调那个闭包。三处改法与编译验证结论见
// docs/模型池-聚合车道接通补丁.md §2.4。
//
// registry 读不到时返回 enabled:false 与空映射 —— 与改造前行为等价，
// 绝不因为清单缺失而把请求静默改道。
func AggregateForDSHHome(dshHome string, specs []Spec) (map[string]any, map[string]string, error) {
	empty := map[string]any{"enabled": false, "providers": map[string]map[string]any{}}
	registry, _, err := cachedProviderRegistry(dshHome)
	if err != nil {
		return empty, map[string]string{}, err
	}
	proj := ProjectAggregate(specs, registry)
	return proj.AggregateSection(), proj.ModelIDs, nil
}

// AggregateStatus 是面向面板/CLI 的聚合车道视图（密钥一律脱敏）。
type AggregateStatus struct {
	RegistryLoaded bool   `json:"registryLoaded"`
	RegistryPath   string `json:"registryPath,omitempty"`
	RegistryCount  int    `json:"registryCount"`
	// Enabled 为 true 表示至少有一个自填端点真的能进 aggregate。
	Enabled   bool                    `json:"enabled"`
	Providers []AggregateProviderView `json:"providers"`
	// Rejections/Warnings 必须展示：静默丢配置是本模块最不能犯的错。
	Rejections []AggregateNotice `json:"rejections,omitempty"`
	Warnings   []AggregateNotice `json:"warnings,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// AggregateProviderView 是单个已投影 provider 的脱敏视图。
type AggregateProviderView struct {
	ProviderID string   `json:"providerId"`
	BaseURL    string   `json:"baseUrl"`
	Format     string   `json:"format"`
	Models     []string `json:"models"`
	HasKey     bool     `json:"hasKey"`
	KeyHint    string   `json:"keyHint,omitempty"`
	// OverlayModel 是写进 DSH overlay 的复合 id。
	OverlayModel string `json:"overlayModel,omitempty"`
}

// AggregateView 计算聚合车道投影的脱敏视图（面板 /api/pool 与 CLI 共用）。
func (s *Service) AggregateView() AggregateStatus {
	specs := s.mgr.Specs()
	registry, path, err := cachedProviderRegistry(s.cfg.DSHHome)
	if err != nil {
		return AggregateStatus{RegistryLoaded: false, Error: err.Error(), Providers: []AggregateProviderView{}}
	}
	proj := ProjectAggregate(specs, registry)
	view := AggregateStatus{
		RegistryLoaded: true, RegistryPath: path, RegistryCount: len(registry),
		Enabled: proj.Enabled, Providers: []AggregateProviderView{},
		Rejections: proj.Rejections, Warnings: proj.Warnings,
	}
	// providerId → overlay 复合 id（取该 provider 下任一池内模型代表）。
	byProvider := map[string]string{}
	for _, overlay := range proj.ModelIDs {
		if i := strings.Index(overlay, "/"); i > 0 {
			if _, seen := byProvider[overlay[:i]]; !seen {
				byProvider[overlay[:i]] = overlay
			}
		}
	}
	for id, entry := range proj.Providers {
		pv := AggregateProviderView{ProviderID: id}
		if b, okf := entry["baseUrl"].(string); okf {
			pv.BaseURL = b
		}
		if k, okf := entry["apiKey"].(string); okf {
			pv.HasKey = k != ""
			pv.KeyHint = keyFingerprint(k)
		}
		if ms, okf := entry["models"].([]string); okf {
			pv.Models = ms
		}
		if info, okf := registry[id]; okf {
			pv.Format = info.Format
		}
		pv.OverlayModel = byProvider[id]
		view.Providers = append(view.Providers, pv)
	}
	sort.Slice(view.Providers, func(i, j int) bool { return view.Providers[i].ProviderID < view.Providers[j].ProviderID })
	return view
}

// keyFingerprint 只回显密钥后四位（明文绝不跨进程边界）。
// web 包有一份同逻辑的 keyHint；这里独立实现以避免 modelpool → web 的反向依赖。
func keyFingerprint(k string) string {
	if k == "" {
		return ""
	}
	if len(k) <= 4 {
		return "…" + k
	}
	return "…" + k[len(k)-4:]
}
