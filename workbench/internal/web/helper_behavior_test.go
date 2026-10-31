package web

// helper_behavior_test.go —— 三份内嵌 JS（appJS / govJS / poolJS）的**行为级**判据。
//
// 补的是三条「对称改坏永绿」缺口（Lead 独立复算过，本轮自己也复跑过，见文末负控表）：
//   overlay_M4_apiToken.json ⇒ 原判据全绿（三份 api() 同时删掉 X-WB-Token 鉴权头）
//   overlay_M3_toast.json    ⇒ 原判据全绿（三份 toast() 同时删掉 classList.add 的 show）
//   overlay_M5_fold.json     ⇒ 原判据全绿（fold / poolFold / foldCard 的 esc(title) 被去掉）
//
// 共同根因：仓内既有判据是 CONTAIN 层（「某字面量在整份脚本里出现过」）+ 跨份 IDENTICAL 层
// （「三份复制体必须相同」）。对称改坏同时满足「字面量还在别处」与「三份仍然相同」，
// 两层一起失明。overlay_M12_entity_swap 进一步证明 CONTAIN 层**防删不防换**：
// 把 esc 的左尖括号映射改成右尖括号实体，实体字面量全在、行为已坏，
// 只有真跑 JS 的 TestEscBehavesOnAllFiveChars 转红。⇒ 本文件一律用 node 真执行，不新增 CONTAIN 主判据。
//
// 判据形态照本仓既有水准，不新造机制：
//   ① 受理面哨兵   —— 沿用 esc_behavior_test.go:61 的 20000/4000/9000 字节下限；
//   ② 抽取自证     —— 打印抽取行数与行号，并断言区间必含目标定义
//                      （照 esc_behavior_test.go:89、sse_fallback_test.go:14「锚点失配必须判红」）；
//   ③ 无 node 即 Skipf —— 理由写明「没测成 != 合格」（照 esc_behavior_test.go:57）；
//   ④ 成功话术由失败状态门控 —— 元发现 24（既有判据一边 t.Errorf 一边打印「契约合规」）；
//   ⑤ 注释里的缺陷字面量不得判成缺陷 —— 复用 no_dialog_contract_test.go 的 stripLineComments，
//                      并用反向对照钉成断言（元发现 23/25，照 topbar_failure_test.go:92）。
//
// 负控表（本轮实测，go test -overlay 注入；命令与逐条 FAIL 行见报告
// D:/workdesktop/xtmp/gc-judges-2026-10-31.md）：
//   M4_apiToken（删鉴权头）  ⇒ TestApiSendsAuthHeaderAcrossAllThreeBlobs 三份全红
//   M3_toast（删 show）      ⇒ TestToastActuallyShowsElement 三份全红
//   M5_fold（去 esc(title)） ⇒ TestFoldHelpersEscapeTitle：appJS/fold、poolJS/fold、poolJS/poolFold 红
//                              （M5 未注入 govJS，故 govJS/foldCard 保持绿 —— 是预期，不是判据漏守）
//   M12_entity_swap（换实体）⇒ TestFoldHelpersEscapeTitle 的 govJS/foldCard 红（CONTAIN 层抓不到的那一类）
//   M0_posctl（ESC 表改坏）  ⇒ 既有 TestEscBehavesOnAllFiveChars 红 ⇒ 正控：证明 -overlay 真生效
//
// 排除口径：跑本文件判据时加 -skip "TestAssetsPresentInArtifact"。该判据比对 dist 产物与源码，
// 在飞源码（server.go 正被 Lead 并发修改）会让产物落后而判红，与本文件无关，由 Lead 负责重建。
// 本文件三条判据都不读 /api/status 字段：P1 用 stub fetch、不触服务端，故不受 server.go 在飞改动影响。

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// helperBlobMinBytes 是「内嵌脚本确实还在」的字节下限，逐字沿用 esc_behavior_test.go:61 的口径。
// 必须先卡：三份 blob 若退化成空串，下面所有抽取会「找不到东西」、所有 Contains 会假过。
var helperBlobMinBytes = map[string]int{"appJS": 20000, "govJS": 4000, "poolJS": 9000}

// helperBlobNames 固定顺序：判据必须三份都过，顺序稳定便于逐行比对实测输出。
var helperBlobNames = []string{"appJS", "govJS", "poolJS"}

// helperLT / helperGT 用 rune 构造尖括号：本文件因此不含带引号的 HTML 字面量，
// 从根上避开「引号套引号写坏文件」这一类实测坑（同 esc_behavior_test.go:64 的理由）。
var helperLT = string(rune(60))

var helperGT = string(rune(62))

const (
	// helperTokenKey 是页面注入 token 的 window 键名（三份脚本共用同一注入约定）。
	helperTokenKey = "__WB_TOKEN__"
	// helperToastID 是 toast 容器元素 id；helperToastClass 是 CSS 真正据以显形的类
	// （assets_css.go 里 .toast.show 才把 opacity 拉到 1）。
	helperToastID    = "toast"
	helperToastClass = "show"
	// helperFoldWant 是「标题被正确转义」的唯一合法产物：输入 <img src=x onerror=1> 必须变成这个串。
	// 写成整串而不是「含某个实体」—— M12 那种「左右尖括号实体互换」的改动，单看「有没有 &lt;」会蒙过去。
	helperFoldWant = "&lt;img src=x onerror=1&gt;"
	helperFoldBody = "BODY-88"
	helperToastMsg = "PING-4e1b"
	// helperProbeA / helperProbeB 是两枚不同的假 token：同一份代码必须两次都把自己那枚发出去。
	// 为什么要两枚：若鉴权头的值是硬编码字面量，单枚 token 的行为判据会照样绿；换第二枚就露馅。
	helperProbeA = "TOK-9f3a7c"
	helperProbeB = "TOK-1b2c3d"
)

// 三条「缺陷字面量」—— 本文件的注释里会原样引用它们来解释判据在防什么，
// 因此每条判据都配一条注释免疫反向对照（⑤），否则注释自己会把修复判成缺陷。
const (
	helperDefectEmptyToken = "'X-WB-Token': ''"
	helperDefectWrongClass = "classList.add('toast')"
	helperDefectRawTitle   = "+ title +"
)

