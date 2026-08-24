package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetCaptureAndAutoBornAppendAndReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "# 头部注释\n\n[[enforce]]\ntype = \"changelog_required\"\nmessage = \"x\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetCaptureAndAutoBorn(path, "auto", 10, false, ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "# 头部注释") || !strings.Contains(got, "[[enforce]]") {
		t.Fatalf("unrelated content lost: %q", got)
	}
	if !strings.Contains(got, "[capture]\nmode = \"auto\"\nturn_interval = 10") {
		t.Fatalf("capture block missing: %q", got)
	}
	if !strings.Contains(got, "[provenance]\nauto_born = false") {
		t.Fatalf("provenance block missing: %q", got)
	}
	// 替换而非叠加：两小节各唯一
	if err := SetCaptureAndAutoBorn(path, "propose", 3, true, ""); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	got = string(data)
	if strings.Count(got, "[capture]") != 1 || strings.Count(got, "[provenance]") != 1 {
		t.Fatalf("duplicate block: %q", got)
	}
	if !strings.Contains(got, "mode = \"propose\"") || !strings.Contains(got, "turn_interval = 3") || strings.Contains(got, "turn_interval = 10") {
		t.Fatalf("capture replace failed: %q", got)
	}
	if !strings.Contains(got, "auto_born = true") || strings.Contains(got, "auto_born = false") {
		t.Fatalf("provenance replace failed: %q", got)
	}
	// 合并读取应生效
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Capture.Mode != "propose" || cfg.Capture.TurnInterval != 3 || !cfg.Provenance.AutoBorn {
		t.Fatalf("merged config wrong: %+v %+v", cfg.Capture, cfg.Provenance)
	}
}

func TestSetCaptureAndAutoBornKeepsExistingProvenance(t *testing.T) {
	// 已存在的 [provenance] 被整段替换，其前的 [capture] 同次替换（单次落盘语义）
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "[capture]\nmode = \"auto\"\nturn_interval = 5\n\n[provenance]\nauto_born = true\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetCaptureAndAutoBorn(path, "propose", 2, false, ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	if strings.Count(got, "[capture]") != 1 || strings.Count(got, "[provenance]") != 1 {
		t.Fatalf("duplicate block: %q", got)
	}
	if !strings.Contains(got, "mode = \"propose\"") || !strings.Contains(got, "auto_born = false") {
		t.Fatalf("replace failed: %q", got)
	}
}

func TestSetCaptureAndAutoBornMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SetCaptureAndAutoBorn(path, "auto", 7, true, "# header\n"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.HasPrefix(got, "# header\n") || !strings.Contains(got, "turn_interval = 7") || !strings.Contains(got, "auto_born = true") {
		t.Fatalf("unexpected: %q", got)
	}
}
