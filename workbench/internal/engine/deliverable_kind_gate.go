package engine

import (
	"strings"

	"workbench/internal/artifact"
	"workbench/internal/model"
)

// deliverableKindProblems 检查「被要求交报告文件、却一个 .md 都没交」（round 135 · T88）。
//
// 为什么需要它（E12 的实测）：有 3 条任务 pass=true 而产物只有一个 explore.py ——
// 模型把自写脚本跑通就算验收通过，于是面板计数在涨、目标要的「五轴分级报告」一条也没产出。
// 这不是现有任何门禁的漏判：T66 只在有 docs/*.md 时生效，T73 只挡代码体量下限，
// T74 只挡"自称报告的文件没落在 docs/" ⇒ 一个 .md 都没交时，三道门全部沉默。
//
// 触发器用 **引擎自己注入 prompt 的 marker**（model.DeliverableGateMarker），不用任务标题词族：
// 实测标题词族（报告|审查|体检|立项|复盘|五轴|深化）命中 1,120 条任务，而其中产物无 .md 的只有 5 条
// ⇒ 收益≈0、误杀面比 T74 当年那次一刀切大两个数量级。marker 是"我自己承诺过要什么"的凭据，
// 只有被明确交过报告要求的人才被这道门管。
//
// 判定**只看结构、不看内容**：只问"有没有任何一个 .md"，不问它写得好不好 ——
// 内容是 T66/T67 的职责。这样容器 E2E（交 result.py + 一份占位 result.md）必然通过这道门；
// 若在这里也判内容，就会重演 T73 把容器桩判红的那次事故（阈值上界由最该放过的真实样本决定）。
func deliverableKindProblems(prompt string, files []artifact.File) []string {
	if !strings.Contains(prompt, model.DeliverableGateMarker) {
		return nil // 没被要求交报告：不适用（非报告类模板/文章类任务不受影响）
	}
	for _, f := range files {
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(f.Path)), ".md") {
			return nil // 交了 .md：交给 T66/T74 判内容合格与落点，这里不越权
		}
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Path)
	}
	if len(names) == 0 {
		return []string{"被要求产出报告文件（prompt 含交付物门禁），但本轮没有任何产物文件"}
	}
	return []string{"被要求产出报告文件（prompt 含交付物门禁），交付物里却没有 .md，只有 " + strings.Join(names, ", ") + "：自写脚本跑通不等于交出报告"}
}
