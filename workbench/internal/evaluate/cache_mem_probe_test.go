package evaluate

// 缓存自身的内存成本（2026-10-28）。
//
// 省 CPU 不能拿无界内存换：本仓 supervisor 的 MemLimitMB 默认 700，现场曾报过常驻
// 170MB / 高峰 752MB（store.PruneTasks 注释在册）。每个窗口条目挂两个 3-gram 集合
// 到底占多少，必须量化。
//
// 三次方法迭代（前两版的失败都留在注释里，避免下一个人重踩）：
//   v1「填充前后 HeapAlloc 之差」⇒ 得到负数（-0.27 / -1.26 / -3.71 MB）。基线被 GC
//      清扫时机污染：填充期间别处的临时对象同时被回收，差值不只属于缓存。负读数即证据。
//   v2「两个同内容窗口，只填一边再填另一边」⇒ 同一份缓存两次测出 -0.21MB 与 +2.29MB，
//      噪声（约 2.5MB）与信号（2.3MB）同量级，单次差值不可用。
//   v3（本版）「五个同内容窗口交替填充，丢弃第一次读数，取其后四次的中位数」：
//      每个窗口的缓存内容完全相同 ⇒ 真值相同；首填吞掉清扫噪声后，后续读数收敛。
//      同时判据要求四次读数的极差 ≤ 中位数的 30%，否则视为测量不稳定直接失败。
//   另外硬编码一项行为判据：超过 maxCachedShingleEntries 的条目**不得**被长期缓存
//   （内存护栏生效），短文必须缓存且第二次调用命中同一个 map（缓存真的生效）。
//   调用形态必须是生产形态（只调 titleSh/fullSh），第一版写成强行赋值，等于把护栏旁路。
//
// 默认跳过。跑法（可复现）：
//   WB_MEM=1 go test ./internal/evaluate -run TestCacheMemoryCost -count=1 -v

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"testing"
)

// maxInt64 局部小助手（本文件只用一次，不跨文件复用别处的同名函数）。
func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// mkVariedWindow 造内容**互不相同**的窗口。
//
// 为什么不能沿用 mkWindow 的重复句：相似度集合存的是 distinct 3-gram，重复句会让
// distinct 数被封顶，实测 100/900/3000 rune 三档都得到同一个 2.9KB/条 —— 那就
// 低估了真实长文本的成本（内存预算判据会被假绿蒙过去）。这里每条正文由该条
// 自己的序号生成互不重叠的字符序列，distinct 3-gram 数才随长度真实增长。
func mkVariedWindow(n int, promptRunes int) []recentItem {
	items := make([]recentItem, 0, n)
	for i := 0; i < n; i++ {
		runes := make([]rune, 0, promptRunes)
		seed := uint64(i*2654435761 + 12345)
		for len(runes) < promptRunes {
			seed ^= seed << 13
			seed ^= seed >> 7
			seed ^= seed << 17
			// 取常用汉字区间内的码点，模拟真实中文提示词的字符分布
			runes = append(runes, rune(0x4e00+int(seed%2048)))
		}
		body := string(runes)
		items = append(items, recentItem{
			TaskID:     fmt.Sprintf("task-%d", i),
			Title:      fmt.Sprintf("任务标题示例%d %s", i, body[:8]),
			Prompt:     body,
			TemplateID: fmt.Sprintf("tpl-%d", i%24),
			CreatedAt:  int64(1_700_000_000_000) + int64(i)*1000,
		})
	}
	return items
}
func heapNow() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// fillOne 走生产调用形态：由方法自己决定存不存（护栏在这里生效）。
func fillOne(items []recentItem) {
	for i := range items {
		_ = items[i].titleSh()
		_ = items[i].fullSh()
	}
}

func TestCacheMemoryCost(t *testing.T) {
	if os.Getenv("WB_MEM") != "1" {
		t.Skip("需显式 WB_MEM=1（要测堆增量，需独占 GC 时机）")
	}
	const n = 400
	const rounds = 5
	for _, promptRunes := range []int{100, 900, 3000} {
		wins := make([][]recentItem, rounds)
		for i := range wins {
			wins[i] = mkVariedWindow(n, promptRunes)
		}
		deltas := make([]int64, 0, rounds-1)
		for i := range wins {
			h0 := heapNow()
			fillOne(wins[i])
			h1 := heapNow()
			if i > 0 { // 首吞掉清扫噪声
				deltas = append(deltas, int64(h1)-int64(h0))
			}
			if len(wins[i]) != n {
				t.Fatal("窗口丢失，读数作废")
			}
		}
		sort.Slice(deltas, func(a, b int) bool { return deltas[a] < deltas[b] })
		med := deltas[len(deltas)/2]
		lo, hi := deltas[0], deltas[len(deltas)-1]
		spread := float64(hi-lo) / float64(maxInt64(med, 1))
		t.Logf("窗口 %d 条 / 正文约 %d rune：单份缓存中位=%.2fMB（%.1fKB/条） 极差=[%.2f, %.2f]MB 离散度=%.2f",
			n, promptRunes, float64(med)/1048576.0, float64(med)/1024.0/n,
			float64(lo)/1048576.0, float64(hi)/1048576.0, spread)
		if med <= 0 {
			t.Fatalf("中位数非正（%.2fMB）⇒ 测量仍不可信", float64(med)/1048576.0)
		}
		if spread > 0.3 {
			t.Fatalf("读数离散度 %.2f 超阈值 0.30 ⇒ 环境噪声过大，本判据不下结论", spread)
		}
		// 内存预算：400 条窗口的缓存在最坏档（3000 rune）也不得超过 40MB。
		if med > 40*1024*1024 {
			t.Fatalf("缓存内存越界：单份 %.1fMB 超 40MB 预算（%d 条 x %d rune），与 700MB 自愈阈值冲突，请下调 maxCachedShingleEntries",
				float64(med)/1048576.0, n, promptRunes)
		}
		// 行为判据（护栏 + 命中）：长文不得长期缓存，短文必须缓存并命中同一实例。
		last := wins[rounds-1]
		if promptRunes > maxCachedShingleEntries*2 {
			if last[0].shFull != nil {
				t.Fatalf("内存护栏失效：%d rune 正文仍被长期缓存", promptRunes)
			}
		} else if last[0].shFull == nil {
			t.Fatalf("短文未被缓存（%d rune < 上限），优化没生效", promptRunes)
		}
		p0 := fmt.Sprintf("%p", last[1].shTitle)
		_ = last[1].titleSh()
		if fmt.Sprintf("%p", last[1].shTitle) != p0 {
			t.Fatal("titleSh 第二次调用换了 map ⇒ 缓存未命中")
		}
	}
}