// helperRequireNode 取 node 路径；没有就 Skip 并写明「没测成 != 合格」。
// 照 esc_behavior_test.go:56-58 的口径 —— 绝不用「本机没 node」换一条绿色的行为判据。
func helperRequireNode(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("本机无 node，本判据未执行任何行为断言（没测成 != 合格）：%v", err)
	}
	return node
}

// helperBlobOf 按名取内嵌脚本本体。
func helperBlobOf(name string) string {
	switch name {
	case "appJS":
		return appJS
	case "govJS":
		return govJS
	case "poolJS":
		return poolJS
	}
	return ""
}

// helperCheckBlobsDistinct 自证「三份各测一遍」这件事成立：三份必须各自非空且互不相同。
// 若有人把 govJS 写成 appJS 的别名，三份循环会「全绿」而实际只守住一份（同 innerhtml_escape_test.go:32）。
func helperCheckBlobsDistinct(t *testing.T, names []string) {
	t.Helper()
	for i, n := range names {
		js := helperBlobOf(n)
		if js == "" {
			t.Fatalf("%s 为空 —— 三份覆盖不成立，本判据会静默漏守这一份", n)
		}
		for j := i + 1; j < len(names); j++ {
			if js == helperBlobOf(names[j]) {
				t.Errorf("%s 与 %s 内容完全相同 —— 疑似别名/复制漏改，三份覆盖会塌成一份", n, names[j])
			}
		}
	}
}

// helperFloor 受理面哨兵：blob 低于登记下限即判红（判据失去受理面时必须响）。
func helperFloor(t *testing.T, name string) int {
	t.Helper()
	js := helperBlobOf(name)
	need, ok := helperBlobMinBytes[name]
	if !ok {
		t.Fatalf("没有 %s 的字节下限登记 ⇒ 受理面口径漏配，不许按 0 放行", name)
	}
	if len(js) < need {
		t.Fatalf("%s 只有 %d 字节，低于下限 %d ⇒ 内嵌脚本可能整段丢失，后面所有断言都会假过", name, len(js), need)
	}
	return len(js)
}

// helperLineBy 按行前缀取整行，返回行内容与 1-based 行号（行号即「抽取自证」：
// 读者能去源码核对，而不是只信一个布尔）。取不到即判红，不静默返回空串。
func helperLineBy(t *testing.T, js, prefix, what string) (string, int) {
	t.Helper()
	lines := strings.Split(js, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), prefix) {
			return strings.TrimSpace(l), i + 1
		}
	}
	t.Fatalf("找不到 %s（前缀 %q）⇒ 结构已变/被改名，本判据必须同步改写，不许静默放行", what, prefix)
	return "", 0
}

// helperArrowBody 从 anchor 起抽取「整条箭头函数声明」（含收尾 };），返回内容与行数。
//
// 为什么不用 sse_fallback_test.go 的 functionBody：它按「第一个左花括号」起配对，
// 而 api 的签名是 async (path, opts = {}) =>  —— 第一个花括号是参数默认值里的那个空对象，
// 配对当场结束（实测只抽到 opts = {}）。⇒ 这里先跳到箭头 => 之后的第一个花括号，
// 再配对到收尾，并显式要求区间以 anchor 开头、以 }; 结尾（抽取区间必须完整，纪律 b）。
func helperArrowBody(t *testing.T, js, anchor, what string) (string, int) {
	t.Helper()
	i := strings.Index(js, anchor)
	if i < 0 {
		t.Fatalf("找不到 %s（锚点 %q）⇒ 结构已变/被改名，本判据必须同步改写，不许静默放行", what, anchor)
	}
	rest := js[i:]
	a := strings.Index(rest, "=>")
	if a < 0 {
		t.Fatalf("%s 的声明里没有 => ⇒ 形态已变（不再是箭头函数），本判据需同步改写", what)
	}
	o := strings.Index(rest[a:], "{")
	if o < 0 {
		t.Fatalf("%s 没有函数体", what)
	}
	o += a
	depth := 0
	for k := o; k < len(rest); k++ {
		switch rest[k] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end := k + 1
				if end < len(rest) && rest[end] == ';' {
					end++
				}
				region := rest[:end]
				if !strings.HasPrefix(region, anchor) {
					t.Fatalf("%s 抽取区间不以锚点开头 ⇒ 抽取法退化，不许据此判行为", what)
				}
				if !strings.HasSuffix(region, "};") {
					t.Fatalf("%s 抽取区间没有收尾到 };（尾部 %q）⇒ 抽取不完整，会把「抽到一半」当成缺陷（纪律 b）",
						what, region[len(region)-12:])
				}
				return region, strings.Count(region, "\n") + 1
			}
		}
	}
	t.Fatalf("%s 的大括号不配对（JS 被改坏或抽取起点不对）", what)
	return "", 0
}

// helperRunNode 把 harness 落成临时 .js 真跑一次，返回合并输出（stdout+stderr，便于排障）。
// label 进文件名 ⇒ 同一 t.TempDir() 下多条用例互不覆盖。
func helperRunNode(t *testing.T, node, label, src string) (string, error) {
	t.Helper()
	f := filepath.Join(t.TempDir(), label+".js")
	if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
		t.Fatalf("%s：写临时脚本失败：%v", label, err)
	}
	out, err := exec.Command(node, f).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// helperClip 截断长输出只用于报错可读性（不改变判定，只改日志）。
func helperClip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// helperCommentImmunity 反向对照（⑤）：把缺陷字面量只放进注释，剥注释后必须搜不到 ——
// 证明本判据判的是代码不是文本。
// 门控：若 code 本体已含该字面量（缺陷是真的），这条自证不报错、只记一行说明；
// 否则会挤出一条「stripLineComments 失效」的误导结论（变异注入时主判据已经判红，让它说话）。
func helperCommentImmunity(t *testing.T, code, defect string) {
	t.Helper()
	sample := "  // 修前形态是 " + defect + " —— 说明用\n" + code
	if !strings.Contains(sample, defect) {
		t.Fatalf("反向对照本身失效：样本里搜不到 %q（空对照 ⇒ 这条自证什么都没测）", defect)
	}
	if strings.Contains(code, defect) {
		t.Logf("代码本体已含 %q ⇒ 缺陷为真，跳过注释免疫自证（由主判据判红）", defect)
		return
	}
	if strings.Contains(stripLineComments(sample), defect) {
		t.Errorf("注释里的缺陷字面量没被剥掉 ⇒ stripLineComments 未生效，本判据会被注释骗（元发现 23/25）")
	}
}

