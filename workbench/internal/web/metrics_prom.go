package web

// Prometheus 文本格式（exposition format v0.0.4）指标导出。
//
// 对标改进项 P1-3：/api/metrics 是面板专用的 JSON 读数，外部监控系统接不了；
// 本文件把同一批事实（任务台账 + 失败分类 + 进程资源）再投影成
// Prometheus 可以直接抓取的文本格式，不新增任何第三方依赖。
//
// 三条硬约束（都写在结构里，而不是靠人自觉）：
//  1. 每条指标必须有 # HELP 与 # TYPE —— 由 promExposition.family() 保证：
//     样本只能挂在已登记的家庭上，登记时 HELP/TYPE 一起写，无法漏掉一半。
//  2. 浮点不得出现 NaN / +Inf / -Inf —— 由 promSanitizeFloat 兜底为 0。
//  3. 标签值绝不破坏行结构 —— 由 promEscapeLabelValue 处理：
//     反斜杠、双引号、换行按规范转义；其余控制字符（回车、制表、NUL）替换为空格，
//     非法 UTF-8 先修正，超长值按 UTF-8 边界截断。

import (
	"fmt"
	"math"
	"net/http"
	"runtime"
	rtmetrics "runtime/metrics"
	"sort"
	"strconv"
	"strings"

	"workbench/internal/model"
)

// promContentType 是 Prometheus 文本 exposition 的标准 Content-Type。
const promContentType = "text/plain; version=0.0.4; charset=utf-8"

// 指标名（统一 workbench_ 前缀，避免与采集端其他 job 的指标撞名）。
const (
	promNameTasksTotal    = "workbench_tasks_total"
	promNameAttemptsTotal = "workbench_attempts_total"
	promNameFailuresTotal = "workbench_task_failures_total"
	promNameDuration      = "workbench_task_duration_seconds"
	promNameGoroutines    = "workbench_goroutines"
	promNameHeapAlloc     = "workbench_heap_alloc_bytes"
	promNameGCPauseTotal  = "workbench_gc_pause_total_seconds"
)

// promTaskStates 是 model 包的全部合法状态（来自 model.TaskState 枚举）。
// 零任务时每个状态都出一条 0 值序列，保证采集端在任务表为空时也能建出面板，
// 而不是等 series 第一次出现才"凭空多一条线"。
var promTaskStates = []string{
	string(model.StateCandidate),
	string(model.StateEvaluating),
	string(model.StateRejected),
	string(model.StateApproved),
	string(model.StateQueued),
	string(model.StateRunning),
	string(model.StateDone),
	string(model.StateFailed),
	string(model.StateTimeout),
	string(model.StateCancelled),
	string(model.StateInterrupted),
	string(model.StateDegraded),
}

// promFailureKindsBaseline 是 executor.Kind 的非空枚举（含 unknown）。
// 同样零值预置：错误分类进指标（对标 D2）的意义是"能按类别告警"，
// 类别没发生时序列必须存在且为 0，否则 rate()/increase() 无从判定。
var promFailureKindsBaseline = []string{
	"timeout", "missing_credential", "model_unavailable", "upstream_failed",
	"boot_failed", "bad_exit", "cancelled", "stalled", "rate_limited",
	"transport_failed", "stream_cut", "unknown",
}

// promAttemptsScanMax 是尝试台账的扫描上限（条数）。
// 台账是 tasks/<id>/attempts.jsonl，每次抓取都要按任务读文件；
// 给上限是为了让"抓取"这个动作的成本有上界（面板一次 scrape 不该变成全站 IO 扫描）。
const promAttemptsScanMax = 20000

// promLabelMaxBytes 是单个标签值的字节上限（按 UTF-8 边界截断）。
const promLabelMaxBytes = 128

// promFamily 是一条指标族：HELP/TYPE 头 + 若干样本行。
type promFamily struct {
	name    string
	typ     string
	help    string
	samples []string
}

// promExposition 是文本输出累加器。
type promExposition struct {
	order []*promFamily
	index map[string]*promFamily
}

func newPromExposition() *promExposition {
	return &promExposition{index: map[string]*promFamily{}}
}

