package web

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// 面板使用计数（round 161 · T113）。
//
// 为什么需要（一手取证，非推测）：本会话连着两轮想回答「这两个页面真被人打开过吗」都取不到证据 ——
//   · audit.jsonl 165,211 行里 actor 只有 engine / autonomy / system / user，**没有 panel 这种取值**；
//     176 条 actor=user 全是 cancel+transition，其中今日 6 条实为我部署 --quit 的副作用（不是人点面板）；
//   · withMiddleware(实测 server.go:183) 只做回环与 Origin 校验、不记访问；guard(实测 server.go:276) 只验 token、不记访问；
//     全仓 grep accessLog / logRequest / 访问计数 = 0 处。
// ⇒ 一个 7x24 无人值守工作台，**界面是否被真人使用过完全不可知**。后果不是"少个统计"，而是排期没有依据：
//   面板 JS 里的重绘保态、表单不被 15 秒清空（C3/C4）这类改动，永远只能停在「机制成立、伤害率未取证」，
//   既不能据实投入，也不能据实否决。本文件把这条证据补齐。
//
// 设计约束（四条都来自本仓已登记的事故，不是偏好）：
//   ① 只落一条 Info、每页每进程一次（seen 去重）：withMiddleware 那圈中间件在每次资源请求都会跑，
//      若在那里逐请求打日志，就是 engine.go:116 记过的「audit.jsonl 一夜涨到 2.3MB」同型事故；
//   ② 绝不记 token：/admin 的 URL 带 ?token=，日志会落盘。logging.Redact 虽会按键名脱敏，
//      但不能把安全寄托在"键名恰好命中脱敏名单"上 ⇒ 干脆不取 URL 查询串，只记 path 与 UA；
//   ③ UA 是**攻击者可控字符串**：截到 80 字节并把 CR/LF 换成下划线 —— 否则一行日志能被伪造出
//      后续整行（日志注入），甚至伪造出「有人用浏览器打开过面板」这条结论本身；
//   ④ 排除自动化与资源型 UA（HeadlessChrome / python-requests / curl / go-http-client / PowerShell）：
//      本仓自己的 CDP 探针（cmd/workbench/ui_probe.go）和测试脚本都会访问面板，不过滤的话
//      计数器会把「我自己跑过探针」报成「老大打开过面板」—— 观测者效应必须显式处理。

// panelVisit 是一个面板页的访问计数快照。
type panelVisit struct {
	path    string
	count   int
	firstUA string
	firstAt int64
}

// panelVisits 保存各页的"首次访问时刻 + 累计次数"。零值可用，无需构造。
type panelVisits struct {
	mu   sync.Mutex
	seen map[string]*panelVisit
}

// autoUASubstrings 是"这不是真人"的判据片段（小写比较）。
// 名单只收**线上实测出现过**的形态（部署后从 workbench.jsonl 的 ua 字段直接取，不是我猜的）：
//
//	WindowsPowerShell/5.1.26100.916（liveness 探针） ｜ curl/8.22.0（Lead 的命令行探针） ｜ node（run_code 探针）。
//
// 原先我写过两个**没实测过**的条目：一个是 Windows 外壳工具的请求类名（它恰好命中本仓无人值守静态门禁
// —— tests/check_noninteractive.ps1 会扫 *.go 里的字符串字面量，理由写在它 :59-62：PS 缺陷不必在 .ps1 里），
// 另一个是某个假想的微软客户端串。⇒ 两条都删；Windows 外壳的真实 UA 含 WindowsPowerShell，
// 由下面 "powershell" 一条覆盖（大小写不敏感 ⇒ 同一规则也覆盖 WindowsPowerShell）。
var autoUASubstrings = []string{
	"headlesschrome", "headless", "go-http-client", "curl/", "python-requests",
	"powershell", "wget/",
	"phantomjs", "selenium", "puppeteer", "playwright",
	"node.js",
}

// exactAutoUA 是**整串相等**才算自动件的 UA：本仓自己的 run_code/探针经 fetch 发出的 UA 就是字面量 "node"。
// 之所以不用 Contains("node")：真人 UA 里可能含 "…Node…" 这类词（例如某些内网客户端/插件串），
// 子串匹配会误伤真人；而 "node" 作为完整 UA 只可能是 Node 运行时本身。
//
// 这条是**线上实测抓出来的**（round 163）：部署后我用 node 探针复验 firstAt，结果 /admin 的首次访问
// 被记成 firstHuman=true —— 也就是本功能要防的"伪造人类使用"被我自己触发了。修复前它会把
// "Lead 跑了个脚本"显示成"有人打开了管理面板"。
var exactAutoUA = []string{"node"}

