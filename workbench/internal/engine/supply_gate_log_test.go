package engine

import "testing"

// TestSupplyBlockedEvidence 钉住「供料门被挡」的判定口径（round 128 · T83 第一块砖）。
//
// 现场（2026-10-30 09:10 实读）：12 条模板任务 parked（queued、attempts=0/3、age=37 分钟），
// 而供料门是 `if e.q.Len() == 0` ⇒ 自主与模板补货同时被挡住；tick 每 3 秒一次 ⇒ 每小时静默跳过
// 约 1200 次，日志里一条痕迹都没有（T82 的线上判据取不到，正是这个原因）。
//
// 两条轴必须分开：blocked = 门关着；worthLog = 「有货但没人在干活」（parked 饿死供料）。
// 只按 blocked 记日志会刷屏正常态；只按 worthLog 返回 blocked 会漏掉真故障。
func TestSupplyBlockedEvidence(t *testing.T) {
	cases := []struct {
		name     string
		queued   int
		inflight int
		wantB    bool
		wantW    bool
	}{
		{"队列空 ⇒ 门开着，不记", 0, 0, false, false},
		{"队列空但有在跑（异常组合）也不算被挡", 0, 2, false, false},
		{"有货且正在消化 ⇒ 正常态，不刷屏", 12, 1, true, false},
		{"有货且无在跑 ⇒ parked 饿死供料，必须发声", 12, 0, true, true},
		{"单条 parked 也要发声（T70 的最小形态）", 1, 0, true, true},
		{"负数按空处理（防御性，不给假告警）", -1, 0, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, w := supplyBlockedEvidence(tc.queued, tc.inflight)
			if b != tc.wantB || w != tc.wantW {
				t.Fatalf("supplyBlockedEvidence(%d,%d) = (%v,%v)，应为 (%v,%v)",
					tc.queued, tc.inflight, b, w, tc.wantB, tc.wantW)
			}
			// 不变量：值得记录必然是关着门（不可能"门开着却值得告警"）
			if w && !b {
				t.Fatal("不变量被破坏：worthLog 蕴含 blocked")
			}
		})
	}
}
