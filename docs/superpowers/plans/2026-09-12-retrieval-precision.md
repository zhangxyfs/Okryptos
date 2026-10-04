# 检索注入精度提升（方案A + LLM 后置过滤 + GUI 本地 LLM）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 消除短口语 prompt 的关键词通道误注入（"还是黑底的"事故），叠加 LLM 相关性后置过滤，并让 GUI 的 LLM 配置支持本地 ollama 档与"识别意图"双使用中槽位。

**Architecture:** 三部分独立可测。Part A（方案A，规则层）：虚词停用表只作用于关键词通道准入计数 + 命中词元覆盖度门槛（minimum_should_match 语义）+ 纯虚词查询跳过关键词通道，不动"通道独立准入"架构与 FTS 打分。Part B（后置过滤）：top_n 候选经分支过滤/冷却后，交 LLM 逐条裁决，只删不加，全程 fail-open。Part C（GUI 本地 LLM + 双槽位）：llmx/config/GUI 增加 kind=ollama（本地、免 key、默认 localhost:11434）；`[llm]` 拆两个"使用中"槽位——普通 active（条目优化等手动场景）与 active_filter（识别意图：检索过滤每轮自动调用），profile 上的"识别意图"勾选决定落入哪个槽；filterx 取专用槽优先、空则回退普通槽。

**Tech Stack:** Go（stdlib only，无新依赖）、SQLite FTS5、llmx（已有 openai/anthropic 客户端）、原生 JS GUI（web/app.js）。

**方案依据:** `docs/2026-09-12-retrieval-noise-solutions.md` §8（方案A/B/C 与业界一手来源）。

**范围说明（已裁掉）:** "ok 托管下载 gguf + chat sidecar"的 builtin 本地 LLM 档不在本计划内——它等价于再造一套 embedsidecar（chat 模型清单/下载/独立状态文件族/daemon 第二 janitor/GUI 下载进度条），应单独立项；立项时模型清单可直接采用 Part C 的选型结论（首选 qwen3 1.7B Q4_K_M）。本计划的 ollama 档先提供本地 LLM 能力。

## Global Constraints

- Go 版本见 `go.mod`；**禁止新增第三方依赖**（stdlib + 现有 internal 包）。
- 行为变化类新测试必须先验证对旧代码变红（判别性测试，知识库既定教训）。
- hook 链路 fail-open：任何新失败只记 `ok.log`（`logErr`），绝不阻断注入。
- LLM 调用纪律：超时按场景区分（过滤场景默认 3s，远小于 hook 预算）；temperature 缺省不传（llmx 已收口，新代码不得传 temperature）。
- 全局 config.toml 写盘只走 `setupx.updateGlobalConfig`（fsx.WithFileLock 锁内整档重编码），禁止裸读改写。
- GUI 开关类设置用勾选+保存按钮两段式（弹窗内勾选随「确定」统一生效，不用 change 即存）。
- 提交信息格式：`type(scope): 中文描述`（参照 git log，如 `fix(okmeter): ...`）。
- changelog 与文档不硬折行（一段一行）。
- Windows 上跑测试用 git-bash；`go test` 不要接管道（会吞退出码，知识库既定教训）。

---

## Part A：方案A——关键词通道覆盖度准入（Tasks 1-3）

事故链：短口语 prompt 经 `retrieve.Terms` 切出 CJK 二元组，虚词 bigram（"还是""底的"）经 FTS5 OR 匹配 + 高 idf 过 BM25 阈值，关键词通道独立准入无关条目。方案A 只改准入计数，不动索引与打分。

### Task 1: retrieve 包——虚词停用表与覆盖度公式

**Files:**
- Create: `internal/retrieve/stopwords.go`
- Test: `internal/retrieve/stopwords_test.go`

**Interfaces:**
- Produces（Task 3 消费）:
  - `func EffectiveTerms(terms []string, extra []string) []string` — 过滤停用词元后的有效词元（保序去重）
  - `func RequiredCoverage(n int, ratio float64) int` — 关键词准入要求的最少命中有效词元数

- [ ] **Step 1: 写失败测试**

创建 `internal/retrieve/stopwords_test.go`：

```go
package retrieve

import (
	"reflect"
	"testing"
)

func TestEffectiveTerms(t *testing.T) {
	cases := []struct {
		name  string
		terms []string
		extra []string
		want  []string
	}{
		{"事故词元：虚词被滤", []string{"还是", "是黑", "黑底", "底的"}, nil, []string{"是黑", "黑底"}},
		{"纯虚词", []string{"还是", "就是"}, nil, nil},
		{"实词保留且去重", []string{"构建", "双路", "构建"}, nil, []string{"构建", "双路"}},
		{"追加层生效", []string{"甲乙", "丙丁"}, []string{"甲乙"}, []string{"丙丁"}},
		{"空输入", nil, nil, nil},
	}
	for _, c := range cases {
		if got := EffectiveTerms(c.terms, c.extra); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: EffectiveTerms(%v, %v) = %v, want %v", c.name, c.terms, c.extra, got, c.want)
		}
	}
}

func TestRequiredCoverage(t *testing.T) {
	cases := []struct{ n, want int }{
		{0, 0}, {1, 1}, {2, 1}, {3, 1}, {4, 1}, {5, 2}, {6, 2}, {8, 2},
	}
	for _, c := range cases {
		if got := RequiredCoverage(c.n, 0.25); got != c.want {
			t.Errorf("RequiredCoverage(%d, 0.25) = %d, want %d", c.n, got, c.want)
		}
	}
	if got := RequiredCoverage(4, 0); got != 1 {
		t.Errorf("非法 ratio 应按 0.25: got %d", got)
	}
	if got := RequiredCoverage(8, 0.75); got != 6 {
		t.Errorf("RequiredCoverage(8, 0.75) = %d, want 6", got)
	}
}
```

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/retrieve/ -run "TestEffectiveTerms|TestRequiredCoverage" -v`
Expected: FAIL（`undefined: EffectiveTerms` / `undefined: RequiredCoverage` 编译错误）

- [ ] **Step 3: 实现**

创建 `internal/retrieve/stopwords.go`：

```go
package retrieve

import "math"

// stopwords.go 中文虚词/低判别力词元表：只作用于关键词通道的"命中词元覆盖度"
// 准入计数（index.queryAll），不进入索引文本、不影响 FTS 打分与排序。
// 依据 docs/2026-09-12-retrieval-noise-solutions.md 方案A：虚词 bigram
// （"还是""底的"类）是短口语查询误注入的主要来源。

// builtinStopTerms 内置虚词词元表（编译进二进制、随版本演进，与门控短语表同惯例；
// 用户追加层走 [retrieve.coverage] extra_stop_terms）。Terms 只产出单字与二元组，
// 表内条目不超 2 字。
var builtinStopTerms = map[string]bool{
	// 单字虚词（孤立单字 CJK 产出单字词元）
	"的": true, "了": true, "吗": true, "呢": true, "吧": true, "啊": true,
	"和": true, "与": true, "或": true, "在": true, "是": true, "有": true,
	"我": true, "你": true, "他": true, "她": true, "它": true, "这": true,
	"那": true, "就": true, "都": true, "也": true, "还": true, "不": true,
	"没": true, "很": true, "太": true, "被": true, "把": true, "给": true,
	// 高频虚词/跨词界假词二元组
	"还是": true, "就是": true, "是就": true, "底的": true, "了的": true, "的是": true,
	"什么": true, "怎么": true, "这个": true, "那个": true, "我们": true,
	"你们": true, "他们": true, "可以": true, "没有": true, "一下": true,
	"现在": true, "已经": true, "这样": true, "那样": true, "知道": true,
	"觉得": true, "应该": true, "可能": true, "但是": true, "因为": true,
	"所以": true, "如果": true, "然后": true, "一个": true, "一些": true,
	"有点": true, "非常": true, "特别": true, "真的": true,
}

