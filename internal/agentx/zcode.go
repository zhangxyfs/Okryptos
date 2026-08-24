package agentx

import (
	"fmt"
	"os"
	"path/filepath"

	"openknowledge/internal/fsx"
)

// ZcodeHome 返回 ZCode 配置根目录（OK_ZCODE_HOME 优先——ok 自留的测试隔离口，
// ZCode 官方未文档化配置目录环境变量），否则 ~/.zcode。
// 见 https://zcode.z.ai/cn/docs/hooks 与 /cn/docs/skill。
func ZcodeHome() string {
	if h := os.Getenv("OK_ZCODE_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".zcode")
}

func zcodeConfigPath() string { return filepath.Join(ZcodeHome(), "cli", "config.json") }

// zcodeHookEvents 是 ok 接入的 ZCode hook 事件（对应 ok 的三条 hook 链路）。
// 输出协议用 Claude 风格 JSON（args 末尾的 "claude"）：ZCode 只把以 { 开头的
// 合法 JSON stdout 解析为协议结果，纯文本 stdout 不进模型上下文。
var zcodeHookEvents = []hookEvent{
	{"UserPromptSubmit", "", "prompt"},
	{"PostToolUse", "Write|Edit", "post-tool"},
	{"Stop", "", "stop"},
}

// zcodeAgent ZCode 适配器：hook 集成 = 合并写 ~/.zcode/cli/config.json；
// 技能目录独立于共享 SkillsHome（ZCode 不自动读 ~/.agents/skills）。
type zcodeAgent struct{}

func init() { Register(zcodeAgent{}) }

func (zcodeAgent) ID() string          { return "zcode" }
func (zcodeAgent) DisplayName() string { return "ZCode" }
func (zcodeAgent) HooksTarget() string { return zcodeConfigPath() }
func (zcodeAgent) SkillsDir() string   { return filepath.Join(ZcodeHome(), "skills") }

func (zcodeAgent) Detect() bool {
	info, err := os.Stat(ZcodeHome())
	return err == nil && info.IsDir()
}

// isOKZcodeHook 判定一条 ZCode hook 条目是否 ok 生成：args 形如 ["hook", "<事件>", ...]，
// 事件为 ok 的三条链路之一。不看 command 的 basename——测试二进制、exe 改名/迁移都不影响识别。
func isOKZcodeHook(h map[string]any) bool {
	args, _ := h["args"].([]any)
	if len(args) < 2 || args[0] != "hook" {
		return false
	}
	switch args[1] {
	case "prompt", "post-tool", "stop":
		return true
	}
	return false
}

// zcodeOKGroup 生成一个事件的 ok hook 组：process 直 exec（不过 shell），
// 三条事件统一 timeoutMs = 全局 [hooks] timeout_sec × 1000。
func zcodeOKGroup(exe, matcher, okHook string) map[string]any {
	hook := map[string]any{
		"type":      "process",
		"command":   filepath.ToSlash(exe),
		"args":      []any{"hook", okHook, "claude"},
		"timeoutMs": HookTimeoutSec() * 1000,
	}
	g := map[string]any{"hooks": []any{hook}}
	if matcher != "" {
		g["matcher"] = matcher
	}
	return g
}

// loadZcodeConfig 读 config.json；文件不存在返回空对象，解析失败报错（不覆盖损坏文件）。
// 用 map[string]any 合并写会重排 key 顺序——未知字段内容保留，代价可接受。
func loadZcodeConfig() (map[string]any, error) {
	return loadSettingsJSON(zcodeConfigPath(), "zcode config.json")
}

// zcodeEventsOf 取 hooks.events（只读视图），不做任何创建。
func zcodeEventsOf(cfg map[string]any) map[string]any {
	hooks, _ := cfg["hooks"].(map[string]any)
	events, _ := hooks["events"].(map[string]any)
	return events
}

// zcodeHooksEnabled 报告 hooks.enabled 是否显式为 true。ZCode 缺省不派发 hooks，
// 用户显式关闭 = 集成失效——与 qoder 的 hooksConfig.enabled 检查对称防御。
func zcodeHooksEnabled(cfg map[string]any) bool {
	hooks, _ := cfg["hooks"].(map[string]any)
	enabled, _ := hooks["enabled"].(bool)
	return enabled
}

// zcodeEventsEdit 取 hooks.events 供写入：缺失时创建并把 hooks.enabled 置 true
// （ZCode 要求显式开启，否则整份 hooks 配置不生效）。
func zcodeEventsEdit(cfg map[string]any) map[string]any {
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		cfg["hooks"] = hooks
	}
	hooks["enabled"] = true
	events, _ := hooks["events"].(map[string]any)
	if events == nil {
		events = map[string]any{}
		hooks["events"] = events
	}
	return events
}

// hasOKZcodeHook 报告 events 里是否存在任何 ok 自有 hook。
func hasOKZcodeHook(events map[string]any) bool {
	return containsOKHook(events, isOKZcodeHook)
}

