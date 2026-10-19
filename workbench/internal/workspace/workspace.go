// Package workspace 是「AI 开发工作目录」的唯一定义与护栏实现。
//
// 背景：改造前任务的工作目录硬编码为 <DataRoot>/tasks/<id>/workspace，用户无法
// 让 AI 在自己选定的项目文件夹里开发。本包把「工作目录」抽成可切换的两态模式：
//
//	project  （默认，兼容旧行为）：每个任务一个隔离沙箱目录，位于工作台数据根下；
//	external （可选新模式）：所有任务直接以用户选定的本地文件夹为工作目录。
//
// 安全边界（本包的核心职责，不是可选项）：
//  1. 选定目录必须通过 Validate：绝对路径、真实存在的目录、可读写、非系统目录、
//     非工作台项目目录/数据目录、非用户主目录本身；符号链接先解析再判定。
//  2. 所有落盘/读取都必须经 ResolveWrite / ResolveRead：路径归一化 + 软链接解析后
//     再做包含性判定（IsWithin），越界一律拒绝并写审计。
//  3. 系统目录（Windows / Program Files / ProgramData / System32）在任何模式下都
//     写不进去，作为纵深防御。
//  4. 每次允许/拒绝都追加 workspace-audit.jsonl，可被面板与 CLI 复核。
//
// 本包只依赖标准库，且不感知 HTTP/引擎，便于单测与复用。
package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Mode 是工作目录模式。
type Mode string

const (
	// ModeProject 是默认模式：任务在数据根下的沙箱目录中执行（兼容旧行为）。
	ModeProject Mode = "project"
	// ModeExternal 是可选模式：任务直接以用户选定的本地文件夹为工作目录。
	ModeExternal Mode = "external"
)

// ParseMode 解析模式字符串，非法值回落 project。
func ParseMode(s string) Mode {
	if Mode(strings.ToLower(strings.TrimSpace(s))) == ModeExternal {
		return ModeExternal
	}
	return ModeProject
}

// Config 是工作目录配置（持久化在 config.json 的 workspace 段）。
type Config struct {
	Mode             Mode     `json:"mode"`
	Root             string   `json:"root"`             // 用户选定的绝对目录（external 模式生效）
	AllowedReadRoots []string `json:"allowedReadRoots"` // 额外只读根（默认含数据根，供成品读取）
	MaxFiles         int      `json:"maxFiles"`         // 上下文注入文件数上限
	MaxBytes         int64    `json:"maxBytes"`         // 上下文注入总字节上限
	MaxFileBytes     int64    `json:"maxFileBytes"`     // 单文件上限
	AllowDotDirs     bool     `json:"allowDotDirs"`     // 是否扫隐藏目录（默认否）
	Since            int64    `json:"since"`            // 最近一次切换时间（毫秒）
}

// DefaultConfig 返回默认（项目模式）配置。
func DefaultConfig() Config {
	return Config{
		Mode:         ModeProject,
		MaxFiles:     80,
		MaxBytes:     512 * 1024,
		MaxFileBytes: 128 * 1024,
	}
}

// Normalized 把越界/空值钳制到安全默认值。
func (c Config) Normalized() Config {
	d := DefaultConfig()
	c.Mode = ParseMode(string(c.Mode))
	if c.MaxFiles <= 0 || c.MaxFiles > 2000 {
		c.MaxFiles = d.MaxFiles
	}
	if c.MaxBytes <= 0 || c.MaxBytes > 16<<20 {
		c.MaxBytes = d.MaxBytes
	}
	if c.MaxFileBytes <= 0 || c.MaxFileBytes > 4<<20 {
		c.MaxFileBytes = d.MaxFileBytes
	}
	c.Root = strings.TrimSpace(c.Root)
	roots := make([]string, 0, len(c.AllowedReadRoots))
	seen := map[string]bool{}
	for _, r := range c.AllowedReadRoots {
		r = strings.TrimSpace(r)
		if r == "" || !filepath.IsAbs(r) {
			continue
		}
		r = filepath.Clean(r)
		key := strings.ToLower(r)
		if seen[key] {
			continue
		}
		seen[key] = true
		roots = append(roots, r)
	}
	c.AllowedReadRoots = roots
	return c
}

