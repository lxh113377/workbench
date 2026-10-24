package dshprofile

import (
	"strings"
	"testing"
)

// TestBundlesAllowlistNeverMountsWebApp 钉住 CONTRIBUTING 硬约束 #4：
// 「不挂 @deepseek-ai/dsh-web-app —— 它会起常驻服务器让 headless 永不退出」。
//
// 为什么必须有测试（2026-10-25 实测）：这条约束今天只靠 Bundles 白名单的写法成立 ——
// 白名单里没有它，就挂不上。但 dshprofile 包两个受跟踪测试文件（bootstrap_env_test.go /
// profile_patch_tooling_test.go，共 13 个 Test）没有一个断言 Bundles 的内容，
// 且全仓 tests/ 目录里字符串 dsh-web-app 出现 0 次。
// ⇒ 后人往 Bundles 里加一行就能静默违反「一律不合并，没有例外」的约束，
//
//	而后果不是崩溃而是 headless 进程永不退出（CONTRIBUTING 记的实测：卡死 150s+），
//	在 7x24 无人值守里表现为「任务不再推进」—— 难归因、难发现。
//
// 判据取两面：① 黑名单（绝不含 web-app 类常驻 bundle）；② 白名单快照（当前就这三项，
//
//	增减必须显式改这里 —— 让「加 bundle」成为一个有意识的动作而非顺手编辑）。
func TestBundlesAllowlistNeverMountsWebApp(t *testing.T) {
	if len(Bundles) == 0 {
		t.Fatal("Bundles 为空 —— 判据失去受理面")
	}
	for _, b := range Bundles {
		if strings.Contains(b, "web-app") {
			t.Errorf("Bundles 含 %q —— 违反硬约束 #4：dsh-web-app 会起常驻 HTTP 服务，headless 进程永不退出", b)
		}
	}
	want := []string{"@deepseek-ai/dsh-base", "@deepseek-ai/dsh-headless", "dsh-our-free-model"}
	if len(Bundles) != len(want) {
		t.Errorf("Bundles 快照变化：现 %v，登记为 %v —— 若确需变更，请同时改本测试与 CONTRIBUTING 硬约束说明", Bundles, want)
		return
	}
	for i := range want {
		if Bundles[i] != want[i] {
			t.Errorf("Bundles[%d] = %q，登记为 %q", i, Bundles[i], want[i])
		}
	}
}
