// Command assertionprobe 是 tests/check_test_assertions.ps1 的解析后端（issue #12/#9 收口轮新增）。
//
// 为什么必须用 go/parser 而不是在 PowerShell 里做文本扫描（2026-10-29 实测教训）：
//
//	旧版用「下一个以 func 开头的行」当函数体边界，被 Go **raw string 字面量**骗 ——
//	kind_writeback_contract_test.go 的负控测试体内 fake := ` … func f() { … } `，那行 func
//	恰好顶格 ⇒ 函数体被截成两行、里面的 t.Fatalf 不在扫描窗口 ⇒ 把**有断言的测试**判成
//	零断言（CI run 40889071988 假红，红的是判据不是被测件）。
//	我第一版补救是「按行内反引号奇偶跟踪 raw string 状态」，实测**更危险**：双引号字符串里
//	也可以合法含反引号（"a`b"），奇偶一旦被带偏，后续**真实** Test 声明会被当成"在串内"而
//	整体跳过 ⇒ 真零断言测试永不报 = **假绿**（本仓口径：假绿比假红严重）。
//	量化：未修 1035 个声明、奇偶版只数到 1030，少的 5 个全是真测试（artifact/protocol_test.go:42、
//	engine/p3_offline_e2e_test.go:85、engine/repair_test.go:72、engine/replan_boundary_test.go:68、
//	modelpool/protocol_test.go:32），不是假声明。⇒ 文本层无法可靠判定词法状态，只能交给 parser。
//
// 输出契约（供 PowerShell 逐行消费）：每行 TAB 分隔
//
//	TEST<TAB>文件相对路径<TAB>行号<TAB>函数名<TAB>HAS|NONE<TAB>豁免类别（无豁免为空）
//
// 豁免沿用旧口径，不放宽也不收紧（头注详述两条合法形态的来历）：
//
//	compiletime —— `var _ SomeInterface = ...` 型编译期契约断言（类型不符则编译失败）
//	panicdoc    —— 紧邻文档注释写明「不得 panic/不崩溃/survives nil」，且体内无 fmt.Print*
//
// 退出码：0=正常输出 / 非 0=解析失败（调用方必须按 UNVERIFIED 处理，不得当成"全都有断言"）。
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// assertMethods 与旧版正则同集合：判的是「这个 Test 会不会让失败可见」。
var assertMethods = map[string]bool{
	"Fatal": true, "Fatalf": true, "Error": true, "Errorf": true, "Fail": true, "FailNow": true,
	// Helper/t.Run 不算断言；Skip 也不算（跳过不等于验证）。
}

var panicDocRe = regexp.MustCompile(`不[得应会]?\s*panic|不崩溃|survives?\s*nil|not\s+panic|no\s+panic`)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: assertionprobe <文件...>（通常由 git ls-files 提供）")
		os.Exit(2)
	}
	fset := token.NewFileSet()
	bad := 0
	for _, f := range args {
		f = strings.Trim(f, "\"")
		if f == "" {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
		if err != nil {
			// 解析失败必须显式报出去：静默跳过会退化成「没测到 = 通过」。
			fmt.Fprint(os.Stderr, "PARSE_FAIL|"+f+"|"+err.Error()+"\n")
			bad++
			continue
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			if !isTestingTFunc(fn) {
				continue
			}
			pos := fset.Position(fn.Pos())
			has := hasAssertion(fn)
			exempt := ""
			if !has {
				if compileTimeAssert(fn) {
					exempt = "compiletime"
				}
				if exempt == "" && panicContractDoc(fn, file) && !bodyHasPrint(fn) {
					exempt = "panicdoc"
				}
			}
			rel := filepath.ToSlash(f)
			if has || exempt != "" {
				fmt.Print("TEST|" + rel + "|" + strconv.Itoa(pos.Line) + "|" + fn.Name.Name + "|HAS|" + exempt + "\n")
			} else {
				fmt.Print("TEST|" + rel + "|" + strconv.Itoa(pos.Line) + "|" + fn.Name.Name + "|NONE|\n")
			}
		}
	}
	if bad > 0 {
		os.Exit(3)
	}
}

