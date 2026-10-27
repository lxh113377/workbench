package executor

// 从 Run 主体里抽出的纯逻辑（P1-2 收尾）。
//
// 目的：Run 有 250+ 行且与子进程强耦合（Start/pipe/Job Object），几乎不可单测；
// 但其中的"拼参数 / 造环境变量 / 判定失败类别 / 从 stderr 摘要"是纯函数，
// 抽出来既能被单测覆盖，也让 Run 只留编排。对标 river 把 JobExecutor 的
// 结果判定拆成 jobExecutorResult 的做法。

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"workbench/internal/artifact"
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

// promptFor 决定本次调用真正写给子进程的提示词，并回报「本轮是否要求产物」（T24）。
//
// 为什么必须按阶段分流（round 43 独立审计的铁证）：产物协议原先被**无条件**拼进方案调用，
// 而方案提示词第一句就是「只输出一个 JSON 对象，不要任何解释文字、不要 Markdown 围栏」——
// 实测方案 session 缓存里「只输出一个 JSON 对象」与「<<<ARTIFACTS>>>」同框出现，
// 33 条方案尝试里 30 条 degraded（方案 JSON 解析失败 / 方案模型无输出）。
// 方案阶段要的是「想清楚」；产物协议的适用对象是**执行阶段**。
//
// 返回值 required 只对调用方有意义：它决定 failover 是否把「无产物块」当成可切换失败，
// 因此方案阶段必须回报 false（否则方案又被产物判据判死 —— round 24 修过一次的同类问题）。
func promptFor(task *model.Task, appendProtocol bool) (prompt string, required bool) {
	base := ""
	if task != nil {
		base = task.Prompt
	}
	if !appendProtocol || task == nil || task.Phase == model.PhasePlan {
		return base, false
	}
	return base + "\n" + artifact.ProtocolInstruction, true
}

