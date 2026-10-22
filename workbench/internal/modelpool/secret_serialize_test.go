package modelpool

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestGatewayMarshalDoesNotLeakKey 锁住 gateway.go 那句注释的承诺：
// 「Key 只在本进程内存中使用，API 只回显指纹」。
//
// 原先这里是明文 json:"key" 标签：Status() 虽然手挑字段（"hasKey": gw.Key != ""），
// 但类型本身不设防 —— 任何人把整个 Gateway 塞进 map/debug 输出就静默外流密钥，
// 且没有任何测试会翻红。现在给 Key 打 json:"-" 让承诺结构化。
//
// 实测确认当前生产路径从未整体 Marshal(Gateway)（grep json.Marshal 仅此测试），
// 所以是潜在面而非现行漏洞；改 tag 零风险（无反序列化依赖，
// GatewayCandidates/checkGateway 都按字段读，不经 JSON）。
func TestGatewayMarshalDoesNotLeakKey(t *testing.T) {
	const secret = "sk-GATEWAY-SECRET-DO-NOT-LEAK-99887766"
	gw := Gateway{BaseURL: "http://127.0.0.1:8326/v1", Key: secret,
		KeyFrom: "/home/.dsh/openai-gateway/api-key", Models: []string{"m1"}, At: 1}
	b, err := json.Marshal(gw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) {
		t.Fatalf("Marshal(Gateway) 明文泄漏网关密钥：%s", string(b))
	}
	// 安全面必须仍在：BaseURL 可排查；KeyFrom 是路径非密钥（排 401 要用）。
	if !strings.Contains(string(b), "8326") {
		t.Errorf("BaseURL 应可序列化：%s", string(b))
	}
	if !strings.Contains(string(b), "openai-gateway/api-key") {
		t.Errorf("KeyFrom（来源路径）应保留以便排查 401：%s", string(b))
	}
}

// TestSecretFieldsAreMarkedSkip 反射守卫：Gateway 与 Discovery 里任何暗示
// "密钥本体"的导出字段必须是 json:"-"。将来新增密钥字段忘了标，这里翻红。
//
// 为什么不含 Spec：Spec.APIKey 必须带明文 json:"apiKey" —— Store.Save 要把它
// 写进 pool.json 持久化（改了就无法落盘、面板填的端点重启即丢）。
// Spec 的对外脱敏由 RedactSpecs 在出口做（见 redact_test.go），口径不同。
// 同理 Discovery 不能整体对外序列化：它嵌套 Candidates[].Spec.APIKey，
// Marshal(Discovery) 会从嵌套层漏密钥（已实测确认）—— 所以 Discovery 是内存
// 中间态，出口只挑 base/availability 等非密钥字段或走 RedactSpecs。
func TestSecretFieldsAreMarkedSkip(t *testing.T) {
	for _, sample := range []any{Gateway{}, Discovery{}} {
		rt := reflect.TypeOf(sample)
		checked := 0
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if !isSecretBearer(f.Name) {
				continue
			}
			checked++
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag != "-" {
				t.Errorf("%T 的密钥字段 %s json tag=%q，应为 \"-\"", sample, f.Name, tag)
			}
		}
		if checked == 0 {
			t.Errorf("%T 反射守卫未识别到密钥字段（判据本身可能失效）", sample)
		}
	}
}

// isSecretBearer：字段名是否暗示"密钥本体"。排除 KeyFrom（来源路径）、
// KeyHint/HasKey（指纹/存在性，本就是设计来安全回显的，见 redact.go 口径）。
func isSecretBearer(name string) bool {
	n := strings.ToLower(name)
	if !strings.Contains(n, "key") {
		return false
	}
	for _, safe := range []string{"keyfrom", "keyhint", "haskey"} {
		if strings.Contains(n, safe) {
			return false
		}
	}
	return true
}
