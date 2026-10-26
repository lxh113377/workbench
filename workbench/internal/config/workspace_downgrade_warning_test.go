package config

import (
	"path/filepath"
	"strings"
	"testing"

	"workbench/internal/workspace"
)

// TestNormalizeWarnsWhenWorkspaceFallsBackToProject 钉住「external 降级为 project 必须留痕」。
//
// 为什么需要（2026-10-26 round 4 实测，报告 docs/工作空间聚焦与模板域覆盖-五轴审查-2026-10-26.md §1.1/§1.6）：
// normalizeWorkspace 在 external 模式却缺少合法绝对根时会把模式**静默**改回 project 并清空 Root ——
// 后果是「被选中的工作目录」被无声丢掉：任务回到数据根沙箱里跑、workspace context 不再注入
// （engine.go 的 workspaceContextText 在非 external 时返回空串），而配置里只剩一个 stale root（round 1 现场）。
// 本仓对「隐身失败」的既定处置就是 Config.Warnings（config.go addWarning 自述：钳制是静默的，
// Warnings 让被钳过这件事可观测，且会随 --diagnose 的 config 段直接可见）。这条判据先红后绿。
func TestNormalizeWarnsWhenWorkspaceFallsBackToProject(t *testing.T) {
	cases := []struct {
		name string
		root string
	}{
		{"空根", ""},
		{"相对路径", filepath.Join("relative", "dir")},
	}
	for _, tc := range cases {
		c := Config{SchemaVersion: 3}
		c.Workspace.Mode = workspace.ModeExternal
		c.Workspace.Root = tc.root
		c.Normalize()

		if c.Workspace.Mode != workspace.ModeProject {
			t.Fatalf("%s: external 缺合法根时应回落 project，实得 %s", tc.name, c.Workspace.Mode)
		}
		if c.Workspace.Root != "" {
			t.Errorf("%s: 回落后 Root 必须清空（否则留 stale root 误导读配置者），实得 %q", tc.name, c.Workspace.Root)
		}
		warned := false
		for _, w := range c.Warnings {
			if strings.Contains(w, "workspace") {
				warned = true
			}
		}
		if !warned {
			t.Errorf("%s: 静默降级 —— Warnings 里没有工作目录相关条目：%v", tc.name, c.Warnings)
		}
	}
}

// TestNormalizeKeepsValidExternalWorkspace 是对照组：合法绝对根不得被降级、也不许报警。
// （对照组不成立的实验等于没做 —— 本仓在册判据标准。）
func TestNormalizeKeepsValidExternalWorkspace(t *testing.T) {
	dir := t.TempDir()
	c := Config{SchemaVersion: 3}
	c.Workspace.Mode = workspace.ModeExternal
	c.Workspace.Root = dir
	c.Normalize()

	if c.Workspace.Mode != workspace.ModeExternal {
		t.Fatalf("合法绝对根被降级：mode=%s", c.Workspace.Mode)
	}
	if c.Workspace.Root != dir {
		t.Fatalf("Root 被改写：%q（期望 %q）", c.Workspace.Root, dir)
	}
	for _, w := range c.Warnings {
		if strings.Contains(w, "workspace.mode") {
			t.Errorf("合法配置不该产生降级告警：%q", w)
		}
	}
}
