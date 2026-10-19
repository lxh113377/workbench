// Package dshprofile 负责准备并校验工作台专用的 DSH 运行环境。
//
// 关键设计（来自本机实测，详见 memory/03-tech-stack.md）：
//  1. 使用**隔离的 DSH_HOME**（<root>/runtime/dsh-home），绝不污染用户 ~/.dsh。
//  2. profile 目录名必须是 web-desktop：our-free-model 的 sealed 协付车道按
//     profileContext.name 白名单放行（desktop/tauri/web/web-desktop）。
//  3. profile 的 bundles 只能是 base + headless + our-free-model；
//     绝不能挂 dsh-web-app —— 它会起常驻服务器让 headless 永不退出。
//  4. our-free-model 用 junction 指向 desktop profile 里的已安装副本，
//     使桌面端升级插件后工作台自动跟随，避免版本漂移。
package dshprofile

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"workbench/internal/config"
)

// ProfileName 是工作台专用 profile 名（OFM sealed 白名单要求，不可改）。
const ProfileName = "web-desktop"

// Bundles 是工作台 profile 的 bundle 列表。
var Bundles = []string{
	"@deepseek-ai/dsh-base",
	"@deepseek-ai/dsh-headless",
	"dsh-our-free-model",
}

// Info 描述探测结果。
type Info struct {
	Exe          string   `json:"exe"`
	CLI          string   `json:"cli"`
	DSHHome      string   `json:"dshHome"`
	ProfileDir   string   `json:"profileDir"`
	ProfileReady bool     `json:"profileReady"`
	OFMLinked    bool     `json:"ofmLinked"`
	SealedLane   bool     `json:"sealedLane"`
	Warnings     []string `json:"warnings"`
	CheckedAt    int64    `json:"checkedAt"`
}

// Bootstrap 幂等地建立工作台运行环境，返回探测信息。
// 任何一步缺失都会被补齐；无法补齐时返回带 Warnings 的信息与 nil 之外的错误。
func Bootstrap(layout config.Layout) (*Info, error) {
	info := &Info{CheckedAt: time.Now().UnixMilli(), DSHHome: layout.DSHHome}
	info.Warnings = []string{}

	exe, cli, err := LocateDSH()
	if err != nil {
		return info, err
	}
	info.Exe, info.CLI = exe, cli

	if err := os.MkdirAll(layout.DSHHome, 0o755); err != nil {
		return info, fmt.Errorf("创建隔离 DSH_HOME 失败: %w", err)
	}
	if err := ensureOFMData(layout, info); err != nil {
		info.Warnings = append(info.Warnings, "OFM 数据目录初始化不完整: "+err.Error())
	}
	if err := ensureProfile(layout, info); err != nil {
		return info, err
	}
	info.ProfileDir = profileDir(layout)
	info.ProfileReady = true
	return info, nil
}

// LocateDSH 解析 DSH Desktop 的可执行文件与 CLI 入口。
// 解析顺序：配置显式路径 -> 注册表/已知安装路径 -> PATH 中的 dsh shim。
func LocateDSH() (exe, cli string, err error) {
	candidates := []string{}
	if p := strings.TrimSpace(os.Getenv("DSH_DESKTOP_EXE")); p != "" {
		candidates = append(candidates, p)
	}
	candidates = append(candidates,
		`D:\deepseek desktop\DSH Desktop\DSH Desktop.exe`,
		filepath.Join(os.Getenv("ProgramFiles"), "DeepSeek Harness Desktop", "DSH Desktop.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "DSH Desktop", "DSH Desktop.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "DSH Desktop", "DSH Desktop.exe"),
	)
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, statErr := os.Stat(c); statErr == nil && !st.IsDir() {
			exe = c
			break
		}
	}
	if exe == "" {
		if p, lookErr := exec.LookPath("dsh.cmd"); lookErr == nil {
			// shim 里带着真实 exe 路径，解析失败则放弃
			if resolved, parseErr := resolveFromShim(p); parseErr == nil {
				exe = resolved
			}
		}
	}
	if exe == "" {
		return "", "", fmt.Errorf("未找到 DSH Desktop，可用环境变量 DSH_DESKTOP_EXE 指定路径")
	}
	cli = filepath.Join(filepath.Dir(exe), "resources", "app", "lib", "desktop-cli.js")
	if st, statErr := os.Stat(cli); statErr != nil || st.IsDir() {
		return "", "", fmt.Errorf("未找到 desktop-cli.js（DSH Desktop 安装可能不完整）: %s", cli)
	}
	return exe, cli, nil
}

