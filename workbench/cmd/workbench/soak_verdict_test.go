package main

// soak_verdict_test.go 守护 --soak 与总验收 A7 的**同一判据**（2026-10-19 现场缺陷）。
//
// 现场：报表同时输出 `longEnough: false` 与 `verdict: leak / exitCode: 2`，
// 即「自己声明证据不足，却给出已证泄漏的结论」。
// 根因有二，且第二层才是要命的：
//   1) 原逻辑只把 `ok` 在短跨度下降级为 unknown，**漏掉了 leak**；
//   2) soak CLI 与总验收 A7 **各自实现了一遍**同一判据，只修一处 => 漂移。
//
// 现在唯一实现是 soakConclusion（acceptance.go），本测试直接钉住它，
// 任何人再想「在某个调用点单独写一套」都会被这里的矩阵挡住。

import "testing"

// TestSoakConclusionMatrix 钉住唯一实现的四象限（+ 未知态）。
func TestSoakConclusionMatrix(t *testing.T) {
	const minHours = 24.0
	cases := []struct {
		trend string
		span  float64
		want  string
		code  int
	}{
		// 短跨度：无论趋势如何，一律 unknown/3（现场缺陷的关键回归）
		{"leak", 3.17, "unknown", 3},
		{"ok", 3.17, "unknown", 3},
		{"unknown", 3.17, "unknown", 3},
		// 跑满：才有资格下结论
		{"leak", 24.0, "leak", 2},
		{"ok", 24.0, "ok", 0},
		{"unknown", 30.0, "unknown", 3},
		// 边界：恰好等于 minHours 视为足够（>= 语义）
		{"ok", 23.99, "unknown", 3},
		{"ok", 24.00, "ok", 0},
	}
	for _, c := range cases {
		v, k := soakConclusion(c.trend, c.span, minHours)
		if v != c.want || k != c.code {
			t.Fatalf("trend=%s span=%.2f => (%s,%d), want (%s,%d)", c.trend, c.span, v, k, c.want, c.code)
		}
	}
}

// TestSoakVerdictRequiresMinHours 断言 A7 侧的 checkState 映射与之一致。
func TestSoakVerdictRequiresMinHours(t *testing.T) {
	cases := []struct {
		trend string
		span  float64
		want  checkState
	}{
		{"leak", 3.17, stateUnverified}, // 以前这里是 stateFail —— 现场缺陷
		{"leak", 24.0, stateFail},
		{"ok", 24.0, statePass},
		{"ok", 5.0, stateUnverified},
		{"unknown", 30.0, stateUnverified},
	}
	for _, c := range cases {
		if got := classifySoakVerdict(c.trend, c.span); got != c.want {
			t.Fatalf("trend=%s span=%.2f => %s, want %s", c.trend, c.span, got, c.want)
		}
	}
}

// TestSoakSidesAgree 断言「CLI 结论」与「A7 结论」在同一输入下语义一致 ——
// 这是防止两处实现再次漂移的护栏。
func TestSoakSidesAgree(t *testing.T) {
	const minHours = 24.0
	spans := []float64{0.1, 1.65, 3.17, 23.99, 24.0, 30.0, 100.0}
	trends := []string{"leak", "ok", "unknown", ""}
	for _, tr := range trends {
		for _, sp := range spans {
			cliVerdict, cliCode := soakConclusion(tr, sp, minHours)
			a7State := classifySoakVerdict(tr, sp)
			// 映射约定：cli ok<->pass、leak<->fail、unknown<->unverified
			var wantState checkState
			switch cliVerdict {
			case "ok":
				wantState = statePass
			case "leak":
				wantState = stateFail
			default:
				wantState = stateUnverified
			}
			if a7State != wantState {
				t.Fatalf("两侧漂移：trend=%q span=%.2f cli=(%s,%d) a7=%s", tr, sp, cliVerdict, cliCode, a7State)
			}
			// 退出码约定：2 仅当 leak，0 仅当 ok，否则 3
			switch cliCode {
			case 2:
				if cliVerdict != "leak" {
					t.Fatalf("退出码 2 只允许 leak，得到 %s", cliVerdict)
				}
			case 0:
				if cliVerdict != "ok" {
					t.Fatalf("退出码 0 只允许 ok，得到 %s", cliVerdict)
				}
			case 3:
			default:
				t.Fatalf("非法退出码 %d", cliCode)
			}
		}
	}
}
