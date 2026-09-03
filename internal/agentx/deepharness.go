package agentx

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"okryptos/internal/fsx"
)

//go:embed dsh_plugin.js
var dshPluginTemplate string

// dshPluginMarker 本工具生成的插件文件头标记（RemoveHooks 据此识别归属）。
// legacyDSHPluginMarker 是 2.25.0 改名前（OpenKnowledge 时代）的旧标记：
// 归属识别双认，渲染只写新标记。
const (
	dshPluginMarker       = "// okryptos hooks (managed by ok.exe; do not edit)"
	legacyDSHPluginMarker = "// openknowledge hooks (managed by ok.exe; do not edit)"
)

// ownsDSHPlugin 报告内容是否本工具生成（新旧标记任一命中）。
func ownsDSHPlugin(content string) bool {
	return strings.Contains(content, dshPluginMarker) ||
		strings.Contains(content, legacyDSHPluginMarker)
}

// DSHHome 返回 DeepSeek Harness 家目录。解析序：OK_DSH_HOME（ok 自留测试隔离口，
// OK_ZCODE_HOME 同款）> DSH_HOME（官方重定位变量，packages/util/home-paths 的
// resolveDshHome）> ~/.dsh。
func DSHHome() string {
	if h := os.Getenv("OK_DSH_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("DSH_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".dsh")
}

// dshPluginPath 插件写入目标：<home>/plugins/okryptos/index.js。
// DSH 无插件目录自动扫描，位置为 ok 自选，经 cordis.patch.yml 绝对路径挂载。
func dshPluginPath() string { return filepath.Join(DSHHome(), "plugins", "okryptos", "index.js") }

// legacyDSHPluginDir 是 2.25.0 改名前的插件目录；patch 行内的 file URL 指向它，
// 不清理则旧目录成孤儿（patch 块由 upsert 双标记剥除，目录须显式删）。
func legacyDSHPluginDir() string { return filepath.Join(DSHHome(), "plugins", "openknowledge") }

// removeLegacyDSHPlugin 删除旧名插件目录（仅当其中 index.js 为本工具生成）。
func removeLegacyDSHPlugin() {
	dir := legacyDSHPluginDir()
	data, err := os.ReadFile(filepath.Join(dir, "index.js"))
	if err != nil || !ownsDSHPlugin(string(data)) {
		return
	}
	_ = os.RemoveAll(dir)
}

// dshPatchPath 家目录级 patch 文件：<home>/cordis.patch.yml（所有 profile 共享，
// DSH 文档明示的家目录级 patch 层）。
func dshPatchPath() string { return filepath.Join(DSHHome(), "cordis.patch.yml") }

// dshTemplateFingerprint 模板内容指纹（sha256 前 12 位十六进制），随模板升级变化。
func dshTemplateFingerprint() string {
	sum := sha256.Sum256([]byte(dshPluginTemplate))
	return fmt.Sprintf("%x", sum)[:12]
}

// renderDSHPlugin 渲染插件：头标记 + 指纹行 + 烘焙 exe 绝对路径的模板。
func renderDSHPlugin(exe string) string {
	body := strings.ReplaceAll(dshPluginTemplate, "{{EXE}}", filepath.ToSlash(exe))
	return dshPluginMarker + "\n// fingerprint: " + dshTemplateFingerprint() + "\n" + body
}