func resolveFromShim(shim string) (string, error) {
	data, err := os.ReadFile(shim)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "DSH Desktop.exe") {
			continue
		}
		if i := strings.Index(line, `"`); i >= 0 {
			line = line[i+1:]
			if j := strings.Index(line, ".exe\""); j >= 0 {
				return line[:j+len(".exe")], nil
			}
		}
	}
	return "", fmt.Errorf("无法从 shim 解析出 DSH Desktop 路径")
}

func profileDir(layout config.Layout) string {
	return filepath.Join(layout.DSHHome, "profiles", ProfileName)
}

func ensureOFMData(layout config.Layout, info *Info) error {
	dir := filepath.Join(layout.DSHHome, "our-free-model")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	settingsPath := filepath.Join(dir, "settings.json")
	if _, err := os.Stat(settingsPath); err == nil {
		return nil
	}
	// 质量优先：关闭 tokenSaver 的提示词压缩（caveman/ponytail），
	// 否则模型会被要求用"电报体"回答，直接毁掉高质量产出。
	settings := map[string]any{
		"version":              1,
		"enabled":              true,
		"exposeRegionModels":   true,
		"hiddenModelIds":       []string{},
		"hideFreeModels":       false,
		"hideEacModels":        false,
		"probeIntervalMinutes": 60,
		"forward":              map[string]any{"enabled": false, "host": "127.0.0.1", "port": 18899, "lan": map[string]any{"enabled": false, "port": 0}},
		"egress":               map[string]any{"enabled": false, "mode": "subscription", "url": "", "mihomoPath": ""},
		"chanGateway":          map[string]any{"relay": map[string]any{"enabled": false, "host": "127.0.0.1", "port": 18326, "key": ""}},
		"defaultMaxTokens":     32768,
		"streamRecovery":       true,
		"tokenSaver":           map[string]any{"enabled": true, "rtk": true, "prompts": map[string]any{"caveman": "off", "ponytail": "off"}},
		"aggregate":            map[string]any{"enabled": false, "providers": map[string]any{}},
		"combos":               []any{},
		"localOptimize":        map[string]any{"enabled": true, "quota": true, "prefix": true, "title": false, "suggestion": false, "filepath": true},
		"notifyOs":             false,
		"updateCheckHours":     0,
	}
	if err := config.WriteJSONAtomic(settingsPath, settings); err != nil {
		return err
	}
	// 复制账号态（只读取用户目录，不写入用户目录）
	userHome, _ := os.UserHomeDir()
	srcDir := filepath.Join(userHome, ".dsh", "our-free-model")
	for _, name := range []string{"eac-user.json", "catalog.json", "availability.json", "feed.json"} {
		src := filepath.Join(srcDir, name)
		dst := filepath.Join(dir, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		data, readErr := os.ReadFile(src)
		if readErr != nil {
			continue
		}
		_ = config.WriteFileAtomic(dst, data)
	}
	return nil
}

func ensureProfile(layout config.Layout, info *Info) error {
	dir := profileDir(layout)
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		return err
	}
	// package.json
	pkg := map[string]any{
		"name":         "dsh-profile-" + ProfileName,
		"private":      true,
		"dependencies": map[string]any{},
		"dsh":          map[string]any{"profile": map[string]any{"bundles": Bundles}},
	}
	if err := writeIfChanged(filepath.Join(dir, "package.json"), mustJSON(pkg)); err != nil {
		return err
	}
	if err := writeIfChanged(filepath.Join(dir, "cordis.yml"), []byte("[]\n")); err != nil {
		return err
	}
	ws := "packages:\n  - .\n\nnodeLinker: hoisted\nautoInstallPeers: false\n"
	if err := writeIfChanged(filepath.Join(dir, "pnpm-workspace.yaml"), []byte(ws)); err != nil {
		return err
	}
	if err := writeIfChanged(filepath.Join(dir, "cordis.patch.yml"), []byte(profilePatch())); err != nil {
		return err
	}
	if err := linkOFM(layout, info); err != nil {
		return err
	}
	return nil
}