// family 登记一条指标族并立刻写出 # HELP 与 # TYPE。
// 重复登记同一个名字返回同一个族（样本继续追加），因此不可能出现
// "有样本无 TYPE"或"TYPE 出现两次"这两种破坏 exposition 结构的情况。
func (e *promExposition) family(name, typ, help string) *promFamily {
	if f, ok := e.index[name]; ok {
		return f
	}
	f := &promFamily{name: name, typ: typ, help: promCleanHelpText(help)}
	e.index[name] = f
	e.order = append(e.order, f)
	return f
}

// add 追加一个样本（族名本身）。labels 为 nil/空时输出无标签形式。
func (f *promFamily) add(labels [][2]string, value string) {
	f.addNamed(f.name, labels, value)
}

// addNamed 追加一个**带后缀**的样本（summary 族必需的 _sum/_count 形态）。
//
// 为什么必须有这个：改造前 summary 族用两次 add() 输出"求和"与"计数"，
// 于是同一族出现两条**同名同标签**的样本行 —— Prometheus 解析器按
// (name, labels) 唯一标识序列，重复行直接判 "duplicate sample for timestamp"
// 让整次抓取作废（本地看是"少一条指标"，实则是整份文本不可用）。
// 规范做法是 summary 必须拆成 <name>_sum 与 <name>_count。
func (f *promFamily) addNamed(full string, labels [][2]string, value string) {
	var b strings.Builder
	b.WriteString(full)
	if len(labels) > 0 {
		b.WriteByte('{')
		for i, lb := range labels {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(lb[0])
			b.WriteByte('=')
			b.WriteByte('"')
			b.WriteString(promEscapeLabelValue(lb[1]))
			b.WriteByte('"')
		}
		b.WriteByte('}')
	}
	b.WriteByte(' ')
	b.WriteString(value)
	b.WriteByte('\n')
	f.samples = append(f.samples, b.String())
}

// addInt 追加整数样本（永远合法，不经浮点）。
func (f *promFamily) addInt(labels [][2]string, v int64) {
	f.add(labels, strconv.FormatInt(v, 10))
}

// addFloat 追加浮点样本，NaN/±Inf 一律降级为 0（见 promSanitizeFloat）。
func (f *promFamily) addFloat(labels [][2]string, v float64) {
	buf := make([]byte, 0, 32)
	buf = strconv.AppendFloat(buf, promSanitizeFloat(v), 'f', -1, 64)
	f.add(labels, string(buf))
}

// String 渲染完整文本。没有样本的族仍然输出 HELP/TYPE（合法：空族允许）。
func (e *promExposition) String() string {
	var b strings.Builder
	for _, f := range e.order {
		fmt.Fprintf(&b, "# HELP %s %s\n", f.name, f.help)
		fmt.Fprintf(&b, "# TYPE %s %s\n", f.name, f.typ)
		for _, s := range f.samples {
			b.WriteString(s)
		}
	}
	return b.String()
}

