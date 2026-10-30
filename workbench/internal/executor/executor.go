// Package executor 负责把任务变成一次真实的 DSH headless 调用，并把事件流
// 解析成结构化结果。它是整个工作台与外部世界唯一的耦合点。
package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

	"workbench/internal/config"
	"workbench/internal/dshprofile"
	"workbench/internal/model"
	"workbench/internal/ratelimit"
	"workbench/internal/winhide"
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
	// StderrPath 是本轮子进程 stderr 的落盘位置（有内容才非空；由来见 persistStderrTail）。
	StderrPath string
	// StreamError 是事件流里抓到的**上游真实错误**（round 58 · T50，由来见 captureEventError）。
	// 与 FinalText/SessionID 同一个读流 goroutine 写、cmd.Wait 之后读，不引入新竞态。
	StreamError string
	// CliPath / Profile / Command 是「这次尝试确实经 DSH 无头控制执行」的自证字段（round 38 · T21）：
	//	CliPath —— 实际使用的 desktop-cli.js 路径；
	//	Profile —— 实际使用的 profile（本仓恒为 web-desktop，见 dshprofile/bootstrap.go 的白名单说明）；
	//	Command —— exe + 全部参数（与 BuildArgsWithPatches 同源拼装，不含密钥），可直接入台账。
	CliPath string
	Profile string
	Command string
	// Model 是**本次调用真正执行的模型 id**（round 44 · T27）。
	// 为什么必须由执行器上报：`FailoverRunner` 会在自己内部沿候选链换车道，引擎只知道「它选的是谁」；
	// 台账/健康度若记 selected 而不记 actual，就会把失败算到没被执行的模型头上（审计实测 3/3 不符）。
	Model     string
	LastError string
	Stalled   bool
	// EarlyStall 标记「启动后零进展」的提前回收：与 Stalled 同 kind，但原因不同，便于归因（round 14 · T15）。
	EarlyStall bool
	// ArtifactBlock 标记最终文本里带不带本仓产物协议块（`<<<ARTIFACTS>>>`）。
	// 用途：failover 对「提示词要求产物的任务」把「无产物块」当成可切换的失败（round 21 · T18）。
	ArtifactBlock bool
	// lastProgress 只在**实质进展**时前进（握手 status 帧不算），供 IsEarlyStall 判「从未起步」。
	lastProgress atomic.Int64
	// StallDecidedMs 是看门狗**判定时刻**距启动的毫秒数（0 表示未判定）。
	// 与 elapsedMs（cmd.Wait 返回时刻）之差 = 回收进程树的耗时，用于把「预算是否被遵守」与
	// 「判定后回收花了多久」分开读（round 14 实测残差 T15-③）。
	StallDecidedMs int64
	Touch          func()
	// SawThinking 标记本轮是否出现过 thinking 帧：用于自适应放宽卡死阈值。
	SawThinking atomic.Bool
	// ArtifactsRequired 标记本轮提示词里**真的追加了产物协议**（AppendProtocol 开关 + 拼 prompt 的那一方才知道）。
	//
	// 为什么要用执行器上报而不是让上层扫 task.Prompt：协议是**执行器**拼进去的（见下面写 stdin 的那段），
	// task.Prompt 里根本没有 `<<<ARTIFACTS>>>`。round 21 实测：按 task.Prompt 判据时 no_artifact 一次都没触发。
	// 用 atomic 是为了与 SawThinking 同口径 —— 该字段在写 prompt 的 goroutine 里置位，收尾方在另一 goroutine 读。
	ArtifactsRequired atomic.Bool
}

func (r *Result) markStalled() { r.Stalled = true }

// Kind 把失败归类为可重试/不可重试的语义错误类别。
type Kind string

