package web

import (
	"strings"
	"testing"
)

// TestHomeToggleIsLiveAndInstant 守护 2026-10-19 实测抓出的两个真缺陷：
//
//	① 首页按钮曾把 running/paused 置为 disabled：文案写着「运行中/已暂停」却永远点不动；
//	② 点击后只发请求、不 paint：CDP 实测按钮会在「已暂停」上多停 2 秒以上才跳，
//	   看起来像「点了没反应」（用户实际体验就是按钮坏了）。
//
// 判据：暂停/继续 helper 必须定义在首页分支**内**（它在页尾 return，写在分支外首页拿不到），
// 且必须先 paint 再发请求（即时反馈），失败要回滚。
func TestHomeToggleIsLiveAndInstant(t *testing.T) {
	branch := strings.Index(appJS, "if (!isAdmin) {")
	if branch < 0 {
		t.Fatal("首页分支标记 if (!isAdmin) 不存在，判据失效")
	}
	admin := strings.Index(appJS, "// ── 管理面板")
	if admin < 0 {
		t.Fatal("找不到管理面板段标记，判据失效")
	}
	home := appJS[branch:admin]

	for _, need := range []string{
		"const pauseHome = async ()",
		"const resumeHome = async ()",
		"paint('paused')",  // 即时反馈：先改按钮
		"paint('running')", // 即时反馈 + 失败回滚
		"/api/engine/pause",
		"/api/engine/resume",
	} {
		if !strings.Contains(home, need) {
			t.Fatalf("首页分支内缺少 %q（按钮不可用/无即时反馈）", need)
		}
	}
	// 首页分支不得引用管理面板段的 WB（分支 return 后 WB 根本不存在）。
	if strings.Contains(home, "WB.pause") || strings.Contains(home, "WB.resume") {
		t.Fatal("首页分支引用了管理面板才定义的 WB 对象（点击会抛 TypeError）")
	}
	// 点击分派（2026-10-19 目标轮定稿）：点它就进子界面，且三分支齐全。
	//   running → 直接进管理面板；paused → 继续再进；其余 → 启动后进（start(true)）。
	for _, need := range []string{
		"if (st === 'running') { enterPanel(); return; }",
		"if (st === 'paused') { resumeHome(); enterPanel(); return; }",
		"start(true);",
	} {
		if !strings.Contains(home, need) {
			t.Fatalf("点击分派缺少 %q（点击进入子界面的语义不完整）", need)
		}
	}
	// 自动触发必须 start(false)：开屏自动启动不得把人甩进子界面（越权，且会让探针全 unverified）。
	if !strings.Contains(home, "start(false)") {
		t.Fatal("自动触发没有用 start(false)：页面一打开就会跳走")
	}
	if strings.Contains(home, "{ start(); }") {
		t.Fatal("自动触发仍在调用无参 start()：会继承「点击进入」语义而自动跳转")
	}
}

// TestHomeButtonNeverLockedWhileRunning 诚实性：运行/暂停态不得被 paint 锁成 disabled，
// 否则按钮的文案（运行中/已暂停）与实际可操作性矛盾。
func TestHomeButtonNeverLockedWhileRunning(t *testing.T) {
	branch := strings.Index(appJS, "if (!isAdmin) {")
	admin := strings.Index(appJS, "// ── 管理面板")
	home := appJS[branch:admin]
	for _, bad := range []string{
		"running: { text: '运行中',   hint: '引擎运行中',   busy: false, lock: true  }",
		"paused:  { text: '已暂停',   hint: '引擎已暂停',   busy: false, lock: true  }",
	} {
		if strings.Contains(home, bad) {
			t.Fatalf("运行/暂停态按钮被锁死：%q", bad)
		}
	}
}
