package web

// failure_label_contract_test.go —— 面板中文标签必须覆盖全部预置失败类别（round 66 · 改造一）。
//
// 为什么要钉：面板渲染走 `kindText[b.kind] || b.kind` —— 漏配的类别**不报错**，只裸显英文原值。
// 本轮之前手抄的标签表只有 10 项，而预置基线（改由 retry.AllKinds() 派生后）有 17 项 ⇒
// 七个类别在中文面板里是半中半英的桶名。这类「静默降级显示」靠人记不住，只能机器对账。
//
// 口径（㉑二 在册教训）：解析面结果为零一律判红，绝不把「什么都没解析到」当成「全部对齐」。

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"workbench/internal/retry"
)

// kindKeyRe 匹配 JS 对象字面量里的一个键值对起点：标识符 + 冒号 + 单引号（键不带引号、值带单引号）。
//
// 为什么用正则而不是按行取冒号前：这张表是**折行写**的，一行可含多个键值对；
// 按行只认得到每行第一个键，其余会被误判成「没有中文条目」—— 本判据第一版就是这么错的
// （纪律 D10.2：判据比被测面更红，先怀疑判据本身）。
// 引号写成八进制转义：避免在源码里嵌引号（本仓在册的引号坑）。
var kindKeyRe = regexp.MustCompile("([A-Za-z_][A-Za-z0-9_]*)\\s*:\\s*\047")

// parseKindTextKeys 从 appJS 源文本里解析 kindText 表的键集合。
func parseKindTextKeys(t *testing.T, body string) map[string]bool {
	t.Helper()
	start := strings.Index(body, "const kindText = {")
	if start < 0 {
		t.Fatal("找不到 kindText 标签表（被改名 ⇒ 本判据失去对照面，判红而非跳过）")
	}
	rest := body[start:]
	end := strings.Index(rest, "};")
	if end < 0 {
		t.Fatal("kindText 表没有收尾（解析面不完整，拒绝下结论）")
	}
	have := map[string]bool{}
	for _, m := range kindKeyRe.FindAllStringSubmatch(rest[:end], -1) {
		have[m[1]] = true
	}
	if len(have) == 0 {
		t.Fatal("标签表解析结果为零 —— 判据不许把空集合当成全部对齐")
	}
	return have
}

func TestFailureLabelsCoverBaselineKinds(t *testing.T) {
	src, err := os.ReadFile("assets_script.go")
	if err != nil {
		t.Fatalf("读 assets_script.go 失败: %v（对照面丢失判红，不 Skip）", err)
	}
	have := parseKindTextKeys(t, string(src))
	// 方向一：每个预置类别都要有中文标签（缺 ⇒ 面板裸显英文桶名）。
	for _, k := range promFailureKindsBaseline {
		if !have[k] {
			t.Errorf("类别 %q 在面板标签表里没有中文条目 ⇒ 失败分布裸显英文；补进 assets_script.go 的 kindText", k)
		}
	}
	// 方向二：标签表里不得留「枚举中已不存在」的死标签（done 是面板给成功态的例外，显式放行）。
	for label := range have {
		if label == "done" {
			continue
		}
		found := false
		for _, k := range retry.AllKinds() {
			if string(k) == label {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("面板标签表里的 %q 已不在失败枚举里（死标签会让人以为还有这一类失败）", label)
		}
	}
}

// TestFailureLabelGateHasTeeth 是判据自证（负控）：喂一张「折行且含两个键」的假表，
// 必须一次认全两个键；漏认说明解析器回到按行取键的旧错。不碰磁盘 ⇒ 红了只可能是判据坏了。
func TestFailureLabelGateHasTeeth(t *testing.T) {
	fake := `const kindText = {
      timeout: '超时', stalled: '卡死',
};`
	have := parseKindTextKeys(t, fake)
	if len(have) != 2 || !have["timeout"] || !have["stalled"] {
		t.Fatalf("解析器不认折行表形态（判据已失去牙）: %+v", have)
	}
	if have["pool_wait_capped"] {
		t.Fatal("负控失效：假表里本不该有 pool_wait_capped")
	}
}
