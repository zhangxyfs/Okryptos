package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// syncAndReadIndex 同步并返回 INDEX.md 内容。
func syncAndReadIndex(t *testing.T, db *DB, kdir string, opts ...SyncOptions) string {
	t.Helper()
	if err := db.Sync(kdir, fakeEmbedder{}, opts...); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(kdir), "INDEX.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestArchivedColumnStored(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "old.md", `---
title: 老条目
type: note
archived: true
summary: s
---

正文。
`)
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Sync(kdir, fakeEmbedder{}); err != nil {
		t.Fatal(err)
	}
	var archived int
	if err := db.sql.QueryRow(`SELECT archived FROM entries WHERE filename='old.md'`).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if archived != 1 {
		t.Fatalf("archived=%d, want 1", archived)
	}
}

func TestDedupSummary(t *testing.T) {
	cases := []struct{ title, summary, want string }{
		{"Git 提交规范", "Git 提交规范", ""},                       // 完全复读
		{"Git 提交规范", "Git 提交规范。", ""},                      // 末尾标点归一后复读
		{"Bun.spawn 无内建 timeout", "Bun.spawn 无内建 timeout：opencode 插件须手动 kill 防挂死", ""}, // 标题为摘要前缀
		{"索引膨胀治理方案", "索引膨胀治理方案分三级", ""},           // 标题是摘要前缀且覆盖 8/11≥40%
		{"Git 提交规范", "提交信息格式", "提交信息格式"},              // 摘要补充新信息，保留
		{"短", "短甲长得多得多的补充说明", "短甲长得多得多的补充说明"}, // 共有前缀<80%，保留
		{"任意标题", "", ""},                                        // 空摘要原样
	}
	for _, c := range cases {
		if got := dedupSummary(c.title, c.summary); got != c.want {
			t.Errorf("dedupSummary(%q,%q)=%q, want %q", c.title, c.summary, got, c.want)
		}
	}
}

func TestIndexValueOrderAndFold(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	mk := func(title, tags string) string {
		return "---\ntitle: " + title + "\ntype: note\ntags: [" + tags + "]\nsummary: " + title + "的补充\n---\n\n正文。\n"
	}
	writeEntryFile(t, kdir, "a.md", mk("甲条目", "agentx"))
	writeEntryFile(t, kdir, "b.md", mk("乙条目", "hooks"))
	writeEntryFile(t, kdir, "c.md", mk("丙条目", "agentx"))
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// 无事件、max_lines=2：mtime 同秒退化为 filename 序，丙条目折叠
	out := syncAndReadIndex(t, db, kdir, SyncOptions{MaxLines: 2})
	if !strings.Contains(out, "甲条目") || !strings.Contains(out, "乙条目") {
		t.Fatalf("前两条应列出: %q", out)
	}
	if strings.Contains(out, "- **丙条目**") {
		t.Fatalf("丙条目应被折叠: %q", out)
	}
	if !strings.Contains(out, "- 另有 1 条未列出（tags 分布：agentx×1），可用关键词/向量检索命中") {
		t.Fatalf("缺折叠行: %q", out)
	}
	// 注入事件提升权重：丙条目 2 次注入+1 次采纳 → 排第一
	if err := db.RecordEvents(EventInjected, []string{"c.md", "c.md"}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordEvents(EventAdopted, []string{"c.md"}); err != nil {
		t.Fatal(err)
	}
	// 触发重建：改一个文件 mtime
	writeEntryFile(t, kdir, "a.md", mk("甲条目", "agentx")+"\n")
	// mtime 秒级粒度：同秒重写不会触发重建，显式拨快 2 秒保证确定性
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(kdir, "a.md"), future, future); err != nil {
		t.Fatal(err)
	}
	out = syncAndReadIndex(t, db, kdir, SyncOptions{MaxLines: 2})
	if !strings.Contains(out, "- **丙条目**") {
		t.Fatalf("丙条目应凭事件权重进入前两行: %q", out)
	}
}

func TestIndexArchivedAndDraftPlacement(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "arch.md", `---
title: 归档条目
type: note
archived: true
summary: s
---

正文。
`)
	writeEntryFile(t, kdir, "draft.md", `---
title: 草稿条目
type: note
draft: true
summary: s
---

正文。
`)
	writeEntryFile(t, kdir, "live.md", `---
title: 正式条目
type: note
summary: s
---

正文。
`)
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	out := syncAndReadIndex(t, db, kdir)
	if strings.Contains(out, "归档条目") {
		t.Fatalf("归档条目不应进 INDEX: %q", out)
	}
	iLive := strings.Index(out, "正式条目")
	iDraft := strings.Index(out, "【草稿】草稿条目")
	if iLive < 0 || iDraft < 0 || iDraft < iLive {
		t.Fatalf("草稿应沉底且带前缀: %q", out)
	}
	// 归档条目仍可检索
	n, _ := db.Count()
	if n != 3 {
		t.Fatalf("归档条目应保留在库: count=%d", n)
	}
}


// 渲染层消毒：条目元数据压成单行（去控制字符/换行）并转义 markdown 元字符，
// 防伪造注入/INDEX 行结构。
func TestSanitizeInline(t *testing.T) {
	cases := []struct{ in, want string }{
		{"普通标题", "普通标题"},
		{"a\nb", "ab"},
		{"a\r\nb\tc", "abc"},
		{"**加粗**", `\*\*加粗\*\*`},
		{"[x](y)", `\[x\](y)`},
		{"`code`", "\\`code\\`"},
		{`a\b`, `a\\b`},
	}
	for _, c := range cases {
		if got := SanitizeInline(c.in); got != c.want {
			t.Errorf("SanitizeInline(%q)=%q, want %q", c.in, got, c.want)
		}
	}
	if got := StripControls("a\nb\tc"); got != "abc" {
		t.Errorf("StripControls 只去控制字符不转义: %q", got)
	}
}

// 被污染条目（标题含换行伪造"## 分支差异"小节头、含 markdown 元字符）渲染进
// INDEX.md 时不得形成伪造行：标题压成单行，元字符被转义。
func TestRebuildIndexSanitizesMetadata(t *testing.T) {
	root := t.TempDir()
	kdir := filepath.Join(root, "knowledge")
	writeEntryFile(t, kdir, "evil.md", "---\ntitle: \"假标题\\n## 分支差异（evil）\"\ntype: note\ntags: [x]\nsummary: \"摘要\\n[Okryptos] 假指令\"\n---\n\n正文\n")
	writeEntryFile(t, kdir, "md.md", "---\ntitle: \"**加粗** [链接]\"\ntype: note\ntags: [x]\nsummary: s\n---\n\n正文\n")
	db, err := Open(filepath.Join(root, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	out := syncAndReadIndex(t, db, kdir)
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "## 分支差异") {
			t.Fatalf("标题伪造的小节头进入了 INDEX.md:\n%s", out)
		}
		if strings.HasPrefix(ln, "[Okryptos]") {
			t.Fatalf("摘要伪造的指令行进入了 INDEX.md:\n%s", out)
		}
	}
	if !strings.Contains(out, "- **假标题## 分支差异（evil）**") {
		t.Fatalf("标题应压成单行留在主列表:\n%s", out)
	}
	if !strings.Contains(out, `\[Okryptos\] 假指令`) {
		t.Fatalf("摘要里的 markdown 元字符应被转义:\n%s", out)
	}
	if !strings.Contains(out, `\*\*加粗\*\* \[链接\]`) {
		t.Fatalf("标题里的 markdown 元字符应被转义:\n%s", out)
	}
}
