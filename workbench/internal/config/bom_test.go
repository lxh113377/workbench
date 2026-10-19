package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadJSONToleratesBOM 覆盖真实故障：
// Windows 记事本 / PowerShell Set-Content -Encoding UTF8 写入的 UTF-8 BOM，
// 会让 Go 的 json 解析器直接报 invalid character '\ufeff'，进而导致
// 工作台启动即退出（现场实测：手改一次 config.json 就打不开）。
func TestReadJSONToleratesBOM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	body := []byte("{\"a\":1}")
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, body...)
	if err := os.WriteFile(path, withBOM, 0o644); err != nil {
		t.Fatal(err)
	}
	var got struct {
		A int `json:"a"`
	}
	if err := ReadJSON(path, &got); err != nil {
		t.Fatalf("含 BOM 的 JSON 必须能读: %v", err)
	}
	if got.A != 1 {
		t.Fatalf("a = %d, want 1", got.A)
	}
}

// TestReadJSONUTF16BOM 保证 UTF-16 BOM 也被剥掉（内容仍会因编码不同而失败，
// 但错误必须是"解析失败"而不是"文件不存在"这类误导信息）。
func TestReadJSONUTF16BOM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg16.json")
	// UTF-16 LE BOM + 非 JSON 内容
	if err := os.WriteFile(path, []byte{0xFF, 0xFE, 0x7B, 0x00, 0x7D, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := ReadJSON(path, &v); err == nil {
		t.Fatal("UTF-16 内容应报解析失败（但不应是 BOM 字符错误）")
	}
}
