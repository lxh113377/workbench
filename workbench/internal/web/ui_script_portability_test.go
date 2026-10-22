package web

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// repoRootFromCaller 用调用栈定位本文件所在目录，再上溯到仓根。
// 为什么不用相对路径：go test 的 cwd 是包目录，且可能被 -C / 脚本改过，
// 相对路径会算错（本轮实测踩到：..\..\tests\ui 找不到）。
func repoRootFromCaller(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 取不到本文件路径")
	}
	// 本文件在 <ROOT>/workbench/internal/web/，上溯 3 层到 <ROOT>
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
}

// TestUIScriptsHaveNoHardcodedRoot 钉住「前端验收脚本不得硬编码仓根路径」。
//
// 一手缺陷（2026-10-23 可移植性审计）：4 个前端验收脚本把仓根写死成
// `const ROOT = "D:\\workdesktop"` / `OUT = "D:...ui-shots"`。
// 后果：换机器 / CI 检出到其它路径，脚本一启动就崩 —— 前端验收**永远无法接进 CI**，
// 判据「存在但只在某台机器上能跑」，等于没有受理面。
// 修：改为 `path.dirname(fileURLToPath(import.meta.url))` 上溯两级推导仓根。
// 迁移实证：把脚本复制到 D:\\workdesktop\\xtmp\\relocate-test\\tests\\ui/ 后，ROOT 推导为 relocate-test（正确）。
func TestUIScriptsHaveNoHardcodedRoot(t *testing.T) {
	// Go 测试的 cwd = 包目录（workbench/internal/web），仓根需上溯 3 层；
	// 用 runtime.Caller 更稳（不受 -C/工作目录影响）。
	uiDir := filepath.Join(repoRootFromCaller(t), "tests", "ui")
	entries, err := os.ReadDir(uiDir)
	if err != nil {
		t.Fatalf("读不到 tests/ui 目录: %v", err)
	}
	// 匹配绝对盘符路径字面量（D:\\ 或 C:\\ 开头），排除注释行
	hardcoded := regexp.MustCompile(`["'][A-Za-z]:\\`)
	var scanned int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mjs") {
			continue
		}
		scanned++
		data, err := os.ReadFile(filepath.Join(uiDir, e.Name()))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue // 注释里提到旧路径是说明文字，不算
			}
			// 排除浏览器安装路径：Edge/Chrome 装在系统固定位置是**事实**，不是缺陷；
			// 且 cdp_probe 已支持 EDGE_BIN 环境变量覆盖（CI 上可用）。
			if strings.Contains(line, "Edge") || strings.Contains(line, "msedge") || strings.Contains(line, "EDGE_BIN") {
				continue
			}
			if hardcoded.MatchString(line) {
				t.Errorf("%s:%d 硬编码了绝对路径 —— 换机器/CI 检出即崩，须用 fileURLToPath 推导: %s", e.Name(), i+1, strings.TrimSpace(line))
			}
		}
	}
	if scanned == 0 {
		t.Fatalf("tests/ui 下没扫到任何 .mjs —— 判据失去受理面。dir=%s", uiDir)
	}
	// 至少确认关键脚本已用 fileURLToPath 推导
	for _, name := range []string{
		"frontend-gate.mjs", "cdp_probe.mjs", "design-audit.mjs", "design-shots.mjs",
	} {
		data, err := os.ReadFile(filepath.Join(uiDir, name))
		if err != nil {
			continue
		}
		if !strings.Contains(string(data), "fileURLToPath(import.meta.url)") {
			t.Errorf("%s 未用 fileURLToPath 推导仓根", name)
		}
	}
}
