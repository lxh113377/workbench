package main

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// nl 是换行的字节形式：本仓在册教训「多层字符串改写」会吃掉 Go 源码里的反斜杠转义，
// 所以这里用 rune 常量表达换行，源码内不出现任何反斜杠。
const nl = string(rune(10))

// extractBaselineRefs 从 check_perf.ps1 提取它引用的基线字段名（$bl.<名>）。
//
// 为什么不用正则：本仓在册教训「多层字符串改写」会让反斜杠在传输/落盘途中被吃掉，
// 所以这里用手写扫描，源码里不出现任何反斜杠或反引号。
// 为什么跳过注释行：注释里写 $bl.xxx 只是文档举例，不该逼基线给出该键（那是假红）。
func extractBaselineRefs(src string) []string {
	const marker = "$bl."
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(src, nl) {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		rest := line
		for {
			i := strings.Index(rest, marker)
			if i < 0 {
				break
			}
			rest = rest[i+len(marker):]
			j := 0
			for j < len(rest) && isIdentByte(rest[j]) {
				j++
			}
			name := rest[:j]
			if name != "" && !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

func isIdentByte(c byte) bool {
	return c == 95 || (c >= 48 && c <= 57) || (c >= 65 && c <= 90) || (c >= 97 && c <= 122)
}

// TestPerfBaselineKeysCoverGateReferences 钉住「判据引用的每个基线字段都必须在基线里存在」。
//
// 为什么需要（2026-10-25 实证的静默失效三天）：tests/check_perf.ps1 的体积下界判定（1b 段）
// 读 $bl.exeMinBytes；当基线里没有这个键时，PowerShell 取到 $null，该判定**不报错、不打日志**，
// 直接走 Skip 分支并让整条门禁 rc=0。演变链：ebd6442 立双向判且键=7747072 -> c6fa36e
// 「裁剪后重取基线」把键与 1b 段一并删 -> 74ce0ac（说明只写「25 个脚本补 BOM」）diff 却是
// +20 行，把 1b 段带回而无人补键 => 判据自此永走 SKIP，三天无人发现。
// 这类「代码里有判据、数据件里缺字段」的组合无法被任何运行期读数发现（SKIP 与 PASS 都 rc=0），
// 只能靠本判据这种静态配对检查。
//
// 反向也守：基线里为 null 的字段必须在 nullFieldReasons 登记理由 —— 否则下游无法区分
// 「确实未实测」与「漏填」，而本仓口径是 null 只能表示未实测。
func TestPerfBaselineKeysCoverGateReferences(t *testing.T) {
	ps, err := readRepoFile("tests/check_perf.ps1")
	if err != nil {
		t.Fatalf("读不到 tests/check_perf.ps1: %v", err)
	}
	rawJSON, err := readRepoFile("docs/benchmark/perf-baseline.json")
	if err != nil {
		t.Fatalf("读不到 docs/benchmark/perf-baseline.json: %v", err)
	}
	var bl map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &bl); err != nil {
		t.Fatalf("基线 JSON 解析失败: %v", err)
	}
	refs := extractBaselineRefs(ps)
	// 受理面哨兵：提取为空就说明测法或被测文件变了，此时下面所有循环都不执行、判据恒绿。
	if len(refs) == 0 {
		t.Fatal("从 check_perf.ps1 提取到 0 个基线字段引用 —— 判据在比空气（提取逻辑或被测文件已变）")
	}
	if len(bl) == 0 {
		t.Fatal("基线 JSON 解析出 0 个键 —— 判据失去受理面")
	}
	t.Logf("check_perf.ps1 引用基线字段 %d 个：%s", len(refs), strings.Join(refs, ", "))
	for _, r := range refs {
		if _, ok := bl[r]; !ok {
			t.Errorf("check_perf.ps1 引用了基线字段 %s，但 perf-baseline.json 没有这个键 —— 该判定会静默走 SKIP 且门禁仍 rc=0（实证：74ce0ac 带回 1b 段却没带键，失效三天）", r)
		}
	}
	reasons, _ := bl["nullFieldReasons"].(map[string]interface{})
	for k, v := range bl {
		if v != nil {
			continue
		}
		if _, ok := reasons[k]; !ok {
			t.Errorf("基线字段 %s 为 null 但 nullFieldReasons 未登记理由 —— null 在本仓口径里只表示「确实未实测」，漏登记会让下游误判为漏填", k)
		}
	}
}

// TestPerfBaselineLowerBoundStaysActive 钉住体积下界这条**双向判**不许再静默消失。
//
// 上一条判据只保证「引用了的字段必须存在」；若有人把 1b 段整体删掉，引用也随之消失，
// 上一条会变绿 —— 而那正是 c6fa36e 干过的事（删键+删段，自洽但功能没了，提交说明还写着旧的
// exeSizeMin PASS 读数）。所以这条对体积的两个方向做**双向核对**：键必须在且非 null，
// 判据分支也必须在（非注释行）。
func TestPerfBaselineLowerBoundStaysActive(t *testing.T) {
	ps, err := readRepoFile("tests/check_perf.ps1")
	if err != nil {
		t.Fatalf("读不到 tests/check_perf.ps1: %v", err)
	}
	rawJSON, err := readRepoFile("docs/benchmark/perf-baseline.json")
	if err != nil {
		t.Fatalf("读不到 docs/benchmark/perf-baseline.json: %v", err)
	}
	var bl map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &bl); err != nil {
		t.Fatalf("基线 JSON 解析失败: %v", err)
	}
	codeLines := []string{}
	for _, line := range strings.Split(ps, nl) {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			codeLines = append(codeLines, line)
		}
	}
	code := strings.Join(codeLines, nl)
	pairs := []struct {
		key    string
		branch string
		why    string
	}{
		{"exeMaxBytes", "exeSize ", "体积上界：拦依赖/资产误入产物的膨胀"},
		{"exeMinBytes", "exeSizeMin", "体积下界：拦截断/空壳/资产丢失的缩水"},
	}
	for _, p := range pairs {
		v, ok := bl[p.key]
		if !ok || v == nil {
			t.Errorf("体积双向判的基线字段 %s 缺失或为 null（%s）—— 该方向的判定会静默 SKIP", p.key, p.why)
			continue
		}
		if n, isNum := v.(float64); isNum && n <= 0 {
			t.Errorf("基线字段 %s = %v，非正数 —— 判定会走「非法值」SKIP 分支", p.key, n)
		}
		if !strings.Contains(code, p.branch) {
			t.Errorf("check_perf.ps1 里已找不到 %q 分支（%s）—— 与基线字段 %s 失去配对，判据被删而数据件还在", p.branch, p.why, p.key)
		}
	}
}
