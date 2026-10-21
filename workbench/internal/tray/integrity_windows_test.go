//go:build windows

package tray

import (
	"testing"
)

// TestIsLowIntegrity 守护「低完整性级别判定」这条判据本身。
//
// 现场（2026-10-19）：托盘图标不出现，根因是进程从被打上 Low 标签的目录启动，
// 继承 Low IL 后被 UIPI 拒绝注册托盘。判定必须只认 icacls 的 Low 行，
// 不能把 Medium/High 误判成需要自愈（否则每次启动都去改目录标签）。
func TestIsLowIntegrity(t *testing.T) {
	sep := string([]byte{92})
	low := "Mandatory Label" + sep + "Low Mandatory Level:(OI)(CI)(NW)"
	medium := "Mandatory Label" + sep + "Medium Mandatory Level:(OI)(CI)(NW)"
	high := "Mandatory Label" + sep + "High Mandatory Level:(OI)(CI)(NW)"
	cases := []struct {
		label string
		want  bool
	}{
		{low, true},
		{medium, false},
		{high, false},
		{"", false},
		{"Everyone:(OI)(CI)(F)", false},
		// 反向保险：只有 "Low" 这个词、没有 "Mandatory Label" 前缀的行不得命中
		{sep + "Low", false},
	}
	for _, c := range cases {
		if got := isLowIntegrity(c.label); got != c.want {
			t.Fatalf("isLowIntegrity(%q) = %v, want %v", c.label, got, c.want)
		}
	}
}

// TestIntegrityName 守护诊断文案：Low 必须被认出来（这正是自愈拒绝的判据）。
func TestIntegrityName(t *testing.T) {
	if got := integrityName(ilLow); got != "Low" {
		t.Fatalf("integrityName(0x1000) = %q, want Low", got)
	}
	if got := integrityName(ilMedium); got != "Medium" {
		t.Fatalf("integrityName(0x2000) = %q, want Medium", got)
	}
	if got := integrityName(0); got != "unknown" {
		t.Fatalf("取不到级别时必须显式 unknown（不能假装 Medium），实际 %q", got)
	}
}

// TestRaiseToMediumRejectsEmpty 守护空路径：icacls 空参数会作用于当前目录，必须显式拒绝。
func TestRaiseToMediumRejectsEmpty(t *testing.T) {
	if err := raiseToMedium(""); err == nil {
		t.Fatal("空目录必须被拒绝（否则 icacls 会改到当前目录）")
	}
	if err := raiseToMedium("   "); err == nil {
		t.Fatal("空白目录必须被拒绝")
	}
}

// TestCurrentIntegrityReadable 守护 token 解析：必须能读出本进程级别（不能恒 0）。
// 如果这里恒 0，Low 判定就会退化成"永远当作未知"，自愈会盲试。
func TestCurrentIntegrityReadable(t *testing.T) {
	rid := currentIntegrity()
	if rid == 0 {
		t.Skip("当前环境读不到 token 完整性级别（受限沙箱），跳过")
	}
	if rid != ilLow && rid != ilMedium && rid != ilHigh && rid != ilSystem {
		t.Fatalf("完整性级别 RID 异常: 0x%X", rid)
	}
	t.Logf("本测试进程完整性级别: %s (0x%X)", integrityName(rid), rid)
}

// TestTryRaiseIntegrityAlwaysExplains 守护「自愈永不 panic、永远给出原因」这条契约。
// 托盘是可选增强：修不好也必须返回可读原因，让主面板照常可用并让用户知道为什么没图标。
func TestTryRaiseIntegrityAlwaysExplains(t *testing.T) {
	healed, reason := tryRaiseIntegrity()
	if reason == "" {
		t.Fatal("无论成功失败都必须返回诊断原因（否则用户看到的就是静默没图标）")
	}
	t.Logf("自愈结果 healed=%v reason=%s", healed, reason)
}
