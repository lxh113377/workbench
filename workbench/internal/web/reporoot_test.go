package web

import (
	"os"
	"path/filepath"
	"testing"
)

// findRepoRoot 从当前工作目录逐级向上，定位含 tests/ui 子目录的仓根。
//
// 为什么不用 runtime.Caller 记录的路径（本 helper 的存在理由，2026-10-23 实测）：
// go test 若带 -trimpath（发布/CI 构建常见），编译期记录的源文件路径会被裁成去前缀形式，
// runtime.Caller 取到的 thisFile 不再是真实磁盘路径，按它上溯算出错误仓根 → 判据假失败，
// 正是本仓在册的「本机绿 CI 红」死结。而 os.Getwd() 是 go test 真实运行目录（文件系统真实路径，
// 不受 -trimpath 影响），从它向上找含 tests/ui 的祖先目录，无论 cwd 是包目录还是仓根都稳定命中。
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Skipf("取不到工作目录（%v），无法定位仓根", err)
	}
	for i := 0; i < 12 && dir != ""; i++ {
		if st, serr := os.Stat(filepath.Join(dir, "tests", "ui")); serr == nil && st.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Skip("从工作目录向上未找到含 tests/ui 的仓根（cwd 可能不在仓库内），跳过")
	return ""
}
