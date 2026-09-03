package enforce

import (
	"testing"

	"okryptos/internal/config"
	"okryptos/internal/state"
)

var rule = config.EnforceRule{
	Type:          "changelog_required",
	CodeGlobs:     []string{"**/*.go"},
	ChangelogGlob: "docs/changelogs/**",
	Message:       "请补变更日志",
}

func TestBlockWhenCodeTouchedWithoutChangelog(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("cmd/ok/main.go")
	block, reason, err := EvalChangelog(rule, st)
	if err != nil {
		t.Fatal(err)
	}
	if !block || reason != "请补变更日志" {
		t.Fatalf("expected block, got %v %q", block, reason)
	}
}

func TestNoBlockWhenChangelogTouched(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("cmd/ok/main.go")
	st.AddTouched("docs/changelogs/2026-07-22.md")
	block, _, _ := EvalChangelog(rule, st)
	if block {
		t.Fatal("expected no block")
	}
}

func TestNoBlockWhenNoCodeTouched(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("README.md")
	block, _, _ := EvalChangelog(rule, st)
	if block {
		t.Fatal("expected no block")
	}
}

func TestBlockOnlyOncePerSession(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("cmd/ok/main.go")
	st.MarkBlocked("changelog_required")
	block, _, _ := EvalChangelog(rule, st)
	if block {
		t.Fatal("rule already blocked once this session")
	}
}

func TestRootLevelGoFileMatches(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("main.go")
	block, _, _ := EvalChangelog(rule, st)
	if !block {
		t.Fatal("**/*.go should match root-level main.go")
	}
}

func TestChangelogGlobCaseInsensitive(t *testing.T) {
	// Touched 全小写入库；用户按惯例写大写 CHANGELOG.md 也必须命中
	r := config.EnforceRule{
		Type:          "changelog_required",
		CodeGlobs:     []string{"**/*.go"},
		ChangelogGlob: "CHANGELOG.md",
		Message:       "请补变更日志",
	}
	st := &state.Session{}
	st.AddTouched("cmd/ok/main.go")
	st.AddTouched("changelog.md")
	if block, _, _ := EvalChangelog(r, st); block {
		t.Fatal("uppercase CHANGELOG.md glob must match lowercase touched path")
	}
	// 反向：glob 小写、路径大写（防御写侧口径变化）
	st2 := &state.Session{}
	st2.AddTouched("cmd/ok/main.go")
	st2.AddTouched("CHANGELOG.md")
	if block, _, _ := EvalChangelog(r, st2); block {
		t.Fatal("match must be case-insensitive on both sides")
	}
}

// TestMalformedGlobReturnsError：L-20 回归——malformed glob 不得静默吞掉，
// 错误须上抛给调用方（否则规则失效无人知晓）。
func TestMalformedGlobReturnsError(t *testing.T) {
	st := &state.Session{}
	st.AddTouched("cmd/ok/main.go")
	bad := config.EnforceRule{
		Type:          "changelog_required",
		CodeGlobs:     []string{"**/*.go"},
		ChangelogGlob: "docs/[", // 未闭合字符类
	}
	if _, _, err := EvalChangelog(bad, st); err == nil {
		t.Fatal("malformed changelog_glob 应返回错误")
	}
	bad2 := config.EnforceRule{
		Type:          "changelog_required",
		CodeGlobs:     []string{"**/["},
		ChangelogGlob: "docs/changelogs/**",
	}
	if _, _, err := EvalChangelog(bad2, st); err == nil {
		t.Fatal("malformed code_globs 应返回错误")
	}
	// 合法 glob 不应误报错误
	if _, _, err := EvalChangelog(rule, st); err != nil {
		t.Fatalf("合法 glob 不应报错: %v", err)
	}
}
