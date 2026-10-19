package workspace

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 上下文注入的忽略规则：这些目录/文件不进 prompt，避免把依赖与产物灌进模型。
var ignoreDirs = map[string]bool{
	"node_modules": true, ".git": true, ".svn": true, ".hg": true,
	"vendor": true, "dist": true, "build": true, "out": true, "target": true,
	"__pycache__": true, ".venv": true, "venv": true, ".idea": true, ".vscode": true,
	".next": true, ".nuxt": true, "coverage": true, ".pytest_cache": true,
	"bin": true, "obj": true, ".gradle": true, ".m2": true,
}

var ignoreExt = map[string]bool{
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".pdb": true, ".o": true, ".a": true,
	".class": true, ".jar": true, ".zip": true, ".7z": true, ".rar": true, ".tar": true, ".gz": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".ico": true, ".bmp": true,
	".mp3": true, ".mp4": true, ".mov": true, ".wav": true, ".pdf": true, ".docx": true, ".xlsx": true,
	".pptx": true, ".db": true, ".sqlite": true, ".log": true, ".lock": true,
}

// 视为"项目关键文件"的名字：优先注入内容，帮助模型快速理解项目。
var keyFiles = []string{
	"README.md", "go.mod", "package.json", "pyproject.toml",
	"requirements.txt", "Cargo.toml", "pom.xml", "build.gradle", "Makefile", "Dockerfile",
	"AGENTS.md", "CLAUDE.md", "tsconfig.json",
}

// Entry 是工作目录内的一个文件条目。
type Entry struct {
	Path  string `json:"path"`  // 相对工作目录，斜杠分隔
	Size  int64  `json:"size"`
	IsDir bool   `json:"isDir,omitempty"`
	Key   bool   `json:"key,omitempty"` // 是否项目关键文件
}

// TreeOptions 控制目录扫描。
type TreeOptions struct {
	MaxEntries int  // 最多返回多少条（默认 200）
	MaxDepth   int  // 最大深度（默认 6）
	DotDirs    bool // 是否包含隐藏目录
}

func (o TreeOptions) normalized() TreeOptions {
	if o.MaxEntries <= 0 {
		o.MaxEntries = 200
	}
	if o.MaxDepth <= 0 {
		o.MaxDepth = 6
	}
	return o
}

// Tree 扫描工作目录，返回稳定的条目列表（目录在前、同层按名字排序）。
// 软链接一律不跟随；越界条目直接跳过（防"链接指向外面"的信息泄漏）。
func Tree(root string, opt TreeOptions) []Entry {
	opt = opt.normalized()
	root = filepath.Clean(root)
	if root == "" {
		return nil
	}
	var out []Entry
	baseDepth := strings.Count(root, string(os.PathSeparator))
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || len(out) >= opt.MaxEntries {
			return nil
		}
		if path == root || info == nil {
			return nil
		}
		depth := strings.Count(path, string(os.PathSeparator)) - baseDepth
		if depth > opt.MaxDepth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		name := info.Name()
		if info.IsDir() {
			if ignoreDirs[strings.ToLower(name)] {
				return filepath.SkipDir
			}
			if !opt.DotDirs && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
		} else {
			if ignoreExt[strings.ToLower(filepath.Ext(name))] {
				return nil
			}
			if !opt.DotDirs && strings.HasPrefix(name, ".") {
				return nil
			}
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		e := Entry{Path: rel, IsDir: info.IsDir()}
		if !info.IsDir() {
			e.Size = info.Size()
			e.Key = isKeyFile(rel)
		}
		out = append(out, e)
		if len(out) >= opt.MaxEntries {
			return filepath.SkipAll
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		ad := strings.Count(a.Path, "/")
		bd := strings.Count(b.Path, "/")
		if ad != bd {
			return ad < bd
		}
		return a.Path < b.Path
	})
	return out
}

func isKeyFile(rel string) bool {
	name := filepath.Base(rel)
	for _, k := range keyFiles {
		if strings.EqualFold(name, k) {
			return true
		}
	}
	return false
}

// ContextInfo 是注入到任务 prompt 的工作目录上下文。
type ContextInfo struct {
	Mode      Mode      `json:"mode"`
	Root      string    `json:"root"`
	Tree      []Entry   `json:"tree"`
	KeyFiles  []KeyFile `json:"keyFiles"`
	Entries   int       `json:"entries"`
	Truncated bool      `json:"truncated"`
}

// KeyFile 是关键文件的内容快照（已截断）。
type KeyFile struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
}

