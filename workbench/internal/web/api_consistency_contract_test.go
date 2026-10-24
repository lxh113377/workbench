package web

import (
	"strings"
	"testing"
)

// TestThreeApiImplsAreIdentical 钉住三份内嵌 JS 的 api() 必须逐行相同（注释除外）。
//
// 为什么需要（2026-10-24 round 108）：
//
//	前端基建是**三份复制粘贴**的内嵌脚本（appJS / govJS / poolJS，各自 IIFE，不共享模块）。
//	round 103-107 给 appJS 的 api() 加了可选 timeoutMs，另两份没跟上 —— 三份开始分叉；
//	复核时又发现**更早的一处静默分叉**：govJS 的错误处理是 data.error，而 appJS/poolJS 是
//	data.error || data.message。这类分叉靠人眼比对必然漏（本轮就是靠 Node 逐行 diff 才发现）。
//
//	服务端只经 bad() 发 {"error": msg}（web 全包搜 message 键为 0 处），所以那处差异
//	是死分支、不改输出 —— 但**下一个差异未必无害**：鉴权头、Content-Type、错误兜底
//	任何一处单边改动都会让三份行为静默不一致，而没有测试会失败。
//
// 判据：抽出三份的 api() 函数体，去掉纯注释行后必须逐字节相同。
// 本测试不判「谁对谁错」（那是设计问题），只判「不许分叉」—— 可证伪、零误报。
func TestThreeApiImplsAreIdentical(t *testing.T) {
	blobs := map[string]string{"appJS": appJS, "govJS": govJS, "poolJS": poolJS}
	var ref string
	var refName string
	for _, name := range []string{"appJS", "govJS", "poolJS"} {
		body, ok := extractAPI(blobs[name])
		if !ok {
			t.Fatalf("%s 里找不到 api() —— 结构变了须同步本测试（否则判据空转会一直判绿）", name)
		}
		if ref == "" {
			ref, refName = body, name
			continue
		}
		if body != ref {
			diffAPIBodies(t, refName, name, ref, body)
		}
	}
}

// extractAPI 抽出 `const api = async ...` 到与之配对的 `};` 结束的函数体，并去掉纯注释行。
func extractAPI(js string) (string, bool) {
	start := strings.Index(js, "const api = async")
	if start < 0 {
		return "", false
	}
	// 以缩进定位函数结束：api() 定义在 IIFE 顶层，收尾形如独立的 "  };"
	rest := js[start:]
	end := strings.Index(rest, "\n  };")
	if end < 0 {
		return "", false
	}
	body := rest[:end+len("\n  };")]
	var kept []string
	for _, ln := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "//") {
			continue // 注释允许不同（各文件解释各自语境）
		}
		if strings.TrimSpace(ln) == "" {
			continue
		}
		kept = append(kept, strings.TrimRight(ln, " "))
	}
	return strings.Join(kept, "\n"), true
}

func diffAPIBodies(t *testing.T, a, b string, bodyA, bodyB string) {
	la := strings.Split(bodyA, "\n")
	lb := strings.Split(bodyB, "\n")
	t.Errorf("api() 三份分叉：%s 与 %s 不一致（分叉会让鉴权头/错误处理/超时能力静默不同）", a, b)
	for i := 0; i < len(la) || i < len(lb); i++ {
		var x, y string
		if i < len(la) {
			x = la[i]
		}
		if i < len(lb) {
			y = lb[i]
		}
		if x != y {
			t.Errorf("  第 %d 行不同：%s=%q", i+1, a, x)
			t.Errorf("  第 %d 行不同：%s=%q", i+1, b, y)
		}
	}
	t.Log("修法：把三份 api() 改成一致（或抽成共享常量）。历史上三份曾分别漂出 timeoutMs 与错误兜底差异。")
}
