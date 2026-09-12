package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"okryptos/internal/chatx"
)

// TestLLMRoundTrip llm 配置的 保存/掩码回显/掩码提交保留原 key/active 切换/删除置空。
func TestLLMRoundTrip(t *testing.T) {
	h, _, okHome := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	cfgPath := filepath.Join(okHome, "config.toml")

	// 默认空配置
	code, data := do(t, "GET", srv.URL+"/api/llm", testToken, nil)
	if code != 200 {
		t.Fatalf("llm get: %d", code)
	}
	var view struct {
		Active   string `json:"active"`
		Profiles []struct {
			Name   string `json:"name"`
			APIKey string `json:"api_key"`
			Active bool   `json:"active"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	if view.Active != "" || len(view.Profiles) != 0 {
		t.Fatalf("默认应为空: %+v", view)
	}

	// 保存并激活
	code, _ = do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "测试服务", "kind": "openai", "base_url": "https://x.example.com/v1",
		"model": "m1", "api_key": "sk-real", "activate": true,
	})
	if code != 200 {
		t.Fatalf("save: %d", code)
	}
	cfgData, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(cfgData), "sk-real") {
		t.Fatalf("key 应落盘: %q", cfgData)
	}

	// GET 掩码不回明文
	_, data = do(t, "GET", srv.URL+"/api/llm", testToken, nil)
	if strings.Contains(string(data), "sk-real") {
		t.Fatalf("GET 不得回明文 key: %s", data)
	}
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	if view.Active != "测试服务" || len(view.Profiles) != 1 || view.Profiles[0].APIKey != llmKeyMask || !view.Profiles[0].Active {
		t.Fatalf("回显不对: %+v", view)
	}

	// 掩码提交（改 model + 高级参数）→ 原 key 保留
	code, _ = do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "测试服务", "kind": "openai", "base_url": "https://x.example.com/v1",
		"model": "m2", "api_key": llmKeyMask, "temperature": "0.7", "max_tokens": 512,
	})
	if code != 200 {
		t.Fatalf("re-save: %d", code)
	}
	cfgData, _ = os.ReadFile(cfgPath)
	if !strings.Contains(string(cfgData), "sk-real") || !strings.Contains(string(cfgData), `model = "m2"`) ||
		!strings.Contains(string(cfgData), `temperature = "0.7"`) || !strings.Contains(string(cfgData), "max_tokens = 512") {
		t.Fatalf("掩码提交应保留原 key 并更新 model/高级参数: %q", cfgData)
	}

	// 非法 kind → 400
	code, _ = do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "x", "kind": "gemini", "base_url": "u", "model": "m",
	})
	if code != 400 {
		t.Fatalf("非法 kind 应 400, got %d", code)
	}

	// 非法高级参数 → 400
	code, _ = do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "x", "kind": "openai", "base_url": "u", "model": "m", "temperature": "abc",
	})
	if code != 400 {
		t.Fatalf("非法 temperature 应 400, got %d", code)
	}
	code, _ = do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "x", "kind": "openai", "base_url": "u", "model": "m", "max_tokens": -1,
	})
	if code != 400 {
		t.Fatalf("负 max_tokens 应 400, got %d", code)
	}

	// 删除使用中 → active 置空
	code, _ = do(t, "POST", srv.URL+"/api/llm/delete", testToken, map[string]any{"name": "测试服务"})
	if code != 200 {
		t.Fatalf("delete: %d", code)
	}
	_, data = do(t, "GET", srv.URL+"/api/llm", testToken, nil)
	_ = json.Unmarshal(data, &view)
	if view.Active != "" || len(view.Profiles) != 0 {
		t.Fatalf("删除使用中应置空 active: %+v", view)
	}
}

// TestLLMProfileSaveOllama ollama 档：base_url 可留空、免 api_key。
// POST 应 200（非 400），GET 回显的 profiles 含 kind=ollama。
func TestLLMProfileSaveOllama(t *testing.T) {
	h, _, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, data := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "本地", "kind": "ollama", "base_url": "", "model": "qwen3:1.7b", "activate": false,
	})
	if code != 200 {
		t.Fatalf("ollama 档应放行: %d %s", code, data)
	}

	var view struct {
		Profiles []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		} `json:"profiles"`
	}
	_, data = do(t, "GET", srv.URL+"/api/llm", testToken, nil)
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Profiles) != 1 || view.Profiles[0].Name != "本地" || view.Profiles[0].Kind != "ollama" {
		t.Fatalf("GET 应回显 ollama profile: %+v", view)
	}
}

// TestLLMProfileSaveFilterFlag 识别意图标记随保存回显：POST 带 filter=true，
// GET 该 profile filter=true。
func TestLLMProfileSaveFilterFlag(t *testing.T) {
	h, _, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, data := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "意图", "kind": "openai", "base_url": "http://x", "model": "m", "filter": true,
	})
	if code != 200 {
		t.Fatalf("save: %d %s", code, data)
	}

	var view struct {
		Profiles []struct {
			Name   string `json:"name"`
			Filter bool   `json:"filter"`
		} `json:"profiles"`
	}
	_, data = do(t, "GET", srv.URL+"/api/llm", testToken, nil)
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Profiles) != 1 || view.Profiles[0].Name != "意图" || !view.Profiles[0].Filter {
		t.Fatalf("filter 标记应随保存回显: %+v", view)
	}
}

// TestLLMActiveSlots 双槽位：slot=filter 走 active_filter 槽且不动普通槽；
// 空 name 清对应槽。
func TestLLMActiveSlots(t *testing.T) {
	h, _, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "s", "kind": "openai", "base_url": "https://x.example.com/v1",
		"model": "m", "api_key": "k", "activate": true,
	})
	if code != 200 {
		t.Fatalf("save general: %d", code)
	}
	code, _ = do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "意图", "kind": "openai", "base_url": "http://x", "model": "m", "filter": true,
	})
	if code != 200 {
		t.Fatalf("save filter profile: %d", code)
	}

	var view struct {
		Active       string `json:"active"`
		ActiveFilter string `json:"active_filter"`
	}

	// slot=filter → active_filter 落槽，active 不变
	code, data := do(t, "POST", srv.URL+"/api/llm/active", testToken, map[string]any{
		"name": "意图", "slot": "filter",
	})
	if code != 200 {
		t.Fatalf("active slot=filter: %d %s", code, data)
	}
	_, data = do(t, "GET", srv.URL+"/api/llm", testToken, nil)
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	if view.ActiveFilter != "意图" || view.Active != "s" {
		t.Fatalf("active_filter 应落槽且 active 不变: %+v", view)
	}

	// 空 name + slot=filter → 清空该槽，active 仍不变
	code, _ = do(t, "POST", srv.URL+"/api/llm/active", testToken, map[string]any{
		"name": "", "slot": "filter",
	})
	if code != 200 {
		t.Fatalf("clear slot=filter: %d", code)
	}
	_, data = do(t, "GET", srv.URL+"/api/llm", testToken, nil)
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	if view.ActiveFilter != "" || view.Active != "s" {
		t.Fatalf("空 name 应清 active_filter 槽: %+v", view)
	}
}

// TestLLMTestEndpoint 连通性测试接口：假 openai 服务 200，错误地址 502。
func TestLLMTestEndpoint(t *testing.T) {
	h, _, _ := newEnv(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "p"}}},
		})
	}))
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/llm/test", testToken, map[string]any{
		"kind": "openai", "base_url": fake.URL, "model": "m", "api_key": "k",
	})
	if code != 200 {
		t.Fatalf("假服务应 200, got %d", code)
	}
	code, _ = do(t, "POST", srv.URL+"/api/llm/test", testToken, map[string]any{
		"kind": "openai", "base_url": "http://127.0.0.1:1", "model": "m", "api_key": "k",
	})
	if code != 502 {
		t.Fatalf("连不上应 502, got %d", code)
	}
}

// TestEntryOptimizeNoLLM 无 active 配置 → 409 no_llm。
func TestEntryOptimizeNoLLM(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	srv := httptest.NewServer(h)
	defer srv.Close()
	code, data := do(t, "POST", srv.URL+"/api/entry/optimize", testToken, map[string]any{
		"project": "demo", "file": "a.md", "title": "t", "body": "正文内容",
	})
	if code != 409 || !strings.Contains(string(data), "no_llm") {
		t.Fatalf("应 409 no_llm, got %d %s", code, data)
	}
}

// TestEntryOptimizeOK 全链路：假 LLM 返回带围栏 JSON → 解析回填字段；盘上 .md 不被改写。
func TestEntryOptimizeOK(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")
	kdir := filepath.Join(okHome, "projects", "demo", "knowledge")
	entryPath := filepath.Join(kdir, "a.md")
	if err := os.WriteFile(entryPath, []byte("---\ntitle: 旧标题\n---\n\n旧正文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(entryPath)
	mtimeBefore := fi.ModTime()

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(raw)
		if !strings.Contains(string(raw), "旧正文") {
			t.Errorf("请求应携带条目正文: %s", raw)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{
				"content": "```json\n{\"title\":\"新标题\",\"tags\":[\"x\"],\"summary\":\"新摘要\",\"body\":\"新正文\"}\n```",
			}}},
			"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 42},
		})
	}))
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 配置 active profile 指向假服务（走真实保存链路）
	code, _ := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "s", "kind": "openai", "base_url": fake.URL, "model": "m", "api_key": "k", "activate": true,
	})
	if code != 200 {
		t.Fatalf("save profile: %d", code)
	}

	code, data := do(t, "POST", srv.URL+"/api/entry/optimize", testToken, map[string]any{
		"project": "demo", "file": "a.md", "title": "旧标题", "body": "旧正文",
	})
	if code != 200 {
		t.Fatalf("optimize: %d, %s", code, data)
	}
	var out struct {
		Title string   `json:"title"`
		Tags  []string `json:"tags"`
		Body  string   `json:"body"`
		Usage struct {
			Prompt     int `json:"prompt"`
			Completion int `json:"completion"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Title != "新标题" || len(out.Tags) != 1 || out.Body != "新正文" {
		t.Fatalf("围栏 JSON 应被剥壳解析: %+v", out)
	}
	if out.Usage.Prompt != 100 || out.Usage.Completion != 42 {
		t.Fatalf("usage 应透传: %+v", out.Usage)
	}

	// 优化不写盘：.md 内容与 mtime 均不变
	fi2, _ := os.Stat(entryPath)
	content, _ := os.ReadFile(entryPath)
	if !fi2.ModTime().Equal(mtimeBefore) || !strings.Contains(string(content), "旧正文") {
		t.Fatalf("optimize 不得写盘: mtime %v→%v", mtimeBefore, fi2.ModTime())
	}

	// 调用记录落 ok.log（GUI 日志页 ok 来源）：开始 + 回答 + 结果
	logData, _ := os.ReadFile(filepath.Join(okHome, "ok.log"))
	for _, want := range []string{"optimize 开始", "optimize 回答", "optimize 结果: ok"} {
		if !strings.Contains(string(logData), want) {
			t.Fatalf("ok.log 缺少 %q: %q", want, logData)
		}
	}
}

// TestEntryOptimizeNoChange 模型自报 no_change 或逐字段原样返回时，接口回
// {no_change:true}（前端据此提示「无需优化」），且不写盘。
func TestEntryOptimizeNoChange(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")

	// 模型没听指令、原样返回 → 兜底判定 no_change
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{
				"content": `{"title":"旧标题","tags":["hook","设计裁决"],"summary":"旧摘要","body":"旧正文"}`,
			}}},
		})
	}))
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "s", "kind": "openai", "base_url": fake.URL, "model": "m", "api_key": "k", "activate": true,
	})
	if code != 200 {
		t.Fatalf("save profile: %d", code)
	}
	code, data := do(t, "POST", srv.URL+"/api/entry/optimize", testToken, map[string]any{
		"project": "demo", "title": "旧标题", "tags": "hook, 设计裁决", "summary": "旧摘要", "body": "旧正文",
	})
	if code != 200 {
		t.Fatalf("optimize: %d, %s", code, data)
	}
	if !strings.Contains(string(data), `"no_change":true`) {
		t.Fatalf("逐字段相同应判定 no_change: %s", data)
	}
}

// TestEntryOptimizeNoChangeFuzzy 语义级相同也判 no_change：全半角标点差异、
// 多余换行/空格、tags 顺序变化都不算有效优化。
func TestEntryOptimizeNoChangeFuzzy(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{
				// 与表单值仅差：全半角标点（《》/……/～/％/：）、多余换行、tags 乱序
				"content": `{"title":"旧标题","tags":["设计裁决","hook"],"summary":"旧摘要《v2》……","body":"旧正文：\n\n第2行～50％"}`,
			}}},
		})
	}))
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "s", "kind": "openai", "base_url": fake.URL, "model": "m", "api_key": "k", "activate": true,
	})
	if code != 200 {
		t.Fatalf("save profile: %d", code)
	}
	code, data := do(t, "POST", srv.URL+"/api/entry/optimize", testToken, map[string]any{
		"project": "demo", "title": "旧标题", "tags": "hook, 设计裁决", "summary": "旧摘要<v2>...", "body": "旧正文: 第2行~50%",
	})
	if code != 200 {
		t.Fatalf("optimize: %d, %s", code, data)
	}
	if !strings.Contains(string(data), `"no_change":true`) {
		t.Fatalf("标点/空白/tags 顺序差异应判定 no_change: %s", data)
	}

	// 对照组：body 有实质文字差异 → 不判 no_change
	code, data = do(t, "POST", srv.URL+"/api/entry/optimize", testToken, map[string]any{
		"project": "demo", "title": "旧标题", "tags": "hook, 设计裁决", "summary": "旧摘要<v2>...", "body": "旧正文: 第三行",
	})
	if code != 200 || strings.Contains(string(data), `"no_change":true`) {
		t.Fatalf("实质差异不应判定 no_change: %d %s", code, data)
	}
}

// TestEntryOptimizeReasoningFallback 推理模型把答案 JSON 整个吐进 reasoning_content、
// content 留空时（2026-08-19 deepseek-v4-flash 实证），从 reasoning 兜底提取完整
// JSON 照常解析，不再报「非合法 JSON」。
func TestEntryOptimizeReasoningFallback(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"finish_reason": "stop",
				"message": map[string]string{
					"content": "",
					"reasoning_content": "逐条分析后结论如下：\n" +
						`{"title":"新标题","tags":["x"],"summary":"新摘要","body":"新正文"}`,
				},
			}},
		})
	}))
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "s", "kind": "openai", "base_url": fake.URL, "model": "m", "api_key": "k", "activate": true,
	})
	if code != 200 {
		t.Fatalf("save profile: %d", code)
	}
	code, data := do(t, "POST", srv.URL+"/api/entry/optimize", testToken, map[string]any{
		"project": "demo", "file": "a.md", "title": "旧标题", "body": "旧正文",
	})
	if code != 200 {
		t.Fatalf("reasoning 兜底应 200, got %d %s", code, data)
	}
	var out struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Title != "新标题" || out.Body != "新正文" {
		t.Fatalf("应从 reasoning 提取 JSON: %+v", out)
	}
	logData, _ := os.ReadFile(filepath.Join(okHome, "ok.log"))
	if !strings.Contains(string(logData), "optimize 兜底") {
		t.Fatalf("ok.log 应记兜底提取: %q", logData)
	}
}

// TestEntryOptimizeTruncated finish_reason=length（max_tokens 触顶）时报错须点明
// 截断与当前生效上限（profile 未配时为调用方默认 8192），不再笼统「非合法 JSON」。
func TestEntryOptimizeTruncated(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"finish_reason": "length",
				"message":       map[string]string{"content": `{"title":"半截`},
			}},
		})
	}))
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "s", "kind": "openai", "base_url": fake.URL, "model": "m", "api_key": "k", "activate": true,
	})
	if code != 200 {
		t.Fatalf("save profile: %d", code)
	}
	code, data := do(t, "POST", srv.URL+"/api/entry/optimize", testToken, map[string]any{
		"project": "demo", "file": "a.md", "title": "旧标题", "body": "旧正文",
	})
	if code != 502 {
		t.Fatalf("截断应 502, got %d %s", code, data)
	}
	if !strings.Contains(string(data), "截断") || !strings.Contains(string(data), "8192") {
		t.Fatalf("报错须点明截断与生效上限: %s", data)
	}
	logData, _ := os.ReadFile(filepath.Join(okHome, "ok.log"))
	if !strings.Contains(string(logData), "被截断") {
		t.Fatalf("ok.log 应记截断: %q", logData)
	}
}

