package daemon

import (
	"path/filepath"
	"time"

	"okryptos/internal/chatsc"
	"okryptos/internal/chatx"
	"okryptos/internal/config"
	"okryptos/internal/embed"
	"okryptos/internal/embedsidecar"
	"okryptos/internal/registry"
)

// sidecarJanitorInterval sidecar 调和周期；测试可调小。
var sidecarJanitorInterval = 10 * time.Second

// desiredBuiltinModel 从全局配置解析期望的内置模型；非内置/未知清单 id 返回 nil。
func desiredBuiltinModel(cfg config.Config) *embed.BuiltinModel {
	p := cfg.Embedding.ActiveProfile()
	if p == nil || p.Type != "builtin" {
		return nil
	}
	return embed.FindBuiltinModel(p.Model)
}

// desiredChatModel 从全局配置解析期望的内置 chat 模型；kind 非 builtin/未知清单 id 返回 nil。
// 与 desiredBuiltinModel 同构，数据源是 [llm] 段（kind=builtin 时 Model 为 chatx 清单 id）。
// 解析顺序与 filterx.Filter 对齐：识别意图槽（active_filter）优先，未配置回退普通
// active——builtin 只挂识别意图槽是本特性的主力场景，只看 active 会让 janitor
// desired=nil，sidecar 永不拉起、want 每轮被清、过滤永久静默 fail-open。
// 回退条件是"过滤槽未配置或挂的不是 builtin"：两槽谁挂 builtin 就维持谁（filter 槽
// 挂 ollama/云端时普通槽的 builtin 也不能失去看护，否则条目优化永久"启动中"静默降级）。
// 已知边界：两槽各挂不同 builtin 模型时只有过滤槽的会被维持（与 filterx 优先序
// 一致）；优化场景用到另一个模型时走 notReady 降级（sidecar 单实例架构的固有限制）。
func desiredChatModel(cfg config.Config) *chatx.Model {
	p := cfg.LLM.FilterProfile()
	if p == nil || p.Kind != "builtin" {
		p = cfg.LLM.ActiveProfile()
	}
	if p == nil || p.Kind != "builtin" {
		return nil
	}
	m, ok := chatx.FindModel(p.Model)
	if !ok {
		return nil
	}
	return &m
}

// chatJanitor 周期调和 chat sidecar，与 sidecarJanitor 同构。
// 模型目录与 embedding 共用同一目录（embedsidecar.ModelsDir(cfg)；chatsc 刻意不搬
// ModelsDir(cfg)，GGUF 按 id 文件名天然隔离）。
func chatJanitor(mgr *chatsc.Manager) {
	ticker := time.NewTicker(sidecarJanitorInterval)
	defer ticker.Stop()
	for range ticker.C {
		cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
		if err != nil {
			continue
		}
		mgr.ModelsDir = embedsidecar.ModelsDir(cfg) // 与 embedding 共用，每轮按当前配置刷新
		mgr.Reconcile(desiredChatModel(cfg), time.Now())
	}
}

// sidecarJanitor 周期调和 embedding sidecar（active 为内置且模型就绪 → 在线；
// 空闲/切换/停用 → 回收）。配置变更经周期轮询自然生效（GUI/CLI 写全局配置）。
func sidecarJanitor(mgr *embedsidecar.Manager) {
	ticker := time.NewTicker(sidecarJanitorInterval)
	defer ticker.Stop()
	for range ticker.C {
		cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
		if err != nil {
			continue
		}
		mgr.ModelsDir = embedsidecar.ModelsDir(cfg) // 模型目录可配：每轮按当前配置刷新
		mgr.Reconcile(desiredBuiltinModel(cfg), time.Now())
	}
}
