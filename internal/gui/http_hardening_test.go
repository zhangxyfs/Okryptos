package gui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLLMTestKeyFallbackPinnedToSavedBaseURL /api/llm/test 回查已存真实 key 时，
// base_url 必须与已存值一致——否则拿到 token 的攻击方可把 config.toml 里的
// 真实 key 以 Bearer 头送往任意服务器（密钥外传）。改 URL 必须显式重填 key。
func TestLLMTestKeyFallbackPinnedToSavedBaseURL(t *testing.T) {
	h, _, _ := newEnv(t)
	var gotAuth string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"p"}}]}`)
	}))
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "s", "kind": "openai", "base_url": fake.URL, "model": "m", "api_key": "sk-real", "activate": true,
	})
	if code != 200 {
		t.Fatalf("save profile: %d", code)
	}

	// key 留空 + base_url 指向别处 → 400（不得回查真实 key 发出去）
	for _, key := range []string{"", llmKeyMask} {
		code, _ = do(t, "POST", srv.URL+"/api/llm/test", testToken, map[string]any{
			"name": "s", "kind": "openai", "base_url": "http://evil.example.com", "model": "m", "api_key": key,
		})
		if code != 400 {
			t.Fatalf("key=%q 换地址应 400, got %d", key, code)
		}
	}

	// base_url 与已存一致 → 回查照旧工作，真实 key 只发往已存地址
	code, _ = do(t, "POST", srv.URL+"/api/llm/test", testToken, map[string]any{
		"name": "s", "kind": "openai", "base_url": fake.URL, "model": "m",
	})
	if code != 200 {
		t.Fatalf("同地址回查应 200, got %d", code)
	}
	if gotAuth != "Bearer sk-real" {
		t.Fatalf("回查应带出真实 key: %q", gotAuth)
	}

	// 非 http/https scheme 一律拒绝（显式带 key 也不行）
	code, _ = do(t, "POST", srv.URL+"/api/llm/test", testToken, map[string]any{
		"name": "s", "kind": "openai", "base_url": "ftp://x.example.com", "model": "m", "api_key": "k",
	})
	if code != 400 {
		t.Fatalf("非 http(s) scheme 应 400, got %d", code)
	}
}

// TestEmbeddingTestKeyFallbackPinnedToSavedBaseURL embedding test 端点同款防线：
// 回查已存 key 要求 base_url 一致；非 builtin 的 base_url 必须 http/https。
func TestEmbeddingTestKeyFallbackPinnedToSavedBaseURL(t *testing.T) {
	h, _, _ := newEnv(t)
	var gotAuth string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"data":[{"embedding":[0.1,0.2],"index":0}]}`)
	}))
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/setup/embedding/profile", testToken, map[string]any{
		"name": "e", "type": "openai", "base_url": fake.URL, "model": "m", "api_key": "sk-real",
	})
	if code != 200 {
		t.Fatalf("save profile: %d", code)
	}

	code, _ = do(t, "POST", srv.URL+"/api/setup/embedding/test", testToken, map[string]any{
		"name": "e", "type": "openai", "base_url": "http://evil.example.com", "model": "m",
	})
	if code != 400 {
		t.Fatalf("换地址回查应 400, got %d", code)
	}

	code, data := do(t, "POST", srv.URL+"/api/setup/embedding/test", testToken, map[string]any{
		"name": "e", "type": "openai", "base_url": fake.URL, "model": "m",
	})
	if code != 200 || !strings.Contains(string(data), `"ok":true`) {
		t.Fatalf("同地址回查应 ok, got %d %s", code, data)
	}
	if gotAuth != "Bearer sk-real" {
		t.Fatalf("回查应带出真实 key: %q", gotAuth)
	}

	code, _ = do(t, "POST", srv.URL+"/api/setup/embedding/test", testToken, map[string]any{
		"name": "e2", "type": "openai", "base_url": "gopher://x", "model": "m", "api_key": "k",
	})
	if code != 400 {
		t.Fatalf("非 http(s) scheme 应 400, got %d", code)
	}
}

// TestOllamaModelsLoopbackOnly ollama-models 代理只放行本机回环目标——
// 该端点 GET 目标并回读响应，放任任意地址即成内网/云元数据探测口。
func TestOllamaModelsLoopbackOnly(t *testing.T) {
	h, _, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	for _, base := range []string{
		"http://169.254.169.254", "http://evil.example.com", "file:///etc/passwd",
	} {
		code, _ := do(t, "GET", srv.URL+"/api/setup/embedding/ollama-models?base_url="+base, testToken, nil)
		if code != 400 {
			t.Fatalf("base_url=%s 应 400, got %d", base, code)
		}
	}
	// 回环目标（无服务监听，连接被拒）→ 200 且 error 字段说明原因
	code, data := do(t, "GET", srv.URL+"/api/setup/embedding/ollama-models?base_url=http://127.0.0.1:1", testToken, nil)
	if code != 200 || !strings.Contains(string(data), `"error"`) {
		t.Fatalf("回环目标应 200 带 error 字段, got %d %s", code, data)
	}
}

// TestDecodeJSONBodyLimit JSON 端点统一 4MB 请求体上限。
func TestDecodeJSONBodyLimit(t *testing.T) {
	h, _, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/hooks/timeout", testToken, map[string]any{
		"timeout_sec": 20, "pad": strings.Repeat("x", 5<<20),
	})
	if code != 400 {
		t.Fatalf("超 4MB 应 400, got %d", code)
	}
	// 正常小 body 不受影响
	code, _ = do(t, "POST", srv.URL+"/api/hooks/timeout", testToken, map[string]any{"timeout_sec": 20})
	if code != 200 {
		t.Fatalf("正常 body 应 200, got %d", code)
	}
}

// TestExportRejectsTraversalName /api/export 与其它端点同款 validProjectName
// 校验：注册表被毒化时，穿越段名字不得进入 projects/<name>/ 拼路径。
func TestExportRejectsTraversalName(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	registerRawName(t, "../escape")
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "GET", srv.URL+"/api/export?project=..%2Fescape", testToken, nil)
	if code != 400 {
		t.Fatalf("穿越项目名应 400, got %d", code)
	}
	code, _ = do(t, "GET", srv.URL+"/api/export?project=demo", testToken, nil)
	if code != 200 {
		t.Fatalf("正常项目导出应 200, got %d", code)
	}
}
