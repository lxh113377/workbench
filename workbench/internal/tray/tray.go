// Package tray 提供零依赖的 Windows 托盘图标与右键菜单。
// 非 Windows 平台退化为阻塞等待，保证主流程结构一致。
package tray

// Options 描述托盘行为。
type Options struct {
	Title    string
	Tooltip  string
	Status   func() string
	OnOpen   func()
	OnStart  func()
	OnPause  func()
	OnResume func()
	OnStop   func()
	OnQuit   func()
}
