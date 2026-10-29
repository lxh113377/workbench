package modelpool

import (
	"strings"
	"testing"
)

// TestListingDiagCarriesScene 钉住「判 not_listed 时的现场要素」（round 68 · T72）。
//
// 为什么要有这条：本轮出现过「进程内用生产 spec 探测 34 healthy，而线上 38/38 全 not_listed」的矛盾，
// 当时没有任何可复算的现场 —— 差一点就靠猜。诊断字符串必须带齐：base / 钥匙指纹 / 清单条数 / 样例 id / 试过的形态。
func TestListingDiagCarriesScene(t *testing.T) {
	body := []byte(`{"data":[{"id":"our-free-model/beam-free"},{"id":"deepseek-official/deepseek-flash"}]}`)
	d := listingDiag(body, "http://127.0.0.1:8326/v1", "LcL1C2m-OSvqCPNQ", []string{"beam-free", "our-free-model/beam-free"})
	for _, need := range []string{"base=http://127.0.0.1:8326/v1", "key=LcL1C2", "清单条数=2", "our-free-model/beam-free", "试过=[beam-free our-free-model/beam-free]"} {
		if !strings.Contains(d, need) {
			t.Fatalf("诊断缺少要素 %q：%s", need, d)
		}
	}
	// 非清单形状（known=false 的形态）也要能给出条数=-1，而不是崩。
	bad := listingDiag([]byte("not json"), "http://x/v1", "abcdefg", nil)
	if !strings.Contains(bad, "清单条数=-1") {
		t.Fatalf("非清单形状应报 -1：%s", bad)
	}
	// 钥匙指纹最多 6 位（不得把整把钥匙写进 pool.json）。
	if strings.Contains(bad, "abcdefg") {
		t.Fatalf("钥匙指纹过长（应截断到 6 位）：%s", bad)
	}
}
