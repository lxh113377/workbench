package modelpool

// errtext_contract_test.go -- 钉住 errTextBounded 的**截断契约**（2026-10-25 round 177）。
//
// 为什么需要：本函数原名 errText，与 engine/main 里同名函数「返回完整 err.Error()」契约不同 ——
// 属本仓在册的「同名不同契约」漂移族。改名后行为一字未改，但契约必须由测试守住，
// 否则将来有人「顺手统一」成不截断，状态/原因字串的长度界就悄悄消失。
import (
	"errors"
	"strings"
	"testing"
)

func TestErrTextBoundedTruncatesTo200(t *testing.T) {
	if got := errTextBounded(nil); got != "" {
		t.Fatalf("nil 应返回空串，实际 %q", got)
	}
	short := errors.New("短错误")
	if got := errTextBounded(short); got != "短错误" {
		t.Fatalf("短错误应原样返回，实际 %q", got)
	}
	long := errors.New(strings.Repeat("x", 500))
	got := errTextBounded(long)
	if len(got) > 200 {
		t.Fatalf("必须截断到 200 字符以内，实际 %d", len(got))
	}
	if len(got) == 0 {
		t.Fatal("截断后不应为空（否则状态里看不到任何原因）")
	}
}
