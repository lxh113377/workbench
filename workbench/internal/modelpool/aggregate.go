package modelpool

// ── 聚合车道投影 ──────────────────────────────────────────────────
//
// 需求1「适配接入更多兼容的模型接口」的最后一步：面板自填的 OpenAI 兼容端点
// 能被登记、能被真实 HTTP 探测，但 DSH 子进程不会真的调用它 —— 执行层只写
// provider our-free-model 的 overlay，而每任务隔离 HOME 的 settings.json 把
// aggregate 车道硬编码成 {"enabled":false,"providers":{}}。
// 后果：OFM 的 buildAggregateCatalog 产不出行 → resolveModel 查不到 entry →
// 静默回退免费车道，用户以为在用自建端点。
//
// 本文件把池内 Spec 投影成 OFM aggregate 契约要求的形状，并**如实拒绝**注定
// 不可用的项（附原因），绝不静默改道。字段名逐条对照插件源码 src/aggregate.js
// 的 buildAggregateCatalog / resolveProvider / authUsable / effectiveChatUrl
// 与 providers/registry.json（2026-10-19 实证，未猜标识符）：
//
//   "aggregate": { "enabled": true, "providers": {
//     "<providerId>": { "enabled": true, "apiKey": "...",
//                       "baseUrl": "http://host/v1", "models": ["m1"], "addedAt": 1 } } }
//
// 三条硬约束（违反任一条，OFM 侧就不服务该 provider）：
//  1. providerId 必须存在于 registry —— parseProviderModelId 用 BY_ID.has 校验；
//  2. auth.kind 为 oauth/adc 的不服务（authUsable 直接 false）；
//  3. baseUrl 不得含 {},否则 hasPlaceholder 命中、该行不产出。
//
// 另一条易踩语义：**线协议由 registry 的 provider.format 决定，不由用户填的
// format 决定**（entryOf 里 format/wire 都取 provider 的）。填错不报错、只会
// 走成 registry 那条线 —— 所以这里作为 Warning 如实上报。
//
// 纯函数设计：registry 由调用方注入（DSH 安装路径由组合根决定），
// 本包因此零 IO、可单测。
//
// 接线方式的重要提醒（我此前在这里写过「executor.taskHome 里一行调用即可」，
// 那是错的）：executor **不能** import 本包 —— 本包的 failover.go 已 import
// executor（FailoverRunner 包装 executor.RunnerContract），反向依赖会
// "import cycle not allowed"（已在 HEAD 快照实测）。正确做法是由组合根
// cmd/workbench 用回调注入，与 Manager.SetRouteSink 同模式。
// 完整写法（三处、已编译验证）见 docs/模型池-聚合车道接通补丁.md §2.4。

import (
	"sort"
	"strings"
)

// ProviderInfo 是 OFM registry.json 里投影用得上那几个字段。
type ProviderInfo struct {
	ID      string // registry provider id，如 open_router / siliconflow / ollama
	Name    string // 显示名
	Auth    string // apiKey / none / oauth / adc
	Format  string // openai-chat / openai-responses / anthropic-messages
	BaseURL string // registry 自带默认 base（用户可覆盖）
	Local   bool   // registry 标记的本地端点（ollama/lmstudio/llamacpp，auth=none）
}

// AggregateNotice 是一条拒绝或告警（都必须展示到面板，静默丢配置是本模块最不能犯的错）。
type AggregateNotice struct {
	SpecID string `json:"specId"`
	Reason string `json:"reason"`
}

// AggregateProjection 是投影结果。
type AggregateProjection struct {
	// Providers 直接对应 settings.aggregate.providers。
	Providers map[string]map[string]any `json:"providers"`
	// ModelIDs 是「池内 Spec.ID → overlay 可写的 provider/model 复合 id」。
	// 只有出现在这里的模型才是真可路由的。
	ModelIDs   map[string]string `json:"modelIds"`
	Rejections []AggregateNotice `json:"rejections,omitempty"`
	Warnings   []AggregateNotice `json:"warnings,omitempty"`
	// Enabled=false 表示没有任何可用投影（aggregate 段就该写 enabled:false）。
	Enabled bool `json:"enabled"`
}

// AggregateSection 返回可直接塞进 settings.json 的 aggregate 段。
func (p AggregateProjection) AggregateSection() map[string]any {
	providers := p.Providers
	if providers == nil {
		providers = map[string]map[string]any{}
	}
	return map[string]any{"enabled": p.Enabled, "providers": providers}
}