// P1：三份 api() 必须真的带鉴权头。
//
// 为什么不能只判「X-WB-Token 这个字面量在脚本里出现过」：那是 CONTAIN 层，M4 正是它的盲区 ——
// 三份同时删掉 headers 里的鉴权项后，字面量在别处（服务端注释、既有 Go 测试）照样存在，
// 且三份仍然逐字相同 ⇒ CONTAIN + IDENTICAL 两层同时失明、判绿，而运行时所有请求一律 401。
// 本判据改判行为：stub 掉 fetch，真跑 api 打 /api/status，断言**实际发出的 headers**。
// 并用两枚不同的假 token 各跑一次：值必须跟着变 ⇒ 同时排掉「硬编码字面量」这种伪装修复。
func TestApiSendsAuthHeaderAcrossAllThreeBlobs(t *testing.T) {
	node := helperRequireNode(t)
	helperCheckBlobsDistinct(t, helperBlobNames)
	probes := []string{helperProbeA, helperProbeB}
	var failed bool
	for _, name := range helperBlobNames {
		size := helperFloor(t, name)
		// 抽取自证：TOKEN 定义行（含行号）+ api() 整条声明（大括号配对到收尾 };）。
		tokenLine, tokenNo := helperLineBy(t, helperBlobOf(name), "const TOKEN =", name+" 的 TOKEN 定义")
		apiRegion, apiLines := helperArrowBody(t, helperBlobOf(name), "const api = async", name+" 的 api() 定义")
		if !strings.Contains(apiRegion, "fetch(") {
			t.Fatalf("%s 抽取区间不含 fetch 调用（抽到 %d 行）⇒ 抽取区间不完整，不许据此判鉴权（纪律 b）", name, apiLines)
		}
		// 结构层（CONTAIN）只作第二道防线：剥注释后仍须有鉴权键与 TOKEN 引用。
		// 显式写明它不是主判据 —— 单独靠它就是本洞的病因（防删不防换、三份一起删就瞎）。
		code := stripLineComments(apiRegion)
		if !strings.Contains(code, "X-WB-Token") {
			t.Errorf("%s 的 api() 代码本体里没有 X-WB-Token ⇒ fetch 不会带鉴权头出去", name)
			failed = true
		}
		if !strings.Contains(code, "TOKEN") {
			t.Errorf("%s 的 api() 代码本体里没有 TOKEN 引用 ⇒ 鉴权头的值不是来自 TOKEN", name)
			failed = true
		}
		t.Logf("%-6s 自证：blob %d 字节｜TOKEN 定义在第 %d 行｜api() 抽取 %d 行", name, size, tokenNo, apiLines)

		var seen []string
		for _, probe := range probes {
			out, err := helperRunNode(t, node, name+"_P1_"+probe, helperP1Harness(tokenLine, apiRegion, probe))
			if err != nil {
				t.Errorf("%s 的 api() 行为判据跑不起来（probe %s，%v）｜抽取 %d 行｜输出：%s｜先判抽取是否完整，再判鉴权是否有缺陷",
					name, probe, err, apiLines, out)
				failed = true
				continue
			}
			cells, uerr := helperDecodeArray(out)
			if uerr != nil {
				t.Errorf("%s 输出无法解析成数组（probe %s）：%q", name, probe, out)
				failed = true
				continue
			}
			if len(cells) != 4 {
				t.Errorf("%s 返回 %d 项，期望 4 项（path/鉴权头/Content-Type/键名集）：%q", name, len(cells), cells)
				failed = true
				continue
			}
			if cells[0] != "/api/status" {
				t.Errorf("%s 请求的真实路径是 %q，期望 /api/status ⇒ 没打到预期接口，判据受理面可疑", name, cells[0])
				failed = true
			}
			if cells[1] == helperAbsent {
				t.Errorf("%s 的 api() 实际发出的 headers 里没有 X-WB-Token（headers=%v｜probe=%s）⇒ 前端所有请求都不带鉴权，服务端一律 401（M4 形态）",
					name, cells[3], probe)
				failed = true
			} else if cells[1] != probe {
				t.Errorf("%s 发出的 X-WB-Token=%q，期望 %q ⇒ 值不是来自 TOKEN（被硬编码或在拼装后被覆盖）", name, cells[1], probe)
				failed = true
			} else {
				seen = append(seen, cells[1])
			}
			if cells[2] == helperAbsent || !strings.Contains(cells[2], "application/json") {
				t.Errorf("%s 的 headers 缺 Content-Type（=%q）⇒ POST 体会被服务端按空体/表单体解析", name, cells[2])
				failed = true
			}
		}
		if len(seen) == len(probes) && seen[0] == seen[1] {
			t.Errorf("%s 两枚不同 probe 发出同一个 X-WB-Token=%q ⇒ 值是硬编码字面量，不是读 TOKEN", name, seen[0])
			failed = true
		}

		// 反向自证（证明判据有牙）：内存里对称删掉鉴权头（照 M4 的改法），stub 必须收不到该键。
		// 替换未命中 ⇒ 本条自证作废并判红，不许拿「跑出了结果」当结论（纪律 a）。
		old := "'X-WB-Token': TOKEN, "
		if !strings.Contains(apiRegion, old) {
			t.Errorf("%s 找不到 %q ⇒ 变异未命中，反向自证作废（必须重做变异，不得据此宣称已取证）", name, old)
			failed = true
		} else {
			mut := strings.Replace(apiRegion, old, "", 1)
			mout, merr := helperRunNode(t, node, name+"_P1_mut", helperP1Harness(tokenLine, mut, helperProbeA))
			if merr != nil {
				t.Errorf("%s 反向自证的变异脚本跑不起来（%v）｜输出：%s", name, merr, mout)
				failed = true
			} else if strings.Contains(mout, "X-WB-Token") {
				t.Errorf("%s 反向自证失败：删掉鉴权头后 stub 仍收到 X-WB-Token ⇒ 本判据恒绿（%s）", name, helperClip(mout, 200))
				failed = true
			} else {
				t.Logf("%s 反向自证通过：对称删掉鉴权头后 stub 收不到 X-WB-Token ⇒ 主判据有牙", name)
			}
		}
		helperCommentImmunity(t, code, helperDefectEmptyToken)
	}
	if !failed {
		t.Logf("三份 api() 真发出的 headers 均含 X-WB-Token，且值随 probe 变化（跑出来的，不是 Contains 断言的）")
	} else {
		t.Logf("（不打印成功结论：本轮已记录失败 ⇒ 成功话术由失败状态门控，元发现 24）")
	}
}

