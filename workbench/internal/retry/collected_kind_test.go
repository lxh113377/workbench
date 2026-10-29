package retry

// collected_kind_test.go —— issue #9 收编的三类逐个钉桩（既有范式 = noartifact_kind_test.go）。
//
// 为什么每条都要钉满三件事，而不是只钉 ParseKind：三类各自的危害正来自「三个下游各管各的」——
//   一、可解析（进 ParseKind 名单）：收编前 crash_loop / verify_failed / replan 是第三份隐性名单，
//      解析收敛成 Unknown ⇒ 台账写成 kind=unknown、config 的 RetryableKinds 白名单管不到（同形态已复发四次）；
//   二、不可重试：三者语义都是「非模型原因的终态处置」—— 崩溃救援已判死、返工环排队、重规划环排队，
//      再叠一层重试只会重演僵尸任务，或让 attempt 额度与 RepairRounds/PlanRounds 两套额度互相掩盖；
//   三、面板有中文：面板渲染走 kindText[b.kind] 兜底 b.kind，漏配**不报错**、只裸显英文桶名。
// 只钉其中一件，另外两件照样静默退化。Prometheus 侧不单独钉：baseline 由 AllKinds() 派生，
// 而「在 AllKinds 里」由本文件第四项断言 + kind_drift_test.go 的双向对账共同保证。

import (
	"strings"
	"testing"
)

// kindTextLabels 从面板源文本里解析 kindText 标签表的键集合。
//
// 为什么在 retry 包里重做一遍解析而不是 import web：web 依赖 retry（metrics_prom.go 用 retry.AllKinds），
// 反向 import 会成环 ⇒ 沿用本包既有手法（kind_drift_test.go 扫兄弟包源文本）。
// 这张表是折行写的、一行可含多个键值对，所以按「标识符 + 冒号 + 单引号」扫，
// 不能按行取冒号前（failure_label_contract_test.go 第一版就栽在按行上）。
func kindTextLabels(t *testing.T) map[string]bool {
	t.Helper()
	src := readRepoFile(t, repoRootJoin("internal", "web", "assets_script.go"))
	start := strings.Index(src, "const kindText = {")
	if start < 0 {
		t.Fatal("找不到面板 kindText 标签表（被改名 ⇒ 本判据失去对照面，判红而非跳过）")
	}
	rest := src[start:]
	end := strings.Index(rest, "};")
	if end < 0 {
		t.Fatal("kindText 表没有收尾（解析面不完整，拒绝下结论）")
	}
	body := rest[:end]
	sq := string(rune(39)) // 单引号：JS 标签值的定界符
	identSet := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_0123456789"
	have := map[string]bool{}
	for i := 0; i < len(body); i++ {
		if strings.IndexByte(identSet, body[i]) < 0 {
			continue
		}
		j := i
		for j < len(body) && strings.IndexByte(identSet, body[j]) >= 0 {
			j++
		}
		k := j
		for k < len(body) && (body[k] == 32 || body[k] == 9) {
			k++
		}
		if k < len(body) && body[k] == byte(58) { // 冒号
			m := k + 1
			for m < len(body) && (body[m] == 32 || body[m] == 9) {
				m++
			}
			if m < len(body) && strings.HasPrefix(body[m:], sq) {
				have[body[i:j]] = true
			}
		}
		i = j - 1
	}
	if len(have) == 0 {
		t.Fatal("标签表解析结果为零 —— 判据不许把空集合当成全部对齐")
	}
	return have
}

