package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetRetrieveDedupTurnsUpsert [retrieve] 小节内单键 upsert：其余键、注释与
// [retrieve.gate] 子表原样保留；键行落在 [retrieve] 顶层段内（子表之前）；
// 重复设置幂等。
func TestSetRetrieveDedupTurnsUpsert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	src := "# 顶部注释\n[retrieve]\nalpha = 1.5 # 行内注释\nfusion = \"rrf\"\n\n[retrieve.gate]\nenabled = false\nextra_phrases = [\"走起\"]\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetRetrieveDedupTurns(path, 5); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"# 顶部注释", "alpha = 1.5 # 行内注释", "fusion = \"rrf\"", "dedup_turns = 5", "[retrieve.gate]\nenabled = false", `extra_phrases = ["走起"]`} {
		if !strings.Contains(got, want) {
			t.Fatalf("upsert 后丢失 %q:\n%s", want, got)
		}
	}
	// 键行必须在 [retrieve] 顶层段内（[retrieve.gate] 子表之前）
	if strings.Index(got, "dedup_turns = 5") > strings.Index(got, "[retrieve.gate]") {
		t.Fatalf("键行落在子表内:\n%s", got)
	}
	// 回读生效且子表不受影响
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.DedupTurns != 5 || cfg.Retrieve.Alpha != 1.5 || cfg.Retrieve.Gate.Enabled {
		t.Fatalf("回读值不对: %+v", cfg.Retrieve)
	}
	// 重复设置：替换而非追加
	if err := SetRetrieveDedupTurns(path, 0); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	got = string(data)
	if strings.Count(got, "dedup_turns") != 1 || !strings.Contains(got, "dedup_turns = 0") {
		t.Fatalf("重复设置应幂等替换:\n%s", got)
	}
}

// TestSetRetrieveDedupTurnsNoSection 无 [retrieve] 小节时文件尾追加整块
//（[retrieve.gate] 子表已存在也不受影响）。
func TestSetRetrieveDedupTurnsNoSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[retrieve.gate]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetRetrieveDedupTurns(path, 7); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.DedupTurns != 7 || cfg.Retrieve.Gate.Enabled {
		t.Fatalf("回读值不对: %+v", cfg.Retrieve)
	}
}

// TestSetRetrieveDedupTurnsNewFile 文件不存在时直接创建。
func TestSetRetrieveDedupTurnsNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SetRetrieveDedupTurns(path, 9); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.DedupTurns != 9 {
		t.Fatalf("got %v, want 9", cfg.Retrieve.DedupTurns)
	}
}

// TestDedupTurnsDefault 默认 3；配置文件缺省时 Load 回填默认值。
func TestDedupTurnsDefault(t *testing.T) {
	if got := Default().Retrieve.DedupTurns; got != 3 {
		t.Fatalf("默认应为 3, got %v", got)
	}
}

// TestEffectiveDedupTurns <0 归一为 0（关闭），0 与正值原样返回。
func TestEffectiveDedupTurns(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{-5, 0}, {-1, 0}, {0, 0}, {3, 3}, {99, 99}} {
		if got := (Retrieve{DedupTurns: tc.in}).EffectiveDedupTurns(); got != tc.want {
			t.Fatalf("DedupTurns=%d: got %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestUpsertTomlKeyBoundary 键名前缀匹配带边界判定：dedup_turns_v2 这类
// 前缀相同的自定义键不得被误替换，upsert 应另起正确键行。
func TestUpsertTomlKeyBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	src := "[retrieve]\ndedup_turns_v2 = 9\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetRetrieveDedupTurns(path, 5); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "dedup_turns_v2 = 9") {
		t.Fatalf("自定义键被误替换:\n%s", got)
	}
	if !strings.Contains(got, "dedup_turns = 5") {
		t.Fatalf("目标键未写入:\n%s", got)
	}
	// 再次设置命中真键行，不影响自定义键
	if err := SetRetrieveDedupTurns(path, 0); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	got = string(data)
	if !strings.Contains(got, "dedup_turns_v2 = 9") || !strings.Contains(got, "dedup_turns = 0") || strings.Count(got, "dedup_turns = ") != 1 {
		t.Fatalf("二次 upsert 异常:\n%s", got)
	}
}

func TestCoverageDefault(t *testing.T) {
	cfg := Default()
	if !cfg.Retrieve.Coverage.Enabled || cfg.Retrieve.Coverage.MinRatio != 0.5 {
		t.Fatalf("coverage 默认应为 enabled+0.5: %+v", cfg.Retrieve.Coverage)
	}
}

func TestCoverageMergedNoAliasing(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	project := filepath.Join(dir, "project.toml")
	os.WriteFile(global, []byte("[retrieve.coverage]\nextra_stop_terms = [\"甲乙\"]\n"), 0o644)
	os.WriteFile(project, []byte("[retrieve.coverage]\nmin_ratio = 0.75\n"), 0o644)
	cfg, err := LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.Coverage.MinRatio != 0.75 {
		t.Fatalf("项目层应覆盖 min_ratio: %+v", cfg.Retrieve.Coverage)
	}
	// 全局层数组在项目层未重定义时应保留（且不因合并被污染）
	if len(cfg.Retrieve.Coverage.ExtraStopTerms) != 1 || cfg.Retrieve.Coverage.ExtraStopTerms[0] != "甲乙" {
		t.Fatalf("extra_stop_terms 合并异常: %+v", cfg.Retrieve.Coverage.ExtraStopTerms)
	}
}