// helperAbsent 是 harness 里「那件东西根本不存在」的统一读数，必须与「存在但值为空串」区分开：
// 鉴权头缺失（M4 形态）与鉴权头值为空串是两种不同缺陷，混成一个读数就分不出来。
const helperAbsent = "ABSENT"

// helperNL 用 rune 构造换行：本文件新增代码因此不含任何转义序列，
// 不会因为「反斜杠 + n」被上游工具当转义吃掉而写坏文件（纪律 d 的同族坑）。
var helperNL = string(rune(10))

// helperQUO / helperAPO / helperAMP 与 helperLT/helperGT 同法用 rune 构造：
// 本判据要把这些字符当作「待转义输入」送进 node，Go 源码里因此不出现带引号的 JS/HTML 字面量（纪律 d）。
var helperQUO = string(rune(34))

var helperAPO = string(rune(39))

var helperAMP = string(rune(38))

// helperToastHost 把「哪份脚本跑在哪个页面上」写死，用于守 toast 宿元素。
// 为什么非守不可：toast() 里有一条静默退路 —— 取不到元素就只 console.log 后 return；
// 而 stub 的 getElementById 永远造得出元素，所以光跑 JS 抓不到「页面上根本没有 toast 元素」这类缺陷。
// 这条退路不是假想：pool_page_render.go:27 登记过「前端拿不到注入值 ⇒ 整条链路静默失效」的同族事故。
var helperToastHost = map[string]string{"appJS": adminPage, "govJS": governancePage, "poolJS": modelPoolPage}

// helperFoldHost 是 fold 产物的落点：三个 helper 的输出都进 innerHTML。
// 没有 innerHTML 落点时「转义与否」无可注入面，判据受理面就要在报告里降级 —— 这里先把落点钉住。
var helperFoldHost = map[string]string{"appJS": adminPage, "govJS": governancePage, "poolJS": modelPoolPage}

// helperJSONLit 把 Go 侧值编成 JS 源码里的字面量 —— encoding/json 的真实用途之一。
// 为什么必须走编码器而不是手工拼引号：P3 的第二枚探针本身就含单/双引号，手工拼会写出语法错的 JS，
// node 非零退出又会被当成「判据判红」—— 那是把 harness 自身故障误读成被测缺陷（实测坑）。
// 走 json.Marshal 后，判红只可能来自被测代码本身。
func helperJSONLit(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return helperQUO + "__MARSHAL_FAILED__" + helperQUO
	}
	return string(b)
}

// helperDecodeArray 把 harness 打出的那一行数组解成等长字符串集 —— encoding/json 的真实用途之二。
// 非字符串元素保留其 JSON 文本（而不是整条判红）：这样「JS 侧返回了意外类型」在报错里看得见形状，
// 不会退化成一句无法定位的 parse error。
func helperDecodeArray(out string) ([]string, error) {
	var arr []interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &arr); err != nil {
		return nil, err
	}
	cells := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			cells = append(cells, s)
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		cells = append(cells, string(b))
	}
	return cells, nil
}

// helperSafeFile 把判据标签洗成合法文件名：标签里带冒号与斜杠（行号自证用），Windows 下直接写失败。
func helperSafeFile(s string) string {
	return strings.NewReplacer("/", "_", ":", "_", " ", "_").Replace(s)
}

// helperHasClass 判断 harness 回传的逗号类名集里是否恰有该类名（整项比较，不用 Contains）。
// 用 Contains 会让 show 命中 show-wrap —— 而反向自证造的正是这种前缀名，
// 用 Contains 就等于把「加错类」的变异判成合格（纪律 c：读数口径先钉死）。
func helperHasClass(csv, cls string) bool {
	for _, c := range strings.Split(csv, helperCOMMA) {
		if strings.TrimSpace(c) == cls {
			return true
		}
	}
	return false
}

var helperCOMMA = string(rune(44))

// helperStubRuntimeLines 是 DOM/定时器的最小假件：只 stub「运行环境」，helper 本体一律用抽取到的真实源码。
// 三条判据共用同一份假件，避免「换一套 stub 就换个结论」。getElementById 对同一 id 永远返回同一元素
// —— toast 的加类/去类必须落在同一个对象上，才谈得上「先显形、后收起」。
// 逐行写成 Go 普通字符串（不用反引号原始串）：JS 里满屏单引号，原始串一旦含反引号就把文件写坏（纪律 d）。
var helperStubRuntimeLines = []string{
	"var __els = {};",
	"var __timers = [];",
	"function __mkEl() {",
	"  var e = {textContent: '', innerHTML: '', dataset: {}, _cls: {},",
	"    classList: {",
	"      add: function(c) { e._cls[c] = true; },",
	"      remove: function(c) { delete e._cls[c]; },",
	"      contains: function(c) { return !!e._cls[c]; }",
	"    },",
	"    addEventListener: function() {}, focus: function() {}, setAttribute: function() {}",
	"  };",
	"  return e;",
	"}",
	"var document = {",
	"  body: {classList: {contains: function() { return true; }}, addEventListener: function() {}},",
	"  getElementById: function(id) { if (!__els[id]) { __els[id] = __mkEl(); } return __els[id]; },",
	"  querySelector: function() { return null; },",
	"  querySelectorAll: function() { return []; },",
	"  createElement: function() { return __mkEl(); },",
	"  addEventListener: function() {}",
	"};",
	"var window = {};",
	"var setTimeout = function(fn, ms) { __timers.push({fn: fn, ms: ms}); return __timers.length; };",
	"var clearTimeout = function() {}",
	"var console = {log: function() {}, warn: function() {}, error: function() {}};",
}

