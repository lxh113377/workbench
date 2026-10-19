// Command httpget 是最小 HTTP 客户端，供容器内断言使用。
//
// 为什么不用 wget / curl（实测两条都踩坑）：
//   - debian:bookworm-slim 既没有 curl 也没有 wget；
//   - alpine 有 busybox wget，但它对 4xx/5xx 只给非 0 退出码 ——
//     工具缺失（退出码 127）和"请求被拒"（退出码 1）会撞成同一个值，
//     于是"应当被拒"的断言在缺工具时**假绿**。
// 静态 Go 二进制在两个发行版上行为逐字节一致，且能拿到真实状态码。
//
// 用法：httpget <url> [bearer-key]
// 输出：HTTP_STATUS=200（连不上时 000），BODY=（可选，加 -body）
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: httpget <url> [bearer-key] [-body] [-post JSON]")
		os.Exit(2)
	}
	url := os.Args[1]
	key := ""
	if len(os.Args) > 2 && os.Args[2] != "-body" && os.Args[2] != "-post" {
		key = os.Args[2]
	}
	showBody := false
	var postBody *strings.Reader
	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-body":
			showBody = true
		case "-post":
			if i+1 < len(os.Args) {
				postBody = strings.NewReader(os.Args[i+1])
				i++
			}
		}
	}

	method := http.MethodGet
	var rd io.Reader
	if postBody != nil {
		method = http.MethodPost
		rd = postBody
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		fmt.Printf("HTTP_STATUS=000\nERR=%s\n", err.Error())
		os.Exit(1)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if postBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("HTTP_STATUS=000\nERR=%s\n", err.Error())
		os.Exit(1)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	fmt.Printf("HTTP_STATUS=%d\n", resp.StatusCode)
	if showBody {
		fmt.Printf("BODY=%s\n", strings.ReplaceAll(strings.TrimSpace(string(raw)), "\n", " "))
	}
}
