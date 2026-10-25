// Package artifact 把模型的一次性文本输出解析成「可落盘的真实文件 + 可执行的验收命令」。
//
// 背景（本机实测）：our-free-model 的 sealed 车道在「工具调用后的第二步」会卡死，
// 因此工作台 profile 禁用了全部工具，模型只能一次性输出文本。这带来一个硬问题：
// 产出永远是 Markdown 作文，没有任何真实文件，也无法验证对错。
//
// 本包用一份输出协议弥合这个缺口：模型只需在一次回答里输出
//
//	<<<ARTIFACTS>>>
//	```manifest   -> {"files":[...],"verify":[...]}
//	```file:path  -> 文件完整内容
//	<<<END>>>
//
// 工作台负责落盘、执行验收命令、失败回灌返工。模型永远不需要调用工具。
//
// 解析采取三级降级（宽容优先，但每次降级都记录，作为返工输入而不是静默接受）：
//  1. 严格：存在 ARTIFACTS 包裹块；
//  2. 标记：无包裹块，但存在 file:<path> 标记围栏；
//  3. 启发：无标记，但围栏前一行是文件名。
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

// ParseLevel 表示解析走的降级层级。
type ParseLevel string

const (
	LevelStrict    ParseLevel = "strict"
	LevelMarked    ParseLevel = "marked"
	LevelHeuristic ParseLevel = "heuristic"
	// LevelLoose 是最后的宽容降级：围栏标记数量不对（如 "`manifest"）时仍能取出文件。
	LevelLoose ParseLevel = "loose"
	LevelNone  ParseLevel = "none"
)

const (
	blockBegin  = "<<<ARTIFACTS>>>"
	blockEnd    = "<<<END>>>"
	fenceMarker = "```"
)

