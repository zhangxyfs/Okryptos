package retrieve

import "strings"

const (
	// hitsHeader / stickyHeader / noticePrefix 是 hook 注入文本的固定包裹标记
	// （internal/hook/core.go 的注入模板），净化按标记确定性剥离。
	hitsHeader   = "## 相关知识（需要全文时读取对应文件）"
	stickyHeader = "## 必守规约（全文见文件，必要时读取）"
	noticePrefix = "[Okryptos]"
)

// CleanQuery 剥离 prompt 中已知的 Okryptos 注入块，防止检索词被自身注入
// 污染（外部实测：harness 噪声块可把 FTS/向量命中率打到零——框架把上一轮注入
// 回传进 prompt 时，注入内容会变成检索词的一部分）。
// 覆盖：检索命中块（段序固定后恒为末段，头部截到文末）、必守规约粘性指针块、
// 所有 [Okryptos] 前缀行。不覆盖（无法确定性剥离）：mandatory 全文与 INDEX
// 原文——它们是稳定内容，若被回传只引入稳定关键词，污染有限。
func CleanQuery(prompt string) string {
	s := prompt
	if i := strings.Index(s, hitsHeader); i >= 0 {
		s = s[:i]
	}
	s = stripSection(s, stickyHeader)
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), noticePrefix) {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

// stripSection 删除从 header 起到下一个 "## " 小节头（不含，保留该小节）或文末的整块。
func stripSection(s, header string) string {
	i := strings.Index(s, header)
	if i < 0 {
		return s
	}
	rest := s[i+len(header):]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		return s[:i] + rest[j+1:]
	}
	return s[:i]
}
