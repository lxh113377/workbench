// Package verify 把「模型声明的验收命令」变成**工作台真实执行**的结果。
//
// 设计前提（本机实测）：工作台 profile 禁用了全部工具（OFM sealed 车道在工具
// 调用第二步会卡死），所以模型不能自己跑测试。于是把执行权交给工作台：
// 模型只写命令，工作台在沙箱目录里真跑，退出码/输出落盘，失败回灌返工。
//
// 安全边界（默认拒绝，白名单放行）：
//   - 只允许 python/py/node/pwsh/go 五种解释器；
//   - 命令按空白切分后直接 exec（**不经过 shell**，因此 ; && | > 等不是语法而是普通字符，
//     无法借此串联命令）；
//   - cwd 强制锁定在任务 workspace 内；
//   - 超时强制，输出截断；
//   - 环境变量清洗：剔除 DSH_*、代理、凭据类变量。
package verify

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"workbench/internal/artifact"
	"workbench/internal/model"
	"workbench/internal/winhide"
)

// Outcome 是一条命令的执行结果。
type Outcome struct {
	Cmd      string `json:"cmd"`
	Exit     int    `json:"exit"`
	Expect   int    `json:"expectExit"`
	Pass     bool   `json:"pass"`
	TimedOut bool   `json:"timedOut"`
	Ms       int64  `json:"ms"`
	Stdout   string `json:"stdoutTail"`
	Stderr   string `json:"stderrTail"`
	Skipped  string `json:"skippedReason,omitempty"`
}

// Report 是一轮验收的完整结果。
type Report struct {
	Round    int       `json:"round"`
	WorkDir  string    `json:"workDir"`
	Outcomes []Outcome `json:"outcomes"`
	Pass     bool      `json:"pass"`
	// FailReason 是所有失败项的人读摘要（直接拼进返工 prompt）。
	FailReason string `json:"failReason,omitempty"`
	At         int64  `json:"at"`
}

// Options 控制执行策略。
type Options struct {
	DefaultTimeoutSec int
	MaxOutputBytes    int
	// AllowedInterpreters 覆盖默认白名单（测试用）。
	AllowedInterpreters []string
}

func (o Options) normalized() Options {
	if o.DefaultTimeoutSec <= 0 {
		o.DefaultTimeoutSec = 60
	}
	if o.MaxOutputBytes <= 0 {
		o.MaxOutputBytes = 64 << 10
	}
	if len(o.AllowedInterpreters) == 0 {
		o.AllowedInterpreters = defaultInterpreters()
	}
	return o
}

// defaultInterpreters 是允许的解释器白名单（Windows 下 python 常是别名，故全收）。
func defaultInterpreters() []string {
	base := []string{"python", "python3", "py", "node", "node.exe", "pwsh", "pwsh.exe", "powershell", "powershell.exe", "go", "go.exe"}
	return base
}

// Run 执行一轮验收：依次跑每条 check，全部通过才算 pass。
func Run(ctx context.Context, workDir string, checks []artifact.Check, opt Options) *Report {
	opt = opt.normalized()
	rep := &Report{Round: 1, WorkDir: workDir, At: time.Now().UnixMilli(), Pass: true}
	if len(checks) == 0 {
		rep.Pass = false
		rep.FailReason = "没有任何验收命令（无法证明产物可运行）"
		return rep
	}
	var fails []string
	for i, c := range checks {
		oc := runOne(ctx, workDir, c, opt)
		rep.Outcomes = append(rep.Outcomes, oc)
		if !oc.Pass {
			rep.Pass = false
			reason := oc.Stderr
			if strings.TrimSpace(reason) == "" {
				reason = oc.Stdout
			}
			if oc.Skipped != "" {
				reason = oc.Skipped
			}
			fails = append(fails, fmt.Sprintf("#%d `%s` 退出码 %d（期望 %d）：%s",
				i+1, oc.Cmd, oc.Exit, oc.Expect, model.Truncate(reason, 400)))
		}
	}
	if !rep.Pass {
		rep.FailReason = strings.Join(fails, "\n")
	}
	return rep
}

