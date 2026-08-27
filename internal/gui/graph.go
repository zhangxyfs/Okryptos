package gui

import (
	"path/filepath"
	"regexp"
	"sort"

	"openknowledge/internal/entry"
)

type graphNode struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Type     string   `json:"type"`
	Tags     []string `json:"tags"`
	Category string   `json:"category"`
	Summary  string   `json:"summary"`
	IsDir    bool     `json:"is_dir"`
	Deg      int      `json:"deg"`
}

type graphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

type graphData struct {
	Nodes      []graphNode `json:"nodes"`
	Edges      []graphEdge `json:"edges"`
	Categories []string    `json:"categories"`
}

var mdLinkRe = regexp.MustCompile(`\]\(([^)\s]+\.md)\)`)

// graphCategory 与前端 catKeyOf（web/app.js:1942）同优先级。
func graphCategory(e *entry.Entry) string {
	switch {
	case e.Archived:
		return "archived"
	case e.Draft:
		return "draft"
	case e.Mandatory:
		return "mandatory"
	case e.Type == "rule" || e.Type == "pitfall" || e.Type == "note":
		return e.Type
	default:
		return "reference"
	}
}

func hasTag(e *entry.Entry, tag string) bool {
	for _, t := range e.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// buildGraph 由条目集构建图谱页数据：节点 id 为文件名；ref 边来自正文
// markdown 链接命中，struct 边把 wiki 条目挂到 架构总览/演进历程 目录节点
// （对齐前端 refSubGroups 标题约定）；deg 为每条边两端各 +1。
func buildGraph(entries []*entry.Entry) graphData {
	nodes := make([]graphNode, 0, len(entries))
	idx := map[string]int{}
	files := map[string]bool{}
	for _, e := range entries {
		f := filepath.Base(e.Path)
		files[f] = true
		idx[f] = len(nodes)
		nodes = append(nodes, graphNode{
			ID: f, Title: e.Title, Type: e.Type, Tags: e.Tags,
			Category: graphCategory(e), Summary: e.Summary,
			IsDir: e.Type == "reference" && (e.Title == "架构总览" || e.Title == "演进历程"),
		})
	}
	var edges []graphEdge
	seen := map[string]bool{}
	add := func(s, t, k string) {
		if s == t || !files[s] || !files[t] {
			return
		}
		key := s + "→" + t + "→" + k
		if seen[key] {
			return
		}
		seen[key] = true
		edges = append(edges, graphEdge{Source: s, Target: t, Kind: k})
		nodes[idx[s]].Deg++
		nodes[idx[t]].Deg++
	}
	for _, e := range entries {
		f := filepath.Base(e.Path)
		for _, m := range mdLinkRe.FindAllStringSubmatch(e.Body, -1) {
			add(f, filepath.Base(m[1]), "ref")
		}
		if e.Type == "reference" && hasTag(e, "wiki") && !nodes[idx[f]].IsDir {
			if hasTag(e, "历史") {
				add("演进历程.md", f, "struct")
			} else {
				add("架构总览.md", f, "struct")
			}
		}
	}
	order := []string{"mandatory", "rule", "pitfall", "note", "reference", "draft", "archived"}
	used := map[string]bool{}
	for _, n := range nodes {
		used[n.Category] = true
	}
	var cats []string
	for _, c := range order {
		if used[c] {
			cats = append(cats, c)
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		return edges[i].Target < edges[j].Target
	})
	return graphData{Nodes: nodes, Edges: edges, Categories: cats}
}
