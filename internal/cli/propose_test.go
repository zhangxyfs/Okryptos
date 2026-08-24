package cli

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"openknowledge/internal/entry"
)

// setupProject 在临时 OK_HOME 下初始化 demo 项目并 chdir 进去。
func setupProject(t *testing.T) (home, kb string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("OK_HOME", home)
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, "kimi")) // 隔离真实 kimi 配置，Init 会写 hooks
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("OK_ZCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-zcode"))
	t.Setenv("OK_REASONIX_HOME", filepath.Join(t.TempDir(), "nonexistent-reasonix"))
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent-dsh"))
	t.Setenv("OK_OPENCODE_HOME", filepath.Join(t.TempDir(), "nonexistent-opencode"))
	t.Setenv("OK_CLAUDE_HOME", filepath.Join(t.TempDir(), "nonexistent-claude"))
	t.Setenv("OK_CODEPILOT_HOME", filepath.Join(t.TempDir(), "nonexistent-codepilot"))
	t.Setenv("OK_CODEX_HOME", filepath.Join(t.TempDir(), "nonexistent-codex"))
	t.Setenv("OK_QODER_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder"))
	t.Setenv("OK_QODER_IDE_HOME", filepath.Join(t.TempDir(), "nonexistent-qoder-ide"))
	t.Setenv("OPENAI_API_KEY", "") // 防止真实网络调用，保证测试离线
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, proj)
	var out, errBuf bytes.Buffer
	if code := Init([]string{"demo"}, &out, &errBuf); code != 0 {
		t.Fatalf("init code=%d err=%q", code, errBuf.String())
	}
	return home, filepath.Join(home, "projects", "demo")
}

