package registry

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// shadow HOME 免疫：HOME/USERPROFILE 被重定向到临时目录时（CodePilot shadow 模式），
// Home() 仍解析真实用户目录。
func TestHomeImmuneToShadowEnv(t *testing.T) {
	realHome, err := os.UserHomeDir() // 重定向前先取基准
	if err != nil {
		t.Skip("无法获取用户目录")
	}
	shadow := t.TempDir()
	t.Setenv("HOME", shadow)
	t.Setenv("USERPROFILE", shadow)
	t.Setenv("OK_HOME", "") // 确保不生效
	got := Home()
	// 2.25.0 改名版后数据根可能是 .okryptos 或（未迁移时）.openknowledge，
	// 本测试只管 shadow 免疫：根必须落在真实用户目录下，由 homeDirForBase 裁决。
	if want := homeDirForBase(realHome); got != want {
		t.Fatalf("Home() = %q, want %q（跟随了 shadow HOME 重定向）", got, want)
	}
}

// 2.25.0 改名版（OpenKnowledge→Okryptos）：数据根按存在性选择——新根优先；
// 仅旧根存在时仍用旧根（等 MigrateLegacyHome 显式迁移，Home 自身绝不做文件系统
// 写操作）；都没有则默认新根。
func TestHomeDirForBase(t *testing.T) {
	base := t.TempDir()
	if got := homeDirForBase(base); got != filepath.Join(base, ".okryptos") {
		t.Fatalf("空 base = %q, want 新根 .okryptos", got)
	}
	legacy := filepath.Join(base, ".openknowledge")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := homeDirForBase(base); got != legacy {
		t.Fatalf("仅旧根存在 = %q, want 旧根（未迁移）", got)
	}
	newDir := filepath.Join(base, ".okryptos")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := homeDirForBase(base); got != newDir {
		t.Fatalf("新旧并存 = %q, want 新根", got)
	}
}

// 迁移主路径：整体改名 + config 内旧根绝对路径修正 + migrated-from 标记。
func TestMigrateLegacyHome(t *testing.T) {
	base := t.TempDir()
	legacy := filepath.Join(base, ".openknowledge")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "registry.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	// models_dir 场景：TOML 基本字符串里反斜杠是转义形式
	escLegacy := strings.ReplaceAll(legacy, `\`, `\\`)
	cfg := "[embedding]\nmodels_dir = \"" + escLegacy + `\models"` + "\n"
	if err := os.WriteFile(filepath.Join(legacy, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := migrateLegacyHome(base); err != nil {
		t.Fatal(err)
	}
	newDir := filepath.Join(base, ".okryptos")
	if _, err := os.Stat(filepath.Join(newDir, "registry.toml")); err != nil {
		t.Fatalf("迁移后 registry.toml 缺失: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("旧根应消失, stat err = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(newDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), ".openknowledge") {
		t.Fatalf("config 旧根路径未修正: %s", data)
	}
	if _, err := os.Stat(filepath.Join(newDir, "migrated-from")); err != nil {
		t.Fatalf("缺 migrated-from 标记: %v", err)
	}
}

// 旧 daemon 存活时不迁（健康探测 200 → 跳过），下次启动重试。
func TestMigrateLegacyHomeSkipsWhenDaemonAlive(t *testing.T) {
	base := t.TempDir()
	legacy := filepath.Join(base, ".openknowledge")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.TrimPrefix(srv.URL, "http://127.0.0.1:"))
	dj := fmt.Sprintf(`{"pid":1,"port":%d,"token":"t"}`, port)
	if err := os.WriteFile(filepath.Join(legacy, "daemon.json"), []byte(dj), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyHome(base); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("daemon 存活时不应迁移: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, ".okryptos")); !os.IsNotExist(err) {
		t.Fatalf("daemon 存活时不应出现新根")
	}
}

// 幂等：无旧根（已迁移/全新安装/OK_HOME 场景）直接 nil，不创建任何目录。
func TestMigrateLegacyHomeNoLegacy(t *testing.T) {
	base := t.TempDir()
	if err := migrateLegacyHome(base); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, ".okryptos")); !os.IsNotExist(err) {
		t.Fatalf("无旧根时不应创建新根")
	}
}


// OK_HOME 覆盖仍第一优先（全仓测试隔离依赖）。
func TestHomeOKHomeOverrideStillWins(t *testing.T) {
	okHome := filepath.Join(t.TempDir(), "okhome")
	t.Setenv("OK_HOME", okHome)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	if got := Home(); got != okHome {
		t.Fatalf("Home() = %q, want OK_HOME %q", got, okHome)
	}
}

// L-25：双重失败兜底须为进程内一致的绝对路径——裸相对 ".openknowledge"
// 会让数据根随 cwd 漂移，且两次调用可能解析到不同目录。
func TestHomeFallbackStableAndAbsolute(t *testing.T) {
	first := fallbackHome()
	if !filepath.IsAbs(first) {
		t.Fatalf("fallback 应为绝对路径，got %q", first)
	}
	other := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(other); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if got := fallbackHome(); got != first {
		t.Fatalf("fallback 随 cwd 漂移: %q → %q", first, got)
	}
}