// TestEntryOptimizeTruncatedNoFinishReason 网关不回 finish_reason 时的兜底判定：
// 解析失败且 completion 打满生效上限（默认 8192）同样报截断，不落「非合法 JSON」。
func TestEntryOptimizeTruncatedNoFinishReason(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			// 无 finish_reason 字段（部分第三方网关如此），但 completion 钉在上限
			"choices": []map[string]any{{
				"message": map[string]string{"content": `{"title":"半截`},
			}},
			"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 8192},
		})
	}))
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "s", "kind": "openai", "base_url": fake.URL, "model": "m", "api_key": "k", "activate": true,
	})
	if code != 200 {
		t.Fatalf("save profile: %d", code)
	}
	code, data := do(t, "POST", srv.URL+"/api/entry/optimize", testToken, map[string]any{
		"project": "demo", "file": "a.md", "title": "旧标题", "body": "旧正文",
	})
	if code != 502 {
		t.Fatalf("截断应 502, got %d %s", code, data)
	}
	if !strings.Contains(string(data), "截断") || strings.Contains(string(data), "非合法 JSON") {
		t.Fatalf("completion 打满上限应报截断而非「非合法 JSON」: %s", data)
	}
	logData, _ := os.ReadFile(filepath.Join(okHome, "ok.log"))
	if !strings.Contains(string(logData), "被截断") {
		t.Fatalf("ok.log 应记截断: %q", logData)
	}
}

