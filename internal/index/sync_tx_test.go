package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// failAfterEmbedder 前 failFrom-1 次批量调用成功、之后返回错误，
// 模拟 embedding 服务在同步中途故障。
type failAfterEmbedder struct {
	calls    int
	failFrom int
}

func (f *failAfterEmbedder) ModelIdentity() string { return "" }

func (f *failAfterEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return fakeEmbedder{}.EmbedQuery(ctx, text)
}

func (f *failAfterEmbedder) EmbedDocument(ctx context.Context, text string) ([]float32, error) {
	return fakeEmbedder{}.EmbedDocument(ctx, text)
}

func (f *failAfterEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	f.calls++
	if f.calls >= f.failFrom {
		return nil, fmt.Errorf("embedding service down")
	}
	return fakeEmbedder{}.EmbedDocuments(ctx, texts)
}

func vectorCount(t *testing.T, db *DB) int {
	t.Helper()
	var n int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM vectors`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// embedding 失败不再回滚 entries：写事务只覆盖 entries/fts，向量写在其后的
// 小事务里。失败轮返回错误但条目已入库可检索；下一轮 Sync 由"未变化缺向量
// 补齐"路径补上向量并重建 INDEX.md。
func TestSyncEmbeddingFailureKeepsEntries(t *testing.T) {
	db, kdir := setupDB(t)
	bad := &failAfterEmbedder{failFrom: 1}
	if err := db.Sync(kdir, bad); err == nil {
		t.Fatal("embedding 失败应返回错误")
	}
	if n, _ := db.Count(); n != 3 {
		t.Fatalf("entries 必须保留（不被 embedding 失败回滚）: count=%d", n)
	}
	if n := vectorCount(t, db); n != 0 {
		t.Fatalf("失败批不应有向量写入: vectors=%d", n)
	}
	// 重试：条目未变化、缺向量 → 补齐路径重试成功，INDEX.md 一并重建
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	if n := vectorCount(t, db); n != 3 {
		t.Fatalf("重试后向量应补齐: vectors=%d", n)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(kdir), "INDEX.md")); err != nil {
		t.Fatalf("重试后 INDEX.md 应重建: %v", err)
	}
}

// 跨批部分失败：40 条分两批（32+8），第二批失败时第一批向量保留、
// entries 全部入库；重试只补缺失的 8 条。
func TestSyncEmbeddingPartialBatchFailure(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	for i := 0; i < 40; i++ {
		writeEntryFile(t, kdir, fmt.Sprintf("e%02d.md", i),
			fmt.Sprintf("---\ntitle: 条目%02d\ntype: note\ntags: [t]\nsummary: s%02d\n---\n\n正文 %02d。\n", i, i, i))
	}
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	bad := &failAfterEmbedder{failFrom: 2} // 第一批成功、第二批失败
	if err := db.Sync(kdir, bad); err == nil {
		t.Fatal("第二批 embedding 失败应返回错误")
	}
	if n, _ := db.Count(); n != 40 {
		t.Fatalf("entries 应全部入库: count=%d", n)
	}
	if n := vectorCount(t, db); n != 32 {
		t.Fatalf("第一批 32 条向量应已提交: vectors=%d", n)
	}
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	if n := vectorCount(t, db); n != 40 {
		t.Fatalf("重试后向量应补齐: vectors=%d", n)
	}
}

// mtime+size 双判：外部编辑器同秒重写（mtime 复原但长度不同）必须判为变化，
// 否则 FTS/向量/INDEX 静默停留在旧内容。
func TestSyncDetectsSameMtimeDifferentSize(t *testing.T) {
	db, kdir := setupDB(t)
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(kdir, "git.md")
	fi, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	updated := "---\ntitle: Git 分支策略新版\ntype: note\ntags: [git]\nsummary: 分支模型\n---\n\n主干开发。\n"
	if len(updated) == int(fi.Size()) {
		updated += "补一字。\n"
	}
	if err := os.WriteFile(victim, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	// 复原 mtime：单靠秒级 mtime 的旧判据会漏掉这次写入
	if err := os.Chtimes(victim, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	var title string
	if err := db.sql.QueryRow(`SELECT title FROM entries WHERE filename='git.md'`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "Git 分支策略新版" {
		t.Fatalf("同 mtime 不同 size 的重写被判未变化: title=%q", title)
	}
}
