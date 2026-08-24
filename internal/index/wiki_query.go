package index

// wiki 查询群：WikiEntries 一族（wiki 标签条目的目录/计数/覆盖判定），
// 供 INDEX.md Wiki 目录节、ok wiki、ok search 提示使用。

// WikiEntry 是 Wiki 目录的一行。
type WikiEntry struct {
	Title    string
	Filename string
	Summary  string
	Branch   string
}

// WikiEntries 返回打 wiki 标签的已转正未归档条目（按 title 排序）。
// 与 rebuildIndex 主列表口径一致：归档条目不进 INDEX（含 Wiki 目录节）。
// SQL 的 LIKE 只是粗筛，精确判定在 Go 侧（hasWikiTag），防 sewiki/nowiki 误判。
func (db *DB) WikiEntries() ([]WikiEntry, error) {
	rows, err := db.sql.Query(`SELECT title, filename, summary, tags FROM entries WHERE draft = 0 AND archived = 0 AND tags LIKE '%wiki%' ORDER BY title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WikiEntry
	for rows.Next() {
		var e WikiEntry
		var tagsStr string
		if err := rows.Scan(&e.Title, &e.Filename, &e.Summary, &tagsStr); err != nil {
			return nil, err
		}
		if !hasWikiTag(tagsStr) {
			continue
		}
		e.Branch = BranchOf(splitTags(tagsStr))
		out = append(out, e)
	}
	return out, rows.Err()
}

// HasBranchWiki 报告指定分支是否存在已转正的差异条目（wiki 标签且 branch 精确匹配）。
// 空分支（非 git/未知）直接 false：无分支 wiki 条目不是任何分支的差异条目。
func (db *DB) HasBranchWiki(branch string) (bool, error) {
	if branch == "" {
		return false, nil
	}
	entries, err := db.WikiEntries()
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Branch == branch {
			return true, nil
		}
	}
	return false, nil
}

// WikiCount 返回 wiki 条目数（ok wiki mark 展示用）。与 WikiEntries 同口径
//（已转正、未归档、wiki 精确标签）——直接复用计数，防两处 SQL 各自漂移。
func (db *DB) WikiCount() (int, error) {
	entries, err := db.WikiEntries()
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

// HasWikiMatch 报告检索词是否有 wiki 条目（draft=0 且 tags 含 wiki）覆盖。
// 仅看 FTS 关键词、不看向量——兜底启发式，供 ok search 输出提示；terms 为空返回 true。
// 与 WikiEntries 同一实现口径：SQL 的 LIKE '%wiki%' 只是粗筛（超集，常量子串无
// 通配符转义问题；SQLite LIKE 的 ASCII 大小写不敏感只会放宽不会漏），精确判定
// 收敛到 Go 侧 hasWikiTag（防 sewiki/nowiki 误判，全包唯一实现）。
func (db *DB) HasWikiMatch(terms []string) (bool, error) {
	match := buildMatch(terms)
	if match == "" {
		return true, nil
	}
	rows, err := db.sql.Query(
		`SELECT e.tags FROM entries_fts JOIN entries e ON e.filename = entries_fts.filename
		WHERE entries_fts MATCH ? AND e.draft = 0 AND e.tags LIKE '%wiki%'`, match)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var tagsStr string
		if err := rows.Scan(&tagsStr); err != nil {
			return false, err
		}
		if hasWikiTag(tagsStr) {
			return true, nil
		}
	}
	return false, rows.Err()
}