// File 是一个待落盘文件。
type File struct {
	Path   string `json:"path"`
	Lang   string `json:"lang,omitempty"`
	Body   []byte `json:"-"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

// Check 是一条验收命令。
type Check struct {
	Cmd        string `json:"cmd"`
	TimeoutSec int    `json:"timeout,omitempty"`
	ExpectExit int    `json:"expectExit"`
	Why        string `json:"why,omitempty"`
}

// ManifestFile 是 manifest 里声明的文件。
type ManifestFile struct {
	Path string `json:"path"`
	Lang string `json:"lang"`
}

// Manifest 是协议头部声明的文件清单与验收命令。
type Manifest struct {
	Files  []ManifestFile `json:"files"`
	Verify []Check        `json:"verify"`
	Notes  string         `json:"notes,omitempty"`
}

// Result 是一次解析的完整结果。
type Result struct {
	Files    []File
	Manifest Manifest
	Level    ParseLevel
	Problems []string
	Raw      string
}

// HasFiles 判断是否解析出至少一个文件。
func (r *Result) HasFiles() bool { return r != nil && len(r.Files) > 0 }

// OK 判断产物是否可直接进入验收（有文件且无结构性缺陷）。
func (r *Result) OK() bool { return r.HasFiles() && len(r.Problems) == 0 }

// Limits 约束解析与落盘的安全边界。
type Limits struct {
	MaxFileBytes  int
	MaxTotalBytes int
	MaxFiles      int
}

func (l Limits) normalized() Limits {
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = 2 << 20
	}
	if l.MaxTotalBytes <= 0 {
		l.MaxTotalBytes = 8 << 20
	}
	if l.MaxFiles <= 0 {
		l.MaxFiles = 20
	}
	return l
}

// Parse 从模型回答里解析产物。
func Parse(raw string, lim Limits) *Result {
	lim = lim.normalized()
	res := &Result{Raw: raw, Level: LevelNone}

	if body, ok := extractBlock(raw); ok {
		strict := &Result{Raw: raw, Level: LevelStrict}
		parseStrict(body, strict)
		if strict.HasFiles() && len(strict.Problems) == 0 {
			// 严格层无缺陷：最高置信，直接用。
			finalize(strict, lim)
			return strict
		}
		if strict.HasFiles() {
			// 严格层取到了文件但有缺陷。只有"围栏个数不对导致的误判"可以被 loose 层纠正：
			// 条件是严格层确实报了"代码围栏未闭合"（这是围栏错位的唯一指纹），
			// 且 loose 层能解析出**同一批文件**并带出非空 verify。
			// 真正的缺陷（manifest 与正文不一致、截断丢内容）保留严格层判定 —— 不能被容错层洗绿。
			if hasUnclosedProblem(strict.Problems) && hasLooseMarker(body) {
				if lf, man := parseLooseArtifacts(body); len(lf) > 0 {
					loose := &Result{Raw: raw, Level: LevelLoose, Files: lf}
					loose.Manifest.Verify = man.Verify
					loose.Manifest.Files = man.Files
					if len(loose.Manifest.Verify) == 0 {
						loose.Manifest.Verify = parseLooseVerify(raw)
					}
					loose.Manifest.Notes = "loose-fence"
					validateManifestFiles(loose)
					finalize(loose, lim)
					if len(loose.Problems) == 0 && len(loose.Manifest.Verify) > 0 && samePathSet(strict.Files, lf) {
						return loose
					}
				}
			}
			finalize(strict, lim)
			return strict
		}
		// 严格层一个文件都没取到：留给下面几级降级（marked/heuristic/loose）。
		res = strict
	}

	files := parseMarkedFences(raw)
	if len(files) > 0 {
		res.Level = LevelMarked
		res.Files = files
		res.Manifest.Verify = parseLooseVerify(raw)
		finalize(res, lim)
		return res
	}

	files = parseHeuristicFences(raw)
	if len(files) > 0 {
		res.Level = LevelHeuristic
		res.Files = files
		finalize(res, lim)
		return res
	}

	// 第四级降级（loose）：真实模型常把围栏写成 "\u0060manifest" / "\u0060file:path"
	// （反引号个数不对，或把 manifest 塞进匿名围栏里）。这种输出内容其实是可用的，
	// 直接判"未找到产物"会让真实任务全部降级（现场 glm-5.3 实测）。
	// 解析仍走同一套安全校验（SafePath/大小上限），只是放宽了围栏识别。
	files, man2 := parseLooseArtifacts(raw)
	if len(files) > 0 {
		// 进入宽松层：只有严格层报过"围栏未闭合"（围栏错位指纹）时，才清掉上一级问题；
		// 否则说明是模型真的没按协议写（这里能捡到文件只是兜底），保留原有问题作为返工依据。
		if hasUnclosedProblem(res.Problems) && hasLooseMarker(raw) {
			res.Problems = nil
		}
		res.Level = LevelLoose
		res.Files = files
		res.Manifest.Verify = man2.Verify
		res.Manifest.Files = man2.Files
		if len(res.Manifest.Verify) == 0 {
			res.Manifest.Verify = parseLooseVerify(raw)
		}
		if len(res.Manifest.Verify) == 0 {
			res.Problems = append(res.Problems, "manifest 未声明任何 verify 验收命令（无法证明产物可运行）")
		}
		res.Manifest.Notes = "loose-fence"
		validateManifestFiles(res)
		finalize(res, lim)
		if len(res.Problems) == 0 {
			// 降级本身要留痕：不算失败，但必须可观测（面板/审计能看到走了 loose 解析）。
			res.Manifest.Notes = "loose-fence"
		}
		return res
	}

	res.Problems = append(res.Problems, "回答中未找到任何可落盘的文件块（缺少 ARTIFACTS 协议块）")
	finalize(res, lim)
	return res
}

// validateManifestFiles 校验 manifest 声明的文件与正文是否一致（容错层也要走同一判据）。
func validateManifestFiles(res *Result) {
	if len(res.Manifest.Files) == 0 {
		return
	}
	declared := map[string]bool{}
	byPath := map[string]bool{}
	for _, f := range res.Files {
		byPath[f.Path] = true
	}
	for _, d := range res.Manifest.Files {
		dp := strings.TrimSpace(d.Path)
		if dp == "" {
			continue
		}
		declared[dp] = true
		if !byPath[dp] {
			res.Problems = append(res.Problems, "manifest 声明了文件但正文缺失: "+dp)
		}
	}
	for _, f := range res.Files {
		if !declared[f.Path] {
			res.Problems = append(res.Problems, "正文有文件但 manifest 未声明: "+f.Path)
		}
	}
}

// hasUnclosedProblem 判断问题里是否含"代码围栏未闭合"。
//
// 这是"模型把围栏个数写错"的唯一可靠指纹：严格扫描器按行首三连反引号配对，
// 遇到一重反引号标记时会错位，从而把正常输出报成未闭合。只有这种误判才允许用
// 宽松层纠正；没有这个指纹的问题（缺 verify、manifest 不一致、截断丢内容）
// 一律保留严格层判定，避免容错层把真缺陷洗绿。
func hasUnclosedProblem(problems []string) bool {
	for _, p := range problems {
		if strings.Contains(p, "未闭合") {
			return true
		}
	}
	return false
}

// hasLooseMarker 判断输出里是否存在"单/双反引号标记行"（\`manifest / \`file:path）。
//
// 这是「模型把围栏个数写错」的正面指纹：严格扫描器只认三连反引号，遇到一重反引号
// 标记就会把标记行当围栏边界，造成配对错位（表现为"未闭合"）。
// 与之相对，"真的被截断"的输出里不会有这种标记行 —— 因此截断检测不会被容错层绕过。
func hasLooseMarker(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		n := 0
		for n < len(trimmed) && trimmed[n] == '`' {
			n++
		}
		if n == 0 || n >= 3 {
			continue
		}
		rest := strings.TrimSpace(trimmed[n:])
		if rest == "manifest" || strings.HasPrefix(rest, "manifest ") || strings.HasPrefix(rest, "file:") {
			return true
		}
	}
	return false
}