// runOne 执行一条命令。
func runOne(ctx context.Context, workDir string, c artifact.Check, opt Options) Outcome {
	timeout := time.Duration(c.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(opt.DefaultTimeoutSec) * time.Second
	}
	oc := Outcome{Cmd: c.Cmd, Expect: c.ExpectExit}
	argv, err := splitCommand(c.Cmd)
	if err != nil {
		oc.Skipped = err.Error()
		oc.Exit = -1
		return oc
	}
	if len(argv) == 0 {
		oc.Skipped = "空命令"
		oc.Exit = -1
		return oc
	}
	if !allowed(argv[0], opt.AllowedInterpreters) {
		oc.Skipped = "解释器不在白名单内（只允许 python/node/pwsh/go）: " + argv[0]
		oc.Exit = -1
		return oc
	}
	// 阻止命令参数越出 workspace（round 45 · T30）：抽成纯函数才能被单测钉住（见 argOutOfBounds 注释）。
	if bad := argOutOfBounds(argv[1:]); bad != "" {
		oc.Skipped = "参数含越界路径: " + bad
		oc.Exit = -1
		return oc
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// 开箱即用的硬要求：验收命令绝不能向人要输入。
	// 现场事故（2026-10-19 老大桌面）：模型写了 powershell -Command
	// "Invoke-WebRequest ..."，Windows PowerShell 5.1 默认用 IE 引擎解析页面，
	// 页面含 <script> 时弹出「Security Warning: Script Execution Risk / [Y]Yes [N]No」，
	// 默认答案 N 且会**一直挂着等按键** —— 整条验收链卡死，用户看到的就是弹窗。
	// 两道加固：
	//   1) 给 powershell/pwsh 注入 -NonInteractive（把「等人输入」变成明确报错）；
	//   2) Windows 上以 CREATE_NO_WINDOW + HideWindow 派生（根本没有控制台可显示，
	//      弹窗连出现的载体都没有）。
	argv = forceNonInteractive(argv)
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = workDir
	cmd.Env = sanitizedEnv()
	// 显式切断 stdin：Go 的默认值是 os.DevNull，但这里写出来是为了让
	// 「子进程读不到任何人工输入」成为可见的契约，而不是依赖库默认行为。
	cmd.Stdin = nil
	cmd.SysProcAttr = winhide.SysProcAttr()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &stdout, max: opt.MaxOutputBytes}
	cmd.Stderr = &limitedWriter{w: &stderr, max: opt.MaxOutputBytes}
	start := time.Now()
	runErr := cmd.Run()
	oc.Ms = time.Since(start).Milliseconds()
	oc.Stdout = tailText(stdout.String(), 2000)
	oc.Stderr = tailText(stderr.String(), 2000)

	if runCtx.Err() == context.DeadlineExceeded {
		oc.TimedOut = true
		oc.Exit = -1
		return oc
	}
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			oc.Exit = ee.ExitCode()
		} else {
			oc.Exit = -1
			oc.Skipped = "启动失败: " + runErr.Error()
			return oc
		}
	} else {
		oc.Exit = 0
	}
	oc.Pass = oc.Exit == oc.Expect
	return oc
}

// shellInterps 是需要额外注入「非交互」开关的解释器。
// 其余白名单解释器（python/node/go）在 stdin=DevNull 下读输入会立刻抛错，
// 不存在挂住等人的问题。
var shellInterps = map[string]bool{
	"powershell": true, "powershell.exe": true,
	"pwsh": true, "pwsh.exe": true,
}

