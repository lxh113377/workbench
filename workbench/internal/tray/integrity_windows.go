//go:build windows

package tray

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// integrity_windows.go — 托盘图标的「完整性级别」诊断与自愈。
//
// 现场根因（2026-10-19 老大："状态栏也没有图标"）：
//
//	Shell_NotifyIconW(NIM_ADD) 返回 0，GetLastError = "Access is denied"。
//	排除法实测（同一二进制换目录跑，唯一变量 = 所在目录）：
//	  C:\Users\...\Temp、D:\、D:\app-cache\...  -> NIM_ADD ret=1（图标出现）
//	  D:\workdesktop\**（整棵子树）                -> NIM_ADD ret=0 Access is denied
//	icacls 给出解释：D:\workdesktop 带
//	  "Mandatory Label\Low Mandatory Level:(OI)(CI)(NW)"
//	可执行文件带 Low 标签时，**即使由 Medium 的 Explorer 启动**，新进程也会被降为
//	Low 完整性级别（Low IL）；Low IL 进程不允许向 Medium IL 的 Explorer 托盘注册
//	图标（UIPI 强制），于是图标永远不出现。这条标签来自文件系统沙箱的驻留改动。
//
// 两条硬事实（实测，决定修复方式）：
//  1. Low IL 进程**不能**给自己目录提标签：icacls 返回 1299
//     "Indicates a particular Security ID may not be assigned as the label of an object"。
//  2. Medium IL 进程可以（目录属主有 WRITE_DAC），提完立即生效，托盘图标马上出现。
//
// ⇒ 自愈分两层：
//   - 进程内：仅当自身已是 Medium 及以上才提标签；Low 时直接放弃并把原因上报，
//     不做无意义的 icacls 调用（避免每次启动刷失败日志）。
//   - 进程外：Medium IL 的守护进程/装机脚本负责（tests/keepalive.ps1、setup/build.ps1）。

var (
	advapi32                    = syscall.NewLazyDLL("advapi32.dll")
	procOpenProcessToken        = advapi32.NewProc("OpenProcessToken")
	procGetTokenInformation     = advapi32.NewProc("GetTokenInformation")
	kernel32b                   = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentProcessHandle = kernel32b.NewProc("GetCurrentProcess")
)

// 完整性级别 RID（token 的 TokenIntegrityLevel → SID 最后一段）。
const (
	ilLow     = 0x1000
	ilMedium  = 0x2000
	ilHigh    = 0x3000
	ilSystem  = 0x4000
	// tokenIL 是 TOKEN_INFORMATION_CLASS.TokenIntegrityLevel = **25**。
	//
	// 踩坑实测（2026-10-19）：18 是 TokenElevationType，用它会"成功"返回 4 字节
	// 0x03（即 TokenElevationTypeDefault），被误解析成"读不到级别"，
	// 于是 Low 判定永远走不到、自愈形同虚设。25 才返回 TOKEN_MANDATORY_LABEL。
	tokenIL   = 25
	tokenRead = 0x0008
)

// currentIntegrity 返回本进程的完整性级别 RID（取不到返回 0）。
//
// 解析方式：GetTokenInformation 回填 TOKEN_MANDATORY_LABEL { SID_AND_ATTRIBUTES }，
// 其 SID 紧跟在结构体之后（同一块缓冲）。直接按偏移解析，避免 uintptr→unsafe.Pointer
// 的来回转换（go vet 会判 "possible misuse of unsafe.Pointer"）。
func currentIntegrity() uint32 {
	h, _, _ := procGetCurrentProcessHandle.Call()
	var tok uintptr
	if r, _, _ := procOpenProcessToken.Call(h, tokenRead, uintptr(unsafe.Pointer(&tok))); r == 0 {
		return 0
	}
	// x64：TOKEN_MANDATORY_LABEL = { PSID (8) + DWORD Attributes (4) + 4 对齐 } = 16 字节。
	const labelSize = 16
	buf := make([]byte, 256)
	var ret uint32
	if r, _, _ := procGetTokenInformation.Call(tok, tokenIL, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&ret))); r == 0 {
		return 0
	}
	if int(ret) < labelSize+8 || int(ret) > len(buf) {
		return 0
	}
	sid := buf[labelSize:ret]
	cnt := int(sid[1])
	if cnt <= 0 {
		return 0
	}
	last := 8 + (cnt-1)*4
	if last+4 > len(sid) {
		return 0
	}
	return binary.LittleEndian.Uint32(sid[last : last+4])
}

