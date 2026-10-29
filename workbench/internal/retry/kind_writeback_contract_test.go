package retry

// kind_writeback_contract_test.go —— 「直写类别字面量」必须有名分（issue #9 第 3 条）。
//
// 由来：本仓有三类 kind 长期以裸字符串直写 task.ErrorKind —— crash_loop（engine/stuck_rescue.go 的
// crashRescueKind 常量，写入点在同文件与 engine/runtime_rescue.go）、verify_failed
// （engine/repair_schedule.go）、replan（engine/replan.go）。它们不走 ParseKind、不在任何枚举里，于是：
//   一、IsRetryable 恒为 true（实测：ParseKind 收敛成 Unknown，而 Unknown.Retryable()==true）
//      ⇒ 判死/返工/重规划这类终态处置反而落进最宽的重试桶（与 issue #9 正文所说的「判 false」相反，
//      此处按 HEAD 源码实测写）；
//   二、Prometheus 侧靠「实测值并集」才没丢读数，若将来改成只输出 baseline 就会静默丢；
//   三、面板中文标签只对账 AllKinds，这几类在中文面板里裸显英文桶名。
//
// 本轮把三类收编进两侧枚举（字符串值逐字不变），并给台账里**不是失败类别**的 kind 一份显式旁路名单
// NonFailureLedgerKinds()：plan 是「这一轮在跑方案层」的**尝试类别**（生产台账实测 1035 行 / 共 2467 行），
// done 是成功行占位。它们塞进失败枚举会让 Prometheus 预置「失败」序列、面板补「失败」中文标签 ——
// 尝试与成功语义被污染成失败语义，所以走旁路而不是入枚举（issue #9 原话：不要混进 ErrorKind 枚举）。
//
// 判据三件：
//   A 全仓（非 _test.go）源文本里所有 ErrorKind 直写字面量、以及 AttemptRecord 内的 Kind 字面量，
//     必须落在 AllKinds() 或 NonFailureLedgerKinds() 里 —— 凭空新增一个裸字符串类别必红。
//   B 已收编的三类不得有「第二处字面量来源」：除两份枚举定义文件之外，任何非测试源文件里出现
//     带引号的该类别字符串即判红（放行比较形态与注释，见下面各函数的注释）。const X = "crash_loop"
//     这种形态正被本条抓住 —— 它就是本轮之前的实际形态。
//   C 旁路名单与失败枚举互斥、且不得被 ParseKind 解析 —— 把尝试类别混进失败枚举必红。
//
// 手法：扫**源文本**（本仓既有范式 = kind_drift_test.go / daily_count_source_test.go），只 import 标准库、
// 不 import executor，避开 retry 与 executor 的包环。扫描是**只读**的，覆盖面包含 engine.go 与 cmd/**
// （那是 Lead 的写面，本判据不需要动它们就能扫到 ⇒ 不留死角、也不需要放宽判据）。
// 口径（㉑二 在册教训）：解析结果为空一律判红，绝不把「什么都没解析到」当成「全部对齐」。

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 标点一律用码位常量：判据要按字节扫源文本，而 Go 源码里嵌引号是本仓在册坑（写错会让判据静默失效）。
const (
	bangByte   = byte(33)  // 感叹号：排除 != 比较
	dquoteByte = byte(34)  // 双引号：类别字面量的定界符
	colonByte  = byte(58)  // 冒号：composite literal 的键值分隔
	equalByte  = byte(61)  // 等号：赋值与比较
	braceByte  = byte(123) // 左大括号：深度计数
	closeByte  = byte(125) // 右大括号：深度计数
	starByte   = byte(42)  // 星号：块注释续行前缀
	nlByte     = byte(10)  // 换行
	spByte     = byte(32)  // 空格
	tabByte    = byte(9)   // 制表
	crByte     = byte(13)  // 回车
)

// kindQuote 是双引号的字符串形态（按码位现算），用于定界类别字面量。
var kindQuote = string(rune(dquoteByte))

// identByteSet 是 Go 标识符字符集，用来排除 XErrorKind 这类同后缀的其他字段名。
const identByteSet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_"

// isSpaceByte 判断空白（含换行）：跨行 composite literal 必须能跨过换行，否则 engine.go 六处台账写入点
// （全是跨行写法）会被漏检 —— 漏检即漏判。
func isSpaceByte(c byte) bool {
	return c == spByte || c == tabByte || c == nlByte || c == crByte
}

