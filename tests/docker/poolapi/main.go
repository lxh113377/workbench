// Command poolapi 驱动工作台面板的 /api/pool/* 接口（容器内验收用）。
//
// 为什么需要它：验收矩阵的 alpine 腿没有 curl / jq / python，
// 而「可配置管理界面」这条目标必须被真实 HTTP 调用证明，不能只靠单元测试。
//
// 用法：
//
//	poolapi <面板地址> <METHOD> <路径> [JSON体]
//	poolapi http://127.0.0.1:17890 GET /api/pool
//	poolapi http://127.0.0.1:17890 POST /api/pool/models '{"id":"a/b","model":"b",...}'
//
// 输出：
//
//	HTTP_STATUS=200
//	BODY=<原始响应体（单行）>
//
// token 从 /admin 页面里现取（服务端注入），因此不需要外部传密钥。
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

var tokenRe = regexp.MustCompile(`__WB_TOKEN__ = "([^"]+)"`)

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "用法: poolapi <面板地址> <METHOD> <路径> [JSON体]")
		os.Exit(2)
	}
	base := strings.TrimRight(os.Args[1], "/")
	method := strings.ToUpper(os.Args[2])
	path := os.Args[3]
	body := ""
	if len(os.Args) > 4 {
		body = os.Args[4]
	}

	client := &http.Client{Timeout: 60 * time.Second}
	// POOLAPI_BAD_TOKEN=1 时故意带错 token，用来验证「接口有鉴权围栏」。
	token := "bogus-token-for-fence-test"
	if os.Getenv("POOLAPI_BAD_TOKEN") != "1" {
		tok, err := fetchToken(client, base)
		if err != nil {
			fmt.Fprintln(os.Stderr, "取 token 失败: "+err.Error())
			fmt.Printf("HTTP_STATUS=0\nBODY=\n")
			os.Exit(1)
		}
		token = tok
	}

	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, base+path, rd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	req.Header.Set("X-WB-Token", token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "请求失败: "+err.Error())
		fmt.Printf("HTTP_STATUS=0\nBODY=\n")
		os.Exit(1)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	fmt.Printf("HTTP_STATUS=%d\n", resp.StatusCode)
	fmt.Printf("BODY=%s\n", strings.ReplaceAll(strings.TrimSpace(string(raw)), "\n", " "))
}

// fetchToken 从 /admin 页面里取出本次进程的服务端注入 token。
func fetchToken(c *http.Client, base string) (string, error) {
	resp, err := c.Get(base + "/admin?token=x")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	// 正则已把引号内的内容单独捕获，直接取用即可（token 是十六进制串，无需 JSON 解引号）。
	m := tokenRe.FindSubmatch(raw)
	if m == nil {
		return "", fmt.Errorf("页面里找不到 __WB_TOKEN__（面板未就绪？）")
	}
	return string(m[1]), nil
}
