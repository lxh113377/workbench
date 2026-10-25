package main

import (
	"os"
	"strings"
	"testing"
)

// TestNoFmtScanInCLIArgs 是本包的**家族守卫**：禁止再用 fmt.Sscan / fmt.Sscanf 解析命令行数字。
//
// 为什么需要一个「禁止某种写法」的守卫（2026-10-25 round 156-157 两次实测）：同一个 bug 在本包
// 出现了三次、形态相同 —— 用 fmt.Sscan* 解析数字时它会**只取一段、对尾部残留不报错**：
//
//	· ui_probe.go 的 parseIntArg：Sscanf(%d) 下 "12abc"→12、"1e3"→1、"0x10"→0（端口静默变随机）；
//	· prune.go 的保留天数：Sscan 下 "3abc"→3、"0x10"→16（**不可逆删除**按错误年龄执行）；
//	· pool_cli.go 的日志条数：Sscanf 的**返回值被丢弃**，"5abc"→5、"abc"→静默沿用 30。
//
// 三次都属于「注释/文档承诺 A，实现做 B」且**没有测试**。修完三次之后，真正该防的是**第四次**：
// 这类写法太顺手，新加的 CLI 很容易再写一遍。本守卫把「禁止」变成可执行的事实，而不是一句注释。
//
// 口径（窄、零误报）：只扫本包目录下的 .go 文件，**先剥注释**再找 needle；
// needle 用拼接构造，避免守卫文件把自己扫出来。
// 自我纠错（2026-10-25 本轮）：首版针写错（详见下），变异 B 暴露为 SILENT；已按实测修正。
// 局限（如实写）：只按行剥 "//" 开头/行内第一个 "//"，不处理块注释与字符串里的 "//"；
// 本包没有这类写法，若将来有了，本守卫可能误报 —— 那时按实际情况调整，而不是删守卫。
func TestNoFmtScanInCLIArgs(t *testing.T) {
	// 针必须覆盖 **Sscan 与 Sscanf** 两种形态。首版我写成 "fmt.S" + "can"（= fmt.Scan），
	// 它不是 fmt.Sscan/fmt.Sscanf 的子串（S 之后是 s 不是 c）—— 变异 B 实测让守卫 SILENT，
	// 即守卫只挡了从未出现过的写法、漏掉了真正出事的那两种。以下按实测修好。
	needle := "fmt.Ss" + "can" // = fmt.Sscan，同时是 fmt.Sscanf 的前缀
	// nl 是换行的字节形式：本仓写 Go 源码时我习惯用 charCode 构造避免转义，但**构造的是「表达式」不是「字面量」** ——
	// 首版这里被我拼成了字面字符串 "string(rune(10))"，于是 Split 只得到「整文件一块」，再被首个 // 截断，
	// 针永远匹配不到（文件级诊断能看见针、行级永远不报）。变异 B 把它暴露成 SILENT —— 已按实测改成本变量。
	nl := string(rune(10))
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读包目录失败: %v", err)
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读 %s 失败: %v", name, err)
		}
		scanned++
		for i, line := range strings.Split(string(raw), nl) {
			code := line
			if idx := strings.Index(code, "//"); idx >= 0 {
				code = code[:idx]
			}
			if strings.Contains(code, needle) {
				t.Errorf("%s:%d 用了 %s —— 命令行的数字参数必须用 parseIntArg（整串解析）；", name, i+1, needle)
			}
		}
	}
	// 受理面哨兵：一个 .go 都没扫到就说明 cwd 不是包目录，判据在比空气。
	if scanned == 0 {
		t.Fatal("没扫到任何 .go 文件 —— 判据失去受理面（测试 cwd 不是包目录？）")
	}
}
