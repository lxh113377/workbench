package verify

import (
	"strings"
	"testing"

	"workbench/internal/artifact"
)

// TestProtocolExampleHasNoDecorativeBrackets 复现 docs/质量铁证-协议示例中文方括号-2026-10-19.md。
//
// 根因不在 splitCommand，而在**协议示例本身**：示例命令被中文方括号【】包裹，
// 模型照抄后命令被空格切碎（【if 成为一个 token），pwsh 拿它当命令名必然失败。
// 修复（2026-10-23）：protocol.go 的示例改为不带【】、明示引号必须保留。
//
// 原测试期望 splitCommand 把【】当引号处理（错误假设：中文方括号不是引号，
// 没有理由不被空格分割）。故本测试改为守**真正的根因**：协议文本里不得再出现
// 包裹命令的装饰性方括号。
func TestProtocolExampleHasNoDecorativeBrackets(t *testing.T) {
	text := artifact.ProtocolInstruction
	for _, line := range strings.Split(text, "\n") {
		// 只查**命令行本身**：以 -Command 结尾前缀的行才是示例命令；
		// 说明文字里提到 pwsh（如「解释器限 python/node/pwsh/go」）不是命令，不该被本判据约束。
		if !strings.Contains(line, "-Command") {
			continue
		}
		if strings.ContainsAny(line, "【】") {
			t.Errorf("协议示例命令行仍含装饰性中文方括号（模型照抄后必被切碎）: %q", line)
		}
		if !strings.Contains(line, "\"") {
			t.Errorf("协议示例命令行缺少引号（引号必须保留，否则多词脚本被切碎）: %q", line)
		}
	}
}
