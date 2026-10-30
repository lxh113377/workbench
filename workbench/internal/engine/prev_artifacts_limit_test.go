package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadPrevArtifactsRespectsLimit 是对"根因 B"的取证式测试（round 136）。
// 我只从代码读出「limit 是累计口径」，按本会话纪律不能拿代码阅读当结论 —— 这条测试
// 把两种可能行为钉成一种：越过累计阈值后是否还继续吸收后续文件。
// 本轮只确证行为，不改行为；确证完才谈得上要不要加单文件上限。
func TestReadPrevArtifactsRespectsLimit(t *testing.T) {
	dir := t.TempDir()
	const limit = 2000
	// 一个远超 limit 的大文件 + 三个小文件（big.md 字典序在前，先被读到）
	huge := strings.Repeat("x", limit*5)
	if err := os.WriteFile(filepath.Join(dir, "big.md"), []byte(huge), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"s1.md", "s2.md", "s3.md"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("small-content-"+n), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := readPrevArtifacts(dir, limit)
	if len(got) == 0 {
		t.Fatal("一个产物都没读到：测不到点上（先修测试，别改实现）")
	}
	total := 0
	for _, f := range got {
		total += len(f.Body)
	}
	t.Logf("读回 %d 个产物，合计 %d 字节（limit=%d）", len(got), total, limit)
	// 关键：越过 limit 后不得继续把后续文件全吸进来（否则返工 prompt 体积随工作区文件数增长）
	if len(got) > 2 {
		t.Fatalf("limit 没起到截断后续文件的作用：读到 %d 个产物，合计 %d 字节（limit=%d）", len(got), total, limit)
	}
	// 合计上界（round 138 · 采纳独立复核指出的缺口）：原测试只断"文件个数"，**没有任何合计上界**
	// ⇒ "加了单文件上限"根本不等于"prompt 体积有上界"，光有个数断言会给出虚假安全感。
	// 这里的数学事实是：实现是"先 append → 累加 → 再判 break"（repair.go:113-116），
	// 所以越限那一个文件整份入内、其后文件被丢 ⇒ 合法上界是 limit + 最大单文件。
	// 为什么断"上界等于 limit+largest"而不是断 total<=limit：后者会判红当前实现 ——
	// 单文件可击穿 limit 是**现状**（实测 CHANGELOG.md 277,803 B 整份进 prompt），
	// 我这条判据的职责是把天花板钉成可复算事实，不是虚构一个还不存在的保证（那属 T91）。
	// 可演进性：T91 真加了单文件上限 fileCap 后，本断言应收紧为 total <= limit + fileCap。
	largest := 0
	for _, f := range got {
		if len(f.Body) > largest {
			largest = len(f.Body)
		}
	}
	if total > limit+largest {
		t.Fatalf("合计 %d 超出机制天花板 limit(%d)+最大单文件(%d)=%d ⇒ 截断语义已变，天花板要重算",
			total, limit, largest, limit+largest)
	}
	// 天花板本身要能被看见（供 T91 定阈值用，不参与判定）
	t.Logf("本轮天花板：limit=%d 最大单文件=%d ⇒ 上界=%d B/轮", limit, largest, limit+largest)
}