// EffectiveTerms 过滤停用词元后的有效词元（保序去重）：覆盖度计数只认有效词元，
// 防止虚词"助攻"无关条目过准入。extra 为配置追加层，与内置表取并集。
func EffectiveTerms(terms []string, extra []string) []string {
	var extraSet map[string]bool
	if len(extra) > 0 {
		extraSet = make(map[string]bool, len(extra))
		for _, t := range extra {
			extraSet[t] = true
		}
	}
	seen := make(map[string]bool, len(terms))
	var out []string
	for _, t := range terms {
		if builtinStopTerms[t] || extraSet[t] || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// RequiredCoverage 关键词准入要求的最少命中有效词元数（minimum_should_match
// 语义）：n=0 → 0（调用方据此跳过关键词通道）；n≥1 → max(1, ⌈n×ratio⌉)。
// min-1 下限：「实词+口语填充」是查询常态，单词元命中即可准入；比例默认 0.25——
// 重叠 bigram 约一半是跨词界假词（建漂/移怎类），计入 n 但不计入 cov，
// 0.25 ≈ 实词 50% 覆盖（曾定 50%+min2，实证过严：n=2 退化 AND、多词查询必拒，
// 见 task-3 探针数据与"门控太严比太松更伤"教训）。ratio<=0 或 >1 按 0.25。
func RequiredCoverage(n int, ratio float64) int {
	if n <= 0 {
		return 0
	}
	if ratio <= 0 || ratio > 1 {
		ratio = 0.25
	}
	need := int(math.Ceil(float64(n) * ratio))
	if need < 1 {
		need = 1
	}
	return need
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/retrieve/ -v`
Expected: PASS（含既有 gate/clean/Terms 测试不回归）

- [ ] **Step 5: Commit**

```bash
git add internal/retrieve/stopwords.go internal/retrieve/stopwords_test.go
git commit -m "feat(retrieve): 虚词停用表与覆盖度公式（方案A 词元过滤层）"
```

### Task 2: config——[retrieve.coverage] 子表

**Files:**
- Modify: `internal/config/config.go:179-224`（RetrieveFeedback 之后加结构体；Retrieve 加字段；Default 加默认值）
- Test: `internal/config/retrieve_test.go`

**Interfaces:**
- Produces（Task 3 消费）:
  - `type RetrieveCoverage struct { Enabled bool; MinRatio float64; ExtraStopTerms []string }`（toml: `enabled` / `min_ratio` / `extra_stop_terms`）
  - `Retrieve.Coverage RetrieveCoverage`（toml: `coverage`），Default: `{Enabled: true, MinRatio: 0.25}`

- [ ] **Step 1: 写失败测试**

在 `internal/config/retrieve_test.go` 追加（先读该文件头部确认包名与现有测试风格，通常为 `package config`）：

```go
func TestCoverageDefault(t *testing.T) {
	cfg := Default()
	if !cfg.Retrieve.Coverage.Enabled || cfg.Retrieve.Coverage.MinRatio != 0.25 {
		t.Fatalf("coverage 默认应为 enabled+0.25: %+v", cfg.Retrieve.Coverage)
	}
}

func TestCoverageMergedNoAliasing(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	project := filepath.Join(dir, "project.toml")
	os.WriteFile(global, []byte("[retrieve.coverage]\nextra_stop_terms = [\"甲乙\"]\n"), 0o644)
	os.WriteFile(project, []byte("[retrieve.coverage]\nmin_ratio = 0.75\n"), 0o644)
	cfg, err := LoadMerged(project, global)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieve.Coverage.MinRatio != 0.75 {
		t.Fatalf("项目层应覆盖 min_ratio: %+v", cfg.Retrieve.Coverage)
	}
	// 全局层数组在项目层未重定义时应保留（且不因合并被污染）
	if len(cfg.Retrieve.Coverage.ExtraStopTerms) != 1 || cfg.Retrieve.Coverage.ExtraStopTerms[0] != "甲乙" {
		t.Fatalf("extra_stop_terms 合并异常: %+v", cfg.Retrieve.Coverage.ExtraStopTerms)
	}
}
```

（若 `retrieve_test.go` 缺 `os`/`path/filepath` import 则补上。）

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/config/ -run TestCoverage -v`
Expected: FAIL（`cfg.Retrieve.Coverage` 未定义，编译错误）

- [ ] **Step 3: 实现**

`internal/config/config.go`，在 `RetrieveFeedback` 结构体（:179-184）之后插入：

```go
// RetrieveCoverage 控制关键词通道的"命中词元覆盖度"准入（[retrieve.coverage]
// 子表）：FTS5 是 OR 匹配，短口语查询的虚词 bigram 会捞进无关条目（事故复盘见
// docs/2026-09-12-retrieval-noise-solutions.md）；准入要求命中
// ≥ RequiredCoverage 个不同有效词元（虚词停用表不计入，见 retrieve.EffectiveTerms）。
// 只作用于准入计数，不影响 FTS 打分与排序。
type RetrieveCoverage struct {
	Enabled        bool     `toml:"enabled"`          // 默认 true（见 Default）
	MinRatio       float64  `toml:"min_ratio"`        // 命中比例下限，默认 0.25（重叠 bigram 稀释口径，见 retrieve.RequiredCoverage）；非法值按 0.25
	ExtraStopTerms []string `toml:"extra_stop_terms"` // 内置虚词表之外的追加层
}
```

`Retrieve` 结构体在 `Feedback RetrieveFeedback \`toml:"feedback"\``（:219）之后加：

```go
	// Coverage 是关键词通道的词元覆盖度准入（[retrieve.coverage] 子表）：
	// 虚词命中的无关条目因覆盖度不足被拒；纯虚词查询跳过整个关键词通道。
	Coverage RetrieveCoverage `toml:"coverage"`
```

`Default()`（:301-306）Retrieve 字面量中 `Feedback: ...` 行之后加：

```go
			Coverage: RetrieveCoverage{Enabled: true, MinRatio: 0.25},
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ -v`
Expected: PASS（含 TestCoverageMergedNoAliasing——若因 toml 数组复用底层数组失败，按 `config.go:349-351` 注释先例在 LoadMerged 的 Decode 前深拷贝 `prevCoverageStop`）

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/retrieve_test.go
git commit -m "feat(config): [retrieve.coverage] 覆盖度准入配置节（方案A）"
```

### Task 3: index+hook——覆盖度准入与空查询门控落地

**Files:**
- Modify: `internal/index/query.go`（queryAll 关键词通道 + QueryInfo + 语义诊断改字段赋值）
- Modify: `internal/hook/core.go:218-221`（QueryInfo 诊断日志区追加覆盖度日志）
- Test: `internal/index/query_coverage_test.go`（新建）
- Test: `internal/hook/core_test.go`（追加集成测试）

**Interfaces:**
- Consumes: `retrieve.EffectiveTerms` / `retrieve.RequiredCoverage`（Task 1）；`config.RetrieveCoverage`（Task 2）
- Produces: `QueryInfo.CoverageRejected []string`、`QueryInfo.KeywordGated bool`（core.go 日志消费）

**关键既有代码事实：**
- `queryAll` 在 `internal/index/query.go:186`；关键词通道 :218-251；`var info QueryInfo` 在 :214；语义诊断块 :322-331 当前是**整体赋值** `info = QueryInfo{...}`（会冲掉此前追加的 CoverageRejected，必须改成字段赋值）。
- FTS 返回行含 `e.title, e.type, e.summary, e.body, e.tags`（tags 原始串经 `splitTags`）；`buildMatch` 在 :112。
- 测试模板见 `internal/index/query_admission_test.go:80-116`（writeEntryFile/Open/Sync/Query 四步）；`setupDB(t)` 助手在 `index_test.go:86-99`。
- hook 集成测试模板见 `internal/hook/core_test.go:547-568`（`setupProject(t)` + `writeEntry(t, kbRoot, ...)` + 写 `kbRoot/config.toml` + `project.FromCwd(projDir)` + `InjectForPrompt`）。

- [ ] **Step 1: 写失败测试**

创建 `internal/index/query_coverage_test.go`：

```go
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
```

在 `internal/hook/core_test.go` 追加集成测试（放 TestCooldownGatedTurnTicks 附近）：

```go
// TestPromptCoverageFilter 方案A 端到端：虚词查询不注入，实词查询照常注入。
func TestPromptCoverageFilter(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "构建.md", "---\ntitle: 构建双路径漂移\ntype: pitfall\ntags: [构建]\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\ndist/changelogs/ 还是陈旧内容。更彻底的做法是收敛到单一构建入口。\n")
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	if out := InjectForPrompt(pc, "s-cov", projDir, "还是黑底的"); strings.Contains(out, "构建.md") {
		t.Fatalf("虚词查询不应注入无关条目, got: %q", out)
	}
	if out := InjectForPrompt(pc, "s-cov", projDir, "构建漂移怎么回事"); !strings.Contains(out, "构建.md") {
		t.Fatalf("实词查询应照常注入, got: %q", out)
	}
}
```

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/index/ -run TestCoverage -v` 和 `go test ./internal/hook/ -run TestPromptCoverageFilter -v`
Expected: FAIL（`config.RetrieveCoverage` 若 Task 2 已合则为行为失败：虚词条目仍被注入；`info.CoverageRejected`/`KeywordGated` 未定义）。**确认 index 测试的对照组（cfgOff）在旧代码下确实命中——否则测试无判别力。**

- [ ] **Step 3: 实现**

`internal/index/query.go`：

(a) import 块加 `"okryptos/internal/retrieve"`。

(b) `QueryInfo`（:81-98）追加两个字段：

```go
	// CoverageRejected：关键词通道因有效词元覆盖度不足被拒的条目（按检出序）。
	// 供 hook 层记 ok.log（GUI 日志页按"覆盖"过滤）。
	CoverageRejected []string
	// KeywordGated：原词元非空但有效词元为 0（纯虚词查询），关键词通道整体跳过。
	KeywordGated bool
```

(c) `queryAll` 关键词通道（:216-218 注释处）替换为：

```go
	// 覆盖度准入（方案A）：有效词元 = 原词元滤虚词停用表（只作用于准入计数，
	// FTS MATCH 仍用原词元，打分/排序不变）。纯虚词查询跳过整个关键词通道。
	coverageOn := cfg.Coverage.Enabled
	effTerms := terms
	need := 0
	if coverageOn {
		effTerms = retrieve.EffectiveTerms(terms, cfg.Coverage.ExtraStopTerms)
		need = retrieve.RequiredCoverage(len(effTerms), cfg.Coverage.MinRatio)
		info.KeywordGated = len(terms) > 0 && len(effTerms) == 0
	}
	// 关键词通道：FTS5 BM25。bm25 返回负值（越小越好），取 kw=-rank，
	// 归一化为 kw/(kw+6)。
	if match := buildMatch(terms); match != "" && !info.KeywordGated {
```

（原 `if match := buildMatch(terms); match != "" {` 行随之替换。）

(d) 行循环内 `h.Tags = splitTags(tagsStr)`（:235）之后、`kw := -rank` 之前插入：

```go
			// 覆盖度计数：命中标题/tags/摘要/正文的有效词元数须 ≥ need。
			// 词元均小写（Terms 已归一），haystack 同步小写；拉丁词元按子串计
			// （"go" 可计入 "golang"）——方向是放宽准入，fail-open 可接受。
			if coverageOn {
				haystack := strings.ToLower(h.Title + " " + tagsStr + " " + h.Summary + " " + h.Body)
				cov := 0
				for _, et := range effTerms {
					if strings.Contains(haystack, et) {
						cov++
					}
				}
				if cov < need {
					info.CoverageRejected = append(info.CoverageRejected, h.Filename)
					continue
				}
			}
```

(e) 语义诊断块（:322-331）整体赋值改字段赋值（否则冲掉 CoverageRejected/KeywordGated）：

```go
		if len(coses) >= 3 && !semAdmitted {
			max, median, relGap := cosStats(coses)
			info.SemanticRejected = true
			info.Coses = len(coses)
			info.MaxCos = max
			info.MedianCos = median
			info.RelGap = relGap
		}
```

`internal/hook/core.go` 在冷却跳过日志（:218-221）之后追加：

```go
		// 覆盖度准入诊断（方案A）：记 ok.log，GUI 日志页可按"覆盖"过滤
		if info.KeywordGated {
			logErr("prompt coverage: 纯虚词查询，关键词通道整体跳过")
		}
		if len(info.CoverageRejected) > 0 {
			logErr("prompt coverage: 覆盖度不足跳过（%s）", strings.Join(info.CoverageRejected, "、"))
		}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/index/ ./internal/hook/ ./internal/retrieve/ ./internal/config/`
Expected: PASS（全绿；重点确认 `TestCoverageRejectsStopwordHit` 对照组仍命中、`TestPromptCoverageFilter` 双向断言通过）

- [ ] **Step 5: 事故回归验证（可选但建议）**

Run: `go run ./cmd/ok search "还是黑底的"`（在真实知识库目录环境下需 `OK_HOME` 指向真实库；或用 `dist/ok.exe` 重编译后跑）
Expected: 不再返回"构建双路径漂移"条目；`ok search "构建 漂移"` 类实词查询不受影响。

- [ ] **Step 6: Commit**

```bash
git add internal/index/query.go internal/index/query_coverage_test.go internal/hook/core.go internal/hook/core_test.go
git commit -m "feat(index): 关键词通道覆盖度准入与纯虚词门控（方案A 落地，修'还是黑底的'误注入）"
```

---

## Part B：LLM 相关性后置过滤（Tasks 4-6）

只删不加；未配置 LLM 时静默空转；超时/失败/解析异常全部 fail-open 保留原候选。

### Task 4: config——[retrieve.filter] 子表

**Files:**
- Modify: `internal/config/config.go`（RetrieveCoverage 之后加结构体；Retrieve 加字段；Default 加默认值）
- Test: `internal/config/retrieve_test.go`

**Interfaces:**
- Produces（Task 5 消费）:
  - `type RetrieveFilter struct { Enabled bool; TimeoutMs int; MaxTokens int }`（toml: `enabled` / `timeout_ms` / `max_tokens`）
  - `Retrieve.Filter RetrieveFilter`（toml: `filter`），Default: `{Enabled: true, TimeoutMs: 3000, MaxTokens: 64}`

- [ ] **Step 1: 写失败测试**

`internal/config/retrieve_test.go` 追加：

```go
func TestFilterDefault(t *testing.T) {
	cfg := Default()
	f := cfg.Retrieve.Filter
	if !f.Enabled || f.TimeoutMs != 3000 || f.MaxTokens != 64 {
		t.Fatalf("filter 默认应为 enabled+3000ms+64: %+v", f)
	}
}
```

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/config/ -run TestFilterDefault -v`
Expected: FAIL（`cfg.Retrieve.Filter` 未定义）

- [ ] **Step 3: 实现**

`internal/config/config.go`，`RetrieveCoverage` 之后插入：

```go
// RetrieveFilter 控制注入前的 LLM 相关性后置过滤（[retrieve.filter] 子表）：
// 检索候选（top_n 截断+分支过滤+冷却排除后）交给激活的 LLM profile 逐条裁决，
// 不相关的丢弃；只删不加。fail-open：未配置 LLM、超时、输出截断、解析失败
// 一律保留原候选（filterx 收口）。过滤用 profile 取自 [llm] 的 active_filter
// 识别意图槽（Task 7），未配置时回退普通 active。
type RetrieveFilter struct {
	Enabled   bool `toml:"enabled"`    // 默认 true（见 Default）；无可用 LLM profile 时自动空转
	TimeoutMs int  `toml:"timeout_ms"` // 单次过滤超时，默认 3000；<=0 按 3000（hook 路径同步预算内）
	MaxTokens int  `toml:"max_tokens"` // 过滤输出上限，默认 64；<=0 按 64（只输出编号数组，够用即可）
}
```

`Retrieve` 结构体在 `Coverage` 字段后加：

```go
	// Filter 是注入前的 LLM 相关性后置过滤（[retrieve.filter] 子表）。
	Filter RetrieveFilter `toml:"filter"`
```

`Default()` Retrieve 字面量加：

```go
			Filter:   RetrieveFilter{Enabled: true, TimeoutMs: 3000, MaxTokens: 64},
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/retrieve_test.go
git commit -m "feat(config): [retrieve.filter] LLM 后置过滤配置节"
```

### Task 5: filterx 包——LLM 相关性裁决

**Files:**
- Create: `internal/filterx/filterx.go`
- Test: `internal/filterx/filterx_test.go`

**Interfaces:**
- Consumes: `config.Config`（`LLM.ActiveProfile()` `config.go:99`、`Retrieve.Filter` Task 4）；`llmx.New(*p, timeout).Chat(ctx, system, user, maxTokens) (llmx.Reply, error)`（`llmx.go:28/60`，Reply 字段 `Text/Truncated` :48-56）；`index.Hit`（`Filename/Title/Summary`）；`index.SanitizeInline`
- Produces（Task 6 消费）: `func Filter(ctx context.Context, cfg config.Config, prompt string, hits []index.Hit) (kept []index.Hit, note string)`
- 注意：本任务 profile 选择先用 `ActiveProfile()`；Task 7 引入双槽位后改为 FilterProfile 优先（Task 7 内含该改动与测试）。

**既有测试先例：** `internal/llmx/llmx_test.go:16-51`（httptest 模拟 OpenAI 响应）。

- [ ] **Step 1: 写失败测试**

创建 `internal/filterx/filterx_test.go`：

```go
package filterx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"okryptos/internal/config"
	"okryptos/internal/index"
)

func testHits() []index.Hit {
	return []index.Hit{
		{Filename: "a.md", Title: "条目甲", Summary: "摘要甲"},
		{Filename: "b.md", Title: "条目乙", Summary: "摘要乙"},
		{Filename: "c.md", Title: "条目丙", Summary: "摘要丙"},
	}
}

// llmServer 按 content 应答 OpenAI 兼容 /chat/completions；delay 模拟慢响应。
func llmServer(t *testing.T, content string, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": content}}},
		})
	}))
}

