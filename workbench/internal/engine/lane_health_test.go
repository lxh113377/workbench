package engine

import (
	"os"
	"path/filepath"
	"testing"
)

const availFixture = `{
  "at": 1792370000000,
  "egress": {"ip": "117.164.185.6", "country": "CN"},
  "results": {
    "m1": {"state": "unavailable", "detail": "OpenCode's free models aren't available from your current network because we've detected abusive traffic from this IP address. This isn't related to your account."},
    "m2": {"state": "unavailable", "detail": "Forbidden"},
    "m3": {"state": "region-blocked", "detail": "This model is not available in your country"},
    "m4": {"state": "unknown", "detail": ""}
  }
}`

// TestProbeLaneHealthIPBan 是本轮最有价值的诊断能力的回归测试：
// 区分「临时限速」与「上游按 IP 封禁」——前者工作台能等，后者等多久都没用。
//
// 现场（2026-10-19）：工作台持续 429 达 64 分钟，多轮修复后仍 429。
// 最终从 availability.json 里发现上游原文：
//
//	"we've detected abusive traffic from this IP address"
//
// 这是平台能力缺口，不是代码缺陷。
func TestProbeLaneHealthIPBan(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "our-free-model")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "availability.json"), []byte(availFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	h := ProbeLaneHealth(home)
	if !h.Blocked {
		t.Fatal("检测到 abuse 关键词时必须判定为 Blocked")
	}
	if h.Reachable {
		t.Fatal("全部不可用时 Reachable 应为 false")
	}
	if h.EgressIP != "117.164.185.6" || h.Country != "CN" {
		t.Fatalf("出口信息解析错误: %+v", h)
	}
	// 结论必须说清"不是工作台缺陷"
	if !contains(h.Reason, "不是工作台缺陷") {
		t.Fatalf("结论应明确排除工作台缺陷: %q", h.Reason)
	}
	if h.Evidence == "" {
		t.Fatal("必须保留上游原文证据")
	}
}

// TestProbeLaneHealthRegionBlock 地区限制同样属于结构性阻塞。
func TestProbeLaneHealthRegionBlock(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "our-free-model")
	_ = os.MkdirAll(dir, 0o755)
	fixture := `{"at":1,"egress":{"ip":"1.2.3.4","country":"CN"},"results":{"m":{"state":"region-blocked","detail":"This model is not available in your country"}}}`
	_ = os.WriteFile(filepath.Join(dir, "availability.json"), []byte(fixture), 0o644)

	h := ProbeLaneHealth(home)
	if !h.Blocked {
		t.Fatal("地区限制应判定为 Blocked")
	}
	if !contains(h.Reason, "地区") {
		t.Fatalf("结论应提到地区限制: %q", h.Reason)
	}
}

// TestProbeLaneHealthHealthy 至少一个可用时不应判阻塞。
func TestProbeLaneHealthHealthy(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "our-free-model")
	_ = os.MkdirAll(dir, 0o755)
	fixture := `{"at":1,"egress":{"ip":"1.2.3.4","country":"CN"},"results":{"ok":{"state":"available"},"bad":{"state":"unavailable","detail":"boom"}}}`
	_ = os.WriteFile(filepath.Join(dir, "availability.json"), []byte(fixture), 0o644)

	h := ProbeLaneHealth(home)
	if h.Blocked {
		t.Fatal("有可用模型时不应判阻塞")
	}
	if !h.Reachable {
		t.Fatal("Reachable 应为 true")
	}
}

// TestProbeLaneHealthMissingFile 文件不存在时必须给可读说明，不能 panic。
func TestProbeLaneHealthMissingFile(t *testing.T) {
	h := ProbeLaneHealth(t.TempDir())
	if h.Blocked || h.Reachable {
		t.Fatalf("无数据时不应断言可用或阻塞: %+v", h)
	}
	if h.Reason == "" {
		t.Fatal("无数据时应给出说明")
	}
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
