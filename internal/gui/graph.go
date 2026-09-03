package gui

import (
	"path/filepath"
	"regexp"
	"sort"

	"okryptos/internal/embed"
	"okryptos/internal/entry"
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

// graphCategory 与前端 catKeyOf（web/app.js 的 catKeyOf）同优先级。
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
// （对齐前端 refSubGroups 标题约定），sem 边来自向量余弦近邻（vecs 为 nil 时跳过）；
// deg 为每条边两端各 +1。
func buildGraph(entries []*entry.Entry, vecs map[string][]float32) graphData {
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
	// sem 通道：每条目取动态阈值以上的前 3 近邻。top-3 候选与打分在同一趟
	// O(n²) 循环里收集，再由 semThreshold 按目标密度（平均度≈1）分位反推阈值
	// 过滤加边。配对按文件名升序定向（source<target），A→B 与 B→A 经 add 的
	// key 去重只留一条。n>2000 跳过（O(n²×dim) 护栏，静默）。
	const (
		semTopK = 3
		semMaxN = 2000
	)
	if n := len(vecs); n > 0 && n <= semMaxN {
		names := make([]string, 0, n)
		for name := range vecs {
			names = append(names, name)
		}
		sort.Strings(names)
		type sim struct {
			name string
			cos  float64
		}
		tops := make([][]sim, len(names))
		cands := make([]float64, 0, semTopK*len(names))
		for i, a := range names {
			var sims []sim
			for j, b := range names {
				if i == j {
					continue
				}
				sims = append(sims, sim{b, embed.Cosine(vecs[a], vecs[b])})
			}
			// 相似度降序，并列按文件名升序（确定性）
			sort.Slice(sims, func(x, y int) bool {
				if sims[x].cos != sims[y].cos {
					return sims[x].cos > sims[y].cos
				}
				return sims[x].name < sims[y].name
			})
			if len(sims) > semTopK {
				sims = sims[:semTopK]
			}
			tops[i] = sims
			for _, s := range sims {
				cands = append(cands, s.cos)
			}
		}
		thr := semThreshold(cands, len(nodes))
		for i, a := range names {
			for _, s := range tops[i] {
				if s.cos < thr {
					continue
				}
				src, tgt := a, s.name
				if src > tgt {
					src, tgt = tgt, src
				}
				add(src, tgt, "sem")
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

// semThreshold 由全部条目 top-3 候选相似度（≤3n 个）分位反推 sem 边阈值：目标
// 去重后边数 ≈0.45×nodeCount（平均度≈1），即候选降序第 ceil(0.45×nodeCount×2)
// 名的分值（×2 是每候选被两端各提名一次的换算）；候选不足取候选最小值。结果
// 钳制 [0.55,0.75]；nodeCount<10 的小库分布噪声大，直接用 0.55。
func semThreshold(cands []float64, nodeCount int) float64 {
	const (
		semLo = 0.55
		semHi = 0.75
	)
	if nodeCount < 10 {
		return semLo
	}
	if len(cands) == 0 {
		return semHi
	}
	sorted := make([]float64, len(cands))
	copy(sorted, cands)
	sort.Sort(sort.Reverse(sort.Float64Slice(sorted)))
	rank := (9*nodeCount + 9) / 10 // ceil(0.45×nodeCount×2)，整数运算避免浮点误差
	thr := sorted[len(sorted)-1]
	if rank <= len(sorted) {
		thr = sorted[rank-1]
	}
	return min(max(thr, semLo), semHi)
}
