package retention

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mkTaskHome 在 <dshHome>/tasks/<name> 下造假任务 DSH_HOME。
//
// 目录结构必须与生产一致（PruneDSHHome 遍历的是 dshHome/tasks/）：
// 第一版测试把目录直接建在 root 下，而生产代码拼了 root/tasks，
// 于是永远遍历不到 —— 3 个用例假红。
//
// 另外 age 要设在**文件** mtime 上：生产判定用"目录内最新文件 mtime"。
func mkTaskHome(t *testing.T, dshHome, name string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(dshHome, "tasks", name)
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "sessions", "log.bin")
	if err := os.WriteFile(log, make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-age)
	_ = os.Chtimes(log, old, old)
	return dir
}

// TestPruneDryRunDoesNotDelete dry-run 必须只统计不删除。
func TestPruneDryRunDoesNotDelete(t *testing.T) {
	dshHome := t.TempDir()
	mkTaskHome(t, dshHome, "old-1", 10*24*time.Hour)
	mkTaskHome(t, dshHome, "new-1", 1*time.Hour)

	res := PruneDSHHome(dshHome, DefaultPolicy(), true)
	if !res.DryRun {
		t.Fatal("DryRun 应为 true")
	}
	if res.ReclaimedDirs != 1 {
		t.Fatalf("应命中 1 个过期目录，实际 %d", res.ReclaimedDirs)
	}
	if res.SkippedDirs != 1 {
		t.Fatalf("应跳过 1 个新鲜目录，实际 %d", res.SkippedDirs)
	}
	// 关键：dry-run 后目录必须都还在
	if _, err := os.Stat(filepath.Join(dshHome, "tasks", "old-1")); err != nil {
		t.Fatal("dry-run 不应删除任何东西")
	}
	if res.FreedBytes <= 0 {
		t.Fatal("应统计出可释放的字节数")
	}
}

// TestPruneActuallyDeletes 真跑必须删掉过期的、留下新鲜的。
func TestPruneActuallyDeletes(t *testing.T) {
	dshHome := t.TempDir()
	mkTaskHome(t, dshHome, "old-1", 10*24*time.Hour)
	mkTaskHome(t, dshHome, "old-2", 20*24*time.Hour)
	mkTaskHome(t, dshHome, "new-1", 2*time.Hour)

	res := PruneDSHHome(dshHome, DefaultPolicy(), false)
	if res.ReclaimedDirs != 2 {
		t.Fatalf("应回收 2 个，实际 %d", res.ReclaimedDirs)
	}
	if _, err := os.Stat(filepath.Join(dshHome, "tasks", "old-1")); !os.IsNotExist(err) {
		t.Fatal("过期目录应被删除")
	}
	if _, err := os.Stat(filepath.Join(dshHome, "tasks", "old-2")); !os.IsNotExist(err) {
		t.Fatal("过期目录应被删除")
	}
	if _, err := os.Stat(filepath.Join(dshHome, "tasks", "new-1")); err != nil {
		t.Fatal("保留窗口内的目录不应被删")
	}
}

// TestPruneIgnoresFiles 非目录条目必须忽略（不误删文件）。
func TestPruneIgnoresFiles(t *testing.T) {
	dshHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dshHome, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dshHome, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkTaskHome(t, dshHome, "old", 10*24*time.Hour)

	PruneDSHHome(dshHome, DefaultPolicy(), false)
	if _, err := os.Stat(filepath.Join(dshHome, "stray.txt")); err != nil {
		t.Fatal("文件不应被清理逻辑碰")
	}
}

// TestPruneWorkspaceKeepsTaskDir 清 workspace 不能动 final.md。
func TestPruneWorkspaceKeepsTaskDir(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "task-1")
	ws := filepath.Join(taskDir, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "out.py"), []byte("print(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "final.md"), []byte("# 成果"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-40 * 24 * time.Hour)
	_ = os.Chtimes(filepath.Join(ws, "out.py"), old, old)

	PruneTaskWorkspaces(root, DefaultPolicy(), false)
	if _, err := os.Stat(ws); !os.IsNotExist(err) {
		t.Fatal("过期 workspace 应被清理")
	}
	if _, err := os.Stat(filepath.Join(taskDir, "final.md")); err != nil {
		t.Fatal("final.md 是成果资产，绝不能删")
	}
}

