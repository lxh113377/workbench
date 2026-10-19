package modelpool

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestRedactSpecsNeverLeaksKey 是密钥外流的最后一道闸。
func TestRedactSpecsNeverLeaksKey(t *testing.T) {
	specs := []Spec{{ID: "vendor/x", Model: "x", APIKey: "sk-super-secret-abcdef",
		BaseURL: "http://127.0.0.1:1/v1", Enabled: true, Routable: true}}
	b, err := json.Marshal(RedactSpecs(specs))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "sk-super-secret-abcdef") {
		t.Fatal("RedactSpecs 输出了明文密钥")
	}
	if strings.Contains(s, "apiKey") {
		t.Fatal("RedactSpecs 不得保留 apiKey 字段名（即便值为空，也会被下游误当成可回写字段）")
	}
	if !strings.Contains(s, "9876") {
		t.Logf("指纹形式：%s", s)
	}
}

// TestRedactSpecsCoversAllSpecFields 用反射钉住字段覆盖：
// 新增 Spec 字段时若忘了在 RedactSpecs 里输出，面板/CLI 会静默缺字段。
func TestRedactSpecsCoversAllSpecFields(t *testing.T) {
	out := RedactSpecs([]Spec{{ID: "a", Model: "a"}})[0]
	rt := reflect.TypeOf(Spec{})
	// Runtime 是运行期投影（不落盘、不进 API）；APIKey 由 hasKey/keyHint 替代。
	skip := map[string]bool{"Runtime": true, "APIKey": true}
	var missing []string
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if skip[f.Name] {
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			name = lowerFirst(f.Name)
		}
		if _, okf := out[name]; !okf {
			missing = append(missing, f.Name+"->"+name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("RedactSpecs 缺字段（新增 Spec 字段后忘了同步，或改了 json tag）：%v", missing)
	}
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
