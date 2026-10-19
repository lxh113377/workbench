package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// BrowseEntry 是目录选择器里的一个子目录。
type BrowseEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	HasChild bool   `json:"hasChild,omitempty"`
}

// BrowseResult 是一次目录浏览的结果（只返回目录名，绝不返回文件内容）。
type BrowseResult struct {
	Path      string        `json:"path"`
	Parent    string        `json:"parent,omitempty"`
	Drives    []string      `json:"drives,omitempty"`
	Entries   []BrowseEntry `json:"entries"`
	Forbidden bool          `json:"forbidden,omitempty"`
	Reason    string        `json:"reason,omitempty"`
	At        int64         `json:"at"`
}

// Browse 列出一个目录下的子目录，用于面板上的"文件夹选择器"。
//
// 安全取舍：选择器需要让用户看到任意目录的**目录名**才能导航，因此这里不做
// 工作目录范围限制；但它只列目录、不读文件内容、不跟随软链接，且对系统目录/盘符根
// 仍然可列（用户可能想选其子目录）。真正的写权限仍由 Validate/ResolveWrite 把关。
func Browse(path string, showHidden bool) (*BrowseResult, error) {
	res := &BrowseResult{At: time.Now().UnixMilli()}
	if runtime.GOOS == "windows" && strings.TrimSpace(path) == "" {
		res.Drives = listDrives()
		res.Entries = nil
		return res, nil
	}
	if strings.TrimSpace(path) == "" {
		path = "/"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return res, err
	}
	abs = filepath.Clean(abs)
	res.Path = abs
	if abs != filepath.VolumeName(abs)+string(os.PathSeparator) && abs != string(os.PathSeparator) {
		res.Parent = filepath.Dir(abs)
	}
	if runtime.GOOS == "windows" {
		res.Drives = listDrives()
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		res.Reason = err.Error()
		return res, fmt.Errorf("无法读取目录 %s: %w", abs, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if ignoreDirs[strings.ToLower(name)] {
			continue
		}
		res.Entries = append(res.Entries, BrowseEntry{Name: name, Path: filepath.Join(abs, name)})
	}
	sort.Slice(res.Entries, func(i, j int) bool {
		return strings.ToLower(res.Entries[i].Name) < strings.ToLower(res.Entries[j].Name)
	})
	return res, nil
}

// listDrives 返回 Windows 盘符列表（其它平台返回根目录）。
func listDrives() []string {
	if runtime.GOOS != "windows" {
		return []string{"/"}
	}
	var out []string
	for c := 'A'; c <= 'Z'; c++ {
		p := string(c) + ":\\"
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}