// BuildContext 采集工作目录上下文：目录树 + 关键文件内容。
//
// 设计目的：external 模式下模型看不到沙箱，必须先把"真实项目长什么样"喂给它，
// 否则它只会凭空输出文件清单。采集有硬上限：文件数、单文件字节、总字节，
// 保证 prompt 不会把模型上下文撑爆。
func BuildContext(root string, cfg Config) ContextInfo {
	cfg = cfg.Normalized()
	info := ContextInfo{Mode: cfg.Mode, Root: root}
	if root == "" {
		return info
	}
	entries := Tree(root, TreeOptions{MaxEntries: cfg.MaxFiles * 4})
	info.Tree = entries
	info.Entries = len(entries)
	if len(info.Tree) > cfg.MaxFiles {
		info.Tree = info.Tree[:cfg.MaxFiles]
		info.Truncated = true
	}
	var total int64
	for _, e := range entries {
		if !e.Key || total >= cfg.MaxBytes {
			continue
		}
		if len(info.KeyFiles) >= 12 {
			break
		}
		p := filepath.Join(root, filepath.FromSlash(e.Path))
		resolved, rerr := evalExisting(p)
		if rerr == nil && !IsWithin(root, resolved) {
			continue // 软链接逃逸：不读
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		body := data
		trunc := false
		if int64(len(body)) > cfg.MaxFileBytes {
			body = body[:cfg.MaxFileBytes]
			trunc = true
		}
		if total+int64(len(body)) > cfg.MaxBytes {
			if total >= cfg.MaxBytes {
				continue
			}
			body = body[:cfg.MaxBytes-total]
			trunc = true
		}
		total += int64(len(body))
		info.KeyFiles = append(info.KeyFiles, KeyFile{Path: e.Path, Content: string(body), Truncated: trunc})
	}
	return info
}

// PromptBlock 把上下文渲染成注入 prompt 的文本块。
//
// 这是 external 模式"让模型知道自己在哪个真实项目里干活"的唯一通道：
// 块里显式声明工作目录、禁写区、可见文件树与关键文件内容。
func (ci ContextInfo) PromptBlock() string {
	if ci.Root == "" {
		return ""
	}
	fence := strings.Repeat("`", 3)
	var b strings.Builder
	b.WriteString("## 工作目录上下文（由工作台自动注入）\n\n")
	b.WriteString("- 当前工作目录（真实绝对路径）：" + ci.Root + "\n")
	b.WriteString("- 模式：" + string(ci.Mode) + "（external = 用户选定的本地项目目录，你的文件操作直接作用于此目录）\n")
	b.WriteString("- 安全边界：只能读写上述目录及其子目录；禁止访问系统目录、工作台自身目录与其它盘的任何路径。\n")
	b.WriteString("- 产出协议不变：仍然在回答中用 <<<ARTIFACTS>>> 块给出文件清单与内容，工作台会把这些文件落盘到该工作目录并真实执行 verify 命令。\n")
	if len(ci.Tree) > 0 {
		b.WriteString("\n### 现有文件树" + ternary(ci.Truncated, "（已截断，仅列前 "+itoa(len(ci.Tree))+" 项）", "") + "\n" + fence + "\n")
		for _, e := range ci.Tree {
			if e.IsDir {
				b.WriteString(e.Path + "/\n")
			} else {
				b.WriteString(e.Path + "\n")
			}
		}
		b.WriteString(fence + "\n")
	} else {
		b.WriteString("\n### 现有文件树\n（目录为空：这是一个全新项目，可以从零开始创建文件）\n")
	}
	if len(ci.KeyFiles) > 0 {
		b.WriteString("\n### 关键文件内容\n")
		for _, kf := range ci.KeyFiles {
			b.WriteString("\n#### " + kf.Path + ternary(kf.Truncated, "（内容已截断）", "") + "\n" + fence + "\n")
			b.WriteString(kf.Content)
			if !strings.HasSuffix(kf.Content, "\n") {
				b.WriteString("\n")
			}
			b.WriteString(fence + "\n")
		}
	}
	b.WriteString("\n注意：修改现有文件时必须保留其原有功能，不要删除与本任务无关的文件。\n")
	return b.String()
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Snapshot 是工作目录校验结果 + 文件树 + 审计摘要的快照（面板 GET 用）。
type Snapshot struct {
	Mode        Mode        `json:"mode"`
	Root        string      `json:"root"`
	DataRoot    string      `json:"dataRoot"`
	ProjectRoot string      `json:"projectRoot"`
	Validation  *Validation `json:"validation,omitempty"`
	Tree        []Entry     `json:"tree,omitempty"`
	Entries     int         `json:"entries"`
	Truncated   bool        `json:"treeTruncated,omitempty"`
	Audit       Summary     `json:"audit"`
	AuditPath   string      `json:"auditPath,omitempty"`
	At          int64       `json:"at"`
}

// Snapshot 构造面板展示用的快照（不修改任何状态）。
func (m *Manager) Snapshot() Snapshot {
	c := m.Config()
	s := Snapshot{
		Mode:        c.Mode,
		Root:        m.Root(),
		DataRoot:    m.dataRoot,
		ProjectRoot: m.projectRoot,
		At:          time.Now().UnixMilli(),
	}
	if c.Mode == ModeExternal {
		s.Validation = m.LastValidation()
	}
	entries := Tree(s.Root, TreeOptions{MaxEntries: 400})
	s.Entries = len(entries)
	if len(entries) > 200 {
		entries = entries[:200]
		s.Truncated = true
	}
	s.Tree = entries
	if m.audit != nil {
		s.AuditPath = m.audit.Path()
		recs, err := ReadAudit(m.audit.Path())
		if err == nil {
			s.Audit = Summarize(recs)
		}
	}
	return s
}
