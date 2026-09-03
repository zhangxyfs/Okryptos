package agentx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiAgentInstallDetectRemove(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	a, ok := Find("pi")
	if !ok {
		t.Fatal("pi agent not registered")
	}
	if !a.Detect() {
		t.Fatal("Detect should be true when PI_CODING_AGENT_DIR exists")
	}
	if a.HooksInstalled() {
		t.Fatal("HooksInstalled should be false before install")
	}
	exe := currentExe(t)
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(PiHome(), "extensions", "okryptos.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("extension not written: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "// fingerprint: ") || !strings.Contains(content, filepath.ToSlash(exe)) {
		t.Fatalf("bad extension content: %.200s", content)
	}
	if strings.Contains(content, "{{EXE}}") {
		t.Fatal("unrendered placeholder remains")
	}
	if !a.HooksInstalled() {
		t.Fatal("HooksInstalled should be true after install")
	}
	// exe 迁移/改名后扩展烘焙的是旧路径：应判为未安装/过期
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if a.HooksInstalled() {
		t.Fatal("HooksInstalled should be false when extension bakes a stale exe path")
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("extension file should be removed")
	}
}

func TestPiAgentForeignFilePreserved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	extDir := filepath.Join(dir, "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(extDir, "okryptos.ts")
	if err := os.WriteFile(path, []byte("// user hand-written extension\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := piAgent{}
	if a.HooksInstalled() {
		t.Fatal("foreign file should not count as installed")
	}
	if err := a.InstallHooks(`D:\x\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak-openknowledge"); err != nil {
		t.Fatal("foreign file should be backed up before overwrite")
	}
	// 新文件是本工具生成后可删除；但手工文件恢复后不删
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	if err := os.WriteFile(path, []byte("// user hand-written extension\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err = a.RemoveHooks()
	if err != nil || removed {
		t.Fatalf("foreign file must not be removed: %v, %v", removed, err)
	}
}

func TestPiAgentEnsureHooksStaleRewrite(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	a := piAgent{}
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(`D:\new\ok.exe`); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(a.HooksTarget())
	if !strings.Contains(string(data), filepath.ToSlash(`D:\new\ok.exe`)) {
		t.Fatal("EnsureHooks should rewrite stale extension with new exe path")
	}
	// 文件不存在时 EnsureHooks 为 no-op（pi 不会触发 hook）
	if err := os.Remove(a.HooksTarget()); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(`D:\new\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.HooksTarget()); !os.IsNotExist(err) {
		t.Fatal("EnsureHooks must not recreate a deleted extension")
	}
}

// TestPiAgentLegacyMigration（2.25.0 改名迁移）：旧名扩展 openknowledge.ts 在
// EnsureHooks 窗口迁移为 okryptos.ts（按当前 exe 重渲染）并删除旧件——
// legacy 在 = 旧版接入过 hooks，"缺失不复活"在此让位，否则升级后 hooks 被静默摘死。
// 外来旧名文件（无归属标记）不动；全新安装（无旧件）不复活。
func TestPiAgentLegacyMigration(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	a := piAgent{}
	legacyPath := legacyPiExtensionPath()
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := legacyPiExtensionMarker + "\n// fingerprint: 000000000000\n// exe: D:/old/ok.exe\n"
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(`D:\new\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatal("旧名扩展应被迁移清除")
	}
	data, err := os.ReadFile(a.HooksTarget())
	if err != nil {
		t.Fatalf("新名扩展应被迁移创建: %v", err)
	}
	if !strings.Contains(string(data), filepath.ToSlash(`D:\new\ok.exe`)) {
		t.Fatal("迁移创建的新扩展应烘焙当前 exe")
	}
	// 外来旧名文件不动
	if err := os.WriteFile(legacyPath, []byte("// user hand-written\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(`D:\new\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if d, _ := os.ReadFile(legacyPath); string(d) != "// user hand-written\n" {
		t.Fatal("外来旧名文件不应被删除/改写")
	}
}
