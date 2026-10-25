// Package retention 实现运行时产物的保留策略（DSH_HOME 任务副本回收）。
//
// 问题（2026-10-19 现场审计）：
//
//	executor 每执行一个任务就创建一份独立 DSH_HOME（为了并发隔离，这是必须的）。
//	但**从未有回收机制** —— 现场 24 小时内积累了 247 个副本、26.3MB。
//	按 7×24 每天约 100 个任务估算：
//	  1 周后 ≈ 1000 个副本 / 100MB
//	  1 月后 ≈ 3000 个副本 / 300MB
//	  1 年后 ≈ 36000 个副本 / 3.6GB
//	而且每份都含 OFM 的账号态文件、profile junction、sessions 日志。
//
// 安全边界（项目硬约束 5「所有删除走回收站」）：
//   - 只回收**任务级 DSH_HOME 副本**（runtime/dsh-home/tasks/<id>/）；
//   - 绝不触碰共享 profile（profiles/web-desktop）与 OFM 安装目录；
//   - 绝不删除任何产出（workbench-output/tasks/<id>/ 的 final.md 等是用户资产）；
//   - 删除走 Windows 回收站（SHFileOperationW + FOF_ALLOWUNDO），可反悔；
//   - 保留窗口内的副本不动，即使它属于失败任务。
package retention

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Defaults 是保留策略的默认值。
const (
	// DefaultDSHHomeDays 是任务 DSH_HOME 副本的保留天数。
	// 取 3 天：足够覆盖"昨天失败的任务今天重试"的场景，又不至于无限堆积。
	DefaultDSHHomeDays = 3
	// DefaultWorkspaceDays 是任务 workspace（模型产出文件）的保留天数。
	// 取 30 天：workspace 里的文件是任务产出的主体，保留久一些。
	DefaultWorkspaceDays = 30
	// DefaultTaskDirDays 是任务目录（events.jsonl / final.md 等）的保留天数。
	// 取 90 天：这些是审计与成果资产，保留最久。
	DefaultTaskDirDays = 90
	// DefaultTerminalTaskDays 是**终态任务记录**（queue.json 里的一行）的保留天数。
	//
	// 为什么需要（2026-10-23 实测）：queue.json 已达 **59MB / 33,740 个任务**，
	// 其中 33,733 条是终态（33,187 条 rejected —— 空转热循环留下的），在跑任务只有 7 条。
	// store 此前**没有任何删除任务的 API**：retention 只清 tasks/ 目录，记录本身只增不减。
	// 后果不是磁盘（59MB 不算大），而是**每次快照全量序列化 59MB + 内存常驻 3.3 万对象**
	// —— 实测常驻 170MB、并发高峰 752MB，而基线 residentMBMax 是 64MB。
	// 取 7 天：足够覆盖「上周怎么失败的」这类追溯，又不让 rejected 无限堆积。
	DefaultTerminalTaskDays = 7
	// DefaultMaxTerminalTasks 是终态任务记录的**条数上限**（硬闸，与天数取交集）。
	// 为什么天数不够：热循环可在数小时内产生数万条 rejected，天数窗口内照样能撑爆快照。
	// 取 5000 条：按平均 1KB/条 ≈ 5MB 快照，远低于内存与写放大阈值。
	DefaultMaxTerminalTasks = 5000
)

// Policy 是保留策略。
type Policy struct {
	// DSHHomeDays 是 DSH_HOME 副本保留天数（<=0 表示默认）。
	DSHHomeDays int `json:"dshHomeDays"`
	// WorkspaceDays 是任务 workspace 保留天数。
	WorkspaceDays int `json:"workspaceDays"`
	// TaskDirDays 是任务目录保留天数。
	TaskDirDays int `json:"taskDirDays"`
	// TerminalTaskDays 是终态任务**记录**的保留天数（queue.json 条目，非目录）。
	TerminalTaskDays int `json:"terminalTaskDays"`
	// MaxTerminalTasks 是终态任务记录的条数上限（与 TerminalTaskDays 取交集）。
	MaxTerminalTasks int `json:"maxTerminalTasks"`
}

