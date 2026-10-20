// Package executor 负责把任务变成一次真实的 DSH headless 调用，并把事件流
// 解析成结构化结果。它是整个工作台与外部世界唯一的耦合点。
package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"workbench/internal/artifact"
	"workbench/internal/config"
	"workbench/internal/dshprofile"
	"workbench/internal/model"
	"workbench/internal/ratelimit"
)

// Event 是 DSH --json 输出的一行事件（保留原始字段，便于无损落盘）。
type Event struct {
	Type       string          `json:"type"`
	SessionID  string          `json:"sessionId,omitempty"`
	Phase      string          `json:"phase,omitempty"`
	Text       string          `json:"text,omitempty"`
	Usage      *Usage          `json:"usage,omitempty"`
	Reason     json.RawMessage `json:"reason,omitempty"`
	Error      *ErrInfo        `json:"error,omitempty"`
	Raw        json.RawMessage `json:"-"`
	LineNumber int             `json:"-"`
}

// Usage 是单步 token 用量。
type Usage struct {
	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	CacheReadTokens  int `json:"cacheReadTokens"`
	CacheWriteTokens int `json:"cacheWriteTokens"`
	TotalTokens      int `json:"totalTokens"`
	ReasoningTokens  int `json:"reasoningTokens"`
}

// ErrInfo 是事件流里的错误详情。
type ErrInfo struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

// Result 是一次执行的完整结果。
type Result struct {
	SessionID  string
	FinalText  string
	ExitCode   int
	TimedOut   bool
	Elapsed    time.Duration
	Err        error
	ErrorKind  string
	Usage      Usage
	ToolCalls  int
	Thinking   strings.Builder
	EventsPath string
	LastError  string
	Stalled    bool
	Touch      func()
	// SawThinking 标记本轮是否出现过 thinking 帧：用于自适应放宽卡死阈值。
	SawThinking atomic.Bool
}

func (r *Result) markStalled() { r.Stalled = true }

// Kind 把失败归类为可重试/不可重试的语义错误类别。
type Kind string

const (
	KindNone       Kind = ""
	KindTimeout    Kind = "timeout"
	KindMissingKey Kind = "missing_credential"
	KindModelDead  Kind = "model_unavailable"
	KindUpstream   Kind = "upstream_failed"
	KindBoot       Kind = "boot_failed"
	KindExit       Kind = "bad_exit"
	KindCancelled  Kind = "cancelled"
	KindStalled    Kind = "stalled"
	// KindRateLimited 是上游 429 限流（车道级，不是任务问题）。
	// 引擎据此只冷却出问题的车道并把任务退回队列（不计失败）。
	KindRateLimited Kind = "rate_limited"
	KindUnknown     Kind = "unknown"
)

// Retryable 判断该类错误是否值得重试。
func (k Kind) Retryable() bool {
	switch k {
	case KindTimeout, KindUpstream, KindExit, KindUnknown, KindStalled:
		return true
	case KindRateLimited:
		// 限流可重试：它不是模型下线（那个要长冷却 12h），只需整池冷却后重试。
		return true
	}
	return false
}

// Runner 执行一次任务。
type Runner struct {
	cfg    config.ExecutorConfig
	hooks  *Hooks
	layout config.Layout
	// limiter 是全局请求速率闸门（跨任务共享）。
	//
	// 事故复盘（2026-10-19）：8 路并发 x 失败即换模型 x 25 模型轮转，
	// 一分钟打出 45-70 次请求，免费车道 429 持续 50 分钟。并发数管不住
	// "每秒发多少请求"，必须由令牌桶按长期平均速率约束。nil 表示不限制。
	limiter *ratelimit.Limiter
	// allowAll 是 external（用户目录）模式的放行开关：打开后执行器不再
	// 把产物限制在数据根沙箱内，而是写进用户真实目录。默认关闭。
	allowAll bool
	// wsContext 返回"工作目录上下文"文本（由引擎注入）。未注入时为空串。
	wsContext func() string
}