func cfgWithLLM(baseURL string) config.Config {
	cfg := config.Default()
	cfg.LLM.Active = "测试"
	cfg.LLM.Profiles = []config.LLMProfile{{Name: "测试", Kind: "openai", BaseURL: baseURL, Model: "m"}}
	cfg.Retrieve.Filter = config.RetrieveFilter{Enabled: true, TimeoutMs: 3000, MaxTokens: 64}
	return cfg
}

func TestFilterKeepsSelected(t *testing.T) {
	srv := llmServer(t, "[1,3]", 0)
	defer srv.Close()
	kept, note := Filter(context.Background(), cfgWithLLM(srv.URL), "查询", testHits())
	if len(kept) != 2 || kept[0].Filename != "a.md" || kept[1].Filename != "c.md" {
		t.Fatalf("应保留 [1,3]: %+v", kept)
	}
	if note == "" {
		t.Fatal("实际调用后 note 应非空")
	}
}

func TestFilterDropAll(t *testing.T) {
	srv := llmServer(t, "[]", 0)
	defer srv.Close()
	kept, _ := Filter(context.Background(), cfgWithLLM(srv.URL), "查询", testHits())
	if len(kept) != 0 {
		t.Fatalf("空数组应全丢: %+v", kept)
	}
}

func TestFilterToleratesFence(t *testing.T) {
	srv := llmServer(t, "```json\n[2]\n```", 0)
	defer srv.Close()
	kept, _ := Filter(context.Background(), cfgWithLLM(srv.URL), "查询", testHits())
	if len(kept) != 1 || kept[0].Filename != "b.md" {
		t.Fatalf("围栏输出应被容忍: %+v", kept)
	}
}

