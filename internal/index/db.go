// Package index 提供基于 SQLite+FTS5 的知识条目索引，
// 使检索在大规模条目（万级）下无需逐文件扫描 Markdown。
package index

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"

	_ "modernc.org/sqlite"
)

// schema 建表语句。entries 存原文（供注入与 INDEX.md）；
// entries_fts 是独立内容的 FTS5 表（不用 external-content + 触发器），
// 存 ftsText 切分后的文本，由同步代码显式维护（delete+insert），
// 行与 entries 以 filename 关联（UNINDEXED 列）。
const schema = `
CREATE TABLE IF NOT EXISTS entries(
  filename TEXT PRIMARY KEY,
  title TEXT NOT NULL, type TEXT NOT NULL,
  tags TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  mandatory INTEGER NOT NULL DEFAULT 0,
  draft INTEGER NOT NULL DEFAULT 0,
  archived INTEGER NOT NULL DEFAULT 0,
  mtime INTEGER NOT NULL DEFAULT 0,
  size INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS vectors(
  filename TEXT PRIMARY KEY,
  dim INTEGER NOT NULL, blob BLOB NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(
  title, tags, summary, body,
  filename UNINDEXED
);
CREATE TABLE IF NOT EXISTS meta(
  key TEXT PRIMARY KEY, value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS entry_events(
  filename TEXT NOT NULL, kind TEXT NOT NULL, ts INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_entry_events_filename_ts ON entry_events(filename, ts);
`

// DB 是知识索引库句柄。
type DB struct {
	sql *sql.DB
}

// Open 打开（必要时创建）索引库并建表；若同目录存在旧版 vectors.json
// 且 vectors 表为空，则导入其向量并将该文件改名为 vectors.json.bak。
func Open(path string) (*DB, error) {
	// busy_timeout：daemon 与本地兜底路径短暂并发写时等待而非立即 SQLITE_BUSY；
	// WAL：读写不互斥（注入查询不再被同步写事务挡住，多进程并发下 busy 大幅减少）；
	// synchronous=NORMAL：WAL 下的推荐配对（断电至多丢最后一个已提交事务，不损坏
	// 库；索引可由条目文件重建，可接受）
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(3000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := sqldb.Exec(schema); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	db := &DB{sql: sqldb}
	// 旧库补列（CREATE TABLE IF NOT EXISTS 不会改动已存在的表）：
	// draft（v2.0 及更早的库）、archived（归档标记）、size（mtime+size 变化双判）
	for _, c := range []struct{ column, ddl string }{
		{"draft", `ALTER TABLE entries ADD COLUMN draft INTEGER NOT NULL DEFAULT 0`},
		{"archived", `ALTER TABLE entries ADD COLUMN archived INTEGER NOT NULL DEFAULT 0`},
		{"size", `ALTER TABLE entries ADD COLUMN size INTEGER NOT NULL DEFAULT 0`},
	} {
		if err := db.ensureColumn(c.column, c.ddl); err != nil {
			_ = sqldb.Close()
			return nil, err
		}
	}
	if err := db.migrateVectorsJSON(path); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	return db, nil
}

// ensureColumn 为旧库补 entries 列：先探列存在性，缺失才 ALTER。
// ALTER 失败后重探一次：多进程并发首开同一旧库时另一进程可能已抢先加列
// （duplicate column 错误），列已存在即收尾成功，否则返回原始错误。
func (db *DB) ensureColumn(column, ddl string) error {
	has, err := db.hasColumn(column)
	if err != nil || has {
		return err
	}
	if _, err := db.sql.Exec(ddl); err != nil {
		if has2, perr := db.hasColumn(column); perr == nil && has2 {
			return nil // 并发首开：别的进程已加列
		}
		return err
	}
	return nil
}

// hasColumn 报告 entries 表是否已有指定列。
func (db *DB) hasColumn(column string) (bool, error) {
	rows, err := db.sql.Query(`PRAGMA table_info(entries)`)
	if err != nil {
		return false, err
	}
	has := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.RawBytes
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return false, err
		}
		if name == column {
			has = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, err
	}
	_ = rows.Close()
	return has, nil
}

// Close 关闭索引库。
func (db *DB) Close() error { return db.sql.Close() }

// Count 返回已索引的条目数。
func (db *DB) Count() (int, error) {
	var n int
	err := db.sql.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&n)
	return n, err
}