// TestEntryOptimizeBelowCapNotTruncated 对照组：completion 远低于上限时解析失败
// 仍报「非合法 JSON」（不误报截断）。
func TestEntryOptimizeBelowCapNotTruncated(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "demo")

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"finish_reason": "stop",
				"message":       map[string]string{"content": "这不是 JSON"},
			}},
			"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 50},
		})
	}))
	defer fake.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "s", "kind": "openai", "base_url": fake.URL, "model": "m", "api_key": "k", "activate": true,
	})
	if code != 200 {
		t.Fatalf("save profile: %d", code)
	}
	code, data := do(t, "POST", srv.URL+"/api/entry/optimize", testToken, map[string]any{
		"project": "demo", "file": "a.md", "title": "旧标题", "body": "旧正文",
	})
	if code != 502 || !strings.Contains(string(data), "非合法 JSON") {
		t.Fatalf("未打满上限的乱输出仍应报「非合法 JSON」: %d %s", code, data)
	}
}

// TestLLMMaxTokensEndpoint 模型配置卡「最大 token」单字段保存：只改使用中 profile
// 的 max_tokens，其他高级参数（temperature）不动；0=恢复默认；越界 400；
// 无使用中 profile 409。
func TestLLMMaxTokensEndpoint(t *testing.T) {
	h, _, _ := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, _ := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "s", "kind": "openai", "base_url": "https://x.example.com/v1",
		"model": "m", "api_key": "sk-real", "temperature": "0.7", "activate": true,
	})
	if code != 200 {
		t.Fatalf("save profile: %d", code)
	}

	var view struct {
		Profiles []struct {
			MaxTokens   int    `json:"max_tokens"`
			Temperature string `json:"temperature"`
		} `json:"profiles"`
	}

	// 保存 32768（5 位数上限场景）→ GET 回显；temperature 不受影响
	code, _ = do(t, "POST", srv.URL+"/api/llm/max-tokens", testToken, map[string]any{"max_tokens": 32768})
	if code != 200 {
		t.Fatalf("max-tokens save: %d", code)
	}
	_, data := do(t, "GET", srv.URL+"/api/llm", testToken, nil)
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Profiles) != 1 || view.Profiles[0].MaxTokens != 32768 {
		t.Fatalf("max_tokens 应落盘: %+v", view)
	}
	if view.Profiles[0].Temperature != "0.7" {
		t.Fatalf("单字段保存不得动 temperature: %+v", view)
	}

	// 0 = 恢复默认（调用方值）
	code, _ = do(t, "POST", srv.URL+"/api/llm/max-tokens", testToken, map[string]any{"max_tokens": 0})
	if code != 200 {
		t.Fatalf("置 0 应合法: %d", code)
	}

	// 越界 → 400
	for _, bad := range []int{-1, 100, 200000} {
		code, _ = do(t, "POST", srv.URL+"/api/llm/max-tokens", testToken, map[string]any{"max_tokens": bad})
		if code != 400 {
			t.Fatalf("max_tokens=%d 应 400, got %d", bad, code)
		}
	}

	// 删除使用中 profile 后 → 409
	code, _ = do(t, "POST", srv.URL+"/api/llm/delete", testToken, map[string]any{"name": "s"})
	if code != 200 {
		t.Fatalf("delete: %d", code)
	}
	code, _ = do(t, "POST", srv.URL+"/api/llm/max-tokens", testToken, map[string]any{"max_tokens": 8192})
	if code != 409 {
		t.Fatalf("无使用中 profile 应 409, got %d", code)
	}
}

