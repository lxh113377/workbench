package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// newManager 构造一个数据根/项目根都在临时目录里的管理器，避免命中真实系统目录规则。
func newManager(t *testing.T) (*Manager, string, string) {
	t.Helper()
	base := t.TempDir()
	project := filepath.Join(base, "wb")
	data := filepath.Join(project, "workbench-output")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	m := New(data, project, DefaultConfig())
	return m, project, data
}

func mkdir(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseModeAndNormalize(t *testing.T) {
	cases := map[string]Mode{
		"external": ModeExternal, "EXTERNAL": ModeExternal, " external ": ModeExternal,
		"project": ModeProject, "": ModeProject, "weird": ModeProject,
	}
	for in, want := range cases {
		if got := ParseMode(in); got != want {
			t.Fatalf("ParseMode(%q) = %q, want %q", in, got, want)
		}
	}
	// 只读根的去重/过滤判据必须用**本平台**的绝对路径，否则在 Linux 上会被全部判为相对路径，
	// 测试红的是平台假设而不是被测逻辑（2026-10-19 容器轮实证）。
	absRoot := filepath.Join(t.TempDir(), "shared")
	c := Config{Mode: "external", MaxFiles: -1, MaxBytes: 0, MaxFileBytes: 1 << 30,
		AllowedReadRoots: []string{"relative/path", "", " " + absRoot + " ", absRoot}}
	n := c.Normalized()
	if n.MaxFiles != DefaultConfig().MaxFiles || n.MaxBytes != DefaultConfig().MaxBytes {
		t.Fatalf("越界上限未回默认: %+v", n)
	}
	if n.MaxFileBytes != DefaultConfig().MaxFileBytes {
		t.Fatalf("MaxFileBytes 未钳制: %d", n.MaxFileBytes)
	}
	if len(n.AllowedReadRoots) != 1 {
		t.Fatalf("只读根应去重并剔除相对路径: %+v", n.AllowedReadRoots)
	}
}

func TestIsWithinBoundaries(t *testing.T) {
	root := filepath.Join(filepath.VolumeName(os.TempDir()), "wb-root")
	sep := string(os.PathSeparator)
	cases := []struct {
		root, target string
		want         bool
	}{
		{root, root, true},
		{root, filepath.Join(root, "a"), true},
		{root, filepath.Join(root, "a", "b"), true},
		{root, root + "-evil", false},          // 前缀同形但不是子目录（关键边界）
		{root, filepath.Join(root, "..", "x"), false}, // 归一化后越界
		{root, "", false},
		{"", root, false},
	}
	for _, c := range cases {
		if got := IsWithin(c.root, c.target); got != c.want {
			t.Fatalf("IsWithin(%q, %q) = %v, want %v", c.root, c.target, got, c.want)
		}
	}
	_ = sep
}


// normCanonical 归一化路径用于跨环境比较：解析软链接与 Windows 8.3 短路径名，
// 使 CI（runneradmin 临时目录常为 RUNNER~1 短名）与本机（长名）结果一致。
func normCanonical(p string) string {
	if r, err := evalExisting(p); err == nil && r != "" {
		p = r
	}
	return normForCompare(p)
}

func TestValidateRejectsBadInputs(t *testing.T) {
	m, project, data := newManager(t)
	abs := func(p string) string { return p }

	// 空路径
	if v, err := m.Validate("   "); err == nil || v.Allowed {
		t.Fatalf("空路径应拒绝: %+v", v)
	}
	// 相对路径
	if v, err := m.Validate("relative/dir"); err == nil || v.Allowed {
		t.Fatalf("相对路径应拒绝: %+v", v)
	}
	// 不存在的目录
	if v, err := m.Validate(abs(filepath.Join(t.TempDir(), "nope"))); err == nil || v.Allowed || v.Exists {
		t.Fatalf("不存在的目录应拒绝: %+v", v)
	}
	// 文件而不是目录
	f := filepath.Join(t.TempDir(), "a.txt")
	writeFile(t, f, "x")
	if v, err := m.Validate(f); err == nil || v.Allowed {
		t.Fatalf("文件应拒绝: %+v", v)
	}
	// 盘符根
	if v, err := m.Validate(filepath.VolumeName(os.TempDir()) + string(os.PathSeparator)); err == nil || v.Allowed || !v.Forbidden {
		t.Fatalf("盘符根应拒绝: %+v", v)
	}
	// 工作台自身项目根（以及其子目录）
	for _, p := range []string{project, data, filepath.Join(data, "tasks")} {
		if v, err := m.Validate(mkdir(t, p)); err == nil || v.Allowed {
			t.Fatalf("工作台自身目录应拒绝: %s => %+v", p, v)
		}
	}
	// 工作台项目根的父目录（会包含项目根）
	if v, err := m.Validate(filepath.Dir(project)); err == nil || v.Allowed {
		t.Fatalf("项目根的父目录应拒绝: %+v", v)
	}
}

func TestValidateAcceptsNormalDir(t *testing.T) {
	m, _, _ := newManager(t)
	dir := mkdir(t, filepath.Join(t.TempDir(), "my-project"))
	writeFile(t, filepath.Join(dir, "README.md"), "# hi")
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	v, err := m.Validate(dir)
	if err != nil {
		t.Fatalf("普通目录应通过: %v", err)
	}
	if !v.Allowed || !v.Exists || !v.IsDir || !v.Readable || !v.Writable {
		t.Fatalf("校验结果字段不完整: %+v", v)
	}
	if v.Files != 1 || v.Dirs != 1 {
		t.Fatalf("入口统计不对: files=%d dirs=%d", v.Files, v.Dirs)
	}
	if v.Canonical == "" || normCanonical(v.Canonical) != normCanonical(dir) {
		t.Fatalf("canonical = %q, want %q", v.Canonical, dir)
	}
	// 写探针不得留下文件
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), "wb-write-probe") {
			t.Fatalf("写探针残留: %s", e.Name())
		}
	}
}

