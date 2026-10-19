package dshprofile

import "encoding/json"

// jsonMarshal 统一 JSON 序列化入口（便于替换与测试）。
func jsonMarshal(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return []byte("{}")
	}
	return append(b, '\n')
}