func isIdentByte(c byte) bool { return strings.IndexByte(identByteSet, c) >= 0 }

// skipSpaceTo 返回从 i 起第一个非空白下标。
func skipSpaceTo(src string, i int) int {
	for i < len(src) && isSpaceByte(src[i]) {
		i++
	}
	return i
}

// lineIsCommentAt 判断下标 i 所在行的前缀（到 i 为止）是否只有注释符与空白。
// 为什么要剔注释：doc 注释里有 res.ErrorKind = "unknown" 这种**举例文本**（本仓实测存在）。
// 不剔掉就把「注释里提到某类别」当成「代码直写某类别」，判据会比被测面更红（纪律 D10.2：先怀疑判据）。
func lineIsCommentAt(src string, i int) bool {
	lineStart := strings.LastIndexByte(src[:i], nlByte) + 1 // 未命中返回 -1，+1 即 0（文件首行）
	for j := lineStart; j < i; j++ {
		if isSpaceByte(src[j]) {
			continue
		}
		return strings.HasPrefix(src[j:], "//") || src[j] == starByte
	}
	return false
}

// comparisonBefore 判断引号起点之前是不是比较运算符（== 或 !=）。
// 为什么放行：断言面（cmd/workbench/plan_selftest.go 第 12 项 after.ErrorKind == "replan"、
// 以及 engine 侧若干 *_test.go）是**校验**历史值不被改名，它不供给写入值，不是「第二处来源」。
func comparisonBefore(src string, quoteIdx int) bool {
	j := quoteIdx - 1
	for j >= 0 && isSpaceByte(src[j]) {
		j--
	}
	if j < 0 || src[j] != equalByte {
		return false
	}
	k := j - 1
	for k >= 0 && isSpaceByte(src[k]) {
		k--
	}
	return k >= 0 && (src[k] == equalByte || src[k] == bangByte)
}

// repoSourceFiles 列出仓库 internal/ 与 cmd/ 下所有非测试 .go 源文件（相对测试工作目录，见 repoRootJoin）。
// 任一根下一个都没扫到即判红：对照面丢失不许被洗成「全部对齐」。
func repoSourceFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, root := range []string{repoRootJoin("internal"), repoRootJoin("cmd")} {
		before := len(out)
		if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			out = append(out, path)
			return nil
		}); err != nil {
			t.Fatalf("扫描 %s 失败: %v（对照面丢失判红，不许 Skip）", root, err)
		}
		if len(out) == before {
			t.Fatalf("根 %s 下没有一个非测试源文件 —— 判据不许把空集合当成全部对齐", root)
		}
	}
	return out
}

// repoRootJoin 把「仓库根相对」的路径拼成「测试工作目录相对」的路径。
//
// 为什么要包一层而不是直接写 filepath.Join("..", ...)：本测试在 internal/retry 包里，
// 工作目录是 internal/retry ⇒ 仓库根是上两级。少写一级会静默扫到空目录（把「对照面丢失」
// 洗成「全部对齐」，正是本仓在册禁例）。包一层让深度只有一处定义。
func repoRootJoin(parts ...string) string {
	return filepath.Join(append([]string{"..", ".."}, parts...)...)
}

// readRepoFile 读仓库源文件；读不到判红（绝不 Skip）。
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("读 %s 失败: %v（对照面丢失判红，不许 Skip）", rel, err)
	}
	return string(data)
}

