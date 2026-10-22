package web

import (
	"os"
	"path/filepath"
	"strings"

	"workbench/internal/model"
)

// artifactSource 是产出读取所需的最小能力面（*store.Store 已满足）。
// 抽成接口是为了让解析逻辑可单测，不必拉起整个 Server/Engine。
type artifactSource interface {
	ReadTaskArtifact(taskID, name string) ([]byte, error)
	TaskDir(taskID string) string
}

// readTaskArtifact 取任务的主产出内容，返回 (内容, 来源说明, 是否命中)。
//
// 为什么必须有它（2026-10-22 实测缺陷）：产出文件名由 engine.choosePrimary 决定
// （按 outputFormat 与文件体积挑最大的那个，例如 article.md），final.md 只是
// 「模型一个文件都没写」时的兜底名。而详情接口与批量导出此前都硬编码
// ReadTaskArtifact(id, "final.md") ⇒ 绝大多数已完成任务在面板里显示「（暂无产出）」、
// 导出静默跳过。实测样本：outputBytes=5935 的任务，面板 .doc 只有 6 个字符。
//
// 三级回落顺序（前两级是精确路径，第三级是目录内兜底）：
//  ① task.OutputFile —— 引擎落盘时写的绝对路径，最可信；
//  ② 任务目录 final.md —— 历史任务或无 OutputFile 的旧数据；
//  ③ 任务目录里最大的 .md —— OutputFile 指向的文件被改名/清理时仍可读到产出。
//     只扫任务目录本身（不递归），因此不存在越界读别任务文件的风险。
func readTaskArtifact(src artifactSource, t *model.Task) (string, string, bool) {
	if t == nil || src == nil {
		return "", "", false
	}
	if t.OutputFile != "" {
		if data, err := os.ReadFile(t.OutputFile); err == nil && len(data) > 0 {
			return string(data), filepath.Base(t.OutputFile), true
		}
	}
	if data, err := src.ReadTaskArtifact(t.ID, "final.md"); err == nil && len(data) > 0 {
		return string(data), "final.md", true
	}
	entries, err := os.ReadDir(src.TaskDir(t.ID))
	if err != nil {
		return "", "", false
	}
	best, bestSize := "", int64(-1)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.EqualFold(name, "final.md") {
			continue // 第 ② 级已试过
		}
		if ext := strings.ToLower(filepath.Ext(name)); ext != ".md" && ext != ".txt" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() > bestSize {
			bestSize = info.Size()
			best = name
		}
	}
	if best == "" {
		return "", "", false
	}
	data, err := src.ReadTaskArtifact(t.ID, best)
	if err != nil || len(data) == 0 {
		return "", "", false
	}
	return string(data), best, true
}