// samePathSet 判断两组文件的路径集合是否一致（容错层不得改变产物清单）。
func samePathSet(a, b []File) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, f := range a {
		set[f.Path] = true
	}
	for _, f := range b {
		if !set[f.Path] {
			return false
		}
	}
	return true
}

// extractBlock 取出 ARTIFACTS 与 END 之间的内容。
func extractBlock(raw string) (string, bool) {
	i := strings.Index(raw, blockBegin)
	if i < 0 {
		return "", false
	}
	rest := raw[i+len(blockBegin):]
	if j := strings.Index(rest, blockEnd); j >= 0 {
		return rest[:j], true
	}
	return rest, true
}

// fence 描述一个围栏块。
type fence struct {
	info   string
	body   string
	closed bool
}

// scanFences 扫描全文围栏块，并返回未闭合数量。
func scanFences(text string) ([]fence, int) {
	var out []fence
	lines := strings.Split(text, "\n")
	var cur *fence
	var buf []string
	unclosed := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, fenceMarker) {
			if cur == nil {
				cur = &fence{info: strings.TrimSpace(strings.TrimPrefix(trimmed, fenceMarker))}
				buf = buf[:0]
				continue
			}
			cur.body = strings.Join(buf, "\n")
			cur.closed = true
			out = append(out, *cur)
			cur = nil
			buf = buf[:0]
			continue
		}
		if cur != nil {
			buf = append(buf, line)
		}
	}
	if cur != nil {
		cur.body = strings.Join(buf, "\n")
		out = append(out, *cur)
		unclosed++
	}
	return out, unclosed
}

// parseStrict 解析 ARTIFACTS 块：manifest 围栏 + file:<path> 围栏。
func parseStrict(body string, res *Result) {
	fences, unclosed := scanFences(body)
	if unclosed > 0 {
		res.Problems = append(res.Problems, fmt.Sprintf("有 %d 个代码围栏未闭合（输出被截断）", unclosed))
	}
	byPath := map[string]File{}
	var order []string
	for _, f := range fences {
		switch {
		case strings.HasPrefix(f.info, "manifest"):
			if err := json.Unmarshal([]byte(f.body), &res.Manifest); err != nil {
				res.Problems = append(res.Problems, "manifest JSON 解析失败: "+err.Error())
			}
		case strings.HasPrefix(f.info, "file:"):
			p := strings.TrimSpace(strings.TrimPrefix(f.info, "file:"))
			if p == "" {
				res.Problems = append(res.Problems, "存在空的 file: 标记")
				continue
			}
			if _, exists := byPath[p]; exists {
				res.Problems = append(res.Problems, "重复的文件块: "+p)
			}
			byPath[p] = File{Path: p, Lang: langOf(p), Body: []byte(f.body)}
			order = append(order, p)
		}
	}
	for _, p := range order {
		res.Files = append(res.Files, byPath[p])
	}
	if len(res.Manifest.Files) > 0 {
		declared := map[string]bool{}
		for _, d := range res.Manifest.Files {
			dp := strings.TrimSpace(d.Path)
			if dp == "" {
				continue
			}
			declared[dp] = true
			if _, ok := byPath[dp]; !ok {
				res.Problems = append(res.Problems, "manifest 声明了文件但正文缺失: "+dp)
			}
		}
		for _, f := range res.Files {
			if !declared[f.Path] {
				res.Problems = append(res.Problems, "正文有文件但 manifest 未声明: "+f.Path)
			}
		}
	}
	if len(res.Manifest.Verify) == 0 {
		res.Problems = append(res.Problems, "manifest 未声明任何 verify 验收命令（无法证明产物可运行）")
	}
}