func TestFilterGarbageKeepsAll(t *testing.T) {
	srv := llmServer(t, "我觉得都相关", 0)
	defer srv.Close()
	kept, note := Filter(context.Background(), cfgWithLLM(srv.URL), "查询", testHits())
	if len(kept) != 3 {
		t.Fatalf("解析失败应保留全部: %+v", kept)
	}
	if note == "" {
		t.Fatal("解析失败应有 note")
	}
}

func TestFilterTimeoutKeepsAll(t *testing.T) {
	srv := llmServer(t, "[1]", 500*time.Millisecond)
	defer srv.Close()
	cfg := cfgWithLLM(srv.URL)
	cfg.Retrieve.Filter.TimeoutMs = 100
	kept, note := Filter(context.Background(), cfg, "查询", testHits())
	if len(kept) != 3 {
		t.Fatalf("超时应保留全部: %+v", kept)
	}
	if note == "" {
		t.Fatal("超时应有 note")
	}
}

func TestFilterNoLLMSilent(t *testing.T) {
	cfg := config.Default() // 无激活 profile
	kept, note := Filter(context.Background(), cfg, "查询", testHits())
	if len(kept) != 3 || note != "" {
		t.Fatalf("未配置 LLM 应静默原样返回: kept=%d note=%q", len(kept), note)
	}
}

func TestFilterDisabled(t *testing.T) {
	cfg := cfgWithLLM("http://127.0.0.1:1")
	cfg.Retrieve.Filter.Enabled = false
	kept, note := Filter(context.Background(), cfg, "查询", testHits())
	if len(kept) != 3 || note != "" {
		t.Fatalf("关闭时应原样返回: kept=%d note=%q", len(kept), note)
	}
}
```

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/filterx/ -v`
Expected: FAIL（package 不存在 / `undefined: Filter`）

- [ ] **Step 3: 实现**

创建 `internal/filterx/filterx.go`：

