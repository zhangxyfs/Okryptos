package agentx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"okryptos/internal/fsx"
)

// wrappers.go：Windows .cmd 包装文件三件套的共享实现（M-03）——codex/qoder/
// qoderide 三适配器逐字复制、仅目录与错误前缀不同，此处参数化。
//
// 背景：Windows 上宿主执行 command 型 hook 时整行外套引号（Codex cmd.exe /C
// "<整行>"，上游 issue #38168；Qoder cmd /d /s /c 剥首尾引号，同源问题），含
// 内嵌引号的 quoted 命令串静默不执行——故 command 用 .cmd 包装文件绝对路径裸串
// （无引号、反斜杠形态），与 exe 位置解耦：exe 迁移只改包装文件内容，宿主配置
// 不变，Codex 信任哈希也永不因此过期。
//
// 已知限制：用户名含空格时包装路径带空格，裸串命令仍会被外层引号 bug 截断——
// 上游修复前不额外处理。

// hookWrapperPath 返回 Windows 包装文件路径：<home>/ok-hook-<okHook>.cmd
// （filepath.Join 在 Windows 出反斜杠形态——宿主配置里的 command 裸串即它）。
func hookWrapperPath(home, okHook string) string {
	return filepath.Join(home, "ok-hook-"+okHook+".cmd")
}

// hookWrapperContent 返回包装文件内容：单行 @"<exe>" hook <okHook> claude
// （CRLF 结尾）。exe 路径在 .cmd 文件内部带引号无妨——宿主外壳的剥引号 bug
// 只作用于配置文件里的命令行。
func hookWrapperContent(exe, okHook string) string {
	return "@\"" + exe + "\" hook " + okHook + " claude\r\n"
}

// ensureHookWrappers 确保事件表各事件的包装文件存在且内容为当前 exe（缺失/过期
// 重写，已当前则不写盘）。op 为错误信息前缀（如 "写入 codex hook 包装"）。
// 仅 Windows 调用。
func ensureHookWrappers(home, op string, events []hookEvent, exe string) error {
	for _, e := range events {
		path := hookWrapperPath(home, e.okHook)
		want := hookWrapperContent(exe, e.okHook)
		if data, err := os.ReadFile(path); err == nil && string(data) == want {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
		if err := fsx.WriteFile(path, []byte(want), 0o644); err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
	}
	return nil
}

// removeHookWrappers 删除事件表各事件的包装文件，返回是否有删除。仅删内容确为
// ok 生成的（含 " hook <okHook> claude"）——防误删用户同名文件。仅 Windows 调用。
func removeHookWrappers(home string, events []hookEvent) bool {
	removed := false
	for _, e := range events {
		path := hookWrapperPath(home, e.okHook)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), " hook "+e.okHook+" claude") &&
			os.Remove(path) == nil {
			removed = true
		}
	}
	return removed
}
