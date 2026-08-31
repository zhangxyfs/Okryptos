package deployx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v2.22.3", "v2.22.3", 0},
		{"v2.22.3", "v2.22.10", -1}, // 逐段数值比，不是字符串比
		{"v2.23.0", "v2.22.9", 1},
		{"v3.0.0", "v2.99.99", 1},
		{"latest", "v2.22.3", -1}, // 非标准串恒最小
		{"v2.22.3", "latest", 1},
		{"latest", "latest", 0},
		{"", "v0.0.1", -1},
	}
	for _, c := range cases {
		if got := compareSemver(c.a, c.b); got != c.want {
			t.Errorf("compareSemver(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestLatestImageTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"results":[{"name":"latest"},{"name":"v2.22.3"},{"name":"v2.22.10"},{"name":"v2.9.9"},{"name":"nightly"}]}`))
	}))
	defer srv.Close()
	got := latestImageTag(context.Background(), srv.URL)
	if got != "v2.22.10" {
		t.Fatalf("latestImageTag = %q, want v2.22.10", got)
	}
}

func TestLatestImageTagDegrade(t *testing.T) {
	// 404 / 坏 JSON / 无正式 tag → 空串（前端隐藏提示）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	if got := latestImageTag(context.Background(), srv.URL); got != "" {
		t.Fatalf("404 must degrade to empty, got %q", got)
	}
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"results":[{"name":"latest"}]}`))
	}))
	defer srv2.Close()
	if got := latestImageTag(context.Background(), srv2.URL); got != "" {
		t.Fatalf("no semver tag must be empty, got %q", got)
	}
}

func TestQueryRunningVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"version":"v2.22.3","initialized":true}`))
	}))
	defer srv.Close()
	if got := queryRunningVersion(context.Background(), srv.URL); got != "v2.22.3" {
		t.Fatalf("version = %q", got)
	}
	// 连接被拒 → 空串降级
	if got := queryRunningVersion(context.Background(), "http://127.0.0.1:1/api/v1/meta"); got != "" {
		t.Fatalf("unreachable must degrade to empty, got %q", got)
	}
}
