package index

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"openknowledge/internal/config"
)

// M-08 回归：语义通道两阶段查询——第一阶段只读 filename+向量算余弦准入，
// 第二阶段按 filename 回查正文。语义准入的命中必须携带与全量加载等价的完整
// 字段（body/tags/summary/mtime），且多次查询结果逐字节确定。
func TestQuerySemanticTwoPhaseBackfill(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	// fakeEmbedder：含 "git" 的文本向量 [1,0]，其余 [0,1]
	writeEntryFile(t, kdir, "sem.md",
		"---\ntitle: 语义条目\ntype: pitfall\ntags: [git, 坑]\nsummary: 语义摘要\n---\n\ngit 语义通道正文唯一串。\n")
	writeEntryFile(t, kdir, "other.md",
		"---\ntitle: 无关条目\ntype: note\ntags: [t]\nsummary: 无关\n---\n\n构建工具闲聊。\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Retrieve{Alpha: 1, Beta: 1, TopN: 5, Fusion: "weighted"}

	// 纯语义查询（无关键词通道）：queryVec=[1,0] 只与 sem.md 正余弦
	hits, err := db.Query(nil, []float32{1, 0}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Filename != "sem.md" {
		t.Fatalf("语义通道应只准入 sem.md: %+v", hits)
	}
	h := hits[0]
	// 第二阶段回查必须补齐全部注入字段（全量加载等价）
	if h.Body != "git 语义通道正文唯一串。" {
		t.Fatalf("body 回查缺失: %q", h.Body)
	}
	if h.Title != "语义条目" || h.Type != "pitfall" || h.Summary != "语义摘要" {
		t.Fatalf("元数据回查缺失: %+v", h)
	}
	if !reflect.DeepEqual(h.Tags, []string{"git", "坑"}) {
		t.Fatalf("tags 回查缺失: %v", h.Tags)
	}
	if h.Mtime <= 0 {
		t.Fatalf("mtime 回查缺失: %v", h.Mtime)
	}
	if h.Score != 1 { // Beta×cos([1,0],[1,0])=1
		t.Fatalf("score 应为 Beta×cos=1: %v", h.Score)
	}

	// 反向 queryVec：只有 other.md 准入
	hits, err = db.Query(nil, []float32{0, 1}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Filename != "other.md" || hits[0].Body != "构建工具闲聊。" {
		t.Fatalf("反向语义查询应准入 other.md 且带正文: %+v", hits)
	}

	// 等价性/确定性：同一查询反复执行，结果逐字节一致（两阶段不得引入顺序漂移）
	first, _, err := db.QueryEx(nil, []float32{1, 0}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		got, _, err := db.QueryEx(nil, []float32{1, 0}, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("第 %d 次查询结果漂移: %+v vs %+v", i, got, first)
		}
	}
}

// M-09 回归：recency 排序比较器含 Filename 决胜后，同名同分条目在 map 随机
// 输入序 + sort.Slice 不稳定下输出确定。构造：p.md/q.md 同分同名打平，
// p.md 陈旧被降权后是否上榜 RecencyShifted 取决于打平决胜——缺 Filename
// 决胜时该结果随 map 遍历序漂移。
func TestApplyRecencyDeterministicTie(t *testing.T) {
	const now int64 = 1_800_000_000
	cfg := config.RetrieveRecency{Enabled: true, Floor: 0.85, Windows: config.RecencyWindows{
		Note: []int{60, 180},
	}}
	want := []string{"p.md×0.85"}
	for i := 0; i < 200; i++ {
		hits := map[string]*Hit{
			// p/q 同分同名（打平），p 陈旧 ×0.85=0.425 跌到 r 之后；
			// r 同样陈旧但基数低，名次不变
			"p.md": {Filename: "p.md", Title: "同名条目", Type: "note", Score: 0.5, Mtime: now - 400*daySec},
			"q.md": {Filename: "q.md", Title: "同名条目", Type: "note", Score: 0.5, Mtime: now},
			"r.md": {Filename: "r.md", Title: "同名条目", Type: "note", Score: 0.45, Mtime: now - 400*daySec},
		}
		got := applyRecency(hits, now, cfg)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("第 %d 轮结果漂移: got %v, want %v", i, got, want)
		}
	}
}

// L-12 回归：rebuildIndex 主列表价值排序的反馈统计窗口走 SyncOptions
// （与查询侧 retrieve.feedback.window_days 同口径），不再硬编码 30 天。
// 40 天前的事件在默认 30 天窗口下不计权重，FeedbackWindowDays=60 时计入。
func TestRebuildIndexFeedbackWindowFromOptions(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "a.md",
		"---\ntitle: 甲窗口\ntype: note\ntags: [t]\nsummary: s\n---\n\n正文甲。\n")
	writeEntryFile(t, kdir, "b.md",
		"---\ntitle: 乙窗口\ntype: note\ntags: [t]\nsummary: s\n---\n\n正文乙。\n")
	// 同秒 mtime：权重为零时退化为 filename 升序（a.md 在前）
	past := time.Now().Add(-time.Hour)
	for _, n := range []string{"a.md", "b.md"} {
		if err := os.Chtimes(filepath.Join(kdir, n), past, past); err != nil {
			t.Fatal(err)
		}
	}
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// b.md 40 天前 5 次注入（RecordEvents 只有 now，直接 SQL 造旧事件）
	for i := 0; i < 5; i++ {
		if _, err := db.sql.Exec(`INSERT INTO entry_events(filename, kind, ts) VALUES('b.md', 'injected', ?)`,
			time.Now().Unix()-40*86400); err != nil {
			t.Fatal(err)
		}
	}
	order := func(out string) int {
		ia, ib := strings.Index(out, "- **甲窗口**"), strings.Index(out, "- **乙窗口**")
		if ia < 0 || ib < 0 {
			t.Fatalf("主列表缺条目: %q", out)
		}
		if ia < ib {
			return 0 // a 在前
		}
		return 1
	}
	// 默认窗口 30 天：40 天前事件不计入，a.md 凭 filename 序在前
	if got := order(syncAndReadIndex(t, db, kdir)); got != 0 {
		t.Fatal("默认 30 天窗口不应统计 40 天前事件，a.md 应在前")
	}
	// 触发重建并改走 60 天窗口：b.md 权重 5 登顶
	writeEntryFile(t, kdir, "a.md",
		"---\ntitle: 甲窗口\ntype: note\ntags: [t]\nsummary: s\n---\n\n正文甲。\n\n")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(kdir, "a.md"), future, future); err != nil {
		t.Fatal(err)
	}
	if got := order(syncAndReadIndex(t, db, kdir, SyncOptions{FeedbackWindowDays: 60})); got != 1 {
		t.Fatal("60 天窗口应统计 40 天前事件，b.md 应凭权重在前")
	}
}
