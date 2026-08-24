package agentx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestInstallHooksRespectsSettingsLock 回归 H-02：五个适配器对宿主 settings 的
// 读-改-写必须走 fsx.WithFileLock——锁文件被占（模拟另一进程持锁，锁龄低于抢占
// 阈值）时 InstallHooks 不得完成，锁释放后才落盘。未包锁的实现会立即完成而失败。
func TestInstallHooksRespectsSettingsLock(t *testing.T) {
	exe := `D:\develop\OpenKnowledge\dist\ok.exe`
	cases := []struct {
		name    string
		isolate func(t *testing.T) string
		agent   Agent
		target  func() string
	}{
		{"claude", isolateClaude, claudeAgent{}, claudeSettingsPath},
		{"codex", isolateCodex, codexAgent{}, codexHooksPath},
		{"qoder", isolateQoder, qoderAgent{}, qoderSettingsPath},
		{"qoderide", isolateQoderIde, qoderIdeAgent{}, lingmaSettingsPath},
		{"zcode", setupZcode, zcodeAgent{}, zcodeConfigPath},
		// R3 A-01：kimi（config.toml 标记块）、dsh（cordis.patch.yml 标记块）、
		// reasonix（plugin-packages.json 登记）三家的读-改-写同属宿主文件，
		// 与上五家同款必须走 WithFileLock。
		{"kimi", isolateKimiLock, kimiAgent{}, kimiConfigPath},
		{"dsh", isolateDshLock, dshAgent{}, dshPatchPath},
		{"reasonix", isolateReasonixLock, reasonixAgent{}, reasonixStatePath},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.isolate(t)
			target := c.target()
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			lockPath := target + ".lock"
			if err := os.WriteFile(lockPath, []byte("other-process"), 0o644); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- c.agent.InstallHooks(exe) }()
			select {
			case err := <-done:
				t.Fatalf("锁被占用时 InstallHooks 已完成（err=%v）——读-改-写未走 WithFileLock", err)
			case <-time.After(300 * time.Millisecond):
			}
			if err := os.Remove(lockPath); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("InstallHooks: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("锁释放后 InstallHooks 未完成")
			}
		})
	}
}

// TestClaudeHooksConcurrentReadModifyWrite 并发压力：多个 Install/Ensure/Remove 交错
// 跑完均不得报错，settings.json 保持合法 JSON 且预置第三方 hooks 不丢（H-02 场景：
// selfHealHooks 每 prompt 跑 + GUI 安装并发）。
func TestClaudeHooksConcurrentReadModifyWrite(t *testing.T) {
	home := isolateClaude(t)
	sp := filepath.Join(home, "settings.json")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	preset := `{"theme":"light","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"third-party"}]}]}}`
	if err := os.WriteFile(sp, []byte(preset), 0o644); err != nil {
		t.Fatal(err)
	}
	a := claudeAgent{}
	exe := claudeTestExe()
	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 3 {
			case 0:
				errs <- a.InstallHooks(exe)
			case 1:
				errs <- a.EnsureHooks(exe)
			case 2:
				_, err := a.RemoveHooks()
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("并发读-改-写报错: %v", err)
		}
	}
	data, err := os.ReadFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("并发后 settings.json 非法: %v", err)
	}
	if cfg["theme"] != "light" {
		t.Error("第三方字段 theme 丢失")
	}
	events, _ := cfg["hooks"].(map[string]any)
	pre, _ := events["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Error("第三方 PreToolUse 组被丢")
	}
}

// isolateKimiLock kimi 隔离（KIMI_CODE_HOME 官方重定位变量即可隔离）。
func isolateKimiLock(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	return home
}

// isolateDshLock dsh 隔离（OK_DSH_HOME 测试口）。
func isolateDshLock(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_DSH_HOME", home)
	return home
}

// isolateReasonixLock reasonix 隔离（OK_REASONIX_HOME 测试口）。
func isolateReasonixLock(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_REASONIX_HOME", home)
	return home
}