// DefaultPolicy 返回默认保留策略。
func DefaultPolicy() Policy {
	return Policy{
		DSHHomeDays:      DefaultDSHHomeDays,
		WorkspaceDays:    DefaultWorkspaceDays,
		TaskDirDays:      DefaultTaskDirDays,
		TerminalTaskDays: DefaultTerminalTaskDays,
		MaxTerminalTasks: DefaultMaxTerminalTasks,
	}
}

func (p Policy) normalized() Policy {
	if p.DSHHomeDays <= 0 {
		p.DSHHomeDays = DefaultDSHHomeDays
	}
	if p.WorkspaceDays <= 0 {
		p.WorkspaceDays = DefaultWorkspaceDays
	}
	if p.TaskDirDays <= 0 {
		p.TaskDirDays = DefaultTaskDirDays
	}
	if p.TerminalTaskDays <= 0 {
		p.TerminalTaskDays = DefaultTerminalTaskDays
	}
	if p.MaxTerminalTasks <= 0 {
		p.MaxTerminalTasks = DefaultMaxTerminalTasks
	}
	return p
}

// Result 是一次清理的结果。
type Result struct {
	// FreedBytes 是释放的字节数。
	FreedBytes int64 `json:"freedBytes"`
	// ReclaimedDirs 是回收的目录数。
	ReclaimedDirs int `json:"reclaimedDirs"`
	// SkippedDirs 是跳过的目录数（保留窗口内 / 非任务目录）。
	SkippedDirs int `json:"skippedDirs"`
	// Errors 是非致命错误（单个目录失败不影响整体）。
	Errors []string `json:"errors,omitempty"`
	// DryRun 为 true 时只统计不删除。
	DryRun bool `json:"dryRun"`
}

// PruneDSHHome 清理过期的任务级 DSH_HOME 副本。
//
// 安全保证：
//  1. 只遍历 dshHome/tasks/ 下的**一层**子目录（不递归，不碰深层）；
//  2. 只删除 mtime 早于保留窗口的目录；
//  3. 删除走回收站（见 trash_windows.go / trash_other.go）；
//  4. 任何单个目录失败只记录，不中断整体清理。
func PruneDSHHome(dshHome string, p Policy, dryRun bool) Result {
	p = p.normalized()
	var res Result
	res.DryRun = dryRun

	tasksRoot := filepath.Join(dshHome, "tasks")
	entries, err := os.ReadDir(tasksRoot)
	if err != nil {
		// 目录不存在 = 还没有任务副本，正常。
		return res
	}
	cutoff := time.Now().AddDate(0, 0, -p.DSHHomeDays)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(tasksRoot, e.Name())
		// 用"目录内最新文件的 mtime"而不是目录自身 mtime：
		// Windows 上目录 mtime 会被子文件创建/删除意外刷新（OS 行为），
		// 且 os.Chtimes 对目录不可靠（实测导致 3 个用例假红）。
		// 文件 mtime 才是"这份副本最后被真正使用"的可信信号。
		if newestModTime(dir).After(cutoff) {
			res.SkippedDirs++
			continue
		}
		size, _ := dirSize(dir)
		if dryRun {
			res.ReclaimedDirs++
			res.FreedBytes += size
			continue
		}
		if err := Trash(dir); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("回收 %s 失败: %v", e.Name(), err))
			continue
		}
		res.ReclaimedDirs++
		res.FreedBytes += size
	}
	return res
}

