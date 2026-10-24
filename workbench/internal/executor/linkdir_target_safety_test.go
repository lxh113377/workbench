package executor

// linkdir_target_safety_test.go 钉住 linkDir 的「不伤害 target」安全性质。
//
// 由来（2026-10-25 五轴审查硬约束 #5 时的探针实测）：linkDir 在 Stat 失败时会执行
// os.RemoveAll(dst)，初看像经典的「rd /s 删穿 junction」风险面。一次性探针实测
// （临时 cmd，跑完即删）给出的结论相反：
//
//	LSTAT isDir=false isSymlink=false   <- Go 把 junction 认成普通目录，Stat 成功即早退
//	REMOVEALL err=<nil>  LINK_GONE      <- 删除只摘链接
//	TARGET_SURVIVED_OK   TARGET_DIR isDir=true  <- target 与其内容完好
//
// 结论是两层安全：① dst 已是健康 junction 时 Stat 成功即早退，RemoveAll 不会执行
// （RemoveAll 只在 dst 不存在或为非目录占位时走到，见测试 C 分支）；
// ② 即使执行，Go 的 os.RemoveAll 对 junction 是「摘链」而非「删穿」。
// 探针是一次性的，实测结论必须变成测试才守得住 —— 本文件把两层都锁进 CI。
//
// 平台边界（如实声明，不冒充覆盖）：junction 是 Windows 语义，非 Windows 下 t.Skip ——
// 与包内既有 linkdir_silent_test.go 同一模式；Linux CI 上这两类断言无对象可测。
// 但 Windows 上「建不出 junction」判 FAIL 不判 SKIP：skip 会把环境故障洗成绿。

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// mustJunction 用与 linkDir 相同的方式建 junction（mklink /J，无需管理员权限）。
func mustJunction(t *testing.T, target, link string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		// 刻意判红不判跳：Windows runner 上建不出 junction 属于环境异常，
		// 静默 skip 会让这两条安全断言变成「从没跑过」（vacuous pass 家族）。
		t.Fatalf("建 junction 失败: %v (%s)", err, out)
	}
}

// keepPrecious 断言 target 目录与其中的珍贵文件仍在——「没被删穿」的判据。
func keepPrecious(t *testing.T, target, mark string) {
	t.Helper()
	if st, err := os.Stat(target); err != nil || !st.IsDir() {
		t.Fatalf("target 目录消失（删穿）: err=%v st=%v", err, st)
	}
	if _, err := os.Stat(filepath.Join(target, mark)); err != nil {
		t.Fatalf("target 内珍贵文件被删（删穿）: %v", err)
	}
}

// TestRemoveAllOnJunctionDetachesNotDeletesThrough 钉住第 ②层：OS 语义层。
// 变异验证（本轮实测）：控制组直接毁 target 内容 => 判红（断言有牙齿）；
// 而把摘链换成 cmd rd /s /q 也**不判红** —— 本机实测 rd /s /q 对 junction 同样只摘链
// （「rd 删穿 junction」是 XP 时代的行为，Vista+ 已修）。所以本测试锁的是**这台 OS
// 的现行语义**：将来任何改动或系统行为变化若真删穿，它都会变红，而不是替旧传说话。
func TestRemoveAllOnJunctionDetachesNotDeletesThrough(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction 仅 Windows")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	const mark = "precious.txt"
	if err := os.WriteFile(filepath.Join(target, mark), []byte("do-not-lose"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustJunction(t, target, link)

	if err := os.RemoveAll(link); err != nil {
		t.Fatalf("RemoveAll(junction) 失败: %v", err)
	}
	if _, err := os.Stat(link); err == nil {
		t.Fatal("junction 未被摘除")
	}
	keepPrecious(t, target, mark)
}

// TestLinkDirNeverDamagesTarget 钉住第 ①层：linkDir 的两条分支都不伤 target。
// 覆盖幂等早退路径（dst 已是健康 junction）与 ReplaceAll 分支（dst 是普通文件）。
func TestLinkDirNeverDamagesTarget(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction 仅 Windows")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	const mark = "f.txt"
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, mark), []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 分支 A：正常建链 -> target 完好、链接可读
	if err := linkDir(src, dst); err != nil {
		t.Fatalf("linkDir 建链失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, mark)); err != nil {
		t.Fatalf("junction 不可读: %v", err)
	}
	keepPrecious(t, src, mark)

	// 分支 B：再调一次走 Stat 早退（幂等）—— 不得触碰 target
	if err := linkDir(src, dst); err != nil {
		t.Fatalf("linkDir 应幂等: %v", err)
	}
	keepPrecious(t, src, mark)

	// 分支 C：dst 是普通文件（Stat 成功但非目录）=> 执行 RemoveAll(dst) 后建链。
	// 这条分支是唯一真的会删东西的路径，必须证明它删的只是 dst、不是 src。
	fileDst := filepath.Join(root, "filedst")
	if err := os.WriteFile(fileDst, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := linkDir(src, fileDst); err != nil {
		t.Fatalf("linkDir 应能替换非目录 dst: %v", err)
	}
	keepPrecious(t, src, mark)
	if _, err := os.Stat(filepath.Join(fileDst, mark)); err != nil {
		t.Fatalf("替换后 junction 不可读: %v", err)
	}
}
