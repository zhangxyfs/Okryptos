package index

// INDEX.md 渲染层：rebuildIndex 从 entries 表重写 <dir>/../INDEX.md，
// 及其渲染辅助（消毒、摘要去重、折叠行）。由 Sync 在 diff 非空或
// INDEX.md 缺失时调用。

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"openknowledge/internal/fsx"
)

// dedupSummary 摘要与标题冗余（规范化后相同/标题复读摘要主干/共有前缀≥摘要 80%）
// 时返回空串——渲染层兜底，存量"摘要复读标题"的条目无需回填。
// "标题复读摘要主干"判据：规范化后标题是摘要前缀，且标题长度≥摘要 40%——
// 仅首字偶然相同（如标题"短"与摘要"短甲……"）不算复读，摘要保留。
func dedupSummary(title, summary string) string {
	norm := func(s string) string {
		return strings.TrimRight(strings.TrimSpace(s), "。．.：:，,；;、 ")
	}
	t, s := norm(title), norm(summary)
	if s == "" || t == "" {
		return summary
	}
	tr, sr := []rune(t), []rune(s)
	n := 0
	for n < len(tr) && n < len(sr) && tr[n] == sr[n] {
		n++
	}
	if s == t {
		return ""
	}
	// 标题是摘要前缀且覆盖摘要主干（≥40%）：尾巴只是补充说明，省略摘要
	if n == len(tr) && float64(n) >= 0.4*float64(len(sr)) {
		return ""
	}
	if float64(n) >= 0.8*float64(len(sr)) {
		return ""
	}
	return summary
}

