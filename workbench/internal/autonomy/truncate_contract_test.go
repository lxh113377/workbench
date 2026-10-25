package autonomy

// truncate_contract_test.go 钉住 truncateRunes 的**预算契约**（2026-10-26 round 197）。
//
// 起因：internal/engine 与 internal/autonomy 各有一份同名函数（跨包收敛仍是登记项），实测两版
// 契约不同 —— engine 旧版是 string(r[:n]) 加省略号，输出恒比预算多 1 个字符（n=2 给 3 个）。
// 调用面全是预算语义（事件 reason 200/160/120、报告 stderr/stdout 尾部 1200）。这属本仓在册的
// 「同名不同契约」族（errText / baseName 都栽过）；在收敛成公共件之前，先用测试把两版钉成同一契约，
// 任何一侧漂回去都会当场判红。

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateRunesHonorsBudget(t *testing.T) {
	zh := "汉字测试内容较长需要截断才会触发预算" + strings.Repeat("补充", 100)
	for _, n := range []int{0, 1, 2, 12, 49, 50, 51, 120} {
		got := truncateRunes(zh, n)
		if !utf8.ValidString(got) {
			t.Fatalf("n=%d 产出残缺 UTF-8", n)
		}
		if utf8.RuneCountInString(zh) > n && utf8.RuneCountInString(got) > n {
			t.Fatalf("预算契约被破坏：n=%d（声称上限）实际输出 %d 个字符", n, utf8.RuneCountInString(got))
		}
	}
}

func TestTruncateRunesShortInputUntouched(t *testing.T) {
	if got := truncateRunes("短串", 10); got != "短串" {
		t.Fatalf("未超预算不应改动内容，实际 %q", got)
	}
	if got := truncateRunes("长字符串测试", 1); utf8.RuneCountInString(got) != 1 || strings.Contains(got, "…") {
		t.Fatalf("n=1 应为 1 个字符且不含省略号，实际 %q", got)
	}
}

func TestTruncateRunesIdempotent(t *testing.T) {
	// 事件 reason 会被多层包装反复截：截过的结果再截必须不变
	once := truncateRunes(strings.Repeat("x", 300), 120)
	if twice := truncateRunes(once, 120); twice != once {
		t.Fatalf("不幂等：%d -> %d 字符", utf8.RuneCountInString(once), utf8.RuneCountInString(twice))
	}
}