// errorKindWrites 抽出 src 里「ErrorKind 被直写成字面量」的值，覆盖两种形态：
//
//	x.ErrorKind = "v"（赋值）与 composite literal 里的 ErrorKind: "v"（键值）。
//
// 排除：比较形态（== / !=）、非字面量右值（string(executor.KindX)、引用常量）、注释行、
// 以及 XErrorKind 这类同后缀字段名。空串（清空 ErrorKind，重跑前复位）也返回，由调用方按语义处置。
func errorKindWrites(src string) []string {
	var out []string
	from := 0
	for {
		i := strings.Index(src[from:], "ErrorKind")
		if i < 0 {
			break
		}
		i += from
		after := i + len("ErrorKind")
		if i > 0 && isIdentByte(src[i-1]) {
			from = after // XErrorKind：不是本字段
			continue
		}
		if lineIsCommentAt(src, i) {
			from = after
			continue
		}
		j := skipSpaceTo(src, after)
		if j >= len(src) {
			break
		}
		var val int // 字面量内容起点
		switch {
		case src[j] == colonByte:
			val = skipSpaceTo(src, j+1)
		case src[j] == equalByte && j+1 < len(src) && src[j+1] == equalByte:
			from = after // == 比较，不是直写
			continue
		case src[j] == equalByte && j > 0 && (src[j-1] == bangByte || src[j-1] == equalByte || src[j-1] == colonByte):
			from = after // != / = 出现在其它运算符里
			continue
		case src[j] == equalByte:
			val = skipSpaceTo(src, j+1)
		default:
			from = after
			continue
		}
		if val >= len(src) || src[val] != dquoteByte {
			from = after
			continue // 右值不是字面量（引用常量或枚举转换），已由别处保证有名分
		}
		end := strings.Index(src[val+1:], kindQuote)
		if end < 0 {
			break
		}
		out = append(out, src[val+1:val+1+end])
		from = val + 1 + end
	}
	return out
}

// attemptKindWrites 抽出 AttemptRecord{…} **一层之内** Kind: "v" 的字面量值。
// 为什么按大括号深度切而不是按行：台账写入点普遍跨行（engine.go 六处），按行会把后续行的
// Kind: 漏掉 —— 漏检就是漏判（本仓在册「解析面不完整就下结论」禁例）。
// 只认字面量：Kind: string(executor.KindRateLimited) 这类已由枚举供值的写法不是裸字符串。
// 排除 ErrorKind:（同后缀键）与注释行。
func attemptKindWrites(src string) []string {
	marker := "AttemptRecord{"
	var out []string
	pos := 0
	for {
		i := strings.Index(src[pos:], marker)
		if i < 0 {
			break
		}
		i += pos
		start := i + len(marker)
		depth := 1
		j := start
		for j < len(src) && depth > 0 {
			c := src[j]
			switch {
			case c == dquoteByte:
				// 整串跳过：字符串里的花括号不参与深度计数，否则会在 "...{..." 处错位
				n := strings.Index(src[j+1:], kindQuote)
				if n < 0 {
					j = len(src)
					break
				}
				j += n + 1
			case c == braceByte:
				depth++
			case c == closeByte:
				depth--
			}
			j++
		}
		if depth != 0 || j <= start {
			pos = start // 花括号不闭合 ⇒ 解析面不完整，跳过本处但继续扫后面的（漏扫由 checked==0 判据兜底）
			continue
		}
		body := src[start : j-1]
		q := 0
		for {
			ki := strings.Index(body[q:], "Kind:")
			if ki < 0 {
				break
			}
			ki += q
			if ki > 0 && isIdentByte(body[ki-1]) {
				q = ki + 5 // ErrorKind: 之类的同后缀键，跳过
				continue
			}
			m := skipSpaceTo(body, ki+len("Kind:"))
			if m >= len(body) || body[m] != dquoteByte {
				q = m + 1
				continue
			}
			e := strings.Index(body[m+1:], kindQuote)
			if e < 0 {
				break
			}
			out = append(out, body[m+1:m+1+e])
			q = m + 1 + e
		}
		pos = j
	}
	return out
}

// legalKindSet 是「枚举 ∪ 旁路名单」的并集。
func legalKindSet() map[string]bool {
	m := map[string]bool{}
	for _, k := range AllKinds() {
		m[string(k)] = true
	}
	for _, k := range NonFailureLedgerKinds() {
		m[k] = true
	}
	return m
}

