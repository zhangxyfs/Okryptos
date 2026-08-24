package agentx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// settings_engine_test.go：共享引擎（settings_engine.go / wrappers.go）的表驱动
// 测试——四家 shell 形态适配器（claude/codex/qoder/qoderide）的共用行为在此一处
// 覆盖；各适配器文件内的既有测试继续覆盖其特有逻辑（codex 信任门、qoder
// hooksConfig、zcode process 形态等）。

var shellAdapters = []struct {
	name    string
	events  []hookEvent
	isOK    func(map[string]any) bool
	command func(exe, okHook string) string
}{
	{"claude", claudeHookEvents, isOKClaudeHook, claudeCommand},
	{"codex", codexEventSpecs(), isOKCodexHook, codexCommand},
	{"qoder", qoderHookEvents, isOKQoderHook, qoderCommand},
	{"qoder-ide", lingmaHookEvents, isOKQoderIdeHook, lingmaCommand},
}

// TestShellEngineStripAppendRoundtrip 追加-剥离往返：ok 组被剥净（空事件键删除），
// 第三方组与非 map 垃圾条目原样保留，二次剥离无改动。
func TestShellEngineStripAppendRoundtrip(t *testing.T) {
	for _, a := range shellAdapters {
		t.Run(a.name, func(t *testing.T) {
			exe := filepath.Join(t.TempDir(), "ok.exe")
			events := map[string]any{}
			appendShellOKGroups(events, a.events, exe, a.command)
			if !containsOKHook(events, a.isOK) {
				t.Fatal("追加后应存在 ok hook")
			}
			third := map[string]any{"matcher": "*", "hooks": []any{
				map[string]any{"type": "command", "command": "echo hi"},
			}}
			for _, e := range a.events {
				groups, _ := events[e.event].([]any)
				events[e.event] = append([]any{third, "junk"}, groups...)
			}
			events["Other"] = "not-a-group"
			if !stripOKHooks(events, a.isOK) {
				t.Fatal("应报告改动")
			}
			if containsOKHook(events, a.isOK) {
				t.Fatal("剥离后不应残留 ok hook")
			}
			for name, v := range events {
				groups, ok := v.([]any)
				if !ok {
					continue // 非数组条目（Other）原样保留
				}
				if len(groups) != 2 {
					t.Fatalf("事件 %s 的第三方组与垃圾条目应原样保留，余 %d 组", name, len(groups))
				}
			}
			if stripOKHooks(events, a.isOK) {
				t.Fatal("二次剥离应无改动")
			}
		})
	}
}

// jsonRoundTrip 经 JSON 往返把内存形态转换为落盘读回形态（数字归一 float64，
// 与 loadSettingsJSON 读回一致）。
func jsonRoundTrip(t *testing.T, events map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestShellEngineHooksCurrent 新安装形态判 current；timeout 被篡改后判过期。
// wrapperDir 传 ""（跳过 Windows 包装校验）——包装校验由 TestEnsureRemoveHookWrappers
// 与各适配器既有测试覆盖。
func TestShellEngineHooksCurrent(t *testing.T) {
	for _, a := range shellAdapters {
		t.Run(a.name, func(t *testing.T) {
			exe := filepath.Join(t.TempDir(), "ok.exe")
			events := map[string]any{}
			appendShellOKGroups(events, a.events, exe, a.command)
			events = jsonRoundTrip(t, events)
			if !shellHooksCurrent(events, a.events, exe, a.isOK, a.command, "") {
				t.Fatal("新安装形态应判 current")
			}
			for _, e := range a.events {
				groups, _ := events[e.event].([]any)
				gm, _ := groups[0].(map[string]any)
				hooks, _ := gm["hooks"].([]any)
				hm, _ := hooks[0].(map[string]any)
				hm["timeout"] = float64(HookTimeoutSec() + 1)
			}
			if shellHooksCurrent(events, a.events, exe, a.isOK, a.command, "") {
				t.Fatal("timeout 被改后应判过期")
			}
		})
	}
}

// TestZcodeStripUsesSharedEngine zcode 的 strip/has 同样走共享引擎（process 形态
// 组生成留在 zcode.go）。
func TestZcodeStripUsesSharedEngine(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "ok.exe")
	events := map[string]any{}
	for _, e := range zcodeHookEvents {
		groups, _ := events[e.event].([]any)
		events[e.event] = append(groups, zcodeOKGroup(exe, e.matcher, e.okHook))
	}
	if !containsOKHook(events, isOKZcodeHook) {
		t.Fatal("追加后应存在 ok hook")
	}
	if !stripOKHooks(events, isOKZcodeHook) {
		t.Fatal("应报告改动")
	}
	if len(events) != 0 {
		t.Fatalf("纯 ok 事件表剥离后应为空: %v", events)
	}
}

