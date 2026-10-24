package web

// docx_atomic_test.go 钉住 DOCX 导出的落盘契约（2026-10-25 硬约束 #5 五轴审查引出）。
//
// 为什么必须有测试（探针实测，非推测）：改造前 writeDocx 自制原子写 ——
// Create(path+".tmp") 写、Close、Sync、Close，再 os.Remove(path)、os.Rename(tmp, path)。
// 一次性探针复现两个 goroutine 同时导出同一 path（HTTP handler 允许并发）：
//
//	RESULTS=[B-ERR:rename ...export.docx.tmp ...export.docx: The process cannot access
//	 the file because it is being used by another process. A-OK]
//
// 即固定后缀 .tmp 让并发导出必有一方失败（用户看到「生成 DOCX 失败」）。
// 另一形态是「先删目标再 rename」：中间失败就丢掉上一版导出，与 config/atomic.go
// 本轮修的「数据归零」同形态。现改为 zip 写内存 buffer + config.WriteFileAtomic 落盘。
//
// 本文件测行为不测源码文本：① 并发导出同一目标全部成功（旧实现必红）；
// ② 产物是合法 zip 且含 OOXML 必备部件；③ 不留 .tmp 残渣。
// 失败窗口（rename 失败时旧内容仍在）由 config 包的 seam 判据负责，不在此重复构造 ——
// 那个窗口在本机四种方案都构造不稳（见 config/atomic.go 注释），重复尝试只会造出 SKIP 假绿。

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"workbench/internal/model"
)

func docFixture(title, content string) []exportDoc {
	return []exportDoc{{
		Task:    &model.Task{ID: "t1", Title: title, Domain: "content", Model: "m1"},
		Content: content,
	}}
}

func TestWriteDocxConcurrentExportsAllSucceed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export.docx")
	// 预置一个已存在的同名文件：旧实现会先 os.Remove 它再 rename（有丢失窗口），
	// 新实现交给 WriteFileAtomic —— 同名目标要么旧要么新，不能半途消失。
	if err := os.WriteFile(path, []byte("PRE-EXISTING"), 0o644); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = writeDocx(path, docFixture("并发导出", "## 段落\n\n- 项目一\n- 项目二"))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("第 %d 个并发导出失败（固定后缀 tmp 互相抢占的旧缺陷形态）: %v", i, err)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("四个导出都报成功但目标文件不在: %v", err)
	}
	residue, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(residue) > 0 {
		t.Errorf("留下临时文件残渣: %v", residue)
	}
}

func TestWriteDocxProducesValidOOXMLPackage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ok.docx")
	if err := writeDocx(path, docFixture("标题校验", "# 一级\n正文内容\n\n* 列表项")); err != nil {
		t.Fatalf("writeDocx 失败: %v", err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("产物不是合法 zip（Word 打不开）: %v", err)
	}
	defer zr.Close()
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/styles.xml"} {
		if !names[want] {
			t.Errorf("缺 OOXML 部件 %q（Word 会报文件损坏）", want)
		}
	}
	// document.xml 里应能看到转义后的正文，确认内容真写进去了
	df, err := zr.Open("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer df.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := df.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(sb.String(), "正文内容") {
		t.Error("document.xml 未含正文 —— 导出内容为空的静默缺陷形态")
	}
}