// promSanitizeFloat 把非法浮点收敛为 0。
// Prometheus 文本格式没有 NaN/Inf 的字面量表示（只有 +Inf 的例外写法），
// 一旦写出会让整个 scrape 解析失败——所以宁可报 0 也不污染整份文本。
func promSanitizeFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// promCleanHelpText 去掉会破坏行结构的字符。
// HELP 是单行注释：换行会把一条 HELP 劈成两行，反斜杠在部分解析器里是转义起点。
func promCleanHelpText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.NewReplacer("\\", "/", "\"", "'", "\n", " ", "\r", " ", "\t", " ").Replace(s)
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// promEscapeLabelValue 按 exposition 规范转义标签值。
//
// 规范只承认三种转义：\\（反斜杠）、\"（引号）、\n（换行）。
// 因此其余控制字符不能"转义"（写 \r 会被解析器当非法转义），只能替换成空格。
// 这样无论输入多脏，产出的引号内绝不出现裸换行/裸引号/反斜杠歧义，行结构不破。
func promEscapeLabelValue(s string) string {
	s = model.Truncate(strings.ToValidUTF8(s, "�"), promLabelMaxBytes)
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString("\\\\")
		case r == '"':
			b.WriteString("\\\"")
		case r == '\n':
			b.WriteString("\\n")
		case r == '\r' || r == '\t' || r < 0x20 || r == 0x7f:
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// promLabelKey 校验标签名：非 [a-zA-Z_][a-zA-Z0-9_]* 的一律丢弃该标签（而不是丢弃样本），
// 保证"少一个标签"优于"产出一行非法文本"。
func promLabelKey(k string) string {
	if k == "" {
		return ""
	}
	for i, r := range k {
		valid := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9' && i > 0)
		if !valid {
			return ""
		}
	}
	return k
}

// promLabeledCounts 把「标签值 → 计数」渲染成升序样本行（键按标签值排序，输出可复现）。
func promLabeledCounts(f *promFamily, baseline []string, counts map[string]int64) {
	keys := make([]string, 0, len(counts))
	seen := map[string]bool{}
	for _, k := range baseline {
		e := promEscapeLabelValue(k)
		if seen[e] {
			continue
		}
		seen[e] = true
		keys = append(keys, k)
		if _, ok := counts[k]; !ok {
			counts[k] = 0
		}
	}
	for k := range counts {
		e := promEscapeLabelValue(k)
		if seen[e] {
			continue
		}
		seen[e] = true
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		f.add([][2]string{{"label", k}}, "")
	}
}

// ── 采集 ──────────────────────────────────────────────────────────

// promDomainSnapshot 是一次抓取读到的领域侧事实。
type promDomainSnapshot struct {
	states     map[string]int64
	kinds      map[string]int64
	attempts   int64
	attemptsOK bool // 台账是否扫完（未触顶时为 true）
	durSumMs   int64
	durCount   int64
}

// collectPromDomain 从引擎台账取数（engine 未注入时返回空快照，不 panic）。
func (s *Server) collectPromDomain() promDomainSnapshot {
	snap := promDomainSnapshot{
		states: map[string]int64{},
		kinds:  map[string]int64{},
	}
	if s == nil || s.engine == nil {
		return snap
	}
	store := s.engine.Store()
	if store == nil {
		return snap
	}
	tasks := store.List()
	scanned := 0
	for _, t := range tasks {
		if t == nil {
			continue
		}
		state := string(t.State)
		if strings.TrimSpace(state) == "" {
			state = "none"
		}
		snap.states[state]++
		if t.ElapsedMs > 0 {
			snap.durSumMs += t.ElapsedMs
			snap.durCount++
		}
		if scanned < promAttemptsScanMax {
			scanned++
			if recs, err := store.ListAttempts(t.ID); err == nil {
				snap.attempts += int64(len(recs))
			}
		}
	}
	snap.attemptsOK = scanned == len(tasks)
	// 失败分类与 /api/failures 同源（任务级终态 + ErrorKind），不在这里另立口径。
	for _, b := range s.engine.FailureBreakdown() {
		kind := strings.TrimSpace(b.Kind)
		if kind == "" {
			kind = "unknown"
		}
		snap.kinds[kind] += int64(b.Count)
	}
	return snap
}

// collectPromProcess 取进程侧读数（runtime + runtime/metrics，纯标准库）。
func collectPromProcess() (goroutines int64, heapBytes int64, gcPauseSeconds float64) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	goroutines = int64(runtime.NumGoroutine())
	heapBytes = int64(ms.HeapAlloc)
	gcPauseSeconds = float64(ms.PauseTotalNs) / 1e9

	// runtime/metrics 优先（官方口径，承诺不产 NaN/Inf）；取不到就保留 MemStats 读数。
	samples := []rtmetrics.Sample{
		{Name: "/sched/goroutines:goroutines"},
		{Name: "/memory/classes/heap/objects:bytes"},
	}
	rtmetrics.Read(samples)
	for _, sm := range samples {
		if sm.Value.Kind() != rtmetrics.KindUint64 {
			continue
		}
		switch sm.Name {
		case "/sched/goroutines:goroutines":
			goroutines = int64(sm.Value.Uint64())
		case "/memory/classes/heap/objects:bytes":
			heapBytes = int64(sm.Value.Uint64())
		}
	}
	return goroutines, heapBytes, gcPauseSeconds
}

