package executor

import "os"

// osProcess 是 *os.Process 的最小抽象，便于注入测试替身。
type osProcess struct {
	pid   int
	inner *os.Process
}

// ProcessOf 提取 PID（进程已退出时返回 0）。
func ProcessOf(p *os.Process) *osProcess {
	if p == nil {
		return &osProcess{}
	}
	return &osProcess{pid: p.Pid, inner: p}
}
