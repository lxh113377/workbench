package main

// build_asset_order_contract_test.go —— 守护构建脚本的步骤顺序：产物新鲜度判据必须在建完 exe 之后（2026-11-02 新增）。
//
// 为什么需要（这是一次真实的顺序死锁，已修过一轮，必须防回退）：
//   build.ps1 原先「第 4 步跑测试 → 第 5 步建 exe」，而 TestAssetsPresentInArtifact 比对本包内嵌资产
//   （appJS/appCSS 等 raw string）与 dist/workbench.exe 的字节。于是：改了内嵌资产 ⇒ 第 4 步必然
//   「产物缺或旧于源码的内嵌资产」判红 ⇒ throw 测试失败 ⇒ 永远走不到第 5 步重建 exe。
//   该判据自己的文档还写着「重跑 setup/build.ps1 重建产物」—— 在陈旧态下这条路根本走不通。
//
// 修法：第 4 步用 -skip 排除该判据，第 5 步建完 exe 后新增 5.5 步补跑它（此时 exe 已新，若仍红就是真缺陷）。
// 本测试锁的就是这三件事，任何一条被改回去都会红：
//   1. 第 4 步的 go test 必须带 -skip 且覆盖 TestAssetsPresentInArtifact（否则死锁复现）；
//   2. go build 必须出现在第一次 TestAssetsPresentInArtifact 判据调用之前（建完才可判）；
//   3. 必须存在一次「不带 -skip」的补跑（否则这道判据等于被摘掉、覆盖面悄悄消失）。
//
// 断言口径：在源码里找「位置」而不是找「文本长得像」—— 用字节偏移比较先后，避免被注释里的同名字样骗过。

import (
	"strings"
	"testing"
)

const buildPs1Rel = "setup/build.ps1"

func TestBuildScriptRunsAssetFreshnessAfterGoBuild(t *testing.T) {
	src, err := readRepoFile(buildPs1Rel)
	if err != nil {
		t.Skipf("读不到 %s（只在仓库树内有效）: %v", buildPs1Rel, err)
	}

	// 只看非注释行，避免注释里提到判据名就被当成调用
	var codeOnly strings.Builder
	for _, ln := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		codeOnly.WriteString(ln)
		codeOnly.WriteString("\n")
	}
	body := codeOnly.String()

	buildAt := strings.Index(body, "go build")
	if buildAt < 0 {
		t.Fatalf("%s 里找不到 go build 这一步 —— 构建链结构被改动了，请同步本判据", buildPs1Rel)
	}

	// 两类位置要分开找，不能混：
	//   · -skip 形态 = 测试步的「排除」，它**必须早于** go build（否则死锁）；
	//   · -run  形态 = 建完 exe 后的「补跑」，它**必须晚于** go build（否则判 ---
	// 早期版本我把两者混成一个「第一次调用」，于是把合法早于 build 的 -skip 误判成死锁（2026-11-02 自纠）。
	skipAt := strings.Index(body, "-skip \"TestAssetsPresentInArtifact\"")
	runAt := strings.Index(body, "-run TestAssetsPresentInArtifact")
	if skipAt < 0 {
		t.Fatalf("%s 的测试步没有排除该判据（缺 -skip 形态）—— 改了内嵌资产后重建会再次卡死", buildPs1Rel)
	}
	if runAt < 0 {
		t.Fatalf("%s 建完 exe 后没有补跑该判据（缺 -run 形态）—— 这道覆盖会被悄悄摘掉", buildPs1Rel)
	}
	if skipAt > buildAt {
		t.Errorf("排除动作（偏移 %d）晚于 go build（偏移 %d）—— 死锁会在测试步先生效，排除就没意义了", skipAt, buildAt)
	}
	if runAt < buildAt {
		t.Errorf("补跑动作（偏移 %d）早于 go build（偏移 %d）—— 产物还没新就判，等于没修（顺序死锁复现）", runAt, buildAt)
	}
}
