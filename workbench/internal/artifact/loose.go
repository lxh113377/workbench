package artifact

import (
	"encoding/json"
	"strings"
)

// parseLooseArtifacts 是四级解析的最后一层容错。
//
// 现场证据（2026-10-19，隔离实例真实模型 glm-5.3）：模型输出形如
//
//	<<<ARTIFACTS>>>
//	```
//	`manifest
//	{"files":[{"path":"hello.py"}],"verify":[{"cmd":"python hello.py","expectExit":0}]}
//	```
//	`file:hello.py
//	print("hello from workbench")
//	```
//	<<<END>>>
//
// 围栏"个数"不对：外层是三重反引号，内层标记只有一重。标准 scanFences 会把
// 一重反引号也当围栏边界，导致 manifest 与文件内容互相错位 → 严格层判结构缺陷。
//
// 这一层不再依赖围栏配对，改为按"标记行"做状态机：
//   - 遇到 <n 个反引号>manifest → 读其后直到下一个标记行，作为 manifest JSON
//   - 遇到 <n 个反引号>file:<path> → 读其后直到下一个标记行或 ARTIFACTS 结束，作为文件正文
//   - 正文首尾多余的空行/围栏行自动剥掉（模型常在内容末尾再包一层围栏）
//
// 安全边界不变：路径仍过 SafePath，大小仍过 Limits（finalize 统一处理）。
func parseLooseArtifacts(raw string) ([]File, Manifest) {
	var out []File
	var man Manifest
	lines := strings.Split(raw, "\n")
	seen := map[string]bool{}
	for i := 0; i < len(lines); i++ {
		marker, rest, ok := looseLine(lines[i])
		if !ok {
			continue
		}
		var buf []string
		j := i + 1
		for ; j < len(lines); j++ {
			if strings.Contains(lines[j], blockEnd) {
				break
			}
			if _, _, isMark := looseLine(lines[j]); isMark {
				break
			}
			buf = append(buf, lines[j])
		}
		body := trimLooseFences(buf)
		if marker == "manifest" {
			text := strings.TrimSpace(strings.Join(append([]string{rest}, body...), "\n"))
			if m, ok := parseManifestJSON(text); ok {
				man.Files = append(man.Files, m.Files...)
				man.Verify = append(man.Verify, m.Verify...)
			}
		} else if marker == "file" && rest != "" && !seen[rest] {
			seen[rest] = true
			out = append(out, File{Path: rest, Lang: langOf(rest), Body: []byte(strings.Join(body, "\n"))})
		}
		i = j - 1
	}
	return out, man
}

// looseLine 判断一行是否是宽松解析的标记行，返回标记类别（file/manifest）、
// 标记后剩余内容（文件的相对路径）与是否命中。
//
// 允许前缀任意个反引号与空白：`manifest / ```manifest / manifest 都认；
// 文件标记同理：`file:a.py / ```file:a.py / file:a.py 都认。
func looseLine(line string) (marker, rest string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.Contains(trimmed, blockEnd) {
		return "", "", false
	}
	i := 0
	for i < len(trimmed) && trimmed[i] == '`' {
		i++
	}
	body := strings.TrimSpace(trimmed[i:])
	switch {
	case strings.HasPrefix(body, "file:"):
		return "file", strings.TrimSpace(strings.TrimPrefix(body, "file:")), true
	case body == "manifest" || strings.HasPrefix(body, "manifest "):
		return "manifest", strings.TrimSpace(strings.TrimPrefix(body, "manifest")), true
	}
	return "", "", false
}

// trimLooseFences 去掉正文首尾的空行与"纯围栏行"。
func trimLooseFences(lines []string) []string {
	start, end := 0, len(lines)
	for start < end {
		t := strings.TrimSpace(lines[start])
		if t == "" || strings.Trim(t, "`") == "" {
			start++
			continue
		}
		break
	}
	for end > start {
		t := strings.TrimSpace(lines[end-1])
		if t == "" || strings.Trim(t, "`") == "" || strings.Contains(t, blockEnd) {
			end--
			continue
		}
		break
	}
	return lines[start:end]
}

// parseManifestJSON 从一段可能夹着说明文字的文本里取出 manifest JSON。
func parseManifestJSON(text string) (Manifest, bool) {
	var m Manifest
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return m, false
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return m, false
	}
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &m); err != nil {
		return m, false
	}
	return m, true
}