// helperStubFetchLines 记录「实际发出的请求」：判据问的是 fetch 收到什么，不是代码里写了什么。
// 返回体固定为空 JSON，让 api() 走通解析分支而不触网 —— 本判据因此与 server.go 在飞改动无关
// （P1 不启 httptest、不打真接口，Lead 正在改的字段一个都不碰）。
var helperStubFetchLines = []string{
	"var __calls = [];",
	"var fetch = function(p, o) { __calls.push({p: p, o: o || {}}); return Promise.resolve({ok: true, status: 200, text: function() { return Promise.resolve('{}'); }}); };",
}

// helperJoinLines 把若干 Go 字符串切片拼成一份 .js 源码。
func helperJoinLines(groups ...[]string) string {
	var all []string
	for _, g := range groups {
		all = append(all, g...)
	}
	return strings.Join(all, helperNL)
}

// helperP1Harness 组装 P1 的可执行脚本：假件 + TOKEN 定义行 + api() 整条声明 + 驱动。
// token 走 window.__WB_TOKEN__ 注入（与 page.go:16 的真实注入同名同形），
// 于是「头里的值」必然来自 TOKEN 这条读取链，中间没有我为了让它变绿而铺的捷径。
func helperP1Harness(tokenLine, apiRegion, probe string) string {
	return helperJoinLines(helperStubRuntimeLines, helperStubFetchLines, []string{
		"window." + helperTokenKey + " = " + helperJSONLit(probe) + ";",
		tokenLine,
		apiRegion,
	}, helperP1DriverLines("/api/status"))
}

// helperP1DriverLines 只打一行 JSON 数组：[请求路径, 鉴权头值, Content-Type 值, 键名集]。
// 键名集放第 4 格：报错时读者能直接看出 fetch 到底收到哪几个头，而不是只信一个 bool。
// threw 拼进键名集：api() 若抛异常，报错里看得见异常文本（否则「跑挂了」会被读成「没带头」）。
func helperP1DriverLines(path string) []string {
	return []string{
		"(async function () {",
		"  var threw = '';",
		"  try { await api(" + helperJSONLit(path) + ", {method: 'GET'}); } catch (e) { threw = String((e && e.message) || e); }",
		"  var c = __calls.length ? __calls[__calls.length - 1] : null;",
		"  var h = (c && c.o && c.o.headers) ? c.o.headers : {};",
		"  var has = function(k) { return Object.prototype.hasOwnProperty.call(h, k); };",
		"  var keys = Object.keys(h).sort().join(',');",
		"  var ak = " + helperJSONLit("X-WB-Token") + ";",
		"  var ck = " + helperJSONLit("Content-Type") + ";",
		"  var ab = " + helperJSONLit(helperAbsent) + ";",
		"  var tok = has(ak) ? String(h[ak]) : ab;",
		"  var ctype = has(ck) ? String(h[ck]) : ab;",
		"  var p = c ? String(c.p) : ab;",
		"  if (threw) { keys = keys + '|THREW:' + threw; }",
		"  process.stdout.write(JSON.stringify([p, tok, ctype, keys]));",
		"})();",
	}
}

// helperP2Harness 组装 P2 脚本：假件 + 真 toast 定义行 + 驱动。
func helperP2Harness(toastLine, msg, toastID string) string {
	return helperJoinLines(helperStubRuntimeLines, []string{toastLine}, helperP2DriverLines(msg, toastID))
}

// helperP2DriverLines 打 [textContent, 调用后类名集, 定时器跑完后的类名集, 挂了几枚定时器]。
// 第 3、4 格守的是另一半契约：显形之后还必须真的收起（只 add 不 remove = 提示永久挂在屏上）。
func helperP2DriverLines(msg, toastID string) []string {
	return []string{
		"toast(" + helperJSONLit(msg) + ");",
		"var el = __els[" + helperJSONLit(toastID) + "] || null;",
		"var clsOf = function() { return el ? Object.keys(el._cls).sort().join(',') : " + helperJSONLit(helperAbsent) + "; };",
		"var text = el ? String(el.textContent) : " + helperJSONLit(helperAbsent) + ";",
		"var shown = clsOf();",
		"__timers.forEach(function (tm) { try { tm.fn(); } catch (e) {} });",
		"var rest = clsOf();",
		"process.stdout.write(JSON.stringify([text, shown, rest, String(__timers.length)]));",
	}
}

// helperP3Harness 组装 P3 脚本：真 esc 片段 + 真 fold 系定义行 + 驱动。
// escRegion 必须来自 escSnippet（含 app/pool 那行 fromCharCode(39) 动态补齐）——
// 少抽那一行就会把「表里没有单引号键」当成缺陷，那是本仓已经踩过一次的假红（纪律 b）。
// 探针标题逐条 helperJSONLit 编进去：第二枚含单双引号，手工拼必炸。
func helperP3Harness(escRegion, helperLine, fname string, titles []string, body string) string {
	drv := []string{
		"var out = [(typeof " + fname + " === 'function') ? 'function' : " + helperJSONLit(helperAbsent) + "];",
	}
	for _, tt := range titles {
		drv = append(drv, "try { out.push(String("+fname+"("+helperJSONLit(tt)+", "+helperJSONLit(body)+"))); } catch (e) { out.push('THREW:' + String((e && e.message) || e)); }")
	}
	drv = append(drv, "process.stdout.write(JSON.stringify(out));")
	return helperJoinLines(helperStubRuntimeLines, []string{escRegion, helperLine}, drv)
}

// 用 rune 现拼的 CSS 片段常量：本文件因此不出现带引号的 CSS/JS 字面量（同纪律 d 的理由）。
func helperLBCurly() string { return string(rune(123)) }

