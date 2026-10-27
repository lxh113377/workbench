package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"workbench/internal/artifact"
	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/verify"
)

// materializeArtifacts 是 P3 的核心：把模型文本变成真实文件，并真跑验收。
//
// 流程：
//  1. 用 artifact.Parse 从回答里解析文件清单（三级降级，缺陷记入 Problems）；
//  2. 把文件原子落盘到任务的 workspace 内（路径已过 SafePath 校验）；
//  3. 用 internal/verify 在工作台进程里真实执行 verify 命令；
//  4. 无论成败都把结果写 artifacts/index.json 与 verify.json，留下可复核证据。
//
// 返回 nil 表示"这次回答不含产物协议"（旧格式），此时退化为纯 Markdown 归档，
// 但会被标记为未验收（Verified=false），因此不会计入今日产出。
func (e *Engine) materializeArtifacts(task *model.Task, finalText, taskDir string, cfg config.Config) *model.VerifyReport {
	// 工作目录选择（external 模式护栏）：
	//   - 有 WorkRoot（用户选定的外部目录）时，产物写进用户真实目录；
	//   - 否则写任务沙箱 workspace（项目模式）。
	//
	// 现场缺陷（2026-10-19）：改造前无条件写任务沙箱，external 模式下
	// 用户看不到任何产物 —— 工作台的"在用户目录干活"承诺失效。
	workDir := filepath.Join(taskDir, "workspace")
	if task != nil && strings.TrimSpace(task.WorkRoot) != "" {
		workDir = task.WorkRoot
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		e.log.Warn("创建产物目录失败", "taskId", task.ID, "err", err.Error())
		return nil
	}
	lim := artifact.Limits{
		MaxFileBytes:  int64ToInt(int64(cfg.Executor.MaxOutputMB) << 20 / 16),
		MaxTotalBytes: cfg.Executor.MaxOutputMB << 20,
	}

	res := artifact.Parse(finalText, lim)
	if !res.HasFiles() {
		// 语义边界（2026-10-19 修正，两类都有实测依据）：
		//
		//   1) 过短且无产物块 → 失败报告。
		//      现场证据：生产数据里 9/77 篇是"我先查看当前工作区…"这类过程独白，
		//      却被判 done 并计入今日产出。这是 P3 失效链的一环。
		//
		//   2) 内容足够长但无产物块 → nil（历史兼容的纯文本归档路径）。
		//      调用方会把 nil 当成未验收（Verified=false），因此不会计入今日产出；
		//      保留 nil 是为了不破坏既有测试与面板的"纯文本任务"语义。
		if n := countWordsEngine(finalText); n < 200 && !hasMarkdownHeading(finalText) {
			reason := "产出过短（" + intText(n) + " 字，最少需要 200 字）：这不是可交付成果，而是执行过程中的一句话或中途截断。"
			rep := &model.VerifyReport{Round: 1, Pass: false, FailReason: reason, At: model.Now()}
			_ = config.WriteJSONAtomic(filepath.Join(taskDir, "verify.json"), rep)
			e.log.Warn("产出过短且无产物块，判定为未完成", "taskId", task.ID, "runes", n)
			return rep
		}
		e.log.Info("回答不含产物块，退化为纯文本归档", "taskId", task.ID, "level", string(res.Level))
		return nil
	}

	// 过短产出判据已合并进上面的"无产物块"分支。
	//
	// 为什么不在解析前做（2026-10-19 实测教训）：带合规产物块的回答**本来就短**
	// （文件内容才是主体，正文只有一两句说明），前置长度门禁会把它们全部误杀。
	// 只有"既没有产物块、正文又极短"才是真垃圾。

	// 落盘真实文件
	index := make([]map[string]any, 0, len(res.Files))
	for _, f := range res.Files {
		dst := filepath.Join(workDir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			e.log.Warn("创建文件父目录失败", "taskId", task.ID, "path", f.Path, "err", err.Error())
			continue
		}
		if err := config.WriteFileAtomic(dst, f.Body); err != nil {
			e.log.Warn("落盘产物失败", "taskId", task.ID, "path", f.Path, "err", err.Error())
			continue
		}
		index = append(index, map[string]any{
			"path": f.Path, "lang": f.Lang, "bytes": f.Bytes, "sha256": f.SHA256,
		})
	}
	if len(index) > 0 {
		_ = config.WriteJSONAtomic(filepath.Join(taskDir, "artifacts-index.json"), map[string]any{
			"level": string(res.Level), "files": index, "problems": res.Problems,
		})
	}

	// 结构性缺陷 = 不可验收：直接判失败并给出返工依据，不浪费一轮真实执行。
	if len(res.Problems) > 0 {
		rep := &model.VerifyReport{
			Round: 1, Pass: false,
			FailReason: "产物结构缺陷：" + strings.Join(res.Problems, "；"),
			At:         model.Now(),
		}
		_ = config.WriteJSONAtomic(filepath.Join(taskDir, "verify.json"), rep)
		e.log.Warn("产物结构缺陷", "taskId", task.ID, "problems", strings.Join(res.Problems, "；"))
		return rep
	}

	// 真实验收
	opt := verify.Options{DefaultTimeoutSec: 90, MaxOutputBytes: 64 << 10}
	// 验收本身再套一层总超时，避免某条命令卡住整个调度（引擎侧另有 Stop 兜底）。
	vctx, vcancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer vcancel()
	vrep := verify.Run(vctx, workDir, res.Manifest.Verify, opt)
	out := &model.VerifyReport{
		Round: 1, Pass: vrep.Pass, FailReason: vrep.FailReason, At: model.Now(),
	}
	for _, oc := range vrep.Outcomes {
		out.Outcomes = append(out.Outcomes, model.VerifyOutcome{
			Cmd: oc.Cmd, Exit: oc.Exit, Expect: oc.Expect, Pass: oc.Pass,
			TimedOut: oc.TimedOut, Ms: oc.Ms,
			Stdout: oc.Stdout, Stderr: oc.Stderr, Skipped: oc.Skipped,
		})
	}
	_ = config.WriteJSONAtomic(filepath.Join(taskDir, "verify.json"), out)
	if out.Pass {
		e.log.Info("产物验收通过", "taskId", task.ID, "files", len(index), "checks", len(out.Outcomes))
	} else {
		e.log.Warn("产物验收失败", "taskId", task.ID, "reason", failReasonOf(out))
	}
	return out
}