// DSHCommandLine 把一次 DSH 无头调用拼成**可入台账**的字符串。
//
// 为什么需要（round 38 · T21）：目标要求「自动执行的任务通过 DeepSeek Harness Desktop 无头控制执行」，
// 但修前台账只记错误类别与耗时 —— 事后无法自证某次尝试到底有没有真的经 DSH 无头跑、用的哪个 CLI 与 profile。
// 与 BuildArgsWithPatches **同源拼装**，保证「记下来的」与「真正执行的」不会漂移（参数在别处手抄一份=迟早不一致）。
func DSHCommandLine(exe, cliPath, profileName string, overlayPaths []string) string {
	return strings.TrimSpace(exe) + " " + strings.Join(BuildArgsWithPatches(cliPath, profileName, overlayPaths), " ")
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

// stderrTailLimit 是落盘 stderr 的字节上限（只留尾部：失败原因通常在最末几行）。
const stderrTailLimit = 8 << 10

// persistStderrTail 把子进程 stderr 写入 events.jsonl 同目录的 stderr.txt，返回落盘路径。
//
// 为什么需要（2026-10-26 round 9 · T10 实测）：bad_exit 的**判定依据就是 stderr**
// （executor.classify 全靠它），可它此前只活在内存里 —— 分类用完即丢，任务目录里只剩
// events.jsonl + overlay.yml，于是「为什么失败」在产物里无据可查（round 8 真跑一次自动任务，
// bad_exit 47,603 ms，事后翻遍任务目录与日志都答不出原因）。
// 本仓在册教训：失败必须可归因；7×24 无人值守下丢掉失败原因等于把缺陷锁死。
//
// 口径：非空才写；只留尾部 stderrTailLimit 字节；写失败**不阻塞执行**（观测面失败不改变判定）。
func persistStderrTail(eventsPath, stderr string) string {
	s := strings.TrimSpace(stderr)
	if s == "" || strings.TrimSpace(eventsPath) == "" {
		return ""
	}
	if len(s) > stderrTailLimit {
		s = s[len(s)-stderrTailLimit:]
	}
	p := filepath.Join(filepath.Dir(eventsPath), "stderr.txt")
	if err := os.WriteFile(p, []byte(s+"\n"), 0o644); err != nil {
		return ""
	}
	return p
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

// outputExtensions 是「模型产出文件」的扩展名集合（与 hasWorkspaceOutput 同一口径）。
// 看门狗把它当作活跃度信号：模型在静默写盘期间不吐 stdout 事件，
// 但 mtime 会一直前进 —— 只认事件的旧判据会把这类任务误判成卡死并杀掉，
// 已写出的产物一并丢失（本轮对标 river watchStuck 后新增的覆盖）。
var outputExtensions = map[string]bool{
	".md": true, ".txt": true, ".json": true, ".csv": true, ".py": true,
	".js": true, ".ts": true, ".go": true, ".sql": true, ".sh": true,
	".ps1": true, ".html": true, ".xml": true, ".yaml": true, ".yml": true,
}

// stallLimitFor 计算本轮卡死阈值：基础 StallSeconds，出现过 thinking 帧则放大。
// 纯函数（无 IO、无时钟），便于表驱动测试与变异验证。
func stallLimitFor(baseSeconds int, sawThinking bool) time.Duration {
	stall := time.Duration(baseSeconds) * time.Second
	if stall <= 0 {
		stall = 420 * time.Second
	}
	if !sawThinking {
		return stall
	}
	thinkingStall := stall * 2
	if thinkingStall < 600*time.Second {
		thinkingStall = 600 * time.Second
	}
	return thinkingStall
}

// FirstProgressSeconds 是「启动后多久还没有任何进展就提前判卡死」的预算（秒）。
//
// 为什么需要（2026-10-26 round 13 实测）：一次自动任务在计划阶段跑了 452,399 ms 才被看门狗判定卡死，
// 而这 7.5 分钟里子会话只有 turn_start/step_start —— **一步都没有推进**（无事件、无产出文件）。
// 对「零进展」这种形态，等满 StallSeconds（甚至 thinking 放大后的 840 s）毫无收益：
// 车道已经放弃，早回收早轮换才有产出机会，也少烧配额。
//
// 取值：base/3，夹在 [30, 120] 秒。不引入新配置项（避免为一处判定扩配置面）；
// 若将来要可调，再加 executor.firstProgressSeconds 并保留同口径默认。
func FirstProgressSeconds(baseSeconds int) int {
	base := baseSeconds
	if base <= 0 {
		base = 420
	}
	v := base / 3
	if v < 30 {
		v = 30
	}
	if v > 120 {
		v = 120
	}
	return v
}

// IsEarlyStall 判定「从进程启动到现在一次进展都没有」的卡死。
//
// 与 IsStalled 的分工：后者看「最近一次进展距今多久」（容忍静默写盘的长任务）；
// 本函数只看「有没有过第一次进展」——既无事件（lastEventMs 未超过 startedMs），也无产出文件。
//
//   - startedMs：进程启动时刻（毫秒；<=0 表示未知，不判）
//   - lastEventMs：最近一条 stdout 事件时刻（其初始值就是 startedMs）
//   - workDir：任务工作目录（探测产出文件 mtime）
//   - baseSeconds：StallSeconds 配置（据此推导首进展预算）
//   - nowMs：当前时刻（注入以便测试，不使用 time.Now）
func IsEarlyStall(startedMs, lastEventMs int64, workDir string, baseSeconds int, nowMs int64) bool {
	if startedMs <= 0 || nowMs <= 0 {
		return false
	}
	if nowMs-startedMs < int64(FirstProgressSeconds(baseSeconds))*1000 {
		return false
	}
	if lastEventMs > startedMs {
		return false // 有过事件：交给 IsStalled 按「最近进展」判
	}
	// 产出探测带 watermark = startedMs：命中「比启动时刻更新的白名单文件」即刻返回，
	// 不必扫完整棵树（等价性见 latestOutputMtime 注释）。
	if fm := latestOutputMtime(workDir, startedMs); fm > startedMs {
		return false // 有过产出：同上
	}
	return true
}

// IsStalled 是看门狗的活跃度判定（纯函数，双信号取较新者）。
//
//   - lastEventMs：最近一条 stdout 事件的时刻（0 表示从未收到）
//   - workDir：任务工作目录，用于探测产出文件 mtime（空串表示无目录）
//   - baseSeconds：StallSeconds 配置
//   - sawThinking：本轮是否出现过 thinking 帧（阈值放大）
//   - nowMs：当前时刻（注入以便测试，不使用 time.Now）
//
// 语义：事件与产出文件**任一**在前进就不算卡死。只有两者都超过阈值才判卡死。
// 这是 2026-10-22 对标 river watchStuck 后新增的覆盖：旧判据只认事件，
// 会把"静默写盘期"的长任务误判成卡死并杀掉，已写出的产物一并丢失。
func IsStalled(lastEventMs int64, workDir string, baseSeconds int, sawThinking bool, nowMs int64) bool {
	limit := stallLimitFor(baseSeconds, sawThinking)
	last := lastEventMs
	// 门槛值：判据是「now-last > limit」，等价于 last < watermark。
	watermark := nowMs - limit.Milliseconds()
	// 快速路径（2026-10-27 实测优化）：事件信号本身仍在窗口内 ⇒ last = max(事件, 产出) ≥ 事件 ≥ watermark
	// ⇒ 无论产出多新都**不可能**判卡死 ⇒ 这一轮根本不需要读盘。
	//
	// 为什么必须加（改造前的实测代价）：看门狗每 5 s 回调本函数，而 external 模式下 workDir 是
	// 用户真实项目（现场 D:workdesktop = 480,572 个文件），旧实现**无条件**走一遍全树
	// （filepath.Walk 实测单次 18.0 s）⇒ 每个在跑任务全程把 CPU/磁盘元数据打满，同机所有
	// DSH 子进程与 GUI 会话一起被拖慢；同时把「零进展」判定从预算 120 s 拖到 ~150 s
	// （实测 stalled 的 elapsedMs p50 = 149,994 ms = 120 s 预算 + 两轮 18 s 扫描）。
	if lastEventMs >= watermark {
		return false
	}
	if fm := latestOutputMtime(workDir, watermark); fm > last {
		last = fm
	}
	if last <= 0 {
		// 两个信号都没有：尚无任何进展证据，不判卡死（启动初期）。
		return false
	}
	return nowMs-last > limit.Milliseconds()
}

// LatestOutputMtimeMs 返回目录下最新一个产出文件的修改时间（毫秒时间戳）。
// 没有产出文件或目录不可读时返回 0（调用方按「无信号」处理）。
//
// 设计要点：
//   - 只认扩展名白名单，避免把日志/临时文件当产出；
//   - 返回**全量最大值**（watermark=0 ⇒ 不做提前退出），与历史语义逐字一致；
//   - 出错不扩散：遍历的任何错误都被忽略，最坏情况退化为「无产出信号」，
//     与改造前行为一致（不会因为探测失败而误判）。
func LatestOutputMtimeMs(dir string) int64 { return latestOutputMtime(dir, 0) }

// latestOutputMtime 是产出探测的唯一实现（看门狗与 LatestOutputMtimeMs 共用一份，避免两套判据漂移）。
//
// watermarkMs > 0 时启用**提前退出**：一旦发现某个白名单文件的 mtime 严格大于 watermark 就立刻返回
// 该 mtime（SkipAll），不再扫完剩下的树。为什么返回「首个命中」而不是全局最大值仍然等价 ——
// 两个调用方都只把它与 watermark 比较：
//
//	IsStalled    判据 now-last > limit，其中 last = max(lastEventMs, 产出最大 mtime)、watermark = now-limit。
//	             命中 ⇒ 返回值 > watermark ⇒ now-返回值 < limit ⇒ 必不判卡死；全量最大值也必然 > watermark，
//	             两者结论同向。未命中 ⇒ 全树白名单文件都 ≤ watermark ⇒ 返回真实最大值 ⇒ 与旧实现逐位一致
//	             （含 now-last == limit 的边界：命中测试与判据都用严格 >，不会把「恰好等于」判成卡死）。
//	IsEarlyStall 同理（watermark = startedMs，判据 fm > startedMs）。
//
// 为什么改成 WalkDir 而不是 Walk（2026-10-27 本机实测，目录 = D:\workdesktop / 480,572 个文件）：
// Walk 对每个条目额外做一次 Lstat（全树 18.0 s），WalkDir 直接吃 ReadDir 得到的目录项（全树 4.87 s，快 3.7 倍）。
// 先扫顶层文件再递归：产物协议下模型通常把文件写在 cwd 根，命中即可省掉整棵树。
func latestOutputMtime(dir string, watermarkMs int64) int64 {
	if strings.TrimSpace(dir) == "" {
		return 0
	}
	if watermarkMs > 0 {
		if ms, hit := topLevelOutputAfter(dir, watermarkMs); hit {
			return ms
		}
	}
	var latest int64
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if !outputExtensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		ms := info.ModTime().UnixMilli()
		if ms > latest {
			latest = ms
		}
		if watermarkMs > 0 && ms > watermarkMs {
			return filepath.SkipAll // 已证「有比 watermark 更新的产出」⇒ 结论已定，停止遍历
		}
		return nil
	})
	return latest
}

