package web

import (
	"strings"
	"testing"
)

// 空白字符常量：用 rune 数值构造，使本文件不含任何转义序列或多字节字面量。
const (
	spaceCh = rune(32)
	tabCh   = rune(9)
	crCh    = rune(13)
)

// nl 换行符；ws 空白裁剪集（空格 + 制表）。
func nl() string { return string(rune(10)) }

func ws() string { return string(spaceCh) + string(tabCh) }

// TestSharedHelpersRemainIdentical 钉住三份内嵌 JS 里「同职责复制体」必须逐字节相同（空白除外）。
//
// 背景（2026-10-25 round 122-124 全量实测）：前端是 appJS / govJS / poolJS 三份复制粘贴的内嵌
// 脚本（各自 IIFE、不共享模块），这是「单 EXE 零依赖、无构建链」架构的代价。round 108 只为 api
// 立过契约；本轮把全部同名跨 blob 定义逐个过完（app 56 / gov 21 / pool 28 个），分成两类。
//
// 下表只列 A 类（同一工具的复制体，要求相同）。B 类同名异义**不许**要求相同，已核实并排除：
// esc（app/pool 用表查、gov 用内联对象，行为等价且各有逐 blob 行为测试，实测使用数 60/31/56）；
// renderOverview / render / load（三处渲染不同 DOM，签名参数都不同）；
// STATE_TEXT（app 是任务状态 12 键、pool 是模型健康 5 键，键完全不重叠）。
//
// A 类为什么判「相同」而不是各自测行为：它们语义上就是同一工具被抄三份，任何单边改动（超时值、
// 错误兜底、文案时长）会让三个页面静默表现不一致，而没有测试会失败。第一个实证就是 toast：
// app/gov=2600、pool=3000，两值自「仓库损毁后全量恢复」提交 a5cf134 就并存，无注释说明有意。
//
// 提取按缩进取块，不用花括号配对：blob 里既有块体函数也有箭头表达式体（const fold 是 => 一行），
// 花括号配对会一路吞进后面的函数。这坑本轮真踩过 —— 用花括号配对时 fold 与 renderOverview 报出
// 相同长度，于是把逐字节相同的 fold 误分类成「同名异义」写进了报告；换缩进法重测才发现。
func TestSharedHelpersRemainIdentical(t *testing.T) {
	blobs := map[string]string{"appJS": appJS, "govJS": govJS, "poolJS": poolJS}
	cases := []struct {
		name string
		in   []string
		want int
	}{
		{"api", []string{"appJS", "govJS", "poolJS"}, 630},
		{"toast", []string{"appJS", "govJS", "poolJS"}, 214},
		{"TOKEN", []string{"appJS", "govJS", "poolJS"}, 35},
		{"armConfirm", []string{"appJS", "poolJS"}, 343},
		{"disarmArm", []string{"appJS", "poolJS"}, 212},
		{"fmtTime", []string{"appJS", "poolJS"}, 79},
		{"fold", []string{"appJS", "poolJS"}, 127},
	}
	for _, c := range cases {
		var ref, refName string
		got := 0
		for _, bn := range c.in {
			body, ok := declBlock(blobs[bn], c.name)
			if !ok {
				// 结构变了必须显式同步本表：否则判据退化成「什么都没比」而恒绿。
				t.Errorf("%s 在 %s 里找不到定义 —— 判据失去受理面（空转），请同步本测试", c.name, bn)
				continue
			}
			got++
			norm := squashJS(body)
			if ref == "" {
				ref, refName = norm, bn
				continue
			}
			if norm == ref {
				continue
			}
			t.Errorf("%s 的复制体分叉：%s 与 %s 不一致\n  参考 %s 长度 %d，本份 %s 长度 %d\n  要么改回一致，要么在注释与本表里写明为什么必须不同", c.name, refName, bn, refName, len(ref), bn, len(norm))
		}
		if got == len(c.in) && len(ref) < c.want/2 {
			t.Errorf("%s 归一长度 %d 远低于登记值 %d —— 提取法可能退化，判据在比空气", c.name, len(ref), c.want)
		}
	}
}

// declBlock 按缩进取整条声明：声明行 + 后续缩进严格更深的连续行；空行或缩进回同层即结束。
func declBlock(s, name string) (string, bool) {
	lines := strings.Split(s, nl())
	for i, l := range lines {
		ind := indentOf(l)
		if !declares(strings.TrimLeft(l, ws()), name) {
			continue
		}
		out := []string{l}
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" || indentOf(lines[j]) <= ind {
				break
			}
			out = append(out, lines[j])
		}
		return strings.Join(out, nl()), true
	}
	return "", false
}

var kwPrefixes = []string{"const ", "let ", "var ", "function ", "async function "}

// declares 判断去缩进后的行首是否为「关键字 + 目标名 + = 或 (」的声明。
func declares(head, name string) bool {
	for _, kw := range kwPrefixes {
		if !strings.HasPrefix(head, kw) {
			continue
		}
		rest := strings.TrimLeft(strings.TrimPrefix(head, kw), ws())
		if !strings.HasPrefix(rest, name) {
			continue
		}
		tail := strings.TrimLeft(rest[len(name):], ws())
		return strings.HasPrefix(tail, "=") || strings.HasPrefix(tail, "(")
	}
	return false
}

func indentOf(l string) int {
	n := 0
	for _, r := range l {
		switch r {
		case spaceCh:
			n++
		case tabCh:
			n += 2
		default:
			return n
		}
	}
	return n
}

// squashJS 先经 stripLineComments 剥注释、再去空白，使判据只关心实际代码。
// 为什么复用 stripLineComments 而不是本文件自己处理注释：它在 no_dialog_contract_test.go
// 里已被 TestStripLineCommentsIsSound 四项自检验过（注释不误报、字符串里的 // 不漏剥、
// 真调用不被剥、行尾注释剥净）。本判据最初的失败就是「行尾注释」——
// appJS 的 armConfirm 带一条行尾注释、poolJS 多两行整行注释，代码逐字相同却被判分叉：
// **注释差异不是行为分叉**，把它算进判据会制造假红（与 round 64 家族同源，方向相反）。
// 复用经审计的实现，比在这里再写一个更难保证正确的版本好。
func squashJS(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(stripLineComments(s), nl()) {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		for _, r := range line {
			if r == spaceCh || r == tabCh || r == crCh {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}
