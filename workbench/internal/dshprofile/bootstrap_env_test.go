package dshprofile

// bootstrap_env_test.go 覆盖「工作台运行环境准备」的可离线判据
// （2026-10-22 新增：本包此前只有 cordis.patch.yml 文案的 3 条判据，
//  环境构建链 LocateDSH/ensureOFMData/ensureProfile/Bootstrap 零覆盖，
//  CI 覆盖率门禁实测 1.4% < 阈值 2.5% 连续判红 —— 补的是真判据，不是降阈值）。
//
// 可测性设计：所有外部依赖（DSH 安装路径 / OFM 插件目录 / 用户主目录）都走
// 环境变量注入，测试用 t.Setenv + 临时目录构造，不碰真实 ~/.dsh，也不要求 CI
// runner 装有 DSH Desktop。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"workbench/internal/config"
)

// fakeDSH 在临时目录里搭出一套最小的「DSH 安装 + OFM 插件」结构并注入环境变量。
func fakeDSH(t *testing.T) (exe, ofm string) {
	t.Helper()
	root := t.TempDir()
	exe = filepath.Join(root, "DSH Desktop.exe")
	if err := os.WriteFile(exe, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	// LocateDSH 的 CLI 判据：exe 同级的 resources/app/lib/desktop-cli.js 必须是文件
	cli := filepath.Join(root, "resources", "app", "lib", "desktop-cli.js")
	if err := os.MkdirAll(filepath.Dir(cli), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cli, []byte("// fake cli"), 0o644); err != nil {
		t.Fatal(err)
	}
	// linkOFM 的判据：ofmSourceDir 命中目录下必须有 index.js
	ofm = filepath.Join(root, "ofm")
	if err := os.MkdirAll(ofm, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ofm, "index.js"), []byte("export default {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DSH_DESKTOP_EXE", exe)
	t.Setenv("DSH_OFM_DIR", ofm)
	// 把「用户主目录候选」隔离掉：ofmSourceDir 的回退候选在 USERPROFILE/HOME 之下，
	// 指向空临时目录后，未命中 DSH_OFM_DIR 时的搜索结果可判定（开发机上真实 ~/.dsh 存在）。
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USERPROFILE", home)
	if runtime.GOOS != "windows" {
		t.Setenv("HOME", home)
	}
	return exe, ofm
}

func TestLocateDSHRespectsEnvOverride(t *testing.T) {
	exe, _ := fakeDSH(t)
	gotExe, gotCLI, err := LocateDSH()
	if err != nil {
		t.Fatalf("注入完整安装结构后 LocateDSH 仍失败: %v", err)
	}
	if gotExe != exe {
		t.Fatalf("必须优先采用 DSH_DESKTOP_EXE 注入的路径，got %q want %q", gotExe, exe)
	}
	if !strings.HasSuffix(gotCLI, filepath.Join("resources", "app", "lib", "desktop-cli.js")) {
		t.Fatalf("CLI 入口推导错误: %q", gotCLI)
	}
}

// TestLocateDSHFailsWhenCLIFileMissing 钉死「装了但残缺」的处置：desktop-cli.js 缺失
// 必须**报错点名**，而不是静默回退到候选路径 —— 工作台后续所有 headless 调用都靠这个 CLI。
func TestLocateDSHFailsWhenCLIFileMissing(t *testing.T) {
	exe, _ := fakeDSH(t)
	cli := filepath.Join(filepath.Dir(exe), "resources", "app", "lib", "desktop-cli.js")
	if err := os.Remove(cli); err != nil {
		t.Fatal(err)
	}
	_, _, err := LocateDSH()
	if err == nil {
		t.Fatal("CLI 文件缺失时 LocateDSH 必须报错")
	}
	if !strings.Contains(err.Error(), "desktop-cli.js") {
		t.Fatalf("错误必须点名缺失文件，got %q", err.Error())
	}
}

func TestResolveFromShim(t *testing.T) {
	shim := filepath.Join(t.TempDir(), "dsh.cmd")
	body := "@echo off\r\nendlocal & set PATH=%~dp0;%PATH%\r\n\"D:\\Somewhere\\DSH Desktop\\DSH Desktop.exe\" %*\r\n"
	if err := os.WriteFile(shim, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveFromShim(shim)
	if err != nil {
		t.Fatalf("解析 shim 失败: %v", err)
	}
	if got != `D:\Somewhere\DSH Desktop\DSH Desktop.exe` {
		t.Fatalf("解析结果错误: %q", got)
	}
	// 负向：不含 exe 行的 shim 必须报错而不是返回空串冒充成功
	bad := filepath.Join(t.TempDir(), "bad.cmd")
	if err := os.WriteFile(bad, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveFromShim(bad); err == nil {
		t.Fatal("无 exe 行时必须报错")
	}
}

// TestResolveFromShimSkipsEarlierQuotedPaths 钉死「按行扫描」这件事本身：shim 里常有多条
// 带引号的路径（先设 PATH 的行、node launcher 行），只有按行切分才能保证命中的是含
// "DSH Desktop.exe" 的那一行。旧实现按字面量反斜杠+n 切分（整文件当一行），首引号会
// 落进更早那行的引号里 → 解析出错误路径，本判据当时必判红。
func TestResolveFromShimSkipsEarlierQuotedPaths(t *testing.T) {
	shim := filepath.Join(t.TempDir(), "dsh.cmd")
	body := "@echo off" + "\r\n" + "set \"C:\\\\Junk First.exe\"=1" + "\r\n" + "endlocal & set PATH=%~dp0" + "\r\n" + "\"C:\\\\Real\\\\DSH Desktop\\\\DSH Desktop.exe\" %*" + "\r\n"
	if err := os.WriteFile(shim, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveFromShim(shim)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	want := "C:\\\\Real\\\\DSH Desktop\\\\DSH Desktop.exe"
	if got != want {
		t.Fatalf("必须跳过更早的引号路径、取含 DSH Desktop.exe 的那一行，got %q want %q", got, want)
	}
}
func TestOfmSourceDirPrefersEnv(t *testing.T) {
	_, ofm := fakeDSH(t)
	if got := ofmSourceDir(); got != ofm {
		t.Fatalf("DSH_OFM_DIR 必须优先，got %q want %q", got, ofm)
	}
}

// TestOfmSourceDirRequiresIndexJS 钉死「目录存在但未安装插件」的识别：
// 判据是 index.js 文件存在，不是目录存在。
func TestOfmSourceDirRequiresIndexJS(t *testing.T) {
	fakeDSH(t)
	empty := filepath.Join(t.TempDir(), "empty-ofm")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DSH_OFM_DIR", empty)
	if got := ofmSourceDir(); got != "" {
		t.Fatalf("缺 index.js 的候选目录不得被当成已安装，got %q", got)
	}
}

func TestEnsureOFMDataWritesQualitySettings(t *testing.T) {
	fakeDSH(t)
	root := t.TempDir()
	layout, err := config.BuildLayout(root, "")
	if err != nil {
		t.Fatal(err)
	}
	info := &Info{Warnings: []string{}}
	if err := ensureOFMData(layout, info); err != nil {
		t.Fatalf("ensureOFMData 失败: %v", err)
	}
	settingsPath := filepath.Join(layout.DSHHome, "our-free-model", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings.json 未落盘: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("settings.json 不是合法 JSON: %v", err)
	}
	// 质量优先铁律：tokenSaver 的电报体提示词必须关（开着会毁掉高质量产出）
	ts, _ := s["tokenSaver"].(map[string]any)
	prompts, _ := ts["prompts"].(map[string]any)
	if prompts["caveman"] != "off" || prompts["ponytail"] != "off" {
		t.Fatalf("caveman/ponytail 必须为 off，got %v", prompts)
	}
	if s["defaultMaxTokens"] != float64(32768) {
		t.Fatalf("defaultMaxTokens 必须为 32768，got %v", s["defaultMaxTokens"])
	}
	// 幂等：已存在时不得重写（用户在隔离目录里的手改必须保留）
	if err := os.WriteFile(settingsPath, []byte("{\"sentinel\":true}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureOFMData(layout, info); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(settingsPath)
	if !strings.Contains(string(again), "sentinel") {
		t.Fatal("已存在的 settings.json 被重写了（幂等契约破坏）")
	}
}

func TestWriteIfChangedAndMustJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.json")
	if err := writeIfChanged(p, []byte("A")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "A" {
		t.Fatalf("首次写入失败: %q", got)
	}
	// 相同内容必须跳过重写（防 updatedAt 抖动引发无谓落盘）
	st1, _ := os.Stat(p)
	if err := writeIfChanged(p, []byte("A")); err != nil {
		t.Fatal(err)
	}
	st2, _ := os.Stat(p)
	if !st1.ModTime().Equal(st2.ModTime()) {
		t.Fatal("内容未变却重写了文件")
	}
	if err := writeIfChanged(p, []byte("B")); err != nil {
		t.Fatal(err)
	}
	if got, _ = os.ReadFile(p); string(got) != "B" {
		t.Fatal("内容变化后必须重写")
	}
	if b := mustJSON(map[string]any{"k": 1}); !strings.Contains(string(b), `"k": 1`) {
		t.Fatalf("mustJSON 输出错误: %s", b)
	}
}

func TestCopyDirNested(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")
	deep := filepath.Join(src, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "f.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir 失败: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "a", "b", "f.txt"))
	if err != nil {
		t.Fatalf("嵌套文件未复制: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("内容不符: %q", got)
	}
}

// TestBootstrapEndToEnd 是本包的主干判据：注入完整环境后 Bootstrap 必须
// 幂等地把「隔离 DSH_HOME + profile 四件套 + OFM 挂载」全部备齐。
func TestBootstrapEndToEnd(t *testing.T) {
	fakeDSH(t)
	root := t.TempDir()
	layout, err := config.BuildLayout(root, "")
	if err != nil {
		t.Fatal(err)
	}
	info, err := Bootstrap(layout)
	if err != nil {
		t.Fatalf("Bootstrap 失败: %v（warnings=%v）", err, info.Warnings)
	}
	if !info.ProfileReady {
		t.Fatal("ProfileReady 必须为 true")
	}
	if !info.OFMLinked {
		t.Fatal("OFMLinked 必须为 true（junction 或复制退化都必须成功）")
	}
	for _, f := range []string{"package.json", "cordis.yml", "pnpm-workspace.yaml", "cordis.patch.yml"} {
		p := filepath.Join(info.ProfileDir, f)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("profile 文件未生成 %s: %v", f, err)
		}
	}
	patch, _ := os.ReadFile(filepath.Join(info.ProfileDir, "cordis.patch.yml"))
	for _, must := range []string{"- id: plan-mode", "disabled: true", "only: []", "policy: never"} {
		if !strings.Contains(string(patch), must) {
			t.Fatalf("落盘的 cordis.patch.yml 缺少 %q（落盘链与 profilePatch 必须同源）", must)
		}
	}
	// 幂等：第二次 Bootstrap 不得报错（7x24 引擎每轮启动都会调它）
	if _, err := Bootstrap(layout); err != nil {
		t.Fatalf("二次 Bootstrap 失败: %v", err)
	}
}