// renderPrometheus 生成 exposition 文本（独立成函数便于单测直接调用）。
func (s *Server) renderPrometheus() string {
	dom := s.collectPromDomain()
	e := newPromExposition()

	states := e.family(promNameTasksTotal, "gauge",
		"Current number of WorkBench tasks per state, read from the task ledger on this scrape. Snapshot by state, therefore a gauge and not a counter: a task moving out of a state decrements that series. Every legal model.TaskState is emitted, zero-valued when no task is in it.")
	promLabeledSeries(states, promTaskStates, "state", dom.states)

	attempts := e.family(promNameAttemptsTotal, "counter",
		"Total execution attempts recorded in the per-task attempts ledger (tasks/ID/attempts.jsonl). Not strictly monotonic: directory retention can prune old task folders, so treat decreases as data loss rather than a process restart.")
	attempts.addInt(nil, dom.attempts)

	failures := e.family(promNameFailuresTotal, "counter",
		"Tasks in terminal failed or timeout state grouped by ErrorKind. Same source and same grouping as GET /api/failures, counted per task rather than per attempt so retries do not inflate it. Baseline kinds are emitted zero-valued (executor.Kind enum plus unknown).")
	promLabeledSeries(failures, promFailureKindsBaseline, "kind", dom.kinds)

	dur := e.family(promNameDuration, "summary",
		"Task duration in seconds as sum and count over tasks whose ElapsedMs is recorded. ElapsedMs is written only when a run archives successfully, so both sum and count cover successful runs and exclude failed, timeout and cancelled tasks; count is therefore a task count, not an attempt count.")
	// summary 族必须用 _sum / _count 两条不同名的序列（见 addNamed 注释）。
	dur.addNamed(promNameDuration+"_sum", nil, promFormatSeconds(dom.durSumMs))
	dur.addNamed(promNameDuration+"_count", nil, strconv.FormatInt(dom.durCount, 10))

	gor := e.family(promNameGoroutines, "gauge",
		"Number of live goroutines in this process, from runtime.NumGoroutine cross-checked with the runtime metric /sched/goroutines:goroutines.")
	procGoroutines, procHeap, procGCPause := collectPromProcess()
	gor.addInt(nil, procGoroutines)

	heap := e.family(promNameHeapAlloc, "gauge",
		"Bytes of heap memory occupied by live objects plus objects not yet swept by the garbage collector, from the runtime metric /memory/classes/heap/objects:bytes (equivalent to runtime.MemStats HeapAlloc).")
	heap.addInt(nil, procHeap)

	gc := e.family(promNameGCPauseTotal, "counter",
		"Cumulative stop-the-world garbage collection pause time since process start, in seconds, from runtime.MemStats PauseTotalNs. Monotonic for the life of the process.")
	gc.addFloat(nil, procGCPause)

	return e.String()
}

// promFormatSeconds 把毫秒和渲染成秒字符串（整数除法避免长小数噪声）。
func promFormatSeconds(ms int64) string {
	buf := make([]byte, 0, 32)
	buf = strconv.AppendFloat(buf, promSanitizeFloat(float64(ms)/1000), 'f', -1, 64)
	return string(buf)
}

// promLabeledSeries 输出一条带标签计数族：baseline 里的值补 0，实测值覆盖，
// 键按标签值字典序输出（抓取结果可复现，diff 友好）。
func promLabeledSeries(f *promFamily, baseline []string, label string, counts map[string]int64) {
	if counts == nil {
		counts = map[string]int64{}
	}
	key := promLabelKey(label)
	// 按转义后的标签值归并：两个不同的脏输入转义后可能撞成同一个值，
	// 若不归并就会输出两条完全相同的序列（Prometheus 视为重复样本）。
	merged := map[string]int64{}
	var keys []string
	add := func(raw string) {
		esc := promEscapeLabelValue(raw)
		if _, ok := merged[esc]; ok {
			return
		}
		merged[esc] = counts[raw]
		keys = append(keys, esc)
	}
	for _, b := range baseline {
		add(b)
	}
	for raw := range counts {
		add(raw)
	}
	sort.Strings(keys)
	for _, esc := range keys {
		if key == "" {
			f.add(nil, strconv.FormatInt(merged[esc], 10))
			continue
		}
		f.add([][2]string{{key, esc}}, strconv.FormatInt(merged[esc], 10))
	}
}

// handleMetricsProm 输出 Prometheus 文本格式指标（GET /metrics）。
//
// 与 /api/metrics 一样挂在 s.guard 下（见 routes）：本服务只监听回环，
// 但整个 API 面统一要求 token，指标面不该成为例外。采集端用
// metrics_path: /metrics?token=... 或 params 传 token。
func (s *Server) handleMetricsProm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body := s.renderPrometheus()
	w.Header().Set("Content-Type", promContentType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(body))
}
