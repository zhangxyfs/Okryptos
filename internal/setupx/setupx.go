// Package setupx 提供 ok setup / on / off 的核心逻辑，供 CLI 与 GUI 共享。
package setupx

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"okryptos/internal/agentx"
	"okryptos/internal/config"
	"okryptos/internal/embed"
	"okryptos/internal/embedsidecar"
	"okryptos/internal/embedx"
	"okryptos/internal/fsx"
	"okryptos/internal/registry"
)

// SkillNames 返回登记的技能名（供状态检测遍历）。
func SkillNames() []string {
	names := make([]string, 0, len(skillTemplates))
	for name := range skillTemplates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SkillDirs 返回技能安装目标目录并集：全部已检测 agent 的 SkillsDir() 去重
// （kimi/pi/reasonix/opencode 共享 SkillsHome，zcode 是独立的 ~/.zcode/skills）；无已检测 agent
// 时回退共享 SkillsHome（保持原语义）。
func SkillDirs() []string {
	seen := map[string]bool{}
	var dirs []string
	for _, a := range agentx.Detected() {
		d := a.SkillsDir()
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	if len(dirs) == 0 {
		dirs = append(dirs, agentx.SkillsHome())
	}
	return dirs
}

// AllSkillDirs 返回全部已注册 agent 的技能目录并集（卸载清理用，不问是否检测到）。
func AllSkillDirs() []string {
	seen := map[string]bool{}
	var dirs []string
	for _, a := range agentx.All() {
		d := a.SkillsDir()
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// renderSkill 渲染技能模板（烘焙 exe 绝对路径，正斜杠口径同 InstallSkills）。
func renderSkill(name, exe string) string {
	return strings.ReplaceAll(skillTemplates[name], "{{EXE}}", filepath.ToSlash(exe))
}

// legacySkillPrefix 是 2.25.0 改名前的技能名前缀（ok-init 等六个 → ok-*）。
const legacySkillPrefix = "openknowledge-"

// RemoveLegacySkills 删除全部 agent 技能目录下的 openknowledge-* 旧技能副本
//（2.25.0 改名迁移：旧副本不删会被 agent 照常扫描发现成幽灵技能，且新版卸载
// 只认 ok-* 删不掉它们）。用 AllSkillDirs（不问是否检测到 hooks 接入）不留死角。
// 幂等；只在目录名确实是本项目旧技能（含 SKILL.md 且 front matter name 匹配
// openknowledge-*）时删除，同名外来目录不动。
func RemoveLegacySkills() {
	for _, home := range AllSkillDirs() {
		matches, err := filepath.Glob(filepath.Join(home, legacySkillPrefix+"*"))
		if err != nil {
			continue
		}
		for _, dir := range matches {
			data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
			if err != nil {
				continue
			}
			name := filepath.Base(dir)
			if strings.Contains(string(data), "name: "+name+"\n") {
				_ = os.RemoveAll(dir)
			}
		}
	}
}

// InstallSkills 把技能模板（烘焙 exe 路径）写入 SkillDirs() 的每个目录。
func InstallSkills(exe string) error {
	RemoveLegacySkills()
	for _, home := range SkillDirs() {
		for name := range skillTemplates {
			dir := filepath.Join(home, name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			if err := fsx.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(renderSkill(name, exe)), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// EnsureSkills 技能自愈（R3 B-01，与各适配器 EnsureHooks 同款"不复活"语义）：
// 文件存在、front matter name 表明是本项目技能、但烘焙的 exe 已过期
//（gui-split 部署迁移/改名）时重写为当前路径；文件缺失不复活（用户显式
// 删除），外来内容（name 不匹配）不动。技能指令里的 exe 指向死路径时
// agent 执行报错，而状态页只查存在性会误报正常——故须随 selfHealHooks
// 同窗口自愈。
func EnsureSkills(exe string) error {
	RemoveLegacySkills()
	cur := `"` + filepath.ToSlash(exe) + `"`
	for _, home := range SkillDirs() {
		for name := range skillTemplates {
			p := filepath.Join(home, name, "SKILL.md")
			data, err := os.ReadFile(p)
			if err != nil {
				continue // 缺失不复活
			}
			content := string(data)
			if !strings.Contains(content, "name: "+name+"\n") || strings.Contains(content, cur) {
				continue // 外来内容不动；exe 仍最新不动
			}
			if err := fsx.WriteFile(p, []byte(renderSkill(name, exe)), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// SkillsInstalled 技能状态检测（R3 B-01）：全部目标目录的全部技能文件存在
// 且烘焙的 exe 为当前路径。只查存在性会把 exe 迁移后的死路径误报为已接入。
func SkillsInstalled(exe string) bool {
	cur := `"` + filepath.ToSlash(exe) + `"`
	for _, home := range SkillDirs() {
		for _, name := range SkillNames() {
			data, err := os.ReadFile(filepath.Join(home, name, "SKILL.md"))
			if err != nil || !strings.Contains(string(data), cur) {
				return false
			}
		}
	}
	return true
}

// updateGlobalConfig 跨进程锁内 读-改-写 全局 config.toml（0600）：LoadMerged 取
// 合并态 → fn 修改 → fsx.WriteFile 原子落盘。GUI 并发两个 Save*（如保存 profile
// 的同时切换 active）裸读改写会互相覆盖丢更新；非原子写崩溃留半截文件会让
// LoadMerged 失败、GUI/CLI 报错。Save* 系列全部走这里。
// 注意：整档重编码不保留注释与未知键（已接受的取舍——全局配置由 GUI/CLI 托管）。
func updateGlobalConfig(fn func(*config.Config) error) error {
	globalPath := filepath.Join(registry.Home(), "config.toml")
	return fsx.WithFileLock(globalPath, func() error {
		cfg, err := config.LoadMerged("", globalPath)
		if err != nil {
			return fmt.Errorf("全局配置读取失败: %w", err)
		}
		if err := fn(&cfg); err != nil {
			return err
		}
		var buf strings.Builder
		if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
			return fmt.Errorf("全局配置编码失败: %w", err)
		}
		if err := os.MkdirAll(registry.Home(), 0o755); err != nil {
			return err
		}
		if err := fsx.WriteFile(globalPath, []byte(buf.String()), 0o600); err != nil {
			return fmt.Errorf("全局配置写入失败: %w", err)
		}
		return nil
	})
}

// SaveEmbeddingProfile 保存（同名覆盖）一个 profile 到全局配置；activate 时
// 同时置为使用中。api_key/api_key_env 留空 = 保留同名旧值（GUI 密文不回传语义）。
func SaveEmbeddingProfile(p config.EmbeddingProfile, activate bool) error {
	return updateGlobalConfig(func(cfg *config.Config) error {
		for i := range cfg.Embedding.Profiles {
			if cfg.Embedding.Profiles[i].Name == p.Name {
				if p.APIKey == "" {
					p.APIKey = cfg.Embedding.Profiles[i].APIKey
				}
				if p.APIKeyEnv == "" {
					p.APIKeyEnv = cfg.Embedding.Profiles[i].APIKeyEnv
				}
				cfg.Embedding.Profiles[i] = p
				if activate {
					cfg.Embedding.Active = p.Name
				}
				return nil
			}
		}
		cfg.Embedding.Profiles = append(cfg.Embedding.Profiles, p)
		if activate {
			cfg.Embedding.Active = p.Name
		}
		return nil
	})
}

// SetActiveEmbedding 切换使用中 profile；name 空串 = 停用（纯关键词检索）。
func SetActiveEmbedding(name string) error {
	return updateGlobalConfig(func(cfg *config.Config) error {
		if name != "" {
			found := false
			for _, p := range cfg.Embedding.Profiles {
				if p.Name == name {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("profile 不存在: %s", name)
			}
		}
		cfg.Embedding.Active = name
		return nil
	})
}

// DeleteEmbeddingProfile 删除 profile；删除使用中项时 Active 置空（退回纯关键词）。
func DeleteEmbeddingProfile(name string) error {
	return updateGlobalConfig(func(cfg *config.Config) error {
		kept := cfg.Embedding.Profiles[:0]
		for _, p := range cfg.Embedding.Profiles {
			if p.Name != name {
				kept = append(kept, p)
			}
		}
		cfg.Embedding.Profiles = kept
		if cfg.Embedding.Active == name {
			cfg.Embedding.Active = ""
		}
		return nil
	})
}

// SaveLLMProfile 保存（同名覆盖）一个 llm profile 到全局配置；activate 时同时置为
// 使用中。api_key 留空 = 保留同名旧值（GUI 密文不回传语义，同 SaveEmbeddingProfile）。
func SaveLLMProfile(p config.LLMProfile, activate bool) error {
	return updateGlobalConfig(func(cfg *config.Config) error {
		for i := range cfg.LLM.Profiles {
			if cfg.LLM.Profiles[i].Name == p.Name {
				if p.APIKey == "" {
					p.APIKey = cfg.LLM.Profiles[i].APIKey
				}
				cfg.LLM.Profiles[i] = p
				if activate {
					cfg.LLM.Active = p.Name
				}
				return nil
			}
		}
		cfg.LLM.Profiles = append(cfg.LLM.Profiles, p)
		if activate {
			cfg.LLM.Active = p.Name
		}
		return nil
	})
}

// SetActiveLLM 切换使用中 llm profile；name 空串 = 停用（优化功能不可用）。
func SetActiveLLM(name string) error {
	return updateGlobalConfig(func(cfg *config.Config) error {
		if name != "" {
			found := false
			for _, p := range cfg.LLM.Profiles {
				if p.Name == name {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("profile 不存在: %s", name)
			}
		}
		cfg.LLM.Active = name
		return nil
	})
}

// SetActiveLLMMaxTokens 只更新使用中 profile 的 max_tokens（0 = 用调用方默认）。
// 单字段改而不整 profile 覆盖：GUI 模型配置卡的「最大 token」两段式保存走此，
// 避免误清 temperature 等其他高级参数。无使用中 profile 报错。
func SetActiveLLMMaxTokens(n int) error {
	return updateGlobalConfig(func(cfg *config.Config) error {
		for i := range cfg.LLM.Profiles {
			if cfg.LLM.Profiles[i].Name == cfg.LLM.Active {
				cfg.LLM.Profiles[i].MaxTokens = n
				return nil
			}
		}
		return fmt.Errorf("无使用中的 llm profile（先在模型配置里设置「使用中」）")
	})
}

// DeleteLLMProfile 删除 llm profile；删除使用中项时 Active 置空。
func DeleteLLMProfile(name string) error {
	return updateGlobalConfig(func(cfg *config.Config) error {
		kept := cfg.LLM.Profiles[:0]
		for _, p := range cfg.LLM.Profiles {
			if p.Name != name {
				kept = append(kept, p)
			}
		}
		cfg.LLM.Profiles = kept
		if cfg.LLM.Active == name {
			cfg.LLM.Active = ""
		}
		return nil
	})
}

// SaveEmbeddingModelsDir 把内置模型目录写入全局配置 [embedding] models_dir；
// 空串 = 恢复默认（<ok.exe 所在目录>/models）。调用方负责校验/创建目录。
func SaveEmbeddingModelsDir(path string) error {
	return updateGlobalConfig(func(cfg *config.Config) error {
		cfg.Embedding.ModelsDir = path
		return nil
	})
}

// TestEmbeddingProfile 以 timeout 做 profile 连通性检查。
// builtin：检查 runtime/模型文件，sidecar 未就绪时写 want 并返回"启动中"提示性错误。
func TestEmbeddingProfile(p config.EmbeddingProfile, timeout time.Duration) error {
	if p.Type == "builtin" {
		m := embed.FindBuiltinModel(p.Model)
		if m == nil {
			return fmt.Errorf("未知内置模型: %s", p.Model)
		}
		if _, err := embedsidecar.RuntimeServerPath(embedsidecar.DefaultRuntimeDir()); err != nil {
			return err
		}
		cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
		if err != nil {
			return fmt.Errorf("全局配置读取失败: %w", err)
		}
		if !m.Installed(embedsidecar.ModelsDir(cfg)) {
			return errors.New("模型未下载（先在配置弹窗或 ok setup 中下载）")
		}
		c := embedx.ClientForProfile(p, timeout)
		if c == nil {
			return errors.New("sidecar 未就绪——已请求 daemon 拉起，稍后自动生效（数秒到一分钟）")
		}
		_, err = c.EmbedQuery(context.Background(), "ping")
		return err
	}
	c := embedx.ClientForProfile(p, timeout)
	if c == nil {
		return fmt.Errorf("profile 不可用（类型 %s，检查必填项）", p.Type)
	}
	_, err := c.EmbedQuery(context.Background(), "ping")
	return err
}

// ListOllamaModels 探测 Ollama 已安装模型（GET {base}/api/tags，3s 超时）。
func ListOllamaModels(baseURL string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/api/tags"
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama API %d", resp.StatusCode)
	}
	var tr struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tr.Models))
	for _, m := range tr.Models {
		names = append(names, m.Name)
	}
	return names, nil
}
// SaveHooksTimeout 把 hooks 超时（秒）写入全局配置 [hooks] timeout_sec；
// 下次写入/自愈 hooks 块（含 GUI 引导页安装）时生效。
func SaveHooksTimeout(sec int) error {
	return updateGlobalConfig(func(cfg *config.Config) error {
		cfg.Hooks.TimeoutSec = sec
		return nil
	})
}

// ReasonixEnforceMode 返回 reasonix sidecar 的强制检查表达方式：
// 全局配置 [reasonix] enforce_mode（soft|hard|mixed），缺省/非法按 mixed。
func ReasonixEnforceMode() string {
	cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		return "mixed"
	}
	switch cfg.Reasonix.EnforceMode {
	case "soft", "hard":
		return cfg.Reasonix.EnforceMode
	default:
		return "mixed"
	}
}

// SaveReasonixEnforceMode 校验并写入全局配置 [reasonix] enforce_mode；
// sidecar 每条输入实时读配置，即时生效。
func SaveReasonixEnforceMode(mode string) error {
	switch mode {
	case "soft", "hard", "mixed":
	default:
		return fmt.Errorf("enforce_mode 必须是 soft|hard|mixed: %q", mode)
	}
	return updateGlobalConfig(func(cfg *config.Config) error {
		cfg.Reasonix.EnforceMode = mode
		return nil
	})
}

// DisabledFlagPath 返回 hooks 全局关闭标志文件路径。
func DisabledFlagPath() string { return filepath.Join(registry.Home(), "hooks-disabled") }

// Disable 写入关闭标志文件，全局关闭 hooks（持续到 Enable）。
func Disable() error {
	content := fmt.Sprintf("disabled at %s\nrun `ok on` to re-enable\n", time.Now().Format(time.RFC3339))
	if err := os.MkdirAll(registry.Home(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(DisabledFlagPath(), []byte(content), 0o644)
}

// Enable 删除关闭标志文件，开启 hooks（幂等）。
func Enable() error {
	if err := os.Remove(DisabledFlagPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

//go:embed skills/ok-wiki/SKILL.md
var wikiSkillTemplate string

//go:embed skills/ok-propose/SKILL.md
var proposeSkillTemplate string

func init() {
	skillTemplates["ok-wiki"] = wikiSkillTemplate
	skillTemplates["ok-propose"] = proposeSkillTemplate
}

var skillTemplates = map[string]string{
	"ok-init": "---\nname: ok-init\ndescription: 在当前项目目录初始化 Okryptos 知识库（ok init，自动以当前目录名注册，无需用户提供项目名）。当用户要求\"初始化知识库\"或\"把本项目注册到知识库\"时使用。\n---\n\n# ok-init\n\n用 Bash 工具在当前工作目录直接执行（无参数，自动取当前目录名，不要向用户询问项目名）：\n\n    \"{{EXE}}\" init\n\n把输出的知识库路径汇报给用户；若提示重复注册，告知用户该项目已初始化过。\n",
	"ok-on":   "---\nname: ok-on\ndescription: 开启 Okryptos 知识库 hooks 全局开关。当用户要求\"开启知识库\"\"启用知识库 hooks\"时使用。\n---\n\n# ok-on\n\n用 Bash 工具执行：\n\n    \"{{EXE}}\" on\n\n把输出汇报给用户。\n",
	"ok-off":  "---\nname: ok-off\ndescription: 关闭 Okryptos 知识库 hooks 全局开关（持续到手动开启）。当用户要求\"关闭知识库\"\"停用知识库 hooks\"时使用。\n---\n\n# ok-off\n\n用 Bash 工具执行：\n\n    \"{{EXE}}\" off\n\n把输出汇报给用户，并说明：关闭后所有项目的知识库注入与强制检查都会暂停，直到执行 ok on。\n",
	"ok-capture": "---\nname: ok-capture\ndescription: 查看或切换 Okryptos 知识库的经验沉淀模式与轮次间隔（ok capture propose|auto|interval）。当用户要求\"切换沉淀模式\"\"开启自动提取\"\"关闭自动提取\"\"调整提取频率\"时使用。\n---\n\n# ok-capture\n\n查看当前模式与轮次间隔，用 Bash 工具执行：\n\n    \"{{EXE}}\" capture\n\n切换模式，用 Bash 工具执行（二选一）：\n\n    \"{{EXE}}\" capture propose\n    \"{{EXE}}\" capture auto\n\n设置轮次间隔（n ≥ 1，仅 auto 模式生效），用 Bash 工具执行：\n\n    \"{{EXE}}\" capture interval <n>\n\n## 两种模式\n\n- **propose（默认）**：AI 主动提议——AI 觉得值得记录时用 ok propose 记为草稿条目，无轮次限制，由人批准后转正入库。\n- **auto（Stop 自动提取）**：每 turn_interval 轮对话结束时，Stop hook 阻断一次并强制 AI 自省本轮是否有值得沉淀的经验，有则当场 propose 草稿。\n\n把切换结果汇报给用户。\n",
}