func isExactAutoUA(ua string) bool {
	low := strings.ToLower(strings.TrimSpace(ua))
	for _, s := range exactAutoUA {
		if low == s {
			return true
		}
	}
	return false
}

// isAutomatedUA 判断 UA 是否来自自动件。空 UA 按自动处理：真人浏览器一定会带 UA，
// 空值只可能是刻意抹掉或裸 HTTP 客户端 —— 宁可少记，不可把机器行为记成人的行为。
func isAutomatedUA(ua string) bool {
	if strings.TrimSpace(ua) == "" {
		return true
	}
	if isExactAutoUA(ua) {
		return true
	}
	low := strings.ToLower(ua)
	for _, s := range autoUASubstrings {
		if strings.Contains(low, s) {
			return true
		}
	}
	return false
}

// cleanUAForLog 把 UA 压成可安全入日志的短串：去 CR/LF（防日志注入伪造后续行）、
// 截到 80 字节（防一行被塞进超长内容）、并去掉制表符以外的连续空白。
func cleanUAForLog(ua string) string {
	s := strings.ReplaceAll(ua, "\r", "_")
	s = strings.ReplaceAll(s, "\n", "_")
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// note 记录一次页面访问，返回 (本次累计次数, 是否首次, 清洗后的 UA 是否像真人)。
// 计数与"是否首次"一起返回，调用方据此决定要不要打那条一次性日志。
func (p *panelVisits) note(path, ua string) (count int, first bool, human bool, at int64) {
	human = !isAutomatedUA(ua)
	sanitized := cleanUAForLog(ua)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.seen == nil {
		p.seen = map[string]*panelVisit{}
	}
	v := p.seen[path]
	if v == nil {
		// firstAt 必须在首次那次就填：线上实测（curl + PowerShell 探针）暴露过它恒为 0 ——
		// 单测当时断言了 path/human/count/ua，唯独没断 firstAt ⇒ 字段一路是零值。
		// 教训：结构体里"有字段"不等于运行时"字段有值"；单测必须逐个字段断言，不能只断好写的。
		v = &panelVisit{path: path, firstUA: sanitized, firstAt: time.Now().UnixMilli()}
		p.seen[path] = v
		first = true
	}
	v.count++
	return v.count, first, human, v.firstAt
}

// snapshot 返回各页计数的副本（供 /api/status 暴露；不暴露内部指针，避免调用方改到状态）。
func (p *panelVisits) snapshot() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]any, len(p.seen))
	for k, v := range p.seen {
		out[k] = map[string]any{
			"count":      v.count,
			"firstAt":    v.firstAt,
			"firstUA":    v.firstUA,
			"firstHuman": !isAutomatedUA(v.firstUA),
		}
	}
	return out
}

// total 返回所有页累计访问次数（判据与面板都用得上的单一读数）。
func (p *panelVisits) total() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, v := range p.seen {
		n += v.count
	}
	return n
}

// recordVisit 是四个页面 handler 的统一入口：记一笔，并且只在**每页首次**落一条 Info。
// logger 允许为 nil（未注入时用 logging.Nop 语义，emit 内已有 nil 保护）⇒ 单测与嵌入式调用都不会炸。
func (s *Server) recordVisit(r *http.Request) {
	count, first, human, at := s.visits.note(r.URL.Path, r.UserAgent())
	if !first {
		return
	}
	// 直接在 s.log 上调：logging.emit 首行即 `if l == nil || l.inner == nil { return }`
	// （internal/logging/logging.go 的 emit 开头）⇒ 未注入日志器（单测、CLI 子命令）时是安全空操作，
	// 不会炸也不会假装记过了。判据侧要断言这条，必须自己注入 Handler 捕获（panel_visits_test.go）。
	s.log.Info("面板页面首次被访问",
		"component", "panel-visit",
		"path", r.URL.Path,
		"ua", cleanUAForLog(r.UserAgent()),
		"human", human,
		"count", count,
		"firstAt", at)
}
