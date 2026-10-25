package retention

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// refuseDangerousPath 是全平台一致的安全阀：拒绝盘根、文件系统根、系统目录与用户主目录根。
//
// 2026-10-19 平台解耦（issue #2）：本函数原先只存在于 trash_windows.go，
// 于是非 Windows 的 Trash（直接 os.RemoveAll）**完全没有安全阀** ——
// 实测 Trash("/") 在 Linux 上会真的递归删掉文件系统根。
// 现在上移到平台无关文件，Windows 与非 Windows 两条删除路径都必须先过这道闸。
//
// 判定只依赖路径字符串（不碰文件系统），因此可跨平台单测。
// 归一化必须双向：filepath.ToSlash 只在 Windows 转换分隔符，Linux 上
// "C:\\Windows" 的反斜杠是普通字符不会被转 —— 而测试在两平台喂同一组
// Windows 形态路径断言必须拒绝（Windows 上 Clean("/etc") 同样会变 \\etc，
// 本机实测抓出：单向归一会让同一份判定在两平台行为不一致）。
func refuseDangerousPath(abs string) error {
	clean := filepath.Clean(abs)
	if clean == "." {
		return fmt.Errorf("拒绝操作当前目录根: %s", clean)
	}
	// 统一成小写正斜杠形态做字符串判定（对两平台的真实路径与对侧形态路径都成立）。
	key := strings.ToLower(strings.ReplaceAll(filepath.ToSlash(clean), "\\", "/"))
	for strings.HasSuffix(key, "/") && len(key) > 1 {
		key = strings.TrimSuffix(key, "/")
	}

	// 1) 文件系统根。
	if key == "/" {
		return fmt.Errorf("拒绝操作系统根: %s", clean)
	}
	// 2) 盘根（本 OS 语义下的权威判定）：C:\ 与 C: 两种形态都要拒。
	if v := filepath.VolumeName(clean); v != "" {
		if clean == v || strings.EqualFold(clean, v+string(filepath.Separator)) {
			return fmt.Errorf("拒绝操作盘根: %s", clean)
		}
	}
	// 2b) 盘根的字符串形态（覆盖跨平台输入，如 Linux 上测 "C:\\"）。
	if len(key) == 2 && key[1] == ':' && (key[0] >= 'a' && key[0] <= 'z') {
		return fmt.Errorf("拒绝操作盘根: %s", clean)
	}
	// 3) 已知系统目录（Windows 与 Unix 各一组，归一后小写比较）。
	switch key {
	case "c:/windows", "c:/windows/system32", "c:/users", "c:/program files",
		"c:/program files (x86)", "c:/programdata",
		"/bin", "/boot", "/dev", "/etc", "/home", "/lib", "/lib64", "/media", "/mnt",
		"/opt", "/proc", "/root", "/run", "/sbin", "/srv", "/sys", "/usr", "/var":
		return fmt.Errorf("拒绝操作系统路径: %s", clean)
	}
	// 4) 用户主目录根也不动（只删它下面的具体子目录）。
	if home, herr := os.UserHomeDir(); herr == nil &&
		strings.EqualFold(key, strings.ToLower(strings.TrimSuffix(strings.ReplaceAll(filepath.ToSlash(filepath.Clean(home)), "\\", "/"), "/"))) {
		return fmt.Errorf("拒绝删除用户主目录根: %s", clean)
	}
	return nil
}
