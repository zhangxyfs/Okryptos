package agentx

import (
	"fmt"
	"os"
	"path/filepath"

	"okryptos/internal/fsx"
)

// ClaudeHome 返回 Claude 生态配置根目录（OK_CLAUDE_HOME 优先——ok 自留测试隔离口，
// OK_ZCODE_HOME 同款），否则 ~/.claude。CodePilot 等 claude-agent-sdk 兼容宿主经
// settingSources:['user'] 同样加载该目录的 settings.json（hooks 字段 shadow 原样继承）。
func ClaudeHome() string {
	if h := os.Getenv("OK_CLAUDE_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

func claudeSettingsPath() string { return filepath.Join(ClaudeHome(), "settings.json") }

// codepilotHome 仅用于 Detect：只装 CodePilot 的机器可能还没有 ~/.claude。
// OK_CODEPILOT_HOME 为测试隔离口；CLAUDE_GUI_DATA_DIR 是 CodePilot 官方覆盖。
func codepilotHome() string {
	if h := os.Getenv("OK_CODEPILOT_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("CLAUDE_GUI_DATA_DIR"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codepilot")
}

// claudeHookEvents 是 ok 接入的 Claude Code hook 事件（对应 ok 的三条 hook 链路）。
// 命令为 shell 字符串：正斜杠 exe + 双引号（cmd.exe 与 bash 均可执行——探针实测
// cmd 接受正斜杠路径）。输出协议 Claude JSON（args 末尾 "claude"）：注入走
// hookSpecificOutput.additionalContext，Stop 阻断走 decision:block（hook.go 现成）。
var claudeHookEvents = []hookEvent{
	{"UserPromptSubmit", "*", "prompt"},
	{"PostToolUse", "Write|Edit", "post-tool"},
	{"Stop", "*", "stop"},
	{"PreCompact", "*", "compact"},
}

// claudeCommand 生成 hook 命令串（quoted shell 串，见 quotedShellCommand）。
func claudeCommand(exe, okHook string) string {
	return quotedShellCommand(exe, okHook)
}

// isOKClaudeHook 判定一条 hook 条目是否 ok 生成：type=command 且命令串以
// " hook <prompt|post-tool|stop> claude" 结尾。不看 exe basename——改名/迁移/
// 测试二进制都不影响识别。
func isOKClaudeHook(h map[string]any) bool {
	return isOKShellCommand(h, claudeHookEvents, false)
}

// claudeAgent Claude 生态适配器：hook 集成 = 合并写 ~/.claude/settings.json 的
// hooks 字段（Claude Code 与 CodePilot 共享此文件，装一次多宿主生效）；
// 技能目录 ~/.claude/skills（CodePilot 的 skill-discovery 同样扫描）。
type claudeAgent struct{}

func init() { Register(claudeAgent{}) }

func (claudeAgent) ID() string          { return "claude" }
func (claudeAgent) DisplayName() string { return "Claude Code（含 CodePilot 等兼容宿主）" }
func (claudeAgent) HooksTarget() string { return claudeSettingsPath() }
func (claudeAgent) SkillsDir() string   { return filepath.Join(ClaudeHome(), "skills") }

func (claudeAgent) Detect() bool {
	for _, dir := range []string{ClaudeHome(), codepilotHome()} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func (claudeAgent) RemoveHooks() (bool, error) {
	if _, err := os.Stat(claudeSettingsPath()); os.IsNotExist(err) {
		return false, nil
	}
	removed := false
	// 读-改-写包在 fsx.WithFileLock 内（同 InstallHooks）。
	err := fsx.WithFileLock(claudeSettingsPath(), func() error {
		cfg, err := loadClaudeSettings()
		if err != nil {
			return err
		}
		events := claudeEventsOf(cfg)
		if events == nil || !stripOKHooks(events, isOKClaudeHook) {
			return nil
		}
		if err := writeClaudeSettings(cfg); err != nil {
			return fmt.Errorf("移除 claude hooks: %w", err)
		}
		removed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

// EnsureHooks 自愈：settings 存在、曾安装过 ok hooks 且内容过期（exe 迁移、超时
// 变更）时重写；从未安装（无任何 ok 条目）则 no-op——用户显式移除的集成不复活。
func (claudeAgent) EnsureHooks(exe string) error {
	if _, err := os.Stat(claudeSettingsPath()); err != nil {
		return nil
	}
	// 读-改-写包在 fsx.WithFileLock 内（同 InstallHooks）。
	return fsx.WithFileLock(claudeSettingsPath(), func() error {
		cfg, err := loadClaudeSettings()
		if err != nil {
			return err
		}
		events := claudeEventsOf(cfg)
		if events == nil || !hasOKClaudeHook(events) || claudeHooksCurrent(events, exe) {
			return nil
		}
		events = claudeEventsEdit(cfg)
		stripOKHooks(events, isOKClaudeHook)
		appendShellOKGroups(events, claudeHookEvents, exe, claudeCommand)
		return writeClaudeSettings(cfg)
	})
}

func (claudeAgent) HooksInstalled() bool {
	cfg, err := loadClaudeSettings()
	if err != nil {
		return false
	}
	events := claudeEventsOf(cfg)
	if events == nil {
		return false
	}
	exe, err := currentCLIExe()
	if err != nil {
		return false
	}
	return claudeHooksCurrent(events, exe)
}

// loadClaudeSettings 读 settings.json；文件不存在返回空对象，解析失败报错
// （不覆盖损坏文件）。map 合并写会重排 key 顺序——未知字段内容保留，代价可接受。
func loadClaudeSettings() (map[string]any, error) {
	return loadSettingsJSON(claudeSettingsPath(), "claude settings.json")
}

// claudeEventsOf 取 hooks 事件表（只读视图），不做任何创建。
func claudeEventsOf(cfg map[string]any) map[string]any {
	return hookEventsOf(cfg)
}

// claudeEventsEdit 取 hooks 事件表供写入：缺失时创建（Claude Code 无 enabled 开关）。
func claudeEventsEdit(cfg map[string]any) map[string]any {
	return hookEventsEdit(cfg)
}

// hasOKClaudeHook 报告事件表里是否存在任何 ok 自有 hook。
func hasOKClaudeHook(events map[string]any) bool {
	return containsOKHook(events, isOKClaudeHook)
}

// claudeHooksCurrent 报告三事件的 ok hook 是否均为当前期望形态
// （command=exe、matcher 与 timeout 正确）。
func claudeHooksCurrent(events map[string]any, exe string) bool {
	return shellHooksCurrent(events, claudeHookEvents, exe, isOKClaudeHook, claudeCommand, "")
}

// writeClaudeSettings 备份后写回 settings.json（MarshalIndent，未知字段保留）。
func writeClaudeSettings(cfg map[string]any) error {
	return writeSettingsJSON(claudeSettingsPath(), cfg)
}

func (claudeAgent) InstallHooks(exe string) error {
	// 读-改-写全程包在 fsx.WithFileLock 内：selfHealHooks 每个 prompt 都跑，与
	// GUI /api/setup/hooks 或另一 hook 进程并发时裸跑会 load→改→rename 互相
	// 覆盖，用户自装的第三方 hooks 被静默丢弃（config.toml/state 同款锁纪律）。
	return fsx.WithFileLock(claudeSettingsPath(), func() error {
		cfg, err := loadClaudeSettings()
		if err != nil {
			return err
		}
		events := claudeEventsEdit(cfg)
		stripOKHooks(events, isOKClaudeHook)
		appendShellOKGroups(events, claudeHookEvents, exe, claudeCommand)
		return writeClaudeSettings(cfg)
	})
}
