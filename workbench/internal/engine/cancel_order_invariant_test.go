package engine

// 缺陷②的取证不靠「造一次取消然后看车道计数」—— 本轮实测证明那条路在真实引擎里立不起来
// （bad_exit 走 requeue/failover、车道在 35 条间轮转、attempt 恒 0，60 秒窗口内取不到
// RecordFailure 的记账增量；隔离实例的 /api/models 一直 sum_failure=0）。继续改造探针去凑
// 那个增量，就是把证据做成我想要的意思（本仓 #36 禁止）。
//
// 换一条可复算、不赌时序的路：缺陷②的本质是**语句顺序**——旧代码先记账后判取消
// （`022cea5:workbench/internal/engine/engine.go` 里 RecordFailure 在 :1212/:1215、
// 取消判定在 :1219），新代码反过来（classifyCancellation 在记账之前、取消分支先 return）。
// 顺序是纯静态事实，可以直接钉；并且把同一条判据跑在修复前的 blob 上必须判红 ——
// 这才是"判据有牙"的证明（本仓 ⑧：无负控的判据视为无牙）。
//
// 为什么用 git blob 而不是工作树：工作树会被并行会话改动（本轮实测 HEAD 一小时内推进过
// 多次），钉工作树会让判据随别人改动而红/绿。这里读 HEAD（本判据随修复一起入库 ⇒
// HEAD 必然含修复），并且只在文件能取到时下结论；取不到一律 t.Fatal，不 skip 成绿。

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// headEngineSource 取 HEAD 版本的 engine.go 全文。取不到 ⇒ t.Fatal（不许当成"无违规"）。
func headEngineSource(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "show", "HEAD:workbench/internal/engine/engine.go")
	cmd.Dir = repoRootForSourceAssert(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("取不到 HEAD 版 engine.go（git show 失败 ⇒ 判据失去对照面，不能当通过）: %v", err)
	}
	if len(out) < 1000 {
		t.Fatalf("HEAD 版 engine.go 内容异常短（%d 字节）⇒ 拒绝下结论", len(out))
	}
	return string(out)
}

// repoRootForSourceAssert 从测试工作目录（workbench/internal/engine）回溯到 git 顶层。
func repoRootForSourceAssert(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse --show-toplevel 失败: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestCancelClassificationPrecedesLaneAccounting 钉缺陷②的不变量：runOne 的失败分支里，
// 「取消判定 + 取消分支 return」必须出现在 RecordFailure **之前**。
//
// 为什么不是简单比行号：engine.go 里 RecordFailure 有多个调用点（timeout/empty-final 等），
// 而取消判定也出现在别处（finishCancelled 等）。必须先框定「runOne 的失败分支」这个区域，
// 再在区域内取最小行号 —— 区域外的一律不参与比较。（本轮实测过一次假绿：拿全文件首个
// classifyCancellation 与首个 RecordFailure 比，结果 :1003 那个无关调用点让判据永远绿。）
func TestCancelClassificationPrecedesLaneAccounting(t *testing.T) {
	// 两个面都钉：HEAD（CI 检出的就是它）与工作树（本地开发态）。
	// 只钉 HEAD 会留一个假绿窗口：有人把顺序改坏、还没提交时本地 go test 依然绿。
	for _, face := range []struct {
		name string
		src  string
	}{
		{"HEAD", headEngineSource(t)},
		{"工作树", workingEngineSource(t)},
	} {
		t.Run(face.name, func(t *testing.T) {
			assertCancelPrecedesAccounting(t, face.src)
		})
	}
}

// workingEngineSource 读工作树里的 engine.go（与 HEAD 版同一判据，覆盖未提交的改动）。
func workingEngineSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("engine.go")
	if err != nil {
		t.Fatalf("读不到工作树 engine.go（判据失去对照面，不能当通过）: %v", err)
	}
	if len(b) < 1000 {
		t.Fatalf("工作树 engine.go 异常短（%d 字节）⇒ 拒绝下结论", len(b))
	}
	return string(b)
}