func vectorCount(t *testing.T, kb string) int {
	t.Helper()
	raw, err := sql.Open("sqlite", filepath.Join(kb, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var n int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM vectors`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestProposeWritesDraftWithoutVectors(t *testing.T) {
	_, kb := setupProject(t)
	var out, errBuf bytes.Buffer
	code := Propose([]string{"--title", "候选规则", "--type", "rule", "--tags", "x", "--summary", "候选", "--body", "git 正文"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("propose code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "草稿") {
		t.Fatalf("output should mention draft, got %q", out.String())
	}
	entries, err := entry.Load(filepath.Join(kb, "knowledge"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries %+v err=%v", entries, err)
	}
	if e := entries[0]; !e.Draft || e.Mandatory {
		t.Fatalf("propose must write draft:true mandatory:false, got %+v", e)
	}
	// 草稿不同步向量
	if n := vectorCount(t, kb); n != 0 {
		t.Fatalf("propose must not compute vectors, got %d", n)
	}
	// INDEX.md 有草稿标记
	data, err := os.ReadFile(filepath.Join(kb, "INDEX.md"))
	if err != nil || !strings.Contains(string(data), "【草稿】") {
		t.Fatalf("INDEX should mark draft: %v %q", err, data)
	}
	// 检索排除草稿
	out.Reset()
	if code := Search([]string{"git"}, &out, &errBuf); code != 0 {
		t.Fatalf("search code=%d err=%q", code, errBuf.String())
	}
	if strings.Contains(out.String(), "候选规则") {
		t.Fatalf("draft must not appear in search, got %q", out.String())
	}
	// --body 与 --file 互斥
	if code := Propose([]string{"--title", "Y", "--body", "a", "--file", "b"}, &out, &errBuf); code == 0 {
		t.Fatal("--body + --file should fail")
	}
	// 重复 propose 同名条目 → 失败
	if code := Propose([]string{"--title", "候选规则", "--type", "rule"}, &out, &errBuf); code == 0 {
		t.Fatal("duplicate propose should fail")
	}
}

func TestApproveFlipsDraft(t *testing.T) {
	_, kb := setupProject(t)
	var out, errBuf bytes.Buffer
	if code := Propose([]string{"--title", "候选规则", "--type", "note", "--body", "git 正文"}, &out, &errBuf); code != 0 {
		t.Fatalf("propose code=%d err=%q", code, errBuf.String())
	}
	// approve 不存在的文件 → 失败
	if code := Approve([]string{"nope.md"}, &out, &errBuf); code == 0 {
		t.Fatal("approve missing file should fail")
	}
	out.Reset()
	errBuf.Reset()
	if code := Approve([]string{"候选规则.md"}, &out, &errBuf); code != 0 {
		t.Fatalf("approve code=%d err=%q", code, errBuf.String())
	}
	data, err := os.ReadFile(filepath.Join(kb, "knowledge", "候选规则.md"))
	if err != nil {
		t.Fatal(err)
	}
	e, err := entry.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if e.Draft {
		t.Fatal("approve should clear draft flag")
	}
	if e.Body != "git 正文" || e.Title != "候选规则" {
		t.Fatalf("approve must preserve other fields, got %+v", e)
	}
	// INDEX.md 不再有草稿标记
	idxData, err := os.ReadFile(filepath.Join(kb, "INDEX.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(idxData), "【草稿】") {
		t.Fatalf("INDEX should drop draft mark after approve: %q", idxData)
	}
	// 批准后检索能命中
	out.Reset()
	if code := Search([]string{"git"}, &out, &errBuf); code != 0 {
		t.Fatalf("search code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "候选规则") {
		t.Fatalf("approved entry should be searchable, got %q", out.String())
	}
	// 再次 approve → 失败（已不是草稿）
	if code := Approve([]string{"候选规则.md"}, &out, &errBuf); code == 0 {
		t.Fatal("re-approve non-draft should fail")
	}
}

func TestCapturePrintAndSet(t *testing.T) {
	_, kb := setupProject(t)
	var out, errBuf bytes.Buffer
	// 无参打印当前模式（默认 propose / turn_interval 3）
	if code := CaptureCmd(nil, &out, &errBuf); code != 0 {
		t.Fatalf("capture code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "propose") || !strings.Contains(out.String(), "3") {
		t.Fatalf("default capture print wrong: %q", out.String())
	}
	// 非法模式 → 失败
	if code := CaptureCmd([]string{"bogus"}, &out, &errBuf); code == 0 {
		t.Fatal("invalid mode should fail")
	}
	// 设置 auto
	out.Reset()
	if code := CaptureCmd([]string{"auto"}, &out, &errBuf); code != 0 {
		t.Fatalf("capture set code=%d err=%q", code, errBuf.String())
	}
	data, err := os.ReadFile(filepath.Join(kb, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[capture]") || !strings.Contains(string(data), `mode = "auto"`) {
		t.Fatalf("config should contain capture section: %q", data)
	}
	// 原有注释保留
	if !strings.Contains(string(data), "OpenKnowledge 项目知识库配置") {
		t.Fatalf("config comments should be preserved: %q", data)
	}
	// 打印反映新模式
	out.Reset()
	if code := CaptureCmd(nil, &out, &errBuf); code != 0 {
		t.Fatalf("capture print code=%d", code)
	}
	if !strings.Contains(out.String(), "auto") {
		t.Fatalf("capture should print auto after set: %q", out.String())
	}
	// 再设回 propose → 替换而非重复追加
	if code := CaptureCmd([]string{"propose"}, &out, &errBuf); code != 0 {
		t.Fatalf("capture set propose code=%d err=%q", code, errBuf.String())
	}
	data, err = os.ReadFile(filepath.Join(kb, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "[capture]") != 1 {
		t.Fatalf("capture section should not be duplicated: %q", data)
	}
}

func TestCaptureInterval(t *testing.T) {
	_, kb := setupProject(t)
	var out, errBuf bytes.Buffer
	// 非法 interval
	if code := CaptureCmd([]string{"interval", "0"}, &out, &errBuf); code == 0 {
		t.Fatal("interval 0 should fail")
	}
	if code := CaptureCmd([]string{"interval", "abc"}, &out, &errBuf); code == 0 {
		t.Fatal("non-numeric interval should fail")
	}
	// 设置 interval=10，模式保持
	if code := CaptureCmd([]string{"interval", "10"}, &out, &errBuf); code != 0 {
		t.Fatalf("capture interval code=%d err=%q", code, errBuf.String())
	}
	data, err := os.ReadFile(filepath.Join(kb, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "turn_interval = 10") || !strings.Contains(got, `mode = "propose"`) {
		t.Fatalf("config should have interval 10 with mode preserved: %q", got)
	}
	// 打印反映新间隔
	out.Reset()
	if code := CaptureCmd(nil, &out, &errBuf); code != 0 {
		t.Fatalf("capture print code=%d", code)
	}
	if !strings.Contains(out.String(), "10") {
		t.Fatalf("capture should print interval 10: %q", out.String())
	}
}

// M-13 回归：cwd 恰有同名文件时 approve 仍作用于库内草稿，且不改写 cwd 副本。
func TestApproveIgnoresSameNameFileInCwd(t *testing.T) {
	_, kb := setupProject(t)
	var out, errBuf bytes.Buffer
	if code := Propose([]string{"--title", "候选规则", "--type", "note", "--body", "库内正文"}, &out, &errBuf); code != 0 {
		t.Fatalf("propose code=%d err=%q", code, errBuf.String())
	}
	// cwd（项目目录）放一份同名草稿副本（如导出编辑的副本），内容与库内不同
	shadow := "---\ntitle: 副本\ntype: note\ndraft: true\nsummary: s\n---\n\ncwd 副本正文\n"
	cwdFile := "候选规则.md"
	if err := os.WriteFile(cwdFile, []byte(shadow), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := Approve([]string{cwdFile}, &out, &errBuf); code != 0 {
		t.Fatalf("approve code=%d err=%q", code, errBuf.String())
	}
	// 库内草稿已转正，正文未被副本污染
	data, err := os.ReadFile(filepath.Join(kb, "knowledge", cwdFile))
	if err != nil {
		t.Fatal(err)
	}
	e, err := entry.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if e.Draft {
		t.Fatal("库内草稿应被转正")
	}
	if e.Body != "库内正文" || e.Title != "候选规则" {
		t.Fatalf("库内条目内容被 cwd 副本污染: %+v", e)
	}
	// cwd 副本保持原样，未写穿库外
	cwdData, err := os.ReadFile(cwdFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(cwdData) != shadow {
		t.Fatalf("cwd 副本被改写: %q", cwdData)
	}
}

// --file 带 front matter 必须剥离（R3 E-01，与 Add 同款历史坑）：
// 直接落库会被 Serialize 再包一层 ---，内层元数据静默变正文。
func TestProposeFileStripsFrontmatter(t *testing.T) {
	_, kb := setupProject(t)
	src := filepath.Join(t.TempDir(), "src.md")
	fm := "---\ntitle: 内层标题\ntype: pitfall\n---\n\n真正的正文。\n"
	if err := os.WriteFile(src, []byte(fm), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	if code := Propose([]string{"--title", "外层标题", "--file", src}, &out, &errBuf); code != 0 {
		t.Fatalf("propose code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "front matter") {
		t.Fatalf("应警告已剥离 front matter: %q", errBuf.String())
	}
	entries, err := entry.Load(filepath.Join(kb, "knowledge"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries %+v err=%v", entries, err)
	}
	e := entries[0]
	if e.Title != "外层标题" || strings.Contains(e.Body, "---") || strings.Contains(e.Body, "内层标题") {
		t.Fatalf("front matter 应剥离、title 以命令行为准: %+v body=%q", e, e.Body)
	}
	if !strings.Contains(e.Body, "真正的正文") {
		t.Fatalf("正文应保留: %q", e.Body)
	}
}
