package enforce

import (
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"openknowledge/internal/config"
	"openknowledge/internal/state"
)

// EvalChangelog 判定 changelog_required 规则：触碰过 code_globs 且未触碰
// changelog_glob → 阻断（同会话同规则已被阻断过则放行，防死循环）。
// Touched 全小写入库，doublestar 区分大小写——glob 与路径统一折叠小写再匹配，
// 否则 CHANGELOG.md 这类业界惯例大写写法永不命中（每会话白挨一次阻断）。
func EvalChangelog(rule config.EnforceRule, st *state.Session) (block bool, reason string) {
	if st.HasBlocked(rule.Type) {
		return false, ""
	}
	chgGlob := strings.ToLower(rule.ChangelogGlob)
	code := false
	for _, p := range st.Touched {
		p = strings.ToLower(p)
		if ok, _ := doublestar.Match(chgGlob, p); ok {
			return false, ""
		}
		if !code {
			for _, g := range rule.CodeGlobs {
				if ok, _ := doublestar.Match(strings.ToLower(g), p); ok {
					code = true
					break
				}
			}
		}
	}
	if !code {
		return false, ""
	}
	return true, rule.Message
}