func helperRBCurly() string { return string(rune(125)) }

func helperSP() string { return string(rune(32)) }

// helperOpenDelims 是选择器结束的两个合法标记（左花括号 / 逗号分组）。
var helperOpenDelims = string(rune(123)) + string(rune(44))

var helperOP0 = "opacity" + string(rune(58)) + "0"

var helperOP1 = "opacity" + string(rune(58)) + "1"

var helperPE0 = "pointer-events" + string(rune(58)) + "none"

// helperToastVisibleClass 从真实 appCSS 里解析「toast 据以显形的类名」，并回报该规则全文。
//
// 为什么不硬编码 show：硬编码就又是一条 CONTAIN 层断言（「代码里写过 show」）。
// 真契约是「JS 加的类」与「CSS 把 opacity 拉到 1 的类」这两个来源不同的东西必须对上，
// 所以类名从 CSS 一侧取：改类名时判据跟着问 CSS，而不是跟着源码字面量一起失效。
func helperToastVisibleClass(t *testing.T) (string, string) {
	t.Helper()
	tag := ".toast" + helperLBCurly()[:1]
	i := strings.Index(appCSS, ".toast.")
	if i < 0 {
		t.Fatalf("appCSS 里找不到 .toast. 规则 ⇒ 没有据以显形的类，toast 可见性契约无从谈起（受理面已消失）")
	}
	rest := appCSS[i+len(".toast."):]
	cut := strings.IndexAny(rest, helperOpenDelims)
	if cut <= 0 {
		t.Fatalf(".toast. 之后的规则形态已变（头部 %q）⇒ 类名抽取退化，本判据需同步改写", helperClip(rest, 40))
	}
	cls := strings.TrimSpace(rest[:cut])
	b := strings.Index(rest, helperLBCurly())
	e := strings.Index(rest[b:], helperRBCurly())
	if b < 0 || e < 0 {
		t.Fatalf(".toast.%s 规则大括号不配对 ⇒ 抽取不完整，不许据此判可见性（纪律 b）", cls)
	}
	rule := strings.TrimSpace(rest[b+1 : b+e])
	if !strings.Contains(strings.ReplaceAll(rule, helperSP(), ""), helperOP1) {
		t.Errorf("CSS 里 .toast.%s 规则不含 %s（实得 %q）⇒ 该类并不能让 toast 显形，判据受理面已变", cls, helperOP1, helperClip(rule, 90))
	}
	_ = tag
	return cls, rule
}

// helperToastBaseHidden 单独一条：基础态 .toast 必须同时 opacity:0 且 pointer-events:none。
// 不满足时本判据没有受理面（加了类也不改变可见性），必须报出来而不是静默给绿。
func helperToastBaseHidden(t *testing.T) bool {
	t.Helper()
	b := strings.Index(appCSS, ".toast"+helperLBCurly())
	if b < 0 {
		t.Errorf("appCSS 里找不到 .toast 基础规则 ⇒ 无从判断它默认是否隐藏（受理面已消失）")
		return false
	}
	br := appCSS[b:]
	e := strings.Index(br, helperRBCurly())
	if e < 0 {
		t.Errorf(".toast 基础规则大括号不配对 ⇒ 抽取不完整")
		return false
	}
	flat := strings.ReplaceAll(br[:e], helperSP(), "")
	hidden := strings.Contains(flat, helperOP0) && strings.Contains(flat, helperPE0)
	if !hidden {
		t.Errorf("appCSS 的 .toast 基础态没有同时声明 %s 与 %s ⇒ 加不加显形类都不影响可见性，本判据失去受理面", helperOP0, helperPE0)
	}
	return hidden
}

// helperFoldHelpers 按签名（不是按名字）从一份 blob 里找出所有把 title 拼进标记的 helper。
//
// 为什么按签名找：点名（只测 fold）会让 poolFold / foldCard 漏守；
// 而按「有没有 esc(title)」检索更不行 —— M5 恰恰是把 esc(title) 改成 title，
// 检索条件本身会随缺陷消失，判据于是静默什么都不测（CONTAIN 层「防删不防换」的变体）。
// 签名条件与转义与否无关：const 开头 + 形参含 (title, + 产出里含 summary 或 h3 开标签。
func helperFoldHelpers(t *testing.T, js, name string) [][2]string {
	t.Helper()
	var found [][2]string
	sumTag := helperLT + "summary" + helperGT
	h3Tag := helperLT + "h3" + helperGT
	for i, l := range strings.Split(stripLineComments(js), helperNL) {
		s := strings.TrimSpace(l)
		if !strings.HasPrefix(s, "const ") || !strings.Contains(s, "= (title,") {
			continue
		}
		if !strings.Contains(s, sumTag) && !strings.Contains(s, h3Tag) {
			continue
		}
		fields := strings.Fields(s)
		if len(fields) < 3 {
			continue
		}
		hname := fields[1]
		found = append(found, [2]string{name + "/" + hname + ":" + strconv.Itoa(i+1), s})
	}
	if len(found) == 0 {
		t.Errorf("%s 里没找到任何把 title 拼进标记的 helper ⇒ 抽取面为空，不许按合格放行（受理面哨兵）", name)
	}
	return found
}