// dshPluginFileURL 插件绝对路径的 file:// URL 形态。实机验证（Task 6）：vendored
// cordis loader 把 patch 的 name 直接交给 Node ESM 解析（vendor/loader/src/config/
// tree.ts 的 import(name)），Windows 绝对路径（D:/...）报
// ERR_UNSUPPORTED_ESM_URL_SCHEME，必须 file:/// URL；POSIX 绝对路径同样适用。
func dshPluginFileURL() string {
	p := filepath.ToSlash(dshPluginPath())
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// dshYAMLSingleQuoted 把 s 包成 YAML 单引号标量：串内单引号双写（''）——单引号
// 字符串的唯一转义规则（L-04）。当前 dshPluginFileURL 经 url.URL 百分号编码后
// 不含裸 '，本函数是生成层变化时的防线；YAML 解析后 '' 还原为 '，消费方拿到原串。
func dshYAMLSingleQuoted(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// dshPatchBlock 家目录 patch 行：file:// URL 挂载本地插件（cordis patch 的 name
// 字段直接进 Node ESM import；name 经 dshYAMLSingleQuoted 转义，路径含 ' 时
// 标量不截断、file URL 不断裂）。
func dshPatchBlock() string {
	return "- insert:\n    - id: ok-hooks\n      name: " + dshYAMLSingleQuoted(dshPluginFileURL()) + "\n"
}

// dshAgent DeepSeek Harness 适配器：hook 集成 = 本地 JS 插件 + 家目录 patch 行挂载；
// 技能共享 SkillsHome（DSH 原生扫描 ~/.agents/skills）。
type dshAgent struct{}

func init() { Register(dshAgent{}) }

func (dshAgent) ID() string          { return "dsh" }
func (dshAgent) DisplayName() string { return "DeepSeek Harness" }
func (dshAgent) SkillsDir() string   { return SkillsHome() }
func (dshAgent) HooksTarget() string { return dshPluginPath() }

func (dshAgent) Detect() bool {
	info, err := os.Stat(DSHHome())
	return err == nil && info.IsDir()
}

func (dshAgent) HooksInstalled() bool {
	data, err := os.ReadFile(dshPluginPath())
	if err != nil {
		return false
	}
	content := string(data)
	if !ownsDSHPlugin(content) ||
		!strings.Contains(content, "// fingerprint: "+dshTemplateFingerprint()) {
		return false
	}
	// 旧 exe 路径视为过期（与 zcodeAgent 同款，以解析后的当前可执行文件为基准）
	exe, err := currentCLIExe()
	if err != nil {
		return false
	}
	if content != renderDSHPlugin(exe) {
		return false
	}
	patch, err := os.ReadFile(dshPatchPath())
	return err == nil && strings.Contains(string(patch), "id: ok-hooks")
}

func (dshAgent) InstallHooks(exe string) error {
	removeLegacyDSHPlugin() // 改名迁移：旧名插件目录随安装清除
	// 插件文件（自有新文件整写；既有文件非自家则先备份）
	path := dshPluginPath()
	if data, err := os.ReadFile(path); err == nil {
		if !ownsDSHPlugin(string(data)) {
			if err := os.WriteFile(path+".bak-openknowledge", data, 0o644); err != nil {
				return fmt.Errorf("备份既有插件失败: %w", err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("读取既有插件失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := fsx.WriteFile(path, []byte(renderDSHPlugin(exe)), 0o644); err != nil {
		return err
	}
	// patch 行（标记块幂等 upsert；# 标记在 YAML 是合法注释；UpsertHooksBlock
	// 的 StripLegacyOKHooks 只认 TOML [[hooks]] 表，对 YAML 是安全 no-op）
	patch := dshPatchPath()
	if data, err := os.ReadFile(patch); err == nil {
		_ = os.WriteFile(patch+".bak-openknowledge", data, 0o644)
	}
	return UpsertHooksBlock(patch, dshPatchBlock())
}

// removeDSHMarkerBlock 从 patch 内容移除 ok 标记块（新旧品牌双认），返回
// (新内容, 是否移除)。
func removeDSHMarkerBlock(content string) (string, bool) {
	removed := false
	for _, pair := range [][2]string{{MarkerBegin, MarkerEnd}, {LegacyMarkerBegin, LegacyMarkerEnd}} {
		begin, end := pair[0], pair[1]
		for {
			i := strings.Index(content, begin)
			j := strings.Index(content, end)
			if i < 0 || j <= i {
				break
			}
			tail := strings.TrimPrefix(content[j+len(end):], "\n")
			head := strings.TrimRight(content[:i], "\n")
			out := head
			if strings.TrimSpace(tail) != "" {
				if out != "" {
					out += "\n"
				}
				out += "\n" + tail
			}
			content = out
			removed = true
		}
	}
	if !removed {
		return content, false
	}
	out := strings.TrimLeft(content, "\n")
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out, true
}

func (dshAgent) RemoveHooks() (bool, error) {
	removed := false
	path := dshPluginPath()
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return false, err
	case ownsDSHPlugin(string(data)):
		if err := os.Remove(path); err != nil {
			return false, fmt.Errorf("删除 dsh 插件: %w", err)
		}
		removed = true
	}
	// 改名迁移：旧名插件目录一并在卸载窗口清除
	if dir := legacyDSHPluginDir(); dir != "" {
		if d, rerr := os.ReadFile(filepath.Join(dir, "index.js")); rerr == nil && ownsDSHPlugin(string(d)) {
			if os.RemoveAll(dir) == nil {
				removed = true
			}
		}
	}
	// patch 行是宿主 cordis.patch.yml 的读-改-写，包在 WithFileLock 内
	//（同 UpsertHooksBlock；插件文件为自家 marker 校验后的整删，无需锁）。
	patch := dshPatchPath()
	err = fsx.WithFileLock(patch, func() error {
		data, err := os.ReadFile(patch)
		switch {
		case os.IsNotExist(err):
			return nil
		case err != nil:
			return err
		default:
			out, ok := removeDSHMarkerBlock(string(data))
			if !ok {
				return nil
			}
			if err := fsx.WriteFile(patch, []byte(out), 0o644); err != nil {
				return fmt.Errorf("移除 patch 行: %w", err)
			}
			removed = true
		}
		return nil
	})
	if err != nil {
		return removed, err
	}
	return removed, nil
}

// EnsureHooks 自愈：仅在曾安装（patch 标记块存在或插件文件为自家，新旧品牌
// 双认）且内容过期时整体重写；从未安装 / 经 RemoveHooks 显式移除（两者均不在）
// 不复活。旧品牌形态命中 ours 后走 InstallHooks：upsert 剥旧 patch 块、插件写
// 新路径、旧插件目录清除，一次完成迁移。
func (dshAgent) EnsureHooks(exe string) error {
	pluginData, pluginErr := os.ReadFile(dshPluginPath())
	patchData, patchErr := os.ReadFile(dshPatchPath())
	legacyPluginData, legacyPluginErr := os.ReadFile(filepath.Join(legacyDSHPluginDir(), "index.js"))
	ours := (pluginErr == nil && ownsDSHPlugin(string(pluginData))) ||
		(legacyPluginErr == nil && ownsDSHPlugin(string(legacyPluginData))) ||
		(patchErr == nil && (strings.Contains(string(patchData), MarkerBegin) ||
			strings.Contains(string(patchData), LegacyMarkerBegin)))
	if !ours {
		return nil
	}
	if pluginErr == nil && string(pluginData) == renderDSHPlugin(exe) &&
		patchErr == nil && strings.Contains(string(patchData), "id: ok-hooks") &&
		!strings.Contains(string(patchData), LegacyMarkerBegin) && legacyPluginErr != nil {
		return nil
	}
	return dshAgent{}.InstallHooks(exe)
}