// PruneTaskDir 回收**过期的任务目录本身**（audit 流 + 成果资产）。
//
// 现场缺陷（2026-10-19 审计）：Policy.TaskDirDays 被定义（默认 90 天）、被 Normalize 补齐、
// 注释写明「这些是审计与成果资产，保留最久」，但**没有任何代码使用它** ——
// PruneTaskWorkspaces 只回收 <task>/workspace/，任务目录本身永不回收。
// 对 7x24 长跑系统这是「只涨不落的一半」，而且是一个**看起来有闸门、实际没有**的假象。
//
// 安全取舍（刻意保守）：本包头部明确写着
//
//	「绝不删除任何产出（workbench-output/tasks/<id>/ 的 final.md 等是用户资产）」。
//
// 任务目录里就包含 final.md 这类成果，因此本函数**不参与默认回收**，
// 只有调用方显式要求时才执行（见 cmd/workbench 的 --task-dirs 开关）。
// 默认 `--prune --apply` 只回收 DSH_HOME 副本与 workspace —— 那两类不是成果资产。
//
// 语义：
//   - 以目录内**最新文件**的 mtime 作为「最后被真正使用」的信号；
//   - 超过 TaskDirDays 才整体回收，删除走回收站（可反悔）；
//   - 只处理引擎产出的目录前缀（task-*/critique-*/summary-*），别的不动；
//   - dry-run 只统计不删除。
//
// 顺序：调用方应先 PruneTaskWorkspaces（回收 workspace 明细），再调本函数。
func PruneTaskDir(tasksRoot string, p Policy, dryRun bool) Result {
	p = p.normalized()
	var res Result
	res.DryRun = dryRun

	entries, err := os.ReadDir(tasksRoot)
	if err != nil {
		return res
	}
	cutoff := time.Now().AddDate(0, 0, -p.TaskDirDays)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !isEngineTaskDir(e.Name()) {
			continue
		}
		dir := filepath.Join(tasksRoot, e.Name())
		if newestModTime(dir).After(cutoff) {
			res.SkippedDirs++
			continue
		}
		size, _ := dirSize(dir)
		if dryRun {
			res.ReclaimedDirs++
			res.FreedBytes += size
			continue
		}
		if err := Trash(dir); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("回收任务目录 %s 失败: %v", e.Name(), err))
			continue
		}
		res.ReclaimedDirs++
		res.FreedBytes += size
	}
	return res
}

// isEngineTaskDir 判定目录名是否由引擎产出（避免误删用户手放的目录）。
func isEngineTaskDir(name string) bool {
	for _, pre := range []string{"task-", "critique-", "summary-"} {
		if strings.HasPrefix(name, pre) {
			return true
		}
	}
	return false
}

// TaskDirUsage 汇总任务目录的占用（供 --prune 在未显式开启回收时**如实报告**）。
//
// 为什么要报告：默认不删用户资产，但必须让人看到「这里在涨」，
// 否则又是一个「看起来有闸门、其实没有」的黑箱。
func TaskDirUsage(tasksRoot string, p Policy) (count int, totalBytes int64, expired int) {
	p = p.normalized()
	entries, err := os.ReadDir(tasksRoot)
	if err != nil {
		return 0, 0, 0
	}
	cutoff := time.Now().AddDate(0, 0, -p.TaskDirDays)
	for _, e := range entries {
		if !e.IsDir() || !isEngineTaskDir(e.Name()) {
			continue
		}
		dir := filepath.Join(tasksRoot, e.Name())
		sz, _ := dirSize(dir)
		count++
		totalBytes += sz
		if !newestModTime(dir).After(cutoff) {
			expired++
		}
	}
	return count, totalBytes, expired
}

