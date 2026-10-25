package engine

// errtext_contract_test.go -- 钉住 errText 的**不截断契约**（2026-10-25 round 177）。
//
// 与 modelpool.errTextBounded（截断到 200）**刻意不同**：engine 侧需要完整错误文本用于诊断。
// 两条契约由各自的测试守住 ⇒ 「同名不同契约」的漂移不可能再悄悄发生。
import (
	"errors"
	"strings"
	"testing"
)

func TestErrTextKeepsFullMessage(t *testing.T) {
	long := errors.New(strings.Repeat("y", 500))
	if got := errText(long); got != long.Error() {
		t.Fatalf("engine.errText 必须返回完整错误文本（不截断），实际长度 %d", len(got))
	}
}
