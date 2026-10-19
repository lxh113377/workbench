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
	// 阻止命令参数里出现绝对路径越界（保守：只允许 workspace 内的相对引用）
	for _, a := range argv[1:] {
		if strings.Contains(a, "..") || strings.HasPrefix(a, "/") || (len(a) > 1 && a[1] == ':') {
			oc.Skipped = "参数含越界路径: " + a
			oc.Exit = -1
			return oc
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = workDir
	cmd.Env = sanitizedEnv()
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
	for _, r := range s {
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

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.w.Len() >= l.max {
		return len(p), nil
	}
	remain := l.max - l.w.Len()
	if len(p) > remain {
		p = p[:remain]
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