// searchableCount 返回可检索条目数（排除 draft/archived——它们不进检索结果），
// 供关键词准入 floor 按库规模缩放；Count 是全库口径（ok index 统计展示用）。
func (db *DB) searchableCount() (int, error) {
	var n int
	err := db.sql.QueryRow(`SELECT COUNT(*) FROM entries WHERE draft = 0 AND archived = 0`).Scan(&n)
	return n, err
}

// SetMeta 写 kb 级元数据（embedding 模型身份等）。
func (db *DB) SetMeta(key, value string) error {
	_, err := db.sql.Exec(
		`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value)
	return err
}

// GetMeta 读元数据；不存在返回 ("", nil)。
func (db *DB) GetMeta(key string) (string, error) {
	var v string
	err := db.sql.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// EmbeddingMeta 返回建索引的模型身份与维度；未记录返回 ("", 0, nil)。
func (db *DB) EmbeddingMeta() (string, int, error) {
	model, err := db.GetMeta("embedding_model")
	if err != nil {
		return "", 0, err
	}
	ds, err := db.GetMeta("embedding_dim")
	if err != nil {
		return "", 0, err
	}
	dim := 0
	if ds != "" {
		dim, _ = strconv.Atoi(ds)
	}
	return model, dim, nil
}

// HasVectors 报告 vectors 表是否非空（历史向量无身份记录判定用）。
func (db *DB) HasVectors() (bool, error) {
	var n int
	err := db.sql.QueryRow(`SELECT COUNT(*) FROM vectors LIMIT 1`).Scan(&n)
	return n > 0, err
}

// ClearVectors 清空向量表并复位 embedding 身份 meta（模型切换后的全量重建前置）。
func (db *DB) ClearVectors() error {
	if _, err := db.sql.Exec(`DELETE FROM vectors`); err != nil {
		return err
	}
	_, err := db.sql.Exec(`DELETE FROM meta WHERE key IN ('embedding_model','embedding_dim')`)
	return err
}

// legacyVectors 是旧版 vectors.json 的格式（v1.2 embed.VectorSet），仅用于迁移导入。
type legacyVectors struct {
	Vectors map[string]struct {
		Vector []float32 `json:"vector"`
	} `json:"vectors"`
}

// migrateVectorsJSON 导入旧版 vectors.json。
func (db *DB) migrateVectorsJSON(dbPath string) error {
	vj := filepath.Join(filepath.Dir(dbPath), "vectors.json")
	if _, err := os.Stat(vj); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var n int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM vectors`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	data, err := os.ReadFile(vj)
	if err != nil {
		return err
	}
	var vs legacyVectors
	if err := json.Unmarshal(data, &vs); err != nil {
		// 损坏的 vectors.json（旧版 O_TRUNC 直写可能留下半截 JSON）不应让 Open
		// 永久失败：改名隔离后按无向量继续——向量可由条目文件重建
		if rerr := os.Rename(vj, vj+".bad"); rerr != nil {
			fmt.Fprintf(os.Stderr, "openknowledge: vectors.json 损坏（%v）且隔离失败: %v\n", err, rerr)
			return nil
		}
		fmt.Fprintf(os.Stderr, "openknowledge: vectors.json 损坏（%v），已改名为 vectors.json.bad，向量将在下次同步时重建\n", err)
		return nil
	}
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	for name, ev := range vs.Vectors {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO vectors(filename,dim,blob) VALUES(?,?,?)`,
			name, len(ev.Vector), encodeVector(ev.Vector)); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return os.Rename(vj, vj+".bak")
}

// encodeVector 将 float32 向量编码为小端字节 blob（位级精确往返）。
func encodeVector(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// decodeVector 是 encodeVector 的逆操作。
func decodeVector(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// AllVectors 读出 vectors 表全部向量（filename → 解码后的向量），供图谱语义边等
// 全量配对计算使用；无向量的条目不在 map 里。
func (db *DB) AllVectors() (map[string][]float32, error) {
	rows, err := db.sql.Query(`SELECT filename, blob FROM vectors`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]float32{}
	for rows.Next() {
		var name string
		var blob []byte
		if err := rows.Scan(&name, &blob); err != nil {
			return nil, err
		}
		out[name] = decodeVector(blob)
	}
	return out, rows.Err()
}