// Validation 是一次目录校验的完整结果（面板与 CLI 共用）。
type Validation struct {
	Input       string   `json:"input"`
	Canonical   string   `json:"canonical,omitempty"`
	Exists      bool     `json:"exists"`
	IsDir       bool     `json:"isDir"`
	Readable    bool     `json:"readable"`
	Writable    bool     `json:"writable"`
	Allowed     bool     `json:"allowed"`
	Reason      string   `json:"reason,omitempty"`
	Suggestion  string   `json:"suggestion,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	Entries     int      `json:"entries"`
	Files       int      `json:"files"`
	Dirs        int      `json:"dirs"`
	SampleFiles []string `json:"sampleFiles,omitempty"`
	Mode        Mode     `json:"mode"`
	Forbidden   bool     `json:"forbidden"`
	CheckedAt   int64    `json:"checkedAt"`
}

// Manager 持有当前工作目录配置，并提供校验、解析与审计。
type Manager struct {
	mu          sync.RWMutex
	cfg         Config
	dataRoot    string // 工作台数据根（项目模式的沙箱根）
	projectRoot string // 工作台可执行文件根（永不允许成为外部工作目录）
	audit       *AuditLog
	lastValid   *Validation
}

// New 构造管理器。dataRoot/projectRoot 由调用方按真实布局注入。
func New(dataRoot, projectRoot string, cfg Config) *Manager {
	m := &Manager{
		cfg:         cfg.Normalized(),
		dataRoot:    filepath.Clean(dataRoot),
		projectRoot: filepath.Clean(projectRoot),
	}
	if strings.TrimSpace(m.dataRoot) == "." {
		m.dataRoot = ""
	}
	return m
}

// SetAudit 注入审计日志（可为空，空则不落审计）。
func (m *Manager) SetAudit(a *AuditLog) { m.audit = a }

// Config 返回配置副本。
func (m *Manager) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := m.cfg
	cp.AllowedReadRoots = append([]string(nil), m.cfg.AllowedReadRoots...)
	return cp
}

// Mode 返回当前模式。
func (m *Manager) Mode() Mode { return m.Config().Mode }

// IsExternal 判断是否处于外部目录模式。
func (m *Manager) IsExternal() bool { return m.Mode() == ModeExternal }

// DataRoot 返回数据根。
func (m *Manager) DataRoot() string { return m.dataRoot }

// ProjectRoot 返回项目根。
func (m *Manager) ProjectRoot() string { return m.projectRoot }

// Root 返回当前生效的可写根：external 为用户目录；project 为数据根。
func (m *Manager) Root() string {
	c := m.Config()
	if c.Mode == ModeExternal && c.Root != "" {
		return c.Root
	}
	return m.dataRoot
}

// LastValidation 返回最近一次成功校验结果（面板展示用）。
func (m *Manager) LastValidation() *Validation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.lastValid == nil {
		return nil
	}
	cp := *m.lastValid
	cp.Warnings = append([]string(nil), m.lastValid.Warnings...)
	cp.SampleFiles = append([]string(nil), m.lastValid.SampleFiles...)
	return &cp
}

// SetConfig 校验并应用新配置。切换到 external 模式时必须给出合法 Root。
func (m *Manager) SetConfig(cfg Config) error {
	cfg = cfg.Normalized()
	var validated *Validation
	if cfg.Mode == ModeExternal {
		// 校验必须在**取写锁之前**完成：Validate → validateNoAudit → Mode() → Config() 会取读锁，
		// 而 sync.RWMutex 不可重入 —— 持写锁内再取读锁即永久死锁（2026-10-19 容器轮实测：
		// 配置为 external 时工作台启动即挂死，panel.url 永不落盘、零日志输出）。
		v, err := m.Validate(cfg.Root)
		if err != nil {
			return err
		}
		validated = v
		cfg.Root = v.Canonical
		if cfg.Since == 0 {
			cfg.Since = time.Now().UnixMilli()
		}
	}
	// 写锁内只做纯赋值：不得调用任何会重新加锁的方法。
	m.mu.Lock()
	m.cfg = cfg
	if validated != nil {
		m.lastValid = validated
	}
	m.mu.Unlock()
	if validated != nil {
		// 审计写在锁外：Append 自带独立互斥，且不应阻塞配置读取。
		m.audit.Append(Record{Op: "select", Path: validated.Canonical, Root: validated.Canonical, Mode: string(ModeExternal), Allowed: true, Reason: "用户选定外部工作目录"})
	}
	return nil
}

// SelectExternal 校验并切换到外部目录模式。
func (m *Manager) SelectExternal(path string) (*Validation, error) {
	v, err := m.Validate(path)
	if err != nil {
		return v, err
	}
	m.mu.Lock()
	c := m.cfg
	c.Mode = ModeExternal
	c.Root = v.Canonical
	c.Since = time.Now().UnixMilli()
	m.cfg = c.Normalized()
	m.lastValid = v
	m.mu.Unlock()
	m.audit.Append(Record{Op: "select", Path: v.Canonical, Root: v.Canonical, Mode: string(ModeExternal), Allowed: true, Reason: "用户选定外部工作目录"})
	return v, nil
}

// ResetProject 切回项目模式（保留上次选定的目录以便再次切回）。
func (m *Manager) ResetProject() Config {
	m.mu.Lock()
	m.cfg.Mode = ModeProject
	m.cfg.Since = time.Now().UnixMilli()
	cfg := m.cfg
	m.lastValid = nil
	m.mu.Unlock()
	m.audit.Append(Record{Op: "reset", Path: cfg.Root, Root: m.dataRoot, Mode: string(ModeProject), Allowed: true, Reason: "切回项目模式"})
	return m.Config()
}

// Validate 校验一个候选目录是否可作为外部工作目录。
//
// 判定顺序：绝对路径 → 存在 → 是目录 → 非系统目录 → 与项目/数据目录无重叠 →
// 可读 → 可写（真实写探针）→ 统计入口。任一步失败都给出人读原因与建议。
func (m *Manager) Validate(path string) (*Validation, error) {
	v, err := m.validateNoAudit(path)
	if v != nil && v.Allowed {
		m.audit.Append(Record{Op: "validate", Path: v.Canonical, Root: m.Root(), Mode: string(m.Mode()), Allowed: true, Reason: "校验通过"})
	} else {
		p := path
		if v != nil && v.Canonical != "" {
			p = v.Canonical
		}
		reason := ""
		if err != nil {
			reason = err.Error()
		}
		m.audit.Append(Record{Op: "validate", Path: p, Root: m.Root(), Mode: string(m.Mode()), Allowed: false, Reason: reason})
	}
	return v, err
}

func (m *Manager) validateNoAudit(path string) (*Validation, error) {
	v := &Validation{Input: path, CheckedAt: time.Now().UnixMilli(), Mode: m.Mode()}
	raw := strings.TrimSpace(path)
	if raw == "" {
		v.Reason = "目录为空"
		return v, errors.New("目录为空")
	}
	if strings.ContainsRune(raw, 0) {
		v.Reason = "路径含非法字符"
		return v, errors.New("路径含非法字符")
	}
	if !filepath.IsAbs(raw) {
		v.Reason = "必须是绝对路径"
		v.Suggestion = "例如 D:\\projects\\my-app"
		return v, fmt.Errorf("必须是绝对路径: %s", raw)
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		v.Reason = "路径无法解析: " + err.Error()
		return v, err
	}
	abs = filepath.Clean(abs)
	v.Canonical = abs
	st, err := os.Stat(abs)
	if err != nil {
		v.Reason = "目录不存在"
		v.Suggestion = "可在选择器里新建该文件夹，或换一个已存在的目录"
		return v, fmt.Errorf("目录不存在: %s", abs)
	}
	if !st.IsDir() {
		v.Reason = "这是文件不是目录"
		v.Suggestion = "请选择它所在的文件夹"
		v.Suggestion = "请选择它所在的文件夹: " + filepath.Dir(abs)
		return v, fmt.Errorf("不是目录: %s", abs)
	}
	v.Exists = true
	v.IsDir = true
	// 软链接解析后再判定，防止「选了个链接目录、实际写到别处」。
	canon, cerr := evalExisting(abs)
	if cerr == nil {
		v.Canonical = canon
	}
	if reason, bad := m.forbidden(v.Canonical, true); bad {
		v.Forbidden = true
		v.Reason = reason
		v.Suggestion = "请选择磁盘上的普通项目文件夹（不要选系统目录、工作台自身目录或用户主目录）"
		return v, errors.New(reason)
	}
	if err := probeRead(v.Canonical); err != nil {
		v.Reason = "目录不可读: " + err.Error()
		return v, err
	}
	v.Readable = true
	probe, perr := probeWrite(v.Canonical)
	if perr != nil {
		v.Reason = "目录不可写: " + perr.Error()
		v.Suggestion = "请换一个当前用户有写权限的文件夹，或先修改文件夹权限"
		return v, perr
	}
	v.Writable = true
	_ = probe
	if name := filepath.Base(v.Canonical); strings.HasPrefix(name, ".") {
		v.Warnings = append(v.Warnings, "目录名以点开头，可能是隐藏目录")
	}
	if runtime.GOOS == "windows" && strings.HasPrefix(v.Canonical, "\\\\") {
		v.Warnings = append(v.Warnings, "网络共享目录：读写速度与权限取决于远端")
	}
	entries, err := os.ReadDir(v.Canonical)
	if err == nil {
		v.Entries = len(entries)
		for _, e := range entries {
			if e.IsDir() {
				v.Dirs++
				continue
			}
			v.Files++
			if len(v.SampleFiles) < 5 {
				v.SampleFiles = append(v.SampleFiles, e.Name())
			}
		}
	}
	v.Allowed = true
	m.mu.Lock()
	m.lastValid = v
	m.mu.Unlock()
	return v, nil
}

// forbidden 判断路径是否落在禁止区域。返回 (原因, 是否禁止)。
func (m *Manager) forbidden(p string, selecting bool) (string, bool) {
	if p == "" {
		return "目录为空", true
	}
	// 1) 盘符根 / 文件系统根：允许往里写等于允许写整块盘。
	if isVolumeRoot(p) {
		return "不能把磁盘根目录作为工作目录", true
	}
	// 2) 系统目录（含其子目录）。
	for _, sys := range systemDirs() {
		if sys == "" {
			continue
		}
		if IsWithin(sys, p) {
			return "不能把系统目录作为工作目录: " + sys, true
		}
	}
	// 3) 用户主目录本身（允许其子目录）。
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if samePath(home, p) {
			return "不能把用户主目录本身作为工作目录", true
		}
	}
	// 4) 工作台项目根：不允许选中或写入；唯一例外是工作台自己的数据根
	//    （任务沙箱、事件流、成品归档都在里面，运行期必须可读写）。
	inData := m.dataRoot != "" && IsWithin(m.dataRoot, p)
	if pr := m.projectRoot; pr != "" && !inData {
		if IsWithin(pr, p) {
			return "不能选择工作台自身目录或其子目录（会破坏工作台数据）: " + pr, true
		}
		if selecting && IsWithin(p, pr) {
			return "不能选择工作台自身目录或其父目录（会破坏工作台数据）: " + pr, true
		}
	}
	// 5) 数据根：只禁止"被选为外部工作目录"，不禁止作为运行期读写目标。
	if selecting && m.dataRoot != "" && (IsWithin(m.dataRoot, p) || IsWithin(p, m.dataRoot)) {
		return "不能选择工作台数据目录或其父目录（会破坏产出归档）: " + m.dataRoot, true
	}
	// 5) 临时目录根（%TEMP% 本身）：允许其子目录，禁止根本身。
	if tmp := os.TempDir(); tmp != "" && samePath(tmp, p) {
		return "不能把系统临时目录根作为工作目录", true
	}
	return "", false
}

// systemDirs 返回必须拒绝的系统目录清单。
func systemDirs() []string {
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, filepath.Clean(p))
		}
	}
	if runtime.GOOS == "windows" {
		add(os.Getenv("WINDIR"))
		add(os.Getenv("SystemRoot"))
		add(os.Getenv("ProgramFiles"))
		add(os.Getenv("ProgramFiles(x86)"))
		add(os.Getenv("ProgramData"))
		add(os.Getenv("SystemDrive") + "\\Windows")
		if sd := os.Getenv("SystemDrive"); sd != "" {
			add(sd + "\\Program Files")
			add(sd + "\\Program Files (x86)")
		}
	} else {
		add("/etc")
		add("/usr")
		add("/bin")
		add("/sbin")
		add("/boot")
		add("/System")
		add("/Library")
	}
	return out
}

// isVolumeRoot 判断是否是盘符根（C:\）或文件系统根（/）。
func isVolumeRoot(p string) bool {
	clean := filepath.Clean(p)
	if clean == string(os.PathSeparator) {
		return true
	}
	vol := filepath.VolumeName(clean)
	if vol == "" {
		return false
	}
	return samePath(clean, vol+string(os.PathSeparator)) || clean == vol
}

// probeWrite 真实写一个临时文件再删除，验证目录确实可写。
func probeWrite(dir string) (string, error) {
	name := ".wb-write-probe-" + randToken(6) + ".tmp"
	p := filepath.Join(dir, name)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("写入探针失败: %w", err)
	}
	_, werr := f.WriteString("workbench write probe " + time.Now().Format(time.RFC3339))
	cerr := f.Close()
	rmErr := os.Remove(p)
	if werr != nil {
		return "", fmt.Errorf("写入探针失败: %w", werr)
	}
	if cerr != nil {
		return "", fmt.Errorf("写入探针失败: %w", cerr)
	}
	if rmErr != nil {
		return "", fmt.Errorf("清理探针失败（目录可能被占用）: %w", rmErr)
	}
	return p, nil
}

func probeRead(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, ioEOF) {
		// 空目录的 Readdirnames(1) 返回 io.EOF，属正常
		if !strings.Contains(err.Error(), "EOF") {
			return err
		}
	}
	return nil
}

var ioEOF = errors.New("EOF")

// TaskWorkDir 返回任务的工作目录：
//   - project 模式：<DataRoot>/tasks/<id>/workspace（与改造前完全一致）
//   - external 模式：用户选定目录本身（AI 直接在真实项目文件夹里开发）
func (m *Manager) TaskWorkDir(taskID string) (string, bool) {
	c := m.Config()
	if c.Mode == ModeExternal && c.Root != "" {
		return c.Root, false
	}
	return filepath.Join(m.dataRoot, "tasks", taskID, "workspace"), true
}

// ResolveWrite 解析并校验一个写目标：越界/系统目录一律拒绝。
// 返回规范化后的绝对路径；taskID 仅用于审计。
func (m *Manager) ResolveWrite(target, taskID string) (string, error) {
	return m.resolve(target, taskID, "write")
}

// ResolveRead 解析并校验一个读目标（允许根 + 额外只读根）。
func (m *Manager) ResolveRead(target, taskID string) (string, error) {
	return m.resolve(target, taskID, "read")
}

func (m *Manager) resolve(target, taskID, op string) (string, error) {
	raw := strings.TrimSpace(target)
	if raw == "" {
		return "", errors.New("路径为空")
	}
	var abs string
	if filepath.IsAbs(raw) {
		abs = filepath.Clean(raw)
	} else {
		root := m.Root()
		if root == "" {
			return "", errors.New("工作目录未初始化")
		}
		abs = filepath.Join(root, raw)
	}
	abs, _ = filepath.Abs(abs)
	resolved, err := evalExisting(abs)
	if err != nil {
		resolved = abs
	}
	if reason, bad := m.forbidden(resolved, false); bad {
		m.audit.Append(Record{Op: op, Path: resolved, Root: m.Root(), TaskID: taskID, Mode: string(m.Mode()), Allowed: false, Reason: reason})
		return "", fmt.Errorf("路径被拒绝（%s）: %s", reason, resolved)
	}
	roots := m.readRootsLocked(op)
	ok := false
	for _, r := range roots {
		if IsWithin(r, resolved) {
			ok = true
			break
		}
	}
	if !ok {
		reason := "越界访问：目标不在授权工作目录内"
		m.audit.Append(Record{Op: op, Path: resolved, Root: m.Root(), TaskID: taskID, Mode: string(m.Mode()), Allowed: false, Reason: reason})
		return "", fmt.Errorf("%s: %s（授权根 %s）", reason, resolved, strings.Join(roots, ", "))
	}
	m.audit.Append(Record{Op: op, Path: resolved, Root: m.Root(), TaskID: taskID, Mode: string(m.Mode()), Allowed: true})
	return resolved, nil
}

// readRootsLocked 返回当前允许读的根集合：可写根 + 额外只读根 + 数据根。
func (m *Manager) readRootsLocked(op string) []string {
	m.mu.RLock()
	cfg := m.cfg
	m.mu.RUnlock()
	var roots []string
	root := m.dataRoot
	if cfg.Mode == ModeExternal && cfg.Root != "" {
		root = cfg.Root
	}
	if root != "" {
		roots = append(roots, root)
	}
	// 项目模式下数据根本身就是可写根（任务沙箱在其下）；external 模式下
	// 数据根只作为可读根（成品归档），写权限仍锁在用户目录里。
	if op == "read" || cfg.Mode != ModeExternal {
		if m.dataRoot != "" && !samePath(m.dataRoot, root) {
			roots = append(roots, m.dataRoot)
		}
	}
	if op == "read" {
		for _, extra := range cfg.AllowedReadRoots {
			dup := false
			for _, r := range roots {
				if samePath(r, extra) {
					dup = true
					break
				}
			}
			if !dup {
				roots = append(roots, extra)
			}
		}
	}
	return roots
}

// IsWithin 判断 target 是否位于 root 之内（含相等）。Windows 下大小写不敏感，
// 且按路径分隔符边界比较，避免 C:\foo 误匹配 C:\foobar。
func IsWithin(root, target string) bool {
	if root == "" || target == "" {
		return false
	}
	r := normForCompare(root)
	t := normForCompare(target)
	if r == t {
		return true
	}
	if !strings.HasSuffix(r, string(os.PathSeparator)) {
		r += string(os.PathSeparator)
	}
	return strings.HasPrefix(t, r)
}

func normForCompare(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func samePath(a, b string) bool { return normForCompare(a) == normForCompare(b) }

// evalExisting 解析路径中「已存在部分」的软链接，拼接尚不存在的尾部。
// 用于阻止「工作目录内放一个指向外面的软链接」这种逃逸。
func evalExisting(p string) (string, error) {
	p = filepath.Clean(p)
	var tail []string
	cur := p
	for i := 0; i < 64; i++ {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			for j := len(tail) - 1; j >= 0; j-- {
				resolved = filepath.Join(resolved, tail[j])
			}
			return resolved, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
	return p, nil
}

func randToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// SortValidationFiles 对样例文件排序（稳定输出，便于测试与面板展示）。
func SortValidationFiles(v *Validation) {
	if v == nil {
		return
	}
	sort.Strings(v.SampleFiles)
}

// AuditPath 返回审计文件路径（未配置审计时为空串）。
func (m *Manager) AuditPath() string {
	if m.audit == nil {
		return ""
	}
	return m.audit.Path()
}
