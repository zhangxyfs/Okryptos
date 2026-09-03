package index

import (
	"path/filepath"
	"strings"
	"testing"

	"okryptos/internal/config"
	"okryptos/internal/retrieve"
)

// 强负余弦不得否决已过关键词门槛的命中：准入按通道独立判定，语义分只影响排序。
// queryVec 与命中条目向量反向（cos=-1）时旧实现总分为负、条目被静默丢弃。
func TestQueryNegativeCosKeepsKeywordHit(t *testing.T) {
	db, kdir := setupDB(t)
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 3}
	hits, err := db.Query(retrieve.Terms("git 提交"), []float32{-1, 0}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Title != "Git 提交规范" {
		t.Fatalf("keyword-admitted hit must survive strong negative cosine: %+v", hits)
	}
}

// top_n 截断必须发生在分支过滤之后：其他分支的条目占名额时，本分支条目应补位，
// 而不是被无谓挤掉（QueryEx 截断后才过滤的老语义会返回空集）。
func TestQueryExBranchFiltersBeforeTopN(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "main.md",
		"---\ntitle: 主分支条目\ntype: note\ntags: [note]\ndraft: false\n---\n\n构建 构建 相关内容。\n")
	writeEntryFile(t, kdir, "feat.md",
		"---\ntitle: 其他分支条目\ntype: note\ntags: [branch:feat]\ndraft: false\n---\n\n构建 构建 构建 构建 构建构建构建。\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 1}
	// 语义通道关闭，纯关键词：feat 条目词频更高、BM25 更强，独占 top 1
	hits, _, err := db.QueryEx(retrieve.Terms("构建"), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Title != "其他分支条目" {
		t.Fatalf("top1 should be the higher-BM25 cross-branch entry: %+v", hits)
	}
	// 同一查询经分支裁剪：feat 条目被过滤后主分支条目补位，而不是返回空
	hits, _, err = db.QueryExBranch(retrieve.Terms("构建"), nil, cfg, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Title != "主分支条目" {
		t.Fatalf("branch filter should backfill from the same branch, got %+v", hits)
	}
	if strings.Contains(hits[0].Title, "其他分支") {
		t.Fatalf("cross-branch entry must be dropped: %+v", hits)
	}
	// 未知分支（branch 为空）不过滤
	hits, _, err = db.QueryExBranch(retrieve.Terms("构建"), nil, cfg, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Title != "其他分支条目" {
		t.Fatalf("empty branch must not filter: %+v", hits)
	}
}


// 关键词准入 floor 按可检索条目数（排除 draft/archived）缩放：库里有大量草稿时，
// 若按全库 Count 计 n≥30，floor 取满 minScore 会误杀低分真实命中；按可检索数
// （n=5<10）floor 关闭、命中即注入。
func TestQueryFloorExcludesDraftAndArchived(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "target.md",
		"---\ntitle: 目标条目\ntype: note\ntags: [t]\nsummary: s\n---\n\n冷僻词甲 正文。\n")
	for i := 0; i < 4; i++ {
		writeEntryFile(t, kdir, "live"+string(rune('a'+i))+".md",
			"---\ntitle: 正式条目"+string(rune('a'+i))+"\ntype: note\ntags: [t]\nsummary: s\n---\n\n无关正文。\n")
	}
	for i := 0; i < 25; i++ {
		writeEntryFile(t, kdir, "draft"+string(rune('a'+i%26))+string(rune('a'+i/26))+".md",
			"---\ntitle: 草稿条目"+string(rune('a'+i%26))+string(rune('a'+i/26))+"\ntype: note\ndraft: true\ntags: [t]\nsummary: s\n---\n\n草稿正文。\n")
	}
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.Count(); n != 30 {
		t.Fatalf("count=%d, want 30", n)
	}
	if n, _ := db.searchableCount(); n != 5 {
		t.Fatalf("searchableCount=%d, want 5（草稿不计入）", n)
	}
	// minScore 拉满：n=30 口径下 floor=0.9 会滤掉单文档弱命中；n=5 口径关闭阈值
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, MinScore: 0.9}
	hits, err := db.Query(retrieve.Terms("冷僻词甲"), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Filename != "target.md" {
		t.Fatalf("floor 应按可检索条目数关闭，目标条目应命中: %+v", hits)
	}
}

// 同名同分条目按 filename 升序决胜（确定性排序，与 feedback.go 同款）。
func TestQueryTieBreakByFilename(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	body := "---\ntitle: 同名条目\ntype: note\ntags: [t]\nsummary: s\n---\n\n冷僻词乙 相同正文。\n"
	writeEntryFile(t, kdir, "b.md", body)
	writeEntryFile(t, kdir, "a.md", body)
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	// weighted 模式且内容完全相同 → 两分同分、标题同名，进入 filename 决胜
	cfg := config.Retrieve{Alpha: 1, Beta: 0, TopN: 5, Fusion: "weighted"}
	hits, err := db.Query(retrieve.Terms("冷僻词乙"), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].Filename != "a.md" || hits[1].Filename != "b.md" {
		t.Fatalf("同名同分应按 filename 升序: %+v", hits)
	}
}