// TestEstimateGrowth 估算必须给出可读结论。
func TestEstimateGrowth(t *testing.T) {
	dshHome := t.TempDir()
	mkTaskHome(t, dshHome, "d1", 48*time.Hour)
	mkTaskHome(t, dshHome, "d2", 24*time.Hour)
	mkTaskHome(t, dshHome, "d3", 1*time.Hour)

	est := EstimateGrowth(dshHome)
	if est.Count != 3 {
		t.Fatalf("Count = %d, want 3", est.Count)
	}
	if est.TotalBytes <= 0 {
		t.Fatal("应统计总占用")
	}
	if est.PerDay <= 0 {
		t.Fatalf("应估算出日均增长，实际 %v", est.PerDay)
	}
	// 格式必须是人类可读的（不能是空串）
	if s := est.Format(); len(s) < 10 {
		t.Fatalf("Format 输出过短: %q", s)
	}
}

// TestPruneMissingRoot 目录不存在时必须静默成功（不报错）。
func TestPruneMissingRoot(t *testing.T) {
	res := PruneDSHHome(filepath.Join(t.TempDir(), "nonexistent"), DefaultPolicy(), false)
	if res.ReclaimedDirs != 0 || len(res.Errors) != 0 {
		t.Fatalf("目录不存在应静默返回: %+v", res)
	}
}

// TestPolicyNormalize 零值必须补默认（用户不配也能安全运行）。
func TestPolicyNormalize(t *testing.T) {
	p := Policy{}.normalized()
	if p.DSHHomeDays <= 0 || p.WorkspaceDays <= 0 || p.TaskDirDays <= 0 {
		t.Fatalf("零值未补默认: %+v", p)
	}
}

// TestTrashRefusesDangerous 危险路径必须被拒（安全阀）。
//
// 2026-10-19 平台解耦（issue #2）：改造前用例硬编码 "C:\\"，
// 于是 ① Linux 容器里 C:\\ 不存在，Trash 在"目标不存在"处就返回错误 ——
// 安全阀根本没被检验（假绿），② 一旦路径存在，非 Windows 分支直接 os.RemoveAll。
// 现在拆成两层：纯字符串判定的 refuseDangerousPath 跨平台直接测（不碰文件系统），
// Trash 集成测只喂"确实存在"的危险路径（OS 根与主目录根）。
func TestTrashRefusesDangerous(t *testing.T) {
	// 层 1：安全阀纯判定 —— 两平台同一组断言，Windows 形态路径也必须拒。
	for _, dp := range []string{"C:\\", "C:\\Windows", "C:\\Program Files", "C:\\Users"} {
		if err := refuseDangerousPath(dp); err == nil {
			t.Fatalf("refuseDangerousPath(%q) 必须拒绝", dp)
		}
	}
	for _, dp := range []string{"/", "/etc", "/usr", "/bin", "/boot", "/root", "/home", "/var", "/sys", "/proc"} {
		if err := refuseDangerousPath(dp); err == nil {
			t.Fatalf("refuseDangerousPath(%q) 必须拒绝", dp)
		}
	}
	if err := refuseDangerousPath("C:\\workbench-output\\tasks"); err != nil {
		t.Fatalf("普通任务目录不应被拒: %v", err)
	}
	if err := refuseDangerousPath("/tmp/workbench-cache/sub"); err != nil {
		t.Fatalf("普通深层目录不应被拒: %v", err)
	}

	// 层 2：Trash 集成 —— 只喂真实存在的危险路径，确保测的是安全阀而不是存在性检查。
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("拿不到主目录:", err)
	}
	for _, dp := range []string{string(filepath.Separator), home} {
		if err := Trash(dp); err == nil {
			t.Fatalf("Trash(%q) 必须被安全阀拒绝", dp)
		} else if !strings.Contains(err.Error(), "拒绝") {
			t.Fatalf("Trash(%q) 应因安全阀被拒，实际: %v", dp, err)
		}
	}
}
