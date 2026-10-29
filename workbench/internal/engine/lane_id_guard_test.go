package engine

import (
	"strings"
	"testing"

	"workbench/internal/config"
)

// TestOrphanConfiguredModels 用 round 72 的**真实事故形态**钉住这条守卫（round 124 · T79）。
//
// 事故：我把 config.Models 的 id 写成带渠道前缀的形态（网关清单确实带前缀），
// 而模型池的 spec id 与池健康账本的键都是裸 id ⇒ 这些路线在池里没有任何条目：
// 失败记不到车道、冷却与退避全部失效。实测一小时内空转 629 次尝试、0 成功，面板看着一切正常。
func TestOrphanConfiguredModels(t *testing.T) {
	// 池投影清单（裸 id，与线上 pool.json 的键同形态）
	pool := []config.ModelRoute{
		{Model: "kilo-auto/free"}, {Model: "glm-5.3-flash-free"}, {Model: "xingchen-agi/xing4.0-29b-a4b:free"},
	}
	poolIDs := make([]string, 0, len(pool))
	for _, r := range pool {
		poolIDs = append(poolIDs, r.Model)
	}
	cases := []struct {
		name string
		cfg  []config.ModelRoute
		want []string
		pool []string
	}{
		{name: "事故形态：带前缀 id 必被点名", cfg: []config.ModelRoute{{Model: "our-free-model/kilo-auto/free"}}, want: []string{"our-free-model/kilo-auto/free"}},
		{
			name: "混合：只报带前缀那条，裸 id 不误报",
			cfg: []config.ModelRoute{
				{Model: "kilo-auto/free"}, {Model: "our-free-model/glm-5.3-flash-free"},
			},
			want: []string{"our-free-model/glm-5.3-flash-free"},
		},
		{
			name: "全部命中 ⇒ 无告警（默认配置形态不得被误报）",
			cfg:  []config.ModelRoute{{Model: "glm-5.3-flash-free"}, {Model: "kilo-auto/free"}},
			want: nil,
		},
		{
			name: "空池清单（未同步）⇒ 无信息不判，不得把全部路线报成孤儿",
			cfg:  []config.ModelRoute{{Model: "our-free-model/kilo-auto/free"}}, pool: []string{}, want: nil,
		},
		{
			name: "去重：同一 id 配两次只报一次",
			cfg:  []config.ModelRoute{{Model: "x/no-spec"}, {Model: "x/no-spec"}}, want: []string{"x/no-spec"},
		},
		{name: "空 model 字段跳过（不得报空串）", cfg: []config.ModelRoute{{Model: "   "}}, want: nil},
		{name: "配置为空 ⇒ 无告警", cfg: nil, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pids := tc.pool
			if pids == nil {
				pids = poolIDs
			}
			got := orphanConfiguredModels(tc.cfg, pids)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("孤儿清单 = %v, want %v", got, tc.want)
			}
			// 排序不变量：结果必须稳定有序（日志要能逐字复算，不随 map 迭代漂移）
			for i := 1; i < len(got); i++ {
				if got[i-1] > got[i] {
					t.Fatalf("结果未排序：%v", got)
				}
			}
		})
	}
}
