package engine

// 执行器能力注入的公共通道（2026-10-20 实测缺陷 D9 修复）。
//
// 事故复盘在册（docs/缺陷-dailyCap自锁空转 / 外部事件-OFM上游IP阻断）：全局限速
// 闸门在 2026-10-19 写好（internal/ratelimit + executor.SetLimiter + 面板可观测的
// RateLimiterSnapshot），但**生产从未把它装上去** —— 实测证据：
//   - 修复**前**grep 全仓（排除 _test.go 并排除注释行）ratelimit.New 的调用点为 0；
//     （写取证必须带时点：本行的 0 是修复前状态，修复后为 1 —— 见本文件 buildRateLimiter；
//      复核脚本若不过滤注释行，会把我这条注释也数进去而读成 2）
//   - executor.Runner.SetLimiter 的调用点也只有测试（limiter_integration_test.go、
//     limiter_snapshot_test.go）；
//   - RateLimiterSnapshot 的注释写着"main 里先把限流器装在内层 *executor.Runner 上"，
//     而 main.go 里根本没有这一步。
// 于是配置 requestsPerMinute=20 是一行**装饰品**：面板显示限速 20/分钟，实际发请求
// 不受任何约束（这正是 08:00-08:04 每分钟 45-70 次请求打爆免费车道的结构性原因）。
//
// 修复口径：闸门由引擎在装配/热更新时按配置注入到底层执行器，语义层（自评/摘要）
// 因共用同一执行器（D7 修复）自动纳入约束。测试侧仍可注入桩执行器不受影响。

import (
	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/ratelimit"
)

// applyRateLimit 按当前配置把全局限速闸门装到底层执行器上。
//
// 沿装饰器链下探（与 setRunnerAllowAll 同一手法）：main 会用 FailoverRunner 包住
// 真实执行器，直接对 e.runner 断言 SetLimiter 会拿到不匹配，表现为"配了限速但
// 快照全零"。这里找到第一层支持 SetLimiter 的实现即注入。
//
// 幂等：重复调用只是替换成新的令牌桶（配置热更新语义），不会叠加。
func (e *Engine) applyRateLimit(cfg config.ExecutorConfig) {
	lim := buildRateLimiter(cfg)
	r := e.runner
	for i := 0; i < 8; i++ {
		switch x := r.(type) {
		case interface{ SetLimiter(*ratelimit.Limiter) }:
			x.SetLimiter(lim)
			e.log.Info("全局限速闸门已装配",
				"perMinute", cfg.RequestsPerMinute, "burst", cfg.RequestBurst,
				"enabled", lim != nil)
			return
		case interface {
			Inner() executor.RunnerContract
		}:
			r = x.Inner()
			continue
		}
		return
	}
}

// attachHooksToRunner 沿装饰器链下探，把执行钩子装到第一层支持 AttachHooks 的实现上。
//
// 与 applyRateLimit 同一手法，但用「能力接口」而不是具体类型断言（L5 收敛）：
// engine 因此不必知道 *executor.Runner 或模型池装饰器的存在 —— 改造前
// engine.SetLogger 写的是 e.runner.(*executor.Runner)，接口里带具体类型等于没脱离结构体，
// 一旦执行器被装饰器包住（main.go 的做法）断言就不匹配，尝试日志静默失效。
// 新增执行通道只要实现 RunnerContract + AttachHooks 就自动获得尝试日志。
//
// 找不到也不报错：钩子是观测面，装配失败不得影响任务执行。
func attachHooksToRunner(r executor.RunnerContract, h *executor.Hooks) {
	for i := 0; i < 8; i++ {
		if r == nil {
			return
		}
		if x, ok := r.(interface{ AttachHooks(*executor.Hooks) }); ok {
			x.AttachHooks(h)
			return
		}
		if inner, ok := r.(interface {
			Inner() executor.RunnerContract
		}); ok {
			r = inner.Inner()
			continue
		}
		return
	}
}

// buildRateLimiter 从配置算出令牌桶。rate<=0 时返回 nil（不限流，与既有语义一致）。
func buildRateLimiter(cfg config.ExecutorConfig) *ratelimit.Limiter {
	if cfg.RequestsPerMinute <= 0 {
		return nil
	}
	burst := cfg.RequestBurst
	if burst < 1 {
		burst = 1
	}
	return ratelimit.New(float64(cfg.RequestsPerMinute)/60.0, burst)
}
