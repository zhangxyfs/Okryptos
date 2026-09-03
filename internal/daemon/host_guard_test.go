package daemon

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"okryptos/internal/gui"
)

// TestHostGuard 最外层 Host/Origin 防线（DNS rebinding / 跨站直连）：
// Host 必须本机回环；/api/* 带 Origin/Referer 时必须同源自回环；
// 非浏览器客户端（hook 进程）不带这两个头，不受影响。
func TestHostGuard(t *testing.T) {
	gh := gui.NewHandler(t.TempDir(), "tok", nil)
	srv := httptest.NewServer(NewMux(gh, "tok", "fp123"))
	defer srv.Close()

	req := func(path, host, origin, referer string) int {
		t.Helper()
		r, err := http.NewRequest("GET", srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("X-Ok-Token", "tok")
		if host != "" {
			r.Host = host
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if referer != "" {
			r.Header.Set("Referer", referer)
		}
		resp, err := srv.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// Host 非回环 → 403（rebinding 后浏览器 fetch 的 Host 仍是攻击域名）
	for _, host := range []string{"evil.com", "evil.com:17888", "192.168.1.1:17888"} {
		if code := req("/api/health", host, "", ""); code != http.StatusForbidden {
			t.Fatalf("Host=%s 应 403, got %d", host, code)
		}
	}
	// 回环 Host（含 localhost / [::1]，端口不限）→ 放行
	for _, host := range []string{"localhost:17888", "[::1]:17888"} {
		if code := req("/api/health", host, "", ""); code != http.StatusOK {
			t.Fatalf("Host=%s 应 200, got %d", host, code)
		}
	}
	// /api/* 带跨站 Origin/Referer → 403；同源自回环 → 放行
	if code := req("/api/health", "", "http://evil.com", ""); code != http.StatusForbidden {
		t.Fatalf("跨站 Origin 应 403, got %d", code)
	}
	if code := req("/api/health", "", "", "http://evil.com/page"); code != http.StatusForbidden {
		t.Fatalf("跨站 Referer 应 403, got %d", code)
	}
	if code := req("/api/health", "", "http://127.0.0.1:17888", ""); code != http.StatusOK {
		t.Fatalf("回环 Origin 应 200, got %d", code)
	}
	// hook 客户端形态：无 Origin/Referer → 放行
	if code := req("/api/health", "", "", ""); code != http.StatusOK {
		t.Fatalf("无 Origin/Referer 应 200, got %d", code)
	}
	// 永不输出 Access-Control-Allow-Origin
	r, _ := http.NewRequest("GET", srv.URL+"/api/health", nil)
	r.Header.Set("X-Ok-Token", "tok")
	r.Header.Set("Origin", "http://127.0.0.1:17888")
	resp, err := srv.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("不得输出 Access-Control-Allow-Origin")
	}
}
