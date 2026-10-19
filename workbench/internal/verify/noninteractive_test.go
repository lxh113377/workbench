package verify

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workbench/internal/artifact"
)

// TestForceNonInteractive 锁住注入规则本身（纯函数，不依赖本机 shell）。
func TestForceNonInteractive(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"powershell -Command 补 -NonInteractive",
			[]string{"powershell", "-Command", "exit 0"},
			[]string{"powershell", "-NonInteractive", "-Command", "exit 0"}},
		{"pwsh -File 同样补",
			[]string{"pwsh.exe", "-File", "a.ps1"},
			[]string{"pwsh.exe", "-NonInteractive", "-File", "a.ps1"}},
		{"调用方已写过就不重复注入",
			[]string{"powershell", "-NonInteractive", "-Command", "exit 0"},
			[]string{"powershell", "-NonInteractive", "-Command", "exit 0"}},
		{"大小写混写也识别（-NonInteractive 的常见变体）",
			[]string{"PowerShell", "-noninteractive", "-File", "a.ps1"},
			[]string{"PowerShell", "-noninteractive", "-File", "a.ps1"}},
		{"python 不动它", []string{"python", "-c", "print(1)"}, []string{"python", "-c", "print(1)"}},
		{"go 不动它", []string{"go", "test"}, []string{"go", "test"}},
		{"空命令不炸", nil, nil},
	}
	for _, c := range cases {
		got := forceNonInteractive(c.in)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Fatalf("%s: 得到 %v，期望 %v", c.name, got, c.want)
		}
	}
}

// TestPromptingCommandDoesNotHang 是这次桌面事故的回归测试。
//
// 现场：模型写了 powershell -Command "Invoke-WebRequest <页面>"，而 Windows
// PowerShell 5.1 默认用 IE 引擎解析页面；页面含 <script> 时弹出
// 「Security Warning: Script Execution Risk … [Y]es [N]o (default is "N")」，
// 交互态下会**一直挂着等人工按键**，整条验收链卡死，用户看到的就是弹窗。
//
// 加固后必须在秒级返回明确结果：失败可以回灌成返工理由，卡死不行。
// 用 -File 传脚本文件而不是内联 -Command，避开跨 shell 的引号二次解析。
func TestPromptingCommandDoesNotHang(t *testing.T) {
	if _, err := lookShell(); err != nil {
		t.Skipf("本机无 powershell/pwsh，跳过弹窗回归：%v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head><script>var x=1;</script></head><body>hi</body></html>"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	script := filepath.Join(dir, "probe.ps1")
	body := "param([string]$U)\n" +
		"try { $r = Invoke-WebRequest -Uri $U -TimeoutSec 20; exit 0 } catch { exit 3 }\n"
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	// 相对引用：cwd 已锁在 workspace 内，符合 verify 的越界约束。
	cmd := "powershell -File probe.ps1 " + srv.URL

	type res struct {
		oc  Outcome
		dur time.Duration
	}
	ch := make(chan res, 1)
	go func() {
		t0 := time.Now()
		rep := Run(testCtx(90*time.Second), dir,
			[]artifact.Check{{Cmd: cmd, ExpectExit: 0, TimeoutSec: 40}},
			Options{DefaultTimeoutSec: 40})
		if len(rep.Outcomes) != 1 {
			ch <- res{Outcome{Skipped: "无结果"}, time.Since(t0)}
			return
		}
		ch <- res{rep.Outcomes[0], time.Since(t0)}
	}()

	select {
	case got := <-ch:
		if got.oc.TimedOut {
			t.Fatalf("命令挂到超时（耗时 %s）—— 仍在等人按键，加固未生效：%s",
				got.dur.Round(time.Second), got.oc.Stderr)
		}
		if got.dur > 30*time.Second {
			t.Fatalf("耗时 %s 过长，疑似阻塞在交互提示上", got.dur.Round(time.Second))
		}
		if got.oc.Skipped != "" {
			t.Fatalf("命令被跳过而非执行，回归没测到真路径：%s", got.oc.Skipped)
		}
		t.Logf("秒级返回：exit=%d pass=%v 耗时=%s stderrTail=%.160s",
			got.oc.Exit, got.oc.Pass, got.dur.Round(time.Millisecond), got.oc.Stderr)
	case <-time.After(70 * time.Second):
		t.Fatal("验收命令 70s 未返回 —— 弹窗卡住人工输入，回归未修复")
	}
}

// TestInjectionReachesChildProcess 是机制级断言：不依赖本机是否有交互桌面，
// 直接看被派生的 PowerShell 自己的命令行里有没有 -NonInteractive。
//
// 为什么需要它：上一例在「无交互桌面」的环境里，即使不加固也不会挂住，
// 于是它可能因为环境而假通过。本例锁的是加固本身是否真的生效。
func TestInjectionReachesChildProcess(t *testing.T) {
	if _, err := lookShell(); err != nil {
		t.Skipf("本机无 PowerShell：%v", err)
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "cmdline.ps1")
	// 把自身进程的命令行写进文件（PowerShell 的 $PID + WMI 查询）。
	body := "$ErrorActionPreference=\"SilentlyContinue\"\n" +
		"$cl = (Get-CimInstance Win32_Process -Filter \"ProcessId=$PID\").CommandLine\n" +
		"Set-Content -Path cmdline.txt -Value $cl -Encoding UTF8\n" +
		"exit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rep := Run(testCtx(60*time.Second), dir,
		[]artifact.Check{{Cmd: "powershell -File cmdline.ps1", ExpectExit: 0, TimeoutSec: 30}},
		Options{DefaultTimeoutSec: 30})
	if len(rep.Outcomes) != 1 || !rep.Outcomes[0].Pass {
		t.Fatalf("探测脚本未成功执行：%+v", rep.Outcomes)
	}
	data, err := os.ReadFile(filepath.Join(dir, "cmdline.txt"))
	if err != nil {
		t.Fatal(err)
	}
	line := string(data)
	if !strings.Contains(strings.ToLower(line), "-noninteractive") {
		t.Fatalf("注入没到达子进程，命令行实为：%s", strings.TrimSpace(line))
	}
	// 同时确认 -File 仍在（注入位置不能把脚本参数挤掉）
	if !strings.Contains(strings.ToLower(line), "-file") {
		t.Fatalf("注入破坏了原参数结构：%s", strings.TrimSpace(line))
	}
}
