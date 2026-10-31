package web

import (
	"strings"
	"testing"
)

// stateLockValues 从内嵌 JS 的首页按钮状态表里，按**行**解析指定状态的 lock 取值。
//
// 为什么按行而不是匹配整串字面量：状态表是 `running: { text: …, hint: …, busy: …, lock: … }`
// 一态一行；文案会改、对齐会动、空格数会变。老判据（home_contract_test.go 的逐字 Contains）
// 正是因此变成死判据 —— 实测：把 running 改成 lock:true 而文案保持"工作台"，老判据照样 ok。
//
// 匹配条件刻意收紧到「行首是该状态键 且 行内含 lock:」——首页状态表才有 lock 字段，
// 这样不会被任务态 STATE_TEXT（12 键、无 lock）之类同名键误命中。
// 返回找到的所有 lock 值；一个都没有 ⇒ 调用方判红（结构变了不许静默放行）。
func stateLockValues(t *testing.T, js, state string) []bool {
	t.Helper()
	var got []bool
	for _, line := range strings.Split(js, "\n") {
		trim := strings.TrimSpace(line)
		if !strings.HasPrefix(trim, state+":") {
			continue
		}
		if !strings.Contains(trim, "lock:") {
			continue
		}
		i := strings.Index(trim, "lock:") + len("lock:")
		rest := strings.TrimSpace(trim[i:])
		switch {
		case strings.HasPrefix(rest, "true"):
			got = append(got, true)
		case strings.HasPrefix(rest, "false"):
			got = append(got, false)
		default:
			t.Fatalf("%s 行的 lock 取值无法解析（%q）⇒ 状态表结构变了，本判据需同步更新", state, rest)
		}
	}
	return got
}

// TestHomeToggleStatesAreNotLocked 钉住「首页 running / paused 两态的按钮必须是活的（lock:false）」。
//
// 后果（实测问题 A，本文件既有注释记载）：lock:true 会把按钮置 disabled ⇒ 文案写着
// 「工作台/已暂停」却永远点不动，用户无法从首页暂停或继续引擎，而界面看上去一切正常。
//
// 取代关系（不是新增也不是放宽）：本判据替代 home_contract_test.go 里两条已被实测证伪的
// 逐字 Contains 断言 —— 旧断言在"文案不改、只把 lock 改成 true"这一类回归上完全失明（实验② 为证）。
func TestHomeToggleStatesAreNotLocked(t *testing.T) {
	// 成功话术必须由失败状态门控（元发现 24 同族 —— 我在本轮刚看到既有判据一边 t.Errorf
	// 报缺实体、一边无条件打印「契约合规」；写完自己的判据后又犯了一次同样的，故这里显式跟踪）。
	var failed bool
	for _, state := range []string{"running", "paused"} {
		got := stateLockValues(t, appJS, state)
		if len(got) == 0 {
			t.Fatalf("状态表里找不到 %s 行的 lock 字段 ⇒ 首页按钮状态表结构已变（或被改名/拆行），"+
				"本判据此刻什么都没守，必须同步改写，不许静默放行", state)
		}
		for i, v := range got {
			if v {
				t.Errorf("首页 %s 态（第 %d 处）lock:true ⇒ 按钮被锁死，用户无法从首页暂停/继续引擎", state, i+1)
				failed = true
			}
		}
		if !failed {
			t.Logf("%s 态共解析到 %d 处 lock 取值，均为 false（按钮可点）", state, len(got))
		}
	}
	// 反向自证：本判据必须能抓到"真锁死"——用内存里造的状态表跑一次，不碰源码
	fake := "    running: { text: '工作台', hint: 任何文案,   busy: false, lock: true },"
	if vals := stateLockValues(t, fake, "running"); len(vals) != 1 || vals[0] != true {
		t.Fatalf("解析器抓不到构造出的 lock:true（vals=%v）⇒ 本判据本身失效", vals)
	}
	t.Log("反向自证通过：解析器能抓到 lock:true（正是旧逐字 Contains 判据漏掉的那一类）")
}