// zcodeHooksCurrent 报告三事件的 ok hook 是否均为当前期望形态
// （command=exe、args 含 claude、matcher 与 timeoutMs 正确）。
func zcodeHooksCurrent(events map[string]any, exe string) bool {
	wantTimeout := float64(HookTimeoutSec() * 1000)
	wantExe := filepath.ToSlash(exe)
	for _, e := range zcodeHookEvents {
		groups, _ := events[e.event].([]any)
		found := false
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			if gm == nil {
				continue
			}
			matcher, _ := gm["matcher"].(string)
			if matcher != e.matcher {
				continue
			}
			hooks, _ := gm["hooks"].([]any)
			for _, h := range hooks {
				hm, _ := h.(map[string]any)
				if hm == nil || !isOKZcodeHook(hm) {
					continue
				}
				cmd, _ := hm["command"].(string)
				timeout, _ := hm["timeoutMs"].(float64)
				args, _ := hm["args"].([]any)
				if cmd == wantExe && timeout == wantTimeout &&
					len(args) == 3 && args[1] == e.okHook && args[2] == "claude" {
					found = true
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// writeZcodeConfig 备份后写回 config.json（MarshalIndent，未知字段保留）。
func writeZcodeConfig(cfg map[string]any) error {
	return writeSettingsJSON(zcodeConfigPath(), cfg)
}

func (zcodeAgent) InstallHooks(exe string) error {
	// 读-改-写全程包在 fsx.WithFileLock 内：selfHealHooks 每个 prompt 都跑，与
	// GUI /api/setup/hooks 并发时裸跑会 load→改→rename 互相覆盖、丢第三方 hooks。
	return fsx.WithFileLock(zcodeConfigPath(), func() error {
		cfg, err := loadZcodeConfig()
		if err != nil {
			return err
		}
		events := zcodeEventsEdit(cfg)
		stripOKHooks(events, isOKZcodeHook)
		for _, e := range zcodeHookEvents {
			groups, _ := events[e.event].([]any)
			events[e.event] = append(groups, zcodeOKGroup(exe, e.matcher, e.okHook))
		}
		return writeZcodeConfig(cfg)
	})
}

func (zcodeAgent) RemoveHooks() (bool, error) {
	if _, err := os.Stat(zcodeConfigPath()); os.IsNotExist(err) {
		return false, nil
	}
	removed := false
	// 读-改-写包在 fsx.WithFileLock 内（同 InstallHooks）。
	err := fsx.WithFileLock(zcodeConfigPath(), func() error {
		cfg, err := loadZcodeConfig()
		if err != nil {
			return err
		}
		events := zcodeEventsOf(cfg)
		if events == nil || !stripOKHooks(events, isOKZcodeHook) {
			return nil
		}
		if err := writeZcodeConfig(cfg); err != nil {
			return fmt.Errorf("移除 zcode hooks: %w", err)
		}
		removed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

// EnsureHooks 自愈：config 存在、曾安装过 ok hooks 且内容过期（exe 迁移、超时
// 变更、旧格式）时重写；从未安装（无任何 ok 条目）则 no-op——zcode 没有 kimi
// "标记注释被清"的已知行为，用户显式移除的集成不复活。重写保留现状
// hooks.enabled：用户显式关闭的总开关不翻回 true（"显式关闭不复活"，
// 与 qoder 的 hooksConfig.enabled 对称防御）。
func (zcodeAgent) EnsureHooks(exe string) error {
	if _, err := os.Stat(zcodeConfigPath()); err != nil {
		return nil
	}
	// 读-改-写包在 fsx.WithFileLock 内（同 InstallHooks）。
	return fsx.WithFileLock(zcodeConfigPath(), func() error {
		cfg, err := loadZcodeConfig()
		if err != nil {
			return err
		}
		events := zcodeEventsOf(cfg)
		if events == nil || !hasOKZcodeHook(events) || zcodeHooksCurrent(events, exe) {
			return nil
		}
		hooks, _ := cfg["hooks"].(map[string]any)
		prevEnabled, hadEnabled := hooks["enabled"]
		events = zcodeEventsEdit(cfg)
		if hadEnabled {
			hooks["enabled"] = prevEnabled
		} else {
			delete(hooks, "enabled")
		}
		stripOKHooks(events, isOKZcodeHook)
		for _, e := range zcodeHookEvents {
			groups, _ := events[e.event].([]any)
			events[e.event] = append(groups, zcodeOKGroup(exe, e.matcher, e.okHook))
		}
		return writeZcodeConfig(cfg)
	})
}

func (zcodeAgent) HooksInstalled() bool {
	cfg, err := loadZcodeConfig()
	if err != nil {
		return false
	}
	events := zcodeEventsOf(cfg)
	if events == nil {
		return false
	}
	exe, err := currentCLIExe()
	if err != nil {
		return false
	}
	// hooks.enabled 关闭/缺失 = 集成失效（hooks 静默不派发），视为未安装。
	return zcodeHooksCurrent(events, exe) && zcodeHooksEnabled(cfg)
}