// TestExcerptLines 行号窗口截取：带行号取窗口、无行号取头部、越界钳制。
func TestExcerptLines(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		sb.WriteString("line\n")
	}
	src := sb.String()
	if got := excerptLines(src, "50-52", nil); len(strings.Split(strings.TrimRight(got, "\n"), "\n")) != 13 { // 45..57
		t.Fatalf("窗口行数不对: %d", len(strings.Split(got, "\n")))
	}
	if got := excerptLines(src, "", nil); len(strings.Split(strings.TrimRight(got, "\n"), "\n")) != 80 {
		t.Fatalf("无行号应取头 80 行")
	}
	if got := excerptLines("a\nb\n", "99", nil); !strings.Contains(got, "a") {
		t.Fatalf("越界应钳制: %q", got)
	}
}

// TestExcerptLinesHints 无行号时：头 80 行之外，正文中反引号标识符的深处命中
// 行窗口（±5）补进摘录；已在头部覆盖的不重复；同一标识符只取首个命中。
func TestExcerptLinesHints(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 200; i++ {
		sb.WriteString("line\n")
	}
	sb.WriteString("") // 占位保持行号
	lines := strings.Split(strings.TrimRight(sb.String(), "\n"), "\n")
	lines[119] = "if old == mtime { // L120 深处的关键判定" // 索引 119 = 第 120 行
	src := strings.Join(lines, "\n")

	got := excerptLines(src, "", []string{"old == mtime"})
	if !strings.Contains(got, "old == mtime") || !strings.Contains(got, "L115-125") {
		t.Fatalf("深处命中行窗口应补进摘录: 尾部 %q", got[len(got)-200:])
	}
	// 命中行在头 80 行内时不重复追加
	lines[9] = "head hit: old == mtime // L10"
	got = excerptLines(strings.Join(lines, "\n"), "", []string{"old == mtime"})
	if !strings.Contains(got, "head hit: old == mtime") || strings.Count(got, "if old == mtime {") != 1 {
		t.Fatalf("头部命中不重复、深处命中保留: %q", got[len(got)-260:])
	}
}

