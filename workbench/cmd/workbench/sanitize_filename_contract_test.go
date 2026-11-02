package main

// sanitize_filename_contract_test.go —— 守护导出文件名的净化不变式（2026-11-02 新增）。
//
// 为什么需要（gate-spec ㉑「同构机制的覆盖面必须逐个对齐」的实测应用）：
//   本仓有两个「把外部可控字符串变成安全落盘名」的守门函数，而覆盖不对齐：
//     · artifact.SafePath        —— 已有 TestSafePath 与 engine 两份路径门测试；
//     · sanitizeFileName（本件） —— 守 CLI 导出文件名（runExportCLI 用任务标题拼名），此前零测试。
//   输入确实可控：面板手工创建虽校验了非空标题，但自主模式标题来自 LLM 草案
//   （engine_autonomy.go 的 draft.Title）与用户设的 goal 标题 ⇒ 可含任意字符。
//   git grep 实测该函数**没有任何测试引用**（只有定义与 runExportCLI 一处调用）⇒ Replacer 漏掉
//   一个分隔符，导出就会带上路径分量，而没有任何判据会红。这就是 ㉑ 说的「缺一份就是黑洞」：
//   不是漏报某个指标，而是这条防线将来失效时无人知晓（与本仓「基线单向膨胀」同机理）。
//
// 断言口径：锁**不变式**而不锁具体输出串（实现细节改动不该假红）——
//   1. 净化名不得含路径分隔符与冒号（冒号兼防 Windows 交替数据流 file:stream）；
//   2. 拼上调用处的 .md 后缀后仍是单段，且 Join 之后必然落在目标目录内；
//   3. 截断按 rune 不按 byte（汉字标题不得被切出非法 UTF-8）；
//   4. 反向锁一件事：「安全」其实半依赖调用处恒补的后缀（标题恰为两点时），若将来有人
//      把它拿去用在「不补后缀」的地方，第 4 条会提醒去查调用点。
//
// 注：本文件刻意不出现任何反斜杠字面量 —— 反斜杠与双引号一律由 bs / dq 变量（rune 码位）
//   表达。既是跨平台稳的写法，也规避本仓反复登记的「手打转义把文件写坏」这一类坑。

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// bs 是反斜杠、dq 是双引号；用码位表达，避免本文件出现任何转义层。
var (
	bs = string(rune(92))
	dq = string(rune(34))
)

func TestSanitizeFileNameStaysSingleSegment(t *testing.T) {
	// 每条都是「净化一旦漏掉提供符就会逃出目录」的形态
	dangerous := []string{
		"../etc/passwd",
		"..",
		".." + bs + bs + "windows" + bs + bs + "system32",
		"a" + bs + "b",
		"C:" + bs + bs + "temp" + bs + bs + "x",
		"a:b:stream",
		"a/*b?" + dq + "d<e>|f",
		strings.Repeat("../", 8),
		"",
	}
	dir := t.TempDir()
	// 危险字符集与生产 Replacer 的键一致
	bads := []string{"/", bs, ":", "*", "?", dq, "<", ">", "|"}
	for _, title := range dangerous {
		// 调用处契约：runExportCLI 固定补 .md 后缀
		name := sanitizeFileName(title) + ".md"
		for _, bad := range bads {
			if strings.Contains(name, bad) {
				t.Fatalf("title=%q 净化名仍含危险字符 %q: %q", title, bad, name)
			}
		}
		// 单段：Base 等于自身，说明没有目录层级
		if filepath.Base(name) != name {
			t.Fatalf("title=%q 产出了多段路径: %q", title, name)
		}
		// 落盘必在目标目录内
		joined := filepath.Join(dir, name)
		if filepath.Dir(joined) != dir {
			t.Fatalf("title=%q 逃出了目标目录: %q", title, joined)
		}
	}
}

func TestSanitizeFileNameTruncatesByRuneNotByte(t *testing.T) {
	long := strings.Repeat("标", 80)
	got := sanitizeFileName(long)
	if n := len([]rune(got)); n != 60 {
		t.Fatalf("60 rune 上限应恰好截到 60 个字符，实得 %d", n)
	}
	if !utf8.ValidString(got) {
		t.Fatal("截断产生非法 UTF-8（Windows 上会变成乱码文件名）")
	}
}

// 反向对照：凝结「安全性归谁」—— 净化本身不消除两点，是后缀才让它不再是裸上跳名。
func TestSanitizeFileNameSafetyDependsOnSuffix(t *testing.T) {
	if got := sanitizeFileName(".."); got != ".." {
		t.Fatalf("若未来改变了「标题恰为两点时原样返回」这一事实，请同步审查调用处后缀契约（得 %q）", got)
	}
	if got := sanitizeFileName("..") + ".md"; got == ".." || got == "." {
		t.Fatalf("带后缀仍是裸上跳名: %q", got)
	}
}
