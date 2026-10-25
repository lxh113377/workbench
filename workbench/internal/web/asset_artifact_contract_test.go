package web

// asset_artifact_contract_test.go -- 产物必须逐字包含本包的全部内嵌资产（CSS / JS / HTML 页）。
//
// 为什么需要（2026-10-25 round 176，落实 round 175 登记的那条正确做法）：
//   tests/check_artifact_page_fresh.ps1 此前**只判 CSS 一块**；而本包实测有 8 个内嵌资产块
//   （appCSS / appJS / poolJS / govJS / modelPoolPage / indexPage / adminPage / governancePage，
//   合计约 213 KB）。只判 1 块时，**JS 或 HTML 改了不重建，本地全量一样全绿**。
//   round 175 试了三条「数反引号」的抽取口径，全部产生假阳性（struct tag 错位、游离反引号、
//   以及我自己配对 bug 造出的伪块），因此按本仓纪律先登记、不把噪声写进闸。
//
// 现在改用**语言自己的解析器**取 raw literal 的真实位置：
//   go/parser 解析本包源码 -> 遍历 const/var 的 ValueSpec -> 取 *ast.BasicLit 且 Kind == token.STRING
//   -> 只收**以反引号开头**（lit[0] == 96，即 Go raw string）且长度 >= 200 字节的块，名字取声明名。
//   结构信息来自语法树，不数反引号 => struct tag / 行内字符串 / 死代码里的反引号都不可能混进来。
//
// 产物口径：默认 <repo>/dist/workbench.exe（本包测试 cwd 是包目录，往上三层即仓库根）；
//   可用环境变量 WB_ASSET_ARTIFACT 覆盖（便于用不存在的路径验证 SKIP 分支）。
//   产物不存在 => t.Skip（本机未构建 / 干净 clone 的 go test 阶段），不误判为红。

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// assetBlock 是一个内嵌资产块：声明名 + 该 raw literal 的原始字节。
type assetBlock struct {
	Name string
	File string
	Lit  string
}

// extractAssetBlocks 用 go/parser 抽本包所有 >= minBytes 的 raw string 声明。
func extractAssetBlocks(t *testing.T, dir string, minBytes int) []assetBlock {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读包目录失败: %v", err)
	}
	fset := token.NewFileSet()
	var out []assetBlock
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", name, err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, val := range vs.Values {
					lit, ok := val.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					// Go raw string 以反引号开头（字节 96）。这样写避免在本文件里出现反引号字面量。
					if len(lit.Value) == 0 || lit.Value[0] != 96 {
						continue
					}
					body := lit.Value[1 : len(lit.Value)-1]
					if len(body) < minBytes {
						continue
					}
					var declareName string
					if i < len(vs.Names) {
						declareName = vs.Names[i].Name
					} else if len(vs.Names) > 0 {
						declareName = vs.Names[0].Name
					}
					if declareName == "" {
						declareName = name + "#" + fmt.Sprint(len(out))
					}
					out = append(out, assetBlock{Name: declareName, File: name, Lit: body})
				}
			}
		}
	}
	return out
}

// TestAssetsPresentInArtifact 钉住「产物逐字包含全部内嵌资产」。
func TestAssetsPresentInArtifact(t *testing.T) {
	// 只收 >= 200 字节的块：短字符串（如 HKCU 注册表键、struct tag 之外的小常量）不算资产。
	const minBytes = 200
	// 受理面哨兵 1：抽不到足够块 => 抽取口径失效，必须判红（不能静默通过）。
	const minBlocks = 5
	blocks := extractAssetBlocks(t, ".", minBytes)
	if len(blocks) < minBlocks {
		t.Fatalf("只抽到 %d 个资产块（期望至少 %d）—— 判据失去受理面，检查抽取口径", len(blocks), minBlocks)
	}
	// 受理面哨兵 2：总字节数下限（历史实测约 213 KB；取 100 KB 留余量，防「抽到的都是小块」）。
	total := 0
	for _, b := range blocks {
		total += len(b.Lit)
	}
	if total < 100*1024 {
		t.Fatalf("资产合计只有 %d 字节（期望 >= 100KB）—— 判据失去受理面", total)
	}

	exe := os.Getenv("WB_ASSET_ARTIFACT")
	if exe == "" {
		// 本包测试 cwd = <repo>/workbench/internal/web ⇒ 上三层是仓库根。
		exe = filepath.Join("..", "..", "..", "dist", "workbench.exe")
	}
	raw, err := os.ReadFile(exe)
	if err != nil {
		t.Skipf("产物不存在（未构建或干净 clone）: %v", err)
	}
	// 报告口径：把抽到的清单打出来（-v 时可见），便于人工核对「抽对了没」。
	sorted := append([]assetBlock(nil), blocks...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, b := range sorted {
		t.Logf("资产块 %-16s %-24s %8d 字节", b.Name, b.File, len(b.Lit))
	}
	t.Logf("共 %d 块 / %d 字节；产物 %s 大小 %d 字节", len(blocks), total, exe, len(raw))

	var missing []assetBlock
	for _, b := range blocks {
		if !bytes.Contains(raw, []byte(b.Lit)) {
			missing = append(missing, b)
		}
	}
	if len(missing) > 0 {
		for _, m := range missing {
			// 首个差异位置（证据）：用前 200 字节在产物里的落点逐字节比对。
			probe := m.Lit
			if len(probe) > 200 {
				probe = probe[:200]
			}
			detail := "该块前 200 字节未在产物中出现（产物很可能是旧的页面树）"
			if at := bytes.Index(raw, []byte(probe)); at >= 0 {
				i := 0
				for i < len(m.Lit) && at+i < len(raw) && raw[at+i] == m.Lit[i] {
					i++
				}
				lineNo := 1 + strings.Count(m.Lit[:i], string(rune(10)))
				detail = fmt.Sprintf("首个差异在该块第 %d 行附近（偏移 %d 字节）", lineNo, i)
			}
			t.Errorf("产物缺或旧于源码的内嵌资产: %s (%s, %d 字节)：%s", m.Name, m.File, len(m.Lit), detail)
		}
		t.Fatalf("共 %d / %d 块与源码不一致 —— 修法：重跑 setup/build.ps1 重建产物（这是产物落后，不是资产写错）", len(missing), len(blocks))
	}
}
