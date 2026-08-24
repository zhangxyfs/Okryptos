package gui

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openknowledge/internal/registry"
)

// mkProjectAt 注册一个项目：项目根目录指向已存在的 dir（README 夹具用），
// 并在 OK_HOME 中创建空 knowledge 目录。可重复调用注册多个项目（读-改-写注册表）。
func mkProjectAt(t *testing.T, okHome, name, dir string) {
	t.Helper()
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil || reg == nil {
		reg = &registry.Registry{}
	}
	if err := reg.AddProject(name, dir); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(okHome, "projects", name, "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeKnowledge(t *testing.T, okHome, project, file, content string) {
	t.Helper()
	p := filepath.Join(okHome, "projects", project, "knowledge", file)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type readmeResp struct {
	Found   bool   `json:"found"`
	Source  string `json:"source"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

func getReadme(t *testing.T, base, project string) (int, readmeResp) {
	t.Helper()
	code, data := do(t, "GET", base+"/api/project/readme?project="+project, testToken, nil)
	var res readmeResp
	if code == 200 {
		if err := json.Unmarshal(data, &res); err != nil {
			t.Fatalf("invalid JSON: %v (%s)", err, data)
		}
	}
	return code, res
}

func TestAPIProjectReadme(t *testing.T) {
	h, _, okHome := newEnv(t)
	// readmeProj：根目录有 README.md；enProj：只有 README_EN.md；
	// wikiProj：无 README 但有 wiki 概述条目（另放一条草稿 wiki 与分支差异 wiki，均不应命中）；
	// emptyProj：无 README 无 wiki 条目。
	readmeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(readmeDir, "README.md"), []byte("# 演示\n\n这是 README 正文。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	enDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(enDir, "README_EN.md"), []byte("# Demo\n\nEnglish readme.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkProjectAt(t, okHome, "readmeProj", readmeDir)
	mkProjectAt(t, okHome, "enProj", enDir)
	mkProjectAt(t, okHome, "wikiProj", t.TempDir())
	mkProjectAt(t, okHome, "emptyProj", t.TempDir())
	writeKnowledge(t, okHome, "wikiProj", "架构总览.md",
		"---\ntitle: 架构总览\ntype: reference\ntags: [wiki, 架构]\nsummary: 架构\ndraft: false\nmandatory: false\n---\nwiki 概述正文。\n")
	writeKnowledge(t, okHome, "wikiProj", "草稿 wiki.md",
		"---\ntitle: 草稿 wiki\ntype: reference\ntags: [wiki]\nsummary: s\ndraft: true\nmandatory: false\n---\n不应命中。\n")
	writeKnowledge(t, okHome, "wikiProj", "分支差异.md",
		"---\ntitle: 架构总览（dev 分支差异）\ntype: reference\ntags: [wiki, branch:dev]\nsummary: s\ndraft: false\nmandatory: false\n---\n不应命中。\n")

	srv := httptest.NewServer(h)
	defer srv.Close()

	// 1. README.md 命中
	code, res := getReadme(t, srv.URL, "readmeProj")
	if code != 200 || !res.Found || res.Source != "readme" || res.Path != "README.md" ||
		!strings.Contains(res.Content, "这是 README 正文。") {
		t.Fatalf("readmeProj: code=%d res=%+v", code, res)
	}
	// 2. README_EN.md 回落命中
	code, res = getReadme(t, srv.URL, "enProj")
	if code != 200 || !res.Found || res.Source != "readme" || res.Path != "README_EN.md" ||
		!strings.Contains(res.Content, "English readme.") {
		t.Fatalf("enProj: code=%d res=%+v", code, res)
	}
	// 3. wiki 概述回落（草稿与分支差异条目跳过）
	code, res = getReadme(t, srv.URL, "wikiProj")
	if code != 200 || !res.Found || res.Source != "wiki" || res.Title != "架构总览" ||
		!strings.Contains(res.Content, "wiki 概述正文。") ||
		!strings.Contains(res.Path, "projects/wikiProj/knowledge/") {
		t.Fatalf("wikiProj: code=%d res=%+v", code, res)
	}
	// 4. 无 README 无 wiki 条目 → found:false
	code, res = getReadme(t, srv.URL, "emptyProj")
	if code != 200 || res.Found {
		t.Fatalf("emptyProj: code=%d res=%+v", code, res)
	}
	// 5. 参数错误：缺参 400、未注册 404
	if code, _ = getReadme(t, srv.URL, ""); code != 400 {
		t.Fatalf("missing project: code=%d, want 400", code)
	}
	if code, _ = getReadme(t, srv.URL, "ghost"); code != 404 {
		t.Fatalf("unregistered project: code=%d, want 404", code)
	}
	if code, _ = getReadme(t, srv.URL, "../etc"); code != 400 {
		t.Fatalf("traversal project name: code=%d, want 400", code)
	}
}

// claimTicket 申领 readme-asset 一次性票据（L-07）；非 200 即失败。
func claimTicket(t *testing.T, base, proj, path string) string {
	t.Helper()
	code, data := do(t, "POST", base+"/api/project/readme-asset-ticket", testToken,
		map[string]string{"project": proj, "path": path})
	if code != 200 {
		t.Fatalf("claim ticket(%s,%s): code=%d body=%s", proj, path, code, data)
	}
	var res struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(data, &res); err != nil || res.Ticket == "" {
		t.Fatalf("claim ticket: bad response %s", data)
	}
	return res.Ticket
}

// assetURL 拼 readme-asset 票据直链。
func assetURL(base, ticket string) string {
	return base + "/api/project/readme-asset?ticket=" + url.QueryEscape(ticket)
}

// TestAPIProjectReadmeAsset 覆盖 readme-asset 端点四态：正常读取 / .. 穿越与绝对
// 路径 / 非法扩展名 / 文件不存在。鉴权为一次性票据（L-07）：每请求先申领 ticket；
// 另验旧 ?token= 直链不再放行。
func TestAPIProjectReadmeAsset(t *testing.T) {
	h, _, okHome := newEnv(t)
	dir := t.TempDir()
	png := []byte("\x89PNG\r\n\x1a\nfake")
	if err := os.MkdirAll(filepath.Join(dir, "docs", "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "assets", "logo.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkProjectAt(t, okHome, "assetProj", dir)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 1. 正常读取：200 + 字节一致 + Content-Type 按扩展名
	code, data := do(t, "GET", assetURL(srv.URL, claimTicket(t, srv.URL, "assetProj", "docs/assets/logo.png")), "", nil)
	if code != 200 || !bytes.Equal(data, png) {
		t.Fatalf("normal: code=%d len=%d", code, len(data))
	}
	// 2. 穿越与绝对路径：403（票据可申领非法路径，兑换时照卡；含 URL 编码的 ..%2f）
	for _, p := range []string{"../secret.png", "..\\secret.png", "../../etc/x.png", "/etc/x.png", "C:/Windows/x.png"} {
		if code, _ = do(t, "GET", assetURL(srv.URL, claimTicket(t, srv.URL, "assetProj", p)), "", nil); code != 403 {
			t.Fatalf("traversal %q: code=%d, want 403", p, code)
		}
	}
	// 3. 非法扩展名：400（README.md 存在但不是图片）
	if code, _ = do(t, "GET", assetURL(srv.URL, claimTicket(t, srv.URL, "assetProj", "README.md")), "", nil); code != 400 {
		t.Fatalf("bad ext: code=%d, want 400", code)
	}
	// 4. 文件不存在：404
	if code, _ = do(t, "GET", assetURL(srv.URL, claimTicket(t, srv.URL, "assetProj", "docs/assets/ghost.png")), "", nil); code != 404 {
		t.Fatalf("missing: code=%d, want 404", code)
	}
	// 5. L-07：旧 ?token= 长期令牌直链与无票据直链一律 401
	legacy := srv.URL + "/api/project/readme-asset?project=assetProj&path=" +
		url.QueryEscape("docs/assets/logo.png") + "&token=" + testToken
	if code, _ = do(t, "GET", legacy, "", nil); code != 401 {
		t.Fatalf("legacy query token: code=%d, want 401", code)
	}
	if code, _ = do(t, "GET", srv.URL+"/api/project/readme-asset", "", nil); code != 401 {
		t.Fatalf("no ticket: code=%d, want 401", code)
	}
}

// TestReadmeAssetTicket 票据生命周期（L-07）：申领走 X-Ok-Token 头鉴权（长期 token
// 不进 URL）、一次性核销（第二次 401）、过期作废。
func TestReadmeAssetTicket(t *testing.T) {
	h, _, okHome := newEnv(t)
	dir := t.TempDir()
	png := []byte("\x89PNG\r\n\x1a\nfake")
	if err := os.WriteFile(filepath.Join(dir, "a.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	mkProjectAt(t, okHome, "tkProj", dir)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 申领鉴权：无 token 401、未注册项目 404、缺 path 400
	if code, _ := do(t, "POST", srv.URL+"/api/project/readme-asset-ticket", "",
		map[string]string{"project": "tkProj", "path": "a.png"}); code != 401 {
		t.Fatalf("claim without token: code=%d, want 401", code)
	}
	if code, _ := do(t, "POST", srv.URL+"/api/project/readme-asset-ticket", testToken,
		map[string]string{"project": "ghost", "path": "a.png"}); code != 404 {
		t.Fatalf("claim unregistered project: code=%d, want 404", code)
	}
	if code, _ := do(t, "POST", srv.URL+"/api/project/readme-asset-ticket", testToken,
		map[string]string{"project": "tkProj"}); code != 400 {
		t.Fatalf("claim without path: code=%d, want 400", code)
	}

	// 一次性核销：第一次 200 且字节一致，同一票据第二次 401
	tk := claimTicket(t, srv.URL, "tkProj", "a.png")
	code, data := do(t, "GET", assetURL(srv.URL, tk), "", nil)
	if code != 200 || !bytes.Equal(data, png) {
		t.Fatalf("redeem: code=%d len=%d", code, len(data))
	}
	if code, _ = do(t, "GET", assetURL(srv.URL, tk), "", nil); code != 401 {
		t.Fatalf("re-redeem: code=%d, want 401", code)
	}

	// 过期作废：直接把票据过期时间拨到过去（同包测试直改内存态）
	tk2 := claimTicket(t, srv.URL, "tkProj", "a.png")
	h.tkMu.Lock()
	v := h.tickets[tk2]
	v.expires = time.Now().Add(-time.Second)
	h.tickets[tk2] = v
	h.tkMu.Unlock()
	if code, _ = do(t, "GET", assetURL(srv.URL, tk2), "", nil); code != 401 {
		t.Fatalf("expired ticket: code=%d, want 401", code)
	}
}
