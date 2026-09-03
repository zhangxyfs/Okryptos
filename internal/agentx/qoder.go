package agentx

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"okryptos/internal/fsx"
)

// QoderHome 返回 Qoder CN CLI 配置根目录：OK_QODER_HOME（ok 自留测试隔离口，
// OK_CLAUDE_HOME 同款命名）> QODERCN_CONFIG_DIR（Qoder CN CLI 官方重定位环境变量，
// 文档化）> ~/.qoder-cn（终端 CLI 默认用户配置目录）。注意：QoderCN IDE 的 hooks
// 走独立的 ~/.lingma/settings.json（灵码内核，仅 5 事件、Stop 不可阻断），不读本
// 目录——本适配器只覆盖终端 CLI 面。
// bundle 源码（@qodercn-ai/qoderclicn 1.1.20）另有 QODERCN_CLI_HOME / GEMINI_CLI_HOME
// 参与解析（CLI_HOME 作为 ~/.qoder-cn 的父目录）——非文档化且语义嵌套，不接入。
func QoderHome() string {
	if h := os.Getenv("OK_QODER_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("QODERCN_CONFIG_DIR"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".qoder-cn")
}

func qoderSettingsPath() string { return filepath.Join(QoderHome(), "settings.json") }

// qoderHookEvents 是 ok 接入的 Qoder hook 事件（对应 ok 的三条 hook 链路）。
// Qoder 的 hooks 契约逐字兼容 Claude Code：settings.json 的 hooks 分组、command 类型、
// stdin JSON（hook_event_name/session_id/cwd/tool_name/tool_input）、退出码 0/2、
// stdout JSON（decision/reason/hookSpecificOutput.additionalContext）——输出协议
// Claude JSON（args 末尾 "claude"），hook.go 输出层零改动。PostToolUse 追
// Write|Edit（Qoder 与 Claude 同款写盘工具），不追 Bash——与 claude 对齐。
var qoderHookEvents = []hookEvent{
	{"UserPromptSubmit", "*", "prompt"},
	{"PostToolUse", "Write|Edit", "post-tool"},
	{"Stop", "*", "stop"},
}

// qoderCommand 生成 hook 命令串，按平台分叉：
//   - Windows：Qoder 执行 command 型 hook 时走 cmd.exe /d /s /c "<整行>"
//     （bundle 源码 hooks 派发层实证：cmd /s 会把命令行首尾引号剥掉，带内嵌引号的
//     quoted 命令串会被剥坏静默不执行——codex #38168 同源问题），故 command 用
//     .cmd 包装文件绝对路径裸串（无引号、反斜杠形态），与 exe 位置解耦：exe 迁移
//     只改包装文件内容，settings.json 不变，无需信任/哈希刷新。
//   - 其他平台（linux/darwin）：claudeCommand 同款 quoted shell 串（Qoder 走
//     sh -lc，quoted 形态无碍）。
//
// 已知限制：用户名含空格时包装路径带空格，cmd /s 仍会截断——上游修复前不额外处理。
func qoderCommand(exe, okHook string) string {
	if runtime.GOOS == "windows" {
		return hookWrapperPath(QoderHome(), okHook)
	}
	return quotedShellCommand(exe, okHook)
}

// ensureQoderWrappers 确保三个包装文件存在且内容为当前 exe（缺失/过期重写，
// 已当前则不写盘）。仅 Windows 调用。
func ensureQoderWrappers(exe string) error {
	return ensureHookWrappers(QoderHome(), "写入 qoder hook 包装", qoderHookEvents, exe)
}

// removeQoderWrappers 删除三个包装文件，返回是否有删除。仅删内容确为 ok 生成的
// （含 " hook <okHook> claude"）——防误删用户同名文件。仅 Windows 调用。
func removeQoderWrappers() bool {
	return removeHookWrappers(QoderHome(), qoderHookEvents)
}

// isOKQoderHook 判定一条 hook 条目是否 ok 生成：type=command 且命令串形态匹配——
// Windows 认包装文件裸路径（trim 后以 / 或 \ 分隔的 ok-hook-<okHook>.cmd 结尾，
// 大小写不敏感，hasSuffixFold 语意）与 quoted 后缀 " hook <prompt|post-tool|stop>
// claude" 两种形态；其他平台只认 quoted 后缀。不看 exe basename——改名/迁移/测试
// 二进制都不影响识别。
func isOKQoderHook(h map[string]any) bool {
	return isOKShellCommand(h, qoderHookEvents, true)
}

// qoderAgent Qoder CN CLI 适配器：hook 集成 = 合并写 ~/.qoder-cn/settings.json 的
// hooks 分组 + 顶层 hooksConfig.enabled 开关（零成本开关：Qoder 默认关闭 hooks
// 派发——见 qoderEnableHooksConfig）；技能目录 ~/.qoder-cn/skills（bundle 源码
// getUserSkillsDir = join(配置目录, "skills")，SKILL.md 格式与 Claude 逐字一致，
// 现有共享模板零适配）。
type qoderAgent struct{}

func init() { Register(qoderAgent{}) }

func (qoderAgent) ID() string          { return "qoder" }
func (qoderAgent) DisplayName() string { return "Qoder CN CLI" }

// HooksTarget 展示路径返回 settings.json；Windows 另在同目录维护 ok-hook-*.cmd
// 包装文件（settings.json command 指向它们，见 qoderCommand）。
func (qoderAgent) HooksTarget() string { return qoderSettingsPath() }
func (qoderAgent) SkillsDir() string   { return filepath.Join(QoderHome(), "skills") }

func (qoderAgent) Detect() bool {
	info, err := os.Stat(QoderHome())
	return err == nil && info.IsDir()
}

// loadQoderSettings 读 settings.json；文件不存在返回空对象，解析失败报错
// （不覆盖损坏文件）。map 合并写会重排 key 顺序——未知字段内容保留，代价可接受。
func loadQoderSettings() (map[string]any, error) {
	return loadSettingsJSON(qoderSettingsPath(), "qoder settings.json")
}

// qoderEventsOf 取 hooks 事件表（只读视图），不做任何创建。
func qoderEventsOf(cfg map[string]any) map[string]any {
	return hookEventsOf(cfg)
}

// qoderEventsEdit 取 hooks 事件表供写入：缺失时创建。
func qoderEventsEdit(cfg map[string]any) map[string]any {
	return hookEventsEdit(cfg)
}

// hasOKQoderHook 报告事件表里是否存在任何 ok 自有 hook。
func hasOKQoderHook(events map[string]any) bool {
	return containsOKHook(events, isOKQoderHook)
}

// qoderHooksCurrent 报告三事件的 ok hook 是否均为当前期望形态（command=当前命令、
// matcher 与 timeout 正确）；Windows 另要求包装文件内容为当前 exe（exe 迁移后包装
// 过期 = 集成失效，需自愈重写）。
func qoderHooksCurrent(events map[string]any, exe string) bool {
	return shellHooksCurrent(events, qoderHookEvents, exe, isOKQoderHook, qoderCommand, QoderHome())
}

// qoderHooksConfigEnabled 报告 settings 顶层 hooksConfig 的 enabled 是否为真。
// Qoder 对 hooksConfig.enabled 未显式设置时默认关闭——hooks 装好也静默不派发
// （bundle 源码实证：enableHooks = !disableAllHooks && hooksConfig.enabled；
// settings schema 默认 hooksConfig = {} → enabled 未定义 → false，与 codex 的
// codex_hooks 特性开关同款教训）。disableAllHooks 是用户全局 kill switch，
// ok 不读取不修改——只认 hooksConfig。
func qoderHooksConfigEnabled(cfg map[string]any) bool {
	hc, _ := cfg["hooksConfig"].(map[string]any)
	if hc == nil {
		return false
	}
	enabled, _ := hc["enabled"].(bool)
	return enabled
}

// qoderEnableHooksConfig 在 settings 顶层写入/合并 hooksConfig 并把 enabled 置 true，
// 保留 hooksConfig 其余键（如 notifications），返回是否有改动。
func qoderEnableHooksConfig(cfg map[string]any) bool {
	hc, _ := cfg["hooksConfig"].(map[string]any)
	if hc == nil {
		cfg["hooksConfig"] = map[string]any{"enabled": true}
		return true
	}
	enabled, _ := hc["enabled"].(bool)
	if enabled {
		return false
	}
	hc["enabled"] = true
	return true
}

// writeQoderSettings 备份后写回 settings.json（MarshalIndent，未知字段保留）。
func writeQoderSettings(cfg map[string]any) error {
	return writeSettingsJSON(qoderSettingsPath(), cfg)
}

func (qoderAgent) InstallHooks(exe string) error {
	if runtime.GOOS == "windows" {
		// 先写 3 个包装文件（当前 exe）——settings.json command 指向它们。
		if err := ensureQoderWrappers(exe); err != nil {
			return err
		}
	}
	// 读-改-写全程包在 fsx.WithFileLock 内：selfHealHooks 每个 prompt 都跑，与
	// GUI /api/setup/hooks 并发时裸跑会 load→改→rename 互相覆盖、丢第三方 hooks。
	return fsx.WithFileLock(qoderSettingsPath(), func() error {
		cfg, err := loadQoderSettings()
		if err != nil {
			return err
		}
		events := qoderEventsEdit(cfg)
		stripOKHooks(events, isOKQoderHook)
		appendShellOKGroups(events, qoderHookEvents, exe, qoderCommand)
		// hooksConfig.enabled 默认关闭（装了也静默不派发）——安装时一并开启。
		qoderEnableHooksConfig(cfg)
		return writeQoderSettings(cfg)
	})
}

// RemoveHooks 移除 settings.json 里 ok 的 hooks 条目；Windows 另删 3 个包装文件
// （仅删内容确为 ok 生成的，防误删用户同名文件）。hooksConfig.enabled 开关单独
// 存在无副作用（只是允许 Qoder 派发 hooks；关掉会连带停掉用户的第三方 hooks），
// 不随移除关闭。
func (qoderAgent) RemoveHooks() (bool, error) {
	if _, err := os.Stat(qoderSettingsPath()); os.IsNotExist(err) {
		// settings.json 不存在：无指向包装文件的命令，删包装无死命令风险。
		if runtime.GOOS == "windows" {
			return removeQoderWrappers(), nil
		}
		return false, nil
	}
	removed := false
	wrappersRemoved := false
	// 读-改-写包在 fsx.WithFileLock 内（同 InstallHooks）。
	err := fsx.WithFileLock(qoderSettingsPath(), func() error {
		cfg, err := loadQoderSettings()
		if err != nil {
			// 解析失败即停，包装文件保留——settings.json 里指向它们的命令还在（L-01）。
			return err
		}
		events := qoderEventsOf(cfg)
		if events != nil && hasOKQoderHook(events) {
			stripOKHooks(events, isOKQoderHook)
			if err := writeQoderSettings(cfg); err != nil {
				return fmt.Errorf("移除 qoder hooks: %w", err)
			}
			removed = true
		}
		// ok 条目清完再删包装——反序会在上方失败路径留下指向已删文件的死命令（L-01）。
		if runtime.GOOS == "windows" {
			wrappersRemoved = removeQoderWrappers()
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return removed || wrappersRemoved, nil
}

// EnsureHooks 自愈：settings 存在、曾安装过 ok hooks 且内容过期（exe 迁移、超时
// 变更）或 hooksConfig.enabled 被关时重写/重开；从未安装（无任何 ok 条目）则
// no-op——用户显式移除的集成不复活。Windows 先重写过期/缺失的包装文件（exe 迁移
// 只动包装内容）——包装刷新后 settings.json 命令（包装路径）往往仍当前，无需重写。
func (qoderAgent) EnsureHooks(exe string) error {
	if _, err := os.Stat(qoderSettingsPath()); err != nil {
		return nil
	}
	// 读-改-写包在 fsx.WithFileLock 内（同 InstallHooks）。
	return fsx.WithFileLock(qoderSettingsPath(), func() error {
		cfg, err := loadQoderSettings()
		if err != nil {
			return err
		}
		events := qoderEventsOf(cfg)
		if events == nil || !hasOKQoderHook(events) {
			return nil // 从未安装（无任何 ok 条目）不复活——含用户显式移除
		}
		if runtime.GOOS == "windows" {
			if err := ensureQoderWrappers(exe); err != nil {
				return err
			}
		}
		if qoderHooksCurrent(events, exe) && qoderHooksConfigEnabled(cfg) {
			return nil
		}
		events = qoderEventsEdit(cfg)
		stripOKHooks(events, isOKQoderHook)
		appendShellOKGroups(events, qoderHookEvents, exe, qoderCommand)
		qoderEnableHooksConfig(cfg)
		return writeQoderSettings(cfg)
	})
}

func (qoderAgent) HooksInstalled() bool {
	cfg, err := loadQoderSettings()
	if err != nil {
		return false
	}
	events := qoderEventsOf(cfg)
	if events == nil {
		return false
	}
	exe, err := currentCLIExe()
	if err != nil {
		return false
	}
	// hooksConfig.enabled 关闭/缺失 = 集成失效（hooks 静默不派发），视为未安装。
	return qoderHooksCurrent(events, exe) && qoderHooksConfigEnabled(cfg)
}
