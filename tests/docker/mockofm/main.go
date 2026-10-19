// Command mockofm 模拟 DeepSeek Harness 的 our-free-model 渠道网关，供容器内端到端验收使用。
//
// 为什么必须 mock（而不是连宿主真实的 8326）：
//   - 宿主网关绑 127.0.0.1，容器内的回环是容器自己的，连不到宿主；
//   - 真实网关的可用性受出口 IP 影响会波动，验收断言需要确定性；
//   - 「鉴权被拒」「上游 5xx」「清单正常」三种形态必须能按需切换。
//
// 模式切换走文件（$ROOT/mock-mode），这样 POSIX sh 里一句 printf 就能切，
// 容器内不需要 curl / python / jq。也保留 /ctl/mode 供人工调试。
//
// 纯标准库，零第三方依赖。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
)

// gatewayModels 是网关广播的模型 id 清单，刻意覆盖三类前缀形态：
//   - our-free-model/<id>        免费 / Kilo / EAC 车道（DSH provider 直接可调度）
//   - our-free-model-region/<id> 上游地区拦截（保留可见，默认不调度）
//   - deepseek-official/<model>  网关专属车道（只能经 OpenAI 网关调用）
var gatewayModels = []string{
	"our-free-model/z-ai/glm-5.3",
	"our-free-model/openai/gpt-oss-20b",
	"our-free-model/z-ai/glm-5.3-flash",
	"our-free-model/opencode/glm-5.3-flash-free",
	"our-free-model/kilo-auto/free",
	"our-free-model/nvidia/nemotron-3.5-super-vl:free",
	"our-free-model-region/beam-free",
	"deepseek-official/deepseek-flash",
}

// gateway 是 mock 的运行期状态。
type gateway struct {
	mu         sync.Mutex
	key        string
	mode       string
	modeFile   string
	modelsHits int
	chatHits   int
	unauth     int
}

// currentMode 读取当前模式：modefile 非空且可读时以文件为准（每次请求现读）。
func (g *gateway) currentMode() string {
	g.mu.Lock()
	file := g.modeFile
	fallback := g.mode
	g.mu.Unlock()
	if file != "" {
		if b, err := os.ReadFile(file); err == nil {
			if m := strings.TrimSpace(string(b)); m != "" {
				return m
			}
		}
	}
	return fallback
}

// authorized 校验 Bearer 或 X-API-Key。
func (g *gateway) authorized(r *http.Request) bool {
	g.mu.Lock()
	want := g.key
	g.mu.Unlock()
	if want == "" {
		return true
	}
	got := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		got = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	} else {
		got = strings.TrimSpace(r.Header.Get("X-API-Key"))
	}
	return got == want
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func openAIErr(w http.ResponseWriter, code int, msg, kind string) {
	writeJSON(w, code, map[string]any{"error": map[string]any{"message": msg, "type": kind, "code": kind}})
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8326", "监听地址（容器内回环）")
	key := flag.String("key", "", "期望的 Bearer key；空表示不校验")
	mode := flag.String("mode", "normal", "初始模式：normal|unauthorized|down")
	modeFile := flag.String("modefile", "", "模式文件路径；每次请求现读，非空时优先于 -mode")
	flag.Parse()

	g := &gateway{key: *key, mode: *mode, modeFile: *modeFile}

	mux := http.NewServeMux()

	// 存活探针（不带鉴权，供脚本等待就绪）。
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": g.currentMode()})
	})

	// 模型清单：探测主路径。
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		m := g.currentMode()
		g.mu.Lock()
		g.modelsHits++
		g.mu.Unlock()
		if m == "down" {
			openAIErr(w, http.StatusBadGateway, "mock upstream failure", "upstream_failed")
			return
		}
		if m == "unauthorized" || !g.authorized(r) {
			g.mu.Lock()
			g.unauth++
			g.mu.Unlock()
			openAIErr(w, http.StatusUnauthorized, "missing or invalid API key", "invalid_request_error")
			return
		}
		data := make([]map[string]any, 0, len(gatewayModels))
		for _, id := range gatewayModels {
			data = append(data, map[string]any{"id": id, "object": "model", "owned_by": strings.SplitN(id, "/", 2)[0]})
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
	})

	// 最小补全：供 deepProbe 与人工验证。
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		m := g.currentMode()
		g.mu.Lock()
		g.chatHits++
		g.mu.Unlock()
		if m == "down" {
			openAIErr(w, http.StatusBadGateway, "mock upstream failure", "upstream_failed")
			return
		}
		if m == "unauthorized" || !g.authorized(r) {
			openAIErr(w, http.StatusUnauthorized, "missing or invalid API key", "invalid_request_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "mock-completion", "object": "chat.completion", "model": "mock",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "pong"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	})

	// 运行期改模式（人工调试用；脚本走 modefile）。
	mux.HandleFunc("/ctl/mode", func(w http.ResponseWriter, r *http.Request) {
		if m := r.URL.Query().Get("mode"); m != "" {
			g.mu.Lock()
			g.mode = m
			g.mu.Unlock()
		}
		writeJSON(w, http.StatusOK, map[string]any{"mode": g.currentMode()})
	})

	// 计数（断言"探测确实打了请求"）。
	//
	// 注意锁顺序：currentMode() 自己会加锁，必须先在外面取好 mode 再进临界区。
	// 早先版本在持有 g.mu 时调用它，sync.Mutex 不可重入 ⇒ 该请求永久挂起
	// （表现为 httpget 超时），与项目记忆里 chainBonus 持写锁再取读锁同形。
	mux.HandleFunc("/ctl/state", func(w http.ResponseWriter, r *http.Request) {
		mode := g.currentMode()
		g.mu.Lock()
		out := map[string]any{
			"mode": mode, "key": g.key != "",
			"modelsHits": g.modelsHits, "chatHits": g.chatHits, "unauthorizedHits": g.unauth,
			"models": len(gatewayModels),
		}
		g.mu.Unlock()
		writeJSON(w, http.StatusOK, out)
	})

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "MOCKOFM_FAILED: "+err.Error())
		os.Exit(1)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()

	// 就绪标记：脚本据此等待，避免竞态。
	fmt.Printf("MOCKOFM_READY addr=%s mode=%s models=%d key=%v\n", *addr, *mode, len(gatewayModels), *key != "")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	_ = srv.Close()
}
