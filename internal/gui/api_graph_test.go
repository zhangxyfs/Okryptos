package gui

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

type graphTestPayload struct {
	Nodes []struct {
		ID       string   `json:"id"`
		Title    string   `json:"title"`
		Type     string   `json:"type"`
		Tags     []string `json:"tags"`
		Category string   `json:"category"`
		IsDir    bool     `json:"is_dir"`
		Deg      int      `json:"deg"`
	} `json:"nodes"`
	Edges []struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Kind   string `json:"kind"`
	} `json:"edges"`
	Categories []string `json:"categories"`
}

// writeGraphEntry 复用 api_readme_test.go:36 writeKnowledge 的同款文件格式
func writeGraphEntry(t *testing.T, okHome, project, file, title, typ string, tags []string, body string) {
	t.Helper()
	fm := fmt.Sprintf("---\ntitle: %s\ntype: %s\ntags: [%s]\n---\n\n%s\n",
		title, typ, strings.Join(tags, ", "), body)
	writeKnowledge(t, okHome, project, file, fm)
}

func TestGraphBasic(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "p")

	writeGraphEntry(t, okHome, "p", "架构总览.md", "架构总览", "reference", []string{"wiki"}, "总览")
	writeGraphEntry(t, okHome, "p", "演进历程.md", "演进历程", "reference", []string{"wiki"}, "历程")
	writeGraphEntry(t, okHome, "p", "模块甲.md", "模块甲", "reference", []string{"wiki"}, "参见 [乙](模块乙.md)")
	writeGraphEntry(t, okHome, "p", "演进历程（v2 段）.md", "演进历程（v2 段）", "reference", []string{"wiki", "历史"}, "段")
	writeGraphEntry(t, okHome, "p", "模块乙.md", "模块乙", "reference", nil, "正文")
	writeGraphEntry(t, okHome, "p", "坑一.md", "坑一", "pitfall", []string{"gui"}, "坑")

	srv := httptest.NewServer(h)
	defer srv.Close()
	status, body := do(t, "GET", srv.URL+"/api/graph?project=p", testToken, nil)
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	var g graphTestPayload
	if err := json.Unmarshal(body, &g); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(g.Nodes) != 6 {
		t.Fatalf("nodes=%d want 6", len(g.Nodes))
	}
	byID := map[string]int{}
	for _, n := range g.Nodes {
		byID[n.ID] = n.Deg
		if n.ID == "架构总览.md" && (!n.IsDir || n.Category != "reference") {
			t.Errorf("dir node: %+v", n)
		}
		if n.ID == "坑一.md" && n.Category != "pitfall" {
			t.Errorf("category: %+v", n)
		}
	}
	has := func(s, t2, k string) bool {
		for _, e := range g.Edges {
			if e.Source == s && e.Target == t2 && e.Kind == k {
				return true
			}
		}
		return false
	}
	if !has("模块甲.md", "模块乙.md", "ref") {
		t.Errorf("missing ref edge: %+v", g.Edges)
	}
	if !has("架构总览.md", "模块甲.md", "struct") {
		t.Errorf("missing struct edge 架构总览→模块甲")
	}
	if !has("演进历程.md", "演进历程（v2 段）.md", "struct") {
		t.Errorf("missing struct edge 演进历程→历史段")
	}
	if byID["模块乙.md"] != 1 || byID["模块甲.md"] != 2 {
		t.Errorf("deg: %v", byID)
	}
}

func TestGraphGuards(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "p")
	srv := httptest.NewServer(h)
	defer srv.Close()
	if s, _ := do(t, "GET", srv.URL+"/api/graph?project=p", "wrong-token", nil); s != 401 {
		t.Errorf("no token status=%d want 401", s)
	}
	if s, _ := do(t, "GET", srv.URL+"/api/graph", testToken, nil); s != 400 {
		t.Errorf("no project status=%d want 400", s)
	}
	if s, _ := do(t, "GET", srv.URL+"/api/graph?project=nope", testToken, nil); s != 404 {
		t.Errorf("bad project status=%d want 404", s)
	}
}
