package engine

import (
	"strings"
	"testing"
)

// TestPruneBurnMessage 钉住 round 148 的机制缺口：每日额度 tasksToday() 数的是**队列现存记录**，
// 而启动期 PruneTasks 按 maxTerminalTasks 删终态记录 ⇒ 一次重启就把当日计数回收掉。
//
// 实测（agent 根，14:25 那次部署重启）：重启前"今日非 rejected"= 201 条（已 >= DailyCap 200，
// 自主与模板两条供货链都在静默 return）；重启后 audit.jsonl 记「按上限删 41130 条、保留 5000 条」，
// 同口径只剩 6 条 ⇒ 额度凭空恢复，而没有任何一条日志说"额度是被裁剪清掉的"。
// 本函数只负责让这件事**可见**（改额度口径是行为改变，登记 T105 等更多窗口样本）。
func TestPruneBurnMessage(t *testing.T) {
	cases := []struct {
		name     string
		before   int
		after    int
		cap      int
		wantWarn bool
	}{
		{"回收后跌到上限之下 ⇒ 必须发声", 201, 6, 200, true},
		{"恰好等于上限 ⇒ 上限仍生效，不发声", 250, 200, 200, false},
		{"回收后仍在上限之上 ⇒ 停机本就该继续，不发声", 400, 300, 200, false},
		{"没回收（裁剪后反而多了）⇒ 不发声", 5, 5, 200, false},
		{"未启用上限 cap=0 ⇒ 不发声", 300, 3, 0, false},
		{"负数上限（异常配置）⇒ 不发声", 300, 3, -1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, warn := pruneBurnMessage(tc.before, tc.after, tc.cap)
			if warn != tc.wantWarn {
				t.Fatalf("before=%d after=%d cap=%d 的告警意愿 = %v，want %v（msg=%q）",
					tc.before, tc.after, tc.cap, warn, tc.wantWarn, msg)
			}
			if !warn {
				if msg != "" {
					t.Fatalf("不告警时不该给文案：%q", msg)
				}
				return
			}
			// 告警文案必须自带三个数：运维只看到一句话不知道该信哪个口径（本会话的读数纪律）
			for _, need := range []string{"201", "6", "200"} {
				if !strings.Contains(msg, need) {
					t.Fatalf("文案缺少数字 %q（须含裁剪前/后/上限）：%q", need, msg)
				}
			}
		})
	}
}