// mdInlineEscaper 转义行内 markdown 元字符：条目元数据（标题/摘要等）里的
// **/[]/` 不再破坏 INDEX.md 与注入文本的行结构。
var mdInlineEscaper = strings.NewReplacer(
	`\`, `\\`, `*`, `\*`, "[", `\[`, "]", `\]`, "`", "\\`",
)

// StripControls 删除控制字符（含换行/制表）：注入文本与 INDEX.md 按行组织
// 结构，条目元数据里的换行可伪造结构行（假"## 分支差异"小节头、假
// [OpenKnowledge] 系统指令行）。
func StripControls(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// SanitizeInline 把条目元数据（标题/摘要/标签/类型）压成单行安全文本：
// StripControls + markdown 元字符转义。正文（mandatory 全文等）是合法
// markdown，不在此列；文件路径用 StripControls（转义会破坏可读路径）。
func SanitizeInline(s string) string { return mdInlineEscaper.Replace(StripControls(s)) }

// indexRow 是 rebuildIndex 主列表渲染用的条目视图。
type indexRow struct {
	filename, title, typ, tags, summary string
	draft, weight                       int
	mtime                               int64
}

// rebuildIndex 从 entries 表重写 <dir>/../INDEX.md。主列表按价值排序
//（o.FeedbackWindowDays 天窗口 采纳×2+注入×1 降序，平局按 mtime 降序再按
// filename 升序；草稿沉底），超过 maxLines 的尾部折叠为一行可检索提示；
// archived 条目不进主列表（仍保留在库可检索）。wiki 目录节/分支差异节维持原有输出。
func (db *DB) rebuildIndex(dir string, o SyncOptions) error {
	maxLines := o.MaxLines
	if maxLines <= 0 {
		maxLines = 50
	}
	rows, err := db.sql.Query(`SELECT filename, title, type, tags, summary, draft, archived, mtime FROM entries`)
	if err != nil {
		return err
	}
	// FeedbackStats 失败静默降级（与 PruneEvents 一致）：权重全零退回 mtime/filename 序
	stats, _ := db.FeedbackStats(o.FeedbackWindowDays)
	var main, drafts []indexRow
	for rows.Next() {
		var r indexRow
		var archived int
		if err := rows.Scan(&r.filename, &r.title, &r.typ, &r.tags, &r.summary, &r.draft, &archived, &r.mtime); err != nil {
			_ = rows.Close()
			return err
		}
		if archived != 0 {
			continue
		}
		// 已转正的 wiki 条目只进 Wiki 目录节（带链接），主列表不重复
		if r.draft == 0 && hasWikiTag(r.tags) {
			continue
		}
		// 带 branch: 标签的条目（无论类型）不进全分支共享的主列表：
		// branch 标签语义=分支专属——wiki 差异条目已在下方差异节，
		// 非 wiki 分支条目仍可按分支检索命中，只是不进共享目录
		if BranchOf(splitTags(r.tags)) != "" {
			continue
		}
		s := stats[r.filename]
		r.weight = 2*s.Adoptions + s.Injections
		if r.draft != 0 {
			drafts = append(drafts, r)
		} else {
			main = append(main, r)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	byValue := func(rs []indexRow) {
		sort.SliceStable(rs, func(i, j int) bool {
			if rs[i].weight != rs[j].weight {
				return rs[i].weight > rs[j].weight
			}
			if rs[i].mtime != rs[j].mtime {
				return rs[i].mtime > rs[j].mtime
			}
			return rs[i].filename < rs[j].filename
		})
	}
	byValue(main)
	byValue(drafts)
	ordered := append(main, drafts...)

	var b strings.Builder
	b.WriteString("# 知识索引\n\n")
	shown := ordered
	var folded []indexRow
	if len(ordered) > maxLines {
		shown, folded = ordered[:maxLines], ordered[maxLines:]
	}
	for _, r := range shown {
		// 渲染层消毒：title/summary/tags 可被污染（CLI 可传、AI 优化直接落盘
		// LLM 输出），去控制字符防伪造行结构、转义 markdown 元字符防破坏格式
		title := SanitizeInline(r.title)
		if r.draft != 0 {
			title = "【草稿】" + title
		}
		typ, tags := SanitizeInline(r.typ), SanitizeInline(r.tags)
		if sum := dedupSummary(r.title, r.summary); sum != "" {
			fmt.Fprintf(&b, "- **%s** (%s) [%s] — %s\n", title, typ, tags, SanitizeInline(sum))
		} else {
			fmt.Fprintf(&b, "- **%s** (%s) [%s]\n", title, typ, tags)
		}
	}
	if len(folded) > 0 {
		writeFoldedLine(&b, folded)
	}
	if wikiEntries, err := db.WikiEntries(); err == nil && len(wikiEntries) > 0 {
		writeWikiLine := func(b *strings.Builder, we WikiEntry) {
			title, filename := SanitizeInline(we.Title), StripControls(we.Filename)
			if we.Summary != "" {
				fmt.Fprintf(b, "- [%s](%s) — %s\n", title, filename, SanitizeInline(we.Summary))
			} else {
				fmt.Fprintf(b, "- [%s](%s)\n", title, filename)
			}
		}
		b.WriteString("\n## Wiki 目录\n\n")
		branches := map[string][]WikiEntry{}
		for _, we := range wikiEntries {
			if we.Branch == "" {
				writeWikiLine(&b, we)
			} else {
				branches[we.Branch] = append(branches[we.Branch], we)
			}
		}
		names := make([]string, 0, len(branches))
		for n := range branches {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			// 小节头只去控制字符不转义：TrimIndexBranchSections 按原名比对
			// 分支名，转义会让当前分支小节失配被裁；含全角括号的怪异分支名
			// 生成的小节头通不过 Trim 的行格式校验，按 fail-open 保留不裁
			fmt.Fprintf(&b, "\n## 分支差异（%s）\n\n", StripControls(n))
			for _, we := range branches[n] {
				writeWikiLine(&b, we)
			}
		}
	}
	return fsx.WriteFile(filepath.Join(filepath.Dir(dir), "INDEX.md"), []byte(b.String()), 0o644)
}

// writeFoldedLine 渲染溢出折叠行：条数 + 被折叠条目 tags 计数降序前 5。
func writeFoldedLine(b *strings.Builder, folded []indexRow) {
	counts := map[string]int{}
	for _, r := range folded {
		for _, tg := range splitTags(r.tags) {
			counts[tg]++
		}
	}
	if len(counts) == 0 {
		fmt.Fprintf(b, "- 另有 %d 条未列出，可用关键词/向量检索命中\n", len(folded))
		return
	}
	type kv struct {
		tag string
		n   int
	}
	pairs := make([]kv, 0, len(counts))
	for tg, n := range counts {
		pairs = append(pairs, kv{tg, n})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].n != pairs[j].n {
			return pairs[i].n > pairs[j].n
		}
		return pairs[i].tag < pairs[j].tag
	})
	if len(pairs) > 5 {
		pairs = pairs[:5]
	}
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = fmt.Sprintf("%s×%d", SanitizeInline(p.tag), p.n)
	}
	fmt.Fprintf(b, "- 另有 %d 条未列出（tags 分布：%s），可用关键词/向量检索命中\n", len(folded), strings.Join(parts, ", "))
}