func TestSelectExternalAndResetProject(t *testing.T) {
	m, _, data := newManager(t)
	dir := mkdir(t, filepath.Join(t.TempDir(), "target"))
	if _, err := m.SelectExternal(dir); err != nil {
		t.Fatal(err)
	}
	if !m.IsExternal() {
		t.Fatal("应切换为 external")
	}
	wd, sandboxed := m.TaskWorkDir("task-1")
	if sandboxed || normCanonical(wd) != normCanonical(dir) {
		t.Fatalf("external 任务工作目录 = %q sandboxed=%v", wd, sandboxed)
	}
	// 非法目录不得改变状态
	if _, err := m.SelectExternal("relative"); err == nil {
		t.Fatal("非法目录应报错")
	}
	if !m.IsExternal() {
		t.Fatal("失败的选择不应改变模式")
	}
	cfg := m.ResetProject()
	if cfg.Mode != ModeProject {
		t.Fatalf("切回后 mode = %q", cfg.Mode)
	}
	wd, sandboxed = m.TaskWorkDir("task-1")
	if !sandboxed || !strings.Contains(wd, filepath.Join("tasks", "task-1", "workspace")) {
		t.Fatalf("项目模式任务目录 = %q", wd)
	}
	if !strings.HasPrefix(normCanonical(wd), normCanonical(data)) {
		t.Fatalf("项目模式目录应在数据根下: %q", wd)
	}
}

func TestSetConfigRejectsExternalWithoutRoot(t *testing.T) {
	m, _, _ := newManager(t)
	cfg := DefaultConfig()
	cfg.Mode = ModeExternal
	if err := m.SetConfig(cfg); err == nil {
		t.Fatal("external 模式无 root 必须拒绝")
	}
}