// New 构造执行器。
func New(cfg config.ExecutorConfig, layout config.Layout) *Runner {
	return &Runner{cfg: cfg, layout: layout}
}

// AllowAll 返回 external 模式的放行开关状态。
func (r *Runner) AllowAll() bool { return r.allowAll }

// SetAllowAll 切换放行开关（external 模式开，项目模式关）。
func (r *Runner) SetAllowAll(v bool) { r.allowAll = v }

// SetWorkspaceContext 注入工作目录上下文回调。
func (r *Runner) SetWorkspaceContext(fn func() string) { r.wsContext = fn }

// workspaceContext 返回当前工作目录上下文（未注入时为空串）。
func (r *Runner) workspaceContext() string {
	if r.wsContext == nil {
		return ""
	}
	return r.wsContext()
}

// SetLimiter 注入全局请求速率闸门（nil 表示不限制）。
func (r *Runner) SetLimiter(l *ratelimit.Limiter) { r.limiter = l }

// LimiterSnapshot 返回当前令牌桶状态（面板可观测）。
func (r *Runner) LimiterSnapshot() ratelimit.Snapshot {
	return r.limiter.Snapshot()
}

// UpdateConfig 热更新执行器配置。
func (r *Runner) UpdateConfig(cfg config.ExecutorConfig) { r.cfg = cfg }

// Run 执行一次任务：在任务目录内以隔离 DSH_HOME 调用 dsh --json，逐行落盘事件，
// 并在取消/超时时杀掉整棵进程树（Job Object）。
// taskHome 返回该任务专属的 DSH_HOME。
//
// 为什么每任务一份：实测在同一 DSH_HOME 下并发跑两个 headless 进程会互相阻塞
// （OFM 的会话映射与凭据刷新是单写者语义）。按任务隔离 HOME 后可安全并发。
//
// 实现要点：profile 目录与 our-free-model 包目录必须**共享同一份实体**（用 junction），
// 否则复制出来的 profile 里 dsh-our-free-model 的 junction 会失效，
// 表现为 "no adapter registered for provider our-free-model"。
func (r *Runner) taskHome(taskID string) (string, error) {
	home := filepath.Join(r.layout.DSHHome, "tasks", taskID)
	if err := os.MkdirAll(home, 0o755); err != nil {
		return "", err
	}
	shared := r.layout.DSHHome

	// 1) profiles 目录：junction 到共享实体（含已配好的 web-desktop profile 与 OFM 挂载）
	if err := linkDir(filepath.Join(shared, "profiles"), filepath.Join(home, "profiles")); err != nil {
		return "", fmt.Errorf("挂载 profiles 失败: %w", err)
	}

	// 2) our-free-model 数据目录：复制少量账号态文件（可读写，避免并发写同一 stats.json）
	ofmShared := filepath.Join(shared, "our-free-model")
	ofmDst := filepath.Join(home, "our-free-model")
	if err := os.MkdirAll(ofmDst, 0o755); err != nil {
		return "", err
	}
	for _, name := range []string{"eac-user.json", "catalog.json", "feed.json"} {
		from := filepath.Join(ofmShared, name)
		to := filepath.Join(ofmDst, name)
		if _, err := os.Stat(to); err == nil {
			continue
		}
		data, rErr := os.ReadFile(from)
		if rErr != nil {
			continue
		}
		_ = config.WriteFileAtomic(to, data)
	}
	settings := map[string]any{
		"version":              1,
		"enabled":              true,
		"exposeRegionModels":   true,
		"hiddenModelIds":       []string{},
		"hideFreeModels":       false,
		"hideEacModels":        false,
		"probeIntervalMinutes": 240,
		"defaultMaxTokens":     32768,
		"streamRecovery":       true,
		"tokenSaver":           map[string]any{"enabled": true, "rtk": true, "prompts": map[string]any{"caveman": "off", "ponytail": "off"}},
		"aggregate":            map[string]any{"enabled": false, "providers": map[string]any{}},
		"combos":               []any{},
		"localOptimize":        map[string]any{"enabled": true, "quota": true, "prefix": true, "title": false, "suggestion": false, "filepath": true},
		"notifyOs":             false,
		"updateCheckHours":     0,
	}
	if err := config.WriteJSONAtomic(filepath.Join(ofmDst, "settings.json"), settings); err != nil {
		return "", err
	}
	return home, nil
}

