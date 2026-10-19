package engine

import (
	"errors"
	"testing"

	"workbench/internal/executor"
)

// TestIsRateLimited 覆盖三种来源（现场踩过两种）：
//  1. executor 正常分类 -> ErrorKind = rate_limited；
//  2. 包装层只上抛 error、res 为 nil（FailoverRunner 上抛路径）；
//  3. 文本兜底匹配。
//
// 这个函数是整池冷却的闸门判据：判错就会出现"限流时连试 8 个模型"的现场故障。
func TestIsRateLimited(t *testing.T) {
	cases := []struct {
		name string
		res  *executor.Result
		err  error
		want bool
	}{
		{"ErrorKind 分类", &executor.Result{ErrorKind: string(executor.KindRateLimited)}, errors.New("boom"), true},
		{"res 为 nil 但错误文本是 429", nil, errors.New("dsh: RATE_LIMIT: too many requests; retry later"), true},
		{"too many requests 文本", nil, errors.New("HTTP 429: too many requests"), true},
		{"LastError 兜底", &executor.Result{LastError: "rate_limit exceeded"}, nil, true},
		{"普通上游错误不应误判", &executor.Result{ErrorKind: string(executor.KindUpstream)}, errors.New("upstream 502"), false},
		{"超时不应误判", &executor.Result{ErrorKind: string(executor.KindTimeout)}, errors.New("timeout"), false},
		{"nil 全部", nil, nil, false},
	}
	for _, c := range cases {
		if got := isRateLimited(c.res, c.err); got != c.want {
			t.Fatalf("%s: isRateLimited = %v, want %v", c.name, got, c.want)
		}
	}
}
