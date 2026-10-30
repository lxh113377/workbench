package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestAuditReportCitationsResolve 钉住「报告的每条代码位置引用都必须逐字命中那一行」（round 130 · 元发现的机器化）。
//
// 为什么必须有这条（本轮现场）：我把报告写完并过了内容门禁后，用 node 逐条对账引用，
// 结果 **10 条里 8 条行号是错的** —— 不是当初写错，而是我随后几轮持续编辑 engine.go/manager.go，
// 把行号改了而报告没跟着改。这类漂移对读者的伤害比"少写一节"更隐蔽：行号看着精确、点开是别的代码，
// 而内容级门禁（三节/证据/分级）完全看不出来 —— 它只判"有没有 file:line 这种形状"，不判"对不对"。
//
// 判据形态：报告里形如  <路径>:<行号>   <代码片段>  的引用，要求该片段出现在**那一行 ±0 行**上。
// 为什么要求精确到行而不是 ±3：我最初用 ±3 的宽容口径去查，结果把"行号漂 50 多行"全放过了
// （报告第 31 行写 1706、真实位置 1763 —— ±3 判不出来）。宽容口径的判据等于没判（本会话第 N 次同构教训）。
func TestAuditReportCitationsResolve(t *testing.T) {
	text := string(readAuditReport(t))
	// 引用形态：路径 : 行号，后面同一行跟一段代码（用行号后的空白/制表分隔）
	re := regexp.MustCompile(`(?m)^\s*(?:workbench/)?((?:internal|docs|tests|setup)[^\s:]*\.go):(\d+)\s+(.+?)\s*$`)
	matches := re.FindAllStringSubmatch(text, -1)
	if len(matches) < 8 {
		t.Fatalf("只解析到 %d 条代码引用，口径可疑（判据可能被空集/正则失配蒙过）", len(matches))
	}
	for _, m := range matches {
		rel, lineStr, claim := m[1], m[2], m[3]
		line, aerr := strconv.Atoi(lineStr)
		if aerr != nil {
			t.Fatalf("行号解析失败 %q: %v", lineStr, aerr)
		}
		// 片段可能带尾部箭头说明（如 "…   ← 无 kind"），先截掉箭头之后的中文注释
		if k := strings.Index(claim, "←"); k >= 0 {
			claim = strings.TrimSpace(claim[:k])
		}
		// 取**最长**的那个 token 作核对锚，而不是第一个词。
		// 负控实测暴露：取第一个词时 "const poolWaitBudget = 4" 变成只查 "const" ——
		// 而 engine.go 里几十行都含 "const"（连注释都含），漂移到任意一行都可能被判绿，
		// 判据强度形同虚设。最长 token（如 poolWaitBudget / laneOneStrike）才是有区分度的锚。
		snip := longestToken(claim)
		if snip == "" {
			continue // 没有可核对的代码 token（纯说明行）
		}
		// go test 的 cwd = 包目录（workbench/internal/engine），上跳两级即模块根 workbench/；
		// 第一版多拼了一层 "workbench" ⇒ 全部报"文件不存在"，是判据自身写错，不是报告写错。
		p, err := filepath.Abs(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", rel, err)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("报告引用了不存在的文件：%s（%s:%d）", rel, rel, line)
			continue
		}
		lines := strings.Split(string(raw), "\n")
		if line < 1 || line > len(lines) {
			t.Errorf("行号越界：%s:%d（该文件共 %d 行）", rel, line, len(lines))
			continue
		}
		if !strings.Contains(lines[line-1], snip) {
			// 报错要给出"该锚点的真实行号"，方便一次改对而不是回头再查
			real := -1
			for i, l := range lines {
				if strings.Contains(l, snip) {
					real = i + 1
					break
				}
			}
			t.Errorf("报告引用漂移：%s:%d 声称含 %q，实际该行是 %q；该片段真实位置=%s:%d",
				rel, line, snip, strings.TrimSpace(lines[line-1]), rel, real)
		}
	}
}

// longestToken 取字符串里最长的、具标识符形态的词（长度 >=4），作为引用对账的锚。
func longestToken(s string) string {
	best := ""
	for _, w := range strings.Fields(s) {
		w = strings.Trim(w, "`.,;:()[]{}")
		if len(w) < 4 || strings.HasPrefix(w, "//") {
			continue
		}
		if !codeTokenRe.MatchString(w) {
			continue
		}
		if len(w) > len(best) {
			best = w
		}
	}
	return best
}

// codeTokenRe 认标识符/简单表达式形态的词（容忍 . () [] = 等尾巴）。
var codeTokenRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\[\]()=<>!&|*+/-]{3,}$`)