// linkDir 在 dst 处建立指向 src 的目录链接；已存在则跳过。
// Windows 优先用 junction（无需管理员权限，可跨盘）。
func linkDir(src, dst string) error {
	if st, err := os.Stat(dst); err == nil && st.IsDir() {
		return nil
	}
	_ = os.RemoveAll(dst)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "mklink", "/J", dst, src)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("junction 失败: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := os.Symlink(src, dst); err != nil {
		return err
	}
	return nil
}

func (r *Runner) Run(ctx context.Context, task *model.Task, modelID string) (res *Result, runErr error) {
	// 全局速率闸门：先等令牌再发请求（等待可取消，任务取消时立即释放）。
	if r.limiter != nil {
		if werr := r.limiter.Wait(ctx); werr != nil {
			return &Result{ErrorKind: string(KindCancelled), Err: werr}, werr
		}
	}
	// 前置钩子：任一返回错误即中止本次尝试（对标 river HookWorkBegin）
	if herr := r.hooks.RunBefore(ctx, task, modelID); herr != nil {
		return &Result{ErrorKind: string(KindBoot), Err: herr}, herr
	}
	// 后置钩子：无论成功失败都执行（对标 river HookWorkEnd）
	defer func() { r.hooks.RunAfter(ctx, task, modelID, res, runErr) }()
	res, runErr = r.run(ctx, task, modelID)
	return res, runErr
}