// forceNonInteractive 给 PowerShell 系命令补 -NonInteractive（若调用方没写）。
//
// 为什么必须补：交互态下 PowerShell 遇到确认提示（IE 引擎的 Script Execution
// Risk、Read-Host、Get-Credential、-Confirm 等）会**阻塞等待人工按键**。
// 加了它，同类情形改为抛 PSInvalidOperationException 直接返回失败退出码 ——
// 失败可以被回灌成返工理由，卡死不行。
//
// 插入位置有讲究：必须在 -File/-Command 之前，否则会把它当成脚本参数。
// 调用方已显式写了 -NonInteractive（或 /NonInteractive 形式）时保持原样，
// 不重复注入 —— 尊重显式意图，也避免命令行里出现两个同名开关。
func forceNonInteractive(argv []string) []string {
	if len(argv) == 0 {
		return argv
	}
	if !shellInterps[strings.ToLower(baseName(argv[0]))] {
		return argv
	}
	for _, a := range argv[1:] {
		low := strings.ToLower(a)
		if low == "-noninteractive" || low == "/noninteractive" {
			return argv // 调用方已写，保持原样
		}
	}
	out := []string{argv[0], "-NonInteractive"}
	out = append(out, argv[1:]...)
	return out
}

// baseName 取可执行文件名（去掉可能的路径前缀，跨平台分隔符都认）。
func baseName(p string) string {
	p = filepath.Base(p)
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		p = p[i+1:]
	}
	return p
}

// splitCommand 按空白切分命令（不做 shell 解析，因此无法注入 ; && | 等）。
// 支持单/双引号包裹的含空格参数。
func splitCommand(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("空命令")
	}
	var out []string
	var cur strings.Builder
	var quote rune
	inToken := false
	// escaped：上一个字符是反斜杠（转义前缀）。
	//
	// 为什么需要（2026-10-19 缺陷，docs/缺陷-验收命令反斜杠污染-2026-10-19.md）：
	// 模型的 manifest cmd 按 JSON 规则本应写 \"，若写成 \\"（过度转义），反转义后就剩下
	// 一个字面 \ 紧跟引号。旧实现不识别 \"，于是 \ 残留进 argv，pwsh 收到 `\if ...`
	// 报「术语不识别」—— 该任务的真实验收**永远不可能通过**，反复返工直到 repairRounds 耗尽，
	// 并把「模型转义问题」误报成「产物不可运行」。
	var escaped bool
	for _, r := range s {
		if escaped {
			// 转义语义：反斜杠后的字符按字面写入（含引号自身、反斜杠自身）。
			// 关键：这一分支必须在 switch **之前**，否则引号内的 \ 会先被
			// `case quote != 0` 当普通字符吃掉，转义永远不生效（2026-10-23 实测：
			// 首版把 escaped 检查放前面但把 `case r == '\\'` 留在 switch 里，
			// 结果引号内的 \" 仍被切碎）。
			cur.WriteRune(r)
			inToken = true
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			// Windows/POSIX 通用：反斜杠转义下一个字符。单引号内不转义（POSIX 语义，
			// 与 shell 一致：'...' 里的反斜杠是字面量）。
			// 放在 switch 之前：引号内、引号外都要能转义。
			escaped = true
			continue
		}
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inToken = true
		case r == ' ' || r == '\t' || r == '\r' || r == '\n':
			if inToken {
				out = append(out, cur.String())
				cur.Reset()
				inToken = false
			}
		default:
			cur.WriteRune(r)
			inToken = true
		}
	}
	if escaped {
		// 行尾单个反斜杠：按字面保留（不报错 —— 命令可能是被截断的，但那不是本函数的职责）。
		cur.WriteRune('\\')
		inToken = true
	}
	if quote != 0 {
		return nil, fmt.Errorf("引号未闭合")
	}
	if inToken {
		out = append(out, cur.String())
	}
	return out, nil
}

// allowed 判断解释器是否白名单内（按 basename 比较，忽略 .exe）。
func allowed(exe string, whitelist []string) bool {
	base := strings.ToLower(filepath.Base(exe))
	base = strings.TrimSuffix(base, ".exe")
	for _, w := range whitelist {
		wl := strings.ToLower(strings.TrimSuffix(w, ".exe"))
		if base == wl {
			return true
		}
	}
	return false
}

