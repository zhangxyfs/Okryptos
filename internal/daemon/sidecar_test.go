package daemon

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"okryptos/internal/config"
	"okryptos/internal/embed"
	"okryptos/internal/embedsidecar"
)

func TestDesiredBuiltinModel(t *testing.T) {
	var cfg config.Config
	if desiredBuiltinModel(cfg) != nil {
		t.Fatal("无 active 应 nil")
	}
	cfg.Embedding.Active = "a"
	cfg.Embedding.Profiles = []config.EmbeddingProfile{{Name: "a", Type: "openai", Model: "m", BaseURL: "h"}}
	if desiredBuiltinModel(cfg) != nil {
		t.Fatal("openai 应 nil")
	}
	cfg.Embedding.Profiles[0].Type = "builtin"
	cfg.Embedding.Profiles[0].Model = "qwen3-emb-0.6b-q8"
	m := desiredBuiltinModel(cfg)
	if m == nil || m.ID != "qwen3-emb-0.6b-q8" {
		t.Fatalf("%+v", m)
	}
	cfg.Embedding.Profiles[0].Model = "不存在"
	if desiredBuiltinModel(cfg) != nil {
		t.Fatal("未知清单 id 应 nil")
	}
}

func TestDesiredChatModel(t *testing.T) {
	var cfg config.Config
	if desiredChatModel(cfg) != nil {
		t.Fatal("无 active 应 nil")
	}
	cfg.LLM.Active = "a"
	cfg.LLM.Profiles = []config.LLMProfile{{Name: "a", Kind: "openai", Model: "m", BaseURL: "h"}}
	if desiredChatModel(cfg) != nil {
		t.Fatal("openai 应 nil")
	}
	cfg.LLM.Profiles[0].Kind = "builtin"
	cfg.LLM.Profiles[0].Model = "qwen3-1.7b-q8"
	m := desiredChatModel(cfg)
	if m == nil || m.ID != "qwen3-1.7b-q8" {
		t.Fatalf("%+v", m)
	}
	cfg.LLM.Profiles[0].Model = "不存在"
	if desiredChatModel(cfg) != nil {
		t.Fatal("未知清单 id 应 nil")
	}

	// 识别意图槽（active_filter）独占 builtin：普通槽为空时也应解析到该模型，
	// 否则 janitor desired=nil，sidecar 永不拉起（与 filterx 解析顺序对齐）。
	cfg = config.Config{}
	cfg.LLM.ActiveFilter = "意图"
	cfg.LLM.Profiles = []config.LLMProfile{{Name: "意图", Kind: "builtin", Model: "qwen3-1.7b-q8", Filter: true}}
	m = desiredChatModel(cfg)
	if m == nil || m.ID != "qwen3-1.7b-q8" {
		t.Fatalf("filter 槽独占 builtin 应解析到模型, got %+v", m)
	}

	// 普通槽挂 openai + active_filter 挂 builtin：应返回 builtin 模型
	// （filterx 优先序：active_filter 优先于 active）。
	cfg = config.Config{}
	cfg.LLM.Active = "普通"
	cfg.LLM.ActiveFilter = "意图"
	cfg.LLM.Profiles = []config.LLMProfile{
		{Name: "普通", Kind: "openai", Model: "gpt-x", BaseURL: "h"},
		{Name: "意图", Kind: "builtin", Model: "qwen3-1.7b-q8", Filter: true},
	}
	m = desiredChatModel(cfg)
	if m == nil || m.ID != "qwen3-1.7b-q8" {
		t.Fatalf("active_filter 应优先于 active, got %+v", m)
	}

	// filter 槽挂 ollama（非 builtin）+ 普通槽挂 builtin：应回退到普通槽的
	// builtin 模型——两槽谁挂 builtin 就维持谁，否则条目优化永久"启动中"。
	cfg = config.Config{}
	cfg.LLM.Active = "普通"
	cfg.LLM.ActiveFilter = "意图"
	cfg.LLM.Profiles = []config.LLMProfile{
		{Name: "普通", Kind: "builtin", Model: "qwen3-1.7b-q8"},
		{Name: "意图", Kind: "ollama", Model: "qwen3", Filter: true},
	}
	m = desiredChatModel(cfg)
	if m == nil || m.ID != "qwen3-1.7b-q8" {
		t.Fatalf("filter 槽非 builtin 时应回退普通槽 builtin, got %+v", m)
	}
}

// TestJanitorStartsSidecar：全局配置 active=内置 + 假模型就绪 → janitor 一轮内拉起。
func TestJanitorStartsSidecar(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	// 假模型 + 假 runtime
	model := embed.BuiltinModel{ID: "fake-j", File: "f.gguf", Size: 4, Pooling: "cls", Dim: 2}
	embed.BuiltinModels = append(embed.BuiltinModels, model)
	t.Cleanup(func() { embed.BuiltinModels = embed.BuiltinModels[:len(embed.BuiltinModels)-1] })
	modelsDir := filepath.Join(home, "models")
	os.MkdirAll(modelsDir, 0o755)
	os.WriteFile(model.InstalledPath(modelsDir), []byte("fake"), 0o644)
	rtDir := filepath.Join(home, "runtime")
	os.MkdirAll(rtDir, 0o755)
	// Ensure 会 stat <rtDir>/llama-server[.exe]（RuntimeServerPath）；daemon 包见不到
	// embedsidecar.serverExeName，此处按平台推导同名假 exe。
	serverExe := "llama-server"
	if runtime.GOOS == "windows" {
		serverExe = "llama-server.exe"
	}
	os.WriteFile(filepath.Join(rtDir, serverExe), []byte("x"), 0o755)
	// helper 进程接缝（embedsidecar.ServerCommand 包级 var）
	oldSC := embedsidecar.ServerCommand
	embedsidecar.ServerCommand = helperServerCommand
	t.Cleanup(func() { embedsidecar.ServerCommand = oldSC })
	cfgText := fmt.Sprintf("[embedding]\nactive = \"内\"\nmodels_dir = '%s'\n[[embedding.profiles]]\nname = \"内\"\ntype = \"builtin\"\nmodel = \"fake-j\"\n", modelsDir)
	os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfgText), 0o600)

	mgr := &embedsidecar.Manager{RuntimeDir: rtDir, ModelsDir: modelsDir, HealthTimeout: 10 * time.Second, IdleTimeout: time.Hour}
	t.Cleanup(mgr.Stop)
	old := sidecarJanitorInterval
	sidecarJanitorInterval = 50 * time.Millisecond
	t.Cleanup(func() { sidecarJanitorInterval = old })
	go sidecarJanitor(mgr)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st := embedsidecar.LoadState(); st != nil && st.Healthy() {
			return // 成功
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("janitor 未在 5s 内拉起 sidecar")
}

// TestSidecarHelperProcess 伪装 llama-server（仅 /health）。
func TestSidecarHelperProcess(t *testing.T) {
	if os.Getenv("OK_HELPER") != "1" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	_ = http.ListenAndServe("127.0.0.1:"+os.Getenv("OK_HELPER_PORT"), mux)
	os.Exit(0)
}

// helperServerCommand 从 args 挖 --port 并起 helper 进程。
func helperServerCommand(_ string, args ...string) *exec.Cmd {
	port := ""
	for i, a := range args {
		if a == "--port" && i+1 < len(args) {
			port = args[i+1]
		}
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestSidecarHelperProcess")
	cmd.Env = append(os.Environ(), "OK_HELPER=1", "OK_HELPER_PORT="+port)
	return cmd
}
