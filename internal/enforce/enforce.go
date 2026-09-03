package enforce

import (
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"okryptos/internal/config"
	"okryptos/internal/state"
)

// EvalChangelog 判定 changelog_required 规则：触碰过 code_globs 且未触碰
// changelog_glob → 阻断（同会话同规则已被阻断过则放行，防死循环）。
// Touched 全小写入库，doublestar 区分大小写——glob 与路径统一折叠小写再匹配，
// 否则 CHANGELOG.md 这类业界惯例大写写法永不命中（每会话白挨一次阻断）。
// malformed glob 不吞错（L-20）：规则静默失效比报错更难排查，错误经 err 上抛。
func EvalChangelog(rule config.EnforceRule, st *state.Session) (block bool, reason string, err error) {
	if st.HasBlocked(rule.Type) {
		return false, "", nil
	}
	chgGlob := strings.ToLower(rule.ChangelogGlob)
	code := false
	for _, p := range st.Touched {
		p = strings.ToLower(p)
		ok, err := doublestar.Match(chgGlob, p)
		if err != nil {
			return false, "", fmt.Errorf("changelog_glob %q 无效: %w", rule.ChangelogGlob, err)
		}
		if ok {
			return false, "", nil
		}
		if !code {
			for _, g := range rule.CodeGlobs {
				ok, err := doublestar.Match(strings.ToLower(g), p)
				if err != nil {
					return false, "", fmt.Errorf("code_globs %q 无效: %w", g, err)
				}
				if ok {
					code = true
					break
				}
			}
		}
	}
	if !code {
		return false, "", nil
	}
	return true, rule.Message, nil
}
