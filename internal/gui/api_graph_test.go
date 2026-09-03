package gui

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"okryptos/internal/index"
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

// writeGraphVectors 往项目索引库 vectors 表直写测试向量（参 internal/cli/propose_test.go
// 的 raw 开库做法）；先 index.Open 建库保证 schema 与生产一致。
func writeGraphVectors(t *testing.T, okHome, project string, vecs map[string][]float32) {
	t.Helper()
	kb := filepath.Join(okHome, "projects", project, "kb.db")
	db, err := index.Open(kb)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", kb)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	for name, v := range vecs {
		b := make([]byte, 4*len(v))
		for i, f := range v {
			binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
		}
		if _, err := raw.Exec(`INSERT OR REPLACE INTO vectors(filename,dim,blob) VALUES(?,?,?)`,
			name, len(v), b); err != nil {
			t.Fatal(err)
		}
	}
}

// semEdgeCount 统计给定无序文件对之间的 sem 边条数（配对去重后应恰为 1）。
func semEdgeCount(g graphTestPayload, a, b string) int {
	n := 0
	for _, e := range g.Edges {
		if e.Kind != "sem" {
			continue
		}
		if (e.Source == a && e.Target == b) || (e.Source == b && e.Target == a) {
			n++
		}
	}
	return n
}

func TestGraphSemanticEdges(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "p")

	// 10 节点走动态阈值路径（≥10 不触发小库 0.55 捷径）：甲/乙同向（cosine=1），
	// 丙~癸各据一个正交方向（两两 cosine=0），丁无向量。top-3 候选分布为
	// 2×1.0 + 25×0，分位反推出 0 → 钳到下限 0.55：甲乙过线，正交对不过线。
	writeGraphEntry(t, okHome, "p", "甲.md", "甲", "note", nil, "正文甲")
	writeGraphEntry(t, okHome, "p", "乙.md", "乙", "note", nil, "正文乙")
	writeGraphEntry(t, okHome, "p", "丙.md", "丙", "note", nil, "正文丙")
	writeGraphEntry(t, okHome, "p", "丁.md", "丁", "note", nil, "正文丁")
	writeGraphEntry(t, okHome, "p", "戊.md", "戊", "note", nil, "正文戊")
	writeGraphEntry(t, okHome, "p", "己.md", "己", "note", nil, "正文己")
	writeGraphEntry(t, okHome, "p", "庚.md", "庚", "note", nil, "正文庚")
	writeGraphEntry(t, okHome, "p", "辛.md", "辛", "note", nil, "正文辛")
	writeGraphEntry(t, okHome, "p", "壬.md", "壬", "note", nil, "正文壬")
	writeGraphEntry(t, okHome, "p", "癸.md", "癸", "note", nil, "正文癸")
	writeGraphVectors(t, okHome, "p", map[string][]float32{
		"甲.md": {1, 0, 0, 0, 0, 0, 0, 0},
		"乙.md": {1, 0, 0, 0, 0, 0, 0, 0},
		"丙.md": {0, 1, 0, 0, 0, 0, 0, 0},
		"戊.md": {0, 0, 1, 0, 0, 0, 0, 0},
		"己.md": {0, 0, 0, 1, 0, 0, 0, 0},
		"庚.md": {0, 0, 0, 0, 1, 0, 0, 0},
		"辛.md": {0, 0, 0, 0, 0, 1, 0, 0},
		"壬.md": {0, 0, 0, 0, 0, 0, 1, 0},
		"癸.md": {0, 0, 0, 0, 0, 0, 0, 1},
	})

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
	if n := semEdgeCount(g, "甲.md", "乙.md"); n != 1 {
		t.Errorf("甲乙 sem 边条数=%d want 1, edges=%+v", n, g.Edges)
	}
	if n := semEdgeCount(g, "甲.md", "丙.md"); n != 0 {
		t.Errorf("甲丙 cosine=0 不应有 sem 边, edges=%+v", g.Edges)
	}
	// 丁无向量 → 无任何 sem 边；deg 也不受 sem 影响
	deg := map[string]int{}
	for _, n := range g.Nodes {
		deg[n.ID] = n.Deg
	}
	if deg["丁.md"] != 0 {
		t.Errorf("丁 deg=%d want 0", deg["丁.md"])
	}
	// sem 边计入 deg：甲/乙各 +1（无 ref/struct 边干扰）
	if deg["甲.md"] != 1 || deg["乙.md"] != 1 {
		t.Errorf("deg 未计入 sem 边: %v", deg)
	}
	// 配对去重定向：sem 边 source < target（文件名升序）
	for _, e := range g.Edges {
		if e.Kind == "sem" && e.Source >= e.Target {
			t.Errorf("sem 边应 source<target: %+v", e)
		}
	}
}