```go
// Package filterx 检索候选的 LLM 相关性后置过滤：只删不加，全程 fail-open。
// 依据 docs/2026-09-12-retrieval-noise-solutions.md（后置过滤摆位：
// 输入小、只过滤不改召回、超时即退化为现状）。
package filterx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"okryptos/internal/config"
	"okryptos/internal/index"
	"okryptos/internal/llmx"
)

const systemPrompt = `你是知识库检索结果的相关性过滤器。给定用户输入和若干候选知识条目，判断每条候选是否与用户输入真正相关（对回答或执行该任务有实质帮助）。
只输出一个 JSON 数组，包含相关条目的编号（从 1 开始），例如 [1,3]。若都不相关输出 []。
不要输出任何解释、markdown 标记或其他文字。`

const (
	maxPromptRunes  = 1500 // 用户输入截断（hook 载荷预算）
	maxSummaryRunes = 160  // 单条摘要截断
)

// Filter 用激活的 LLM profile 对 hits 做相关性裁决，返回保留子集（顺序不变）。
// note 仅在实际尝试了 LLM 调用时非空（成功/失败各一行，供调用方记 ok.log）；
// 未启用/未配置 LLM 时原样返回且 note 为空（静默——常态不刷日志）。
// fail-open：超时/调用失败/输出截断/解析失败一律返回原 hits。
func Filter(ctx context.Context, cfg config.Config, prompt string, hits []index.Hit) (kept []index.Hit, note string) {
	if !cfg.Retrieve.Filter.Enabled || len(hits) == 0 {
		return hits, ""
	}
	p := cfg.LLM.ActiveProfile()
	if p == nil {
		return hits, ""
	}
	timeout := time.Duration(cfg.Retrieve.Filter.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	maxTokens := cfg.Retrieve.Filter.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 64
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// temperature 不传（llmx.applyTemperature 空值收口，服务端默认兼容性最好）
	rep, err := llmx.New(*p, timeout).Chat(ctx, systemPrompt, userPrompt(prompt, hits), maxTokens)
	if err != nil {
		return hits, fmt.Sprintf("LLM 调用失败，保留全部 %d 条: %v", len(hits), err)
	}
	if rep.Truncated {
		return hits, fmt.Sprintf("LLM 输出截断，保留全部 %d 条", len(hits))
	}
	keepIdx, err := parseKeep(rep.Text, len(hits))
	if err != nil {
		return hits, fmt.Sprintf("LLM 输出解析失败（%v），保留全部 %d 条", err, len(hits))
	}
	inKeep := make(map[int]bool, len(keepIdx))
	for _, i := range keepIdx {
		inKeep[i] = true
	}
	var out []index.Hit
	var dropped []string
	for i, h := range hits {
		if inKeep[i+1] {
			out = append(out, h)
		} else {
			dropped = append(dropped, h.Filename)
		}
	}
	if len(dropped) == 0 {
		return out, fmt.Sprintf("LLM 裁决：%d 条全部保留", len(hits))
	}
	return out, fmt.Sprintf("LLM 裁决：%d→%d 条，丢弃（%s）", len(hits), len(out), strings.Join(dropped, "、"))
}

// userPrompt 组装用户侧载荷：用户输入（截断）+ 编号候选（标题+摘要，消毒截断）。
func userPrompt(prompt string, hits []index.Hit) string {
	var b strings.Builder
	b.WriteString("用户输入：\n")
	b.WriteString(truncateRunes(strings.TrimSpace(prompt), maxPromptRunes))
	b.WriteString("\n\n候选条目：\n")
	for i, h := range hits {
		fmt.Fprintf(&b, "[%d] 标题：%s\n摘要：%s\n", i+1,
			index.SanitizeInline(h.Title),
			truncateRunes(index.SanitizeInline(h.Summary), maxSummaryRunes))
	}
	return b.String()
}

// parseKeep 解析模型输出为 1-based 编号列表：容忍 ```json 围栏与前后杂音
// （取首个 [ 到末个 ] 之间的内容），越界/重复编号丢弃；空数组合法（全不相关）。
func parseKeep(text string, n int) ([]int, error) {
	s := strings.TrimSpace(text)
	i, j := strings.Index(s, "["), strings.LastIndex(s, "]")
	if i < 0 || j <= i {
		return nil, fmt.Errorf("未找到 JSON 数组: %q", truncateRunes(s, 80))
	}
	var idx []int
	if err := json.Unmarshal([]byte(s[i:j+1]), &idx); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %v", err)
	}
	seen := make(map[int]bool, len(idx))
	var out []int
	for _, k := range idx {
		if k < 1 || k > n || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/filterx/ -v`
Expected: PASS（7 个测试全绿）

- [ ] **Step 5: Commit**

```bash
git add internal/filterx/
git commit -m "feat(filterx): LLM 相关性后置过滤（只删不加，全程 fail-open）"
```

### Task 6: hook 接线——InjectForPrompt 接入过滤

**Files:**
- Modify: `internal/hook/core.go:222-223`（`hits = h` 之后、`if len(hits) > 0` 之前）
- Test: `internal/hook/core_test.go`（追加）

**Interfaces:**
- Consumes: `filterx.Filter`（Task 5）

- [ ] **Step 1: 写失败测试**

`internal/hook/core_test.go` 追加（需 import `net/http`、`net/http/httptest`、`encoding/json`，先读文件头确认现有 import）：

```go
// TestPromptLLMFilter LLM 后置过滤端到端：两条候选均被关键词准入，LLM 裁决只留第 2 条。
func TestPromptLLMFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "[2]"}}},
		})
	}))
	defer srv.Close()
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "甲.md", "---\ntitle: 甲条目\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n紫晶灵罗 词甲。\n")
	writeEntry(t, kbRoot, "乙.md", "---\ntitle: 乙条目\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n紫晶灵罗 词乙。\n")
	cfg := "[retrieve]\ntop_n = 3\n\n[llm]\nactive = \"测试\"\n[[llm.profiles]]\nname = \"测试\"\nkind = \"openai\"\nbase_url = \"" + srv.URL + "\"\nmodel = \"m\"\n"
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	out := InjectForPrompt(pc, "s-filter", projDir, "紫晶灵罗")
	if strings.Contains(out, "甲.md") {
		t.Fatalf("LLM 裁决丢弃的条目不不应注入, got: %q", out)
	}
	if !strings.Contains(out, "乙.md") {
		t.Fatalf("LLM 保留的条目应注入, got: %q", out)
	}
}
```

（注意：config.toml 内嵌 httptest URL 含 `:` 与 `/`，TOML 基本字符串用双引号包裹即可，无需转义。若 setupProject 的全局 OK_HOME 已含 [llm] 配置则以项目层覆盖为准——LoadMerged 项目层覆盖全局层。）

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/hook/ -run TestPromptLLMFilter -v`
Expected: FAIL（两条都注入，`甲.md` 仍在输出中）——**此断言必须先红，证明测试能区分接线前后行为**

- [ ] **Step 3: 实现**

`internal/hook/core.go` import 加 `"okryptos/internal/filterx"`；在 `hits = h`（:222）之后插入：

```go
		// LLM 后置过滤：对最终候选（分支过滤/冷却/截断后）做相关性裁决，只删不加。
		// 未配置 LLM 静默空转；超时/失败/解析异常 fail-open 原样放行（filterx 收口）。
		if len(hits) > 0 {
			kept, note := filterx.Filter(context.Background(), pc.Config, queryPrompt, hits)
			if note != "" {
				logErr("prompt filter: %s", note)
			}
			hits = kept
		}
```

（`context` 已在 core.go import 中，:8。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/hook/ -v`
Expected: PASS（含既有注入/冷却/门控测试不回归——过滤对无 LLM 配置的旧测试静默空转）

- [ ] **Step 5: Commit**

```bash
git add internal/hook/core.go internal/hook/core_test.go
git commit -m "feat(hook): 注入链路接入 LLM 后置过滤（fail-open，ok.log 可观测）"
```

---

## Part C：GUI 本地 LLM 与"识别意图"双使用中槽位（Tasks 7-11）

**模型选型结论**（2026-09-12 调研；一手来源：[ollama qwen3 tags 页](https://ollama.com/library/qwen3/tags)、[Qwen3 技术报告 arXiv:2505.09388](https://arxiv.org/abs/2505.09388)）：意图识别/相关性过滤是"输入几百 token、输出几个编号"的小任务，本地小模型足够。推荐（ollama 模型名）：

| 模型 | 体积 | 定位 |
|---|---|---|
| `qwen3:1.7b` | 1.4GB | **首选**：中文强、CPU 可跑（2026 第三方评测 Q4_K_M 约 25-40 tok/s，单次过滤延迟可接受） |
| `qwen3:0.6b` | 523MB | 最省资源：老机器/内存紧张可跑 |
| `qwen3:4b-instruct-2507` | 2.5GB | 输出最稳：2507 刷新版为非思考模型，严格 JSON 输出最可靠 |

注意：`qwen3:0.6b/1.7b` 原版是混合思考模型，思考过程会吃掉过滤的 max_tokens 预算（触发 Truncated → fail-open 保留全部，过滤形同空转）；追求稳定选 `4b-instruct-2507`。**在线低价模型（各厂 mini/nano 档）同样可用于识别意图**——"识别意图"勾选对 openai/anthropic/ollama 三种 kind 一视同仁。

**双使用中槽位设计**：`[llm]` 有两个"使用中"——普通 `active`（条目优化等手动场景）与 `active_filter`（识别意图：检索注入前的相关性过滤，每轮 prompt 自动调用）。profile 编辑表单里的"识别意图"勾选框决定它激活进哪个槽，两槽互不占用、可同时各有一个使用中。filterx 取 `FilterProfile()` 优先、空则回退 `ActiveProfile()`（未配专用档时保持 Part B 行为）。

### Task 7: config+setupx+filterx——双 active 槽位与识别意图标记

**Files:**
- Modify: `internal/config/config.go`（LLMProfile 加 Filter 字段；LLM 加 ActiveFilter 字段与 FilterProfile 方法）
- Modify: `internal/setupx/setupx.go`（照 `SetActiveLLM` :280 同构加 `SetActiveLLMFilter`；`DeleteLLMProfile` :314 同步清 active_filter）
- Modify: `internal/filterx/filterx.go`（Filter 的 profile 选择：FilterProfile 优先、回退 ActiveProfile）
- Test: `internal/config/config_test.go`（追加）、`internal/filterx/filterx_test.go`（追加）

**Interfaces:**
- Produces（Task 9/10 消费）:
  - `LLMProfile.Filter bool`（toml: `filter,omitempty`）— 识别意图标记
  - `LLM.ActiveFilter string`（toml: `active_filter,omitempty`）— 识别意图槽的"使用中"profile 名
  - `func (l LLM) FilterProfile() *LLMProfile` — 识别意图槽激活 profile；未配置/悬空返回 nil
  - `func SetActiveLLMFilter(name string) error`（setupx 包）— 空串=停用该槽

- [ ] **Step 1: 写失败测试**

`internal/config/config_test.go` 追加（先读文件头确认包名，通常为 `package config`）：

```go
func TestLLMFilterProfile(t *testing.T) {
	l := LLM{
		Active:       "普通",
		ActiveFilter: "意图",
		Profiles: []LLMProfile{
			{Name: "普通", Kind: "openai"},
			{Name: "意图", Kind: "ollama", Filter: true},
		},
	}
	if p := l.ActiveProfile(); p == nil || p.Name != "普通" {
		t.Fatalf("ActiveProfile: %+v", p)
	}
	if p := l.FilterProfile(); p == nil || p.Name != "意图" {
		t.Fatalf("FilterProfile: %+v", p)
	}
	l.ActiveFilter = "悬空"
	if p := l.FilterProfile(); p != nil {
		t.Fatalf("悬空 active_filter 应返回 nil: %+v", p)
	}
	if p := (LLM{}).FilterProfile(); p != nil {
		t.Fatalf("未配置应返回 nil: %+v", p)
	}
}
```

`internal/filterx/filterx_test.go` 追加：

```go
// TestFilterPrefersFilterProfile 识别意图槽位优先：active_filter 指向的 profile 被用于
// 过滤，普通 active 不被调用（两个服务器应答不同，裁决结果可区分走了哪个）。
func TestFilterPrefersFilterProfile(t *testing.T) {
	filterSrv := llmServer(t, "[2]", 0)
	defer filterSrv.Close()
	generalSrv := llmServer(t, "[1]", 0)
	defer generalSrv.Close()
	cfg := cfgWithLLM(generalSrv.URL)
	cfg.LLM.ActiveFilter = "意图"
	cfg.LLM.Profiles = append(cfg.LLM.Profiles,
		config.LLMProfile{Name: "意图", Kind: "openai", BaseURL: filterSrv.URL, Model: "m", Filter: true})
	kept, _ := Filter(context.Background(), cfg, "查询", testHits())
	if len(kept) != 1 || kept[0].Filename != "b.md" {
		t.Fatalf("应使用识别意图槽的裁决 [2]: %+v", kept)
	}
}
```

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/config/ -run TestLLMFilterProfile -v` 和 `go test ./internal/filterx/ -run TestFilterPrefersFilterProfile -v`
Expected: FAIL（`FilterProfile`/`Filter` 字段未定义）

