package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"workbench/internal/model"
)

// TestReadPrevArtifactsRespectsBudget 钉住 T91 的核心不变量：**total ≤ limit，一个字都不许超**。
//
// 旧实现是"先 append → 累加 → 再判 break"：越限那一个文件**整份**入内、其后文件才被丢，
// 于是单文件可整份穿透预算，实测天花板 = limit(200KB) + 上游单文件闸(8MiB) ≈ 8.19 MiB/轮。
// 我 round 137 写的测试只断"文件个数 ≤2"、没有合计上界 ⇒ 给的是虚假安全感（独立复核指出）。
// 现在 limit 就是预算：每个 body 先压到剩余预算内（截断留标记），预算耗尽即停。
func TestReadPrevArtifactsRespectsBudget(t *testing.T) {
	dir := t.TempDir()
	const limit = 2000
	if err := os.WriteFile(filepath.Join(dir, "big.md"), []byte(strings.Repeat("x", limit*5)), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"s1.md", "s2.md", "s3.md"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("small-"+n), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := readPrevArtifacts(dir, limit)
	if len(got) == 0 {
		t.Fatal("一个产物都没读到：测不到点上")
	}
	total := 0
	for _, f := range got {
		total += len(f.Body)
	}
	if total > limit {
		t.Fatalf("合计 %d 超出预算 limit=%d ⇒ 预算形同虚设（T91 病灶复发）", total, limit)
	}
	for _, f := range got {
		if len(f.Body) > limit {
			t.Fatalf("%s 单文件 %d > limit=%d ⇒ 单个大文件仍可整份穿透", f.Path, len(f.Body), limit)
		}
	}
	if !strings.Contains(got[0].Body, "已截断") {
		t.Fatalf("截断后没有标记 ⇒ 模型会把残文件当完整文件改：%q", tailOf(got[0].Body, 80))
	}
	if !utf8.ValidString(got[0].Body) {
		t.Fatal("截断把 UTF-8 切成半个字（返工 prompt 会出现乱码）")
	}
	t.Logf("读回 %d 个产物、合计 %d B（limit=%d），首个被截到 %d B 且含标记", len(got), total, limit, len(got[0].Body))
}

// TestReadPrevArtifactsKeepsFailureEvidence 钉住"截断不许剪掉判据自己要的东西"：
// 上一版产出被裁是允许的，但验收失败证据（退出码/命令）必须原样保留，
// 否则返工任务失去"照着报错改"的唯一依据（T91 验收条款）。
func TestReadPrevArtifactsKeepsFailureEvidence(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("文", 5000) // 多字节，兼验 rune 边界
	if err := os.WriteFile(filepath.Join(dir, "huge.md"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := readPrevArtifacts(dir, 1000)
	rep := &model.VerifyReport{Pass: false, FailReason: "验收命令 python m.py 退出码 9（期望 0）"}
	p := repairPrompt(&model.Task{ID: "t-ev", Prompt: "原始要求"}, prev, rep, 2)
	for _, need := range []string{"### 原始任务", "原始要求", "退出码", "python m.py", "已截断"} {
		if !strings.Contains(p, need) {
			t.Fatalf("返工 prompt 丢了 %q —— 截断把判据要用的证据剪掉了", need)
		}
	}
	if !utf8.ValidString(p) {
		t.Fatal("返工 prompt 含非法 UTF-8（rune 边界处理有洞）")
	}
}

// TestTruncateWithMarkerNeverExceedsBudget 是纯函数兜底：极端预算也不许超。
func TestTruncateWithMarkerNeverExceedsBudget(t *testing.T) {
	body := strings.Repeat("a", 10000)
	for _, budget := range []int{0, 1, 10, 100, 999, 1000} {
		got := truncateWithMarker(body, budget, "x.md")
		if budget <= 0 {
			if got != "" {
				t.Fatalf("budget=%d 应返回空串，实得 %d B", budget, len(got))
			}
			continue
		}
		if len(got) > budget {
			t.Fatalf("budget=%d 实得 %d B ⇒ 标记把预算撑爆了", budget, len(got))
		}
	}
	if got := truncateWithMarker("short", 100, "s.md"); got != "short" {
		t.Fatalf("未超预算时不该改动内容，实得 %q", got)
	}
}

func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
