package executor

// 从 Run 主体里抽出的纯逻辑（P1-2 收尾）。
//
// 目的：Run 有 250+ 行且与子进程强耦合（Start/pipe/Job Object），几乎不可单测；
// 但其中的"拼参数 / 造环境变量 / 判定失败类别 / 从 stderr 摘要"是纯函数，
// 抽出来既能被单测覆盖，也让 Run 只留编排。对标 river 把 JobExecutor 的
// 结果判定拆成 jobExecutorResult 的做法。

import (
	"os"
	"strings"
	"time"

	"workbench/internal/config"
	"workbench/internal/model"
)

// BuildArgs 组装 dsh headless 调用参数。
func BuildArgs(cliPath, profileName, overlayPath string) []string {
	return BuildArgsWithPatches(cliPath, profileName, []string{overlayPath})
}

// BuildArgsWithPatches 组装带多个覆盖层的调用参数。
//
// 为什么需要多覆盖层：external（用户选定目录）模式下，模型的文件操作必须越过
// project 运行层默认的 read-only 沙箱（DSH 的 fs 沙箱边界是 session workspace，
// 工作台把 cwd 指向用户目录也没用），因此要再叠一层把 sandboxPolicy.defaultMode
// 设为 danger-full-access 的补丁。作用域限制由工作台自己的 workspace 护栏承担。
func BuildArgsWithPatches(cliPath, profileName string, overlayPaths []string) []string {
	args := []string{"--expose-internals", cliPath, "--profile", profileName}
	for _, p := range overlayPaths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		args = append(args, "--patch", p)
	}
	return append(args, "--json", "-")
}

// SandboxOverlayYAML 是 external 模式追加的运行层沙箱放行补丁。
//
// 字段名来自 @deepseek-ai/dsh-sandbox-policy 的 Config：mode / workspaceRoot；
// 经 DSH 服务定位器命中 id 为 "sandboxPolicy" 的实例后落到 deployment default。
const SandboxOverlayYAML = `- id: sandboxPolicy
  config:
    mode: danger-full-access
`

// BuildEnv 在继承当前进程环境的基础上追加 DSH 隔离变量。
//
// 抽成纯函数的价值：可以断言"隔离 home 一定被设置"、"危险模式一定开启"，
// 而不必真的拉起子进程。
func BuildEnv(home string, base []string) []string {
	if base == nil {
		base = os.Environ()
	}
	env := make([]string, 0, len(base)+4)
	for _, kv := range base {
		// 过滤掉外部已有的 DSH_* 变量，避免宿主环境污染隔离语义
		if strings.HasPrefix(kv, "DSH_HOME=") || strings.HasPrefix(kv, "DSH_PERMISSION_MODE=") ||
			strings.HasPrefix(kv, "DSH_TELEMETRY_DISABLED=") || strings.HasPrefix(kv, "ELECTRON_RUN_AS_NODE=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"DSH_HOME="+home,
		"DSH_PERMISSION_MODE=danger-full-access",
		"DSH_TELEMETRY_DISABLED=1",
		"ELECTRON_RUN_AS_NODE=1",
	)
	return env
}

// ResolveTimeout 计算单次执行超时：任务级优先，缺失回落到执行器默认值。
func ResolveTimeout(taskTimeoutMin, defaultTimeoutMin int) time.Duration {
	if taskTimeoutMin > 0 {
		return time.Duration(taskTimeoutMin) * time.Minute
	}
	if defaultTimeoutMin > 0 {
		return time.Duration(defaultTimeoutMin) * time.Minute
	}
	return 30 * time.Minute
}

// stderrSummary 取 stderr 尾部摘要（用于日志与任务错误字段）。
func stderrSummary(stderr string, max int) string {
	s := strings.TrimSpace(stderr)
	if s == "" {
		return ""
	}
	if max <= 0 {
		max = 400
	}
	return model.Truncate(collapseBlank(s), max)
}

// collapseBlank 把连续空行压成单空行（stderr 里常见大段空白）。
func collapseBlank(s string) string {
	var b strings.Builder
	prevBlank := false
	for _, line := range strings.Split(s, "\n") {
		blank := strings.TrimSpace(line) == ""
		if blank && prevBlank {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
		prevBlank = blank
	}
	return b.String()
}

// FinalizeResult 依据执行产物判定本次结果是否算成功，并在失败时填好 ErrorKind。
//
// 判定顺序（与历史行为一致，抽出来后逐条可测）：
//  1. 卡死（watchdog 判定）→ stalled
//  2. 上下文超时 → timeout
//  3. 调用方取消 → cancelled
//  4. 退出码非 0 → 按 stderr 归类
//  5. 既无 final 文本也无 workspace 产物 → bad_exit（空产出）
//
// 返回的 error 为 nil 表示成功。
func FinalizeResult(res *Result, stderr string, hasWorkspaceFile bool) error {
	if res == nil {
		return errResultNil
	}
	if res.Stalled {
		res.ErrorKind = string(KindStalled)
		return errStalled
	}
	if res.TimedOut {
		res.ErrorKind = string(KindTimeout)
		return errTimedOut
	}
	if res.ErrorKind == string(KindCancelled) {
		return errCancelled
	}
	// 结构化错误类别优先保留（顶层 ev.Error / reason.error）：
	// 现场缺陷：改造前这里无条件用 stderr 覆盖，即使已经解析到结构化 429 也会被冲掉，
	// 于是限流被归成 bad_exit（"无最终答案"），引擎据此换 8 个模型打空整池。
	// 结构化错误优先：既有类别与文本都不被 stderr 覆盖（现场缺陷：429 因此被判成 bad_exit）。
	structuralKind := res.ErrorKind
	structuralMsg := strings.TrimSpace(res.LastError)
	if msg := stderrSummary(stderr, 800); msg != "" && structuralMsg == "" {
		res.LastError = msg
	}
	if res.ExitCode != 0 {
		// 同时用「结构化文本」兜底分类：stderr 可能只是无关噪声，
		// 而 res.LastError 里带着真实类别（如 EAC_UPSTREAM_FAILED / RATE_LIMIT）。
		kind := classify(stderr, res)
		if k2 := classify(res.LastError, res); k2 != KindUnknown && k2 != KindExit {
			kind = k2
		}
		// 文本兜底可把结构化分类升级为限流（429 的 stderr 形如 dsh: RATE_LIMIT...）。
		if kind == KindRateLimited || structuralKind == "" {
			res.ErrorKind = string(kind)
		} else {
			res.ErrorKind = structuralKind
		}
		return newExitError(res.ExitCode, firstNonEmptyStr(res.LastError, stderr))
	}
	if strings.TrimSpace(res.FinalText) == "" && !hasWorkspaceFile {
		res.ErrorKind = string(KindExit)
		return errNoOutput
	}
	return nil
}

// OverlayFromConfig 生成模型覆盖层 YAML（保留独立函数便于单测与未来多 provider 扩展）。
func OverlayFromConfig(modelID string) string { return modelOverlay(modelID) }

// effectiveMaxBytes 计算单任务事件流上限（MB → bytes，带下限保护）。
func effectiveMaxBytes(maxOutputMB int) int64 {
	if maxOutputMB < 1 {
		maxOutputMB = 32
	}
	return int64(maxOutputMB) * 1024 * 1024
}

// normalizeConcurrency 把并发配置钳到 1..6。
func normalizeConcurrency(n int) int {
	if n < 1 {
		return 1
	}
	if n > 6 {
		return 6
	}
	return n
}

var _ = config.ExecutorConfig{}

// firstNonEmptyStr 返回第一个非空字符串（错误信息兜底用）。
func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