// PruneTaskWorkspaces 清理过期任务的 workspace 目录（保留 final.md 等资产）。
func PruneTaskWorkspaces(tasksRoot string, p Policy, dryRun bool) Result {
	p = p.normalized()
	var res Result
	res.DryRun = dryRun

	entries, err := os.ReadDir(tasksRoot)
	if err != nil {
		return res
	}
	cutoff := time.Now().AddDate(0, 0, -p.WorkspaceDays)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ws := filepath.Join(tasksRoot, e.Name(), "workspace")
		if fi, serr := os.Stat(ws); serr != nil || !fi.IsDir() {
			continue
		}
		if newestModTime(ws).After(cutoff) {
			res.SkippedDirs++
			continue
		}
		size, _ := dirSize(ws)
		if dryRun {
			res.ReclaimedDirs++
			res.FreedBytes += size
			continue
		}
		if err := Trash(ws); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("回收 %s/workspace 失败: %v", e.Name(), err))
			continue
		}
		res.ReclaimedDirs++
		res.FreedBytes += size
	}
	return res
}

// newestModTime 返回目录内最新文件的 mtime（无文件则返回目录自身 mtime）。
//
// 为什么不用目录自身 mtime（2026-10-19 实测）：Windows 上目录 mtime 会被
// 子文件创建/删除意外刷新，且 os.Chtimes 对目录不可靠。文件 mtime 才是
// "这份副本最后被真正使用"的可信信号。
func newestModTime(dir string) time.Time {
	var newest time.Time
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if mt := info.ModTime(); mt.After(newest) {
			newest = mt
		}
		return nil
	})
	if newest.IsZero() {
		if fi, serr := os.Stat(dir); serr == nil {
			return fi.ModTime()
		}
	}
	return newest
}

// dirSize 递归统计目录大小（单层失败不中断）。
func dirSize(root string) (int64, error) {
	var total int64
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, nil
}

// EstimateGrowth 估算当前增长速度，用于面板展示"什么时候会爆"。
//
// 现场实测：24 小时积累了 247 个 DSH_HOME 副本。
// 有了这个函数，用户能提前看到"照这个速度 1 个月后是多少"。
func EstimateGrowth(dshHome string) Estimate {
	tasksRoot := filepath.Join(dshHome, "tasks")
	entries, err := os.ReadDir(tasksRoot)
	if err != nil {
		return Estimate{}
	}
	var est Estimate
	var oldest, newest int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mt := newestModTime(filepath.Join(tasksRoot, e.Name())).Unix()
		if mt == 0 {
			continue
		}
		if est.Count == 0 || mt < oldest {
			oldest = mt
		}
		if mt > newest {
			newest = mt
		}
		est.Count++
		if size, _ := dirSize(filepath.Join(tasksRoot, e.Name())); size > 0 {
			est.TotalBytes += size
		}
	}
	if est.Count > 1 && newest > oldest {
		spanDays := float64(newest-oldest) / 86400.0
		if spanDays > 0.01 {
			est.PerDay = float64(est.Count) / spanDays
			if est.PerDay > 0 {
				est.DaysToGB = 1024.0 / (float64(est.TotalBytes) / float64(est.Count) * est.PerDay / (1024 * 1024))
			}
		}
	}
	return est
}

// Estimate 是增长速度估算。
type Estimate struct {
	// Count 是当前副本数。
	Count int `json:"count"`
	// TotalBytes 是当前总占用。
	TotalBytes int64 `json:"totalBytes"`
	// PerDay 是每天新增副本数（估算）。
	PerDay float64 `json:"perDay"`
	// DaysToGB 是照此速度增长到 1GB 所需天数（0 表示无法估算）。
	DaysToGB float64 `json:"daysToGB,omitempty"`
}

// Format 人类可读摘要。
func (e Estimate) Format() string {
	if e.Count == 0 {
		return "无任务副本"
	}
	s := fmt.Sprintf("%d 个副本，%.1f MB，日均新增 %.1f 个", e.Count, float64(e.TotalBytes)/1048576.0, e.PerDay)
	if e.DaysToGB > 0 && e.DaysToGB < 10000 {
		s += fmt.Sprintf("，照此速度约 %.0f 天达到 1GB", e.DaysToGB)
	}
	return s
}

var _ = strings.TrimSpace
