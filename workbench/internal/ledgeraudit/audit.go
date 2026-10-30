// Package ledgeraudit 审计**尝试台账的归因一致性**：拿每行的 error 文本用运行期分类器复算一遍 kind，
// 与台账里记录的 kind 对比，把不一致聚类出来。
//
// 为什么需要（round 145 现场）：两轮内靠这套扫法定位出三处归因缺陷 ——
//
//	· 191 行 boot_failed 实为 not_served（T98，记录面误归因）；
//	· 114 行 rate_limited 实为 quota_exhausted（T100，账号级事实被降级成瞬时限流）；
//	·   1 行 unknown 实为 no_artifact。
//
// 手工按面挖是低效的；把"复算 vs 记录"做成可复跑判据，才知道每一次修分类规则后有没有新漂移。
//
// 口径（刻意保守，宁少报不多报）：
//
//	· 跳过非失败台账类别（plan / done，见 retry.NonFailureLedgerKinds）；
//	· 文本复算不出类别（Unknown）的行**不计入**不一致 —— 那是"判不了"，不是"判错了"；
//	· 只比对已知类别，避免把未知形态算成缺陷。
package ledgeraudit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"workbench/internal/executor"
)

// AttemptRow 是 attempts.jsonl 的一行（只用审计需要的字段）。
type AttemptRow struct {
	At     int64  `json:"at"`
	TaskID string `json:"taskId"`
	Kind   string `json:"kind"`
	Error  string `json:"error"`
}

// Mismatch 是一条"记录与复算不一致"。
type Mismatch struct {
	TaskID string
	At     int64
	Kind   string // 台账记录的
	Want   string // 用文本复算出来的
	Error  string
}

// Report 是扫描结果。
type Report struct {
	Rows       int            // 读到的行数
	Judged     int            // 文本可判定的行数（分母）
	Mismatched int            // 不一致行数（分子）
	Groups     map[string]int // "记录的kind → 应为的kind" 计数
	Samples    []Mismatch     // 前若干条样本（便于人读）
}

// SkipKinds 是不参与归因审计的台账类别（与 retry.NonFailureLedgerKinds 同义）。
//
// 为什么在这里列而不是逐个字符串比较：它们是**尝试类别**而非失败原因，混进失败归因会把分母带歪。
var SkipKinds = map[string]bool{"plan": true, "done": true, "": true}

// Scan 扫描 root/tasks/*/attempts.jsonl；sinceMs>0 时只看该时刻之后的行（回归窗口用）。
//
// 坏行即停（与 store 的 WAL 回放口径一致：宁可少读到一行，也不静默跳过 —— 静默跳过会让
// "不一致数下降"变成读取失败的假象）。
func Scan(root string, sinceMs int64, maxSamples int) (Report, error) {
	rep := Report{Groups: map[string]int{}}
	tasksDir := filepath.Join(root, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return rep, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(tasksDir, e.Name(), "attempts.jsonl")
		raw, err := os.ReadFile(p)
		if err != nil {
			continue // 没有台账的任务目录不是缺陷
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var row AttemptRow
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				break // 坏行即停（口径见函数注释）
			}
			rep.Rows++
			if sinceMs > 0 && row.At < sinceMs {
				continue
			}
			if SkipKinds[row.Kind] {
				continue
			}
			want := string(executor.ClassifyTextForAudit(row.Error))
			if want == string(executor.KindUnknown) {
				continue // 判不了 ≠ 判错了
			}
			rep.Judged++
			if want == row.Kind {
				continue
			}
			rep.Mismatched++
			rep.Groups[row.Kind+" → 应为 "+want]++
			if len(rep.Samples) < maxSamples {
				rep.Samples = append(rep.Samples, Mismatch{TaskID: row.TaskID, At: row.At, Kind: row.Kind, Want: want, Error: row.Error})
			}
		}
	}
	return rep, nil
}

// GroupsSorted 按计数降序返回分组，便于打印与判据比对。
func (r Report) GroupsSorted() []string {
	keys := make([]string, 0, len(r.Groups))
	for k := range r.Groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if r.Groups[keys[i]] != r.Groups[keys[j]] {
			return r.Groups[keys[i]] > r.Groups[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}
