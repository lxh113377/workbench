//go:build windows

package workspace

// probe_windows_test.go -- probeWrite 的 Windows 只读目录判据（2026-10-23 补）。
//
// 为什么需要（round 38 审计）：`workspace.go:335` 用 probeWrite 判定目录是否可写，
// 这是 external 模式选目录时的**核心闸门**（不可写就报「目录不可写」并给建议）。
// 而它唯一的失败路径用例 TestProbeWriteRejectsReadOnlyDir 在 Windows 上直接 t.Skip
// （Unix 用 chmod 0o500 构造只读，Windows 权限模型不同）。
// 后果：本项目是 **Windows 单 EXE** —— 恰恰在唯一会发布它的平台上，
// 「探针能否拒绝只读目录」从未被执行过一次。
//
// 构造只读的手段（本机实测有效，非推测）：icacls 施加 deny 写 ACE：
//   实测「施加后直写 Access denied → 移除后恢复可写」两步均如预期。
//
// 前置自检（防假绿）：真正调用 probeWrite 之前，先直连 os.Create 确认 deny 生效。
//   若直连仍写得进去（进程带绕过 ACL 的特权等），那是**前置条件不成立**，
//   显式 t.Skip 并说明 —— 与被测逻辑失败区分开，禁把「没验成」当「验过了」。

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
)

func icacls(args ...string) (string, error) {
	b, err := exec.Command("icacls", args...).CombinedOutput()
	return string(b), err
}

func TestProbeWriteRejectsReadOnlyDirWindows(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skipf("取不到当前用户，无法构造只读目录: %v", err)
	}
	dir := t.TempDir()
	if out, err := icacls(dir, "/deny", fmt.Sprintf("%s:(OI)(CI)W", u.Username)); err != nil {
		t.Skipf("icacls 施加 deny 失败（无法构造只读前提）: %v / %s", err, out)
	}
	// 恢复必须在 t.TempDir() 删目录之前完成（deny 会挡住删除）。
	// Go 的 cleanup 是 LIFO：本注册晚于 TempDir 的清理注册，故先执行。
	t.Cleanup(func() {
		if out, err := icacls(dir, "/remove:d", u.Username); err != nil {
			t.Logf("恢复 ACL 失败（临时目录可能删不掉，非被测逻辑问题）: %v / %s", err, out)
		}
	})
	// 前置自检：deny 必须真的挡住直写，否则本用例没有构造出只读目录。
	if f, err := os.Create(filepath.Join(dir, "precheck.txt")); err == nil {
		_ = f.Close()
		_ = os.Remove(filepath.Join(dir, "precheck.txt"))
		t.Skip("icacls deny 未生效（进程可能有绕过 ACL 的特权）：无法构造只读目录，未验证")
	}
	if _, err := probeWrite(dir); err == nil {
		t.Fatal("只读目录应写入失败，但 probeWrite 返回成功 —— 可写探针在本平台形同虚设")
	}
}
