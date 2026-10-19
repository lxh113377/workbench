//go:build windows

package shortcut

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsCanonicalExe 守护「快捷方式只指向正式安装位置」这条安全约束。
//
// 现场缺陷（2026-10-19）：没有任何守卫时，从临时目录/压测副本启动的实例
// 会把老大的桌面快捷方式改写成那个副本（实测被指向 runtime\soak-24h\wbsoak24.exe）。
// 判据：
//   - 只有 <根>\workbench.exe 且根下有 runtime 或 workbench-output 才算正式位置；
//   - 其它文件名（wbsoak24.exe / wbh.exe）一律不算；
//   - 文件名对但目录里没有数据根，也不算（防临时目录里放个同名 exe）。
func TestIsCanonicalExe(t *testing.T) {
	base := t.TempDir()

	// 1) 正式安装：workbench.exe + workbench-output/
	install := filepath.Join(base, "install")
	if err := os.MkdirAll(filepath.Join(install, "workbench-output"), 0o755); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(install, "workbench.exe")
	if err := os.WriteFile(canonical, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isCanonicalExe(canonical) {
		t.Fatal("正式安装位置的 workbench.exe 必须被认作 canonical")
	}

	// 2) 压测副本：文件名不是 workbench.exe
	soakDir := filepath.Join(base, "soak")
	if err := os.MkdirAll(filepath.Join(soakDir, "workbench-output"), 0o755); err != nil {
		t.Fatal(err)
	}
	soakExe := filepath.Join(soakDir, "wbsoak24.exe")
	if err := os.WriteFile(soakExe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isCanonicalExe(soakExe) {
		t.Fatal("压测副本（改名 exe）不得被认作 canonical（会覆盖桌面快捷方式）")
	}

	// 3) 名字对但目录里没有数据根：临时目录里的同名 exe
	lonely := filepath.Join(base, "tmp")
	if err := os.MkdirAll(lonely, 0o755); err != nil {
		t.Fatal(err)
	}
	lonelyExe := filepath.Join(lonely, "workbench.exe")
	if err := os.WriteFile(lonelyExe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isCanonicalExe(lonelyExe) {
		t.Fatal("同目录没有 runtime/workbench-output 的 workbench.exe 不得被认作 canonical")
	}

	// 4) 只有 runtime/ 也算正式安装（便携版数据根可能只有 runtime 先建出来）
	install2 := filepath.Join(base, "install2")
	if err := os.MkdirAll(filepath.Join(install2, "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe2 := filepath.Join(install2, "workbench.exe")
	if err := os.WriteFile(exe2, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isCanonicalExe(exe2) {
		t.Fatal("有 runtime/ 的 workbench.exe 应被认作 canonical")
	}

	// 5) 大小写不敏感（Windows 文件名语义）
	upper := filepath.Join(install, "WORKBENCH.EXE")
	if err := os.WriteFile(upper, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isCanonicalExe(upper) {
		t.Fatal("Windows 下文件名比较必须大小写不敏感")
	}
}

// TestCreateSkipsNonCanonical 守护调用侧行为：非正式位置返回 nil（静默跳过），
// 绝不能报错让引擎启动失败，也不能去改桌面快捷方式。
func TestCreateSkipsNonCanonical(t *testing.T) {
	// 测试进程自己的可执行名通常是 __debug_bin / shortcut.test.exe，
	// 一定不是 workbench.exe 且所在目录没有数据根 ⇒ Create 必须静默返回 nil。
	if err := Create(); err != nil {
		t.Fatalf("非正式位置调用 Create 必须静默跳过（返回 nil），实际: %v", err)
	}
}
