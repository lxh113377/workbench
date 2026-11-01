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
// 基线更新记录（round 184，2026-11-01）：由 57859219…fa2093c9 改为 e7f520dc…f1d7b69。
//
//	原因**不是**「测试红了就改期望值」——那会让本判据失效。更新前做了逐字段差分取证：
//	  用 git worktree 检出 HEAD 基线树，在同一份 dump 测试下导出两侧 29 条模板的
//	  17 个非 Prompt 字段（ID/Name/Domain/PriorityWeight/ValuePrior/CostPrior/Cooldown/DiversityGroup/
//	  Chain 三字段/Enabled/Builtin/Format/Sections 数/ModelTier/MaxMinutes）与 PromptTemplate 长度，
//	  结论：ID 顺序 0 处差异、非 Prompt 字段 0 处差异、PromptTemplate 长度**每条都恰好 +821 字节**
//	  （= "7. " 3 + 条款正文 817 + 换行 1）。⇒ 本次哈希变化 100% 来自新增的反虚构条款注入，
//	  没有任何模板的其它字段被顺带改到。这类「条款追加」是本判据允许更新基线的唯一情形，
//	  且必须留下差分证据；若差分不是全条同幅、或出现字段差异，就该修代码而不是改基线。
//
// 基线更新记录 2（round 184-185，2026-11-01）：57859219… → e7f520dc…（加第 7 条反虚构条款）→ 646e3525…（再加 ⑧ 教训）。
//
//	两次都**先做逐字段差分再改基线**（差分脚本 dump 两侧 29 条模板的 17 个非 prompt 字段 + prompt 字节长）：
//	  round 184：28 条 +821、quality-audit 亦 +821，非 prompt 字段 0 处差异 ⇒ 只来自条款注入。
//	  round 185：28 条 +821、quality-audit +1898（=821+1077，多出的 1077 是我这次单独给它加的 ⑧），
//	  ID 顺序 0 处差异、非 prompt 字段 0 处差异 ⇒ 变化 100% 可归因到「预期注入点」。
//	这就是本判据允许更新基线的唯一情形：**全条同幅、字段零变、超幅部分可逐条归因**。
//	若差分出现「字段值变化」或「同幅不成立」，说明改动泄漏到了别处，应修代码而不是改这里。
//
// 基线更新记录 3（round 187，2026-11-01）：646e3525… → 48c94ecc…，原因是把 quality-verify / quality-hygiene
// 两条模板里的「上下文：{context}」占位符从 ⑧/⑤ 位置**移到各自正文末尾**（修结构性漂移，见
// context_position_contract_test.go）。这次差分比前两次更强：不是「全条同幅增长」而是
// 「**长度逐条完全不变**」—— 因为这是纯位移，一个字符都没增删。
//
//	差分证据（dump 双侧 29 条：ID 顺序 / 17 个非 prompt 字段 / prompt 字节长 / prompt sha256 前 8 字节）：
//	  orderDiff=0、fieldDiff=[]、lenDiff=[]（29 条长度全部一字不差）、
//	  hashChanged 恰好 = [quality-verify, quality-hygiene]（就是我在 defaults.go 里编辑的那两条）。
//	⇒ 等价性证明的是「只有位移、没有增删」，比同幅增长更难伪造。若 hashChanged 出现第 3 条，
//	或任何长度变化，就说明改坏了别处，应回滚重做而不是改基线。
//
// 基线更新记录 4（round 191，2026-11-01）：48c94ecc… → cf197d39…。
//
//	给 quality-gate-spec 追加教训 ㉒（静态判据受理面小于运行时 + 同族缺陷须逐个实测复现，见该模板）。
//	这次的差分是四轮里**最强的一次隔离证明**：dump 两侧 29 条模板（ID 顺序 / 17 个非 prompt 字段 /
//	prompt 字节长 / prompt sha256 前 8 字节）实测
//	  ID 顺序 0 处差异；17 个字段 0 处差异；
//	  长度变化**只有 1 条** —— quality-gate-spec 11473→13513（+2040，正是 ㉒ 文本字节数）；
//	  其余 28 条**逐字节不变**（哈希也不变），hashChanged 恰为 [quality-gate-spec]。
//	⇒ 单点改动、单条受影响，不存在"顺手改到别处"的可能。若 hashChanged 出现第 2 条，就是改坏了。
//	契约侧同步：encoded_lessons_contract_test.go 为 quality-gate-spec 加 3 个片段（㉒ 的三个可判要素），
//	TestEncodedLessonsSurviveRender 实测「编码教训 14 条全部到位」。
//
// 基线更新记录 5（round 196，2026-11-01）：cf197d39… → e03d3d80…。
//
//	给 quality-verify 追加教训 ㉑（负控之前必须先跑「健康态为绿」—— 本轮我自己造出一组「三条读数看着
//	全对、实际变异一次都没执行」的 vacuous 负控，根因是空 PATH 子壳里被测工具根本没启动）。
//	更新依据同样是单条隔离增长证明（git worktree 检出 HEAD 基线树，两侧同跑 dump 导出 29 条模板的
//	17 个非 prompt 字段 + prompt 字节长 + prompt sha256 前 8 字节）：
//	  ID 顺序 0 处差异；17 个字段 0 处差异；长度变化**只有 quality-verify 一条**（18861→20293，
//	  +1432 = ㉑ 文本字节数），其余 28 条逐字节不变；hashChanged 恰为 [quality-verify]。
//	与记录 4 同规格。若 hashChanged 出现第 2 条或任何长度变化，说明改坏了别处，应回滚重做而不是改基线。
//
// 基线更新记录 6（round 198，2026-11-01）：e03d3d80… → 4fc81ec2…。
//
//	给两条模板各加一条教训：quality-verify ㉒（断言前先验反例）、quality-gate-spec ㉓（跑门禁必须读结果）。
//	更新依据是同规格的隔离增长证明（worktree 检出 HEAD 基线树，两侧同跑 dump 导出 29 条的 17 个非 prompt 字段
//	+ prompt 字节长 + prompt sha256 前 8 字节）：
//	  ID 顺序 0 处差异；17 个字段 0 处差异；
//	  长度变化**恰好 2 条**（quality-verify 20293→21955、+1662 = ㉒ 字节；quality-gate-spec 13513→14845、
//	  +1332 = ㉓ 字节）；其余 27 条逐字节不变；hashChanged 恰为这两条。
//	⇒ 单点可控。出现第 3 条哈希变化或任何字段差异，即为改坏别处，应回滚而非改基线。
const wantDefaultsSetHash = "4fc81ec203cc289d50461f90e1645ef1cab0ffc6e2ef7f7902bd9eb243c0fbd9"