// parseMarkedFences 解析 file:<path> 标记围栏。
func parseMarkedFences(raw string) []File {
	fences, _ := scanFences(raw)
	var out []File
	seen := map[string]bool{}
	for _, f := range fences {
		if !strings.HasPrefix(f.info, "file:") {
			continue
		}
		p := strings.TrimSpace(strings.TrimPrefix(f.info, "file:"))
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, File{Path: p, Lang: langOf(p), Body: []byte(f.body)})
	}
	return out
}

// parseHeuristicFences 尝试用「围栏前一行是文件名」做启发式解析。
func parseHeuristicFences(raw string) []File {
	lines := strings.Split(raw, "\n")
	var out []File
	seen := map[string]bool{}
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, fenceMarker) || trimmed == fenceMarker {
			continue
		}
		var buf []string
		closed := false
		for j := i + 1; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if strings.HasPrefix(t, fenceMarker) {
				closed = true
				break
			}
			buf = append(buf, lines[j])
		}
		if !closed || len(buf) == 0 {
			continue
		}
		p := ""
		for k := i - 1; k >= 0 && k >= i-2; k-- {
			cand := strings.TrimSpace(lines[k])
			cand = strings.TrimSuffix(cand, ":")
			cand = strings.Trim(cand, "`*")
			cand = strings.TrimSpace(cand)
			if looksLikePath(cand) {
				p = cand
				break
			}
			if cand != "" {
				break
			}
		}
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, File{Path: p, Lang: langOf(p), Body: []byte(strings.Join(buf, "\n"))})
	}
	return out
}

// parseLooseVerify 在非严格层里尽力找验收命令。
//
// 两个实测踩过的坑（隔离实例首跑暴露）：
//  1. 模型常把 manifest JSON 整行贴出来，行内含 "pwsh -NoProfile ..."，
//     于是整段 JSON 被当成命令 → 解释器校验失败，验收永远不过。
//     判据：以 { 或 [ 开头的行一律不是命令。
//  2. 模型常在正文里描述"运行：python x.py"，这种行可以做命令；
//     但只取**第一个 token 是白名单解释器**的行，避免句子里夹带
//     "然后 python 会..." 这种叙述被误执行。
func parseLooseVerify(raw string) []Check {
	var out []Check
	interpreters := map[string]bool{
		"python": true, "python3": true, "py": true,
		"node": true, "pwsh": true, "powershell": true, "go": true,
	}
	for _, line := range strings.Split(raw, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || len(t) > 300 {
			continue
		}
		// 坑 1：JSON/数组行不是命令。
		if strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") ||
			strings.HasPrefix(t, "\"files\"") || strings.HasPrefix(t, "\"verify\"") {
			continue
		}
		lower := strings.ToLower(t)
		if !strings.Contains(lower, "python ") && !strings.Contains(lower, "node ") &&
			!strings.Contains(lower, "pwsh ") && !strings.Contains(lower, "go test") &&
			!strings.Contains(lower, "go run") && !strings.Contains(lower, "go build") {
			continue
		}
		cleaned := strings.TrimSpace(strings.TrimLeft(t, "-*+ 0123456789.、"))
		if cleaned == "" || !strings.Contains(cleaned, " ") {
			continue
		}
		// 坑 2：从行的第一个 token 开始找白名单解释器；找到就把它及其后的内容
		// 当作命令（丢弃前面的散文，如"运行：""执行："）。
		// 若整行找不到任何白名单解释器，说明只是叙述，跳过。
		//
		// 注意：中文冒号/顿号常常紧贴解释器（"运行：python x.py"），
		// 直接 Fields 会切出 "运行：python" 这个整体 token 而漏判。
		// 因此先把中文标点替换成空格再切分。
		sep := strings.NewReplacer("：", " ", ":", " ", "、", " ", "，", " ", "。", " ", "；", " ")
		fields := strings.Fields(sep.Replace(cleaned))
		start := -1
		for i, f := range fields {
			f = strings.TrimSuffix(strings.Trim(f, "\"`'"), ".exe")
			lf := strings.ToLower(f)
			if interpreters[lf] || lf == "go" {
				start = i
				break
			}
		}
		if start < 0 {
			continue
		}
		cmd := strings.Join(fields[start:], " ")
		if !strings.Contains(cmd, " ") {
			continue
		}
		out = append(out, Check{Cmd: cmd, TimeoutSec: 60, ExpectExit: 0})
		if len(out) >= 3 {
			break
		}
	}
	return out
}

