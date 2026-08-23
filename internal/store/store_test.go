package store

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateToBudget(t *testing.T) {
	if got := TruncateToBudget(strings.Repeat("好", 100), 10); !strings.HasSuffix(got, "…(已截断)") {
		t.Fatalf("expected truncation marker: %q", got)
	}
	if got := TruncateToBudget("short", 100); got != "short" {
		t.Fatalf("short text should pass through, got %q", got)
	}
	// CJK 密度约 1 token/字：预算 10 字的文本不应再按旧 2 字/token 放行 20 字
	if got := TruncateToBudget(strings.Repeat("好", 100), 10); strings.Count(got, "好") > 10 {
		t.Fatalf("CJK budget overshoot: %q", got)
	}
	// 负数预算（配置笔误）钳为 0：不得 panic，只留截断标记
	if got := TruncateToBudget("hello", -5); got != "\n…(已截断)" {
		t.Fatalf("negative budget should clamp to marker only, got %q", got)
	}
	// 截断结果含标记也不得超预算
	if got := TruncateToBudget(strings.Repeat("a", 100), 10); EstimateTokens(got) > 10 {
		t.Fatalf("truncated result with marker must stay in budget: %d", EstimateTokens(got))
	}
}

func TestEstimateTokens(t *testing.T) {
	// 纯中文按 1 token/字计（旧实现 runes/2 低估一倍）
	if got := EstimateTokens(strings.Repeat("好", 8)); got != 8 {
		t.Fatalf("CJK estimate = %d, want 8", got)
	}
	// 纯拉丁按 4 字符/token 计
	if got := EstimateTokens(strings.Repeat("a", 8)); got != 2 {
		t.Fatalf("latin estimate = %d, want 2", got)
	}
}

// TestTruncateToBudgetCJKRuneSafe 钉死 CJK 截断的 rune 安全与标记成本预扣：
// 结果必须是合法 UTF-8（不断字）且含标记不超预算。
func TestTruncateToBudgetCJKRuneSafe(t *testing.T) {
	s := strings.Repeat("汉字混排ab", 50)
	got := TruncateToBudget(s, 20)
	if !utf8.ValidString(got) {
		t.Errorf("截断结果必须是合法 UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "…(已截断)") {
		t.Errorf("截断结果应带标记: %q", got)
	}
	if est := EstimateTokens(got); est > 20 {
		t.Errorf("含标记不应超预算: est=%d > 20", est)
	}
}
