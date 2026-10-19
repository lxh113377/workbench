package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Layout 描述工作台在磁盘上的全部固定路径。运行时一切读写都必须经由此结构，
// 禁止在业务包里拼接裸路径字符串。
type Layout struct {
	Root        string // EXE 所在目录（便携根）
	DataRoot    string // <Root>\workbench-output
	ConfigFile  string // <DataRoot>\config.json
	QueueFile   string // <DataRoot>\queue.json
	StateFile   string // <DataRoot>\state.json
	// WALFile 是队列状态的追加日志（P2-3）：每次迁移追加一行，定期压缩进 QueueFile。
	WALFile string // <DataRoot>\queue.wal.jsonl
	TasksDir    string // <DataRoot>\tasks
	LogsDir     string // <DataRoot>\logs
	ExportsDir  string // <DataRoot>\exports
	GovDir      string // <DataRoot>\governance （契约治理 × 体量治理数据）
	AutonomyDir string // <DataRoot>\autonomy （自主目标树 goals.json + 决策台账 decisions.jsonl）
	RuntimeDir  string // <Root>\runtime
	DSHHome     string // <RuntimeDir>\dsh-home  （隔离的 DSH_HOME，绝不污染用户 ~/.dsh）
	// WorkspaceAuditFile 是「AI 开发工作目录」的路径裁决审计（允许/拒绝都记）。
	WorkspaceAuditFile string // <DataRoot>\workspace-audit.jsonl
}

// ResolveLayout 依据可执行文件所在目录推导布局；dataRootOverride 非空时覆盖数据根。
func ResolveLayout(dataRootOverride string) (Layout, error) {
	exe, err := os.Executable()
	if err != nil {
		return Layout{}, fmt.Errorf("无法定位可执行文件: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		exe, err = os.Executable()
		if err != nil {
			return Layout{}, err
		}
	}
	return BuildLayout(filepath.Dir(exe), dataRootOverride)
}

// BuildLayout 在给定根目录上构造布局，便于测试注入。
func BuildLayout(root, dataRootOverride string) (Layout, error) {
	if strings.TrimSpace(root) == "" {
		return Layout{}, fmt.Errorf("根目录为空")
	}
	root = filepath.Clean(root)
	dataRoot := strings.TrimSpace(dataRootOverride)
	if dataRoot == "" {
		dataRoot = filepath.Join(root, "workbench-output")
	}
	l := Layout{
		Root:       root,
		DataRoot:   filepath.Clean(dataRoot),
		RuntimeDir: filepath.Join(root, "runtime"),
	}
	l.ConfigFile = filepath.Join(l.DataRoot, "config.json")
	l.QueueFile = filepath.Join(l.DataRoot, "queue.json")
	l.StateFile = filepath.Join(l.DataRoot, "state.json")
	l.WALFile = filepath.Join(l.DataRoot, "queue.wal.jsonl")
	l.TasksDir = filepath.Join(l.DataRoot, "tasks")
	l.LogsDir = filepath.Join(l.DataRoot, "logs")
	l.ExportsDir = filepath.Join(l.DataRoot, "exports")
	l.GovDir = filepath.Join(l.DataRoot, "governance")
	l.WorkspaceAuditFile = filepath.Join(l.DataRoot, "workspace-audit.jsonl")
	l.AutonomyDir = filepath.Join(l.DataRoot, "autonomy")
	l.DSHHome = filepath.Join(l.RuntimeDir, "dsh-home")
	return l, nil
}

// EnsureDirs 创建全部必需目录（幂等）。
func (l Layout) EnsureDirs() error {
	dirs := []string{l.DataRoot, l.TasksDir, l.LogsDir, l.ExportsDir, l.GovDir, l.AutonomyDir, l.RuntimeDir, l.DSHHome}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", d, err)
		}
	}
	return nil
}

// TaskDir 返回单个任务的独立工作目录。
func (l Layout) TaskDir(taskID string) string { return filepath.Join(l.TasksDir, taskID) }

// TaskFile 返回任务目录内的具名文件路径。
func (l Layout) TaskFile(taskID, name string) string { return filepath.Join(l.TaskDir(taskID), name) }

// LogFile 返回日志目录内的具名文件路径。
func (l Layout) LogFile(name string) string { return filepath.Join(l.LogsDir, name) }

// RuntimeFile 返回 runtime 目录内的具名文件路径（进程级状态，如模型健康度）。
func (l Layout) RuntimeFile(name string) string { return filepath.Join(l.RuntimeDir, name) }

// AutonomyFile 返回自主子系统目录内的具名文件路径（goals.json / decisions.jsonl）。
func (l Layout) AutonomyFile(name string) string { return filepath.Join(l.AutonomyDir, name) }
