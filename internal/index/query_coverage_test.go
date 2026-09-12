package index

import (
	"path/filepath"
	"testing"

	"okryptos/internal/config"
	"okryptos/internal/retrieve"
)

// coverageFixture 复刻"还是黑底的"事故条目：正文含虚词"还是"与跨词界假词"底的"。
func coverageFixture(t *testing.T) *DB {
	t.Helper()
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "changelog.md",
		"---\ntitle: 构建双路径漂移\ntype: pitfall\ntags: [构建]\nsummary: iss 打 dist 暂存区\n---\n\ndist/changelogs/ 还是陈旧内容。更彻底的做法是收敛到单一构建入口。\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Sync(kdir, nil); err != nil {
		t.Fatal(err)
	}
	return db
}

// TestCoverageRejectsStopwordHit 事故复现：虚词命中不再准入；关闭 coverage 恢复旧行为（对照组）。
func TestCoverageRejectsStopwordHit(t *testing.T) {
	db := coverageFixture(t)
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, MinScore: 0.5,
		Coverage: config.RetrieveCoverage{Enabled: true, MinRatio: 0.5}}
	hits, info, err := db.QueryEx(retrieve.Terms("还是黑底的"), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("虚词命中的无关条目应被覆盖度拒绝: %+v", hits)
	}
	if len(info.CoverageRejected) != 1 || info.CoverageRejected[0] != "changelog.md" {
		t.Fatalf("被拒条目应记入 CoverageRejected: %+v", info)
	}
	// 对照组：关闭 coverage 时同查询应命中（证明测试有判别力）
	cfgOff := cfg
	cfgOff.Coverage.Enabled = false
	hitsOff, err := db.Query(retrieve.Terms("还是黑底的"), nil, cfgOff)
	if err != nil {
		t.Fatal(err)
	}
	if len(hitsOff) != 1 {
		t.Fatalf("关闭 coverage 应恢复旧命中（对照）: %+v", hitsOff)
	}
}

// TestCoverageAdmitsRealTerms 实词查询不受影响：两个有效词元都命中的条目照常准入。
func TestCoverageAdmitsRealTerms(t *testing.T) {
	db := coverageFixture(t)
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, MinScore: 0.5,
		Coverage: config.RetrieveCoverage{Enabled: true, MinRatio: 0.5}}
	hits, err := db.Query(retrieve.Terms("构建漂移"), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Filename != "changelog.md" {
		t.Fatalf("实词查询应照常命中: %+v", hits)
	}
}

// TestKeywordGatedEmptyEffective 纯虚词查询：关键词通道整体跳过（KeywordGated 置位）。
func TestKeywordGatedEmptyEffective(t *testing.T) {
	db := coverageFixture(t)
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, MinScore: 0.5,
		Coverage: config.RetrieveCoverage{Enabled: true, MinRatio: 0.5}}
	hits, info, err := db.QueryEx(retrieve.Terms("还是就是"), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 || !info.KeywordGated {
		t.Fatalf("纯虚词查询应跳过关键词通道: hits=%+v KeywordGated=%v", hits, info.KeywordGated)
	}
}
