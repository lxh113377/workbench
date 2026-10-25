//go:build !windows

package executor

import (
	"syscall"
)

// job 在非 Windows 平台没有 Job Object，只能杀进程本身（杀不到子进程树）。
// 这是平台能力边界，不是可以省略的实现：attachJob 必须把 pid 记下来，kill 才有对象可杀。
// 旧版本这里 kill() 是空的、注释却写「退化为直接 kill 进程」，导致超时/卡死/取消三条路径上
// executor 只关了管道、进程根本没死（错误文案还声称「已回收进程」）—— 注释与实现分家的活案例。
type job struct {
	pid int
}

func attachJob(p *osProcess) *job {
	if p == nil {
		return &job{}
	}
	return &job{pid: p.pid}
}

// kill 向该进程发 SIGKILL：只能终止进程本身，其子进程需靠各自的退出逻辑或进程组收敛。
func (j *job) kill() {
	if j == nil || j.pid <= 0 {
		return
	}
	_ = syscall.Kill(j.pid, syscall.SIGKILL)
}

func (j *job) close() {}
