package engine

// retry_policy_guard_contract_test.go —— 钉住「retryPol 只经 policy() 读，不许锁外直读」（2026-11-02 新增）。
//
// 为什么需要：Engine.retryPol 由 retryPolMu 保护，写点在 New / RestoreInterrupted / ReloadConfig 三处，
// 其中 ReloadConfig 由并发 HTTP handler 触发（web/server.go 的 handleSettings 与 handleEvalConfig），
// 即**真实并发写者**。而读侧曾有三处（engine.go 原 L191/L761/L2337）在 Unlock 之后直接读该字段 ——
// 仓内已有正确封装 policy()（读锁保护 + nil 兜底 retry.Default()），那三处属**漏用 getter**。
// 已修；本判据防它被改回去（改回去不会有任何现有测试变红，因为竞态只在并发下出现、本机无 gcc 用不了 -race）。
//
// 断言口径：只看非注释行里 e.retryPol 的**直接读取**，允许两种形态 —— ① 赋值（写在锁内）；
// ② 作为 policy() 内部返回（该函数本身持锁）。其余一律红。

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// retryPolReadRe 匹配 e.retryPol 的读取形态，但**不**匹配 e.retryPolMu（超串）与赋值。
var retryPolReadRe = regexp.MustCompile(`e\.retryPol(?:[^.A-Za-z0-9_]|$)`)

func TestRetryPolOnlyReadThroughGetter(t *testing.T) {
	src, err := os.ReadFile("engine.go")
	if err != nil {
		t.Fatalf("读不到 engine.go（本判据在包目录内运行）: %v", err)
	}

	lines := strings.Split(string(src), "\n")
	// 定位 policy() 函数体范围：它内部允许出现 e.retryPol（持锁读 + 返回）
	getterStart, getterEnd := -1, -1
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "func (e *Engine) policy()") {
			getterStart = i
			// 按大括号配平找函数体结束（「第一个行首 }」会误判嵌套块）
			depth := 0
			for j := i; j < len(lines); j++ {
				for _, ch := range lines[j] {
					if ch == '{' {
						depth++
					} else if ch == '}' {
						depth--
					}
				}
				if depth == 0 && j > i {
					getterEnd = j
					break
				}
			}
			break
		}
	}
	if getterStart < 0 || getterEnd < 0 {
		t.Fatalf("找不到 policy() 函数体 —— 结构被改动了，请同步本判据")
	}
	var bad []string
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if !retryPolReadRe.MatchString(trimmed) {
			continue
		}
		if i >= getterStart && i <= getterEnd {
			continue // policy() 内部，合法
		}
		// 赋值写点合法（须在锁内，但那是另一条判据的事；此处只禁「读」）
		if strings.Contains(trimmed, "e.retryPol =") || strings.Contains(trimmed, "e.retryPol=") {
			continue
		}
		bad = append(bad, "L"+itoaSimple(i+1)+": "+trimmed)
	}
	if len(bad) > 0 {
		t.Fatalf("发现 %d 处 retryPol 锁外直读（应改用 policy()）：\n%s", len(bad), strings.Join(bad, "\n"))
	}
}

func itoaSimple(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
