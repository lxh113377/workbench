package main

import "testing"

// TestParseIntArgRejectsHalfNumbers 钉住 parseIntArg 的**整串**语义。
//
// 为什么需要（2026-10-25 round 156 实测缺陷）：此前的实现用 fmt.Sscanf(s, "%d", &v)，
// 它只取前导整数、忽略尾部残留 —— 于是 "12abc"→12、"1e3"→1、"12.5"→12 全部静默通过，
// 最刺眼的是 "0x10"→0（--port 拿到 0 = 内核随机分配端口，日志里看不出参数被吃掉了）；
// 而函数上方注释写的是「拒绝 "12abc" 这类半数字」，**注释与实现正好相反，且当时零测试**。
// 这类「注释承诺 vs 实现行为」的漂移，本仓此前的门禁抓不到（check_comment_staleness 只抓
// 「只剩/共 N」这类数字断言）—— 只能靠本测试把语义钉住。
func TestParseIntArgRejectsHalfNumbers(t *testing.T) {
	rejected := []string{
		"12abc", // 半数字：尾部残留（原实现静默返回 12）
		"1e3",   // 科学计数法：--seconds 1e3 曾被当成 1（原实现）
		"12.5",  // 小数（原实现返回 12）
		"0x10",  // 十六进制：原实现静默返回 0，端口变成内核随机分配
		"abc",
		"",
		"   ",

		"5 6",
	}
	for _, s := range rejected {
		v, err := parseIntArg(s)
		if err == nil {
			t.Errorf("parseIntArg(%q) = (%d, nil)，应报错（整串必须是十进制数字）", s, v)
		} else if v != 0 {
			t.Errorf("parseIntArg(%q) 出错时还返回了 %d，应返回 0", s, v)
		}
	}
	accepted := []struct {
		in   string
		want int
	}{
		{"42", 42},
		{" 42 ", 42}, // 两侧空白先剥掉
		{"17890", 17890},
		{"-5", -5}, // Atoi 视其为合法十进制整数；「非正不许」是调用点职责
	}
	for _, c := range accepted {
		v, err := parseIntArg(c.in)
		if err != nil {
			t.Errorf("parseIntArg(%q) 报错 %v，应通过", c.in, err)
			continue
		}
		if v != c.want {
			t.Errorf("parseIntArg(%q) = %d，期望 %d", c.in, v, c.want)
		}
	}
}

// TestUIProbeCLIReportsBadIntArgs 钉住「参数错误必须走 rc=2」这条自述契约。
//
// 本文件头注声明「退出码：… / 2 参数错误」，但修复前 --port/--seconds 解析失败时是
// **静默沿用默认值**（连提示都没有），于是 rc=2 从不因数字参数错误而触发 —— 契约形同虚设。
// 本测试只覆盖「在起服务之前就返回 2」的路径，因此不会真的监听端口。
func TestUIProbeCLIReportsBadIntArgs(t *testing.T) {
	cases := [][]string{
		{"--dir", "unused-dir", "--port", "12abc"},
		{"--dir", "unused-dir", "--port", "0x10"},
		{"--dir", "unused-dir", "--port", "0"},
		{"--dir", "unused-dir", "--port", "70000"},
		{"--dir", "unused-dir", "--seconds", "1e3"},
		{"--dir", "unused-dir", "--seconds", "0"},
		{"--dir", "unused-dir", "--seconds", "-5"},
	}
	for _, args := range cases {
		// 关键：这些用例都必须在「构建 layout / 监听端口」之前返回，故用不存在的目录也安全。
		if rc := runUIProbeCLI(args); rc != 2 {
			t.Errorf("runUIProbeCLI(%v) = %d，期望 2（参数错误）", args, rc)
		}
	}
}
