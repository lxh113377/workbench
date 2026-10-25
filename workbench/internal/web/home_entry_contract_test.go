package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestHomeEntryPromise 守护「首页进入子页面的入口」这条产品承诺（2026-10-19 目标轮）。
//
// 背景：首页唯一按钮就是子界面的入口。实测抓到两类会让入口失效的回归：
//
//	① busy 态被 lock:true 禁用 —— disabled 的按钮连 click 都不派发，「启动中点它」白点；
//	② 开屏自动启动在途时的点击被 inflight 直接 return 吞掉 —— 页面停在首页，
//	   用户视角就是「点了没反应」。修复是 pendingEnter：吞请求不吞意图，启动收敛后补进。
//
// 判据用正则而非整行字面量：视觉重做会话会重排空格/对齐，字面量断言会把
// 「行为没坏、只是格式变了」误判成红（假红同样是缺陷）。锁 true / 裸 return
// 这类**语义**回归才允许判红。
func TestHomeEntryPromise(t *testing.T) {
	branch := strings.Index(appJS, "if (!isAdmin) {")
	if branch < 0 {
		t.Fatal("首页分支标记不存在，判据失效")
	}
	admin := strings.Index(appJS, "// ── 管理面板")
	if admin < 0 {
		t.Fatal("找不到管理面板段标记，判据失效")
	}
	home := appJS[branch:admin]

	// 1) busy 态不得锁按钮：锁了就没有 click 事件，入口在启动窗口内必死。
	if re := regexp.MustCompile("busy:\\s*\\{[^}]*lock:\\s*true"); re.MatchString(home) {
		t.Fatal("busy 态按钮被锁死（lock:true）：启动中的点击不会派发，入口失效")
	}
	if re := regexp.MustCompile("busy:\\s*\\{[^}]*lock:\\s*false"); !re.MatchString(home) {
		t.Fatal("busy 态必须保持可点（lock:false），在途点击走 pendingEnter 兜底")
	}

	// 2) 在途点击不得被吞：必须记意图、启动完成后兑现。
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile("pendingEnter\\s*=\\s*true"),
		regexp.MustCompile("enter\\s*\\|\\|\\s*pendingEnter"),
		regexp.MustCompile("pendingEnter\\s*=\\s*false"),
	} {
		if !re.MatchString(home) {
			t.Fatalf("缺少在途点击兜底 %s（启动窗口内的点击会被吞，入口时灵时不灵）", re.String())
		}
	}
	// 裸 return 复活 = 吞意图回归，必须红。
	if re := regexp.MustCompile("if\\s*\\(inflight\\)\\s*\\{\\s*return\\s*;?\\s*\\}"); re.MatchString(home) {
		t.Fatal("inflight 又变回裸 return：在途点击被吞（2026-10-19 实测缺陷复发）")
	}

	// 3) 入口的落点必须是带 token 的管理面板（点击跳转与隐藏后门同源）。
	if !strings.Contains(home, "/admin?token=") {
		t.Fatal("点击入口必须跳 /admin 且携带 token（否则落地就是 401）")
	}
	h := indexHTML("tk", "running")
	if !strings.Contains(h, "/admin?token=tk") {
		t.Fatal("首页隐藏后门必须指向 /admin（与按钮点击同一目的地）")
	}
}
