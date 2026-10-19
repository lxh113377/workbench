// Command pooljson 把 workbench --model-pool 的 JSON 输出摊成 KEY=VALUE 行，
// 供容器内的 POSIX sh 断言脚本使用。
//
// 为什么需要它：验收矩阵的 alpine 腿没有 bash / jq / python，
// 而断言必须能在最小镜像里跑 —— 用一个 20KB 的静态二进制换掉三个依赖，
// 顺便让断言可读（grep '^AVAILABLE=' 而不是 jq -r .available）。
//
// 用法：
//
//	pooljson summary <status.json>     # 概览 + 每个模型一行 SPEC|... + 日志 kind 计数
//	pooljson routes <routes.json>      # 每行一个可路由模型 id
//	pooljson spec <status.json> <id>   # 单个模型的 key=value 行
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "用法: pooljson <summary|routes|spec> <json文件> [模型id]")
		os.Exit(2)
	}
	cmd, file := os.Args[1], os.Args[2]
	raw, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败: "+err.Error())
		os.Exit(1)
	}
	switch cmd {
	case "ids":
		ids(raw)
	case "probe":
		probeSummary(raw)
	case "summary":
		summary(raw)
	case "routes":
		routes(raw)
	case "spec":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "spec 需要模型 id")
			os.Exit(2)
		}
		spec(raw, os.Args[3])
	default:
		fmt.Fprintln(os.Stderr, "未知子命令: "+cmd)
		os.Exit(2)
	}
}

// statusDoc 覆盖 --model-pool status 的响应形状（只取断言要用的字段）。
type statusDoc struct {
	Available int    `json:"available"`
	Routable  int    `json:"routable"`
	Total     int    `json:"total"`
	Strategy  string `json:"strategy"`
	ProbeRuns int    `json:"probeRuns"`
	Gateway   struct {
		Base       string `json:"base"`
		HasKeys    bool   `json:"hasKey"`
		Models     int    `json:"models"`
		Error      string `json:"error"`
		Reachable  bool   `json:"reachable"`
		Authorized bool   `json:"authorized"`
		StatusCode int    `json:"statusCode"`
	} `json:"gateway"`
	LastProbe struct {
		Total   int `json:"total"`
		OK      int `json:"ok"`
		Down    int `json:"down"`
		Unknown int `json:"unknown"`
	} `json:"lastProbe"`
	LastSync struct {
		Added   int `json:"added"`
		Updated int `json:"updated"`
		Skipped int `json:"skipped"`
	} `json:"lastSync"`
	Specs []struct {
		ID         string `json:"id"`
		Provider   string `json:"provider"`
		Lane       string `json:"lane"`
		Group      string `json:"group"`
		Priority   int    `json:"priority"`
		Weight     int    `json:"weight"`
		Enabled    bool   `json:"enabled"`
		Routable   bool   `json:"routable"`
		State      string `json:"state"`
		Source     string `json:"source"`
		AutoDisabl bool   `json:"autoDisabled"`
		CooldownTo int64  `json:"cooldownTo"`
		Runtime    *struct {
			State      string `json:"state"`
			CooldownTo int64  `json:"cooldownTo"`
		} `json:"runtime"`
	} `json:"specs"`
	StatusLog []string `json:"statusLog"`
}

