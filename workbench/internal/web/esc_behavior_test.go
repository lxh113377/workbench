package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// escSnippet 从内嵌脚本里抽出「能独立跑 esc() 的最小片段」。
//
// ⚠️ 抽取区间口径是本轮用一次**假红**换来的（round 156）：app/pool 的映射表是
// 「const ESC 表」加一行动态补齐（用 fromCharCode(39) 做键、补上单引号实体），
// 我第一版探针只取到表结尾，漏掉补齐行，于是把「表里没有单引号键」当成缺陷，
// 差点写成决定性发现。⇒ 这里从 const ESC 一路取到 const esc **整行结束**，
// 并返回行数让读者能核对「抽全了没」，而不是只信一个布尔。
func escSnippet(js string) (string, int) {
	escIdx := strings.Index(js, "const esc =")
	if escIdx < 0 {
		return "", 0
	}
	escEnd := strings.Index(js[escIdx:], "\n")
	if escEnd < 0 {
		escEnd = len(js) - escIdx
	} else {
		escEnd += escIdx
	}
	start := escIdx
	if i := strings.Index(js, "const ESC ="); i >= 0 && i < escIdx {
		start = i
	}
	snippet := js[start:escEnd]
	return snippet, strings.Count(snippet, "\n") + 1
}

// TestEscBehavesOnAllFiveChars 用真实 JS 运行时执行 esc()，逐字符验证产出实体。
//
// 本轮实测出的真实缺口（不是缺陷复现 —— 缺陷复现是我的假红，已撤回）：
//
//	innerhtml_escape_test.go:88 判的是「单引号实体这个字面量在**整份脚本**里出现过」，
//	属字符串包含类判据。而 app/pool 的该实体其实在**动态补齐行**里、不在 ESC 表内，
//	⇒ 一种真实回归形态（删掉表里某行、或把补齐行挪走）字符串包含照旧通过、行为却坏。
//	全仓实测：用 node 真跑 JS 行为的判据只有 js_syntax_contract_test.go 一个（且只 --check
//	语法不执行）；esc 行为级判据 = 0 处。本文件补这个洞，并给 helper_identity_contract_test.go
//	:27 那句「三份 esc 行为等价」补上证据（原先是断言，没人跑过；现在由本判据守着）。
//
// 值得守的单点：实测 esc( 使用数 appJS 60 / poolJS 56 / govJS 31 处，一坏三页同时失守，
// 且 XSS 注入不需要报错、静默就能拼出属性上下文。
//
// 形态无关：三份实现形态不同（表查+补齐 / 内联对象），本判据只问行为，
// 因此既不会像第一版判据那样对 govJS 误报，也不放过任何一种形态的真回归。
func TestEscBehavesOnAllFiveChars(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("本机无 node，esc 行为判据未执行（没测成 != 合格）：%v", err)
	}
	// 受理面哨兵（与 TestEmbeddedJSIsSyntaxValid 同口径）：blob 退化成空串时
	// 后面所有 Contains 都会假过，所以先卡字节下限。
	minLen := map[string]int{"appJS": 20000, "govJS": 4000, "poolJS": 9000}
	blobs := map[string]string{"appJS": appJS, "govJS": govJS, "poolJS": poolJS}
	order := []string{"appJS", "govJS", "poolJS"}
	// 五个危险字符用 rune 构造（与本文件 escSnippet 的口径常量同法）：Go 源码里
	// 不出现带引号的 JS 字面量，从根上避开「引号套引号」这一类写坏文件的坑（本轮实测 3 次）。
	inputs := []string{
		string(rune(38)), // &
		string(rune(60)), // <
		string(rune(62)), // >
		string(rune(34)), // 双引号
		string(rune(39)), // 单引号
	}
	wants := []string{"&amp;", "&lt;", "&gt;", "&quot;", "&#39;"}
	for _, c := range inputs {
		if c == "" {
			t.Fatal("输入集里有空串 —— 转义字符没构造出来，后面会比空气")
		}
	}
	got := map[string][]string{}
	for _, name := range order {
		if len(blobs[name]) < minLen[name] {
			t.Errorf("%s 只有 %d 字节，低于下限 %d —— 内嵌脚本可能整段丢失", name, len(blobs[name]), minLen[name])
			continue
		}
		snippet, nLines := escSnippet(blobs[name])
		if snippet == "" {
			t.Fatalf("%s 里抽不到 esc 定义 —— XSS 防线根基被改名/删除？该页所有 innerHTML 同时失守", name)
		}
		if !strings.Contains(snippet, "const esc =") {
			t.Fatalf("%s 抽取区间不含 esc 定义（抽到 %d 行）⇒ 抽取法退化，不许据此判 esc 好坏", name, nLines)
		}
		// harness 用 fromCharCode 构造五个待测字符，避免在 Go 源码里嵌带引号的 JS 字面量。
		harness := strings.Join([]string{
			snippet,
			"var codes = [38, 60, 62, 34, 39];",
			"var out = [];",
			"for (var i = 0; i < codes.length; i++) { out.push(String(esc(String.fromCharCode(codes[i])))); }",
			"process.stdout.write(JSON.stringify(out));",
		}, "\n")
		f := filepath.Join(t.TempDir(), name+".js")
		if err := os.WriteFile(f, []byte(harness), 0o644); err != nil {
			t.Fatalf("写临时脚本失败：%v", err)
		}
		out, err := exec.Command(node, f).CombinedOutput()
		if err != nil {
			t.Errorf("%s 的 esc 片段跑不起来（%v）｜抽取 %d 行｜输出：%s｜先判抽取是否完整，再判 esc 是否有缺陷", name, err, nLines, strings.TrimSpace(string(out)))
			continue
		}
		var arr []string
		if err := json.Unmarshal(out, &arr); err != nil {
			t.Errorf("%s 输出无法解析成数组：%q", name, strings.TrimSpace(string(out)))
			continue
		}
		if len(arr) != len(wants) {
			t.Errorf("%s 返回 %d 项，期望 %d 项：%q", name, len(arr), len(wants), arr)
			continue
		}
		got[name] = arr
		t.Logf("%-6s 抽取 %d 行 ⇒ esc 行为：%q", name, nLines, arr)
	}
	// mismatch 跟踪：t.Errorf **不会中断函数**，所以"成功话术"若无条件打印，就会在已经判红的
	// 情况下照样输出 —— 本轮我在既有判据 innerhtml_escape_test.go:107 正是踩到这个（负控③ 里它
	// 一边报错一边打印"契约合规"）。⇒ 自己写的判据必须避免同一种误导，故这里显式跟踪。
	mismatch := false
	for _, name := range order {
		arr, ok := got[name]
		if !ok {
			continue
		}
		for i, w := range wants {
			if arr[i] != w {
				mismatch = true
				t.Errorf("%s 的 esc 对 %q 产出 %q，期望 %q —— 该字符在 innerHTML 里不被转义，注入可直接用（字符串包含类判据抓不到：实体字面量可能存在于别处）", name, inputs[i], arr[i], w)
			}
		}
		if !strings.HasPrefix(arr[0], "&") {
			mismatch = true
			t.Errorf("%s 首项 %q 不像实体 ⇒ 可能原样透传（静默透传比报错危险）", name, arr[0])
		}
	}
	if len(got) == len(order) && !mismatch {
		base := strings.Join(got[order[0]], "|")
		for i := 1; i < len(order); i++ {
			b := strings.Join(got[order[i]], "|")
			if base != b {
				mismatch = true
				t.Errorf("%s 与 %s 的 esc 行为不等价：%q vs %q", order[0], order[i], base, b)
			}
		}
		t.Logf("三份 esc 行为等价（逐字符实体相同）：%q —— 这句现在是跑出来的，不是断言的", base)
	} else if mismatch {
		t.Logf("（不打印等价结论：本轮已记录不匹配 ⇒ 成功话术必须由失败状态门控）")
	}
}
