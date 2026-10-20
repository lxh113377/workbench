package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"workbench/internal/model"
)

// promSampleRe 是一条合法样本行的完整形态：指标名 + 可选标签 + 数值。
var promSampleRe = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*(\{[^\r\n]*\})? [-+]?(\d+(\.\d+)?([eE][-+]?\d+)?)$`)
var promNameRe = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

// TestMetricsPromExpositionIsValid 是 P1-3 的主判据：**逐行**校验输出符合 Prometheus
// text exposition format，而不是"包含某个字符串"的弱断言 —— 弱断言放得过两类真实破坏：
// ① 写了 HELP 但漏了 TYPE；② 标签值里塞进裸换行，把一行样本劈成两行。
func TestMetricsPromExpositionIsValid(t *testing.T) {
	srv, _, st := newTestServer(t)
	seed := func(id, kind string) {
		task := &model.Task{ID: id, Title: "指标样本", State: model.StateCandidate, OutputFormat: "md", MaxAttempts: 2, Domain: "content"}
		if err := st.Create(task); err != nil {
			t.Fatal(err)
		}
		for _, to := range []model.TaskState{model.StateApproved, model.StateQueued, model.StateRunning, model.StateFailed} {
			if _, err := st.Transition(id, to, "test", ""); err != nil {
				t.Fatalf("%s -> %s: %v", id, to, err)
			}
		}
		if kind != "" {
			if _, err := st.Update(id, func(x *model.Task) error { x.ErrorKind = kind; return nil }); err != nil {
				t.Fatal(err)
			}
		}
	}
	seed("task-prom-ok", "")
	seed("task-prom-timeout", "timeout")
	seed("task-prom-transport", "transport_failed")

	rec := doPromRequest(t, srv, http.MethodGet)
	body := rec.Body.String()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, 应为 text/plain 前缀", ct)
	}
	validateProm(t, body)

	for _, name := range promExpectedNames {
		if !strings.Contains(body, "# HELP "+name+" ") || !strings.Contains(body, "# TYPE "+name+" ") {
			t.Fatalf("指标 %s 缺少 HELP 或 TYPE 头：\n%s", name, promHead(body, 40))
		}
	}
	// 失败分类必须真的带上 kind 标签（D2「错误分类进指标」的落点，不是只有族头）
	for _, want := range []string{`workbench_task_failures_total{kind="timeout"} 1`, `workbench_task_failures_total{kind="transport_failed"} 1`} {
		if !strings.Contains(body, want) {
			t.Fatalf("缺少样本 %q：\n%s", want, promHead(body, 60))
		}
	}
}

// promSummaryFamilies 记录 TYPE=summary 的族，用于校验 _sum/_count 规范形态。
var promSummaryFamilies = []struct{ Name string }{{"workbench_task_duration_seconds"}}

// promExpectedNames 是必须在场的指标族。
var promExpectedNames = []string{"workbench_tasks_total", "workbench_attempts_total",
	"workbench_task_failures_total", "workbench_task_duration_seconds",
	"workbench_goroutines", "workbench_heap_alloc_bytes", "workbench_gc_pause_total_seconds",
	"workbench_rate_limit_per_second", "workbench_rate_limit_burst", "workbench_rate_limit_available_tokens"}

// TestMetricsPromZeroTaskStillValid 零任务场景（负面用例之一）：空台账也必须输出
// 合法文本，且不得出现 NaN/+Inf；每个合法状态都要有 0 值序列，采集端才能预先建面板。
func TestMetricsPromZeroTaskStillValid(t *testing.T) {
	srv, _, _ := newTestServer(t)
	body := doPromRequest(t, srv, http.MethodGet).Body.String()
	validateProm(t, body)
	for _, bad := range []string{"NaN", "+Inf", "-Inf"} {
		if strings.Contains(body, bad) {
			t.Fatalf("输出含非法浮点字面量 %s：\n%s", bad, promHead(body, 40))
		}
	}
	for _, s := range promTaskStates {
		want := fmt.Sprintf(`workbench_tasks_total{state=%q} 0`, s)
		if !strings.Contains(body, want) {
			t.Fatalf("零任务时缺少 0 值序列 %q", want)
		}
	}
}

// TestMetricsPromMethodNotAllowed 守护写方法拒绝：非 GET/HEAD 不得触发采集。
func TestMetricsPromMethodNotAllowed(t *testing.T) {
	srv, _, _ := newTestServer(t)
	if code := doPromRequest(t, srv, http.MethodPost).Code; code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /metrics 状态码 = %d, want 405", code)
	}
}

// TestPromEscapeLabelValueKeepsLineStructure 脏输入必测：换行、引号、反斜杠、制表、
// NUL、非法 UTF-8、超长值都不允许把一行样本劈成两行或造成引号歧义。
func TestPromEscapeLabelValueKeepsLineStructure(t *testing.T) {
	dirty := []string{
		"a\nb",
		`say "hi"`,
		"c:\\d",
		"tab\there",
		"nul\x00here",
		"cr\rhere",
		"\xff\xfe非法utf8",
		strings.Repeat("长", 200),
	}
	for i, in := range dirty {
		got := promEscapeLabelValue(in)
		if strings.ContainsAny(got, "\r\n") {
			t.Fatalf("#%d 转义后仍含换行（会劈开样本行）: %q", i, got)
		}
		if strings.ContainsRune(got, 0) {
			t.Fatalf("#%d 转义后仍有 NUL: %q", i, got)
		}
		if len(got) > 300 {
			t.Fatalf("#%d 未做长度上限: %d bytes", i, len(got))
		}
		// 结构判据（不是"不许出现引号"）：规范要求 " 必须转义成 \"，合法产物**可以**含引号，
		// 但每个引号前必须有反斜杠；反斜杠只允许出现在 \\ / \" / \n 三种转义里。写错的转义
		// 会让解析器把标签提前闭合、整行样本作废 —— 这才是要防的。
		if wfErr := promEscapedWellFormed(got); wfErr != nil {
			t.Fatalf("#%d 转义结果不是可解析的标签值: %v (%q)", i, wfErr, got)
		}
	}
}

// promEscapedWellFormed 检查转义串能否被 Prometheus 解析器无歧义读回。
func promEscapedWellFormed(s string) error {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case 92: // 反斜杠
			if i+1 >= len(s) {
				return fmt.Errorf("结尾是孤立反斜杠")
			}
			switch s[i+1] {
			case 92, 34, 110: // \  "  n
				i++
			default:
				return fmt.Errorf("非法转义序列（解析器会当字面量或报错）: %q", s[i+1])
			}
		case 34: // 双引号
			return fmt.Errorf("位置 %d 出现未转义的双引号（标签会被提前闭合）", i)
		}
	}
	return nil
}

// TestPromLabelKeyRejectsInvalidNames 守护标签名白名单：非法名丢弃该标签（而不是丢样本），
// 且绝不把非法字符写进输出。
func TestPromLabelKeyRejectsInvalidNames(t *testing.T) {
	for _, bad := range []string{"", "a-b", "1lead", "带中文", "with space", "a\nb"} {
		if got := promLabelKey(bad); got != "" {
			t.Fatalf("promLabelKey(%q) = %q, 应为空串", bad, got)
		}
	}
	if got := promLabelKey("kind_2"); got != "kind_2" {
		t.Fatalf("合法标签名被误拒: %q", got)
	}
}

// TestPromFamilyAlwaysWritesHelpAndType 结构判据：样本只能挂在已登记族上，重复登记必须
// 复用同一个族（TYPE 不得出现两次）。
func TestPromFamilyAlwaysWritesHelpAndType(t *testing.T) {
	e := newPromExposition()
	f1 := e.family("wb_demo", "gauge", "first help")
	f1.addInt(nil, 1)
	f2 := e.family("wb_demo", "gauge", "second help")
	if f1 != f2 {
		t.Fatal("重复登记同名族必须返回同一个族")
	}
	body := e.String()
	if n := strings.Count(body, "# TYPE wb_demo "); n != 1 {
		t.Fatalf("TYPE 行数 = %d, want 1: %q", n, body)
	}
	if n := strings.Count(body, "# HELP wb_demo "); n != 1 {
		t.Fatalf("HELP 行数 = %d, want 1: %q", n, body)
	}
}

// ── helpers ──────────────────────────────────────────────────

func doPromRequest(t *testing.T, srv *Server, method string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/metrics", nil)
	req.Header.Set("X-WB-Token", srv.Token())
	rec := httptest.NewRecorder()
	srv.handleMetricsProm(rec, req)
	if method == http.MethodGet && rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics 状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	return rec
}

// validateProm 逐行校验 exposition 合法性（主判据，不绑定具体指标名）。
func validateProm(t *testing.T, body string) {
	t.Helper()
	if body == "" {
		t.Fatal("输出为空")
	}
	seenHelp := map[string]bool{}
	seenType := map[string]bool{}
	dup := map[string]bool{}
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			t.Fatalf("第 %d 行是空行（行结构被破坏）", i+1)
		}
		if strings.HasPrefix(line, "#") {
			fields := strings.SplitN(line, " ", 4)
			if len(fields) < 3 {
				t.Fatalf("第 %d 行注释形态非法: %q", i+1, line)
			}
			name := fields[2]
			if !promNameRe.MatchString(name) {
				t.Fatalf("第 %d 行指标名非法: %q", i+1, line)
			}
			switch fields[1] {
			case "HELP":
				if seenHelp[name] {
					t.Fatalf("指标 %s 的 HELP 出现第二次", name)
				}
				seenHelp[name] = true
			case "TYPE":
				if !seenHelp[name] {
					t.Fatalf("指标 %s 有 TYPE 但没有 HELP（HELP 必须在先）", name)
				}
				if seenType[name] {
					t.Fatalf("指标 %s 的 TYPE 出现第二次", name)
				}
				seenType[name] = true
			default:
				t.Fatalf("第 %d 行是不认识的注释类型: %q", i+1, line)
			}
			continue
		}
		if !promSampleRe.MatchString(line) {
			t.Fatalf("第 %d 行不是合法样本行: %q", i+1, line)
		}
		name := line
		if k := strings.IndexAny(line, " {"); k >= 0 {
			name = line[:k]
		}
		// summary/histogram 的 _sum/_count/_bucket 后缀属于族的一部分：
		// TYPE 只声明在基名上（"# TYPE x summary"），因此必须先剥后缀再回基名查族。
		base := promStripSuffix(name)
		if !seenType[base] {
			t.Fatalf("样本 %q 挂在没有 TYPE 声明的族 %q 上（结构非法）", line, base)
		}
		// 序列唯一性判据：Prometheus 按 (name, labels) 唯一标识一条序列，
		// 同名同标签出现第二次会让整次抓取判 "duplicate sample for timestamp" ——
		// 这条正是上一版判据漏掉的真实缺陷（summary 族用两条同名样本输出求和与计数）。
		if dup[line] {
			t.Fatalf("重复序列（整次 scrape 会被判 duplicate sample）: %q", line)
		}
		dup[line] = true
	}
	// summary/histogram 族的规范形态判据
	for _, fam := range promSummaryFamilies {
		if !strings.Contains(body, fam.Name+"_sum ") && !strings.Contains(body, fam.Name+"_sum\n") {
			t.Fatalf("summary 族 %s 缺少 _sum 序列", fam.Name)
		}
		if !strings.Contains(body, fam.Name+"_count") {
			t.Fatalf("summary 族 %s 缺少 _count 序列", fam.Name)
		}
	}
	for name := range seenType {
		if !seenHelp[name] {
			t.Fatalf("指标 %s 有 TYPE 却无 HELP", name)
		}
	}
	if len(seenType) == 0 {
		t.Fatal("整份输出没有任何 TYPE 声明")
	}
}

// promStripSuffix 剥掉 summary/histogram 的官方后缀，得到 TYPE 声明所在的基名。
func promStripSuffix(name string) string {
	for _, sfx := range []string{"_bucket", "_sum", "_count"} {
		if strings.HasSuffix(name, sfx) && len(name) > len(sfx) {
			return strings.TrimSuffix(name, sfx)
		}
	}
	return name
}

func promHead(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
