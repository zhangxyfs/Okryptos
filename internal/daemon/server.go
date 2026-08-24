// Package daemon 是 ok 的常驻进程编排：HTTP mux、单实例运行、
// 后台拉起、hook 转发与 GUI 打开。
package daemon

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	"openknowledge/internal/hook"
)

// HookResponse 是 /api/hook/* 的响应：客户端据此还原 stdout/stderr 与退出码。
type HookResponse struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Code   int    `json:"code"`
}

// NewMux 组装 daemon 的全部路由：/api/health、/api/hook/* 由本包处理，
// 其余（GUI 静态页与管理 API）委托给 gh。
func NewMux(gh http.Handler, token, fingerprint string) http.Handler {
	mux := http.NewServeMux()
	auth := func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Ok-Token") != token {
				http.Error(w, `{"error":"缺少或错误的 X-Ok-Token"}`, http.StatusUnauthorized)
				return
			}
			fn(w, r)
		}
	}
	mux.HandleFunc("GET /api/health", auth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{"fingerprint": fingerprint})
	}))
	mux.HandleFunc("POST /api/hook/prompt", auth(hookHandler(func(body []byte, format string) HookResponse {
		var out strings.Builder
		code := hook.HandlePrompt(bytes.NewReader(body), &out, format)
		return HookResponse{Stdout: out.String(), Code: code}
	})))
	mux.HandleFunc("POST /api/hook/post-tool", auth(hookHandler(func(body []byte, _ string) HookResponse {
		return HookResponse{Code: hook.HandlePostTool(bytes.NewReader(body))}
	})))
	mux.HandleFunc("POST /api/hook/stop", auth(hookHandler(func(body []byte, format string) HookResponse {
		var out, errOut strings.Builder
		code := hook.HandleStop(bytes.NewReader(body), &errOut, &out, format)
		return HookResponse{Stdout: out.String(), Stderr: errOut.String(), Code: code}
	})))
	mux.HandleFunc("POST /api/hook/compact", auth(hookHandler(func(body []byte, _ string) HookResponse {
		return HookResponse{Code: hook.HandleCompact(bytes.NewReader(body))}
	})))
	mux.Handle("/", gh)
	return hostGuard(mux)
}

// hostGuard 最外层防线（DNS rebinding / 跨站直连）：Host 头必须是本机回环
// （127.0.0.1/localhost/[::1]，端口不限——默认端口被占时 daemon 回退随机端口）。
// daemon 虽只监听 127.0.0.1，但 rebinding 场景下浏览器把 evil.com 解析到回环后，
// 页面内 fetch 对浏览器是同源，Host 头却仍是 evil.com——卡死 Host 即掐断该链。
// /api/* 请求若带 Origin/Referer 头（浏览器跨站 fetch 必带 Origin）必须同源自
// 回环；非浏览器的 hook 客户端不带这两个头，不受影响。永不输出
// Access-Control-Allow-Origin（本 mux 任何分支都不设该头）。
func hostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			http.Error(w, `{"error":"非法 Host"}`, http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") &&
			(!loopbackOrigin(r.Header.Get("Origin")) || !loopbackOrigin(r.Header.Get("Referer"))) {
			http.Error(w, `{"error":"跨站请求被拒绝"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loopbackHost 判定 Host 头（可带端口）是否本机回环。
func loopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	switch strings.Trim(host, "[]") {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// loopbackOrigin 判定 Origin/Referer 头是否指向本机回环；空头放行
// （非浏览器客户端不发），非空必须是 http/https + 回环主机。
func loopbackOrigin(v string) bool {
	if v == "" {
		return true
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return loopbackHost(u.Host)
}

// hookHandler 把"读 body + format query → 业务函数 → HookResponse JSON"的模板收敛到一处。
// body 读失败（连接重置等）不能伪装成空 body 继续——客户端会以为 hook 正常执行（L-08），
// 记日志并 400。
func hookHandler(fn func([]byte, string) HookResponse) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			log.Printf("hook %s 读取请求体失败: %v", r.URL.Path, err)
			http.Error(w, `{"error":"读取请求体失败"}`, http.StatusBadRequest)
			return
		}
		writeJSON(w, fn(body, r.URL.Query().Get("format")))
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// newToken 生成 16 字节随机的 hex 令牌（32 字符）。
func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
