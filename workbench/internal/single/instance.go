package single

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// Instance 是跨会话单实例锁。
// 实现：用「独占文件句柄」做锁 —— Windows 上被独占打开的文件无法被第二进程独占，
// 且文件锁在任何会话（含 WMI 启动的 session 0 进程）都有效，不像命名互斥体那样受会话命名空间影响。
type Instance struct {
	f    *os.File
	path string
}

// Acquire 获取单实例锁。alreadyRunning=true 表示已有实例持有该锁。
func Acquire(lockPath string) (inst *Instance, alreadyRunning bool, err error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, false, err
	}
	f, openErr := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if openErr != nil {
		return nil, false, fmt.Errorf("打开实例锁失败: %w", openErr)
	}
	// Windows 共享模式：0 = 禁止其他进程读写打开 → 独占
	if lockErr := lockFileExclusive(f); lockErr != nil {
		_ = f.Close()
		// 独占失败通常意味着另一实例持锁
		return nil, true, nil
	}
	payload, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "at": time.Now().UnixMilli()})
	_ = f.Truncate(0)
	_, _ = f.WriteAt(payload, 0)
	_ = f.Sync()
	return &Instance{f: f, path: lockPath}, false, nil
}

// Release 释放锁并清理锁文件内容。
func (i *Instance) Release() {
	if i == nil || i.f == nil {
		return
	}
	_ = i.f.Truncate(0)
	unlockFile(i.f)
	_ = i.f.Close()
	i.f = nil
}

// ProbeRunning 探测某个端口上是否已有工作台在监听。
func ProbeRunning(host string, basePort, attempts int) (int, bool) {
	if host == "" {
		host = "127.0.0.1"
	}
	for i := 0; i < attempts; i++ {
		p := basePort + i
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(p)), 300*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return p, true
		}
	}
	return 0, false
}

var _ = syscall.Getpid
