package web

import (
	"strings"
	"testing"
	"unicode"
)

// TestToastImplsAreIdentical 钉住三份内嵌 JS 的 toast() 必须逐字节相同（空白与注释除外）。
//
// 为什么需要（2026-10-25 实测）：前端基建是 appJS / govJS / poolJS 三份复制粘贴的
// 内嵌脚本（各自 IIFE、不共享模块）。逐副本对比后发现同名 helper 里只有一处
// **实质行为分叉**：toast 的自动隐藏延时
//
//	appJS = 2600ms、govJS = 2600ms、poolJS = 3000ms
//
// git 归因：两个值都自「仓库损毁后全量恢复」那次提交（a5cf134）就并存，代码里
// 没有任何注释说明 3000 是有意为之 —— 属复制漂移，不是设计。
// 后果不算重（消息停留时长不一致，用户在模型池页看到的通知比别处晚 0.4 秒消失），
// 但它是**同类分叉的第一个实证**：本轮还观察到 esc 有两种写法（表查 vs 内联对象），
// 目前行为等价（三副本都被 innerhtml_escape_test.go 逐 blob 验过覆盖五个危险字符）。
// 真正危险的是「下一个差异未必无害」—— 鉴权头、错误兜底、超时值任何一处单边改动
// 都会让三份行为静默不一致，而没有测试会失败。这与 round 108 立
// TestThreeApiImplsAreIdentical 时是同一个理由，故复用同一判据形态。
//
// 判据：抽出三份 toast 函数体，去掉空白后必须逐字节相同。
// 本测试不判「2600 还是 3000 对」（那是设计问题），只判「不许分叉」—— 可证伪、零误报。
func TestToastImplsAreIdentical(t *testing.T) {
	blobs := map[string]string{"appJS": appJS, "govJS": govJS, "poolJS": poolJS}
	var names []string
	var bodies []string
	for _, name := range []string{"appJS", "govJS", "poolJS"} {
		body, ok := extractToast(name, blobs[name])
		if !ok {
			// 结构变了必须显式同步本测试：否则判据退化成「什么都没比」而恒绿。
			t.Fatalf("%s 里找不到 toast 定义 —— 判据失去受理面（空转），请同步本测试", name)
		}
		names = append(names, name)
		bodies = append(bodies, body)
	}
	ref := squash(bodies[0])
	for i := 1; i < len(bodies); i++ {
		got := squash(bodies[i])
		if got == ref {
			continue
		}
		// 定位第一处差异并打印两侧片段：光说「不同」无法定位，等于没报。
		at := 0
		for at < len(ref) && at < len(got) && ref[at] == got[at] {
			at++
		}
		lo := at - 28
		if lo < 0 {
			lo = 0
		}
		t.Errorf("%s 与 %s 的 toast 实现分叉（首个差异在第 %d 个非空白字符）：\n  %s 侧: ...%q...\n  %s 侧: ...%q...\n  要么统一实现，要么在模板/文档里写明为什么必须不同",
			names[i], names[0], at, names[0], sliceAt(ref, lo, 74), names[i], sliceAt(got, lo, 74))
	}
}

// extractToast 从 blob 里取 toast 定义：定位到声明后做花括号配对，返回完整函数体。
func extractToast(name, s string) (string, bool) {
	const marker = "const toast = "
	i := strings.Index(s, marker)
	if i < 0 {
		return "", false
	}
	open := strings.IndexByte(s[i:], '{')
	if open < 0 {
		return "", false
	}
	start := i + open
	depth := 0
	for j := start; j < len(s); j++ {
		switch s[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[i : j+1], true
			}
		}
	}
	return "", false
}

// squash 去掉全部空白，使判据只关心实际代码而不关心排版。
func squash(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func sliceAt(s string, from, n int) string {
	end := from + n
	if end > len(s) {
		end = len(s)
	}
	return s[from:end]
}