// looksLikePath 判断 token 是否像相对文件路径。
func looksLikePath(s string) bool {
	if s == "" || len(s) > 200 {
		return false
	}
	if strings.ContainsAny(s, " \t\"'()（）") {
		return false
	}
	switch strings.ToLower(path.Ext(s)) {
	case ".py", ".js", ".ts", ".go", ".ps1", ".sh", ".sql", ".json", ".yml", ".yaml", ".md", ".txt", ".html", ".css", ".c", ".cpp", ".rs", ".java":
		return true
	}
	return false
}

// langOf 由扩展名推断语言标记。
func langOf(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".go":
		return "go"
	case ".ps1":
		return "powershell"
	case ".sh":
		return "bash"
	case ".sql":
		return "sql"
	case ".json":
		return "json"
	case ".yml", ".yaml":
		return "yaml"
	case ".md":
		return "markdown"
	case ".html":
		return "html"
	case ".css":
		return "css"
	}
	return ""
}

// finalize 做安全校验、去重、算摘要。
func finalize(res *Result, lim Limits) {
	var kept []File
	total := 0
	seen := map[string]bool{}
	for _, f := range res.Files {
		clean, err := SafePath(f.Path)
		if err != nil {
			res.Problems = append(res.Problems, fmt.Sprintf("非法文件路径 %q: %v", f.Path, err))
			continue
		}
		if seen[clean] {
			res.Problems = append(res.Problems, "重复文件路径: "+clean)
			continue
		}
		seen[clean] = true
		if len(f.Body) > lim.MaxFileBytes {
			res.Problems = append(res.Problems, fmt.Sprintf("文件 %s 超过单文件上限 %d 字节", clean, lim.MaxFileBytes))
			continue
		}
		total += len(f.Body)
		if total > lim.MaxTotalBytes {
			res.Problems = append(res.Problems, "产物总大小超过上限")
			break
		}
		if len(kept) >= lim.MaxFiles {
			res.Problems = append(res.Problems, "文件数超过上限")
			break
		}
		sum := sha256.Sum256(f.Body)
		f.Path = clean
		f.SHA256 = hex.EncodeToString(sum[:])
		f.Bytes = len(f.Body)
		if f.Lang == "" {
			f.Lang = langOf(clean)
		}
		kept = append(kept, f)
	}
	res.Files = kept
	sort.SliceStable(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })
}

// SafePath 把声明的路径规范化并拒绝越界。
//
// 关键顺序：必须先判 ".." 再剥前导 "./"，否则 "../a" 会被 TrimLeft 削成 "a"
// 从而绕过越界检查（这是本函数第一版的真实缺陷，由单测抓到）。
func SafePath(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\\\", "/"))
	if p == "" {
		return "", fmt.Errorf("空路径")
	}
	if strings.Contains(p, "..") {
		return "", fmt.Errorf("禁止上跳目录")
	}
	p = strings.TrimPrefix(p, "./")
	if p == "" {
		return "", fmt.Errorf("空路径")
	}
	if strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("必须是相对路径")
	}
	if len(p) > 1 && p[1] == ':' {
		return "", fmt.Errorf("必须是相对路径")
	}
	clean := path.Clean(p)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("路径无效")
	}
	if strings.Contains(clean, "..") {
		return "", fmt.Errorf("禁止上跳目录")
	}
	return clean, nil
}