func summary(raw []byte) {
	var d statusDoc
	if err := json.Unmarshal(raw, &d); err != nil {
		fmt.Fprintln(os.Stderr, "解析 status 失败: "+err.Error())
		os.Exit(1)
	}
	fmt.Printf("TOTAL=%d\n", d.Total)
	fmt.Printf("AVAILABLE=%d\n", d.Available)
	fmt.Printf("ROUTABLE=%d\n", d.Routable)
	fmt.Printf("STRATEGY=%s\n", d.Strategy)
	fmt.Printf("PROBE_RUNS=%d\n", d.ProbeRuns)
	fmt.Printf("GATEWAY_BASE=%s\n", d.Gateway.Base)
	fmt.Printf("GATEWAY_HAS_KEY=%v\n", d.Gateway.HasKeys)
	fmt.Printf("GATEWAY_MODELS=%d\n", d.Gateway.Models)
	fmt.Printf("GATEWAY_REACHABLE=%v\n", d.Gateway.Reachable)
	fmt.Printf("GATEWAY_AUTHORIZED=%v\n", d.Gateway.Authorized)
	fmt.Printf("GATEWAY_STATUS=%d\n", d.Gateway.StatusCode)
	fmt.Printf("GATEWAY_ERROR=%s\n", strings.ReplaceAll(d.Gateway.Error, "\n", " "))
	fmt.Printf("PROBE_TOTAL=%d\n", d.LastProbe.Total)
	fmt.Printf("PROBE_OK=%d\n", d.LastProbe.OK)
	fmt.Printf("PROBE_DOWN=%d\n", d.LastProbe.Down)
	fmt.Printf("PROBE_UNKNOWN=%d\n", d.LastProbe.Unknown)
	fmt.Printf("SYNC_ADDED=%d\n", d.LastSync.Added)
	fmt.Printf("SYNC_UPDATED=%d\n", d.LastSync.Updated)
	fmt.Printf("SYNC_SKIPPED=%d\n", d.LastSync.Skipped)

	// 每个模型一行，按 id 排序保证输出稳定可比对。
	order := make([]int, len(d.Specs))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return d.Specs[order[a]].ID < d.Specs[order[b]].ID })
	for _, idx := range order {
		s := d.Specs[idx]
		state := s.State
		if s.Runtime != nil && s.Runtime.State != "" {
			state = s.Runtime.State
		}
		fmt.Printf("SPEC|%s|provider=%s|lane=%s|group=%s|priority=%d|weight=%d|enabled=%v|routable=%v|state=%s|source=%s|autoDisabled=%v\n",
			s.ID, s.Provider, s.Lane, s.Group, s.Priority, s.Weight, s.Enabled, s.Routable, state, s.Source, s.AutoDisabl)
	}

	// 状态日志 kind 计数（断言"探测/同步/切换确实留了痕"）。
	kinds := map[string]int{}
	for _, line := range d.StatusLog {
		var rec struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err == nil && rec.Kind != "" {
			kinds[rec.Kind]++
		}
	}
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Printf("LOGKIND|%s|%d\n", k, kinds[k])
	}
}

// probeSummary 读 /api/pool/probe 的 {"probe":{...}} 响应。
func probeSummary(raw []byte) {
	var d struct {
		Probe struct {
			Total   int `json:"total"`
			OK      int `json:"ok"`
			Down    int `json:"down"`
			Unknown int `json:"unknown"`
		} `json:"probe"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		fmt.Fprintln(os.Stderr, "解析 probe 响应失败: "+err.Error())
		os.Exit(1)
	}
	fmt.Printf("PROBE_TOTAL=%d\n", d.Probe.Total)
	fmt.Printf("PROBE_OK=%d\n", d.Probe.OK)
	fmt.Printf("PROBE_DOWN=%d\n", d.Probe.Down)
	fmt.Printf("PROBE_UNKNOWN=%d\n", d.Probe.Unknown)
}

// ids 按调度顺序每行输出一个模型 id（reorder 断言用，避免在 sh 里解析 JSON）。
func ids(raw []byte) {
	var d statusDoc
	if err := json.Unmarshal(raw, &d); err != nil {
		fmt.Fprintln(os.Stderr, "解析 status 失败: "+err.Error())
		os.Exit(1)
	}
	for _, s := range d.Specs {
		fmt.Println(s.ID)
	}
}

func routes(raw []byte) {
	var arr []struct {
		Model    string `json:"model"`
		Provider string `json:"provider"`
		Tier     string `json:"tier"`
		Weight   int    `json:"weight"`
	}
	if err := json.Unmarshal(raw, &arr); err != nil {
		fmt.Fprintln(os.Stderr, "解析 routes 失败: "+err.Error())
		os.Exit(1)
	}
	for _, r := range arr {
		fmt.Printf("ROUTE|%s|provider=%s|tier=%s|weight=%d\n", r.Model, r.Provider, r.Tier, r.Weight)
	}
	fmt.Printf("ROUTE_COUNT=%d\n", len(arr))
}

func spec(raw []byte, id string) {
	var d statusDoc
	if err := json.Unmarshal(raw, &d); err != nil {
		fmt.Fprintln(os.Stderr, "解析 status 失败: "+err.Error())
		os.Exit(1)
	}
	for _, s := range d.Specs {
		if s.ID != id {
			continue
		}
		state := s.State
		if s.Runtime != nil && s.Runtime.State != "" {
			state = s.Runtime.State
		}
		fmt.Printf("FOUND=true\n")
		fmt.Printf("ID=%s\nENABLED=%v\nROUTABLE=%v\nPRIORITY=%d\nWEIGHT=%d\nLANE=%s\nPROVIDER=%s\nSTATE=%s\nSOURCE=%s\nAUTO_DISABLED=%v\n",
			s.ID, s.Enabled, s.Routable, s.Priority, s.Weight, s.Lane, s.Provider, state, s.Source, s.AutoDisabl)
		return
	}
	fmt.Printf("FOUND=false\n")
}
