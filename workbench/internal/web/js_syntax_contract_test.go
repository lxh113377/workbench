package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmbeddedJSIsSyntaxValid 用真实 JS 解析器校验三份内嵌脚本的语法有效性。
//
// 为什么必须有（2026-10-25 实测的空白）：三份脚本是 Go 的 raw string（appJS / govJS / poolJS）。
// 语法写坏时 Go 侧完全无感 —— go build / go vet / go test 全绿，只有浏览器打开才白屏。取证：
//  1. 删掉 appJS 一个右花括号 => web 包 rc=1，但抓到它的是 TestThreeApiImplsAreIdentical 与
//     TestSharedHelpersRemainIdentical：它们比对复制体文本，坏在花括号配对导致两侧长度不等，
//     于是碰巧报警。那不是语法判据，是对「文本变了」的判据。
//  2. 改坏 appJS 独有函数 renderTemplates（不参与任何比对）=> 仍 rc=1，抓到它的是
//     TestKeyboardBindingsNotInRepeatablePaths 的正则恰好撞上。依旧不是语法判据。
//  3. 全仓搜 --check / new Function / vm.Script / acorn => 0 处：没有任何判据真正解析过这份 JS。
//
// 判据来源是编译后的常量值本身（不解析 Go 源码）—— 与本仓写进 quality-audit 要求 5 的
// 定规一致：读字段值必须执行程序取运行时真值。
//
// 代价形态具体：本项目是 7x24 无人值守桌面工作台，面板是唯一观测入口；一次括号失配会让
// 整页脚本抛异常而不渲染，引擎仍在后台跑 —— 表现为「一切正常但界面死了」。
func TestEmbeddedJSIsSyntaxValid(t *testing.T) {
	// node --check 只解析不执行，正是本判据需要的强度。
	node, err := exec.LookPath("node")
	if err != nil {
		// 带理由的 skip（check_skip_disclosure 判的是无理由 skip，并让总数可见）。
		// 如实声明降级后果：本机无 node 时本判据不生效，属「没测成」而非「测过没问题」。
		// CI 跑 windows-latest（镜像预装 Node）—— 若 CI 日志里出现这条 Skip 就说明环境假设错了。
		t.Skipf("本机无 node，JS 语法判据未执行（没测成 != 合格）：%v", err)
	}
	blobs := map[string]string{"app.js": appJS, "gov.js": govJS, "pool.js": poolJS}
	// 受理面哨兵：raw string 若因声明改写变成空串，node 检查空文件照样通过 —— 判据就成了比空气。
	// 下限取首次实测字节值的三成：app 74285 / gov 14539 / pool 34123（UTF-8 字节；
	// 对应 JS 字符数是 62914 / 12068 / 28249，中文按 3 字节故比值约 1.18 —— 单位别混）。
	// 这里判的是字节，因为 len(b.s) 与 node 看到的文件都是字节口径。
	minLen := map[string]int{"app.js": 20000, "gov.js": 4000, "pool.js": 9000}
	dir := t.TempDir()
	for name, src := range blobs {
		if len(src) < minLen[name] {
			t.Errorf("%s 只有 %d 字节，低于登记下限 %d —— 内嵌脚本可能整段丢失，判据此刻比的是空气而非真脚本", name, len(src), minLen[name])
			continue
		}
		f := filepath.Join(dir, name)
		if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
			t.Fatalf("写入临时脚本失败: %v", err)
		}
		out, err := exec.Command(node, "--check", f).CombinedOutput()
		if err != nil {
			t.Errorf("%s 不是合法 JavaScript（node --check 失败）：\n"+strings.TrimSpace(string(out))+"\n后果：该页内嵌脚本整体抛异常不渲染，而后台引擎照常运行 —— 界面死了但看起来正常", name)
		} else {
			t.Logf("%s 语法有效（%d 字节，node --check 通过）", name, len(src))
		}
	}
}
