package index

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"okryptos/internal/config"
	"okryptos/internal/retrieve"
)

const draftEntry = `---
title: 待审草稿
type: note
draft: true
summary: 未批准的草稿
---

git 草稿正文。
`

// 草稿不参与任何检索通道（FTS / 向量 / mandatory 注入）。
func TestDraftExcludedFromRetrieval(t *testing.T) {
	db, kdir := setupDB(t)
	writeEntryFile(t, kdir, "draft.md", draftEntry)
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 10}
	// 关键词通道：草稿含 "git" 词元但不应命中
	hits, err := db.Query(retrieve.Terms("git"), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Title == "待审草稿" {
			t.Fatal("draft must be excluded from keyword query")
		}
	}
	// 语义通道：queryVec=[1,0] 与草稿向量同向也不应命中
	hits, err = db.Query(nil, []float32{1, 0}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Title == "待审草稿" {
			t.Fatal("draft must be excluded from vector query")
		}
	}
	// mandatory 通道兜底：即使库里出现 mandatory+draft 组合也不注入
	if _, err := db.sql.Exec(`UPDATE entries SET mandatory=1 WHERE filename='draft.md'`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Mandatory()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range rows {
		if h.Title == "待审草稿" {
			t.Fatal("draft must be excluded from mandatory")
		}
	}
}

// INDEX.md 中草稿行带【草稿】前缀，普通行不带。
func TestIndexMarkdownDraftPrefix(t *testing.T) {
	db, kdir := setupDB(t)
	writeEntryFile(t, kdir, "draft.md", draftEntry)
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(kdir), "INDEX.md"))
	if err != nil {
		t.Fatal(err)
	}
	var draftLine string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.Contains(l, "待审草稿") {
			draftLine = l
		}
		if strings.Contains(l, "Git 提交规范") && strings.Contains(l, "【草稿】") {
			t.Fatalf("non-draft entry must not have draft prefix: %q", l)
		}
	}
	if !strings.Contains(draftLine, "【草稿】") {
		t.Fatalf("draft line should carry 【草稿】 prefix, got %q", draftLine)
	}
}

// 旧库（entries 无 draft 列）Open 时自动迁移，之后可正常同步与检索。
func TestDraftColumnMigration(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "git.md", e1)
	dbPath := filepath.Join(root, "kb.db")
	// 手工建旧版 schema（无 draft 列）并插入一行陈旧数据
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	oldSchema := `
CREATE TABLE entries(
  filename TEXT PRIMARY KEY,
  title TEXT NOT NULL, type TEXT NOT NULL,
  tags TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  mandatory INTEGER NOT NULL DEFAULT 0,
  mtime INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE vectors(filename TEXT PRIMARY KEY, dim INTEGER NOT NULL, blob BLOB NOT NULL);
CREATE VIRTUAL TABLE entries_fts USING fts5(title, tags, summary, body, filename UNINDEXED);
`
	if _, err := raw.Exec(oldSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO entries(filename,title,type,mtime) VALUES('stale.md','旧行','note',1)`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open old-schema db: %v", err)
	}
	defer db.Close()
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	hits, err := db.Query(retrieve.Terms("git 提交"), nil, config.Retrieve{Alpha: 1, Beta: 1, TopN: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Title != "Git 提交规范" {
		t.Fatalf("query after migration wrong: %+v", hits)
	}
}

// 旧库（有 draft/archived、无 size 列）首开时补 size 列；存量行 size=0 与盘上
// 实际大小不符，首轮 Sync 全部重读一次后 size 入库（mtime+size 双判生效）。
// 重复 Open 幂等。
func TestSizeColumnMigration(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "git.md", e1)
	dbPath := filepath.Join(root, "kb.db")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	oldSchema := `
CREATE TABLE entries(
  filename TEXT PRIMARY KEY,
  title TEXT NOT NULL, type TEXT NOT NULL,
  tags TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  mandatory INTEGER NOT NULL DEFAULT 0,
  draft INTEGER NOT NULL DEFAULT 0,
  archived INTEGER NOT NULL DEFAULT 0,
  mtime INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE vectors(filename TEXT PRIMARY KEY, dim INTEGER NOT NULL, blob BLOB NOT NULL);
CREATE VIRTUAL TABLE entries_fts USING fts5(title, tags, summary, body, filename UNINDEXED);
`
	if _, err := raw.Exec(oldSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO entries(filename,title,type,mtime) VALUES('stale.md','旧行','note',1)`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open old-schema db: %v", err)
	}
	defer db.Close()
	if has, err := db.hasColumn("size"); err != nil || !has {
		t.Fatalf("size 列应被迁移补上: has=%v err=%v", has, err)
	}
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(kdir, "git.md"))
	if err != nil {
		t.Fatal(err)
	}
	var size int64
	if err := db.sql.QueryRow(`SELECT size FROM entries WHERE filename='git.md'`).Scan(&size); err != nil {
		t.Fatal(err)
	}
	if size != fi.Size() {
		t.Fatalf("size 应入库: got %d, want %d", size, fi.Size())
	}
	// 幂等：再次 Open 不再 ALTER
	db2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_ = db2.Close()
}