// run 是原有的执行实现（钩子包裹在 Run 外层）。
func (r *Runner) run(ctx context.Context, task *model.Task, modelID string) (*Result, error) {
	exe, cli, err := dshprofile.LocateDSH()
	if err != nil {
		return nil, err
	}
	if r.cfg.DSHExe != "" {
		exe = r.cfg.DSHExe
		cli = filepath.Join(filepath.Dir(exe), "resources", "app", "lib", "desktop-cli.js")
	}
	taskDir := r.layout.TaskDir(task.ID)
	workDir := filepath.Join(taskDir, "workspace")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建任务工作目录失败: %w", err)
	}

	timeout := time.Duration(task.TimeoutMin) * time.Minute
	if timeout <= 0 {
		timeout = time.Duration(r.cfg.DefaultTimeout) * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	overlayPath := filepath.Join(taskDir, "overlay.yml")
	if err := config.WriteFileAtomic(overlayPath, []byte(modelOverlay(modelID))); err != nil {
		return nil, fmt.Errorf("写入模型覆盖层失败: %w", err)
	}

	args := []string{
		"--expose-internals", cli,
		"--profile", r.cfg.ProfileName,
		"--patch", overlayPath,
		"--json", "-",
	}
	cmd := exec.CommandContext(runCtx, exe, args...)
	cmd.Dir = workDir
	taskHome, homeErr := r.taskHome(task.ID)
	if homeErr != nil {
		return nil, fmt.Errorf("准备任务隔离环境失败: %w", homeErr)
	}
	cmd.Env = r.environmentWithHome(taskHome)
	cmd.SysProcAttr = jobSysProcAttr()

	eventsPath := filepath.Join(taskDir, "events.jsonl")
	eventsFile, ferr := os.OpenFile(eventsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if ferr != nil {
		return nil, fmt.Errorf("创建事件文件失败: %w", ferr)
	}
	// 句柄生命周期（2026-10-20 实测缺陷 D8）：用 sync.Once 包一个 closeEvents，
	// 既保证**所有早退路径**（pipe 接管失败、任务隔离环境准备失败、cmd.Start 失败）
	// 都释放句柄，又让正常路径由 consume 自己收尾。
	// 改造前只有这里的 defer：consume 协程可能在 Run 返回后仍在写（wg.Wait 有 15 秒
	// 超时兜底），对已关闭文件继续 Write 的错误被 _ 吞掉，事件尾部静默丢失。
	// 只交给 consume 关闭也不对：早退路径会永久泄漏句柄（实测 executor 包
	// TestRunnerLimiterThrottlesRequests 的 t.TempDir 报 events.jsonl 被占用）。
	closeEventsOnce := &sync.Once{}
	closeEvents := func() { closeEventsOnce.Do(func() { _ = eventsFile.Close() }) }
	defer closeEvents()

	stdout, perr := cmd.StdoutPipe()
	if perr != nil {
		return nil, fmt.Errorf("接管 stdout 失败: %w", perr)
	}
	stderr, perr := cmd.StderrPipe()
	if perr != nil {
		return nil, fmt.Errorf("接管 stderr 失败: %w", perr)
	}
	stdin, perr := cmd.StdinPipe()
	if perr != nil {
		return nil, fmt.Errorf("接管 stdin 失败: %w", perr)
	}

	started := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 DSH 失败: %w", err)
	}
	job := attachJob(ProcessOf(cmd.Process))
	defer job.close()

	res := &Result{EventsPath: eventsPath}
	var stderrBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	// 活跃度：每次收到事件或产出文件都会刷新；超过 stallTimeout 未刷新视为卡死。
	var lastActive atomic.Int64
	lastActive.Store(time.Now().UnixMilli())
	res.Touch = func() { lastActive.Store(time.Now().UnixMilli()) }
	go func() {
		defer wg.Done()
		r.consume(stdout, eventsFile, res, closeEvents)
	}()
	// 卡死看门狗。
	//
	// 自适应阈值（P1 修复）：OFM 上游在思考阶段会静默 60–70 秒、极端可静默 300 秒
	// 才吐第一个正文帧（见 our-free-model README「已知边界」）。固定 180 秒的旧
	// 阈值会把"正在深思的长任务"误判成卡死 —— 现场 glm-5.3 的 4 次失败全部是
	// stalled，正是这条造成的结构性失败。
	// 现在：基础阈值 = StallSeconds（默认 420）；一旦本轮出现过任何 thinking 帧，
	// 阈值再放大到 thinkingStall（默认 2×基础，下限 600 秒）。
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		stall := time.Duration(r.cfg.StallSeconds) * time.Second
		if stall <= 0 {
			stall = 420 * time.Second
		}
		thinkingStall := stall * 2
		if thinkingStall < 600*time.Second {
			thinkingStall = 600 * time.Second
		}
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-watchDone:
				return
			case <-t.C:
				limit := stall
				if res.SawThinking.Load() {
					limit = thinkingStall
				}
				if time.Since(time.UnixMilli(lastActive.Load())) > limit {
					res.markStalled()
					job.kill()
					// 主动关闭读端，避免 consume 协程卡在 Read 上导致收尾挂起
					_ = stdout.Close()
					_ = stderr.Close()
					cancel()
					return
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if stderrBuf.Len() < 1<<20 {
				stderrBuf.WriteString(line)
				stderrBuf.WriteString("\n")
			}
		}
	}()

	go func() {
		// 追加产出协议（P3）：模型必须在一次回答里给出「文件清单 + 文件内容 +
		// 验收命令」，工作台据此落盘并真实执行。协议说明放最后，保证模型读到的是
		// "最终指令"而不是被前面的正文稀释。
		prompt := task.Prompt
		if r.cfg.AppendProtocol {
			prompt += "\n" + artifact.ProtocolInstruction
		}
		_, werr := io.WriteString(stdin, prompt)
		if werr != nil {
			_ = werr
		}
		_ = stdin.Close()
	}()

	// 等待读取 goroutine 收尾；但进程被强杀时 pipe 可能不关闭，必须超时兜底。
	waitCh := make(chan struct{})
	go func() { wg.Wait(); close(waitCh) }()
	select {
	case <-waitCh:
	case <-time.After(15 * time.Second):
		// 读取协程卡住（多半是 pipe 未关闭）：放弃等待，继续收尾。
		// 必须主动关掉两个读端把 consume/stderr 协程解阻塞（配合 D8）：句柄改由
		// consume 自己关闭后，若这里只放弃等待而不解阻塞，卡死的协程会永久持有
		// events.jsonl 句柄 —— 比改造前更糟（后续该文件的原子替换与清理都会撞
		// "被另一进程占用"）。看门狗路径早已用同样手法解阻塞，这里补齐超时路径。
		_ = stdout.Close()
		_ = stderr.Close()
		// 解阻塞后给一次**有界**等待，让 consume 有机会自己写完并释放句柄；
		// 不等就继续往下走的话，run 的 defer 可能抢先关闭而 consume 仍在 Write。
		select {
		case <-waitCh:
		case <-time.After(2 * time.Second):
		}
	}
	waitErr := cmd.Wait()
	res.Elapsed = time.Since(started)
	res.ExitCode = cmd.ProcessState.ExitCode()

	if res.Stalled {
		res.ErrorKind = string(KindStalled)
		res.Err = fmt.Errorf("任务长时间无任何进展，已判定卡死并回收进程")
		job.kill()
		return res, res.Err
	}
	if runCtx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.ErrorKind = string(KindTimeout)
		res.Err = fmt.Errorf("任务超时（%s）", timeout)
		job.kill()
		return res, res.Err
	}
	if ctx.Err() == context.Canceled {
		res.ErrorKind = string(KindCancelled)
		res.Err = ctx.Err()
		job.kill()
		return res, res.Err
	}

	stderrText := strings.TrimSpace(stderrBuf.String())
	res.LastError = stderrText
	if waitErr != nil && res.ExitCode != 0 {
		res.ErrorKind = string(classify(stderrText, res))
		res.Err = fmt.Errorf("DSH 退出码 %d: %s", res.ExitCode, model.Truncate(stderrText, 400))
		return res, nil
	}
	// 有些任务把成果写进文件而没有 final 文本；只要 workspace 里有产物也算成功。
	if strings.TrimSpace(res.FinalText) == "" && !hasWorkspaceOutput(workDir) {
		res.ErrorKind = string(KindExit)
		res.Err = fmt.Errorf("任务未产生最终答案（stderr: %s）", model.Truncate(stderrText, 300))
		return res, nil
	}
	return res, nil
}

