package engine

// 缺陷②（取消不该污染车道健康度）的取证不靠「造一次取消再看计数」—— 本轮实测证明那条路在真实
// 引擎里立不起来：boot_failed 走 requeue、bad_exit 在 35 条车道间 failover 轮转、attempt 恒 0、
// 隔离实例 /api/models 的 sum_failure 恒为 0。继续改造探针去凑那个增量，就是把证据做成我想要的
// 意思（#36 禁止）。所以换轨：缺陷②的本质是**语句顺序**（旧代码先记账、后判取消），顺序是纯静态
// 事实，可以钉死；并用合成样本证明这根钉子在顺序颠倒时会响。
//
// ⚠ 为什么不依赖 git 取 HEAD（2026-10-30 实测教训）：第一版用 git show HEAD:… + git rev-parse 钉
//   HEAD，结果「纯净检出」闸与 CI 双双判红 —— 干净检出里没有 .git，git rev-parse 直接 exit 128
//   （日志原文：cancel_order_invariant_test.go:65 git rev-parse --show-toplevel 失败）。判据在自己
//   机器上绿、在别的面上红，等于把环境差异做成测试失败。⇒ 主判据只读工作树（os.ReadFile，本仓
//   源文本不变量的既有惯例），git 可得时再另行加验历史 blob。
//
// ⚠ 断言为什么必须写在 Test 函数体内（同一轮实测）：本仓零断言判据（check_test_assertions.ps1，
//   后端 workbench/cmd/assertionprobe）只认 Test 体内的 t.Fatal*/t.Error*。我第一版把断言放进辅助
//   函数 ⇒ 被判「无任何断言」。不为此放宽判据、也不塞假断言，直接把判定收回 Test 体来自证。

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// cancelOrderViolation 在「runOne 的失败分支」区域内检查：取消判定是否先于车道记账。
// 返回 (说明, 是否违规)。找不到锚点、区域异常短也判违规 —— 判据失去对照面时必须响，
// 不能「看不到就算通过」（本仓 ㉑二：解析面为空不得当成全部对齐）。
//
// 为什么先框区域再取行号：engine.go 里 RecordFailure 有多个调用点（timeout / empty-final / KindBoot），
// classifyCancellation 也在别处出现。实测过一次假绿：拿全文件首个 classifyCancellation 与首个
// RecordFailure 比，:1003 那个无关调用点会让判据永远绿。
func cancelOrderViolation(src string) (string, bool) {
	lines := strings.Split(src, "\n")
	start := -1
	for i, l := range lines {
		if strings.Contains(l, "只做分类，**先不记车道失败**") {
			start = i
			break
		}
	}
	if start < 0 {
		return "找不到失败分支的区域锚点（注释被改/删 ⇒ 判据失去对照面）", true
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "func ") {
			end = i
			break
		}
	}
	region := lines[start:end]
	if len(region) < 5 {
		return "失败分支区域只有 " + itoa(len(region)) + " 行，异常短 ⇒ 拒绝下结论", true
	}
	classifyAt, cancelAt, guardAt, evictAt := -1, -1, -1, -1
	evictRe := regexp.MustCompile(`e\.router\.RecordFailure\(`)
	for i, l := range region {
		if classifyAt < 0 && strings.Contains(l, "classifyCancellation(") {
			classifyAt = i
		}
		if cancelAt < 0 && strings.Contains(l, "if cancelled {") {
			cancelAt = i
		}
		if guardAt < 0 && strings.Contains(l, "真失败才归因到车道") {
			guardAt = i
		}
		if evictAt < 0 && evictRe.MatchString(l) {
			evictAt = i
		}
	}
	for _, p := range []struct {
		name string
		idx  int
	}{{"classifyCancellation", classifyAt}, {"if cancelled", cancelAt}, {"记账守卫注释", guardAt}, {"RecordFailure 调用", evictAt}} {
		if p.idx < 0 {
			return "区域内找不到 " + p.name + " ⇒ 判据失去对照面（不能当通过）", true
		}
	}
	if !(classifyAt < guardAt && cancelAt < guardAt && evictAt > guardAt) {
		return "取消必须先于车道记账：classify=" + itoa(classifyAt) + " cancel=" + itoa(cancelAt) + " 记账守卫=" + itoa(guardAt) + " RecordFailure=" + itoa(evictAt) + "（要求 classify<cancel<守卫<RecordFailure）", true
	}
	// 第二条独立佐证：取消分支内不得出现 RecordFailure（出现即缺陷②回归）。
	closed := false
	for i := cancelAt; i < len(region); i++ {
		if strings.Contains(region[i], "e.finishCancelled(") {
			closed = true
			break
		}
		if evictRe.MatchString(region[i]) {
			return "取消分支内出现 RecordFailure ⇒ 取消会污染车道（缺陷②回归）", true
		}
	}
	if !closed {
		return "取消分支里找不到 finishCancelled ⇒ 分支结构已变，判据需同步更新（不得静默放过）", true
	}
	return "", false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [24]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte(48 + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

// TestCancelNotRecordedBeforeLaneAccounting 钉工作树里的顺序不变量（不依赖 git ⇒ 纯净检出也能跑）。
func TestCancelNotRecordedBeforeLaneAccounting(t *testing.T) {
	b, err := os.ReadFile("engine.go")
	if err != nil {
		t.Fatalf("读不到工作树 engine.go（判据失去对照面，不能当通过）: %v", err)
	}
	if len(b) < 1000 {
		t.Fatalf("工作树 engine.go 异常短（%d 字节）⇒ 拒绝下结论", len(b))
	}
	msg, violated := cancelOrderViolation(string(b))
	if violated {
		t.Errorf("缺陷②不变量被破坏：%s", msg)
	}
}

// TestCancelOrderAnalyzerHasTeeth 是上面那条的合成负控：同一套分析喂「正确序」与「颠倒序」两个样本，
// 必须一个放过、一个判违规。若颠倒样本也放过，说明分析只在查锚点是否存在、没在比顺序 ⇒ 上面那条
// 绿灯不可信。用合成样本而非 git blob：本仓的纯净检出与 CI 干净面都没有 .git（本轮踩过）。
func TestCancelOrderAnalyzerHasTeeth(t *testing.T) {
	correct := strings.Join([]string{
		"anchor 只做分类，**先不记车道失败**：classifyCancellation(ctx, x, kind)",
		"if cancelled {",
		"e.finishCancelled(id, err, txt)",
		"return",
		"}",
		"// 真失败才归因到车道（取消已在上面 return）",
		"e.router.RecordFailure(m, k, e2)",
		"func other() {}",
	}, "\n")
	wrong := strings.Join([]string{
		"anchor 只做分类，**先不记车道失败**：classifyCancellation(ctx, x, kind)",
		"// 真失败才归因到车道",
		"e.router.RecordFailure(m, k, e2)",
		"if cancelled {",
		"e.finishCancelled(id, err, txt)",
		"return",
		"}",
		"func other() {}",
	}, "\n")
	if _, ok := cancelOrderViolation(correct); ok {
		t.Fatal("合成「正确序」样本被判违规 ⇒ 分析器不认正确形态，判据不可信")
	}
	why, bad := cancelOrderViolation(wrong)
	if !bad {
		t.Error("合成「记账先于取消」样本未被判违规 ⇒ 判据无牙，上面的绿灯不可信")
	} else if !strings.Contains(why, "取消必须先于") {
		t.Errorf("颠倒序样本虽判红，但理由不是顺序问题：%s", why)
	}
}
