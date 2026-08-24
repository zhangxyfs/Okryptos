package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// failAfterEmbedder 前 failFrom-1 次批量调用成功、之后返回错误，
// 模拟 embedding 服务在同步中途故障。identity 非空时模拟真实 client
// 的模型身份（触发 Sync 的模型身份闸）。
type failAfterEmbedder struct {
	calls    int
	failFrom int
	identity string
}

func (f *failAfterEmbedder) ModelIdentity() string { return f.identity }

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

// 跨批部分失败：40 条分两批（32+8），第二批失败时第一批已提交向量被回滚
// （见 TestSyncEmbeddingMidFailureDoesNotStall：不回滚会让下轮被判
// embedBlocked 永久停摆）、entries 全部入库；重试补齐全部 40 条。
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
	if n := vectorCount(t, db); n != 0 {
		t.Fatalf("失败轮已提交的第一批向量应被回滚: vectors=%d", n)
	}
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	if n := vectorCount(t, db); n != 40 {
		t.Fatalf("重试后向量应补齐: vectors=%d", n)
	}
}

// M-07 回归：真实 client（模型身份非空）首次建库分批中途失败——若不清掉
// 本轮已写向量，会留下"meta 未写 + vectors 有部分行"的状态，下一轮同步
// 命中模型身份闸的"meta 空 + HasVectors"分支被判 embedBlocked，向量写入
// 永久静默停摆（须手动 ok index 重建）。失败路径回滚本轮向量后，下轮应
// 干净重试至全量，meta 正常写入。
func TestSyncEmbeddingMidFailureDoesNotStall(t *testing.T) {
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

	bad := &failAfterEmbedder{failFrom: 2, identity: "m1"}
	if err := db.Sync(kdir, bad); err == nil {
		t.Fatal("第二批 embedding 失败应返回错误")
	}
	if n := vectorCount(t, db); n != 0 {
		t.Fatalf("失败轮应回滚本轮向量（否则下轮被判 embedBlocked 停摆）: vectors=%d", n)
	}
	// 下一轮：同一身份的可用 client 同步，不应被身份闸阻断
	if err := db.Sync(kdir, &batchFake{identity: "m1"}); err != nil {
		t.Fatal(err)
	}
	if n := vectorCount(t, db); n != 40 {
		t.Fatalf("重试后向量应补齐（停摆则恒为 0）: vectors=%d", n)
	}
	m, _, err := db.EmbeddingMeta()
	if err != nil {
		t.Fatal(err)
	}
	if m != "m1" {
		t.Fatalf("重试成功后 meta 应写入模型身份: model=%q", m)
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

// meta 末写失败必须回滚本轮已提交向量（R3 D-01）：否则库停留在
// "meta 空 + vectors 有行"，下一轮 Sync 被模型身份闸判 embedBlocked，
// 向量写入永久静默停摆。注入：DROP meta 表（身份闸读 meta 失败静默跳过，
// 不影响前序路径，末尾 SetMeta 必失败）。
func TestSyncMetaFailureRollsBackVectors(t *testing.T) {
	db, kdir := setupDB(t)
	if _, err := db.sql.Exec(`DROP TABLE meta`); err != nil {
		t.Fatal(err)
	}
	if err := db.Sync(kdir, &failAfterEmbedder{failFrom: 99, identity: "m-test"}); err == nil {
		t.Fatal("meta 写失败应返回错误")
	}
	if n := vectorCount(t, db); n != 0 {
		t.Fatalf("meta 写失败应回滚本轮向量: vectors=%d", n)
	}
}