func (r *Runner) environmentWithHome(home string) []string {
	env := os.Environ()
	env = append(env,
		"DSH_HOME="+home,
		"DSH_PERMISSION_MODE=danger-full-access",
		"DSH_TELEMETRY_DISABLED=1",
		"ELECTRON_RUN_AS_NODE=1",
	)
	return env
}

func (r *Runner) environment() []string {
	env := os.Environ()
	env = append(env,
		"DSH_HOME="+r.layout.DSHHome,
		"DSH_PERMISSION_MODE=danger-full-access",
		"DSH_TELEMETRY_DISABLED=1",
		"ELECTRON_RUN_AS_NODE=1",
	)
	return env
}

func (r *Runner) consume(reader io.Reader, eventsFile *os.File, res *Result, closeEvents func()) {
	// 写方自己收尾（与 run() 里 sync.Once 的 defer 去重，缺陷 D8）。
	if closeEvents != nil {
		defer closeEvents()
	}
	sc := bufio.NewScanner(reader)
	maxLine := r.cfg.MaxOutputMB * 1024 * 1024
	if maxLine < 1024*1024 {
		maxLine = 1024 * 1024
	}
	sc.Buffer(make([]byte, 0, 256*1024), maxLine)
	lineNo := 0
	var written int64
	maxBytes := int64(r.cfg.MaxOutputMB) * 1024 * 1024
	for sc.Scan() {
		line := sc.Bytes()
		lineNo++
		if written < maxBytes {
			n, _ := eventsFile.Write(line)
			eventsFile.Write([]byte("\n"))
			written += int64(n)
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		ev.LineNumber = lineNo
		ev.Raw = append(ev.Raw[:0], line...)
		if res.Touch != nil {
			res.Touch()
		}
		switch ev.Type {
		case "session":
			if ev.SessionID != "" {
				res.SessionID = ev.SessionID
			}
		case "final":
			res.FinalText = ev.Text
		case "text":
			// 非 final 的文本帧只用于进度估算，不覆盖最终答案
		case "thinking":
			res.SawThinking.Store(true)
			if res.Thinking.Len() < 200_000 {
				res.Thinking.WriteString(ev.Text)
			}
		case "tool_call":
			res.ToolCalls++
		case "status":
			if ev.Usage != nil {
				if ev.Usage.InputTokens > res.Usage.InputTokens {
					res.Usage.InputTokens = ev.Usage.InputTokens
				}
				res.Usage.OutputTokens += ev.Usage.OutputTokens
				res.Usage.CacheReadTokens += ev.Usage.CacheReadTokens
				res.Usage.CacheWriteTokens += ev.Usage.CacheWriteTokens
				res.Usage.TotalTokens += ev.Usage.TotalTokens
				res.Usage.ReasoningTokens += ev.Usage.ReasoningTokens
			}
		case "error":
			if ev.Error != nil {
				res.LastError = ev.Error.Code + ": " + ev.Error.Message
			}
		}
	}
}

// hasWorkspaceOutput 判断任务 workspace 内是否已有产出文件。
func hasWorkspaceOutput(dir string) bool {
	found := false
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || found {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".md", ".txt", ".json", ".csv", ".py", ".js", ".ts", ".go", ".sql", ".sh", ".ps1", ".html", ".xml", ".yaml", ".yml":
			found = true
		}
		return nil
	})
	return found
}