// TestCollectedKindsAreParsableNotRetryableAndLabeled 逐条钉三类。
func TestCollectedKindsAreParsableNotRetryableAndLabeled(t *testing.T) {
	cases := []struct {
		name string
		kind Kind
	}{
		{"crash_loop", CrashLoop},
		{"verify_failed", VerifyFailed},
		{"replan", Replan},
	}
	labels := kindTextLabels(t)
	for _, c := range cases {
		// 一、字符串值逐字保持历史值 + 可解析。
		if string(c.kind) != c.name {
			t.Errorf("%s 的枚举值 = %q：必须逐字保持生产数据里的值，改名等于篡改历史读数", c.name, string(c.kind))
		}
		if got := ParseKind(c.name); got != c.kind {
			t.Errorf("ParseKind(%q) = %q, want %q：没进 ParseKind 名单 ⇒ 台账落成 kind=unknown、"+
				"config 默认 RetryableKinds 白名单也管不到它", c.name, got, c.kind)
		}
		if got := ParseKind("  " + c.name + "  "); got != c.kind {
			t.Errorf("ParseKind 应 trim 首尾空格后仍识别 %q，实得 %q", c.name, got)
		}
		// 二、不可重试（与 PoolWaitCapped 同判）。
		if c.kind.Retryable() {
			t.Errorf("%s 被 Retryable() 判可重试：再跑由引擎自己的救援/返工/重规划环负责，"+
				"重试策略再叠一层只会重复消耗 attempt 额度", c.name)
		}
		if IsRetryable(c.name) {
			t.Errorf("IsRetryable(%q) = true，与 Kind.Retryable 的口径矛盾", c.name)
		}
		// 三、面板有中文标签。
		if !labels[c.name] {
			t.Errorf("%s 在面板 kindText 标签表里没有中文条目 ⇒ 失败分布裸显英文桶名；"+
				"补进 internal/web/assets_script.go 的 kindText", c.name)
		}
		// 四、在 AllKinds 里（Prometheus 零值 baseline 由它派生，缺即丢按类别告警的样本）。
		found := false
		for _, k := range AllKinds() {
			if k == c.kind {
				found = true
			}
		}
		if !found {
			t.Errorf("%s 不在 AllKinds() 里 ⇒ promFailureKindsBaseline 缺该序列", c.name)
		}
	}
}

// TestCollectedKindsStayOutOfAttributionList 钉住「不可归因到车道」的对照面：
// 三类都是非模型原因的处置终态，若被 modelpool 的归因名单接走，无辜车道会被引擎
// 自己的处置动作冷却（与在册的 PoolWaitCapped 判断同一口径）；同时这里也不允许再抄一份
// 裸字面量（同一字符串只有一处来源 = 枚举定义处）。
func TestCollectedKindsStayOutOfAttributionList(t *testing.T) {
	src := readRepoFile(t, repoRootJoin("internal", "modelpool", "failover.go"))
	if !strings.Contains(src, "func modelAttributable") {
		t.Fatal("modelpool/failover.go 里找不到 modelAttributable（被改名 ⇒ 对照面丢失，判红而非跳过）")
	}
	for _, name := range []string{"crash_loop", "verify_failed", "replan"} {
		if strings.Contains(src, kindQuote+name+kindQuote) {
			t.Errorf("modelpool 源文本里出现了 %s 的裸字面量：这三类不该归因到模型车道，"+
				"也不该在这里再抄一遍（要引用 retry/executor 枚举）", name)
		}
	}
}

// TestCollectedKindGateHasTeeth 判据自证（负控，纪律 D10）：不碰磁盘，红了只可能是判据本身坏。
func TestCollectedKindGateHasTeeth(t *testing.T) {
	labels := kindTextLabels(t)
	if len(labels) < len(AllKinds()) {
		t.Fatalf("标签面比枚举面还窄（解析漏项）: labels=%d allKinds=%d", len(labels), len(AllKinds()))
	}
	for _, name := range []string{"crash_loop", "verify_failed", "replan"} {
		if !labels[name] {
			t.Fatalf("负控失效：真实标签表里本应有 %s —— 说明面板条目被删或解析器坏了", name)
		}
	}
	if labels["brand_new_bare_kind"] {
		t.Fatal("负控失效：解析器把不存在的键认成了有标签")
	}
}
