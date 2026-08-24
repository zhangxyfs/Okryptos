package agentx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"openknowledge/internal/fsx"
)

// settings_engine.go：claude/codex/qoder/qoderide/zcode 五适配器同形部分的共享
// 引擎——事件表参数化（hookEvent）+ 命令生成器注入（M-03）。历史上 Qoder cmd /s、
// Codex 信任门等坑被迫各修三份，抽取后共用行为一处维护。各适配器特有逻辑
// （codex 信任门与 TOML 行级手术、qoder hooksConfig 开关、zcode process 形态与
// hooks.events 嵌套）留在原文件。

// hookEvent 一条 hook 事件参数。
type hookEvent struct {
	event   string // 宿主事件名
	matcher string // 组级 matcher
	okHook  string // ok hook 子命令
}

// quotedShellCommand 生成 quoted shell 命令串：正斜杠 exe + 双引号（cmd.exe 与
// bash 均可执行——探针实测 cmd 接受正斜杠路径）。输出协议 Claude JSON
// （args 末尾 "claude"）。
func quotedShellCommand(exe, okHook string) string {
	return strconv.Quote(filepath.ToSlash(exe)) + " hook " + okHook + " claude"
}

// loadSettingsJSON 读 JSON 配置；文件不存在返回空对象，解析失败报错（不覆盖损坏
// 文件）。desc 为错误信息中的文件描述（如 "claude settings.json"）。map 合并写
// 会重排 key 顺序——未知字段内容保留，代价可接受。
func loadSettingsJSON(path, desc string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg := map[string]any{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%s 解析失败: %w", desc, err)
	}
	return cfg, nil
}

// writeSettingsJSON 备份后写回 JSON 配置（MarshalIndent，未知字段保留）。
func writeSettingsJSON(path string, cfg map[string]any) error {
	if data, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak-openknowledge", data, 0o644)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsx.WriteFile(path, append(data, '\n'), 0o644)
}

// hookEventsOf 取顶层 hooks 事件表（只读视图），不做任何创建。
func hookEventsOf(cfg map[string]any) map[string]any {
	events, _ := cfg["hooks"].(map[string]any)
	return events
}

// hookEventsEdit 取顶层 hooks 事件表供写入：缺失时创建。
func hookEventsEdit(cfg map[string]any) map[string]any {
	events, _ := cfg["hooks"].(map[string]any)
	if events == nil {
		events = map[string]any{}
		cfg["hooks"] = events
	}
	return events
}

// stripOKHooks 移除事件表里所有 ok 自有 hook（isOK 判定；组内 hooks 被删空时整组
// 移除，事件数组空了删事件键），返回是否有改动。第三方条目原样保留。
func stripOKHooks(events map[string]any, isOK func(map[string]any) bool) bool {
	changed := false
	for name, v := range events {
		groups, _ := v.([]any)
		if groups == nil {
			continue
		}
		kept := groups[:0]
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hooks, _ := gm["hooks"].([]any)
			if gm == nil || hooks == nil {
				kept = append(kept, g)
				continue
			}
			var keptHooks []any
			for _, h := range hooks {
				if hm, _ := h.(map[string]any); hm != nil && isOK(hm) {
					changed = true
					continue
				}
				keptHooks = append(keptHooks, h)
			}
			if len(keptHooks) == 0 {
				changed = true // 整组都是 ok 的，连组移除
				continue
			}
			gm["hooks"] = keptHooks
			kept = append(kept, g)
		}
		if len(kept) == 0 {
			delete(events, name)
		} else {
			events[name] = kept
		}
	}
	return changed
}

// containsOKHook 报告事件表里是否存在任何 ok 自有 hook。
func containsOKHook(events map[string]any, isOK func(map[string]any) bool) bool {
	for _, v := range events {
		groups, _ := v.([]any)
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hooks, _ := gm["hooks"].([]any)
			for _, h := range hooks {
				if hm, _ := h.(map[string]any); hm != nil && isOK(hm) {
					return true
				}
			}
		}
	}
	return false
}

// isOKShellCommand 判定 type=command shell 串条目是否 ok 生成：quoted 后缀
// " hook <okHook> claude" 各平台都认；wrapperSuffix 时 Windows 另认包装文件裸
// 路径（trim 后以 / 或 \ 分隔的 ok-hook-<okHook>.cmd 结尾，大小写不敏感，
// Equalfold 语意——cmd.exe 路径语义；判定落空后继续旧 quoted 后缀判定，迁移清理
// 用）。不看 exe basename——改名/迁移/测试二进制都不影响识别。
func isOKShellCommand(h map[string]any, events []hookEvent, wrapperSuffix bool) bool {
	typ, _ := h["type"].(string)
	cmd, _ := h["command"].(string)
	if typ != "command" || cmd == "" {
		return false
	}
	cmd = strings.TrimSpace(cmd)
	for _, e := range events {
		if wrapperSuffix && runtime.GOOS == "windows" {
			for _, sep := range []string{"/", "\\"} {
				if hasSuffixFold(cmd, sep+"ok-hook-"+e.okHook+".cmd") {
					return true
				}
			}
		}
		if strings.HasSuffix(cmd, " hook "+e.okHook+" claude") {
			return true
		}
	}
	return false
}

// hasSuffixFold 报告 s 是否以 suffix 结尾（大小写不敏感，Equalfold 语意）。
func hasSuffixFold(s, suffix string) bool {
	return len(s) >= len(suffix) && strings.EqualFold(s[len(s)-len(suffix):], suffix)
}

// shellOKGroup 生成一个事件的 ok hook 组：type=command shell 串，
// timeout 秒级 = 全局 HookTimeoutSec()。
func shellOKGroup(command, matcher string) map[string]any {
	hook := map[string]any{
		"type":    "command",
		"command": command,
		"timeout": HookTimeoutSec(),
	}
	return map[string]any{"matcher": matcher, "hooks": []any{hook}}
}

// appendShellOKGroups 向事件表各事件追加 ok hook 组（安装语义"先剥离再追加"⇒
// ok 组恒为最后一组，第三方组存在时索引顺移）。
func appendShellOKGroups(events map[string]any, specs []hookEvent, exe string, command func(exe, okHook string) string) {
	for _, e := range specs {
		groups, _ := events[e.event].([]any)
		events[e.event] = append(groups, shellOKGroup(command(exe, e.okHook), e.matcher))
	}
}

// shellHooksCurrent 报告事件表各事件的 ok shell hook 是否均为当前期望形态
// （command=command(exe, okHook)、matcher 与 timeout 正确）；wrapperDir 非空时
// Windows 另要求包装文件内容为当前 exe（exe 迁移后包装过期 = 集成失效，需自愈
// 重写）。
func shellHooksCurrent(events map[string]any, specs []hookEvent, exe string, isOK func(map[string]any) bool, command func(exe, okHook string) string, wrapperDir string) bool {
	wantTimeout := float64(HookTimeoutSec())
	for _, e := range specs {
		if wrapperDir != "" && runtime.GOOS == "windows" {
			data, err := os.ReadFile(hookWrapperPath(wrapperDir, e.okHook))
			if err != nil || string(data) != hookWrapperContent(exe, e.okHook) {
				return false
			}
		}
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
				if hm == nil || !isOK(hm) {
					continue
				}
				cmd, _ := hm["command"].(string)
				timeout, _ := hm["timeout"].(float64)
				if cmd == command(exe, e.okHook) && timeout == wantTimeout {
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
