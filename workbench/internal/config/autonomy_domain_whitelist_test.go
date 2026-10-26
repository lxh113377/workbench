package config

import (
	"strings"
	"testing"
)

// TestNormalizeKeepsQualityDomainInAutonomyWhitelist 钉住「质量域可以进自主白名单」。
//
// 为什么需要（2026-10-26 实测，报告 docs/工作空间聚焦与模板域覆盖-五轴审查-2026-10-26.md §1.6）：
// normalizeAutonomy 把 allowedDomains 逐个与 DefaultAutonomyConfig 的**硬编码六领域**比对，
// 不在里面就剔除；而部署实例想要「自主任务只聚焦工作空间项目的工程质量」时必须写 "quality"。
// 实测证据：往 config.json 写入 quality 后，仅跑一次 exe（config.Store 读盘即 Normalize 并落盘），
// 该键就被静默剔除（备份 3792 B 含 quality / 现盘 3773 B 不含）；随后 --autonomy plan
// 直接报「草案被边界拒绝」、本轮无可规划草案 ⇒ 自主链路永远产不出质量域任务。
func TestNormalizeKeepsQualityDomainInAutonomyWhitelist(t *testing.T) {
	c := Config{SchemaVersion: 3}
	c.Autonomy = DefaultAutonomyConfig()
	c.Autonomy.Bounds.AllowedDomains = []string{"content", "quality"}
	c.Normalize()

	var hasQuality bool
	for _, dm := range c.Autonomy.Bounds.AllowedDomains {
		if dm == "quality" {
			hasQuality = true
		}
	}
	if !hasQuality {
		t.Fatalf("quality 被 Normalize 静默剔除：得到 %v —— 自主白名单永远无法包含质量域", c.Autonomy.Bounds.AllowedDomains)
	}
	for _, w := range c.Warnings {
		if strings.Contains(w, "quality") {
			t.Errorf("quality 是内置领域，不该被判非法：%q", w)
		}
	}
}

// TestNormalizeWarnsWhenStrippingUnknownDomain：剔除非法领域这件事必须**可观测**。
// 本仓既有机制就是 Config.Warnings（config.go:709 自述「钳制是静默的，Warnings 让被钳过
// 这件事可观测」），而白名单剔除路径此前一条都不登记 —— 这正是「写了配置却无效、还不留痕」
// 的隐身失败族；新增域（如后续 frontend）忘同步硬编码表时，应当能看到告警。
func TestNormalizeWarnsWhenStrippingUnknownDomain(t *testing.T) {
	c := Config{SchemaVersion: 3}
	c.Autonomy = DefaultAutonomyConfig()
	c.Autonomy.Bounds.AllowedDomains = []string{"content", "notADomain"}
	c.Normalize()

	for _, dm := range c.Autonomy.Bounds.AllowedDomains {
		if dm == "notADomain" {
			t.Fatalf("非法领域未被剔除：%v", c.Autonomy.Bounds.AllowedDomains)
		}
	}
	warned := false
	for _, w := range c.Warnings {
		if strings.Contains(w, "notADomain") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("剔除非法领域未登记 Warnings（静默钳制）：Warnings=%v", c.Warnings)
	}
}
