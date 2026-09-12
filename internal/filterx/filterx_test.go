package filterx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"okryptos/internal/config"
	"okryptos/internal/index"
)

func testHits() []index.Hit {
	return []index.Hit{
		{Filename: "a.md", Title: "条目甲", Summary: "摘要甲"},
		{Filename: "b.md", Title: "条目乙", Summary: "摘要乙"},
		{Filename: "c.md", Title: "条目丙", Summary: "摘要丙"},
	}
}

// llmServer 按 content 应答 OpenAI 兼容 /chat/completions；delay 模拟慢响应。
func llmServer(t *testing.T, content string, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": content}}},
		})
	}))
}

func cfgWithLLM(baseURL string) config.Config {
	cfg := config.Default()
	cfg.LLM.Active = "测试"
	cfg.LLM.Profiles = []config.LLMProfile{{Name: "测试", Kind: "openai", BaseURL: baseURL, Model: "m"}}
	cfg.Retrieve.Filter = config.RetrieveFilter{Enabled: true, TimeoutMs: 3000, MaxTokens: 64}
	return cfg
}

func TestFilterKeepsSelected(t *testing.T) {
	srv := llmServer(t, "[1,3]", 0)
	defer srv.Close()
	kept, note := Filter(context.Background(), cfgWithLLM(srv.URL), "查询", testHits())
	if len(kept) != 2 || kept[0].Filename != "a.md" || kept[1].Filename != "c.md" {
		t.Fatalf("应保留 [1,3]: %+v", kept)
	}
	if note == "" {
		t.Fatal("实际调用后 note 应非空")
	}
}

func TestFilterDropAll(t *testing.T) {
	srv := llmServer(t, "[]", 0)
	defer srv.Close()
	kept, _ := Filter(context.Background(), cfgWithLLM(srv.URL), "查询", testHits())
	if len(kept) != 0 {
		t.Fatalf("空数组应全丢: %+v", kept)
	}
}

func TestFilterToleratesFence(t *testing.T) {
	srv := llmServer(t, "```json\n[2]\n```", 0)
	defer srv.Close()
	kept, _ := Filter(context.Background(), cfgWithLLM(srv.URL), "查询", testHits())
	if len(kept) != 1 || kept[0].Filename != "b.md" {
		t.Fatalf("围栏输出应被容忍: %+v", kept)
	}
}

func TestFilterGarbageKeepsAll(t *testing.T) {
	srv := llmServer(t, "我觉得都相关", 0)
	defer srv.Close()
	kept, note := Filter(context.Background(), cfgWithLLM(srv.URL), "查询", testHits())
	if len(kept) != 3 {
		t.Fatalf("解析失败应保留全部: %+v", kept)
	}
	if note == "" {
		t.Fatal("解析失败应有 note")
	}
}

func TestFilterTimeoutKeepsAll(t *testing.T) {
	srv := llmServer(t, "[1]", 500*time.Millisecond)
	defer srv.Close()
	cfg := cfgWithLLM(srv.URL)
	cfg.Retrieve.Filter.TimeoutMs = 100
	kept, note := Filter(context.Background(), cfg, "查询", testHits())
	if len(kept) != 3 {
		t.Fatalf("超时应保留全部: %+v", kept)
	}
	if note == "" {
		t.Fatal("超时应有 note")
	}
}

func TestFilterNoLLMSilent(t *testing.T) {
	cfg := config.Default() // 无激活 profile
	kept, note := Filter(context.Background(), cfg, "查询", testHits())
	if len(kept) != 3 || note != "" {
		t.Fatalf("未配置 LLM 应静默原样返回: kept=%d note=%q", len(kept), note)
	}
}

func TestFilterDisabled(t *testing.T) {
	cfg := cfgWithLLM("http://127.0.0.1:1")
	cfg.Retrieve.Filter.Enabled = false
	kept, note := Filter(context.Background(), cfg, "查询", testHits())
	if len(kept) != 3 || note != "" {
		t.Fatalf("关闭时应原样返回: kept=%d note=%q", len(kept), note)
	}
}

// TestFilterPrefersFilterProfile 识别意图槽位优先：active_filter 指向的 profile 被用于
// 过滤，普通 active 不被调用（两个服务器应答不同，裁决结果可区分走了哪个）。
func TestFilterPrefersFilterProfile(t *testing.T) {
	filterSrv := llmServer(t, "[2]", 0)
	defer filterSrv.Close()
	generalSrv := llmServer(t, "[1]", 0)
	defer generalSrv.Close()
	cfg := cfgWithLLM(generalSrv.URL)
	cfg.LLM.ActiveFilter = "意图"
	cfg.LLM.Profiles = append(cfg.LLM.Profiles,
		config.LLMProfile{Name: "意图", Kind: "openai", BaseURL: filterSrv.URL, Model: "m", Filter: true})
	kept, _ := Filter(context.Background(), cfg, "查询", testHits())
	if len(kept) != 1 || kept[0].Filename != "b.md" {
		t.Fatalf("应使用识别意图槽的裁决 [2]: %+v", kept)
	}
}