// TestLoadWriteSettingsJSON 缺失返回空对象；二次写入备份原文；损坏文件报错且不覆盖。
func TestLoadWriteSettingsJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	cfg, err := loadSettingsJSON(path, "test settings.json")
	if err != nil || len(cfg) != 0 {
		t.Fatalf("缺失文件应返回空对象: %v %v", cfg, err)
	}
	cfg["hooks"] = map[string]any{"x": 1.0}
	if err := writeSettingsJSON(path, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak-openknowledge"); !os.IsNotExist(err) {
		t.Fatal("首次写入无原文，不应产生备份")
	}
	cfg["extra"] = "keep"
	if err := writeSettingsJSON(path, cfg); err != nil {
		t.Fatal(err)
	}
	if bak, err := os.ReadFile(path + ".bak-openknowledge"); err != nil || len(bak) == 0 {
		t.Fatal("二次写入应备份原文")
	}
	got, err := loadSettingsJSON(path, "test settings.json")
	if err != nil || got["extra"] != "keep" {
		t.Fatalf("往返后内容不符: %v %v", got, err)
	}
	if err := os.WriteFile(path, []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSettingsJSON(path, "test settings.json"); err == nil {
		t.Fatal("损坏文件应报错")
	}
	if data, _ := os.ReadFile(path); string(data) != "{bad" {
		t.Fatal("解析失败不应覆盖原文件")
	}
}

// TestHookEventsOfEdit 只读视图不创建；编辑视图缺失时创建并对只读视图可见。
func TestHookEventsOfEdit(t *testing.T) {
	cfg := map[string]any{}
	if hookEventsOf(cfg) != nil {
		t.Fatal("hooks 缺失时只读视图应为 nil")
	}
	hookEventsEdit(cfg)["X"] = []any{}
	if hookEventsOf(cfg) == nil {
		t.Fatal("编辑视图创建后只读视图应可见")
	}
}

// TestEnsureRemoveHookWrappers 包装文件按事件表生成、内容为当前 exe；删除只动内容
// 确为 ok 生成的文件，用户同名文件保留。
func TestEnsureRemoveHookWrappers(t *testing.T) {
	home := t.TempDir()
	events := []hookEvent{{"UserPromptSubmit", "*", "prompt"}, {"Stop", "*", "stop"}}
	exe := filepath.Join(home, "bin", "ok.exe")
	if err := ensureHookWrappers(home, "写入 test hook 包装", events, exe); err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		data, err := os.ReadFile(hookWrapperPath(home, e.okHook))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != hookWrapperContent(exe, e.okHook) {
			t.Fatalf("包装内容不符: %q", data)
		}
	}
	// 用户同名文件（内容非 ok 生成）不得被删。
	foreign := hookWrapperPath(home, "prompt")
	if err := os.WriteFile(foreign, []byte("@echo user file\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !removeHookWrappers(home, events) {
		t.Fatal("应报告有删除")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("内容非 ok 生成的同名文件不得误删")
	}
	if _, err := os.Stat(hookWrapperPath(home, "stop")); !os.IsNotExist(err) {
		t.Fatal("ok 生成的包装应被删除")
	}
	if removeHookWrappers(home, events) {
		t.Fatal("二次删除应无改动")
	}
}