// failReasonOf 返回验收失败的人读原因（用于面板与日志）。
func failReasonOf(r *model.VerifyReport) string {
	if r == nil {
		return "无验收报告"
	}
	if r.Pass {
		return ""
	}
	if strings.TrimSpace(r.FailReason) != "" {
		return r.FailReason
	}
	return "验收未通过"
}

// int64ToInt 安全转换（产物上限只在合理范围，不做溢出精致处理）。
func int64ToInt(v int64) int {
	if v > 1<<31-1 {
		return 1<<31 - 1
	}
	if v < 0 {
		return 0
	}
	return int(v)
}

// SelfTestArtifactPipeline 用一段内置样例跑完整的「协议解析 → 落盘 → 真实执行验收」链路，
// 供 CLI --selftest-artifacts 与容器验收使用（完全不依赖模型）。
//
// 设计取舍：样例刻意用 pwsh 的 Test-Path（Windows 本机与 pwsh 容器都能真跑），
// 且**必须真的执行命令**后才判 pass —— 不允许「文件存在就当通过」的伪验收。
func (e *Engine) SelfTestArtifactPipeline(dir string) (*model.VerifyReport, []string) {
	const sample = "<<<ARTIFACTS>>>\n" +
		"```manifest\n" +
		"{\"files\":[{\"path\":\"selftest/hello.md\"}],\"verify\":[{\"cmd\":\"pwsh -NoProfile -NonInteractive -Command \\\"if (Test-Path selftest/hello.md) { exit 0 } else { exit 1 }\\\"\",\"expectExit\":0,\"timeout\":30}]}\n" +
		"```\n" +
		"```file:selftest/hello.md\n" +
		"# 自检产物\n\n该文件由 artifacts 自检链路生成，用于证明落盘与验收都能真跑。\n" +
		"```\n" +
		"<<<END>>>"
	taskDir := filepath.Join(dir, "selftest-artifacts")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return nil, []string{"创建自检目录失败: " + err.Error()}
	}
	task := &model.Task{ID: "selftest-artifacts", Title: "artifacts 自检"}
	rep := e.materializeArtifacts(task, sample, taskDir, e.Config())
	info := []string{
		"dir: " + taskDir,
		"pass: " + boolText(rep != nil && rep.Pass),
	}
	if rep != nil {
		info = append(info, "round: "+intText(rep.Round))
		if rep.FailReason != "" {
			info = append(info, "failReason: "+rep.FailReason)
		}
		for _, oc := range rep.Outcomes {
			info = append(info, "verify: "+oc.Cmd+" exit="+intText(oc.Exit)+" pass="+boolText(oc.Pass))
		}
	}
	return rep, info
}

// boolText 把布尔转成 "true"/"false"（避免 strconv 到热路径的额外 import）。
func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// intText 把整数转成十进制字符串。
func intText(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// countWordsEngine 统计中英文混合字数（与 autonomy.CountWords 同口径，但放在
// engine 本地以避免 engine -> autonomy 的循环依赖）。
//
// 口径：中文按字计（每个汉字 1），连续英文/数字串算 1 个词，标点与空白不计。
func countWordsEngine(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n := 0
	inWord := false
	for _, r := range s {
		switch {
		case r == 32 || r == 9 || r == 10 || r == 13:
			inWord = false
		case r >= 0x4E00 && r <= 0x9FFF, r >= 0x3040 && r <= 0x30FF, r >= 0xAC00 && r <= 0xD7AF:
			n++
			inWord = false
		case r == 0x3000 || r == 0xFF0C || r == 0x3002 || r == 0xFF1B || r == 0xFF1A:
			inWord = false
		default:
			if !inWord {
				n++
				inWord = true
			}
		}
	}
	return n
}

// hasMarkdownHeading 判断文本是否含 markdown 标题行（# ~ ###）。
func hasMarkdownHeading(s string) bool {
	// round 43 独立审计抓到的真实缺陷：这里原来写的是反斜杠 + n 两个字符（不是换行）⇒
	// 无论文本多规范，hasMarkdownHeading 都返回 false，带 `#` 标题的短产物照样被判「产出过短」。
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "# ") || strings.HasPrefix(t, "## ") || strings.HasPrefix(t, "### ") {
			return true
		}
	}
	return false
}