func assertCancelPrecedesAccounting(t *testing.T, src string) {
	lines := strings.Split(src, "\n")

	// 区域锚点：失败分支的入口注释是本轮修复时写死的、语义唯一的一行。
	start := -1
	for i, l := range lines {
		if strings.Contains(l, "只做分类，**先不记车道失败**") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("找不到失败分支的区域锚点（注释被改/删 ⇒ 判据失去对照面，不能当通过）")
	}
	// 区域终点：该失败分支所在函数的结束（下一个顶层 func 或 runOne 之后的注释块）。
	end := linesLen(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "func ") {
			end = i
			break
		}
	}
	region := lines[start:end]
	if len(region) < 5 {
		t.Fatalf("失败分支区域只有 %d 行，异常短 ⇒ 拒绝下结论", len(region))
	}

	cancelAt, classifyAt, recordAt, evictAt := -1, -1, -1, -1
	for i, l := range region {
		if strings.Contains(l, "// 真失败才归因到车道") {
			if recordAt < 0 {
				recordAt = i
			}
		}
		if classifyAt < 0 && strings.Contains(l, "classifyCancellation(") {
			classifyAt = i
		}
		if cancelAt < 0 && strings.Contains(l, "if cancelled {") {
			cancelAt = i
		}
		if evictAt < 0 && regexp.MustCompile(`e\.router\.RecordFailure\(`).MatchString(l) {
			evictAt = i
		}
	}
	for _, p := range []struct {
		name string
		idx  int
	}{{"classifyCancellation", classifyAt}, {"if cancelled", cancelAt}, {"真失败才归因到车道", recordAt}, {"RecordFailure 调用", evictAt}} {
		if p.idx < 0 {
			t.Fatalf("失败分支区域内找不到 %s ⇒ 判据失去对照面（不能当通过）", p.name)
		}
	}
	if !(classifyAt < recordAt && cancelAt < recordAt && evictAt > recordAt) {
		t.Errorf("取消必须先于车道记账：classify=%d cancel=%d 记账守卫=%d RecordFailure=%d（要求 classify<cancel<守卫<RecordFailure）",
			classifyAt, cancelAt, recordAt, evictAt)
	}
	// 第二条独立佐证：取消分支里必须**先 return**，即记账守卫之后才出现 RecordFailure。
	if strings.Contains(region[cancelAt], "if cancelled {") {
		closed := false
		for i := cancelAt; i < len(region); i++ {
			if strings.Contains(region[i], "e.finishCancelled(") {
				closed = true
				break
			}
			if strings.Contains(region[i], "e.router.RecordFailure(") {
				t.Error("取消分支内出现了 RecordFailure ⇒ 取消会污染车道（缺陷②回归）")
				break
			}
		}
		if !closed {
			t.Error("取消分支里找不到 finishCancelled ⇒ 分支结构已变，判据需同步更新（不得静默放过）")
		}
	}
}

// TestLaneAccountingOrderRegressionIsDetected 是上面那条判据的**自证负控**：
// 把同一套区域扫描跑在修复前的 git blob（022cea5，实测不含 6953c76）上，必须判定为
// "顺序相反"。若它不红，说明扫描面根本没在比东西 —— 那上面那条绿灯就是假的。
// 这是本仓 ⑧ 要求的负控，且用真实历史版本而不是构造样本。
func TestLaneAccountingOrderRegressionIsDetected(t *testing.T) {
	cmd := exec.Command("git", "show", "022cea5:workbench/internal/engine/engine.go")
	cmd.Dir = repoRootForSourceAssert(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("取不到修复前版本 022cea5:engine.go（负控没有对照面，判据未自证）: %v", err)
	}
	src := string(out)
	if strings.Contains(src, "classifyCancellation(") {
		t.Fatal("前提不成立：022cea5 里竟含 classifyCancellation ⇒ 它不是修复前版本，本负控无效")
	}
	if !strings.Contains(src, "e.router.RecordFailure(") {
		t.Fatal("022cea5 里没有 RecordFailure ⇒ 记账面不存在，无法做顺序差分")
	}
	// 修复前的形态：记账在前、取消判定在后（用注释文本定位，注释随实现一起入库）。
	recAt, cancelAt := -1, -1
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		if recAt < 0 && strings.Contains(l, "e.router.RecordFailure(actualModel, res.ErrorKind, errText(runErr))") {
			recAt = i
		}
		if cancelAt < 0 && strings.Contains(l, "e.isCancelling(task.ID) || ctx.Err() != nil || kind == retry.Cancelled") {
			cancelAt = i
		}
	}
	if recAt < 0 || cancelAt < 0 {
		t.Fatalf("修复前的两个锚点没同时找到（RecordFailure=%d 取消判定=%d）⇒ 差分面不完整", recAt, cancelAt)
	}
	if recAt > cancelAt {
		t.Errorf("负控失效：修复前版本里记账(%d)竟已晚于取消判定(%d) ⇒ 上面那条绿灯不成立", recAt, cancelAt)
	}
	t.Logf("修复前顺序：RecordFailure 在第 %d 行、取消判定在第 %d 行 ⇒ 记账先于取消（缺陷②成立），差分面有效", recAt+1, cancelAt+1)
}

func linesLen(v []string) int { return len(v) }

// 保证 os 被引用（下面这条测试用它确认 git 可执行文件在，避免 CI 上假绿）。
var _ = os.Getenv