// P2：三份 toast() 必须让提示**真的显形**，而不是只「把文案塞进某个元素」。
//
// 为什么不能判「show 这个字面量在脚本里出现过」：M3 正是删掉 classList.add 的 show 而**保留**
// remove 的 show —— 字面量照旧在场、三份照旧逐字相同 ⇒ CONTAIN + IDENTICAL 两层全绿，
// 而用户端所有成功/失败提示永久 opacity:0（静默失效，没有任何报错）。
// 本判据真调 toast，问 stub 实际收到的**类集合**，类名从真实 CSS 解析（helperToastVisibleClass），
// 于是「根本没加类」「加了但加错类」「加了但 CSS 不再显形」三种形态都会转红。
func TestToastActuallyShowsElement(t *testing.T) {
	node := helperRequireNode(t)
	helperCheckBlobsDistinct(t, helperBlobNames)
	visCls, visRule := helperToastVisibleClass(t)
	baseHidden := helperToastBaseHidden(t)
	failed := false
	for _, name := range helperBlobNames {
		size := helperFloor(t, name)
		line, no := helperLineBy(t, helperBlobOf(name), "const toast = (", name+" 的 toast 定义")
		// 抽取完整性自证（纪律 b）：toast 是单行箭头函数，定义行必须以分号收尾，
		// 否则就是只截到半行 —— 半行喂进 node 会「跑不起来」，很容易被误读成缺陷。
		if !strings.HasSuffix(line, ";") {
			t.Errorf("%s 的 toast 定义行不以分号收尾（尾部 %q）⇒ 抽取只截到半行，不许据此判可见性（纪律 b）", name, helperClip(line, 44))
			failed = true
			continue
		}
		// 受理面另一半：真实页面标记里必须有 id=toast 且带基础类的宿主元素。
		// 缺了它，toast() 走的是「取不到元素就 console.log 后 return」那条静默退路，
		// 而 stub 的 getElementById 永远给得出元素 ⇒ 光跑 JS 抓不到这一类缺陷。
		host, ok := helperToastHost[name]
		if !ok {
			t.Errorf("没有登记 %s 对应的页面标记 ⇒ toast 宿元素没被守住，静默退路可绕过本判据", name)
			failed = true
		} else {
			idAttr := "id=" + helperQUO + helperToastID + helperQUO
			clsAttr := "class=" + helperQUO + helperToastID + helperQUO
			if !strings.Contains(host, idAttr) || !strings.Contains(host, clsAttr) {
				t.Errorf("%s 对应页面标记里没有同时含 %s 与 %s 的元素 ⇒ 真页面上 toast() 只会 console.log，用户看不到任何提示", name, idAttr, clsAttr)
				failed = true
			}
		}
		out, err := helperRunNode(t, node, name+"_P2", helperP2Harness(line, helperToastMsg, helperToastID))
		if err != nil {
			t.Errorf("%s 的 toast 行为判据跑不起来（%v）｜toast 定义在第 %d 行｜输出：%s｜先判抽取与 harness，再判可见性", name, err, no, out)
			failed = true
			continue
		}
		cells, uerr := helperDecodeArray(out)
		if uerr != nil || len(cells) != 4 {
			t.Errorf("%s 的 toast 输出无法解析成 4 项（err=%v）：%q", name, uerr, out)
			failed = true
			continue
		}
		t.Logf("%-6s 自证：blob %d 字节｜toast 定义第 %d 行｜CSS 显形类=%q 规则{%s}｜基础态隐藏=%v｜实跑读数 text=%q 调用后类集=%q 收起后类集=%q 定时器数=%s",
			name, size, no, visCls, helperClip(visRule, 64), baseHidden, cells[0], cells[1], cells[2], cells[3])
		if cells[0] == helperAbsent {
			t.Errorf("%s 的 toast 拿不到 id=%q 的元素（连 stub 都没造出来）⇒ harness 退化，本条什么都没测", name, helperToastID)
			failed = true
		} else if cells[0] != helperToastMsg {
			t.Errorf("%s 调 toast(%q) 后元素 textContent 实为 %q ⇒ 文案没落到 toast 上", name, helperToastMsg, cells[0])
			failed = true
		}
		if cells[1] == helperAbsent || !helperHasClass(cells[1], visCls) {
			t.Errorf("%s 调 toast 后类集是 %q，不含 CSS 显形类 %q ⇒ 提示永久 opacity:0，用户看不到任何反馈且无报错（M3 形态）", name, cells[1], visCls)
			failed = true
		}
		if cells[2] != "" {
			t.Errorf("%s 定时器跑完后类集仍是 %q ⇒ 提示显形后收不回去（永久挂在屏上）", name, cells[2])
			failed = true
		}
		if cells[3] != "1" {
			t.Errorf("%s 调一次 toast 挂了 %s 枚定时器（期望 1）⇒ 自动收起链路不存在或重复叠加", name, cells[3])
			failed = true
		}
		// 反向自证（纪律 a：先校验替换命中，未命中即作废并判红，不许拿「跑出了结果」当结论）：
		// 把加的那个类换成 CSS 不认的前缀名 —— 字面量 show 仍在场，CONTAIN 层照样绿，行为已坏。
		old := "classList.add(" + helperAPO + visCls + helperAPO + ")"
		if !strings.Contains(line, old) {
			t.Errorf("%s 找不到 %q ⇒ 变异未命中，反向自证作废（必须重做变异，不得据此宣称已取证）", name, old)
			failed = true
		} else {
			mut := strings.Replace(line, old, "classList.add("+helperJSONLit(visCls+"-wrap")+")", 1)
			mout, merr := helperRunNode(t, node, name+"_P2_mut", helperP2Harness(mut, helperToastMsg, helperToastID))
			if merr != nil {
				t.Errorf("%s 反向自证的变异脚本跑不起来（%v）｜输出：%s", name, merr, mout)
				failed = true
			} else {
				mcells, cerr := helperDecodeArray(mout)
				if cerr != nil || len(mcells) != 4 {
					t.Errorf("%s 反向自证输出异常（err=%v）：%q", name, cerr, mout)
					failed = true
				} else if helperHasClass(mcells[1], visCls) {
					t.Errorf("%s 反向自证失败：改成加错类后 stub 仍报显形类在场 ⇒ 本判据恒绿：%q", name, mcells[1])
					failed = true
				} else {
					t.Logf("%s 反向自证通过：加错类（字面量仍含 %q）时 stub 类集为 %q ⇒ 主判据有牙", name, visCls, mcells[1])
				}
			}
		}
		helperCommentImmunity(t, stripLineComments(line), helperDefectWrongClass)
	}
	if !failed {
		t.Logf("三份 toast() 真把 CSS 显形类 %q 加上、文案落到元素、并能按时收起（跑出来的，不是 Contains 断言的）", visCls)
	} else {
		t.Logf("（不打印成功结论：本轮已记录失败 ⇒ 成功话术由失败状态门控，元发现 24）")
	}
}