const (
	KindNone       Kind = ""
	KindTimeout    Kind = "timeout"
	KindMissingKey Kind = "missing_credential"
	KindModelDead  Kind = "model_unavailable"
	// KindNotServed 是「上游在这条出口(egress)上根本不服务这个模型」——round 33 实测形态：
	// `dsh: SERVER: our free model does not serve "combo:auto-free" on this egress`。
	// 与 model_unavailable 分层：后者可由探测（端点可达）恢复，本类别**不能** —— 它是配置/出口级事实（T17②-b）。
	KindNotServed Kind = "not_served"
	KindUpstream  Kind = "upstream_failed"
	KindBoot      Kind = "boot_failed"
	KindExit      Kind = "bad_exit"
	KindCancelled Kind = "cancelled"
	KindStalled   Kind = "stalled"
	// KindNoArtifact 是「回答有内容、但没有产物块」的失败类别（round 22 实测：引擎台账曾落成 unknown）。
	// 与 bad_exit 的区别：bad_exit 是「没有最终答案」，本类别是「有答案但不符合产物协议」。
	KindNoArtifact Kind = "no_artifact"
	// KindQuotaExhausted 是**免费额度耗尽**（round 91 · T76）：与瞬时 429 同属限流族，但恢复时间尺度完全不同。
	//
	// 为什么必须分开（现场实测 2026-10-29，近两小时 409 次 rate_limited）：
	//   · 瞬时 429（`too many requests; retry later`）几秒到几十秒就能恢复 —— 整池冷却后重试是对的；
	//   · 额度耗尽的三类原文写着要**人/账号侧动作**：
	//       `Sign up for free to continue and explore 500 other models. Takes 2 minutes to sign up.`
	//       `Free usage limit reached. Free models work in OpenCode (https://...)`
	//       `Provider returned error`（携带 429 状态）
	//     现场每次都恰在 ~3 秒返回（elapsedMs 中位 2.9s，333/409 落在 2.5–4s 桶）⇒ 引擎在**原地空转**：
	//     冷却窗口一开就再试、每次都秒失败、再冷却 —— 同一分钟最多撞 13 次，白烧尝试预算与日志。
	// 处置差异：额度耗尽 ⇒ 该车道**长冷却**（见 modelpool 侧 quota 处置）+ 不计入「模型可归因失败」，
	// 避免把「账号没额度」错记成「这个模型不行」而把它永久踢出候选。
	KindQuotaExhausted Kind = "quota_exhausted"
	// KindRateLimited 是上游 429 限流（车道级，不是任务问题）。
	// 引擎据此只冷却出问题的车道并把任务退回队列（不计失败）。
	KindRateLimited Kind = "rate_limited"
	// KindTransport 是**网络/传输层**故障（2026-10-19 现场补齐）。
	//
	// 现场（扫描 130 个任务的 events.jsonl，202 条 turn_end 错误）：
	//   TRANSPORT|ETIMEDOUT 11 次、ENOTFOUND 7 次、EACCES 5 次 —— 合计 23 次
	//   STREAM_CUT 6 次
	// 这些错误此前**没有专属分支**，落进兜底后被显示成「启动失败」（boot_failed），
	// 于是排查方向被误导到「进程/环境启动问题」，而真实原因是网络连不上或流被中断。
	//
	// 语义：transport = 连不上对端（DNS/连接/权限）；与「模型进程起不来」完全不同域。
	KindTransport Kind = "transport_failed"
	// KindStreamCut 是上游流**中途被切断**（连上了但读一半断了）。
	// 与 transport 区分：transport 是「没连上」，stream_cut 是「连上后断了」。
	KindStreamCut Kind = "stream_cut"
	KindUnknown   Kind = "unknown"
	// KindNoLane 是「池内没有任何可用车道」——不是某条车道的问题，而是**整池此刻不可用**。
	// 引擎据此把任务退回队列并设池闸门，而不是把任务判失败（T19：round 35 实测全池仅剩 not_served 车道时，
	// 引擎仍开跑并把两个任务烧成 boot_failed 任务失败）。
	KindNoLane Kind = "no_lane"
	// KindPoolWaitCapped / KindTaskPanic / KindTransitionFailed 是**引擎侧**产生的失败类别
	// （环境等待封顶判死、panic 兜底、状态迁移失败），与 retry.PoolWaitCapped 等同名对应。
	// 为什么在 executor 也登记一份：本包的 Retryable/分类面与 retry 侧靠「字符串一一对应」维持契约，
	// 少一侧就会出现「executor 不认识引擎自己产出的类别」——2026-10-29 的 rate_limited 缺口正是
	// 这个形态的第三次复发，现由 internal/retry/kind_drift_test.go 逐条双向比对钉死。
	KindPoolWaitCapped   Kind = "pool_wait_capped"
	KindTaskPanic        Kind = "panic"
	KindTransitionFailed Kind = "transition_failed"
	// KindCrashLoop / KindVerifyFailed / KindReplan 是引擎侧「非模型原因的终态处置」类别
	// （崩溃救援判死 / 验收未通过排返工 / 返工用尽排重规划）。此前它们只在 engine 里以裸字符串直写
	// task.ErrorKind，两侧枚举都不认识 ⇒ ParseKind 收敛成 Unknown，而 Unknown.Retryable()==true
	// ⇒ 判死类任务反而落进最宽的重试桶；面板中文标签与 Prometheus 基线也接不住（issue #9）。现与 retry.CrashLoop / VerifyFailed / Replan 同名收编，
	// **字符串值逐字不变**（生产台账里已是这个值）。一致性由 internal/retry/kind_drift_test.go 双向对账。
	KindCrashLoop    Kind = "crash_loop"
	KindVerifyFailed Kind = "verify_failed"
	KindReplan       Kind = "replan"
)