// topLevelOutputAfter 只扫 dir 的顶层文件（不递归），返回第一个 mtime 严格大于 watermark 的白名单文件。
// 存在的意义：模型把产物写在 cwd 根时，一次 ReadDir（几十个条目）就能替代整棵树的遍历 ——
// 命中即返回，语义与递归扫描的命中判据完全相同（缺省不命中则回落到全树递归，不改变结论）。
func topLevelOutputAfter(dir string, watermarkMs int64) (int64, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, false
	}
	for _, e := range entries {
		if e.IsDir() || !outputExtensions[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		if ms := info.ModTime().UnixMilli(); ms > watermarkMs {
			return ms, true
		}
	}
	return 0, false
}

// ArtifactMarker 是本仓产物协议的起始标记（引擎侧 appendProtocol 给出，artifact.Parse 也认它）。
const ArtifactMarker = "<<<ARTIFACTS>>>"

// HasArtifactBlock 判定一段最终文本是否带产物块（纯函数，供执行器给结果打标）。
//
// 为什么需要（round 20 实测的下一步 T18）：实测执行阶段「尝试成功」（kind=ok）但最终文本只是过程独白，
// 引擎的产物闸门随后判「产出过短且无产物块」⇒ 任务未完成。把「有没有产物块」作为**结果字段**上报，
// failover 才能把「协议不合规」当成一次可切换的失败，而不是把首个吐字的模型当成功就此收工。
func HasArtifactBlock(text string) bool {
	return strings.Contains(text, ArtifactMarker)
}