// P3：fold 系 helper 对 title 必须**真的转义**。
//
// 为什么这是独立的第三条判据：esc 自身已由 TestEscBehavesOnAllFiveChars 守住，
// 但「esc 正确」与「fold 用了 esc」是两件事 —— M5 恰恰是把 esc(title) 换成 title：
// esc 行为照旧完美、五个实体字面量照旧全场在场、三份照旧一致 ⇒ 只有真跑 fold 才看得见。
// 断言双向：
//  1. 产出必须含期望实体串（M5 产出原始标签 ⇒ 不含，判红）；
//  2. 产出不得含原始标题（原样透传的直接证据，不依赖实体表内容）；
//  3. 第二枚探针（单引号 & 双引号 < > 连排）钉住实体**对应关系** ⇒
//     M12 那种「左右尖括号实体对调」也判红，而它的五个实体字面量全都还在场。
func TestFoldHelpersEscapeTitle(t *testing.T) {
	node := helperRequireNode(t)
	helperCheckBlobsDistinct(t, helperBlobNames)
	// 两枚探针标题与期望产物全部用 rune 拼（纪律 d：Go 源码里不出现带引号的 HTML/JS 字面量）。
	t1 := helperLT + "img src=x onerror=1" + helperGT
	w1 := helperFoldWant
	t2 := helperAPO + helperAMP + helperQUO + helperLT + helperGT
	hash := string(rune(35))
	w2 := helperAMP + hash + "39;" + helperAMP + "amp;" + helperAMP + "quot;" + helperAMP + "lt;" + helperAMP + "gt;"
	for _, s := range []string{t1, t2, w1, w2, helperFoldBody} {
		if s == "" {
			t.Fatal("探针、期望值或正文标记是空串 ⇒ 后面会比空气，判据恒绿")
		}
	}
	failed := false
	total := 0
	for _, name := range helperBlobNames {
		size := helperFloor(t, name)
		js := helperBlobOf(name)
		// esc 片段抽取自证：必须含 const esc 定义（app/pool 还须含 fromCharCode(39) 那行动态补齐）。
		// 直接复用 esc_behavior_test.go 的 escSnippet，避免两处抽取口径漂移。
		escRegion, escLines := escSnippet(js)
		if escRegion == "" || !strings.Contains(escRegion, "const esc =") {
			t.Fatalf("%s 抽不到 esc 片段（抽到 %d 行）⇒ fold 转义无从判起，先修抽取再谈缺陷", name, escLines)
		}
		if !strings.Contains(escRegion, string(rune(39))) && name != "govJS" {
			t.Errorf("%s 的 esc 片段里没有单引号字符 ⇒ 可能漏抽动态补齐那一行（纪律 b 的假红源头），先看行数再判缺陷", name)
			failed = true
		}
		host, ok := helperFoldHost[name]
		if !ok {
			t.Errorf("没有登记 %s 对应的页面标记 ⇒ fold 产物的 innerHTML 落点没被守住", name)
			failed = true
		} else if !strings.Contains(host, "innerHTML") && !strings.Contains(js, "innerHTML") {
			t.Errorf("%s 与 %s 里都找不到 innerHTML 落点 ⇒ 转义断言失去危害面（受理面已变，需改写判据）", name, "对应页面标记")
			failed = true
		}
		helpers := helperFoldHelpers(t, js, name)
		if len(helpers) == 0 {
			failed = true
			continue
		}
		for _, h := range helpers {
			tag, line := h[0], h[1]
			total++
			if !strings.HasSuffix(line, ";") {
				t.Errorf("%s 的定义行不以分号收尾（尾部 %q）⇒ 抽取只截到半行，不许据此判转义（纪律 b）", tag, helperClip(line, 44))
				failed = true
				continue
			}
			parts := strings.Split(strings.Split(tag, "/")[1], ":")
			fname := parts[0]
			out, err := helperRunNode(t, node, "P3_"+helperSafeFile(tag), helperP3Harness(escRegion, line, fname, []string{t1, t2}, helperFoldBody))
			if err != nil {
				t.Errorf("%s 的 fold 行为判据跑不起来（%v）｜esc 片段 %d 行｜输出：%s", tag, err, escLines, out)
				failed = true
				continue
			}
			cells, uerr := helperDecodeArray(out)
			if uerr != nil || len(cells) != 3 {
				t.Errorf("%s 输出无法解析成 3 项（err=%v）：%q", tag, uerr, out)
				failed = true
				continue
			}
			if cells[0] != "function" {
				t.Errorf("%s 运行期不是函数（实得 %q）⇒ 定义没进 harness，本条什么都没测", tag, cells[0])
				failed = true
				continue
			}
			t.Logf("%-26s 自证：blob %d 字节｜esc 片段 %d 行｜探针1 产出 %q", tag, size, escLines, helperClip(cells[1], 150))
			for k, pair := range [][2]string{{t1, w1}, {t2, w2}} {
				raw, want := pair[0], pair[1]
				got := cells[k+1]
				if !strings.Contains(got, want) {
					t.Errorf("%s 对标题 %q 的产出里找不到期望转义串 %q（实得 %q）⇒ 标题没被转义或实体对应错乱，可拼出注入（M5、M12 两类）", tag, raw, want, helperClip(got, 170))
					failed = true
				}
				if strings.Contains(got, raw) {
					t.Errorf("%s 的产出里出现原始标题 %q（实得 %q）⇒ title 原样进了 innerHTML，标签与属性上下文直接失守", tag, raw, helperClip(got, 170))
					failed = true
				}
			}
			if !strings.Contains(cells[1], helperFoldBody) {
				t.Errorf("%s 的产出里没有正文标记 %q ⇒ 拼接本身坏了，转义断言不可信", tag, helperFoldBody)
				failed = true
			}
			helperCommentImmunity(t, stripLineComments(line), helperDefectRawTitle)
		}
	}
	if !failed {
		t.Logf("%d 个 fold 系 helper（三份各测一遍）真产出了转义后的标题（跑出来的，不是 Contains 断言的）", total)
	} else {
		t.Logf("（不打印成功结论：本轮已记录失败 ⇒ 成功话术由失败状态门控，元发现 24）")
	}
}