func TestResolveRefPathContainment(t *testing.T) {
	root := t.TempDir()
	// 根内引用放行
	p, ok := resolveRefPath(root, "sub/a.go")
	if !ok || !strings.HasPrefix(p, root) {
		t.Fatalf("in-root ref rejected: %q ok=%v", p, ok)
	}
	// ../ 逃逸一律拒绝
	for _, evil := range []string{"../../secret.md", "../sibling/x.go", "sub/../../../secret.md"} {
		if _, ok := resolveRefPath(root, evil); ok {
			t.Fatalf("traversal ref %q not rejected", evil)
		}
	}
}

// fakeChatModel 把一个测试模型追加进 chatx 清单并注册清理（镜像 embedding_test 的做法）。
func fakeChatModel(t *testing.T, m chatx.Model) {
	t.Helper()
	chatx.Models = append(chatx.Models, m)
	t.Cleanup(func() { chatx.Models = chatx.Models[:len(chatx.Models)-1] })
}

// TestLLMBuiltinSave kind=builtin 档保存：清单内 id → 200 且 base_url/api_key 置空忽略
// （不落盘）；未知 id → 400。GET 响应携带 builtin_models 清单与 chat_download 快照。
func TestLLMBuiltinSave(t *testing.T) {
	h, _, okHome := newEnv(t)
	modelsDir := filepath.Join(okHome, "models")
	writeModelsDir(t, modelsDir)
	m := chatx.Model{ID: "fake-chat", Label: "测试模型", Size: 4}
	fakeChatModel(t, m)
	srv := httptest.NewServer(h)
	defer srv.Close()
	cfgPath := filepath.Join(okHome, "config.toml")

	// GET：builtin_models 含清单条目（未下载），chat_download 为零值快照
	_, data := do(t, "GET", srv.URL+"/api/llm", testToken, nil)
	var view struct {
		BuiltinModels []struct {
			ID         string `json:"id"`
			Label      string `json:"label"`
			Size       int64  `json:"size"`
			Downloaded bool   `json:"downloaded"`
		} `json:"builtin_models"`
		ChatDownload struct {
			State string `json:"state"`
		} `json:"chat_download"`
	}
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, bm := range view.BuiltinModels {
		if bm.ID == "fake-chat" {
			found = true
			if bm.Label != "测试模型" || bm.Size != 4 || bm.Downloaded {
				t.Fatalf("清单条目不对: %+v", bm)
			}
		}
	}
	if !found {
		t.Fatalf("builtin_models 应含 fake-chat: %s", data)
	}

	// 清单内 id → 200；base_url/api_key 忽略（置空，不落盘）
	code, data := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "内置", "kind": "builtin", "base_url": "https://evil.example.com",
		"model": "fake-chat", "api_key": "sk-leak",
	})
	if code != 200 {
		t.Fatalf("builtin 清单内保存应 200: %d %s", code, data)
	}
	cfgData, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(cfgData), "sk-leak") || strings.Contains(string(cfgData), "evil.example.com") {
		t.Fatalf("builtin 档不得落盘 base_url/api_key: %q", cfgData)
	}

	// 未知 id → 400
	code, _ = do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "x", "kind": "builtin", "model": "nope",
	})
	if code != 400 {
		t.Fatalf("未知内置模型应 400, got %d", code)
	}
}