// IsSubstantiveEvent 判定一条事件是否算「实质进展」。
//
// 为什么需要（2026-10-26 round 14 实测）：DSH 子进程一启动就吐 status 握手帧（turn_start / step_start），
// 而 consume 对**每条**事件都刷新活跃度 ⇒ 若「零进展」判定用活跃度信号，它永远不成立：
// 实测首进展预算 120 s 的改动在真跑中一次都没触发（尝试跑满 317 s+ 仍在跑），因为握手帧早已把 lastActive 推过 startMs。
// 所以必须区分「子进程活着」（握手）与「模型真的产出了东西」（实质进展）。
//
// 口径：final/text/thinking/tool_call/error 算；status 里带 usage（一次真实模型步）或 phase 落在
// turn_end/step_end 算；其余 status（turn_start/step_start 等握手）不算。
func IsSubstantiveEvent(ev Event) bool {
	switch ev.Type {
	case "final", "text", "thinking", "tool_call", "error":
		return true
	case "status":
		if ev.Usage != nil {
			return true
		}
		switch ev.Phase {
		case "turn_end", "step_end":
			return true
		}
	}
	return false
}

// EarlyStallMessage 组装「零进展提前回收」的原因文案（纯函数，便于单测与日志比对）。
//
// 为什么要把「判定时刻」写进文案（2026-10-26 round 14 实测残差 T15-③）：预算 120 s，
// 但 `elapsedMs` 实测 152.8 s，多出的 ~33 s 一直无法解释。把**看门狗判定时刻**与
// `elapsedMs`（= cmd.Wait 返回时刻）并排写出来，就能一眼区分「预算没被遵守」与
// 「判定及时、回收（杀进程树）耗了时间」—— 这是两种完全不同的处置。
func EarlyStallMessage(budget, decided time.Duration) string {
	return "启动后零进展（首进展预算 " + budget.String() + "，判定于 " + decided.String() +
		" 且无任何实质进展），已提前回收并轮换下一个模型"
}

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
