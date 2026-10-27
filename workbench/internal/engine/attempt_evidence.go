package engine

import "workbench/internal/executor"

// attemptEvidence 从执行结果里抽出「失败取证」三个字段，供 attempts.jsonl 落账（T15-④ / T21）。
//
// 返回值约定（nil 安全，调用方直接展开即可）：
//
//	stderrPath    —— 本轮子进程 stderr 的落盘位置；空串表示这个子进程**没有吐过 stderr**。
//	producedRunes —— 最终回答的字符数；0 表示**零产出**（既没文本也没走产物块）。
//	dshCommand    —— 本轮实际执行的 DSH 无头命令行（exe + cli + profile + 覆盖层 + `--json -`）；
//	                 用于自证「自动任务确实经 DSH 无头控制执行」（round 38 · T21 的目标条款）。
//
// 为什么按 rune 而不是 byte 计数：与引擎既有 `countWordsEngine` 同口径，中文回答不会被算成 3 倍体量。
// 三个字段一起看能区分失败形态：`producedRunes=0` 是模型没说话；`producedRunes>0` + kind=no_artifact 是说了但不合规；
// `dshCommand` 则把「哪一次 DSH 无头调用」钉死，便于把故障归因到具体的 CLI/profile/覆盖层组合。
func attemptEvidence(res *executor.Result) (stderrPath string, producedRunes int, dshCommand string) {
	if res == nil {
		return "", 0, ""
	}
	return res.StderrPath, len([]rune(res.FinalText)), res.Command
}
