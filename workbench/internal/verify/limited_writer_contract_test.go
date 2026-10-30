package verify

import (
	"bytes"
	"io"
	"testing"
)

// TestLimitedWriterKeepsIOWriterContract 复现并钉住一条真实误分类（round 143，线上证据驱动）。
//
// 线上现场：验收命令 `pwsh -NoProfile -File probe.ps1` 的 outcome 是
//
//	exit=-1 ｜ skipped="启动失败: short write" ｜ ms=7453 ｜ stdout 是海量文件列表（超 64KB 上限）
//
// 机制（读 stdlib 即得，非推测）：os/exec 用 io.Copy 把子进程输出搬到我们给的 Writer；
//
//	io.Copy 内部有 `if nr != nw { err = io.ErrShortWrite }` —— 一旦 Write 返回**短计数且 err=nil**，
//	就判 ErrShortWrite。而旧 limitedWriter 在超限时正是"截断后返回 remain、err 返回 nil"，
//	于是 cmd.Run() 拿到 short write ⇒ verify 把它记成「启动失败」（exit=-1、检查判失败）。
//
// 后果两条：① 诊断误导（看起来像解释器起不来，实际是输出洪泛）；
//
//	② 一条本该 exit 0 的命令会因为刷了 >64KB 输出而被判失败 ⇒ 验收假阴性。
func TestLimitedWriterKeepsIOWriterContract(t *testing.T) {
	buf := &bytes.Buffer{}
	const max = 8
	lw := &limitedWriter{w: buf, max: max}
	payload := bytes.Repeat([]byte("y"), 4096)

	// ① io.Writer 契约：Write 必须报"我收下了全部 len(p)"（或返回错误），不许短计数配 nil error。
	n, err := lw.Write(payload)
	if err != nil {
		t.Fatalf("Write 不该返回错误：%v", err)
	}
	if n != len(payload) {
		t.Fatalf("Write 返回 n=%d，len(p)=%d ⇒ 违反 io.Writer 契约：超限时的短计数会让 io.Copy 报 ErrShortWrite，再经 cmd.Run() 变成 verify 的「启动失败」（线上实测 exit=-1 / skipped 含 启动失败: short write）", n, len(payload))
	}
	// ② 缓冲仍必须被截到上限（防刷爆内存的本意不能丢）
	if buf.Len() != max {
		t.Fatalf("缓冲应被截到 max=%d，实得 %d", max, buf.Len())
	}

	// ③ 直接走 os/exec 用的那条通路：io.Copy 不许报错（这一条最贴近线上形态）
	lw2 := &limitedWriter{w: &bytes.Buffer{}, max: 16}
	if _, err := io.Copy(lw2, bytes.NewReader(bytes.Repeat([]byte("z"), 8192))); err != nil {
		t.Fatalf("io.Copy 返回 %v ⇒ 复现「short write」路径（os/exec 正是用 io.Copy 搬子进程输出）", err)
	}
}