// TestDirectKindWritesAreEnumerated 是判据 A。
func TestDirectKindWritesAreEnumerated(t *testing.T) {
	legal := legalKindSet()
	if len(legal) == 0 {
		t.Fatal("「枚举 ∪ 旁路名单」为空：判据没有对照面，不许当成全部对齐")
	}
	files := repoSourceFiles(t)
	checked := 0
	seen := map[string]int{}
	for _, rel := range files {
		src := readRepoFile(t, rel)
		vals := append(errorKindWrites(src), attemptKindWrites(src)...)
		for _, v := range vals {
			if v == "" {
				continue // 清空 ErrorKind（重跑前复位），不是类别
			}
			checked++
			seen[v]++
			if !legal[v] {
				t.Errorf("%s 直写了类别 %q：既不在 retry.AllKinds() 里，也不在 NonFailureLedgerKinds() 旁路名单里。"+
					"后果 = ParseKind 收敛成 Unknown（台账落成 kind=unknown、RetryableKinds 管不到）、"+
					"Prometheus baseline 缺序列、面板裸显英文。修法：失败类别进 retry.Kind + executor.Kind 两侧枚举"+
					"并登记 ParseKind；尝试/成功类别进 NonFailureLedgerKinds。", rel, v)
			}
		}
	}
	if checked == 0 {
		t.Fatalf("在 %d 个源文件里一个直写类别字面量都没解析到 —— 解析面已漂移（写入形态变了？），"+
			"判据不许把空集合当成全部对齐（㉑二）", len(files))
	}
	// 更强的「解析面活着」证据：engine.go 的台账写入点全是跨行 composite literal（六处），
	// plan 至少出现两次、done 一次。若跨行处理坏掉，checked 可能靠其它文件勉强非零而实际漏扫本仓主写面
	// —— 那正是「0 与 0 互查」的近似退化态（㉑二），所以要按已知项点名。
	for _, want := range []struct {
		kind string
		min  int
	}{
		{"plan", 2}, // engine.go 两处方案台账（usable / degraded）
		{"done", 1}, // engine.go 成功台账行（列同构）
	} {
		if seen[want.kind] < want.min {
			t.Errorf("只解析到 %d 处台账 kind=%q（期望至少 %d 处）：跨行 composite literal 的抽取已失效，"+
				"判据会漏扫真实写面", seen[want.kind], want.kind, want.min)
		}
	}
}

// TestCollectedKindsHaveSingleLiteralSource 是判据 B：收编后的三类只允许一处字面量来源。
// 违反现场（本轮修复前）：crashRescueKind 在 engine 里自带 "crash_loop" 裸字面量，与枚举里的
// CrashLoop 各写各的 —— 改一处另一处仍是旧值，那正是「第三份隐性名单」的形态。
func TestCollectedKindsHaveSingleLiteralSource(t *testing.T) {
	enumDefs := map[string]bool{
		repoRootJoin("internal", "retry", "retry.go"):       true,
		repoRootJoin("internal", "executor", "executor.go"): true,
	}
	for _, k := range []Kind{CrashLoop, VerifyFailed, Replan} {
		lit := kindQuote + string(k) + kindQuote
		// 定义面必须真的有这个字面量，否则本判据退化成空转的恒绿。
		for def := range enumDefs {
			if !strings.Contains(readRepoFile(t, def), lit) {
				t.Errorf("%s 的字面量 %s 在 %s 里找不到：枚举定义被改名或改成拼接 ⇒ 本判据失去对照面", k, lit, def)
			}
		}
		// 除此之外，全仓非测试源文件里出现该字面量只允许两种形态：比较（断言）与注释。
		for _, rel := range repoSourceFiles(t) {
			if enumDefs[rel] {
				continue
			}
			src := readRepoFile(t, rel)
			pos := 0
			for {
				i := strings.Index(src[pos:], lit)
				if i < 0 {
					break
				}
				i += pos
				if !lineIsCommentAt(src, i) && !comparisonBefore(src, i) {
					t.Errorf("%s 在 %s 里又出现一遍裸字面量 %s（非比较、非注释形态）：同一字符串必须只有一处来源"+
						"（枚举定义处）。写面改成引用枚举常量：retry 侧 CrashLoop/VerifyFailed/Replan，executor 侧 KindCrashLoop/KindVerifyFailed/KindReplan", k, rel, lit)
				}
				pos = i + len(lit)
			}
		}
	}
}

