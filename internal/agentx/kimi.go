package agentx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"openknowledge/internal/config"
	"openknowledge/internal/fsx"
	"openknowledge/internal/registry"
)

const MarkerBegin = "# >>> openknowledge hooks >>>"
const MarkerEnd = "# <<< openknowledge hooks <<<"

// KimiHome 返回 kimi-code 配置目录（KIMI_CODE_HOME 优先）。
func KimiHome() string {
	if h := os.Getenv("KIMI_CODE_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kimi-code")
}

func kimiConfigPath() string { return filepath.Join(KimiHome(), "config.toml") }

// HooksBlockFor 生成指向 exe 的 hooks 配置块；三条 hook 统一使用 timeoutSec 秒超时
// （Windows 上 ok.exe 冷启动 + daemon 转发在高负载下可超过 5s，超时会被 kimi 静默杀死）。
// exe 必须加引号：路径含空格（如 C:/Users/John Doe/）时按空格分词会断裂，hook 永不执行。
func HooksBlockFor(exe string, timeoutSec int) string {
	exe = filepath.ToSlash(exe)
	return fmt.Sprintf(`[[hooks]]
event = "UserPromptSubmit"
command = "\"%s\" hook prompt"
timeout = %d

[[hooks]]
event = "PostToolUse"
matcher = "Write|Edit"
command = "\"%s\" hook post-tool"
timeout = %d

[[hooks]]
event = "Stop"
command = "\"%s\" hook stop"
timeout = %d
`, exe, timeoutSec, exe, timeoutSec, exe, timeoutSec)
}

// HookTimeoutSec 返回写入 hooks 的超时秒数：全局配置 [hooks] timeout_sec，
// 读取失败或未配置时回退 10（与 config.Default 一致）。
func HookTimeoutSec() int {
	cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
	if err != nil || cfg.Hooks.TimeoutSec <= 0 {
		return 10
	}
	return cfg.Hooks.TimeoutSec
}

// okHookCommand 匹配指向 ok hook 的 command 行（如 "ok hook prompt"、
// "\"D:/x/ok.exe\" hook stop"——exe 加引号后值内含转义引号，需一并兼容）。
// okd 形态（"okd.exe hook prompt"）同样命中：它是 gui-split 注册 bug 留下的存量
// 形态（2026-08-22 实证，见知识库"二进制拆分后daemon内注册命令必须换算CLI入口"），
// okd 是 daemon 二进制，hook 注册的合法入口只有 ok——不识别则存量块永远剥不掉，
// 与标记块并存导致同一事件重复派发（双注入实证 2026-08-28）。
// 完整形态收紧（L-06）：ok/okd/ok.exe/okd.exe（可带引号路径前缀）+ " hook " +
// 三个 ok 子命令之一 + 值结束——用户自装同名 ok 工具的其它命令行（"ok deploy"、
// "ok hook run"、"ok hook prompt --verbose"）不再命中误删。残余不可区分形态：
// 用户工具恰好也有 `hook prompt|post-tool|stop` 子命令且命令行形态逐字相同。
var okHookCommand = regexp.MustCompile(`(?i)^\s*command\s*=\s*"(?:[^"]|\\")*\bokd?(?:\.exe)?(?:\\")?\s+hook\s+(?:prompt|post-tool|stop)(?:\\")?"\s*$`)

