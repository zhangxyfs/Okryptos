// Package chatx 提供 [llm] kind=builtin 档的 chat 模型清单与下载薄封装，
// 复用 internal/embed 的 .part 断点续传 + sha256 校验下载管线（embed 包零改动）。
package chatx

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"okryptos/internal/embed"
)

// Model 是内置（llama-server sidecar）可下载 chat 模型的清单条目。
// size/sha256 在引入时钉死（来源 hf-mirror /api/models/<repo>/tree/main 的 lfs.oid），
// 下载后校验，防篡改与截断。
type Model struct {
	ID       string // 清单 id，如 "qwen3-1.7b-q8"
	Label    string // GUI/CLI 展示名（含体积/量化/特点）
	Repo     string // HF repo，如 "Qwen/Qwen3-1.7B-GGUF"
	File     string // repo 内文件名
	Size     int64
	SHA256   string // 小写 hex
	Thinking bool   // true=混合思考模型，sidecar 启动参数需压 thinking（见 Task 2）
}

// Models 内置 chat 模型清单（默认第一条）。
// 2026-09-12 hf-mirror 实测钉死；变更新增条目即可，无需改代码。
var Models = []Model{
	{
		ID: "qwen3-1.7b-q8", Label: "Qwen3-1.7B · Q8_0（首选 · 1.8GB · 中文强）",
		Repo: "Qwen/Qwen3-1.7B-GGUF", File: "Qwen3-1.7B-Q8_0.gguf",
		Size: 1834426016, SHA256: "061b54daade076b5d3362dac252678d17da8c68f07560be70818cace6590cb1a",
		Thinking: true,
	},
	{
		ID: "qwen3-0.6b-q8", Label: "Qwen3-0.6B · Q8_0（最省 · 640MB · 老机器可跑）",
		Repo: "Qwen/Qwen3-0.6B-GGUF", File: "Qwen3-0.6B-Q8_0.gguf",
		Size: 639446688, SHA256: "9465e63a22add5354d9bb4b99e90117043c7124007664907259bd16d043bb031",
		Thinking: true,
	},
	{
		// 用 unsloth 仓：Qwen 官方未发布 4B-Instruct-2507 的 GGUF。
		ID: "qwen3-4b-instruct-2507-q4km", Label: "Qwen3-4B-Instruct-2507 · Q4_K_M（最稳 · 2.5GB · 非思考版）",
		Repo: "unsloth/Qwen3-4B-Instruct-2507-GGUF", File: "Qwen3-4B-Instruct-2507-Q4_K_M.gguf",
		Size: 2497281120, SHA256: "3605803b982cb64aead44f6c1b2ae36e3acdb41d8e46c8a94c6533bc4c67e597",
		Thinking: false,
	},
}

// FindModel 按 id 查清单；未命中返回 ok=false。
func FindModel(id string) (Model, bool) {
	for _, m := range Models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// InstalledPath 返回模型文件落盘路径（<modelsDir>/<id>.gguf，与 embedding 同惯例）。
func (m Model) InstalledPath(modelsDir string) string {
	return filepath.Join(modelsDir, m.ID+".gguf")
}

// Installed 快速判定模型已就绪（存在且尺寸一致；完整 sha256 校验在下载完成时做）。
func (m Model) Installed(modelsDir string) bool {
	st, err := os.Stat(m.InstalledPath(modelsDir))
	return err == nil && st.Size() == m.Size
}

// Download 复用 embed.Download（.part 断点续传 + sha256 校验 + 原子改名）：
// chat 模型映射为 embed.BuiltinModel 仅取 ID/Repo/File/Size/SHA256 五字段。
func Download(ctx context.Context, hc *http.Client, m Model, mirror, modelsDir string, progress func(done, total int64)) error {
	return embed.Download(ctx, hc, embed.BuiltinModel{
		ID: m.ID, Repo: m.Repo, File: m.File, Size: m.Size, SHA256: m.SHA256,
	}, mirror, modelsDir, progress)
}
