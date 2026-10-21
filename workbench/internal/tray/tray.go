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
	// OnWarn 接收托盘降级/自愈的诊断信息（例如"图标没注册上，原因是 X"）。
	// 托盘是可选增强：注册失败绝不能影响主面板，但**必须**把原因上报出去，
	// 否则用户看到的就是"状态栏没有图标"却不知道发生了什么。
	OnWarn func(msg string)
}