// TestLLMBuiltinActiveGate 激活 builtin profile 要求模型已下载：未下载 → 409；
// 落盘后激活成功并写 chat-sidecar.want（chatsc.RequestStart 预热）。
func TestLLMBuiltinActiveGate(t *testing.T) {
	h, _, okHome := newEnv(t)
	modelsDir := filepath.Join(okHome, "models")
	writeModelsDir(t, modelsDir)
	m := chatx.Model{ID: "fake-chat", Size: 4}
	fakeChatModel(t, m)
	srv := httptest.NewServer(h)
	defer srv.Close()

	code, data := do(t, "POST", srv.URL+"/api/llm/profile", testToken, map[string]any{
		"name": "内置", "kind": "builtin", "model": "fake-chat",
	})
	if code != 200 {
		t.Fatalf("save: %d %s", code, data)
	}

	// 未下载 → 409
	code, _ = do(t, "POST", srv.URL+"/api/llm/active", testToken, map[string]any{"name": "内置"})
	if code != 409 {
		t.Fatalf("未下载激活应 409, got %d", code)
	}

	// 落盘模型 → 激活成功 + want 预热标记
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.InstalledPath(modelsDir), []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, data = do(t, "POST", srv.URL+"/api/llm/active", testToken, map[string]any{"name": "内置"})
	if code != 200 {
		t.Fatalf("已下载激活应 200: %d %s", code, data)
	}
	if _, err := os.Stat(filepath.Join(okHome, "chat-sidecar.want")); err != nil {
		t.Fatalf("激活成功应写 want 预热标记: %v", err)
	}
	var view struct {
		Active string `json:"active"`
	}
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	if view.Active != "内置" {
		t.Fatalf("active 应为 内置: %+v", view)
	}
}

