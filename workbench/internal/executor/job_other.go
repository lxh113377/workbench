//go:build !windows

package executor

import "syscall"

// job 在非 Windows 平台退化为直接 kill 进程。
type job struct{}

func attachJob(p *osProcess) *job { return &job{} }

func (j *job) kill()  {}
func (j *job) close() {}
