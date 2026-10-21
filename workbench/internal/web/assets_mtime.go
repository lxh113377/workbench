package web

import (
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// assetVersion 是静态资源的版本串（进程启动时间），用于 ETag。
//
// 为什么是「进程启动时间」而不是文件内容哈希：资源全部内嵌在 EXE 里，
// 每次发布都是一次编译，启动时间天然唯一且零 I/O 成本。
var assetVersion = strconv.FormatInt(time.Now().UnixNano(), 36)

var assetETagOnce sync.Once
var assetETagValue string

func assetETag() string {
	assetETagOnce.Do(func() { assetETagValue = `"v` + assetVersion + `"` })
	return assetETagValue
}

// serveAsset 统一处理静态资源响应：ETag/304 重校验 + 正确的 MIME。
//
// 缓存策略为什么是 no-cache + ETag 而不是 max-age=31536000, immutable（实测教训）：
//   - 第一次写成 immutable 是对的「性能」直觉，但它是本地单文件应用：升级 EXE 时
//     HTML 是 no-store（每次都新），静态资源 URL 不变。浏览器在 1 年内**不会**再发请求，
//     于是新 EXE 的 CSS/JS 永远不生效 —— 实测就撞上了：改完 CSS 后页面仍是旧样式，
//     而且读数是「缓存副本」而非当前代码，验收结论直接是假的。
//   - 改成 no-cache 后每次仍会发条件请求，命中 ETag 即 304（实测 304 + 0 字节正文），
//     带宽成本约等于零，但保证了「HTML 与资源永远同版本」。
//   - 静态资源全部内嵌、单机回环访问，瓶颈从来不在缓存头。
func serveAsset(w http.ResponseWriter, r *http.Request, contentType, body string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	etag := assetETag()
	w.Header().Set("ETag", etag)
	if r != nil && r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = io.WriteString(w, body)
}