func TestGraphNoVectors(t *testing.T) {
	h, _, okHome := newEnv(t)
	mkProject(t, okHome, "p")
	writeGraphEntry(t, okHome, "p", "甲.md", "甲", "note", nil, "正文甲")
	writeGraphEntry(t, okHome, "p", "乙.md", "乙", "note", nil, "正文乙")

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
	if len(g.Nodes) != 2 {
		t.Fatalf("nodes=%d want 2", len(g.Nodes))
	}
	for _, e := range g.Edges {
		if e.Kind == "sem" {
			t.Errorf("无索引库不应有 sem 边: %+v", e)
		}
	}
	for _, n := range g.Nodes {
		if n.Deg != 0 {
			t.Errorf("无向量 deg 应为 0: %+v", n)
		}
	}
}

// TestSemThresholdCalibration 覆盖 semThreshold 的四种分布口径：均匀分布分位
// 反推、全低分钳下限零边、全高分钳上限且 top-3 上限仍生效、小库固定 0.55。
func TestSemThresholdCalibration(t *testing.T) {
	// 20 节点、候选分值已知分布：17×0.9 + 1×0.65 + 42×0.4，降序第
	// ceil(0.45×20×2)=18 名恰为 0.65 → 阈值=0.65（[0.55,0.75] 内不触发钳制）；
	// 过线候选 18 个，按两端各提名一次折半，去重边≈9（0.45×20）。
	t.Run("quantile", func(t *testing.T) {
		cands := make([]float64, 0, 60)
		for i := 0; i < 17; i++ {
			cands = append(cands, 0.9)
		}
		cands = append(cands, 0.65)
		for i := 0; i < 42; i++ {
			cands = append(cands, 0.4)
		}
		thr := semThreshold(cands, 20)
		if thr != 0.65 {
			t.Fatalf("thr=%v want 0.65", thr)
		}
		over := 0
		for _, c := range cands {
			if c >= thr {
				over++
			}
		}
		if over != 18 {
			t.Errorf("过线候选=%d want 18（去重边≈9）", over)
		}
	})
	// 全低分库：候选全 0.3，分位值被钳到下限 0.55 → 无候选过线，零 sem 边。
	t.Run("low", func(t *testing.T) {
		cands := make([]float64, 60)
		for i := range cands {
			cands[i] = 0.3
		}
		thr := semThreshold(cands, 20)
		if thr != 0.55 {
			t.Fatalf("thr=%v want 0.55", thr)
		}
		for _, c := range cands {
			if c >= thr {
				t.Fatal("低分候选不应过线")
			}
		}
	})
	// 全高分库：候选全 0.9，分位值被钳到上限 0.75；12 节点全同向时 top-3
	// 上限仍生效——sem 边数远低于全连通 C(12,2)=66。
	t.Run("high", func(t *testing.T) {
		cands := make([]float64, 60)
		for i := range cands {
			cands[i] = 0.9
		}
		if thr := semThreshold(cands, 20); thr != 0.75 {
			t.Fatalf("thr=%v want 0.75", thr)
		}
		h, _, okHome := newEnv(t)
		mkProject(t, okHome, "p")
		vecs := map[string][]float32{}
		for i := 0; i < 12; i++ {
			name := fmt.Sprintf("s%02d.md", i)
			writeGraphEntry(t, okHome, "p", name, name, "note", nil, "正文")
			vecs[name] = []float32{1, 0, 0}
		}
		writeGraphVectors(t, okHome, "p", vecs)
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
		semN := 0
		for _, e := range g.Edges {
			if e.Kind == "sem" {
				semN++
			}
		}
		if semN == 0 || semN > 3*12 {
			t.Errorf("sem 边数=%d，top-3 上限应约束在 (0, 3n=36] 内", semN)
		}
	})
	// <10 节点小库：分布噪声大，不看候选直接用 0.55。
	t.Run("small", func(t *testing.T) {
		if thr := semThreshold([]float64{0.9, 0.9, 0.9}, 5); thr != 0.55 {
			t.Fatalf("thr=%v want 0.55", thr)
		}
	})
	// 候选不足（分位排名超出候选数）：取候选最小值再钳制。
	t.Run("shortfall", func(t *testing.T) {
		cands := []float64{0.9, 0.8, 0.7, 0.65, 0.6}
		if thr := semThreshold(cands, 20); thr != 0.6 {
			t.Fatalf("thr=%v want 0.6（候选最小值）", thr)
		}
	})
}
