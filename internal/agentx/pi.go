package agentx

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"okryptos/internal/fsx"
)

//go:embed pi_extension.ts
var piExtensionTemplate string

// piExtensionMarker 本工具生成的扩展文件头标记（RemoveHooks 据此识别归属）。
// legacyPiExtensionMarker 是 2.25.0 改名前（OpenKnowledge 时代）的旧标记：
// 归属识别双认，渲染只写新标记。
const (
	piExtensionMarker       = "// okryptos hooks (managed by ok.exe; do not edit)"
	legacyPiExtensionMarker = "// openknowledge hooks (managed by ok.exe; do not edit)"
)

// ownsPiExtension 报告内容是否本工具生成（新旧标记任一命中）。
func ownsPiExtension(content string) bool {
	return strings.Contains(content, piExtensionMarker) ||
		strings.Contains(content, legacyPiExtensionMarker)
}

// PiHome 返回 pi 配置根目录（PI_CODING_AGENT_DIR 优先）。
func PiHome() string {
	if h := os.Getenv("PI_CODING_AGENT_DIR"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "agent")
}

// piAgent Pi 适配器：hook 集成 = 写 TS 扩展到 ~/.pi/agent/extensions/。
type piAgent struct{}

func init() { Register(piAgent{}) }

func (piAgent) ID() string          { return "pi" }
func (piAgent) DisplayName() string { return "Pi" }
func (piAgent) SkillsDir() string   { return SkillsHome() }
func (piAgent) HooksTarget() string { return piExtensionPath() }

func piExtensionPath() string { return filepath.Join(PiHome(), "extensions", "okryptos.ts") }

// legacyPiExtensionPath 是 2.25.0 改名前的扩展路径。pi 自动加载 extensions
// 目录——旧文件不删则双扩展双注入。
func legacyPiExtensionPath() string { return filepath.Join(PiHome(), "extensions", "openknowledge.ts") }

// removeLegacyPiExtension 删除旧名扩展（仅本工具生成的），防自动加载双注入。
// 返回是否存在并被清除（供迁移判定：legacy 在 = 旧版接入过 hooks）。
func removeLegacyPiExtension() bool {
	p := legacyPiExtensionPath()
	data, err := os.ReadFile(p)
	if err != nil || !ownsPiExtension(string(data)) {
		return false
	}
	return os.Remove(p) == nil
}

func (piAgent) Detect() bool {
	info, err := os.Stat(PiHome())
	return err == nil && info.IsDir()
}

// piTemplateFingerprint 模板内容指纹（sha256 前 12 位十六进制），随模板升级变化。
func piTemplateFingerprint() string {
	sum := sha256.Sum256([]byte(piExtensionTemplate))
	return fmt.Sprintf("%x", sum)[:12]
}

// renderPiExtension 渲染扩展：头标记 + 指纹行 + 烘焙 exe 绝对路径的模板。
func renderPiExtension(exe string) string {
	body := strings.ReplaceAll(piExtensionTemplate, "{{EXE}}", filepath.ToSlash(exe))
	return piExtensionMarker + "\n// fingerprint: " + piTemplateFingerprint() + "\n" + body
}

func (piAgent) HooksInstalled() bool {
	data, err := os.ReadFile(piExtensionPath())
	if err != nil {
		return false
	}
	content := string(data)
	if !ownsPiExtension(content) ||
		!strings.Contains(content, "// fingerprint: "+piTemplateFingerprint()) {
		return false
	}
	// 旧 exe 路径视为过期（与 dshAgent 同款，以解析后的当前可执行文件为基准）
	exe, err := currentCLIExe()
	if err != nil {
		return false
	}
	return content == renderPiExtension(exe)
}

func (piAgent) InstallHooks(exe string) error {
	removeLegacyPiExtension() // 改名迁移：旧名扩展随安装清除
	path := piExtensionPath()
	if data, err := os.ReadFile(path); err == nil {
		if !ownsPiExtension(string(data)) {
			if err := os.WriteFile(path+".bak-openknowledge", data, 0o644); err != nil {
				return fmt.Errorf("备份既有扩展失败: %w", err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("读取既有扩展失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsx.WriteFile(path, []byte(renderPiExtension(exe)), 0o644)
}

func (piAgent) RemoveHooks() (bool, error) {
	removed := removeLegacyPiExtension()
	path := piExtensionPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return removed, nil
	}
	if err != nil {
		return removed, err
	}
	if !ownsPiExtension(string(data)) {
		return removed, nil // 非本工具生成，不删
	}
	if err := os.Remove(path); err != nil {
		return removed, fmt.Errorf("删除 pi 扩展: %w", err)
	}
	return true, nil
}

// EnsureHooks 自愈：文件存在且为本工具生成、但内容过期（模板升级或 exe 迁移）
// 时重写；文件不存在时为 no-op（pi 无扩展即不会触发 hook，无需修复）。
// 改名迁移特例（2.25.0）：新名缺失但旧名扩展在 = 旧版接入过 hooks——此时
// "缺失不复活"让位给迁移，按当前 exe 渲染新文件再删旧件（同 opencode 适配器）。
func (piAgent) EnsureHooks(exe string) error {
	path := piExtensionPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if removeLegacyPiExtension() {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			return fsx.WriteFile(path, []byte(renderPiExtension(exe)), 0o644)
		}
		return nil
	}
	removeLegacyPiExtension() // 新名已在：旧件直接清除
	if !ownsPiExtension(string(data)) {
		return nil
	}
	rendered := renderPiExtension(exe)
	if string(data) == rendered {
		return nil
	}
	return fsx.WriteFile(path, []byte(rendered), 0o644)
}