// ErrNoLane 是「池内无可用车道」的哨兵错误：模型池侧的调度器在候选链为空时返回它，
// 引擎用 errors.Is 识别，转成「设池闸门 + 任务退回队列」的处置（与限流同路）。
var ErrNoLane = errors.New("模型池无可用车道")

// Retryable 判断该类错误是否值得重试。
func (k Kind) Retryable() bool {
	switch k {
	case KindTimeout, KindUpstream, KindExit, KindUnknown, KindStalled, KindNoArtifact, KindNotServed:
		return true
	case KindRateLimited:
		// 限流可重试：它不是模型下线（那个要长冷却 12h），只需整池冷却后重试。
		return true
	case KindQuotaExhausted:
		// 额度耗尽同样可「重试」，但语义是**等额度窗口**而不是等几秒 —— 冷却长度由 modelpool 侧处置；
		// 此处判 true：任务不该因账号没额度被判死，只让车道进长冷却。
		return true
	// 引擎侧三类确定性终态**不可**重试（与 retry.Retryable 同判）：
	// 环境等待封顶判死 = 预算已耗尽；panic = 同因必再炸；状态迁移失败 = 本地状态机/磁盘问题。
	case KindPoolWaitCapped, KindTaskPanic, KindTransitionFailed:
		return false
	// 引擎侧三类「终态处置」同样不可重试（与 retry.Retryable 同判）：
	// crash_loop = 重投会把进程再弄死一次；verify_failed / replan 的再跑由返工环与重规划环负责，
	// 不归重试策略 —— 否则同一件事被两套额度各记一次。
	case KindCrashLoop, KindVerifyFailed, KindReplan:
		return false
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
		// 必须用 hiddenCmd 而不是裸 exec.Command（2026-10-19 现场修复）：
		// cmd.exe 是 CONSOLE(3) 子系统程序，不隐藏就会**每个任务闪一次控制台窗口** ——
		// linkDir 在每个任务准备阶段都会执行（挂 profiles junction）。
		// 同一文件里 DSH 主进程早已用 winhide.SysProcAttr() 带 HideWindow + CREATE_NO_WINDOW，
		// 唯独这处裸奔。现在统一收敛到 hiddenCmd。
		cmd := hiddenCmd("cmd", "/c", "mklink", "/J", dst, src)
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
	if strings.TrimSpace(task.WorkRoot) != "" {
		// external（用户目录）模式：cwd 指向用户真实项目，**不建任务沙箱**。
		//
		// 现场缺陷（2026-10-19）：改造前这里无条件 MkdirAll(taskDir/workspace)，
		// 于是 external 模式下数据根里仍留一堆空沙箱目录，e2e 断言
		// "no task sandbox dir in external mode" 因此变红（实测 dirs=2）。
		workDir = task.WorkRoot
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			return nil, fmt.Errorf("创建用户工作目录失败: %w", err)
		}
	} else if err := os.MkdirAll(workDir, 0o755); err != nil {
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

	// 参数**只经 BuildArgs（已被 pure_test 钉住形态）一处拼装**，不在调用点手抄一遍：
	// 否则「测试覆盖的形态」与「真正执行的形态」会各走各的（round 38 · T21 顺手修掉的历史重复）。
	args := BuildArgs(cli, r.cfg.ProfileName, overlayPath)
	cmdLine := DSHCommandLine(exe, cli, r.cfg.ProfileName, []string{overlayPath})
	cmd := exec.CommandContext(runCtx, exe, args...)
	cmd.Dir = workDir
	taskHome, homeErr := r.taskHome(task.ID)
	if homeErr != nil {
		return nil, fmt.Errorf("准备任务隔离环境失败: %w", homeErr)
	}
	cmd.Env = r.environmentWithHome(taskHome)
	cmd.SysProcAttr = winhide.SysProcAttr()

	eventsPath := filepath.Join(taskDir, "events.jsonl")
	eventsFile, ferr := os.OpenFile(eventsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if ferr != nil {
		return nil, fmt.Errorf("创建事件文件失败: %w", ferr)
	}
	// 句柄生命周期（2026-10-20 实测缺陷 D8 的正确修法）：
	// 用 sync.Once 包一个 closeEvents，既保证**所有早退路径**（pipe 接管失败、
	// 任务隔离环境准备失败、cmd.Start 失败）都释放句柄，又让正常路径由 consume
	// 自己收尾 —— 改造前只有 run 的 defer，consume 协程可能在 Run 返回后仍在写
	// （wg.Wait 有 15 秒超时兜底），对已关闭文件继续 Write 的错误被 _ 吞掉、
	// 事件尾部静默丢失；反之只交给 consume 关闭则会把句柄永久泄漏在早退路径上
	// （实测：executor 包 TestRunnerLimiterThrottlesRequests 因用不存在的 exe
	// 触发 Start 失败，t.TempDir 清理报 events.jsonl "used by another process"）。
	// 超时兜底路径另外主动关掉两个读端解阻塞 consume，避免协程永久持有句柄。
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

	res := &Result{EventsPath: eventsPath, CliPath: cli, Profile: r.cfg.ProfileName, Command: cmdLine, Model: modelID}
	var stderrBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	// 活跃度：每次收到事件或产出文件都会刷新；超过 stallTimeout 未刷新视为卡死。
	var lastActive atomic.Int64
	// 启动时刻同时充当「事件信号的初始值」：IsEarlyStall 靠它区分「从未有过事件」与「有过事件但久未刷新」。
	startMs := started.UnixMilli()
	lastActive.Store(startMs)
	// 实质进展从 0 起算：IsEarlyStall 用「从未有过实质进展」作为零进展判据。
	res.lastProgress.Store(0)
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
	//
	// 活跃度双信号（2026-10-22 对标 river watchStuck 后新增）：
	//   ① stdout 事件（res.Touch，consume 里每条事件刷新）；
	//   ② workspace 产出文件的 mtime（LatestOutputMtimeMs）。
	// 为什么必须加②：artifact 产出协议下，模型常常"先静默写文件、最后才吐 final 事件"，
	// 长文写作类任务的静默写盘期可达数分钟，期间①完全不刷新。只认①时，
	// 这类任务会在 StallSeconds 到期被判卡死并杀进程树，**已写出的产物一并丢失**。
	// 判据取两者较新者：任一信号在前进就算"有进展"。
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-watchDone:
				return
			case <-t.C:
				// 判定逻辑收敛到两个纯函数（都可被单测直接覆盖）：
				//   IsEarlyStall —— 启动后**零进展**（没事件、没产出）：早回收早轮换（round 14 · T15）；
				//   IsStalled    —— 双信号取较新者，按「最近一次进展距今」判（容忍静默写盘的长任务）。
				nowMs := time.Now().UnixMilli()
				early := IsEarlyStall(startMs, res.lastProgress.Load(), workDir, r.cfg.StallSeconds, nowMs)
				if early || IsStalled(lastActive.Load(), workDir, r.cfg.StallSeconds, res.SawThinking.Load(), nowMs) {
					res.EarlyStall = early
					res.StallDecidedMs = nowMs - startMs
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
		// 走纯函数决定「真正写给子进程的提示词」：方案阶段**不得**拼产物协议（T24，见 promptFor 注释）。
		prompt, required := promptFor(task, r.cfg.AppendProtocol)
		if required {
			// 让上层（failover）知道「本轮要求产物」：只有拼 prompt 的这里才有这个事实。
			res.ArtifactsRequired.Store(true)
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
		// 必须主动关掉两个读端把 consume/stderr 协程解阻塞（2026-10-20，配合 D8）：
		// 事件文件句柄改由 consume 自己关闭之后，若这里只放弃等待而不解阻塞，
		// 卡死的协程会永久持有 events.jsonl 句柄 —— 比改造前更糟（Windows 上
		// 后续该文件的原子替换与清理都会撞「被另一进程占用」）。
		// 看门狗路径早已用同样的手法解阻塞，这里补齐超时路径。
		_ = stdout.Close()
		_ = stderr.Close()
		// 解阻塞后再给一次**有界**等待：让 consume 有机会自己收尾（写完并关闭句柄）。
		// 不做这件事时 run 的 defer 可能先释放句柄，而 consume 仍在 Write ——
		// 那正是 D8 的原始形态，只是概率变低；有界等待把它压到可忽略。
		select {
		case <-waitCh:
		case <-time.After(2 * time.Second):
		}
	}
	waitErr := cmd.Wait()
	res.Elapsed = time.Since(started)
	res.ExitCode = cmd.ProcessState.ExitCode()
	// 2026-10-26 round 9（T10）：四条收尾路径（卡死/超时/取消/正常结束）都在这里汇合，
	// stderr 必须在此刻落盘 —— 否则 bad_exit 的判定依据随内存一起消失（见 pure.go persistStderrTail）。
	res.StderrPath = persistStderrTail(eventsPath, stderrBuf.String())
	// 给结果打「产物块」标记：必须在 FinalText 定稿之后、且在看门狗/超时等失败分支之前 ——
	// 那些分支也要带着这个标记回去，failover 才判得准（round 21 · T18）。
	res.ArtifactBlock = HasArtifactBlock(res.FinalText)

	if res.Stalled {
		res.ErrorKind = string(KindStalled)
		if res.EarlyStall {
			res.Err = fmt.Errorf("%s", EarlyStallMessage(
				time.Duration(FirstProgressSeconds(r.cfg.StallSeconds))*time.Second,
				time.Duration(res.StallDecidedMs)*time.Millisecond))
		} else {
			res.Err = fmt.Errorf("任务长时间无任何进展，已判定卡死并回收进程")
		}
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
	// T50：上游错误优先，stderr 作补充（旧写法无条件用 stderr **覆盖**，把结构化错误冲掉）。
	res.LastError = finalErrorText(res.StreamError, stderrText)
	if waitErr != nil && res.ExitCode != 0 {
		res.ErrorKind = string(classify(stderrText, res))
		res.Err = fmt.Errorf("DSH 退出码 %d: %s", res.ExitCode, model.Truncate(stderrText, 400))
		return res, nil
	}
	// 有些任务把成果写进文件而没有 final 文本；只要 workspace 里有产物也算成功。
	if strings.TrimSpace(res.FinalText) == "" && !hasWorkspaceOutput(workDir) {
		// T50：现场最痛的一条 —— 429 / 流断 / 出口不服务都表现为「退出码 0 但没有最终答案」，
		// 旧代码一律记 KindExit（bad_exit），于是 isRateLimited 判不出来、引擎换 8 个模型打空整池。
		// 现在用抓到的上游错误正名：拿到明确通道类别就按它归因，拿不到才维持 KindExit。
		if k := kindFromStreamError(res.StreamError); k != KindUnknown {
			res.ErrorKind = string(k)
			res.Err = fmt.Errorf("任务未产生最终答案（上游错误: %s）", model.Truncate(res.StreamError, 300))
			return res, nil
		}
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

func (r *Runner) consume(reader io.Reader, eventsFile *os.File, res *Result, closeEvents func()) {
	// 写方自己收尾（缺陷 D8 的一面）；与 run() 的 defer 由 sync.Once 去重。
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
		// 只有**实质进展**才算「起步」：握手 status 帧（turn_start/step_start）不刷新它，
		// 否则「启动后零进展」判定会被握手帧永久掩盖（round 14 实测）。
		if IsSubstantiveEvent(ev) {
			res.lastProgress.Store(time.Now().UnixMilli())
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
			// T50：上游错误藏在 status 帧的 reason.error 里（不在 stderr、也不在顶层 error 帧）。
			// 只留第一条 —— 它最贴近根因，后续同型噪声不改写它。
			if txt := captureEventError(ev); txt != "" && res.StreamError == "" {
				res.StreamError = txt
			}
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
			if txt := captureEventError(ev); txt != "" {
				res.LastError = txt
				if res.StreamError == "" {
					res.StreamError = txt
				}
			}
		}
	}
}

// hasWorkspaceOutput 判断任务 workspace 内是否已有产出文件。
//
// 2026-10-27 优化（与看门狗探针同一方向：消灭对同一棵大树的低效全量扫描）：
//   - Walk → WalkDir：不额外做 Lstat（现场 Go 侧可见 295,305 文件 / 431.5k 目录项的树，实测 16–21 s → 4.9–6.6 s）；
//   - 命中即 SkipAll：改造前即使已 found 仍会把整棵树走完（found 只挡住写入，不挡遍历）。
//
// 返回值仍只取决于「是否存在白名单文件」，结论不变。
func hasWorkspaceOutput(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found || d == nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".md", ".txt", ".json", ".csv", ".py", ".js", ".ts", ".go", ".sql", ".sh", ".ps1", ".html", ".xml", ".yaml", ".yml":
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// quotaExhaustedText 判定「这次 429 其实是**额度耗尽**」而不是瞬时抖动（round 91 · T76）。
//
// 判据只用**额度字样**（来自上游原文，2026-10-29 实测 reason 分布 409 次里的前两类）：
//
//	· `sign up for free` —— 免费额度用完，上游直接劝注册（要账号侧动作）；
//	· `free usage limit` —— 明写「免费用量已满」。
//
// ⚠️ `provider returned error` **不能**算判据（本轮被既有测试当场纠正，见 TestKindFromStreamError）：
// 它是 429 的通用外壳（`RATE_LIMIT: Provider returned error` 的 message 里没有任何关键词，靠 code 定性）。
// 若把它当额度耗尽，会把**真正的瞬时限流**也判成小时级冷却 ⇒ 免费车道被无故雪藏。
// 反例（必须不命中，仍按瞬时限流走整池冷却）：`too many requests; retry later`、单独的 `Provider returned error`。
//
// 判据纪律：只认「额度」这件事的**专有表述**，不认任何可能出现在普通限流里的通用词。
func quotaExhaustedText(lower string) bool {
	return strings.Contains(lower, "sign up for free") || strings.Contains(lower, "free usage limit")
}

func classify(stderr string, res *Result) Kind {
	// 额度耗尽必须排在**结构化 rate_limited 短路之前**（round 144 · T100）。
	//
	// 现场量化（agent 根全量扫 attempts.jsonl）：**114 行** kind=rate_limited 的 error 正文其实是
	//   `RATE_LIMIT: Sign up for free to continue and explore 500 other models. Takes 2 minutes to sign up.`
	// —— 它带 `RATE_LIMIT:` 前缀，于是上游的结构化字段先被定性成 rate_limited；
	// 而下面那句 `if res.ErrorKind == rate_limited { return rate_limited }` 会在**额度原文判定之前**生效，
	// 把账号级事实（额度用完、要账号侧动作）降级成瞬时限流：短冷却、还会被当成"这个模型不行"的可归因失败。
	//
	// 这与下方注释里写明的次序原则（"额度耗尽…必须先于下面的通用限流判定"）本是一件事，
	// 只是结构化短路把它破掉了 —— 本轮把次序补回来：**专有措辞 > 结构化外壳 > 通用限流词**。
	quotaProbe := strings.ToLower(stderr)
	if res != nil {
		// 文本可能只落在 res.LastError（结构化字段与 stderr 都可能缺一侧，见 pure.go 的双通道分类）。
		quotaProbe += "\n" + strings.ToLower(res.LastError)
	}
	if quotaExhaustedText(quotaProbe) {
		return KindQuotaExhausted
	}
	// 429 必须先判：现场实测 429 的文本里带 "dsh: RATE_LIMIT"，
	// 若不前置，会被下面的 dsh: 分支吞成 boot_failed —— 于是引擎把它当模型故障，
	// 8 路并发 x 失败即换模型，一分钟打爆车道（2026-10-19 事故根因）。
	if res != nil && res.ErrorKind == string(KindRateLimited) {
		return KindRateLimited
	}
	lower := strings.ToLower(stderr)
	// 额度耗尽与瞬时限流同族但处置不同（round 91 · T76）。
	//
	// 判据用**原文特征**而不是「429」：两条原文本身就是上游给出的恢复条件说明书，
	// 而 `too many requests; retry later` 是真正的瞬时抖动。只认「429/rate_limit」会把两者混为一谈，
	// 于是额度耗尽也走「整池冷却后重试」⇒ 每 3 秒空转（现场 409 次、单分钟最多 13 次）。
	if quotaExhaustedText(lower) {
		return KindQuotaExhausted
	}
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
	// 流中断与网络类必须先于「upstream」判定（2026-10-19 现场）：
	// 实测文案是 "our-free-model: upstream stream read failed: aborted" ——
	// 含 "upstream" 子串，若把 upstream 分支放前面会把流中断吞成 upstream_failed，
	// 丢掉「流被切断」这一更精确、更可行动的类别。
	case strings.Contains(lower, "stream read failed"), strings.Contains(lower, "stream_cut"),
		strings.Contains(lower, "stream failed"):
		return KindStreamCut
	case strings.Contains(lower, "etimedout"), strings.Contains(lower, "enotfound"),
		strings.Contains(lower, "eacces"), strings.Contains(lower, "econnrefused"),
		strings.Contains(lower, "econnreset"), strings.Contains(lower, "fetch failed"),
		strings.Contains(lower, "transport"):
		return KindTransport
	// 出口不服务（round 58 · T50）：实测文案「our free model does not serve \"x\" on this egress」，
	// 必须归 not_served —— 它是**出口配置事实**，T44 靠这个 kind 阻止探测把车道洗白。
	case strings.Contains(lower, "does not serve"), strings.Contains(lower, "not_served"):
		return KindNotServed
	case strings.Contains(lower, "upstream"), strings.Contains(lower, "timeout"):
		return KindUpstream
	case strings.Contains(stderr, "dsh:"):
		return KindBoot
	case res.ExitCode != 0:
		return KindExit
	}
	return KindUnknown
}

// modelOverlay 生成任务级模型覆盖层（喂给 DSH 的 --patch）。
//
// **不得写死 reasoningEffort**（2026-10-21 现场缺陷，是「运行中但产出 0」的最后一层阻塞）：
//
//	现场 events.jsonl 第一轮 turn 就结束在：
//	  provider "our-free-model" model "combo:auto-main" does not support
//	  reasoning effort "high"  (code=UNSUPPORTED_REASONING_EFFORT)
//	随后引擎换模型重试，每个新模型又被同一行配置拒一次 —— 因为覆盖层无条件写死
//	reasoningEffort: high，而池里多个免费/组合车道不支持显式推理档位。
//	结果：31 个「可用」模型没有一个能跑完，任务全部 bad_exit。
//
// 正确口径：默认不写该字段（让每个模型用它自己的默认档位）；确实需要时走
// modelOverlayWithEffort 显式传，做到「要用才写、不用不写」。
func modelOverlay(modelID string) string {
	return modelOverlayWithEffort(modelID, "")
}

// modelOverlayWithEffort 生成覆盖层，effort 非空时才写入 reasoningEffort。
//
// effort 取值由调用方决定（例如 "high"/"medium"/"low"）；
// 传空串 = 不干预，用模型默认 —— 这是唯一对所有模型都安全的取值。
func modelOverlayWithEffort(modelID, effort string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		modelID = defaultOverlayModel
	}
	var b strings.Builder
	b.WriteString("- id: agent-default-model\n  config:\n    provider: our-free-model\n    model: ")
	b.WriteString(modelID)
	b.WriteString("\n")
	if e := strings.TrimSpace(effort); e != "" {
		b.WriteString("    reasoningEffort: ")
		b.WriteString(e)
		b.WriteString("\n")
	}
	return b.String()
}

// defaultOverlayModel 是模型 ID 缺失时的兜底模型。
//
// 真正的模型选择由上层路由决定；这里只是「绝不产出非法 YAML / 空模型」的最后一道兜底。
const defaultOverlayModel = "z-ai/glm-5.3"
