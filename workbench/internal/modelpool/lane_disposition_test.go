package modelpool

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// executorKindValues 从 executor 源码里取出 Kind 枚举的全部字符串值。
// 用 go/ast 而不是正则：这是本仓踩过多次的教训 —— 正则取字面量会把注释里的示例、
// 别的常量的值也算进来（口径失真 ⇒ 判据在错的对象上"通过"）。
func executorKindValues(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "../executor/executor.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("解析 executor.go 失败: %v", err)
	}
	var out []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// 只收 Kind 类型、且值为字符串字面量的（KindNone 的空串除外）
			ty, ok := vs.Type.(*ast.Ident)
			if !ok || ty.Name != "Kind" {
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				s := strings.Trim(lit.Value, `"`)
				if s == "" {
					continue
				}
				out = append(out, s)
			}
		}
	}
	if len(out) < 15 {
		t.Fatalf("只取到 %d 个 Kind 值，口径可疑（判据可能被空集蒙过）", len(out))
	}
	return out
}

// TestLaneDispositionCoversEveryKind 是这次收敛的核心判据：
// executor 每新增一个失败类别，必须在 laneDispositions 里登记处置，或在 engineOnlyKinds 里
// 写明"为什么不进车道账本" —— 两边都没有 ⇒ 判红。
//
// 为什么需要它（本会话同族事故已 5 次）：新增/改名一个类别时只改了其中一处判定，另一处**静默失效**。
// 最隐蔽的一次是记账字段被写死，导致下游按该字段做的判定恒为 false，而面板上一切正常。
// 这条守卫把"记得改三处"从人脑记忆变成交付门禁。
func TestLaneDispositionCoversEveryKind(t *testing.T) {
	for _, k := range executorKindValues(t) {
		_, decided := laneDispositions[k]
		_, engineOnly := engineOnlyKinds[k]
		if decided && engineOnly {
			t.Errorf("类别 %q 同时登记为车道处置与引擎专用（互相矛盾，必须择一）", k)
			continue
		}
		if !decided && !engineOnly {
			t.Errorf("类别 %q 没有任何处置决策：既不在 laneDispositions，也不在 engineOnlyKinds。"+
				"要么登记车道处置（一击冷却/长冷却/探测不可翻案），要么写明它为何不经车道账本。", k)
			continue
		}
		// 登记了处置就必须有理由（note）：没有 note 的条目说明是"顺手补一行"，
		// 下一次改代码的人无从判断量级（600s/1800s）为什么是这个数。
		if decided && strings.TrimSpace(laneDispositions[k].note) == "" {
			t.Errorf("类别 %q 的处置没有 note（量级依据必须留在表里）", k)
		}
		if engineOnly && strings.TrimSpace(engineOnlyKinds[k]) == "" {
			t.Errorf("类别 %q 被列入 engineOnlyKinds 却没写理由", k)
		}
	}
}

// TestProbeCannotCureSetUnchanged 钉住「收敛不改变行为」：
// probeCannotCure 的集合必须与重构前 isExecutionFailureKind 手抄的那一份逐项相等。
// 这类对照是重构的地基 —— 否则"表驱动"可能悄悄把某个类别的冷却语义改掉（比 T75 那次更隐蔽）。
func TestProbeCannotCureSetUnchanged(t *testing.T) {
	want := map[string]bool{
		"stalled": true, "bad_exit": true, "timeout": true,
		"upstream_failed": true, "boot_failed": true, "not_served": true,
	}
	got := map[string]bool{}
	for k, d := range laneDispositions {
		if d.probeCannotCure {
			got[k] = true
		}
	}
	for k := range want {
		if !got[k] {
			t.Errorf("重构前该类别不可被探测翻案，现在不生效了：%s", k)
		}
	}
	for k := range got {
		if !want[k] {
			t.Errorf("新增了不可翻案的类别（行为变了，需单独论证）：%s", k)
		}
	}
	// 行为侧再抽一遍：isExecutionFailureKind 必须与集合完全一致（防止访问器写错）
	for _, k := range []string{"stalled", "bad_exit", "timeout", "upstream_failed", "boot_failed", notServedKind} {
		if !isExecutionFailureKind(k) {
			t.Errorf("isExecutionFailureKind(%s) 应为 true", k)
		}
	}
	for _, k := range []string{"rate_limited", "quota_exhausted", "transport_failed", "stream_cut", "unknown", "cancelled"} {
		if isExecutionFailureKind(k) {
			t.Errorf("isExecutionFailureKind(%s) 应为 false（端点可达可翻案的类别不得被禁）", k)
		}
	}
}

// TestOneStrikeAndLongCooldownUnchanged 同样逐项对照 manager 重构前的两处判定。
func TestOneStrikeAndLongCooldownUnchanged(t *testing.T) {
	for _, tc := range []struct {
		kind     string
		one      bool
		minSec   int
		longCool bool
	}{
		{noArtifactKind, true, 600, false},
		{notServedKind, true, 1800, false},
		{"model_unavailable", false, 0, true},
		{"missing_credential", false, 0, true},
		{probeKindUnreachable, false, 0, true},
		{probeKindBadEndpoint, false, 0, true},
		{"rate_limited", false, 0, false},
		{"unknown", false, 0, false},
	} {
		one, minSec := laneOneStrike(tc.kind)
		if one != tc.one || minSec != tc.minSec {
			t.Errorf("laneOneStrike(%s) = (%v,%d)，应为 (%v,%d)", tc.kind, one, minSec, tc.one, tc.minSec)
		}
		if got := laneLongCooldown(tc.kind); got != tc.longCool {
			t.Errorf("laneLongCooldown(%s) = %v，应为 %v", tc.kind, got, tc.longCool)
		}
	}
}
