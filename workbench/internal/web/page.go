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

// indexHTML 生成极简首页：整页只有一个主按钮「开始运行」。
func indexHTML(token string, phase model.EnginePhase) string {
	return fmt.Sprintf(indexPage, phaseLabel(phase), phaseLabel(phase), token, tokenScript(token))
}

func phaseLabel(p model.EnginePhase) string {
	switch p {
	case model.PhaseRunning:
		return "运行中"
	case model.PhasePaused:
		return "已暂停"
	case model.PhaseStopped:
		return "已停止"
	default:
		return "待启动"
	}
}

// adminHTML 生成管理面板外壳（内容由 app.js 渲染）。
func adminHTML(token string) string {
	return fmt.Sprintf(adminPage, token, token, tokenScript(token))
}

// governanceHTML 生成治理控制台外壳。
func governanceHTML(token string) string {
	return fmt.Sprintf(governancePage, token, token, tokenScript(token))
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