// isTestingTFunc 要求形参里有 *testing.T —— 与旧版正则的语义对齐：
// 只认真正的测试函数，避免把 TestMain 之外的辅助函数算进来。
func isTestingTFunc(fn *ast.FuncDecl) bool {
	if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return false
	}
	for _, p := range fn.Type.Params.List {
		star, ok := p.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		se, ok := star.X.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		id, ok := se.X.(*ast.Ident)
		if ok && id.Name == "testing" && se.Sel.Name == "T" {
			return true
		}
	}
	return false
}

// hasAssertion 在函数体内找 t.Fatal/Error/Fail 系列调用。
// 关键点：raw string 里的文本是**一个 BasicLit**，不会被当成调用 ⇒ 这正是 parser 相对
// 文本扫描的不可替代处（旧版假红的根因）。
func hasAssertion(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		se, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if !assertMethods[se.Sel.Name] {
			return true
		}
		id, ok := se.X.(*ast.Ident)
		if !ok {
			return true
		}
		// 接收者必须是本函数的 *testing.T 形参（旧版按字面 t\. 匹配；这里用类型名更稳）
		if _, exists := testingParamNames(fn)[id.Name]; exists {
			found = true
		}
		return true
	})
	return found
}

// testingParamNames 收集所有 *testing.T 形参名（含 t *testing.T 与 tt *testing.T 之类）。
func testingParamNames(fn *ast.FuncDecl) map[string]struct{} {
	out := map[string]struct{}{}
	if fn.Type.Params == nil {
		return out
	}
	for _, p := range fn.Type.Params.List {
		star, ok := p.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		se, ok := star.X.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		id, ok := se.X.(*ast.Ident)
		if !ok || id.Name != "testing" || se.Sel.Name != "T" {
			continue
		}
		for _, nm := range p.Names {
			out[nm.Name] = struct{}{}
		}
	}
	return out
}

// compileTimeAssert：体内是否有 `var _ <Type> = <value>` 形态的编译期契约断言。
// 只判形态不取值：这类断言在编译期完成（类型不匹配则整个包编译失败），运行期无需再断言。
// 注意全程用带 ok 的类型断言 —— 判据自己 panic 会让它退化成「什么都判不了」的假绿。
func compileTimeAssert(fn *ast.FuncDecl) bool {
	for _, st := range fn.Body.List {
		decl, ok := st.(*ast.DeclStmt)
		if !ok {
			continue
		}
		gd, ok := decl.Decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, sp := range gd.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "_" || len(vs.Values) == 0 {
				continue
			}
			// `var _ Iface = impl`（带类型）或 `var _ = Iface(impl)`（靠转换）都算。
			return true
		}
	}
	return false
}

// panicContractDoc：本函数的 doc 注释**或函数区间内的任意注释**里写明「不 panic/不崩溃/ survives nil」类契约。
// 为什么要覆盖函数体内注释（2026-10-29 实测）：internal/engine/engine_smoke_test.go:276 的
// TestFinishCancelledOnUnknownTask 把契约写在体内第 278 行 —— 旧版 PS 扫的是 body+docBlock，
// 因此本来就算豁免；我的第一版只扫 func 上方注释 ⇒ 把已登记的合法豁免判成零断言（假红）。
// 用 AST 的注释区间判定，两种书写位置都覆盖，且不受 raw string / 反引号影响。
func panicContractDoc(fn *ast.FuncDecl, file *ast.File) bool {
	texts := []string{}
	if fn.Doc != nil {
		texts = append(texts, fn.Doc.Text())
	}
	lo, hi := fn.Pos(), fn.End()
	for _, cg := range file.Comments {
		if cg.Pos() >= lo && cg.End() <= hi {
			texts = append(texts, cg.Text())
		}
	}
	return panicDocRe.MatchString(strings.Join(texts, "\n"))
}

// bodyHasPrint：体内是否有 fmt.Print* —— 调试残留不给豁免（旧版同条件）。
func bodyHasPrint(fn *ast.FuncDecl) bool {
	hit := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if se, ok := call.Fun.(*ast.SelectorExpr); ok {
			if id, ok := se.X.(*ast.Ident); ok && id.Name == "fmt" && strings.HasPrefix(se.Sel.Name, "Print") {
				hit = true
			}
		}
		return true
	})
	return hit
}
