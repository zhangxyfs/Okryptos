package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"openknowledge/internal/embed"
	"openknowledge/internal/entry"
	"openknowledge/internal/retrieve"
)

// ftsText 将原文切分为空格分隔的词元文本（复用 retrieve.Terms），
// 供 FTS 表入库；MATCH 查询使用同样的切分保证词元一致。
func ftsText(s string) string { return strings.Join(retrieve.Terms(s), " ") }

// CorruptEntriesError 表示同步已完成，但有文件因解析失败被跳过。
type CorruptEntriesError struct{ Files []string }

func (e *CorruptEntriesError) Error() string {
	return fmt.Sprintf("跳过 %d 个损坏条目: %s", len(e.Files), strings.Join(e.Files, ", "))
}

// readEntry 读取并解析单个条目文件；仅当 diff 判定条目变化
// （或未变化条目需要补向量）时才被调用。
func readEntry(path string) (*entry.Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	e, err := entry.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	e.Path = path
	return e, nil
}

// SyncOptions 控制 Sync 重建 INDEX.md 的渲染预算与统计口径；零值走默认
//（MaxLines=50、FeedbackWindowDays=30）。
type SyncOptions struct {
	MaxLines int // 主列表最大行数，<=0 按 50
	// FeedbackWindowDays 是主列表价值排序的反馈统计窗口（天），与查询侧
	// [retrieve.feedback] window_days 同口径；<=0 按 30（FeedbackStats 归一）。
	FeedbackWindowDays int
}

