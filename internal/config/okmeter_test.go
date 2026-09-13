package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 默认值语义：缺文件/缺键均为 true（反向判定：显式 false 才关闭）。
func TestOKMeterEnabledDefault(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "config.toml")
	cfg, err := Load(missing)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OKMeterEnabled {
		t.Fatal("缺文件应默认 true")
	}

	empty := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(empty, []byte("[capture]\nmode = \"auto\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(empty)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OKMeterEnabled {
		t.Fatal("缺键应默认 true")
	}

	off := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(off, []byte("okmeter_enabled = false\n\n[capture]\nmode = \"auto\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(off)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OKMeterEnabled {
		t.Fatal("显式 false 应读到 false")
	}
}

// LoadMerged 顶层键语义：项目 config 无该键时全局值生效。
func TestOKMeterEnabledMerged(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	project := filepath.Join(dir, "project.toml")
	if err := os.WriteFile(global, []byte("okmeter_enabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte("[capture]\nmode = \"auto\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OKMeterEnabled {
		t.Fatal("全局 false 应经合并生效")
	}
}

// SetOKMeterEnabled：新建 / 原位替换 / 插到首个小节前（不得落进任何小节内）。
func TestSetOKMeterEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	// 文件不存在 → 创建
	if err := SetOKMeterEnabled(path, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "okmeter_enabled = false\n" {
		t.Fatalf("新建内容错误: %q", data)
	}

	// 追加小节后切换 → 原位替换，不重复追加、不落入小节
	if err := os.WriteFile(path, []byte("okmeter_enabled = false\n\n[capture]\nmode = \"auto\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetOKMeterEnabled(path, true); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Count(string(data), "okmeter_enabled") != 1 || !strings.Contains(string(data), "okmeter_enabled = true") {
		t.Fatalf("替换应唯一且为 true: %q", data)
	}

	// 无顶层键 → 插到首个小节前
	if err := os.WriteFile(path, []byte("[capture]\nmode = \"auto\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetOKMeterEnabled(path, false); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	s := string(data)
	idx := strings.Index(s, "okmeter_enabled = false")
	if idx < 0 || idx > strings.Index(s, "[capture]") {
		t.Fatalf("键应位于首个小节前: %q", s)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OKMeterEnabled || cfg.Capture.Mode != "auto" {
		t.Fatalf("回读错误: %+v", cfg)
	}
}
