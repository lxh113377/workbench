package engine

// lane_health.go — 车道健康探测：区分「临时限速」与「上游按 IP 封禁」。
//
// 为什么需要（2026-10-19 现场）：工作台持续 429 达 64 分钟，多轮代码修复后仍 429。
// 最后从 DSH 的 availability.json 里读到上游原文：
//   "we've detected abusive traffic from this IP address"
// —— 这是平台裁决（外部限制），不是工作台缺陷。修代码、换 profile、调阈值都解不了，
// 只会延长封禁窗口。把这条判据做成代码，是为了让运维能一眼分清该等还是该改。
//
// 判据设计：
//   - 任一模型 available ⇒ Reachable=true，不判 Blocked；
//   - 全部不可用 且 有 abuse/封禁关键词 ⇒ Blocked=true，结论必须写明"不是工作台缺陷"；
//   - 全部不可用 且 有地区限制关键词 ⇒ Blocked=true，结论写明地区限制；
//   - 无数据 ⇒ 不断言（Reachable=false、Blocked=false），只给可读说明。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// LaneHealth 是一次车道健康探测的结论。
type LaneHealth struct {
	Reachable bool   `json:"reachable"`
	Blocked   bool   `json:"blocked"`
	EgressIP  string `json:"egressIp,omitempty"`
	Country   string `json:"country,omitempty"`
	Total     int    `json:"total"`
	Available int    `json:"available"`
	Reason    string `json:"reason"`
	Evidence  string `json:"evidence,omitempty"`
}

// availabilityFile 是 DSH 侧记录各模型可用性的文件名。
type availabilityFile struct {
	At     int64 `json:"at"`
	Egress struct {
		IP      string `json:"ip"`
		Country string `json:"country"`
	} `json:"egress"`
	Results map[string]struct {
		State  string `json:"state"`
		Detail string `json:"detail"`
	} `json:"results"`
}

// LaneHealth 返回当前上游车道健康快照（引擎方法，供 API/面板调用）。
//
// 只读 availability.json，不发起任何网络请求 —— 诊断本身绝不制造流量。
func (e *Engine) LaneHealth() LaneHealth {
	return ProbeLaneHealth(e.layout.DSHHome)
}

// ProbeLaneHealth 读取 <dshHome>/our-free-model/availability.json 并给出结论。
//
// dshHome 为 DSH 的 HOME 目录（探针会去其下 our-free-model 子目录找文件）。
func ProbeLaneHealth(dshHome string) LaneHealth {
	h := LaneHealth{}
	path := filepath.Join(dshHome, "our-free-model", "availability.json")
	b, err := os.ReadFile(path)
	if err != nil {
		h.Reason = "未找到 " + path + "：尚无可用性快照，不断言可用或阻塞"
		return h
	}
	var f availabilityFile
	if err := json.Unmarshal(b, &f); err != nil {
		h.Reason = "可用性快照无法解析: " + err.Error()
		return h
	}
	h.EgressIP = f.Egress.IP
	h.Country = f.Egress.Country
	h.Total = len(f.Results)

	var banned, region string
	for _, r := range f.Results {
		if r.State == "available" {
			h.Available++
			continue
		}
		d := strings.ToLower(r.Detail)
		if strings.Contains(d, "abusive traffic") || strings.Contains(d, "banned") || strings.Contains(d, "forbidden") {
			if banned == "" && r.Detail != "" {
				banned = r.Detail
			}
		}
		if strings.Contains(d, "not available in your country") || strings.Contains(d, "region") {
			if region == "" && r.Detail != "" {
				region = r.Detail
			}
		}
	}

	if h.Available > 0 {
		h.Reachable = true
		h.Reason = "车道可用：" + itoaSeconds(h.Available) + "/" + itoaSeconds(h.Total) + " 个模型可用"
		return h
	}
	// 全部不可用：区分封禁 / 地区限制 / 纯不可用
	switch {
	case banned != "":
		h.Blocked = true
		h.Evidence = banned
		h.Reason = "上游按出口 IP " + f.Egress.IP + " 封禁免费车道：这是平台裁决，不是工作台缺陷"
	case region != "":
		h.Blocked = true
		h.Evidence = region
		h.Reason = "上游按地区限制（当前 " + f.Egress.Country + "）：等多久都不会恢复，属外部限制"
	default:
		h.Reason = "全部 " + itoaSeconds(h.Total) + " 个模型当前不可用（未检出封禁或地区限制关键词）"
	}
	return h
}