// integrityName 把 RID 变成可读名字（日志与面板提示用）。
func integrityName(rid uint32) string {
	switch rid {
	case ilLow:
		return "Low"
	case ilMedium:
		return "Medium"
	case ilHigh:
		return "High"
	case ilSystem:
		return "System"
	case 0:
		return "unknown"
	}
	return "Other"
}

// exeDir 返回当前可执行文件所在目录（自愈作用域 = 实例自己的安装根，不越界）。
func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// integrityLabel 返回路径上的完整性标签行（icacls 输出里的 "Mandatory Label\..." 段）。
func integrityLabel(path string) string {
	cmd := exec.Command("icacls", path)
	cmd.SysProcAttr = hiddenProcAttr()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "Mandatory Label") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// isLowIntegrity 判断标签是否为 Low。
func isLowIntegrity(label string) bool {
	return strings.Contains(label, "Low Mandatory Level")
}

// raiseToMedium 把目录（含继承到子项）的完整性标签提升到 Medium。
//
// 只改 SACL 标签：不动 DACL、不动文件内容、不动属主；(OI)(CI) 让子项继承生效。
// 需要调用进程自身已是 Medium 及以上（Low 进程会被拒，实测错误码 1299）。
func raiseToMedium(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return os.ErrInvalid
	}
	cmd := exec.Command("icacls", dir, "/setintegritylevel", "(OI)(CI)M")
	cmd.SysProcAttr = hiddenProcAttr()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &labelError{dir: dir, out: strings.TrimSpace(string(out)), err: err}
	}
	return nil
}

// tryRaiseIntegrity 尝试把「本 exe 所在目录」的 Low 标签提成 Medium。
//
// 返回值用于**上报**而不是控制流程：
//   - healed: 标签确实被改过（调用方可以重试一次 NIM_ADD）
//   - reason: 一句话诊断，供日志/面板展示（修不了时说明为什么，避免"静默没图标"）
func tryRaiseIntegrity() (healed bool, reason string) {
	dir := exeDir()
	if dir == "" {
		return false, "取不到可执行文件目录"
	}
	if !isLowIntegrity(integrityLabel(dir)) {
		// 不是低标签导致的失败（例如 Explorer 没起来 / 会话未就绪），重试没有意义。
		return false, "安装目录无 Low 标签，托盘注册失败另有原因（可能 Explorer 未就绪）"
	}
	self := currentIntegrity()
	if self != 0 && self < ilMedium {
		return false, "安装目录被打上 Low 完整性标签，本进程自身也是 " + integrityName(self) +
			"，无法自愈（Low 进程不能改 SACL）；需由 Medium 权限的守护进程修复"
	}
	if err := raiseToMedium(dir); err != nil {
		return false, "安装目录被打上 Low 完整性标签，提升失败: " + err.Error()
	}
	return true, "安装目录原为 Low 完整性标签（沙箱残留），已提升为 Medium"
}

// labelError 把 icacls 失败包装成可读错误（保留原始输出便于排查）。
type labelError struct {
	dir string
	out string
	err error
}

func (e *labelError) Error() string {
	msg := "icacls 提升完整性标签失败: " + e.dir
	if e.out != "" {
		msg += " (" + e.out + ")"
	}
	return msg
}

func (e *labelError) Unwrap() error { return e.err }