// TestLLMDownloadEndpoints 下载端点：已下载 → {state:"done"}；未知模型 → 400；
// cancel 幂等 200。
func TestLLMDownloadEndpoints(t *testing.T) {
	h, _, okHome := newEnv(t)
	modelsDir := filepath.Join(okHome, "models")
	writeModelsDir(t, modelsDir)
	m := chatx.Model{ID: "fake-chat", Size: 4}
	fakeChatModel(t, m)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 预置已下载模型
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.InstalledPath(modelsDir), []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, data := do(t, "POST", srv.URL+"/api/llm/download", testToken, map[string]any{"model_id": "fake-chat"})
	if code != 200 || !strings.Contains(string(data), `"state":"done"`) {
		t.Fatalf("已下载应立即 done: %d %s", code, data)
	}

	// 未知模型 → 400
	code, _ = do(t, "POST", srv.URL+"/api/llm/download", testToken, map[string]any{"model_id": "nope"})
	if code != 400 {
		t.Fatalf("未知模型下载应 400, got %d", code)
	}

	// cancel 幂等（无任务也 200）
	code, _ = do(t, "POST", srv.URL+"/api/llm/download/cancel", testToken, map[string]any{"model_id": "fake-chat"})
	if code != 200 {
		t.Fatalf("cancel 应 200, got %d", code)
	}
}