// StripLegacyOKHooks 移除配置中所有指向 ok hook 的无标记 [[hooks]] 表
// （历史遗留的手动粘贴块），其它工具的 hooks 原样保留。
// 注意：被删表连同其后直到下一个 section 头的所有行一起移除（TOML 表所有权范围），
// 调用方需保证不应触碰的区域（如标记块内部）不传入本函数。
func StripLegacyOKHooks(content string) string {
	lines := strings.Split(content, "\n")
	removed := make([]bool, len(lines))
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "[[hooks]]" {
			continue
		}
		j := i + 1
		isOK := false
		for ; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if strings.HasPrefix(t, "[") {
				break
			}
			if okHookCommand.MatchString(lines[j]) {
				isOK = true
			}
		}
		if !isOK {
			continue
		}
		for k := i; k < j; k++ {
			removed[k] = true
		}
		// 连同删除紧随其后的空行（块间分隔），避免留下成串空行
		for k := j; k < len(lines) && strings.TrimSpace(lines[k]) == ""; k++ {
			removed[k] = true
		}
		// 连同删除紧邻其前的 OpenKnowledge 注释行（init 曾打印的引导注释）
		if i > 0 && !removed[i-1] {
			t := strings.TrimSpace(lines[i-1])
			if strings.HasPrefix(t, "#") && strings.Contains(strings.ToLower(t), "openknowledge") {
				removed[i-1] = true
			}
		}
	}
	var out []string
	for i, l := range lines {
		if !removed[i] {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// stripMarkerBlocks 移除 content 中全部标记块（含标记行），供 UpsertHooksBlock
// 在 upsert 前清理重复旧块——只原位替换第一个会让其余旧块残留，hook 双派发。
// 有头无尾（损坏块）报错，不覆盖原文件。
func stripMarkerBlocks(content, configPath string) (string, error) {
	for {
		i := strings.Index(content, MarkerBegin)
		if i < 0 {
			return content, nil
		}
		j := strings.Index(content[i:], MarkerEnd)
		if j < 0 {
			return "", fmt.Errorf("hooks 标记块损坏（缺少结束标记）: %s", configPath)
		}
		j += i
		head := strings.TrimRight(content[:i], "\n")
		tail := strings.TrimPrefix(content[j+len(MarkerEnd):], "\n")
		if head == "" {
			content = tail
		} else {
			content = head + "\n" + tail
		}
	}
}

// UpsertHooksBlock 以标记块幂等写入 hooks 配置：先清除存量 ok hooks（含无标记的
// 历史遗留块），已存在标记块则原位替换（exe 路径随之更新），否则追加。
// 读-改-写包在 fsx.WithFileLock 内（宿主文件，kimi config.toml 与 dsh patch 共用）。
func UpsertHooksBlock(configPath, block string) error {
	return fsx.WithFileLock(configPath, func() error {
		return upsertHooksBlockLocked(configPath, block)
	})
}

func upsertHooksBlockLocked(configPath, block string) error {
	data, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	content := string(data)
	// 存量 ok hooks 只清理标记块之外的区域；块内内容交给原位替换/损坏报错逻辑。
	// 若对整个 content 调用 StripLegacyOKHooks，标记块自身的 ok hook 表会被删掉，
	// 连带着吃掉两个标记行，导致原位替换退化为尾部追加、损坏标记检测失效。
	if i := strings.Index(content, MarkerBegin); i >= 0 {
		if j := strings.Index(content, MarkerEnd); j > i {
			// 第一个标记块留给下面的原位替换；其后若还有重复标记块（历史 bug 可能
			// 留下多个）先整段剥离——只换第一个会让旧块残留双派发。先剥块再清
			// 遗留表：StripLegacyOKHooks 不处理标记块内部（见其注释）。
			tail, err := stripMarkerBlocks(content[j+len(MarkerEnd):], configPath)
			if err != nil {
				return err
			}
			content = StripLegacyOKHooks(content[:i]) + content[i:j+len(MarkerEnd)] + StripLegacyOKHooks(tail)
		} else {
			// 有头无尾：保留原样，交给下面的损坏标记分支报错
			content = StripLegacyOKHooks(content[:i]) + content[i:]
		}
	} else {
		content = StripLegacyOKHooks(content)
	}
	wrapped := MarkerBegin + "\n" + block + MarkerEnd + "\n"
	i := strings.Index(content, MarkerBegin)
	j := strings.Index(content, MarkerEnd)
	var out string
	switch {
	case i >= 0 && j > i:
		tail := strings.TrimPrefix(content[j+len(MarkerEnd):], "\n")
		out = content[:i] + wrapped + tail
	case i >= 0:
		return fmt.Errorf("hooks 标记块损坏（缺少结束标记）: %s", configPath)
	default:
		sep := ""
		if len(content) > 0 && !strings.HasSuffix(content, "\n") {
			sep = "\n"
		}
		out = content + sep + "\n" + wrapped
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	return fsx.WriteFile(configPath, []byte(out), 0o644)
}

// EnsureHooksBlock hook 入口自检：kimi-code 有时会清掉标记注释行，使标记块丢失
// （孤儿 hook 表仍在、hook 照常运行，但下次 setup 的去重依据没了）。标记块缺失且
// 仍残留 ok 的 [[hooks]] 表（okHookCommand 命中）时自动备份并重新 Upsert 修复；
// 标记块存在、或完全无 ok hook 表（用户显式卸载/从未安装）则不动——与其它适配器
// "无 ok 条目不复活"对称，显式移除的集成不被自愈复活。调用方按 fail-open 处理返回错误。
// 检查与重写整体在锁内（WithFileLock 不可重入，内部走 upsertHooksBlockLocked）。
func EnsureHooksBlock(configPath, exe string) error {
	return fsx.WithFileLock(configPath, func() error {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}
		content := string(data)
		if strings.Contains(content, MarkerBegin) {
			return nil
		}
		hasOKHook := false
		for _, l := range strings.Split(content, "\n") {
			if okHookCommand.MatchString(l) {
				hasOKHook = true
				break
			}
		}
		if !hasOKHook {
			return nil
		}
		_ = os.WriteFile(configPath+".bak-openknowledge", data, 0o644)
		return upsertHooksBlockLocked(configPath, HooksBlockFor(exe, HookTimeoutSec()))
	})
}

// kimiAgent kimiCode 适配器。
type kimiAgent struct{}

func init() { Register(kimiAgent{}) }

func (kimiAgent) ID() string          { return "kimi" }
func (kimiAgent) DisplayName() string { return "Kimi Code" }
func (kimiAgent) SkillsDir() string   { return SkillsHome() }
func (kimiAgent) HooksTarget() string { return kimiConfigPath() }

func (kimiAgent) Detect() bool {
	info, err := os.Stat(KimiHome())
	return err == nil && info.IsDir()
}

func (kimiAgent) HooksInstalled() bool {
	data, err := os.ReadFile(kimiConfigPath())
	if err != nil || !strings.Contains(string(data), MarkerBegin) {
		return false
	}
	// 旧 exe 路径（迁移/改名）视为过期——以解析后的当前可执行文件为基准，
	// 与 claude/zcode 同款全量比对（含超时），doctor 误报"已接入"则自愈永不触发。
	exe, err := currentCLIExe()
	if err != nil {
		return false
	}
	return strings.Contains(string(data), HooksBlockFor(exe, HookTimeoutSec()))
}

func (kimiAgent) InstallHooks(exe string) error {
	cfgPath := kimiConfigPath()
	// 备份+写整体在锁内（WithFileLock 不可重入，内部走 locked 变体）。
	return fsx.WithFileLock(cfgPath, func() error {
		if data, err := os.ReadFile(cfgPath); err == nil {
			_ = os.WriteFile(cfgPath+".bak-openknowledge", data, 0o644)
		}
		return upsertHooksBlockLocked(cfgPath, HooksBlockFor(exe, HookTimeoutSec()))
	})
}

func (kimiAgent) RemoveHooks() (bool, error) {
	cfgPath := kimiConfigPath()
	removed := false
	// 读-改-写包在 fsx.WithFileLock 内（同 InstallHooks）。
	err := fsx.WithFileLock(cfgPath, func() error {
		data, err := os.ReadFile(cfgPath)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		content := string(data)
		orig := content
		i := strings.Index(content, MarkerBegin)
		j := strings.Index(content, MarkerEnd)
		if i >= 0 && j > i {
			tail := strings.TrimPrefix(content[j+len(MarkerEnd):], "\n")
			head := strings.TrimRight(content[:i], "\n")
			content = head + "\n" + tail
		}
		content = StripLegacyOKHooks(content)
		if content == orig {
			return nil
		}
		if err := fsx.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("移除 hooks 配置: %w", err)
		}
		removed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

func (kimiAgent) EnsureHooks(exe string) error { return EnsureHooksBlock(kimiConfigPath(), exe) }