- [ ] **Step 3: 实现**

`internal/config/config.go`：

(a) `LLMProfile`（:81-89）`MaxTokens` 字段后加：

```go
	// Filter 识别意图标记：true 时"设为使用中"进入 active_filter 槽（检索注入的
	// 意图识别/相关性过滤专用，每轮 prompt 自动调用），与普通 active 槽互不占用。
	Filter bool `toml:"filter,omitempty"`
```

(b) `LLM` 结构（:92-96）`Active` 字段后加：

```go
	// ActiveFilter 识别意图用途的"使用中"profile 名（建议本地小模型/低价在线模型）；
	// 空=filterx 回退 Active。
	ActiveFilter string `toml:"active_filter,omitempty"`
```

(c) `ActiveProfile` 方法（:99-109）后加：

```go
// FilterProfile 返回识别意图用途的激活 profile；未配置或 active_filter 悬空返回 nil
// （调用方 filterx 据此回退 ActiveProfile）。
func (l LLM) FilterProfile() *LLMProfile {
	if l.ActiveFilter == "" {
		return nil
	}
	for i := range l.Profiles {
		if l.Profiles[i].Name == l.ActiveFilter {
			return &l.Profiles[i]
		}
	}
	return nil
}
```

`internal/filterx/filterx.go` Filter 的 profile 选择段改为：

```go
	// 识别意图专用 profile 优先（[llm] active_filter 槽），未配置回退普通 active
	p := cfg.LLM.FilterProfile()
	if p == nil {
		p = cfg.LLM.ActiveProfile()
	}
	if p == nil {
		return hits, ""
	}
```

`internal/setupx/setupx.go`：先读 `SetActiveLLM`（:280）与 `DeleteLLMProfile`（:314）的实现，照其同构新增 `SetActiveLLMFilter(name string) error`（锁内 updateGlobalConfig，置 `cfg.LLM.ActiveFilter`，空串=停用该槽；name 非空且不存在于 profiles 时返回与 SetActiveLLM 同款错误）；`DeleteLLMProfile` 删除使用中项时除清 `Active` 外同步清 `ActiveFilter`（删的是哪个槽的激活项就清哪个，两者同名都清）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ ./internal/filterx/ ./internal/setupx/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/filterx/ internal/setupx/setupx.go
git commit -m "feat(config): [llm] 识别意图双使用中槽位（active_filter + profile filter 标记，filterx 专用槽优先）"
```

### Task 8: llmx——kind=ollama 协议档

**Files:**
- Modify: `internal/llmx/llmx.go`（New/Chat/错误文案 + normalizeOllamaBase）
- Modify: `internal/config/config.go:79-89`（LLMProfile.Kind 注释）
- Test: `internal/llmx/llmx_test.go`

**Interfaces:**
- Produces（Task 9/10 消费）: `config.LLMProfile.Kind` 接受 `"ollama"`；base_url 留空默认 `http://localhost:11434/v1`，缺 `/v1` 自动补

- [ ] **Step 1: 写失败测试**

`internal/llmx/llmx_test.go` 追加：

```go
func TestNormalizeOllamaBase(t *testing.T) {
	cases := map[string]string{
		"":                      "http://localhost:11434/v1",
		"http://192.168.1.5:11434": "http://192.168.1.5:11434/v1",
		"http://host:11434/":    "http://host:11434/v1",
		"http://host:11434/v1":  "http://host:11434/v1",
		"http://host:11434/v1/": "http://host:11434/v1",
	}
	for in, want := range cases {
		if got := normalizeOllamaBase(in); got != want {
			t.Errorf("normalizeOllamaBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChatOllama(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "pong"}}},
		})
	}))
	defer srv.Close()
	// base_url 不带 /v1：应自动补；无 api_key：ollama 忽略 Authorization
	c := New(config.LLMProfile{Kind: "ollama", BaseURL: srv.URL, Model: "qwen3"}, 0)
	rep, err := c.Chat(context.Background(), "sys", "usr", 100)
	if err != nil || rep.Text != "pong" {
		t.Fatalf("got (%q, %v)", rep.Text, err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q（应自动补 /v1）", gotPath)
	}
}
```

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/llmx/ -run "TestNormalizeOllamaBase|TestChatOllama" -v`
Expected: FAIL（`normalizeOllamaBase` 未定义；kind=ollama 报"未知 llm 类型"）

- [ ] **Step 3: 实现**

`internal/llmx/llmx.go`：

(a) `New`（:28-38）在 anthropic 分支前加：

```go
	if p.Kind == "ollama" {
		p.BaseURL = normalizeOllamaBase(p.BaseURL)
	}
```

(b) 文件尾追加：

```go
// normalizeOllamaBase ollama 的 OpenAI 兼容端点挂在 /v1 下：留空按本机默认，
// 缺 /v1 后缀自动补上（用户按习惯填 http://host:11434 会 404，embedx 修过同款）。
func normalizeOllamaBase(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = "http://localhost:11434"
	}
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base
}
```

(c) `Chat`（:64-71）改：

```go
	switch c.p.Kind {
	case "openai", "ollama":
		return c.chatOpenAI(ctx, system, user, maxTokens)
	case "anthropic":
		return c.chatAnthropic(ctx, system, user, maxTokens)
	default:
		return Reply{}, fmt.Errorf("未知 llm 类型: %q（openai|anthropic|ollama）", c.p.Kind)
	}
