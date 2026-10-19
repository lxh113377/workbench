package artifact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseRealModelOutput 用现场真实模型输出（glm-5.3 的 loose 围栏）回归：
// 这条样本是从 e2e 任务 final.md 原样拷来的，代表"真实一轮开发任务"的输入。
func TestParseRealModelOutput(t *testing.T) {
	path := filepath.Join("testdata", "real_loose_output.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("缺少现场样本: %v", err)
	}
	res := Parse(string(data), Limits{})
	if !res.HasFiles() {
		t.Fatalf("现场样本必须能解析出文件: level=%s problems=%v", res.Level, res.Problems)
	}
	if res.Level != LevelLoose {
		t.Fatalf("应走 loose 层: %s", res.Level)
	}
	if len(res.Files) != 1 || res.Files[0].Path != "hello.py" {
		t.Fatalf("files = %+v", res.Files)
	}
	if !strings.Contains(string(res.Files[0].Body), "hello from workbench") {
		t.Fatalf("body = %q", string(res.Files[0].Body))
	}
	if len(res.Manifest.Verify) == 0 {
		t.Fatalf("应捕获 verify 命令")
	}
	if !res.OK() {
		t.Fatalf("现场样本应可验收: %v", res.Problems)
	}
}
