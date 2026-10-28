package executor

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestCaptureEventError 钉住「上游错误藏在 status 帧的 reason.error 里」这一形态能被抓到（round 58 · T50）。
//
// 样本取自现场 events.jsonl 的三种真实形态（task-1793112012345-13d4 / task-fb880f2adfa8d202）。
// 改造前解析只认 type=="error" ⇒ 三类全丢，台账只剩循环表述「执行失败: bad_exit」。
func TestCaptureEventError(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string // 期望抓到的文本（"" = 不该抓到）
	}{
		{"status 帧 TRANSPORT 流截断",
			`{"type":"status","phase":"turn_end","turn":1,"reason":{"kind":"error","error":{"message":"our free model closed the stream before its finish token, without answering; retrying","code":"TRANSPORT"}}}`,
			"TRANSPORT: our free model closed the stream before its finish token, without answering; retrying"},
		{"status 帧 SERVER 出口不服务（T44 粘性判据依赖它）",
			`{"type":"status","phase":"turn_end","reason":{"kind":"error","error":{"message":"our free model does not serve combo:auto-vision on this egress","code":"SERVER"}}}`,
			"SERVER: our free model does not serve combo:auto-vision on this egress"},
		{"status 帧 RATE_LIMIT（message 无关键词，只有 code 能定性）",
			`{"type":"status","phase":"turn_end","reason":{"kind":"error","error":{"message":"Provider returned error","code":"RATE_LIMIT","status":429}}}`,
			"RATE_LIMIT: Provider returned error"},
		{"正常收尾的 status（end_turn）不得误抓",
			`{"type":"status","phase":"turn_end","reason":{"kind":"end_turn"}}`, ""},
		{"只有 usage 的 status 帧",
			`{"type":"status","phase":"step_start","usage":{"inputTokens":10}}`, ""},
		{"顶层 error 帧（原有路径仍须工作）",
			`{"type":"error","error":{"message":"boom","code":"SERVER"}}`, "SERVER: boom"},
		{"只有 code 没有 message：仍留类别",
			`{"type":"status","reason":{"kind":"error","error":{"code":"RATE_LIMIT"}}}`, "RATE_LIMIT"},
		{"普通 final 帧", `{"type":"final","text":"正文"}`, ""},
	}
	for _, c := range cases {
		var ev Event
		if err := json.Unmarshal([]byte(c.raw), &ev); err != nil {
			t.Fatalf("%s: 样本 JSON 解析失败: %v", c.name, err)
		}
		if got := captureEventError(ev); got != c.want {
			t.Errorf("%s: captureEventError = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestFinalErrorText 钉住「上游错误优先、stderr 只作补充」（T50）。
//
// 旧写法 res.LastError = stderrText 是**无条件覆盖**：事件流里已抓到的结构化错误会被 stderr 噪声冲掉，
// 于是 429 又变回 bad_exit（2026-10-19 事故的放大路径：换 8 个模型打空整池）。
func TestFinalErrorText(t *testing.T) {
	if got := finalErrorText("RATE_LIMIT: Provider returned error", "无关噪声"); !strings.HasPrefix(got, "RATE_LIMIT:") {
		t.Errorf("上游错误必须优先，实得 %q", got)
	}
	if got := finalErrorText("", "dsh: boom"); got != "dsh: boom" {
		t.Errorf("无上游错误时应回落到 stderr，实得 %q", got)
	}
	if got := finalErrorText("TRANSPORT: x", ""); got != "TRANSPORT: x" {
		t.Errorf("stderr 为空时不应拼接分隔符，实得 %q", got)
	}
	if got := finalErrorText("A: x", "B: y"); !strings.Contains(got, "stderr: B: y") {
		t.Errorf("两者都有时都要保留（stderr 作补充），实得 %q", got)
	}
}

// TestKindFromStreamError 钉住「无最终答案」按上游错误正名（T50 的核心收益）。
//
// 现场最痛的一条：429 / 流断 / 出口不服务都表现为「退出码 0、没有 final」，旧代码一律记 KindExit，
// 于是 isRateLimited 判不出来（走换模型重试而不是限流冷却），not_served 也不进 T44 的出口级粘性判定。
func TestKindFromStreamError(t *testing.T) {
	cases := []struct {
		name      string
		streamErr string
		want      Kind
	}{
		{"429 → 限流（message 无关键词，靠 code 定性）", "RATE_LIMIT: Provider returned error", KindRateLimited},
		{"流被切断 → transport", "TRANSPORT: our free model closed the stream before its finish token", KindTransport},
		{"出口不服务 → not_served", "SERVER: our free model does not serve combo:auto-vision on this egress", KindNotServed},
		{"上游流中断 → stream_cut（更精确类别优先于 upstream）", "SERVER: upstream stream read failed: aborted", KindStreamCut},
		{"上游死模型 → model_dead", "SERVER: eac_upstream_failed", KindModelDead},
		{"空错误：不改变原判定", "", KindUnknown},
		{"dsh: 噪声是弱证据，不得推翻原判", "dsh: something", KindUnknown},
		{"匹配不上任何类别：不改变原判定", "SERVER: 一句没见过的话", KindUnknown},
	}
	for _, c := range cases {
		if got := kindFromStreamError(c.streamErr); got != c.want {
			t.Errorf("%s: kindFromStreamError(%q) = %s, want %s", c.name, c.streamErr, got, c.want)
		}
	}
}

// TestRunWiresStreamErrorCapture 是**接线守卫**（T50）。
//
// 为什么要它：本会话已三次踩到「纯函数有单测、生产却不调用」（T36 排除能力、T42 车道偏好、T41 豁免默认值）。
// 解析循环嵌在 run() 内（与子进程强耦合、无法单测），这里退而对源文本立不变量：
// run() 必须真的调用这三个函数 —— 否则上面的表驱动测试全绿也等于没修。
func TestRunWiresStreamErrorCapture(t *testing.T) {
	src, err := os.ReadFile("executor.go")
	if err != nil {
		t.Fatalf("读 executor.go 失败: %v", err)
	}
	body := string(src)
	for _, want := range []string{
		"captureEventError(ev)",                       // 解析循环里真的抓 status.reason.error
		"res.StreamError = txt",                       // 抓到后落到 Result 上
		"finalErrorText(res.StreamError, stderrText)", // 收尾时上游错误优先，不被 stderr 覆盖
		"kindFromStreamError(res.StreamError)",        // 「无最终答案」按上游错误正名
	} {
		if !strings.Contains(body, want) {
			t.Errorf("executor.go 缺少接线 %q —— 辅助函数有测试但生产没调用（假修复）", want)
		}
	}
}