func TestResolveWriteAndRead(t *testing.T) {
	m, _, _ := newManager(t)
	root := mkdir(t, filepath.Join(t.TempDir(), "proj"))
	if _, err := m.SelectExternal(root); err != nil {
		t.Fatal(err)
	}
	// 目录内相对路径 OK
	got, err := m.ResolveWrite("src/main.go", "t1")
	if err != nil || normCanonical(got) != normCanonical(filepath.Join(root, "src", "main.go")) {
		t.Fatalf("目录内相对路径应允许: %q %v", got, err)
	}
	// 目录内绝对路径 OK
	abs := filepath.Join(root, "a", "b.txt")
	if _, err := m.ResolveWrite(abs, "t1"); err != nil {
		t.Fatalf("目录内绝对路径应允许: %v", err)
	}
	// .. 逃逸
	if _, err := m.ResolveWrite(filepath.Join(root, "..", "escape.txt"), "t1"); err == nil {
		t.Fatal(".. 逃逸必须拒绝")
	}
	// 越界绝对路径：必须在**授权根之外**。Windows 上用盘符根；Linux 上用 /tmp（授权根在 t.TempDir() 下，
	// 而 t.TempDir() 与 /tmp 无包含关系）。原写法在 Linux 上 VolumeName(root)=="" ⇒ 拼出相对路径 ⇒ 假绿/假红。
	outside := "/tmp/wb-escape.txt"
	if runtime.GOOS == "windows" {
		outside = string(filepath.VolumeName(root)) + "\\wb-escape.txt"
	}
	if _, err := m.ResolveWrite(outside, "t1"); err == nil {
		t.Fatalf("越界绝对路径必须拒绝: %s", outside)
	}
	// 系统目录：Windows 用 WINDIR，Linux 用 /etc（systemDirs 已按平台列举）。
	sysDir := os.Getenv("WINDIR")
	if sysDir == "" {
		sysDir = "/etc"
	}
	if _, err := m.ResolveWrite(filepath.Join(sysDir, "x.txt"), "t1"); err == nil {
		t.Fatalf("系统目录必须拒绝: %s", sysDir)
	}
	// 读：数据根可读（成品/配置），工作目录之外不可读
	if _, err := m.ResolveRead(filepath.Join(m.DataRoot(), "config.json"), "t1"); err != nil {
		t.Fatalf("数据根应可读: %v", err)
	}
	if _, err := m.ResolveRead(filepath.Join(root, "..", "x"), "t1"); err == nil {
		t.Fatal("越界读必须拒绝")
	}
}

func TestResolveWriteSymlinkEscape(t *testing.T) {
	m, _, _ := newManager(t)
	root := mkdir(t, filepath.Join(t.TempDir(), "proj-escape"))
	outside := mkdir(t, filepath.Join(t.TempDir(), "outside"))
	link := filepath.Join(root, "link-out")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前环境不支持符号链接: %v", err)
	}
	if _, err := m.ResolveWrite(filepath.Join(link, "evil.txt"), "t1"); err == nil {
		t.Fatal("软链接逃逸必须拒绝")
	}
}

func TestBuildContextAndPrompt(t *testing.T) {
	m, _, _ := newManager(t)
	root := mkdir(t, filepath.Join(t.TempDir(), "ctx"))
	writeFile(t, filepath.Join(root, "README.md"), "# 项目说明")
	writeFile(t, filepath.Join(root, "go.mod"), "module demo\n")
	writeFile(t, filepath.Join(root, "src", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "node_modules", "dep", "index.js"), "nope")
	writeFile(t, filepath.Join(root, "bin", "app.exe"), "MZ")
	cfg := m.Config()
	ci := BuildContext(root, cfg)
	if len(ci.Tree) == 0 {
		t.Fatal("应扫描到文件")
	}
	var paths []string
	for _, e := range ci.Tree {
		paths = append(paths, e.Path)
	}
	joined := strings.Join(paths, ",")
	if strings.Contains(joined, "node_modules") || strings.Contains(joined, "app.exe") {
		t.Fatalf("依赖目录/二进制不应出现在树里: %s", joined)
	}
	if !strings.Contains(joined, "README.md") || !strings.Contains(joined, "src/main.go") {
		t.Fatalf("应包含普通文件: %s", joined)
	}
	if len(ci.KeyFiles) == 0 || !strings.Contains(ci.KeyFiles[0].Content, "项目说明") {
		t.Fatalf("关键文件内容未采集: %+v", ci.KeyFiles)
	}
	blk := ci.PromptBlock()
	if !strings.Contains(blk, root) || !strings.Contains(blk, "README.md") || !strings.Contains(blk, "ARTIFACTS") {
		t.Fatalf("prompt 块内容不完整:\n%s", blk)
	}
	// 空目录
	empty := mkdir(t, filepath.Join(t.TempDir(), "empty"))
	blk2 := BuildContext(empty, cfg).PromptBlock()
	if !strings.Contains(blk2, "目录为空") {
		t.Fatalf("空目录应显式声明: %s", blk2)
	}
}

