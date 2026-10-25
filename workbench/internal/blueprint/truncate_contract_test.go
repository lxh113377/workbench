package blueprint

// truncate_contract_test.go 钉住 truncate 的**预算契约**（2026-10-26 round 222）。
//
// 起因：本包这份 truncate 是房内第三份漂移版（另两份 internal/autonomy 与 internal/engine 已在
// round 197 被同款测试钉成同一契约）。实测三处不同：
//   ① n<0 会 panic（真 Go 实测 slice bounds out of range [:-1]）；n=0 不 panic 但输出「…」越预算；
//   ② 输出恒比预算多 1 个字符 —— 旧版是 r[:n] 之后再加省略号，声称 3000 实际 3001；
//   ③ 不幂等（截过的结果再截会再多一个省略号），而失败证据/命令文案会在多层包装里被反复截。
// 本包真实预算是 3000（上一轮失败证据）、200（解析失败原文）、60（被丢弃的验收命令）、40（摘要行），
// 全是预算语义 ⇒ 越界那 1 个字符是缺陷不是风格。跨包收敛成公共件仍是登记项；在收敛之前，
// 先用测试把三份钉成同一契约，任何一侧漂回去都会当场判红。

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateHonorsBudget(t *testing.T) {
	zh := "失败证据：验收命令未通过，stderr 尾部是中文与 ASCII 混合内容。" + strings.Repeat("补充", 100)
	// 覆盖本包真实预算 + 边界（0/1/2）与刚过界的值
	for _, n := range []int{0, 1, 2, 12, 39, 40, 41, 60, 200, 3000} {
		got := truncate(zh, n)
		if !utf8.ValidString(got) {
			t.Fatalf("n=%d 产出残缺 UTF-8", n)
		}
		if utf8.RuneCountInString(zh) > n && utf8.RuneCountInString(got) > n {
			t.Fatalf("预算契约被破坏：n=%d（声称上限）实际输出 %d 个字符", n, utf8.RuneCountInString(got))
		}
	}
}

func TestTruncateNeverPanicsOnNonPositiveN(t *testing.T) {
	// 旧版在这里会 panic：n<0 时 r[:n] 切片越界
	for _, n := range []int{0, -1, -100} {
		if got := truncate("任意中文内容", n); got != "" {
			t.Fatalf("n=%d 应返回空串，实际 %q", n, got)
		}
	}
}

func TestTruncateShortInputUntouched(t *testing.T) {
	if got := truncate("短串", 3000); got != "短串" {
		t.Fatalf("未超预算不应改动内容，实际 %q", got)
	}
	if got := truncate("汉字测试", 1); utf8.RuneCountInString(got) != 1 || strings.Contains(got, "…") {
		t.Fatalf("n=1 应为 1 个字符且不含省略号（否则 2 个字符越预算），实际 %q", got)
	}
}

func TestTruncateIdempotent(t *testing.T) {
	// 失败证据与被丢弃文案会被多层包装反复截：截过的结果再截必须不变
	once := truncate(strings.Repeat("x", 300), 60)
	if twice := truncate(once, 60); twice != once {
		t.Fatalf("不幂等：%d -> %d 字符", utf8.RuneCountInString(once), utf8.RuneCountInString(twice))
	}
	// 幂等性必须在预算内成立（旧版会把 60 截成 61）
	if utf8.RuneCountInString(once) > 60 {
		t.Fatalf("一次截断就越了预算：%d > 60", utf8.RuneCountInString(once))
	}
}