// ProjectAggregate 把池内 Spec 投影成 OFM aggregate 车道配置。
//
// 只处理「用户自带端点」范畴的 Spec（lane=custom/aggregate，或显式可路由且带
// 自建 BaseURL）。免费/Kilo/EAC 三条车道由插件自己的匿名池与协付中继服务，
// 不该被写进 aggregate。
func ProjectAggregate(specs []Spec, registry map[string]ProviderInfo) AggregateProjection {
	out := AggregateProjection{
		Providers: map[string]map[string]any{},
		ModelIDs:  map[string]string{},
	}
	if len(registry) == 0 {
		// 没有 registry 就无法校验 providerId，任何投影都可能被 OFM 判死。
		// 明确拒绝而不是猜：宁可不通，不可静默改道。
		for _, sp := range specs {
			if aggregateCandidate(sp) {
				out.Rejections = append(out.Rejections, AggregateNotice{
					SpecID: sp.ID, Reason: "未加载 OFM provider registry，无法校验 providerId",
				})
			}
		}
		return out
	}

	for _, sp := range specs {
		if !aggregateCandidate(sp) {
			continue
		}
		providerID, modelID, ok := splitCompositeID(sp.ID)
		if !ok {
			out.Rejections = append(out.Rejections, AggregateNotice{
				SpecID: sp.ID,
				Reason: "模型 id 必须是 <providerId>/<modelId> 形态（OFM parseProviderModelId 要求）",
			})
			continue
		}
		info, exists := registry[providerID]
		if !exists {
			out.Rejections = append(out.Rejections, AggregateNotice{
				SpecID: sp.ID,
				Reason: "providerId " + providerID + " 不在 OFM registry 内，聚合车道不会服务它",
			})
			continue
		}
		if info.Auth == "oauth" || info.Auth == "adc" {
			out.Rejections = append(out.Rejections, AggregateNotice{
				SpecID: sp.ID,
				Reason: "provider " + providerID + " 用 " + info.Auth + " 登录，聚合车道只服务 API Key 型",
			})
			continue
		}
		if info.Auth == "apiKey" && strings.TrimSpace(sp.APIKey) == "" {
			out.Rejections = append(out.Rejections, AggregateNotice{
				SpecID: sp.ID, Reason: "provider " + providerID + " 需要 API Key，池内未配置",
			})
			continue
		}
		base := strings.TrimSpace(sp.BaseURL)
		if base == "" {
			out.Rejections = append(out.Rejections, AggregateNotice{
				SpecID: sp.ID, Reason: "缺少 BaseURL，无法投影进聚合车道",
			})
			continue
		}
		if strings.ContainsAny(base, "{}") {
			out.Rejections = append(out.Rejections, AggregateNotice{
				SpecID: sp.ID, Reason: "BaseURL 含占位符，OFM hasPlaceholder 会丢弃该行",
			})
			continue
		}
		if sp.Format != "" && info.Format != "" && string(sp.Format) != info.Format {
			out.Warnings = append(out.Warnings, AggregateNotice{
				SpecID: sp.ID,
				Reason: "线协议以 registry 为准：填的是 " + string(sp.Format) +
					"，provider " + providerID + " 实际服务 " + info.Format,
			})
		}
		entry := map[string]any{
			"enabled": true,
			"apiKey":  strings.TrimSpace(sp.APIKey),
			"baseUrl": base,
			"models":  []string{modelID},
		}
		if sp.UpdatedAt > 0 {
			entry["addedAt"] = sp.UpdatedAt
		}
		// 同一 providerId 配了不同端点时 OFM 只能服务一份。这里保留先到者、
		// 把后到者判为不可路由并如实告警 —— 绝不让它静默覆盖。
		if prev, dup := out.Providers[providerID]; dup {
			if prevBase, _ := prev["baseUrl"].(string); prevBase != base {
				out.Warnings = append(out.Warnings, AggregateNotice{
					SpecID: sp.ID,
					Reason: "provider " + providerID + " 已绑定另一个端点 " + prevBase +
						"，OFM 每 provider 只服务一份配置；本模型按不可路由处理",
				})
				continue
			}
			if models, okm := prev["models"].([]string); okm {
				seen := false
				for _, m := range models {
					if m == modelID {
						seen = true
						break
					}
				}
				if !seen {
					prev["models"] = append(models, modelID)
				}
			}
			out.ModelIDs[sp.ID] = providerID + "/" + modelID
			continue
		}
		out.Providers[providerID] = entry
		out.ModelIDs[sp.ID] = providerID + "/" + modelID
	}
	for _, entry := range out.Providers {
		if models, okm := entry["models"].([]string); okm {
			sort.Strings(models)
		}
	}
	out.Enabled = len(out.Providers) > 0
	return out
}

// aggregateCandidate 判断一个 Spec 是否属于「用户自带端点」范畴。
//
// 两道判据缺一不可：
//
//  1. 车道：只认 custom / aggregate。曾经的宽松版（default 分支看 Routable）
//     是**有害的** —— OFM 同步进来的免费/Kilo/EAC 模型也带 BaseURL（本机网关
//     8326）且 Routable=true，于是它们被当成聚合车道候选、按 provider/model
//     拆出 cohere/poolside 等 id 投影进 aggregate，并把**网关密钥**写进那份
//     配置。一旦接线，这些模型会从免费车道改道成「用网关密钥打网关的
//     aggregate 路由」，既绕过车道隔离又外溢凭据。实测真实账本下产生
//     22 条伪拒绝与 2 个被污染的 provider 条目。
//
//  2. 来源：必须是人工添加（user/panel）。lane=aggregate 不等于「用户自填」——
//     网关专属车道 deepseek-official/* 同步进来时也落在 aggregate 车道，
//     但它是 source=gateway-sync 的机器清单产物，不是用户配置的端点；
//     把它当候选会在每次面板刷新时刷出「providerId 不在 registry」的噪声拒绝。
//     （2026-10-22 起该形态已不入池 —— 见 sync.go 的 default 分支与 lanes.go 的
//     PurgeGatewayLanes；本判据作为存量与旧账本的防线继续保留。）
func aggregateCandidate(sp Spec) bool {
	if !sp.Enabled || strings.TrimSpace(sp.BaseURL) == "" {
		return false
	}
	switch sp.Lane {
	case LaneCustom, LaneAggregate:
	default:
		return false
	}
	switch strings.TrimSpace(sp.Source) {
	case "user", "panel", "":
		return true
	default:
		return false
	}
}

// splitCompositeID 按第一个 / 拆 providerId 与 modelId
// （对齐 OFM parseProviderModelId：斜杠必须在首位之后、两段都非空）。
func splitCompositeID(id string) (provider, model string, ok bool) {
	t := strings.TrimSpace(id)
	i := strings.Index(t, "/")
	if i <= 0 {
		return "", "", false
	}
	provider = strings.TrimSpace(t[:i])
	model = strings.TrimSpace(t[i+1:])
	if provider == "" || model == "" {
		return "", "", false
	}
	return provider, model, true
}