func TestBuildContextTruncates(t *testing.T) {
	root := mkdir(t, filepath.Join(t.TempDir(), "big"))
	big := strings.Repeat("x", 4000)
	for i := 0; i < 20; i++ {
		writeFile(t, filepath.Join(root, "f"+itoa(i)+".md"), "# t\n"+big)
	}
	cfg := Config{MaxFiles: 5, MaxBytes: 100, MaxFileBytes: 50}.Normalized()
	ci := BuildContext(root, cfg)
	if len(ci.Tree) != 5 {
		t.Fatalf("树应按 MaxFiles 截断: %d", len(ci.Tree))
	}
	if !ci.Truncated {
		t.Fatal("应标记截断")
	}
	var total int
	for _, kf := range ci.KeyFiles {
		total += len(kf.Content)
	}
	if int64(total) > cfg.MaxBytes {
		t.Fatalf("关键文件总量超限: %d > %d", total, cfg.MaxBytes)
	}
}

func TestAuditAppendReadTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "audit.jsonl")
	a, err := OpenAudit(path)
	if err != nil {
		t.Fatal(err)
	}
	a.Append(Record{Op: "write", Path: "C:\\x", Allowed: true})
	a.Append(Record{Op: "write", Path: "C:\\y", Allowed: false, Reason: "越界访问"})
	f, oerr := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if oerr != nil {
		t.Fatal(oerr)
	}
	_, _ = f.WriteString("not-json\\n")
	_ = f.Close()
	recs, err := ReadAudit(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("坏行应被跳过，实际 %d 条", len(recs))
	}
	tail := a.Tail(1)
	if len(tail) != 1 || tail[0].Allowed {
		t.Fatalf("Tail 应返回最新一条（拒绝）: %+v", tail)
	}
	s := Summarize(recs)
	if s.Total != 2 || s.Allowed != 1 || s.Denied != 1 || !strings.Contains(s.LastDeny, "越界") {
		t.Fatalf("汇总不对: %+v", s)
	}
	// nil 审计必须安全
	var nilAudit *AuditLog
	nilAudit.Append(Record{})
	if nilAudit.Tail(5) != nil {
		t.Fatal("nil 审计 Tail 应返回 nil")
	}
}

func TestSnapshot(t *testing.T) {
	m, _, data := newManager(t)
	writeFile(t, filepath.Join(data, "a.md"), "x")
	root := mkdir(t, filepath.Join(t.TempDir(), "snap"))
	writeFile(t, filepath.Join(root, "a.md"), "x")
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	a, err := OpenAudit(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	m.SetAudit(a)
	s := m.Snapshot()
	if s.Mode != ModeProject || s.Root == "" || len(s.Tree) == 0 {
		t.Fatalf("项目模式快照不完整: %+v", s)
	}
	if _, err := m.SelectExternal(root); err != nil {
		t.Fatal(err)
	}
	s = m.Snapshot()
	if s.Mode != ModeExternal || s.Validation == nil || !s.Validation.Allowed {
		t.Fatalf("external 快照应带校验结果: %+v", s)
	}
	if s.Audit.Total == 0 {
		t.Fatalf("审计摘要应统计到记录: %+v", s.Audit)
	}
}

func TestProbeWriteRejectsReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACL 下临时改只读不可靠")
	}
	if os.Geteuid() == 0 {
		// root 无视 0o500 权限位（容器里默认 root），此时该用例的**前置条件不成立**，
		// 不是被测逻辑失败 —— 显式跳过并说明原因，禁把它当绿。
		t.Skip("以 root 运行：权限位对 root 无效，无法构造只读目录")
	}
	dir := mkdir(t, filepath.Join(t.TempDir(), "ro"))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skip("无法改权限")
	}
	defer func() { _ = os.Chmod(dir, 0o755) }()
	if _, err := probeWrite(dir); err == nil {
		t.Fatal("只读目录应写入失败")
	}
}
