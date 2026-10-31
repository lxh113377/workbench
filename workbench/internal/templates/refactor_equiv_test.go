package templates

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// TestDefaultsRefactorIsBehaviorPreserving 是「纯机械拆分」的可证判据。
//
// 为什么需要它（2026-11-01 round 177）：拆 Defaults()（491 行 → 8 个函数）时，唯一能保证「零行为变化」
// 的办法不是看测试绿了，而是**把拆分前后每个模板的所有字段值逐一比对**。既有契约测试只钉住少数几个字段
// （chain 链、交付物门禁子句、编码教训），覆盖不到 PriorityWeight / ValuePrior / CooldownMinutes /
// CreatedAt 等其余字段 —— 拆错这些字段不会有任何测试变红。
//
// 做法：对排序后的每个模板生成「全字段指纹」，再对整个集合取一次哈希。
// 期望值来自拆分前的实现（git show HEAD:...defaults.go 编出的二进制产出的 baseline 哈希），
// 写死在 wantSetHash 里；一旦以后有人无意改动任何字段，这条会立刻红。
func defaultsSetHash() string {
	all := Defaults()
	var lines []string
	for _, t := range all {
		lines = append(lines, fingerprintAll(t))
	}
	sort.Strings(lines)
	h := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(h[:])
}

// fingerprintAll 序列化模板的全部可观察字段（不含时间戳 —— 它们随 model.Now() 变化，另用相对断言）。
func fingerprintAll(t tpl) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%d|%v|%v|%d|%s|%v|%d|%d|%v|%v|%s|%s",
		t.ID, t.Name, t.Domain, t.Description, t.PromptTemplate,
		t.PriorityWeight, t.ValuePrior, t.CostPrior, t.CooldownMinutes, t.DiversityGroup,
		t.Chain.SuccessorTemplates, t.Chain.MaxDepth, t.Chain.Fanout,
		t.Enabled, t.Builtin, t.OutputSpec.Format, t.Params.ModelTier)
}

func TestDefaultsRefactorIsBehaviorPreserving(t *testing.T) {
	got := defaultsSetHash()
	t.Logf("defaults set hash = %s", got)
	if got != wantDefaultsSetHash {
		t.Fatalf("Defaults() 的模板集合指纹变了（期望 %s，实得 %s）—— 拆分/改动改变了可观察字段", wantDefaultsSetHash, got)
	}
}

// wantDefaultsSetHash 由拆分**前**的实现算出并固化（见本文件注释的取证步骤）。
// wantDefaultsSetHash 是拆分**前**实现产出的指纹，取证步骤（可复跑）：
//
//	git worktree add -f xtmp/r11_baseline HEAD          # 检出拆分前的代码
//	# 在 baseline 的 templates 包里放一个只打印指纹的临时测试（同一 fpAll 逻辑）
//	cd xtmp/r11_baseline/workbench && go test -run TestBaselineHash -v ./internal/templates/
//	⇒ BASELINE_HASH=57859219…fa2093c9 COUNT=29
//	git worktree remove xtmp/r11_baseline --force        # 用完即删（#44 ⑧ 负控副本纪律）
//
// 拆分后同法算出同一哈希 ⇒ 证明 29 个模板的 17 个可观察字段逐字未变（纯机械拆分）。
const wantDefaultsSetHash = "57859219ff0ceed544627d981381488f7654ce30a7dc09f95be78513fa2093c9"