// TestLLMModelsDir 模型目录（chat 与 embedding 共用 [embedding] models_dir 键，LLM 侧
// 只是同一设置的第二个入口）：GET 回显当前值与默认值；POST 设目录后 GET 回显且落盘；
// 空串恢复默认。
func TestLLMModelsDir(t *testing.T) {
	h, _, okHome := newEnv(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	var view struct {
		ModelsDir        string `json:"models_dir"`
		ModelsDirDefault string `json:"models_dir_default"`
	}
	_, data := do(t, "GET", srv.URL+"/api/llm", testToken, nil)
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	if view.ModelsDir == "" || view.ModelsDirDefault == "" || view.ModelsDir != view.ModelsDirDefault {
		t.Fatalf("未配置时应回显 models_dir == models_dir_default（非空）: %+v", view)
	}

	// 设置自定义目录 → 200 + GET 回显 + 目录即创建 + 落盘 [embedding] models_dir
	dir := filepath.Join(t.TempDir(), "models")
	code, data := do(t, "POST", srv.URL+"/api/llm/models-dir", testToken, map[string]any{"path": dir})
	if code != 200 {
		t.Fatalf("models-dir set: %d %s", code, data)
	}
	_, data = do(t, "GET", srv.URL+"/api/llm", testToken, nil)
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	if view.ModelsDir != dir {
		t.Fatalf("设置后应回显新目录: got %q want %q", view.ModelsDir, dir)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("设置即创建目录: %v", err)
	}
	cfgData, _ := os.ReadFile(filepath.Join(okHome, "config.toml"))
	if !strings.Contains(string(cfgData), "models_dir") {
		t.Fatalf("models_dir 应落盘（[embedding] 段）: %q", cfgData)
	}

	// 空串 = 恢复默认
	code, data = do(t, "POST", srv.URL+"/api/llm/models-dir", testToken, map[string]any{"path": ""})
	if code != 200 {
		t.Fatalf("恢复默认应 200: %d %s", code, data)
	}
	_, data = do(t, "GET", srv.URL+"/api/llm", testToken, nil)
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	if view.ModelsDir != view.ModelsDirDefault {
		t.Fatalf("空串应恢复默认: models_dir=%q default=%q", view.ModelsDir, view.ModelsDirDefault)
	}
}