func profilePatch() string {
	lines := []string{
		"# WorkBench 专用 profile 用户层（由 workbench.exe 维护，请勿手改）",
		"# 说明：本层叠在 dsh-base + dsh-headless + dsh-our-free-model 之上。",
		"- id: agent-default-model",
		"  config:",
		"    provider: our-free-model",
		"    model: z-ai/glm-5.3",
		"    reasoningEffort: high",
		"",
		"# 隔离锋的全局人设/P0 规则注入：任务会话不应背 184 个技能与大段铁律。",
		"- id: agent-instructions",
		"  disabled: true",
		"",
		"- id: skill",
		"  disabled: true",
		"- id: skill-filesystem",
		"  disabled: true",
		"- id: tool-skill",
		"  disabled: true",
		"- id: tool-ralph",
		"  disabled: true",
		"",
		"# ── 工具层：our-free-model 的 sealed 车现在「工具调用后的第二步」会卡死",
		"# （实测：step_start(step2) 后无任何事件）。为保证 7x24 稳定产出，",
		"# 工作台一律让模型**一步直接输出完整成果文本**，禁用全部文件/命令/检索工具。",
		"- id: tools",
		"  config:",
		"    mode: native",
		"    only: []",
		"- id: tool-fs",
		"  disabled: true",
		"- id: tool-fs-search",
		"  disabled: true",
		"- id: tool-pwsh",
		"  disabled: true",
		"- id: tool-bash",
		"  disabled: true",
		"- id: tool-jobs",
		"  disabled: true",
		"- id: tool-subagent",
		"  disabled: true",
		"- id: tool-subagent-fork",
		"  disabled: true",
		"- id: tool-subagent-control",
		"  disabled: true",
		"- id: tool-subagent-list-agents",
		"  disabled: true",
		"- id: tool-workflow",
		"  disabled: true",
		"- id: tool-todo",
		"  disabled: true",
		"- id: tool-web",
		"  disabled: true",
		"- id: tool-present",
		"  disabled: true",
		"- id: tool-ask",
		"  disabled: true",
		"- id: tool-goal",
		"  disabled: true",
		"",
		"# 无人值守：审批策略固定为 never。",
		"- id: approval",
		"  config:",
		"    policy: never",
		"",
	}
	return strings.Join(lines, "\n")
}

func linkOFM(layout config.Layout, info *Info) error {
	dir := profileDir(layout)
	link := filepath.Join(dir, "node_modules", "dsh-our-free-model")
	target := ofmSourceDir()
	if target == "" {
		info.Warnings = append(info.Warnings, "未找到已安装的 dsh-our-free-model，请先在 DSH Desktop 中安装该插件")
		return fmt.Errorf("未找到 dsh-our-free-model 安装目录")
	}
	if st, err := os.Stat(link); err == nil && st.IsDir() {
		info.OFMLinked = true
		return nil
	}
	_ = os.RemoveAll(link)
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
		cmd.SysProcAttr = hiddenProcAttr()
		if out, err := cmd.CombinedOutput(); err != nil {
			// junction 失败时退化为整目录复制，保证可用性优先
			if cpErr := copyDir(target, link); cpErr != nil {
				return fmt.Errorf("挂载 our-free-model 失败（junction: %v / copy: %v）: %s", err, cpErr, string(out))
			}
		}
	} else {
		if err := os.Symlink(target, link); err != nil {
			if cpErr := copyDir(target, link); cpErr != nil {
				return fmt.Errorf("挂载 our-free-model 失败: %w", err)
			}
		}
	}
	info.OFMLinked = true
	return nil
}

func ofmSourceDir() string {
	candidates := []string{}
	if p := strings.TrimSpace(os.Getenv("DSH_OFM_DIR")); p != "" {
		candidates = append(candidates, p)
	}
	userHome, _ := os.UserHomeDir()
	candidates = append(candidates,
		filepath.Join(userHome, ".dsh", "profiles", "desktop", "node_modules", "dsh-our-free-model"),
		filepath.Join(userHome, ".dsh", "profiles", "web", "node_modules", "dsh-our-free-model"),
	)
	for _, c := range candidates {
		if st, err := os.Stat(filepath.Join(c, "index.js")); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if mkErr := os.MkdirAll(filepath.Dir(target), 0o755); mkErr != nil {
			return mkErr
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func writeIfChanged(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil && string(old) == string(data) {
		return nil
	}
	return config.WriteFileAtomic(path, data)
}

func mustJSON(v any) []byte {
	dir, err := os.MkdirTemp("", "wbjson")
	if err == nil {
		defer os.RemoveAll(dir)
	}
	// 用标准库序列化，避免引入额外依赖
	return jsonMarshal(v)
}