// Sync 将 dir（knowledge 目录）下的 Markdown 条目增量同步进索引库：
// 先用 os.ReadDir 枚举文件名+mtime+size（不读文件内容），与 entries 表按
// filename+mtime+size 对比（双判：外部编辑器同秒双写时 size 兜底）；仅
// 新增/变化的条目才 read+parse 并 upsert（entries 存原文，entries_fts 存
// ftsText 切分文本），client!=nil 时收集变化条目与缺向量的未变化条目
//（只读这些文件）的 EmbedText。entries/fts/死条目删除先在一个事务内提交；
// embedding 网络调用在写事务外执行，每批（32 条）算完向量后开一个毫秒级
// 小事务写入 vectors——持锁时长与库规模脱钩，全量重建期间 hook/GUI 的
// 读写不再被分钟级写锁堵死；批次失败时已提交的 entries 保留（mtime 幂等），
// 本轮已写入的向量行会被回滚（否则 meta 未写而 vectors 有部分行，下一轮
// 会命中模型身份闸被判 embedBlocked，向量写入永久静默停摆），缺向量条目
// 由下一轮 Sync 的补齐路径干净重试。
// 提交后若 client 身份非空且确有向量写入，则刷新 meta 表的
// embedding_model/embedding_dim。client 身份与 meta 记录不符时
// 跳过全部向量写与 meta 更新（INDEX/FTS 照常），杜绝新旧模型向量
// 混合——需调用方显式 ClearVectors 后再同步以全量重建。
// 库中多余的 filename 删除。变化条目解析失败时跳过该文件（已索引旧行
// 保留，无旧行则缺席），其余条目照常提交——一个 YAML 笔误不能压制全部
// 注入；提交成功后若有跳过，返回 *CorruptEntriesError 警告（调用方用
// errors.As 区分）。SQL 失败、目录不可读、INDEX.md 写入失败等致命错误
// 仍中止。diff 非空或 INDEX.md 缺失时重建 <dir>/../INDEX.md，
// 无变化的纯热路径不做任何写盘。
func (db *DB) Sync(dir string, client embed.Client, opts ...SyncOptions) error {
	o := SyncOptions{}
	if len(opts) > 0 {
		o = opts[0]
	}
	// Windows 上 DirEntry.Info 复用 readdir 数据，无额外系统调用
	dirents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type diskFile struct {
		name  string
		path  string
		mtime int64
		size  int64
	}
	var disk []diskFile
	for _, de := range dirents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			return err
		}
		disk = append(disk, diskFile{de.Name(), filepath.Join(dir, de.Name()), info.ModTime().Unix(), info.Size()})
	}

	// 变化判据 mtime+size 双判：秒级 mtime 下外部编辑器同秒双写（或 FAT 2 秒
	// 粒度）第二次内容不同的写入单靠 mtime 会漏判。存量库 size 列为 0，首轮
	// 全部判变化重读一次（幂等无害），之后 size 入库。
	type indexedFile struct{ mtime, size int64 }
	existing := map[string]indexedFile{}
	rows, err := db.sql.Query(`SELECT filename, mtime, size FROM entries`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		var f indexedFile
		if err := rows.Scan(&name, &f.mtime, &f.size); err != nil {
			_ = rows.Close()
			return err
		}
		existing[name] = f
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	hasVector := map[string]bool{}
	vrows, err := db.sql.Query(`SELECT filename FROM vectors`)
	if err != nil {
		return err
	}
	for vrows.Next() {
		var name string
		if err := vrows.Scan(&name); err != nil {
			_ = vrows.Close()
			return err
		}
		hasVector[name] = true
	}
	if err := vrows.Err(); err != nil {
		_ = vrows.Close()
		return err
	}
	_ = vrows.Close()

	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}

	// 模型身份闸：client 身份与索引 meta 不符时跳过全部向量写（INDEX/FTS 照常），
	// 杜绝新旧模型向量混合；由 ok index 显式 ClearVectors 后全量重建。
	// meta 空但有向量 = ≤2.13 历史库（向量身份不明），同样阻断待重建。
	embedBlocked := client == nil
	if client != nil && client.ModelIdentity() != "" {
		m, _, err := db.EmbeddingMeta()
		if err == nil {
			switch {
			case m != "" && m != client.ModelIdentity():
				embedBlocked = true // 身份不符
			case m == "":
				if hv, herr := db.HasVectors(); herr == nil && hv {
					embedBlocked = true // 历史向量无身份记录（≤2.13 库），阻断待 ok index 重建
				}
			}
		}
	}
	type pendingEmbed struct{ name, text string }
	var pending []pendingEmbed

	alive := map[string]bool{}
	changed := false
	var skipped []string
	for _, f := range disk {
		name := f.name
		alive[name] = true
		mtime := f.mtime
		if old, ok := existing[name]; ok && old.mtime == mtime && old.size == f.size {
			// 未变化条目不读不解析；仅在缺向量且可算向量时收集补齐
			if !embedBlocked && !hasVector[name] {
				e, err := readEntry(f.path)
				if err != nil {
					// 与 changed 路径同口径：损坏条目跳过、记入告警，不中止整轮
					// 同步（写坏后 mtime+size 均未变的场景会走到这里——"一个
					// YAML 笔误不能压制全部注入"）
					skipped = append(skipped, name)
					continue
				}
				pending = append(pending, pendingEmbed{name, e.EmbedText()})
			}
			continue
		}
		changed = true
		e, err := readEntry(f.path)
		if err != nil {
			// 损坏条目跳过：已索引旧行保留（新文件则缺席），其余条目照常提交；
			// mtime 未入库，下次同步会重试并在修复后自动追上
			skipped = append(skipped, name)
			continue
		}
		tags := strings.Join(e.Tags, ", ")
		mandatory := 0
		if e.Mandatory {
			mandatory = 1
		}
		draft := 0
		if e.Draft {
			draft = 1
		}
		archived := 0
		if e.Archived {
			archived = 1
		}
		if _, err := tx.Exec(`INSERT INTO entries(filename,title,type,tags,summary,body,mandatory,draft,archived,mtime,size)
			VALUES(?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(filename) DO UPDATE SET
			title=excluded.title, type=excluded.type, tags=excluded.tags,
			summary=excluded.summary, body=excluded.body,
			mandatory=excluded.mandatory, draft=excluded.draft,
			archived=excluded.archived, mtime=excluded.mtime, size=excluded.size`,
			name, e.Title, e.Type, tags, e.Summary, e.Body, mandatory, draft, archived, mtime, f.size); err != nil {
			return rollback(err)
		}
		if _, err := tx.Exec(`DELETE FROM entries_fts WHERE filename=?`, name); err != nil {
			return rollback(err)
		}
		if _, err := tx.Exec(`INSERT INTO entries_fts(title,tags,summary,body,filename) VALUES(?,?,?,?,?)`,
			ftsText(e.Title), ftsText(strings.Join(e.Tags, " ")), ftsText(e.Summary), ftsText(e.Body), name); err != nil {
			return rollback(err)
		}
		if !embedBlocked {
			pending = append(pending, pendingEmbed{name, e.EmbedText()})
		}
	}
	for name := range existing {
		if !alive[name] {
			changed = true
			for _, q := range []string{
				`DELETE FROM entries WHERE filename=?`,
				`DELETE FROM entries_fts WHERE filename=?`,
				`DELETE FROM vectors WHERE filename=?`,
			} {
				if _, err := tx.Exec(q, name); err != nil {
					return rollback(err)
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// embedding 网络调用在写事务外执行：每批算完向量开一个毫秒级小事务写入，
	// 持锁时长与库规模脱钩——全量重建期间 hook/GUI 的读写不再被分钟级写锁
	// 堵死。批次失败时已提交的 entries 保留（mtime 幂等），本轮已写入的向量
	// 行由 failEmbed 回滚，缺向量条目由下一轮 Sync 的"未变化缺向量补齐"
	// 路径干净重试。
	const embedBatchSize = 32
	vecDim := 0
	// written 记录本轮已提交的向量行；批次中途失败时 failEmbed 删除它们——
	// 否则会留下"meta 未写 + vectors 有部分行"的状态，下一轮同步命中上方
	// 模型身份闸的"meta 空 + HasVectors"分支被判 embedBlocked（历史库待
	// 重建语义），向量写入永久静默停摆。
	var written []string
	failEmbed := func(err error) error {
		if len(written) > 0 {
			q := `DELETE FROM vectors WHERE filename IN (?` + strings.Repeat(",?", len(written)-1) + `)`
			args := make([]any, len(written))
			for i, name := range written {
				args[i] = name
			}
			// 清理失败不遮蔽原始错误（下一轮身份闸兜底为 embedBlocked，与清理前行为一致）
			_, _ = db.sql.Exec(q, args...)
		}
		return err
	}
	for i := 0; i < len(pending); i += embedBatchSize {
		j := i + embedBatchSize
		if j > len(pending) {
			j = len(pending)
		}
		texts := make([]string, 0, j-i)
		for _, p := range pending[i:j] {
			texts = append(texts, p.text)
		}
		vecs, err := client.EmbedDocuments(context.Background(), texts)
		if err != nil {
			return failEmbed(err)
		}
		vtx, err := db.sql.Begin()
		if err != nil {
			return failEmbed(err)
		}
		for k, vec := range vecs {
			vecDim = len(vec)
			if _, err := vtx.Exec(`INSERT OR REPLACE INTO vectors(filename,dim,blob) VALUES(?,?,?)`,
				pending[i+k].name, len(vec), encodeVector(vec)); err != nil {
				_ = vtx.Rollback()
				return failEmbed(err)
			}
		}
		if err := vtx.Commit(); err != nil {
			return failEmbed(err)
		}
		for k := range vecs {
			written = append(written, pending[i+k].name)
		}
	}
	if !embedBlocked && client != nil && vecDim > 0 && client.ModelIdentity() != "" {
		// meta 写失败与批失败同款处理（R3 D-01）：回滚本轮向量——否则库停留在
		// "meta 空 + vectors 有行"，下一轮被身份闸判 embedBlocked 永久停摆。
		if err := db.SetMeta("embedding_model", client.ModelIdentity()); err != nil {
			return failEmbed(err)
		}
		if err := db.SetMeta("embedding_dim", strconv.Itoa(vecDim)); err != nil {
			return failEmbed(err)
		}
	}
	// 顺带 prune 60 天前的条目事件（统计性数据，失败不阻断 Sync）
	_ = db.PruneEvents(time.Now().Unix() - 60*86400)
	// diff 为空时跳过重写（hook 热路径除上方事件 prune 外零写盘）；INDEX.md 缺失时总是重建
	if !changed {
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "INDEX.md")); err == nil {
			// 补齐路径（未变化但缺向量）的跳过告警不能被热路径吞掉
			if len(skipped) > 0 {
				return &CorruptEntriesError{Files: skipped}
			}
			return nil
		}
	}
	if err := db.rebuildIndex(dir, o); err != nil {
		return err
	}
	if len(skipped) > 0 {
		return &CorruptEntriesError{Files: skipped}
	}
	return nil
}