func classify(stderr string, res *Result) Kind {
	// 429 必须先判：现场实测 429 的文本里带 "dsh: RATE_LIMIT"，
	// 若不前置，会被下面的 dsh: 分支吞成 boot_failed —— 于是引擎把它当模型故障，
	// 8 路并发 x 失败即换模型，一分钟打爆车道（2026-10-19 事故根因）。
	if res != nil && res.ErrorKind == string(KindRateLimited) {
		return KindRateLimited
	}
	lower := strings.ToLower(stderr)
	// 文本兜底：结构化字段可能缺失（漏分类的实现）。
	if strings.Contains(lower, "rate_limit") || strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "429") {
		return KindRateLimited
	}
	switch {
	case strings.Contains(lower, "missing_credential"):
		return KindMissingKey
	case strings.Contains(lower, "eac_upstream_failed"), strings.Contains(lower, "reached its end of life"):
		return KindModelDead
	case strings.Contains(lower, "unsupported_reasoning_effort"):
		return KindModelDead
	case strings.Contains(lower, "upstream"), strings.Contains(lower, "timeout"):
		return KindUpstream
	case strings.Contains(stderr, "dsh:"):
		return KindBoot
	case res.ExitCode != 0:
		return KindExit
	}
	return KindUnknown
}

func modelOverlay(modelID string) string {
	if strings.TrimSpace(modelID) == "" {
		modelID = "z-ai/glm-5.3"
	}
	return "- id: agent-default-model\n  config:\n    provider: our-free-model\n    model: " + modelID + "\n    reasoningEffort: high\n"
}