// TestNonFailureLedgerKindsStayOutOfFailureEnum 是判据 C：旁路名单与失败枚举互斥。
// 理由（issue #9 原话）：台账的 plan 是**尝试类别**而非失败类别，混进 ErrorKind 枚举会让
// Prometheus 为它预置「失败」序列、面板为它补「失败」中文标签，把尝试语义污染成失败语义。
func TestNonFailureLedgerKindsStayOutOfFailureEnum(t *testing.T) {
	bypass := NonFailureLedgerKinds()
	if len(bypass) == 0 {
		t.Fatal("旁路名单为空：plan 这类台账 kind 将无家可归，判据 A 会把它判红 ⇒ 等于判据失效")
	}
	inEnum := map[string]bool{}
	for _, k := range AllKinds() {
		inEnum[string(k)] = true
	}
	for _, b := range bypass {
		if b == "" {
			t.Fatal("旁路名单里有空串：会把「清空 ErrorKind」也放行")
		}
		if inEnum[b] {
			t.Errorf("旁路类 %q 同时出现在 AllKinds() 里：它到底是失败类别还是尝试类别？两者不可兼得", b)
		}
		if got := ParseKind(b); got != Unknown {
			t.Errorf("旁路类 %q 竟能被 ParseKind 解析成 %q：它已进失败枚举（本判据前提消失）", b, got)
		}
	}
}

// TestKindWritebackGateHasTeeth 是判据自证（负控，纪律 D10）：全程用内存里的假源文本，不碰磁盘
// ⇒ 这条红了只可能是判据本身被改坏。假源文本用 Go 原生 raw string 承载：它内部的双引号无需转义，
// 免得判据自己踩本仓在册的转义坑（判据比被测面先坏 = 假绿/假红双向都不可信）。
func TestKindWritebackGateHasTeeth(t *testing.T) {
	fake := `
func f() {
	x.ErrorKind = "brand_new_bare_kind"
	rec := store.AttemptRecord{TaskID: id, Kind: "another_new_kind", Action: "fail"}
	if t.ErrorKind == "timeout" {
		return
	}
	// res.ErrorKind = "unknown" —— 注释里的举例文本（cancel_cause.go 真有这种 doc 注释）
	y.ErrorKind = crashRescueKind
	z.ErrorKind = string(executor.KindReplan)
	prevErrorKind = "not_this_field"
}
`
	got := append(errorKindWrites(fake), attemptKindWrites(fake)...)
	want := []string{"brand_new_bare_kind", "another_new_kind"}
	if len(got) != len(want) {
		t.Fatalf("抽取器不认形态（判据已失去牙）: got=%+v want=%+v", got, want)
	}
	for idx, v := range got {
		if v != want[idx] {
			t.Fatalf("抽取内容/顺序不符: got=%+v want=%+v", got, want)
		}
	}
	// 上面假源文本里的四种「不该被抽到」的形态必须真的被排除：
	// 比较（== "timeout"）、注释（// ... "unknown"）、非字面量右值（crashRescueKind / string(executor.KindReplan)）、
	// 同后缀字段名（prevErrorKind）。若哪天有第五种混进来，got 的长度就会不等于 want 的 2。
	legal := legalKindSet()
	if len(legal) == 0 {
		t.Fatal("并集为空：判据 A 没有对照面")
	}
	for _, v := range want {
		if legal[v] {
			t.Fatalf("负控失效：假类别 %q 本不该有名分", v)
		}
	}
	// 跨行 composite literal 也必须抽得到（engine.go 六处台账写入点就是这个形态）。
	multi := `
e.store.AppendAttempt(store.AttemptRecord{
		TaskID: task.ID, Attempt: attempt, Model: m, Kind: "plan",
		Action: "done",
	})
`
	if got2 := attemptKindWrites(multi); len(got2) != 1 || got2[0] != "plan" {
		t.Fatalf("跨行台账字面量抽取失效（会漏判）: %+v", got2)
	}
	// 真实旁路类必须在并集里，否则判据 A 会把 engine.go 既有的合法台账行判红。
	if !legal["plan"] || !legal["done"] {
		t.Fatal("旁路名单没覆盖 plan/done：判据 A 会把既有台账写入误判成违规")
	}
	// 收编后的三类必须在并集里（判据 A 的正面半边）。
	for _, k := range []string{"crash_loop", "verify_failed", "replan"} {
		if !legal[k] {
			t.Fatalf("类别 %q 未进「枚举 ∪ 旁路」：issue #9 的收编被回退了", k)
		}
		if ParseKind(k) == Unknown {
			t.Fatalf("类别 %q 不能被 ParseKind 解析：登记面漂移", k)
		}
	}
}