// sanitizedEnv 返回清洗后的环境变量：剔除 DSH_*、代理与凭据类变量。
func sanitizedEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		eq := strings.Index(kv, "=")
		if eq <= 0 {
			continue
		}
		k := kv[:eq]
		upper := strings.ToUpper(k)
		if strings.HasPrefix(upper, "DSH_") ||
			strings.Contains(upper, "PROXY") ||
			strings.Contains(upper, "TOKEN") ||
			strings.Contains(upper, "SECRET") ||
			strings.Contains(upper, "PASSWORD") ||
			strings.Contains(upper, "API_KEY") {
			continue
		}
		out = append(out, kv)
	}
	// 保证解释器能找到（Go 写的子进程默认继承 PATH，此处再兜底）
	if runtime.GOOS == "windows" {
		out = append(out, "PYTHONIOENCODING=utf-8")
		out = append(out, "PYTHONUTF8=1")
	}
	return out
}

// limitedWriter 只保留前 max 字节，防止模型产物刷爆内存。
type limitedWriter struct {
	w   *bytes.Buffer
	max int
}

// Write 遵守 io.Writer 契约：**永远报告"全部收下"（返回 len(p)）**，超限部分直接丢弃。
//
// 为什么必须这样（round 143 修复，线上证据驱动）：os/exec 用 io.Copy 把子进程输出搬进本 Writer，
// 而 io.Copy 内部有 `if nr != nw { err = io.ErrShortWrite }` —— 只要 Write 返回**短计数且 err=nil**，
// 就会被判成 ErrShortWrite，接着 cmd.Run() 返回它、verify 又把它记成「启动失败」。
// 旧实现正是"截断后返回 remain、err 返回 nil"，于是线上出现：
//
//	exit=-1 ｜ skipped="启动失败: short write" ｜ stdout 是超 64KB 的文件列表
//
// 两条后果：① 诊断误导（看着像解释器起不来，实际是输出洪泛）；
//
//	② 本该 exit 0 的验收命令只要刷出 > MaxOutputBytes 输出就被判失败 ⇒ **验收假阴性**。
//
// 截断上限的本意（防刷爆内存）不变：只保留前 max 字节，其余丢弃，但计数必须报全额。
func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.w.Len() >= l.max {
		return len(p), nil
	}
	remain := l.max - l.w.Len()
	if len(p) > remain {
		if _, err := l.w.Write(p[:remain]); err != nil {
			return 0, err
		}
		return len(p), nil // 关键：报全额，超限部分丢弃 —— 短计数会触发 io.Copy 的 ErrShortWrite
	}
	return l.w.Write(p)
}

// tailText 取文本尾部（报错通常在尾部）。
func tailText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	cut := s[len(s)-n:]
	return "..." + cut
}

// argOutOfBounds 返回第一个「越出 workspace」的参数（无越界返回空串）（round 45 · T30）。
//
// 为什么单独一个纯函数：原判据内联在 runOne 里，只能靠「真跑一条命令」验证 —— 而它的缺陷恰恰是
// **把合法参数当越界**：`go test ./...`（最典型的验收命令）里的 `./...` 含 `..`，原判据一律 skip，
// 于是每次验收都落 skippedReason、任务白白判失败。
//
// 口径（第一版写错、被单测当场抓出，如实记）：`filepath.Clean("./...")` 得到 `...`，而 `HasPrefix("...", "..")` 为真
// ⇒ 用「前缀判 `..`」仍然误杀 `go test ./...`。真正要对的是**路径分量**：只有整段等于 `..`（父目录引用）才算越界，
// `...`（Go/go-tool 的通配）不是。因此按分量判：`./...` ⇒ 分量 ["..."] 放行；`../../etc` ⇒ ["..","..","etc"] 拒绝；
// `a/../b` ⇒ Clean 成 `b` 放行；绝对路径（含盘符）一律拒绝。
func argOutOfBounds(args []string) string {
	for _, a := range args {
		c := filepath.ToSlash(filepath.Clean(a))
		if strings.HasPrefix(c, "/") || filepath.IsAbs(a) || (len(a) > 1 && a[1] == ':') {
			return a
		}
		for _, part := range strings.Split(c, "/") {
			if part == ".." {
				return a
			}
		}
	}
	return ""
}
