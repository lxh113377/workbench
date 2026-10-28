package engine

import (
	"os"
	"strings"
	"testing"
)

// TestDailyCountSingleSource 是每日口径的**源文本不变量**（round 60 · T57）。
//
// 为什么钉文本而不是行为：口径分叉的危害（engine.go:876-879 在册要求「两处必须完全一致」，
// 历史成因是 12 小时整机零产出）只有在「一天里既有很多 rejected 草案、又有后继要评估」时才会显形，
// 那种现场条件在单测里造出来等于重造一遍引擎；而「谁在 engine.go 里再写一次 todayStart 就地重算」
// 这个**回归本身**，用文本断言零成本、且永远有效 —— tasksToday() 是唯一口径入口。
func TestDailyCountSingleSource(t *testing.T) {
	src, err := os.ReadFile("engine.go")
	if err != nil {
		t.Fatalf("读 engine.go 失败: %v", err)
	}
	body := string(src)
	if strings.Contains(body, "todayStart") {
		t.Fatal("engine.go 里出现了就地重算的 todayStart —— 每日计数必须统一走 tasksToday()（剔 rejected），两处口径分叉曾造成 12 小时整机零产出")
	}
	if !strings.Contains(body, "daily := e.tasksToday()") {
		t.Fatal("engine.go 未通过 tasksToday() 取每日计数")
	}
}
