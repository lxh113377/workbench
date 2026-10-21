package web

import (
	"encoding/json"
	"fmt"

	"workbench/internal/model"
)

// tokenScript 生成安全的 token 注入脚本（JSON 编码，杜绝注入与解析问题）。
func tokenScript(token string) string {
	b, err := json.Marshal(token)
	if err != nil {
		b = []byte("\"\"")
	}
	return "<script>window.__WB_TOKEN__ = " + string(b) + ";</script>"
}

// indexHTML 生成极简首页：整页只有一个自动启动按钮，其余区域保持空净。
//
// 状态反馈完全由按钮自身承担（data-state → 文案/配色/加载环），因此首页不需要
// 任何可见的状态文字与徽标；子页面入口以可见链接条呈现（老大 2026-10-19 指令：
// 「其它子页面的入口在哪？出现在这个页面上」——入口从隐藏后门改为直接摆在首页）。
func indexHTML(token string, phase model.EnginePhase) string {
	return fmt.Sprintf(indexPage, phaseState(phase), phaseHint(phase), phaseLabel(phase), phaseHint(phase), token, token, token, tokenScript(token))
}

// phaseState 把引擎阶段映射为按钮的 data-state（前端据此驱动文案、配色与加载环）。
func phaseState(p model.EnginePhase) string {
	switch p {
	case model.PhaseRunning:
		return "running"
	case model.PhasePaused:
		return "paused"
	case model.PhaseStopped:
		return "stopped"
	default:
		return "idle"
	}
}

// phaseLabel 是按钮文案（唯一的可见文字）。
func phaseLabel(p model.EnginePhase) string {
	switch p {
	case model.PhaseRunning:
		// 「工作台」= 点它进入主功能子界面（目标要求 1：点击后进入主功能子界面）。
		return "工作台"
	case model.PhasePaused:
		return "已暂停"
	case model.PhaseStopped:
		return "重启"
	default:
		return "开始"
	}
}

// phaseHint 是给读屏与按钮 tooltip 用的阶段文字（视觉上不占位）。
func phaseHint(p model.EnginePhase) string {
	switch p {
	case model.PhaseRunning:
		return "工作台运行中，点击进入"
	case model.PhasePaused:
		return "已暂停，点击继续并进入"
	case model.PhaseStopped:
		return "已停止，点击重启"
	default:
		return "待启动，点击开始"
	}
}
// adminHTML 生成管理面板外壳（内容由 app.js 渲染）。
func adminHTML(token string) string {
	return fmt.Sprintf(adminPage, token, token, token, tokenScript(token))
}

// governanceHTML 生成治理控制台外壳。
func governanceHTML(token string) string {
	return fmt.Sprintf(governancePage, token, token, token, tokenScript(token))
}

// JSONString 把任意值序列化成缩进 JSON 字符串（CLI --json 输出用）。
//
// 放在 web 包是为了与既有 CLI 约定一致：acceptance --json 直接复用面板同源的序列化，
// 避免 CLI 与面板两份 JSON 形态漂移。
func JSONString(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}
