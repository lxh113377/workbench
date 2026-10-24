//go:build windows

package shortcut

import (
	"os"
	"path/filepath"
	"strings"
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

// TestTruncateBounds 守护错误信息截断助手。
//
// 覆盖动机（2026-10-24）：覆盖率门禁抓到本包 23.9% < 25% 阈值，而 truncate
// 是 Create 两条错误路径都要走的助手却零覆盖。它是纯函数，直接钉死边界即可。
func TestTruncateBounds(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"", 0, ""},          // 空串：任何 n 都原样返回
		{"abc", 5, "abc"},    // 短于上限：原样
		{"abc", 3, "abc"},    // 恰好等于上限：不截（len<=n 语义）
		{"abcdef", 3, "abc"}, // 超长：截到 n
		{"长字符串测试", 3, "长"},   // 多字节按字节截：一个汉字 = 3 字节，n=3 恰好切出一个
		{"长字符串测试", 6, "长字"},  // n=6 = 两个汉字（每字 3 字节）
	}
	for _, c := range cases {
		if got := truncate(c.in, c.n); got != c.want {
			t.Fatalf("truncate(%q,%d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
	// 幂等性：截断结果再截必须不变（否则错误信息会在多层包装里被反复切）
	once := truncate("abcdefghij", 4)
	if twice := truncate(once, 4); twice != once {
		t.Fatalf("truncate 不幂等：%q -> %q", once, twice)
	}
}

// TestPsScriptShortcutContract 守护快捷方式脚本的字段契约（2026-10-24 补）。
//
// 与 shortcut_silent_test.go 的分工：那份钉「不得有 --silent、必须置空 Arguments」，
// 本份钉其余必备字段与顺序性要求 —— 少任何一项，快捷方式都会以某种方式坏掉：
//
//	TargetPath/WorkingDirectory/IconLocation/WindowStyle/Description 五件套齐全；
//	Arguments 置空必须出现在 Save() **之前**（置空却不保存等于没置空）；
//	回读校验必须同时比对 TargetPath 与 Arguments。
func TestPsScriptShortcutContract(t *testing.T) {
	script := psScript
	for _, must := range []string{
		"$lnk.TargetPath = $exe",
		"$lnk.WorkingDirectory = $workDir",
		"$lnk.IconLocation =",
		"$lnk.WindowStyle = 1",
		"$lnk.Description =",
		"$lnk.Arguments = \"\"",
		"$lnk.Save()",
		"$check.Arguments -eq \"\"",
	} {
		if !strings.Contains(script, must) {
			t.Fatalf("快捷方式脚本缺少必备片段: %s", must)
		}
	}
	// 顺序性：置空 Arguments 必须在 Save() 之前（CreateShortcut 打开已有 .lnk 时
	// 读的是旧值，只赋值不保存不会生效）。
	if strings.Index(script, "$lnk.Arguments = \"\"") > strings.Index(script, "$lnk.Save()") {
		t.Fatal("Arguments 置空必须出现在 Save() 之前，否则对已有 .lnk 不生效")
	}
	// 两个目标位置都要建（桌面 + 开始菜单），只建一个会让用户在另一处找不到入口
	// 口径修正（首版把「写两次 + 回读两次」写成 3 处，实测 2 处：
	// 写与回读在同一个 foreach 体内各一次，两个目标位置 => 共 2 处）。
	// 这里钉「至少 2 处」：少了说明回读校验被摘（只写不读 = 无校验）。
	if n := strings.Count(script, "$shell.CreateShortcut("); n < 2 {
		t.Fatalf("CreateShortcut 调用点应至少有 2 处（两个目标位置各一次），实际 %d", n)
	}
}
