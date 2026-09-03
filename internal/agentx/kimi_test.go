package agentx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertHooksBlockAppendAndReplace(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("default_model = \"kimi\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertHooksBlock(cfg, "BLOCK_V1\n"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	got := string(data)
	if !strings.Contains(got, "default_model") || !strings.Contains(got, "BLOCK_V1") {
		t.Fatalf("append failed: %q", got)
	}
	if err := UpsertHooksBlock(cfg, "BLOCK_V2\n"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(cfg)
	got = string(data)
	if strings.Contains(got, "BLOCK_V1") || !strings.Contains(got, "BLOCK_V2") {
		t.Fatalf("replace failed: %q", got)
	}
	if strings.Count(got, MarkerBegin) != 1 {
		t.Fatalf("duplicate marker block: %q", got)
	}
}

func TestUpsertHooksBlockStripsLegacyOKHooks(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	legacy := `default_model = "kimi"

[[hooks]]
event = "UserPromptSubmit"
command = "D:/old/ok.exe hook prompt"
timeout = 10

[[hooks]]
event = "PostToolUse"
matcher = "Write|Edit"
command = "D:/old/ok.exe hook post-tool"
timeout = 5

[[hooks]]
event = "Stop"
command = "ok hook stop"
timeout = 5

[[hooks]]
event = "SessionStart"
command = "other-tool run"
timeout = 3

[providers]
`
	if err := os.WriteFile(cfg, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertHooksBlock(cfg, HooksBlockFor(`D:\new\ok.exe`, 10)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	got := string(data)
	if strings.Contains(got, "D:/old/ok.exe") || strings.Contains(got, `"ok hook stop"`) {
		t.Fatalf("legacy ok hooks should be stripped: %q", got)
	}
	if !strings.Contains(got, "other-tool run") || !strings.Contains(got, "default_model") || !strings.Contains(got, "[providers]") {
		t.Fatalf("user content should be preserved: %q", got)
	}
	if c := strings.Count(got, MarkerBegin); c != 1 {
		t.Fatalf("expected exactly one marker block, got %d: %q", c, got)
	}
	if c := strings.Count(got, "[[hooks]]"); c != 4 {
		t.Fatalf("expected 3 new + 1 foreign hook tables, got %d: %q", c, got)
	}
	if !strings.Contains(got, `\"`+filepath.ToSlash(`D:\new\ok.exe`)+`\" hook prompt`) {
		t.Fatalf("new exe path should overwrite the old one: %q", got)
	}
}

func TestUpsertHooksBlockNewFile(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := UpsertHooksBlock(cfg, "BLOCK\n"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	if !strings.Contains(string(data), "BLOCK") {
		t.Fatalf("unexpected %q", data)
	}
}

func TestUpsertHooksBlockCorruptMarker(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte(MarkerBegin+"\nno end"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertHooksBlock(cfg, "X\n"); err == nil {
		t.Fatal("expected corrupt marker error")
	}
}

func TestUpsertHooksBlockReplacesMarkerInPlace(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	initial := "default_model = \"kimi\"\n\n" + MarkerBegin + "\n" + HooksBlockFor("D:/old/ok.exe", 10) + MarkerEnd + "\n\n[providers]\n"
	if err := os.WriteFile(cfg, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertHooksBlock(cfg, HooksBlockFor("D:/new/ok.exe", 10)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	got := string(data)
	if c := strings.Count(got, MarkerBegin); c != 1 {
		t.Fatalf("expected exactly one marker block, got %d: %q", c, got)
	}
	if !strings.Contains(got, "D:/new/ok.exe") || strings.Contains(got, "D:/old/ok.exe") {
		t.Fatalf("exe path not replaced: %q", got)
	}
	if strings.Index(got, MarkerBegin) > strings.Index(got, "[providers]") {
		t.Fatalf("marker block should stay before [providers] (in-place replace): %q", got)
	}
	if !strings.Contains(got, `default_model = "kimi"`) {
		t.Fatalf("default_model lost: %q", got)
	}
}

func TestUpsertHooksBlockCorruptMarkerWithOKCommands(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	initial := MarkerBegin + "\n" + HooksBlockFor("D:/x/ok.exe", 10)
	if err := os.WriteFile(cfg, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertHooksBlock(cfg, HooksBlockFor("D:/new/ok.exe", 10)); err == nil {
		t.Fatal("expected corrupt marker error")
	}
	data, _ := os.ReadFile(cfg)
	if string(data) != initial {
		t.Fatalf("file should be unmodified on error: %q", data)
	}
}

func TestEnsureHooksBlock(t *testing.T) {
	t.Run("markers present: untouched", func(t *testing.T) {
		cfg := filepath.Join(t.TempDir(), "config.toml")
		initial := "default_model = \"kimi\"\n\n" + MarkerBegin + "\n" + HooksBlockFor("D:/old/ok.exe", 10) + MarkerEnd + "\n"
		if err := os.WriteFile(cfg, []byte(initial), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := EnsureHooksBlock(cfg, "D:/new/ok.exe"); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(cfg)
		if string(data) != initial {
			t.Fatalf("file should be untouched when markers exist: %q", data)
		}
		if _, err := os.Stat(cfg + ".bak-openknowledge"); !os.IsNotExist(err) {
			t.Fatal("no backup should be written when untouched")
		}
	})

	t.Run("markers stripped by kimi-code: self-heal", func(t *testing.T) {
		cfg := filepath.Join(t.TempDir(), "config.toml")
		// kimi-code 删掉标记注释后剩下的孤儿 ok hook 表 + 其它工具的 hook
		orphan := "default_model = \"kimi\"\n\n" + HooksBlockFor("D:/old/ok.exe", 10) + `
[[hooks]]
event = "SessionStart"
command = "other-tool run"
timeout = 3
`
		if err := os.WriteFile(cfg, []byte(orphan), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := EnsureHooksBlock(cfg, "D:/new/ok.exe"); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(cfg)
		got := string(data)
		if c := strings.Count(got, MarkerBegin); c != 1 {
			t.Fatalf("expected exactly one marker block after heal, got %d: %q", c, got)
		}
		if c := strings.Count(got, "[[hooks]]"); c != 4 {
			t.Fatalf("expected 3 new + 1 foreign hook tables, got %d: %q", c, got)
		}
		if !strings.Contains(got, `\"D:/new/ok.exe\" hook prompt`) || strings.Contains(got, "D:/old/ok.exe") {
			t.Fatalf("orphan ok hooks should be replaced by new block: %q", got)
		}
		if !strings.Contains(got, "other-tool run") || !strings.Contains(got, `default_model = "kimi"`) {
			t.Fatalf("user content should be preserved: %q", got)
		}
		bak, err := os.ReadFile(cfg + ".bak-openknowledge")
		if err != nil || string(bak) != orphan {
			t.Fatalf("backup should hold the pre-heal content: %v %q", err, bak)
		}
	})

	t.Run("config missing: error for fail-open caller", func(t *testing.T) {
		cfg := filepath.Join(t.TempDir(), "config.toml")
		if err := EnsureHooksBlock(cfg, "D:/new/ok.exe"); err == nil {
			t.Fatal("expected error for missing config")
		}
	})

	t.Run("no marker and no ok hooks: no-op (explicit uninstall not revived)", func(t *testing.T) {
		cfg := filepath.Join(t.TempDir(), "config.toml")
		// 用户显式移除 kimi hooks（保留其它工具的 hook 表）后的形态
		initial := "default_model = \"kimi\"\n\n[[hooks]]\nevent = \"SessionStart\"\ncommand = \"other-tool run\"\ntimeout = 3\n"
		if err := os.WriteFile(cfg, []byte(initial), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := EnsureHooksBlock(cfg, "D:/new/ok.exe"); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(cfg)
		if string(data) != initial {
			t.Fatalf("file should be untouched when no ok hooks remain: %q", data)
		}
		if _, err := os.Stat(cfg + ".bak-openknowledge"); !os.IsNotExist(err) {
			t.Fatal("no backup should be written on no-op")
		}
	})
}

func TestUpsertHooksBlockStripsDuplicateMarkerBlocks(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	// 历史 bug 残留的双标记块：旧块不剥离会与新块并存，hook 双派发
	initial := "default_model = \"kimi\"\n\n" +
		MarkerBegin + "\n" + HooksBlockFor("D:/old/ok.exe", 10) + MarkerEnd + "\n\n" +
		MarkerBegin + "\n" + HooksBlockFor("D:/older/ok.exe", 5) + MarkerEnd + "\n\n[providers]\n"
	if err := os.WriteFile(cfg, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertHooksBlock(cfg, HooksBlockFor("D:/new/ok.exe", 10)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	got := string(data)
	if c := strings.Count(got, MarkerBegin); c != 1 {
		t.Fatalf("expected exactly one marker block, got %d: %q", c, got)
	}
	if strings.Contains(got, "D:/old/ok.exe") || strings.Contains(got, "D:/older/ok.exe") {
		t.Fatalf("stale duplicate blocks should be stripped: %q", got)
	}
	if !strings.Contains(got, "D:/new/ok.exe") || !strings.Contains(got, "[providers]") {
		t.Fatalf("new block / user content lost: %q", got)
	}
	// 原位替换语义不变：新块仍在第一个旧块的位置（[providers] 之前）
	if strings.Index(got, MarkerBegin) > strings.Index(got, "[providers]") {
		t.Fatalf("marker block should stay before [providers]: %q", got)
	}
}

func TestKimiAgentInstallDetectRemove(t *testing.T) {
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	a, ok := Find("kimi")
	if !ok {
		t.Fatal("kimi agent not registered")
	}
	if !a.Detect() {
		t.Fatal("Detect should be true when KIMI_CODE_HOME dir exists")
	}
	if a.HooksInstalled() {
		t.Fatal("HooksInstalled should be false before install")
	}
	if err := a.InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Fatal("HooksInstalled should be true after install")
	}
	// exe 迁移/改名后 hook 指向旧路径：应判为未安装，doctor/自愈据此触发重装
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if a.HooksInstalled() {
		t.Fatal("HooksInstalled should be false when hooks point at a stale exe path")
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	if a.HooksInstalled() {
		t.Fatal("HooksInstalled should be false after remove")
	}
}

func TestKimiAgentDetectFalse(t *testing.T) {
	t.Setenv("KIMI_CODE_HOME", filepath.Join(t.TempDir(), "nonexistent"))
	if (kimiAgent{}).Detect() {
		t.Fatal("Detect should be false when dir missing")
	}
}

// TestOKHookCommandRegex 回归 L-06：okHookCommand 只匹配 ok 生成的完整命令形态
// （ok/ok.exe + " hook " + 三个 ok 子命令之一 + 值结束），用户自装同名 ok 工具的
// 其它命令行不命中。
func TestOKHookCommandRegex(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		// ok 生成/历史遗留形态——必须命中（识别以去重/清理）。
		{"quoted exe prompt", `command = "\"D:/x/ok.exe\" hook prompt"`, true},
		{"quoted exe post-tool", `command = "\"D:/x/ok.exe\" hook post-tool"`, true},
		{"quoted exe stop", `command = "\"D:/x/ok.exe\" hook stop"`, true},
		{"bare ok legacy", `command = "ok hook prompt"`, true},
		{"leading spaces", `  command = "ok hook stop"`, true},
		// gui-split 注册 bug 的存量形态（okd.exe hook ...，2026-08-22 实证）——
		// 必须命中以被清理/迁移；okd 是 daemon 二进制，hook 注册的合法入口只有 ok。
		{"quoted okd exe prompt", `command = "\"D:/x/okd.exe\" hook prompt"`, true},
		{"quoted okd exe post-tool", `command = "\"D:/x/okd.exe\" hook post-tool"`, true},
		{"quoted okd exe stop", `command = "\"D:/x/okd.exe\" hook stop"`, true},
		{"bare okd legacy", `command = "okd hook prompt"`, true},
		// 用户自装同名 ok 工具的其它命令行——不得命中误删。
		{"user ok deploy", `command = "ok deploy"`, false},
		{"user ok hook run", `command = "ok hook run"`, false},
		{"user ok hook with args", `command = "ok hook prompt --verbose"`, false},
		{"user ok bare hook", `command = "ok hook"`, false},
		{"user okd deploy", `command = "okd deploy"`, false},
		{"myok 词边界", `command = "myok hook prompt"`, false},
		{"myokd 词边界", `command = "myokd hook prompt"`, false},
		{"third-party", `command = "echo hi"`, false},
		{"非 command 行", `event = "ok hook prompt"`, false},
	}
	for _, c := range cases {
		if got := okHookCommand.MatchString(c.line); got != c.want {
			t.Errorf("%s: okHookCommand.MatchString(%q) = %v, want %v", c.name, c.line, got, c.want)
		}
	}
}

// TestStripLegacyOKHooksKeepsUserOKTool 回归 L-06：StripLegacyOKHooks 清 ok 遗留块时，
// 用户自装同名 ok 工具的 [[hooks]] 表（非 ok 子命令形态）必须原样保留。
func TestStripLegacyOKHooksKeepsUserOKTool(t *testing.T) {
	content := `default_model = "kimi"

[[hooks]]
event = "UserPromptSubmit"
command = "ok hook prompt"
timeout = 10

[[hooks]]
event = "UserPromptSubmit"
command = "ok deploy --prod"
timeout = 5

[[hooks]]
event = "Stop"
command = "echo done"
`
	got := StripLegacyOKHooks(content)
	if strings.Contains(got, "ok hook prompt") {
		t.Errorf("ok 遗留块未剥离:\n%s", got)
	}
	if !strings.Contains(got, `command = "ok deploy --prod"`) {
		t.Errorf("用户自装 ok 工具的 hooks 表被误删:\n%s", got)
	}
	if !strings.Contains(got, `command = "echo done"`) {
		t.Errorf("第三方 hooks 表被误删:\n%s", got)
	}
}

// TestEnsureHooksBlockMigratesLegacyMarkers（2.25.0 改名迁移）：旧品牌标记块
//（# >>> openknowledge hooks >>>）在 Ensure 窗口被剥除并原位换成新标记块，
// 不留孤儿旧块（否则双派发）；卸载 RemoveHooks 也认旧块。
func TestEnsureHooksBlockMigratesLegacyMarkers(t *testing.T) {
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	cfg := kimiConfigPath()
	legacy := "# 其它配置\n\n" + LegacyMarkerBegin + "\n" + HooksBlockFor(`D:\old\ok.exe`, 10) + LegacyMarkerEnd + "\n"
	if err := os.WriteFile(cfg, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureHooksBlock(cfg, `D:\new\ok.exe`); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, LegacyMarkerBegin) || strings.Contains(s, "openknowledge hooks") {
		t.Fatalf("旧标记块应被剥除: %q", s)
	}
	if !strings.Contains(s, MarkerBegin) || !strings.Contains(s, filepath.ToSlash(`D:\new\ok.exe`)) {
		t.Fatalf("新标记块应就位且烘焙当前 exe: %q", s)
	}
	if !strings.Contains(s, "# 其它配置") {
		t.Fatal("块外内容不应受损")
	}
}
