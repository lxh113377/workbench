//go:build windows

// 只判 Windows：本测试引用 labelError（定义在 integrity_windows.go），
// 缺 tag 时 GOOS=linux/darwin 下 go vet 与 go test 编译不过（round 222 CI 交叉编译实测）。

package tray

// integrity_error_contract_test.go -- 完整性标签错误的可读性与可溯源判据（2026-10-22 补）。
//
// 为什么必须有：labelError 是本包**唯一**的 0% 覆盖率类型，而它承载的是
// 「托盘图标注册失败时，用户/日志能不能看到为什么」这条唯一的诊断通道。
// 现场形态（2026-10-20 沙箱残留事故）：安装目录被打上 Low 完整性标签 -> Explorer 拒收
// NIM_ADD -> 托盘图标不出现，且**没有任何报错**。若 labelError 的 Error()/Unwrap() 破，
// 结果就是「静默没图标」——本仓在册的最难排查形态之一。
//
// 判据面：Error() 必须带目录与 icacls 输出（否则拿到 err 也不知道是哪个目录、为什么）；
// Unwrap() 必须可溯源到原始 exec 错误（errors.Is/As 才能穿透判类型）。

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// TestLabelErrorErrorIncludesDirAndIcaclsOutput 钉死诊断信息完备性。
// 缺其中任一项，现场就只能猜：知道目录才知道「哪个安装根被打标」，
// 知道 icacls 输出才知道「是权限拒绝还是路径不存在」。
func TestLabelErrorErrorIncludesDirAndIcaclsOutput(t *testing.T) {
	le := &labelError{dir: `D:\workdesktop`, out: "拒绝访问。", err: os.ErrPermission}
	msg := le.Error()
	if !strings.Contains(msg, `D:\workdesktop`) {
		t.Fatalf("Error() 必须带目录（否则不知道哪个安装根被打标）：%q", msg)
	}
	if !strings.Contains(msg, "拒绝访问") {
		t.Fatalf("Error() 必须带 icacls 原始输出（否则不知道失败原因）：%q", msg)
	}
	if !strings.Contains(msg, "icacls") {
		t.Fatalf("Error() 应说明是 icacls 失败（便于检索与对账）：%q", msg)
	}
}

// TestLabelErrorWithoutOutputStaysReadable 钉死空输出分支：
// exec 未产出任何 stderr 时（例如程序根本起不来），Error() 不得出现空括号 "()"。
// 少了这条，日志里会留下 "... ()" 这种让人以为「输出被吞了」的假象。
func TestLabelErrorWithoutOutputStaysReadable(t *testing.T) {
	le := &labelError{dir: "C:\\x", out: "", err: os.ErrNotExist}
	msg := le.Error()
	if strings.Contains(msg, "()") {
		t.Fatalf("无 icacls 输出时不得渲染空括号（会被误读为输出被吞）：%q", msg)
	}
	if !strings.Contains(msg, "C:\\x") {
		t.Fatalf("仍须带目录：%q", msg)
	}
}

// TestLabelErrorUnwrapPreservesCause 钉死 errors.Is/As 可穿透。
// 调用方要按错误类型分流（权限拒绝 vs 路径不存在 vs 命令缺失），
// 不能只能靠字符串匹配 —— 那正是本仓反复批评的脆弱判据形态。
func TestLabelErrorUnwrapPreservesCause(t *testing.T) {
	le := &labelError{dir: "x", out: "o", err: os.ErrPermission}
	if !errors.Is(le, os.ErrPermission) {
		t.Fatal("errors.Is 必须能穿透到原始错误（Unwrap 契约）")
	}
	var pathErr *os.PathError
	if errors.As(le, &pathErr) {
		t.Log("本例包装的不是 PathError，跳过 As 正向断言（属预期）")
	}
}

// TestRaiseToMediumRejectsEmptyDir 钉死空目录短路：
// 空路径直接返回 os.ErrInvalid，**不得**起 icacls 子进程。
// 为什么重要：exeDir() 取不到时会得到空串，若照样 exec，会变成
// `icacls "" /setintegritylevel ...` —— 一个语义含糊的外部调用，
// 既浪费一次进程创建，又可能被安全软件盯上（无意义的 icacls 调用）。
func TestRaiseToMediumRejectsEmptyDir(t *testing.T) {
	for _, dir := range []string{"", "   ", "\t\n"} {
		err := raiseToMedium(dir)
		if !errors.Is(err, os.ErrInvalid) {
			t.Fatalf("空目录 %q 应直接返回 os.ErrInvalid，got %v", dir, err)
		}
	}
}

// TestIntegrityNameCoversAllKnownLevels 钉死级别名映射完整。
// 这些名字会进日志与面板提示，映射错了会导致「明明 High 却显示 Other」，
// 把权限类故障引向错误方向。
func TestIntegrityNameCoversAllKnownLevels(t *testing.T) {
	cases := []struct {
		rid  uint32
		want string
	}{
		{ilLow, "Low"},
		{ilMedium, "Medium"},
		{ilHigh, "High"},
		{ilSystem, "System"},
		{0, "unknown"},
	}
	for _, c := range cases {
		if got := integrityName(c.rid); got != c.want {
			t.Fatalf("integrityName(%d) = %q, want %q", c.rid, got, c.want)
		}
	}
	// 未知 RID 必须落在 Other（而不是冒充某个已知级别）。
	if got := integrityName(ilSystem + 999); got != "Other" {
		t.Fatalf("未知 RID 应返回 Other，got %q（冒充已知级别会把排查引向错误方向）", got)
	}
}

// TestIsLowIntegrityOnlyMatchesLow 钉死低标签判定的精确性。
// 这条判据直接决定「要不要尝试自愈」：误判 Low 会去改一个本不需要改的 SACL
// （无谓的特权操作），漏判则自愈永不触发、图标永久不出现。
func TestIsLowIntegrityOnlyMatchesLow(t *testing.T) {
	low := "Mandatory Label\\Low Mandatory Level:(OI)(CI)(NW)"
	if !isLowIntegrity(low) {
		t.Fatalf("必须识别 Low 标签: %q", low)
	}
	for _, other := range []string{
		"",
		"Mandatory Label\\Medium Mandatory Level:(OI)(CI)(NW)",
		"Mandatory Label\\High Mandatory Level:(OI)(CI)(NW)",
		"NT AUTHORITY\\SYSTEM:(OI)(CI)(F)",
	} {
		if isLowIntegrity(other) {
			t.Fatalf("非 Low 标签不得被判为 Low（会触发无谓的特权 SACL 修改）: %q", other)
		}
	}
}
