package setupx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateSkills 隔离技能目录与全部 agent 检测（SkillDirs 遍历 detected agent）。
func isolateSkills(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("OK_SKILLS_HOME", dir)
	t.Setenv("KIMI_CODE_HOME", filepath.Join(t.TempDir(), "nonexistent-kimi"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(t.TempDir(), "nonexistent-pi"))
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OK_QODER_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder"))
	t.Setenv("OK_QODER_IDE_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder-ide"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))
	return dir
}

// TestEnsureSkillsRewritesStaleExe（R3 B-01）：exe 迁移/改名后技能里烘焙的
// 绝对路径过期，EnsureSkills 重写为当前路径；缺失不复活（用户显式删除）、
// 外来内容不动。InstallSkills 幂等重装是对照基线。
func TestEnsureSkillsRewritesStaleExe(t *testing.T) {
	dir := isolateSkills(t)
	if err := InstallSkills(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	// 模拟用户删除一个技能（缺失不复活）与外来文件（内容非本项目技能）
	if err := os.RemoveAll(filepath.Join(dir, "ok-on")); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(dir, "ok-off", "SKILL.md")
	if err := os.WriteFile(foreign, []byte("# 用户自己的笔记\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureSkills(`D:\new\ok.exe`); err != nil {
		t.Fatal(err)
	}

	// 仍在的技能：烘焙路径换新
	data, err := os.ReadFile(filepath.Join(dir, "ok-init", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), filepath.ToSlash(`D:\new\ok.exe`)) ||
		strings.Contains(string(data), filepath.ToSlash(`D:\old\ok.exe`)) {
		t.Fatalf("过期 exe 应被重写为新路径: %q", data)
	}
	// 缺失的不复活
	if _, err := os.Stat(filepath.Join(dir, "ok-on", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("缺失技能不应被自愈复活")
	}
	// 外来内容不动
	if d, _ := os.ReadFile(foreign); string(d) != "# 用户自己的笔记\n" {
		t.Fatalf("外来内容不应被改写: %q", d)
	}

	// SkillsInstalled 口径：全目录全技能且烘焙当前 exe 才算已接入
	if SkillsInstalled(`D:\new\ok.exe`) {
		t.Fatal("ok-on 缺失 + ok-off 外来，应判未接入")
	}
	// 补齐后（InstallSkills 幂等）应判已接入
	if err := InstallSkills(`D:\new\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if !SkillsInstalled(`D:\new\ok.exe`) {
		t.Fatal("全部技能就位且 exe 当前，应判已接入")
	}
	if SkillsInstalled(`D:\other\ok.exe`) {
		t.Fatal("exe 不一致应判未接入（只查存在性会把死路径误报为已接入）")
	}
}

// TestRemoveLegacySkills（2.25.0 改名迁移）：openknowledge-* 旧技能副本在
// InstallSkills/EnsureSkills 同窗口被清除；同名但内容不是本项目旧技能的
// 外来目录不动；旧名目录不存在时幂等。
func TestRemoveLegacySkills(t *testing.T) {
	dir := isolateSkills(t)
	// 旧版安装的技能副本（front matter name 匹配 openknowledge-*）
	legacy := filepath.Join(dir, "openknowledge-wiki")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	old := "---\nname: openknowledge-wiki\ndescription: 旧版技能\n---\n\n# openknowledge-wiki\n\n    \"D:/old/ok.exe\" wiki\n"
	if err := os.WriteFile(filepath.Join(legacy, "SKILL.md"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	// 同名前缀的外来目录（不是本项目技能）必须保留
	alien := filepath.Join(dir, "openknowledge-notes")
	if err := os.MkdirAll(alien, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(alien, "SKILL.md"), []byte("# 用户自己的 openknowledge 笔记\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallSkills(`D:\new\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("旧技能副本 openknowledge-wiki 应被清除")
	}
	if _, err := os.Stat(filepath.Join(alien, "SKILL.md")); err != nil {
		t.Fatal("外来目录不应被误删")
	}
	// 新技能已就位
	if _, err := os.Stat(filepath.Join(dir, "ok-wiki", "SKILL.md")); err != nil {
		t.Fatal("新技能 ok-wiki 应已安装")
	}
}
