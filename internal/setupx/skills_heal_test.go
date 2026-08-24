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
	if err := os.RemoveAll(filepath.Join(dir, "openknowledge-on")); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(dir, "openknowledge-off", "SKILL.md")
	if err := os.WriteFile(foreign, []byte("# 用户自己的笔记\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureSkills(`D:\new\ok.exe`); err != nil {
		t.Fatal(err)
	}

	// 仍在的技能：烘焙路径换新
	data, err := os.ReadFile(filepath.Join(dir, "openknowledge-init", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), filepath.ToSlash(`D:\new\ok.exe`)) ||
		strings.Contains(string(data), filepath.ToSlash(`D:\old\ok.exe`)) {
		t.Fatalf("过期 exe 应被重写为新路径: %q", data)
	}
	// 缺失的不复活
	if _, err := os.Stat(filepath.Join(dir, "openknowledge-on", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("缺失技能不应被自愈复活")
	}
	// 外来内容不动
	if d, _ := os.ReadFile(foreign); string(d) != "# 用户自己的笔记\n" {
		t.Fatalf("外来内容不应被改写: %q", d)
	}

	// SkillsInstalled 口径：全目录全技能且烘焙当前 exe 才算已接入
	if SkillsInstalled(`D:\new\ok.exe`) {
		t.Fatal("openknowledge-on 缺失 + openknowledge-off 外来，应判未接入")
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
