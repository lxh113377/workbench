package retry

// kind_drift_test.go —— 「枚举抄写面」漂移门禁（round 66 · 改造一）。
//
// 由来（全部生产实测取证，2026-10-29）：同一个失败类别被手抄在四个地方 ——
//   1) executor.Kind 常量表；2) retry.Kind 常量表 + ParseKind 名单；3) config 默认 RetryableKinds 白名单；
//   4) web 侧的 Prometheus 基线 / 面板中文标签。
// 手抄的必然后果是每加一个类别就漏一处，本轮之前已复发四次：
//   · rate_limited 在 executor 有、retry 没有 ⇒ ParseKind 收敛成 Unknown ⇒ 引擎台账写 kind=unknown
//     （retry.go 里 transport / no_artifact / not_served / no_lane 四段注释写的都是这句教训，每次都要撞上才补）；
//   · no_artifact / not_served 被 Retryable() 判可重试、却不在 config 白名单 ⇒ Policy.allowed() 是
//     Retryable() AND 白名单 ⇒ 生产里 attempt0 即 fail（台账实证 task-7a431ac6ea86b4d2、
//     task-66cf148146331039），而代码注释宣称「可重试」—— 注释与行为矛盾。
// 对标 peer 的做法：river 对枚举漏项直接 panic（internal/jobcompleter/job_completer.go:87），
// asynq 的 code 表忘更新 String 当场崩（internal/errors/errors.go:96）。本项目是 7x24 无人值守
// 不能 panic，等价手段 = 把漏配变成必红的测试。
//
// 手法：扫兄弟包**源文本**（本仓既有范式：daily_count_source_test.go / report_gate_test.go 同款）。
// 只 import 标准库、不 import executor ⇒ 避开 retry 与 executor 的包环；且「新增类别忘登记」会让判据真的变红。

import (
	"os"
	"strings"
	"testing"
)

// readSiblingSource 读兄弟包源文件（测试工作目录 = 本包目录）。
// 读不到 ⇒ 判红，绝不 Skip：Skip 会把「对照面丢失」洗成「全部对齐」（本仓在册禁例）。
func readSiblingSource(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("读 %s 失败: %v —— 两侧枚举无从对照，判据失效要判红，不许跳过", rel, err)
	}
	return string(data)
}

// extractKindLiterals 抽出源文本里所有「Kind = 引号值」形态的非空字面量（去重、保序）。
// 引号用 string(34) 现算而不是写转义串：让判据自身不依赖转义正确性（本仓在册的引号坑）。
func extractKindLiterals(t *testing.T, src string) []string {
	t.Helper()
	quote := string(rune(34)) // 双引号；显式 rune 转换（stringintconv 禁 string(int)）
	marker := "Kind = " + quote
	seen := map[string]bool{}
	var out []string
	rest := src
	for {
		i := strings.Index(rest, marker)
		if i < 0 {
			break
		}
		rest = rest[i+len(marker):]
		j := strings.Index(rest, quote)
		if j < 0 {
			break
		}
		s := rest[:j]
		rest = rest[j+len(quote):]
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	// 解析面为零时必须判红（㉑二 在册教训：「0 与 0 互查」是静默全绿的最危险退化态）。
	if len(out) == 0 {
		t.Fatal("一个 Kind 字面量都没解析到 —— 解析面已漂移，判据不许把空集合当成全部对齐")
	}
	return out
}

// TestEveryExecutorKindParsesInRetry：executor 的每个非空 Kind，retry 侧必须认识。
// 违反现场（本轮修复前）：executor 有 rate_limited、retry 没有 ⇒ 生产 39 条「任务失败」全落「未知错误」。
func TestEveryExecutorKindParsesInRetry(t *testing.T) {
	ex := extractKindLiterals(t, readSiblingSource(t, "../executor/executor.go"))
	for _, s := range ex {
		if Kind(s) == Unknown {
			continue
		}
		if got := ParseKind(s); got == Unknown {
			t.Errorf("executor.Kind=%s 在 retry 侧没登记：ParseKind 收敛成 Unknown ⇒ 台账落成 unknown、"+
				"RetryableKinds 也管不到它。修法：retry.go 常量表加对应值、进 ParseKind 名单、按语义决定进不进 Retryable。", s)
		}
	}
}

// TestEveryRetryKindExistsInExecutor：反方向也要对齐（取证时 retry 15 项 / executor 16 项，差集 = rate_limited）。
func TestEveryRetryKindExistsInExecutor(t *testing.T) {
	ex := map[string]bool{}
	for _, s := range extractKindLiterals(t, readSiblingSource(t, "../executor/executor.go")) {
		ex[s] = true
	}
	for _, k := range AllKinds() {
		if !ex[string(k)] {
			t.Errorf("retry.Kind=%s 在 executor 侧没有同名常量（两侧是同一契约的抄本，见 retry.go 头注一一对应）", k)
		}
	}
}

// TestRetryableTableMatchesConfigWhitelist：凡 Retryable()==true 的类别，必须在 config 默认白名单里。
// 理由：allowed() 是 AND；只进 Retryable 不进白名单 ⇒ 生产一次即判死，而注释宣称可重试。
func TestRetryableTableMatchesConfigWhitelist(t *testing.T) {
	src := readSiblingSource(t, "../config/config.go")
	head := "RetryableKinds: []string{"
	i := strings.Index(src, head)
	if i < 0 {
		t.Fatal("config.go 里找不到默认 RetryableKinds 清单（被改名或挪走 ⇒ 判据失去对照面，判红而非跳过）")
	}
	rest := src[i+len(head):]
	end := strings.Index(rest, "},")
	if end < 0 {
		t.Fatal("默认 RetryableKinds 清单没有收尾（解析面不完整，拒绝下结论）")
	}
	quote := string(rune(34))
	parts := strings.Split(rest[:end], quote)
	inList := map[string]bool{}
	for idx := 1; idx < len(parts); idx += 2 {
		inList[parts[idx]] = true
	}
	if len(inList) == 0 {
		t.Fatal("白名单解析结果为零（㉑二：0 条命中不得当成无需对齐）")
	}
	for _, k := range AllKinds() {
		if k.Retryable() && !inList[string(k)] {
			t.Errorf("类别 %s 被 Retryable() 判可重试，却不在 config 默认白名单 ⇒ allowed() 的 AND 让它一次即判死"+
				"（注释与行为矛盾；生产实证 no_artifact / not_served）", k)
		}
	}
}

// TestKindDriftGateHasTeeth：判据自证（纪律 D10 —— 负控必须实测，防恒绿）。
// 全程用内存里的假源文本，不碰磁盘 ⇒ 这条红了只可能是判据本身被改坏。
func TestKindDriftGateHasTeeth(t *testing.T) {
	quote := string(rune(34))
	NL := string(rune(10))
	fake := "const (" + NL + "	KindWhatever Kind = " + quote + "whatever_new_kind" + quote + NL + ")"
	got := extractKindLiterals(t, fake)
	if len(got) != 1 || got[0] != "whatever_new_kind" {
		t.Fatalf("抽取器不认新形态（判据已失去牙）: %+v", got)
	}
	if ParseKind("whatever_new_kind") != Unknown {
		t.Fatal("负控前提消失：未登记类别本应收敛成 Unknown；若 ParseKind 改成透传，本门禁全部逻辑失效")
	}
	if ParseKind("rate_limited") != RateLimited {
		t.Fatal("rate_limited 必须已被登记（本轮修复的核心断言）")
	}
}
