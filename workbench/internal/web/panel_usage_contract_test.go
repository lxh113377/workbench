package web

import (
	"os/exec"
	"strings"
	"testing"
)

// 本文件守「界面使用自证」这块（round 165 · 承接 T113 的三件事）：
//   ① 显示口径必须把「有人来过」与「有【人】来过」分开 —— 本仓的 liveness / curl / node / 无头探针都会打这些
//      页面，不分开就是拿机器活动对使用者谎称界面被用过（与顶栏「停摆必须自证」同一条原则）。
//   ② appJS 写的每个静态 class 必须在 appCSS 有定义。我本轮就用了两个不存在的类（state-pending，以及指望
//      usage-line 自带基础样式）：JS 与 CSS 是两份独立 blob，编译器不管这个接缝，肉眼对照 38 个类也不可读。
//   ③ humanLatest 必须取最近一次（max）而非任取：多页被访问时任取一条会更早，读数长期偏旧。
//
// 三条都不依赖浏览器渲染；② 纯 Go，③ 复用同包 helperRunNode / helperArrowBody（不新造 node 机制）。

// cssDefinedClassSet 收集 appCSS 里所有被定义的类名（.foo 形式，含 .a:hover / .a .b 这类带伪类与空格的组合）。
func cssDefinedClassSet() map[string]bool {
	out := map[string]bool{}
	for i := 0; i+1 < len(appCSS); i++ {
		if appCSS[i] != 46 { // 点号
			continue
		}
		j := i + 1
		for j < len(appCSS) && isCSSNameByte(appCSS[j]) {
			j++
		}
		if j > i+1 {
			out[appCSS[i+1:j]] = true
		}
	}
	return out
}

func isCSSNameByte(b byte) bool {
	return (b >= 65 && b <= 90) || (b >= 97 && b <= 122) || (b >= 48 && b <= 57) || b == 45 || b == 95
}

// jsStaticClassUses 抽出 appJS 里 class="..." 的字面量类名（跳过含拼接符的，那类无法在此层判定）。
func jsStaticClassUses() []string {
	var got []string
	for i := 0; i+7 < len(appJS); i++ {
		if !strings.HasPrefix(appJS[i:], "class=") {
			continue
		}
		rest := appJS[i+6:]
		if len(rest) == 0 || rest[0] != 34 { // 双引号
			continue
		}
		end := strings.IndexByte(rest[1:], 34)
		if end < 0 {
			continue
		}
		val := rest[1 : 1+end]
		if strings.Contains(val, "+") || strings.Contains(val, "\u0024") || strings.Contains(val, "{") {
			continue
		}
		for _, w := range strings.Fields(val) {
			if w != "" {
				got = append(got, w)
			}
		}
	}
	return got
}

// TestJSClassesAreDefinedInCSS 钉住「JS 用到的每个静态 class 都在 CSS 里有定义」。
// 受理面哨兵先行：两侧集合都要非空且达到量级，否则「0 缺失」只是什么都没扫到（本会话在册形态）。
func TestJSClassesAreDefinedInCSS(t *testing.T) {
	defs := cssDefinedClassSet()
	if len(defs) < 30 {
		t.Fatalf("appCSS 只解出 %d 个类名 ⇒ 解析口径退化，本判据此刻在比空气", len(defs))
	}
	used := jsStaticClassUses()
	if len(used) < 30 {
		t.Fatalf("appJS 只解出 %d 处 class 用法 ⇒ 抽取口径退化（class 写法变了要同步本判据）", len(used))
	}
	missing := map[string]int{}
	for _, u := range used {
		if !defs[u] {
			missing[u]++
		}
	}
	for k, n := range missing {
		t.Errorf("JS 用了 class %q（%d 处）但 appCSS 没有 .%s ⇒ 该元素拿默认样式渲染，走查时不易察觉", k, n, k)
	}
	set := map[string]bool{}
	for _, u := range used {
		set[u] = true
	}
	t.Logf("appCSS 定义 %d 个类名；appJS 静态 class 用法 %d 处、去重 %d 个；未定义 %d 个",
		len(defs), len(used), len(set), len(missing))
}

// TestPanelUsageLineSeparatesHumanFromMachine 钉住显示口径的三态与数据来源。
func TestPanelUsageLineSeparatesHumanFromMachine(t *testing.T) {
	code := stripLineComments(appJS)
	for _, need := range []string{
		"st.panelVisits", "firstHuman", "humanPaths", "humanLatest",
		"本次启动以来无人打开过界面", "只有自动探针访问", "累计 ",
	} {
		if !strings.Contains(code, need) {
			t.Errorf("界面使用块缺少 %q ⇒ 三态之一被删或改名；缺块会被读成「没有这条信息」而非「代码没了」", need)
		}
	}
	h := strings.Index(code, "humanPaths = vpaths.filter")
	if h < 0 {
		t.Fatal("抽不到 humanPaths 的定义（写法变了要同步本判据），无法验证人类分支的数据来源")
	}
	if !strings.Contains(code[h:h+220], "firstHuman") {
		t.Error("humanPaths 不是由 firstHuman 筛出 ⇒ 机器访问会被显示成「有人在看界面」")
	}
	if strings.Contains(code, "state-pending") {
		t.Error("仍在用 appCSS 未定义的 state-pending")
	}
}

// TestHumanLatestPicksNewest 在 node 里真跑 humanLatest（同包已有 helperRunNode，不新造机制）。
func TestHumanLatestPicksNewest(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("本机无 node，humanLatest 行为判据未执行（没测成 != 合格）：%v", err)
	}
	body, n := helperArrowBody(t, appJS, "const humanLatest =", "humanLatest")
	if n < 4 {
		t.Fatalf("humanLatest 只抽到 %d 行 ⇒ 抽取退化，断言会比空气", n)
	}
	src := body + "\nglobal.out = humanLatest({a:{firstAt:100},b:{firstAt:900},c:{}}, [\"a\",\"b\",\"c\"]);" +
		"global.out2 = humanLatest({a:{firstAt:\"123\"},b:{}}, [\"a\",\"b\"]);" +
		"process.stdout.write(\"=\" + global.out + \"=\" + global.out2)"
	out, err := helperRunNode(t, node, "human_latest", src)
	if err != nil {
		t.Fatalf("humanLatest 跑不起来：%v（输出 %q）", err, out)
	}
	if !strings.Contains(out, "=900") {
		t.Errorf("多页时应取最近一次 900，实得 %q ⇒ 可能取了任意/更早的一条", strings.TrimSpace(out))
	}
	if !strings.Contains(out, "=123") {
		t.Errorf("字符串型 firstAt 应由 Number() 吸收成 123，实得 %q", strings.TrimSpace(out))
	}
}