```

`internal/config/config.go:79-83` LLMProfile 注释与 Kind 字段注释改：

```go
// LLMProfile 一个大模型服务配置（生成场景：条目优化、检索过滤等）。Kind 三种：
// openai（/chat/completions 兼容）| anthropic（/v1/messages 兼容）|
// ollama（本地，OpenAI 兼容协议，免 api_key，base_url 留空默认 localhost:11434）。
type LLMProfile struct {
	Name        string `toml:"name"`
	Kind        string `toml:"kind"` // openai | anthropic | ollama
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/llmx/ -v`
Expected: PASS（含既有 openai/anthropic 测试不回归）

- [ ] **Step 5: Commit**

```bash
git add internal/llmx/llmx.go internal/llmx/llmx_test.go internal/config/config.go
git commit -m "feat(llmx): kind=ollama 本地档（免 key，自动补 /v1，默认 localhost:11434）"
```

### Task 9: GUI 后端——双槽位端点 + ollama 校验放行

**Files:**
- Modify: `internal/gui/llm.go`（llmProfileJSON 加 Filter；apiLLMGet 响应加 active_filter；apiLLMProfileSave 接受 filter 并放行 ollama；apiLLMActive 支持 slot）
- Test: `internal/gui/llm_test.go`（追加）

**Interfaces:**
- Consumes: `LLM.ActiveFilter` / `LLMProfile.Filter` / `setupx.SetActiveLLMFilter`（Task 7）；llmx ollama（Task 8）
- Produces（Task 10 前端消费的 API 契约）:
  - `GET /api/llm` → `{active, active_filter, profiles:[{name,kind,base_url,model,api_key(掩码),temperature,max_tokens,filter,active}]}`
  - `POST /api/llm/profile` 请求体加 `filter bool`
  - `POST /api/llm/active` 请求体加 `slot string`（`"filter"` = 识别意图槽；缺省/`"general"` = 普通槽；name 空串 = 清对应槽）

- [ ] **Step 1: 写失败测试**

先读 `internal/gui/llm_test.go` 既有保存/激活测试的写法（env 构造、请求辅助、断言模式），按其模式追加三个测试：

```go
// TestLLMProfileSaveOllama ollama 档：base_url 可留空、免 api_key。
// POST /api/llm/profile {"name":"本地","kind":"ollama","base_url":"","model":"qwen3:1.7b","activate":false}
// 断言：200（非 400），GET /api/llm 的 profiles 含 kind=ollama。

// TestLLMProfileSaveFilterFlag 识别意图标记随保存回显。
// POST /api/llm/profile {"name":"意图","kind":"openai","base_url":"http://x","model":"m","filter":true}
// 断言：200，GET /api/llm 的该 profile filter=true。

// TestLLMActiveSlots 双槽位：POST /api/llm/active {"name":"意图","slot":"filter"} 后
// GET 回显 active_filter="意图" 且 active 不变；再 POST {"name":"","slot":"filter"} 清空该槽。
// 断言：各步 200 且 GET 字段符合预期。
```

（实现者：照抄文件内最近的 save/active 成功用例，仅替换 body 与断言；复用既有 env/请求 helper。）

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/gui/ -run "TestLLMProfileSaveOllama|TestLLMProfileSaveFilterFlag|TestLLMActiveSlots" -v`
Expected: FAIL（400 "类型仅支持 openai | anthropic"；filter/slot 字段被忽略导致断言失败）

- [ ] **Step 3: 实现**

`internal/gui/llm.go`：

(a) `llmProfileJSON`（:42-51）加字段：

```go
	Filter      bool   `json:"filter"` // 识别意图标记（active_filter 槽候选）
```

`apiLLMGet`（:80-92）profiles 组装加 `Filter: p.Filter`，响应 map 改：

```go
	writeJSON(w, http.StatusOK, map[string]any{
		"active": cfg.LLM.Active, "active_filter": cfg.LLM.ActiveFilter, "profiles": profiles})
```

(b) `apiLLMProfileSave`：请求结构体（:98-107）加 `Filter bool \`json:"filter"\``；校验段（:114-121）改：

```go
	if req.Name == "" || req.Model == "" {
		writeErr(w, http.StatusBadRequest, "名称、模型不能为空")
		return
	}
	if req.Kind != "openai" && req.Kind != "anthropic" && req.Kind != "ollama" {
		writeErr(w, http.StatusBadRequest, "类型仅支持 openai | anthropic | ollama")
		return
	}
	if req.Kind != "ollama" && req.BaseURL == "" {
		writeErr(w, http.StatusBadRequest, "base_url 不能为空（ollama 可留空，默认 localhost:11434）")
		return
	}
```

`SaveLLMProfile` 调用（:130-133）的 `config.LLMProfile` 字面量加 `Filter: req.Filter`。

(c) `apiLLMActive`（:160-172）改：

```go
func (h *Handler) apiLLMActive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Slot string `json:"slot"` // "filter"=识别意图槽；缺省=普通槽
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var err error
	if req.Slot == "filter" {
		err = setupx.SetActiveLLMFilter(req.Name)
	} else {
		err = setupx.SetActiveLLM(req.Name)
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.apiLLMGet(w, r)
}
```

(d) `apiLLMTest` URL 校验（:216-219）改：

```go
	if !(req.Kind == "ollama" && req.BaseURL == "") && !httpBaseURLOK(req.BaseURL) {
		writeErr(w, http.StatusBadRequest, "base_url 必须是 http/https URL")
		return
	}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/gui/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gui/llm.go internal/gui/llm_test.go
git commit -m "feat(gui): LLM 双使用中槽位端点（active_filter/slot）+ ollama 档校验放行"
```

### Task 10: GUI 前端——识别意图勾选 + 双使用中徽标 + ollama 档 + 推荐模型提示

**Files:**
- Modify: `web/app.js`（LLM 弹窗 :4713-4830 区域 + i18n 文案键）
- Modify: `dist/web/app.js`（构建期由 web/ 同步，手工验证前需同步——见 Task 11 的既定坑）

**Interfaces:**
- Consumes: Task 9 的 API 契约（`active_filter` / `filter` / `slot` / ollama）

**设计要点（对应用户需求）：**
- profile 编辑表单加"识别意图"勾选框，勾选后说明文案讲清楚用途：此配置的"使用中"独立于普通 LLM，专用于每轮提问前的检索相关性过滤（自动调用），在线/本地模型都可以，建议本地小模型或低价在线模型；
- 列表行的"使用中"按槽位分两个徽标：普通槽"使用中"、识别意图槽"使用中·意图"，可同时在不同 profile 上各亮一个；
- 同一 profile 两槽互斥（设进一槽时清另一槽的同名，避免普通场景误用过滤小模型）；
- 两段式纪律：勾选/设激活都改 draft，点「确定」经 llmApply 统一落盘。

- [ ] **Step 1: i18n 文案**

Grep `kindOpenai` 定位文案字典（`web/app.js`，与 `lTitle`(:102)、`typeOllama`(:93) 同区），在 `kindOpenai`/`kindAnthropic` 旁追加：

```js
kindOllama:"Ollama（本地）",
tagOllama:"Ollama",
eActiveFilter:"使用中·意图",
lFilterChk:"识别意图（检索过滤专用）",
lFilterHelp:"勾选后此配置的「使用中」独立于普通 LLM：专用于每轮提问前的检索相关性过滤（自动调用、按次计费/占本机资源），max_tokens 保持 0。在线低价模型或本地小模型均可；推荐本地模型（ollama）：qwen3:1.7b（1.4GB，首选）/ qwen3:0.6b（523MB，最省）/ qwen3:4b-instruct-2507（2.5GB，非思考版输出最稳）。",
```

- [ ] **Step 2: draft 与列表行（双槽位徽标 + 互斥）**

`openLlmModal`（:4716-4722）draft 改：

```js
  llmDraft = { active:l.active||"", activeFilter:l.active_filter||"",
    profiles:(l.profiles||[]).map(p=>({ name:p.name, kind:p.kind, base:p.base_url||"",
      model:p.model||"", key:"", temperature:p.temperature||"", maxTokens:p.max_tokens||0,
      filter:!!p.filter })) };
```

列表行徽标区（:4762-4768）改双槽位：

```js
    const inGeneral = llmDraft.active===p.name, inFilter = llmDraft.activeFilter===p.name;
    if(inGeneral || inFilter){
      acts.appendChild(Object.assign(el("span","chip on"),
        {textContent:inFilter?t("eActiveFilter"):t("eActive")}));
    } else {
      const sa = el("button","btn"); sa.textContent = t("eSetActive"); sa.style.padding="2px 10px";
      sa.onclick = ()=>{
        // 按 profile 的识别意图标记落槽；同一 profile 两槽互斥
        if(p.filter){ llmDraft.activeFilter = p.name; if(llmDraft.active===p.name) llmDraft.active = ""; }
        else { llmDraft.active = p.name; if(llmDraft.activeFilter===p.name) llmDraft.activeFilter = ""; }
        render();
      };
      acts.appendChild(sa);
    }
```

删除按钮（:4773-4778）的 active 清理同步加：`if(llmDraft.activeFilter===p.name) llmDraft.activeFilter = "";`

- [ ] **Step 3: 编辑表单（ollama 档 + 识别意图勾选）**

kind 下拉（:4791-4793）改：

```js
    [["openai",t("kindOpenai")],["anthropic",t("kindAnthropic")],["ollama",t("kindOllama")]].forEach(([v,label])=>{
```

onchange（:4795）触发重渲：

```js
    sel.onchange = ()=>{ llmForm.kind = sel.value; render(); };
```

base/key 行（:4798-4802）改条件渲染：

```js
    const baseIn = ptext(llmForm.base||"", v=>{ llmForm.base=v; }, "300px");
    if(llmForm.kind==="ollama") baseIn.placeholder = "http://localhost:11434（留空默认）";
    m.appendChild(prow(t("fBase"), baseIn));
    m.appendChild(prow(t("fModel"), ptext(llmForm.model||"", v=>{ llmForm.model=v; }, "300px")));
    if(llmForm.kind!=="ollama"){
      const keyIn = ptext(llmForm.key||"", v=>{ llmForm.key=v; }, "300px");
      keyIn.placeholder = llmEdit>=0 ? t("keySaved") : "api_key";
      m.appendChild(prow(t("fKey"), keyIn));
    }
```

识别意图勾选框 + 说明（插在 max_tokens 行 :4804 之后）：

```js
    const fchk = el("input"); fchk.type = "checkbox"; fchk.checked = !!llmForm.filter;
    fchk.onchange = ()=>{ llmForm.filter = fchk.checked; };
    const fchkRow = el("div","prow");
    fchkRow.appendChild(Object.assign(el("span","k"),{textContent:t("lFilterChk")}));
    fchkRow.appendChild(fchk);
    m.appendChild(fchkRow);
    if(llmForm.filter){
      const help = el("div","hint"); help.textContent = t("lFilterHelp");
      m.appendChild(help);
    }
```

（`hint` 样式类若不存在，用弹窗内既有说明文字的同款车型——Grep `class","hint"` 或改用 `"fb2"`；实现时确认。）

- [ ] **Step 4: llmApply 双槽位落盘**

`llmApply()`（:4727-4746）改三处：
(a) profile POST 体加 `filter:!!p.filter`；
(b) 删除循环后、普通槽 active 逻辑保留，slot 参数补上：

```js
  if(draft.active !== serverActive){
    await api("/api/llm/active", { method:"POST", body:{ name:draft.active, slot:"general" } });
  }
```

(c) 追加识别意图槽 diff：

```js
  let serverFilter = old.active_filter || "";
  if(serverFilter && !keep[serverFilter]) serverFilter = "";
  if(draft.activeFilter !== serverFilter){
    await api("/api/llm/active", { method:"POST", body:{ name:draft.activeFilter, slot:"filter" } });
  }
```

- [ ] **Step 5: 前端静态检查**

Run: `node --check web/app.js`（若本机有 node）或浏览器开 GUI 控制台看语法错误
Expected: 无语法错误；Grep 确认无遗留 `llmForm.kind!=="openai"` 之类的两档硬编码

- [ ] **Step 6: Commit**

```bash
git add web/app.js
git commit -m "feat(gui): LLM 弹窗识别意图勾选（双使用中槽位）+ ollama 本地档 + 推荐模型提示"
```

### Task 11: 手工验证（真实 GUI 端到端）

**Files:**
- Modify: `dist/web/app.js`（同步自 web/，验证用，不单独提交）

- [ ] **Step 1: 构建并同步**

既定坑：裸 `go build` 不同步 `dist/web`，二进制新页面旧。验证前必须同步：

```bash
cp web/app.js dist/web/app.js
go build -o dist/okd.exe ./cmd/okd
./dist/okd.exe &
```

- [ ] **Step 2: 双槽位验证**

打开 GUI → 设置 → 模型配置（LLM）:
1. 新增 profile：类型 "Ollama（本地）"，base_url 留空，模型填本机 ollama 已有模型（如 `qwen3:1.7b`），勾选"识别意图"→ 说明文案显示 → 确定保存；
2. 该 profile 点「设为使用中」→ 徽标显示"使用中·意图"；另选一个普通 profile 设为使用中 → 两个徽标同时存在、互不顶掉；
3. `curl` 或浏览器查 `GET /api/llm`：`active` 与 `active_filter` 分别是两个名字；
4. 识别意图 profile 的「测试连接」应通（本机无 ollama 时预期报连接失败文案而非校验错误）。

- [ ] **Step 3: 过滤端到端验证**

在挂了 hook 的宿主里发一条 prompt（知识库有候选命中时），查 `~/.okryptos/ok.log`：
Expected: 出现 `prompt filter: LLM 裁决：...` 行，且裁决走的是识别意图槽的 profile（本地 ollama 时 ollama 日志/任务管理器可见调用）。

- [ ] **Step 4: 回归确认**

普通 LLM 功能（条目 AI 优化）仍走普通槽 profile 正常；删除识别意图 profile 后 `active_filter` 自动清空。

---

## Task 12: 全量回归 + changelog

**Files:**
- Modify: `docs/changelogs/`（按既有惯例新增/追加，先读最新一个文件确认格式与当前版本号）

- [ ] **Step 1: 全量测试**

Run: `go build ./...` 然后 `go test ./internal/...`（不接管道）
Expected: 全部 PASS；agentx 包在 Windows 上若有平台相关跳过属既有行为（知识库：Linux 跑整包红的平台双态约定已修，但留意）

- [ ] **Step 2: changelog**

读 `docs/changelogs/` 最新文件确认格式后，按惯例记录变更：方案A 覆盖度准入（修短口语 prompt 误注入）、LLM 后置过滤（[retrieve.filter]）、LLM 识别意图双使用中槽位 + ollama 本地档。不硬折行。

- [ ] **Step 3: Commit**

```bash
git add docs/changelogs/
git commit -m "docs(changelog): 检索精度三件套（覆盖度准入/LLM 后置过滤/识别意图槽+ollama 本地档）"
```

---

## Self-Review 记录

- **设计修正（执行期，Task 3）**：原口径 `min(n, max(2, ⌈n×0.5⌉))` 经实现者探针实证与重叠 bigram 分词不自洽（跨词界假词稀释 n 约 2 倍：n=2 退化 AND 灭 13 个预存测试、计划正例 n=6 need=3 必拒；纯虚词门控因"是就"漏表不置位）。控制器裁决重标定为 `max(1, ⌈n×0.25⌉)`（0.25 ≈ 实词 50%，依据"门控太严比太松更伤"教训 + Part B LLM 过滤兜精度），停用表补"是就"。已同步修订 Task 1/2 的公式、默认值与测试期望（落地于 6e2c503）。若误注入回升，优先调 `retrieve.coverage.min_ratio`。
- **Spec 覆盖**:方案A → Tasks 1-3；后置过滤 → Tasks 4-6;GUI 本地 LLM + 识别意图双槽位 → Tasks 7-11（模型选型结论在 Part C 题头，含在线模型可用的说明）。builtin 托管档已显式裁出（见范围说明，立项可直接用本选型）。
- **类型一致性**:`EffectiveTerms`/`RequiredCoverage`(Task 1)→ Task 3 调用签名一致；`RetrieveCoverage`/`RetrieveFilter` 字段名 Task 2/4 定义与 Task 3/5 使用一致；`filterx.Filter` 签名 Task 5 定义与 Task 6 调用一致；`LLMProfile.Filter`/`LLM.ActiveFilter`/`FilterProfile`/`SetActiveLLMFilter`(Task 7 定义）与 Task 9 后端、Task 10 前端（`filter`/`active_filter`/`slot` JSON 键）一致；`normalizeOllamaBase` Task 8 定义与测试一致。Task 5 先用 ActiveProfile、Task 7 切换 FilterProfile 优先，编译链不断。
- **已知陷阱已内建**:info 整体赋值冲掉 CoverageRejected(Task 3 Step 3e);toml 数组合并混叠（Task 2 Step 4);dist/web 不同步（Task 11 Step 1);temperature 不传（Task 5);混合思考模型吃 max_tokens 预算（Part C 题头选型注意 + lFilterHelp 文案）;GUI 两段式（Task 10 设计要点）;测试先红（每个 Task Step 2)。
