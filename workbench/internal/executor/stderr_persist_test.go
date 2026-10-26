package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPersistStderrTail 钉住「失败原因必须落盘」（2026-10-26 round 9 · T10）。
//
// 判据：非空才写；写进 events.jsonl 同目录的 stderr.txt；超限只留**尾部**（失败原因通常在最末几行）；
// 空 stderr / 空路径不写文件且返回空串（观测面失败不得改变执行判定）。
func TestPersistStderrTail(t *testing.T) {
	dir := t.TempDir()
	events := filepath.Join(dir, "events.jsonl")
	stderrPath := filepath.Join(dir, "stderr.txt")

	if p := persistStderrTail(events, "   "); p != "" {
		t.Fatalf("空 stderr 不该落盘，实得 %q", p)
	}
	if _, err := os.Stat(stderrPath); !os.IsNotExist(err) {
		t.Fatal("空 stderr 却创建了 stderr.txt")
	}
	if p := persistStderrTail("", "boom"); p != "" {
		t.Fatalf("空 eventsPath 不该落盘，实得 %q", p)
	}

	msg := "ERROR first line" + "\n" + "READY" + "\n" + strings.Repeat("x", stderrTailLimit+100) + "\n" + "TAIL-MARKER" + "\n"
	p := persistStderrTail(events, msg)
	if p == "" {
		t.Fatal("非空 stderr 应落盘")
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读 stderr.txt: %v", err)
	}
	got := string(raw)
	if len(got) > stderrTailLimit+8 {
		t.Fatalf("落盘超过上限：%d 字节", len(got))
	}
	if !strings.Contains(got, "TAIL-MARKER") {
		t.Fatal("超限时应保留**尾部**（失败原因通常在最后），实测尾部标记丢失")
	}
	if strings.Contains(got, "first line") {
		t.Fatal("超限时不该保留头部（说明截取方向反了）")
	}
}
