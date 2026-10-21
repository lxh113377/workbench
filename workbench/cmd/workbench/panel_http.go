package main

// panel_http.go：面板 HTTP 小助手（2026-10-19 现场补齐）。
//
// 为什么需要：runStopCLI 改造后要「先问运行中的实例，再决定是否写盘」，
// 这需要 4 个极小的原语：探活、取页面、抽 token、POST JSON。
//
// 设计约束：只用标准库（项目硬约束：单 EXE、离线、供应链干净）。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// httpGet 取一个 URL 的页面正文（超时 4s，避免 CLI 卡住）。
func httpGet(url string) (string, error) {
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return string(b), fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return string(b), nil
}

// wbTokenRe 匹配首页注入的 window.__WB_TOKEN__。
var wbTokenRe = regexp.MustCompile(`__WB_TOKEN__\s*=\s*"([^"]+)"`)

// extractWBTokens 从首页 HTML 里抽出令牌；抽不到返回空串。
func extractWBTokens(page string) string {
	m := wbTokenRe.FindStringSubmatch(page)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// httpPostJSON 带令牌 POST 一个 JSON 体（超时 10s，停机请求可能等 drain）。
func httpPostJSON(url, token string, body map[string]any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-WB-Token", token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// panelAlive 判断面板是否活着：GET 必须 2xx 才算（与「TCP 能连上」区分开）。
func panelAlive(url string) bool {
	if strings.TrimSpace(url) == "" {
		return false
	}
	_, err := httpGet(url)
	return err == nil
}
