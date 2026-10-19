package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"workbench/internal/workspace"
)

// runWorkspaceGuard 是护栏的机器可判据实证（容器/CI 友好，零模型依赖）。
//
// 断言面：
//  1. 授权目录内相对/绝对路径可写；
//  2. ".." 逃逸、盘外绝对路径、系统目录一律拒绝；
//  3. 目录内软链接指向外部 → 拒绝（防逃逸）；
//  4. 越界读拒绝；数据根可读（成品归档）；
//  5. 每次拒绝都留下审计记录。
//
// 末行输出 "[GUARD] pass=N fail=M root=..."；任何 fail>0 → rc=1。
func runWorkspaceGuard(mgr *workspace.Manager, root string) int {
	pass, fail := 0, 0
	check := func(name string, ok bool, detail string) {
		if ok {
			fmt.Printf("[PASS] %s\n", name)
			pass++
			return
		}
		fmt.Printf("[FAIL] %s %s\n", name, detail)
		fail++
	}
	cfg := mgr.Config()
	check("mode-external", cfg.Mode == workspace.ModeExternal, "mode="+string(cfg.Mode))
	check("root-that-dir", strings.EqualFold(mgr.Root(), root), "root="+mgr.Root())

	if _, err := mgr.ResolveWrite("guard/ok.txt", "guard"); err == nil {
		check("write-inside-relative", true, "")
	} else {
		check("write-inside-relative", false, err.Error())
	}
	if _, err := mgr.ResolveWrite(filepath.Join(root, "sub", "ok.txt"), "guard"); err == nil {
		check("write-inside-absolute", true, "")
	} else {
		check("write-inside-absolute", false, err.Error())
	}
	if _, err := mgr.ResolveWrite(filepath.Join(root, "..", "escape.txt"), "guard"); err != nil {
		check("deny-dotdot-escape", true, "")
	} else {
		check("deny-dotdot-escape", false, "should be denied")
	}
	if _, err := mgr.ResolveWrite(filepath.Join(string(os.PathSeparator), "tmp", "wb-escape.txt"), "guard"); err != nil {
		check("deny-outside-absolute", true, "")
	} else {
		check("deny-outside-absolute", false, "should be denied")
	}
	if os.Getenv("WINDIR") != "" {
		sys := filepath.Join(os.Getenv("WINDIR"), "wb-guard-should-not-write.txt")
		if _, err := mgr.ResolveWrite(sys, "guard"); err != nil {
			check("deny-system-dir", true, "")
		} else {
			check("deny-system-dir", false, "should be denied")
		}
	} else {
		if _, err := mgr.ResolveWrite("/etc/wb-guard-should-not-write.txt", "guard"); err != nil {
			check("deny-system-dir", true, "")
		} else {
			check("deny-system-dir", false, "should be denied")
		}
	}
	link := filepath.Join(root, "guard-escape-link")
	_ = os.Remove(link)
	if err := os.Symlink(os.TempDir(), link); err == nil {
		if _, err := mgr.ResolveWrite(filepath.Join(link, "evil.txt"), "guard"); err != nil {
			check("deny-symlink-escape", true, "")
		} else {
			check("deny-symlink-escape", false, "should be denied")
		}
		_ = os.Remove(link)
	} else {
		check("deny-symlink-escape", true, "(skip: symlink unavailable)")
	}
	if _, err := mgr.ResolveRead(filepath.Join(root, "..", "read-escape.txt"), "guard"); err != nil {
		check("deny-read-escape", true, "")
	} else {
		check("deny-read-escape", false, "should be denied")
	}
	if mgr.DataRoot() != "" {
		if _, err := mgr.ResolveRead(filepath.Join(mgr.DataRoot(), "config.json"), "guard"); err == nil {
			check("allow-read-data-root", true, "")
		} else {
			check("allow-read-data-root", false, err.Error())
		}
	} else {
		check("allow-read-data-root", true, "(skip: no data root)")
	}
	recs, aerr := workspace.ReadAudit(mgr.AuditPath())
	denied := 0
	for _, r := range recs {
		if !r.Allowed {
			denied++
		}
	}
	if aerr == nil && denied >= 4 {
		check("audit-records-denials", true, "")
	} else {
		check("audit-records-denials", false, fmt.Sprintf("denied=%d err=%v", denied, aerr))
	}
	fmt.Printf("[GUARD] pass=%d fail=%d root=%s\n", pass, fail, root)
	if fail > 0 {
		return 1
	}
	return 0
}
