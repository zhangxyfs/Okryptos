# 外部记忆系统借鉴第一批 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 `docs/2026-08-23-memory-systems-synthesis.md` 行动建议的优先级 1-5 项：沉淀提示词升级、注入段序固定、检索查询净化、per-entry 注入预算、propose 同域候选提示。

**Architecture:** 全部为小步增量改动，集中在 `internal/setupx`（技能提示词）、`internal/hook/core.go`（注入组装）、`internal/retrieve`（新增查询净化）、`internal/config` + `internal/store`（单条预算）、`internal/cli`（propose 候选提示）。不改检索准入/融合逻辑，不引入 LLM 调用，不动人审草稿闸门。

**Tech Stack:** Go 单体，SQLite+FTS5（索引），测试用标准 `testing` 包与各包既有夹具。

## Global Constraints

- **fail-open**：hook 链路任何内部错误仅记 ok.log（`logErr`），绝不阻断 agent 会话。
- **新配置缺省 = 旧语义**：`entry_max_tokens` 缺省 0 = 不限制。
- **TDD + 判别性测试**：行为变化类新测试必须先验证对旧代码变红（项目已有教训：非判别性测试是坑）。
- 代码注释与注入文案用中文，风格与周边一致。
- 提交信息用 Conventional Commits（如 `feat(hook): ...`）。
- 每个 Task 完成后跑 `go test ./...` 全绿再提交；改动注入格式的 Task 另跑 `go test ./tests/e2e/`（默认行为变化的兼容扫荡必须含 e2e 层——已有教训）。
- hook 测试夹具复用 `internal/hook/hook_test.go` 的 `setupProject` / `writeEntry` / `initGitRepo` / `runGit` / `gitHead`；cli 测试复用 `internal/cli/cli_test.go` 的 `chdir` 与 agent home 隔离环境变量块（必须全套导出，漏一个会写真实用户配置——已有教训）。

**不在本计划内**（源报告已标注延后/备用，勿顺手做）：GUI 沉淀转化/检索健康统计页（行动 6）、feedback v2 升权（等宿主 read 派发接通）、GUI 侧草稿重复提示、wiki 条目技能目录导出、AGENTS.md 契约条款。

---

## 已决议：auto 自省 turn_interval 默认 5 → 3，保留配置（用户 2026-08-23 拍板方案 a）

**背景**：用户曾提议定死 1 轮并删掉配置项，经分析否决——`CheckStop`（`internal/hook/core.go:383`）触发条件是"本会话 Touched 非空且距上次提醒满 interval"，Touched 一旦非空整会话保持，定死 1 = 每轮 Stop 都软阻断，提醒疲劳与"琐碎任务不要沉淀"门控打架；删配置要动 config/CLI/GUI/web/测试/文档六个面且破坏存量用户 config.toml 兼容。短会话覆盖率问题的可行解只有调默认值（Stop hook 无法区分"本轮结束"与"会话结束"，做不了结束兜底提醒）。

**执行规格**（并入 Task 1 一并提交）：

1. `internal/config/config.go` `Default()` 中 `Capture.TurnInterval` 默认值 5 → 3；字段注释同步。
2. 同步提及默认值 5 的文案：`ok capture` 帮助/输出、GUI 经验沉淀卡（`web/app.js`）、README（若提及）。
3. 测试同步：`internal/config` 与 `internal/hook` 中钉了默认值 5 的断言改为 3（只允许同步数值断言，语义断言不动）。
4. 行为变化属默认值变化，兼容扫荡：`go test ./...` + `go test ./tests/e2e/`。

---


### Task 1: propose 技能提示词升级 + auto 自省提醒琐碎门控 + turn_interval 默认 5→3

源报告 §2.1（Acontext B1-B4 + TencentDB A4 + OpenMemory §三.4）。propose 技能目前以 Go 字符串内联在 `internal/setupx/setupx.go` 的 `skillTemplates` map（约 :392）；wiki 技能已是 `go:embed` 文件（`internal/setupx/skills/openknowledge-wiki/SKILL.md`，:381-385）。本任务把 propose 模板也转为 go:embed 文件并升级内容——与既有模式一致、提示词可评审。另含已决议的 turn_interval 默认值调整（规格见上方"已决议"一节）。

**Files:**
- Create: `internal/setupx/skills/openknowledge-propose/SKILL.md`
- Modify: `internal/setupx/setupx.go`（:381-392 区域，propose 模板改 go:embed）
- Modify: `internal/hook/core.go:386`（auto 自省提醒文案加琐碎反例）
- Test: `internal/setupx/setupx_test.go`、`internal/hook/core_test.go`

**Interfaces:**
- Consumes: 现有 `wikiSkillTemplate` 的 go:embed 模式；`skillTemplates` map（`InstallSkills` 遍历它写盘，`{{EXE}}` 占位符运行时替换）。
- Produces: `skillTemplates["openknowledge-propose"]` 内容变为新文件内容；CheckStop 的 auto 提醒文案含"琐碎"反例。

- [ ] **Step 1: 写失败测试（setupx 侧）**

在 `internal/setupx/setupx_test.go` 追加（确认该文件 package 声明为 `package setupx` 后同包追加）：

```go
// TestProposeSkillTemplateDiscipline 钉住 propose 技能提示词的写作纪律要素
// （外部调研借鉴：第三人称/作用域/查重路由/琐碎门控——见
// docs/2026-08-23-memory-systems-synthesis.md §2.1）。
func TestProposeSkillTemplateDiscipline(t *testing.T) {
	tpl, ok := skillTemplates["openknowledge-propose"]
	if !ok {
		t.Fatal("skillTemplates 缺 openknowledge-propose")
	}
	for _, kw := range []string{"第三人称", "作用域", "查重", "琐碎"} {
		if !strings.Contains(tpl, kw) {
			t.Errorf("propose 技能模板应含 %q", kw)
		}
	}
}
```

若该文件尚无 `strings` 导入则补上。

- [ ] **Step 2: 写失败测试（自省提醒侧）**

在 `internal/hook/core_test.go` 追加（仿 `TestTrackTouchedAndCheckStopRemind` 的夹具；`writeCaptureConfig` helper 已存在于 hook_test.go）：

```go
// TestCheckStopReminderTriviaGate auto 自省提醒须带琐碎任务反例
// （TencentDB/Acontext 调研：琐碎门控给显式反例，减少低质量草稿）。
func TestCheckStopReminderTriviaGate(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "条目.md", "---\ntitle: 测试条目\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n正文。\n")
	writeCaptureConfig(t, kbRoot, "auto", 1)
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	TrackTouched(pc, "s-trivia", "write", filepath.Join(projDir, "a.go"))
	reason, _ := CheckStop(pc, "s-trivia")
	if reason == "" {
		t.Fatal("auto 模式有文件修改且满间隔应提醒")
	}
	if !strings.Contains(reason, "琐碎") {
		t.Errorf("自省提醒应含琐碎任务反例，got: %q", reason)
	}
}
```

先确认 `writeCaptureConfig` 的实际签名（读 hook_test.go），若签名不同按实际调整调用。

- [ ] **Step 3: 跑测试确认变红**

Run: `go test ./internal/setupx/ -run TestProposeSkillTemplateDiscipline -v`
Expected: FAIL（模板不含"第三人称"等）
Run: `go test ./internal/hook/ -run TestCheckStopReminderTriviaGate -v`
Expected: FAIL（reason 不含"琐碎"）

- [ ] **Step 4: 创建新技能文件**

创建 `internal/setupx/skills/openknowledge-propose/SKILL.md`（完整内容，`{{EXE}}` 占位符与 wiki 技能同款约定）：

````markdown
---
name: openknowledge-propose
description: 把本次会话中沉淀的经验作为草稿条目提议进 OpenKnowledge 知识库（ok propose，待人批准）。当解决了一个非显而易见的问题、踩到坑、发现项目隐藏约定，或新需求/新功能/架构变化定稿需要沉淀时使用。
---

# openknowledge-propose

## 先分类：经验型还是结构型

- **经验型**（踩坑、隐藏约定、非显而易见问题的解法）→ 走本技能 ok propose 记草稿。
- **结构型**（新功能、新模块、新子系统、重要架构/流程变化）→ 不是草稿，改用 openknowledge-wiki 技能新增/更新 wiki 条目（同名 add --force 重写）。
- **两者兼有** → 都记：wiki 条目记"是什么/怎么协作"，草稿记"坑"。

## 何时提议

- 解决了一个非显而易见的问题（排查过程值得复用）
- 踩到坑（环境、依赖、工具链的隐性陷阱）
- 发现了项目的隐藏约定（代码里没有写明但必须遵守的规则）

## 何时不要提议

- 日常例行操作（常规增删改、跑测试、格式化）
- **琐碎任务**：闲聊、一次性问答、纯执行类操作——没有复用价值的不沉淀
- 知识库已有的内容——见下方"查重"

## 沉淀前自检（按顺序过一遍再写）

1. **值得吗**：这条经验下次遇到能省时间吗？有复用价值就提——门槛宁低勿高，草稿有人审兜底质量，门控太严会饿死沉淀（外部系统实测教训：过严门控覆盖率仅 46%）。但未验证的猜测不得写成结论；建议与已确认的决策分开写。
2. **查重**：先执行 `"{{EXE}}" search <关键词>` 查同域条目。有同域条目时**优先建议更新/合入该条目**（在草稿正文里写明"建议并入《标题》"并给出合并后文本），不要平行新建窄条目——少而丰富 > 多而零碎。若输出末尾出现"暂无 wiki 条目覆盖"提示行且内容属于结构型，告诉用户这是知识空白、建议用 openknowledge-wiki 技能补 wiki。
3. **复述**：propose 前先向用户复述将写入的草稿全文，并说明为何不合入已有条目。

## 写作纪律

- **第三人称**：条目会被未来会话里的其他 agent 读，"我"会被误认成它自己的经历。
- **作用域**：写明这条经验在哪个项目/工具/环境下成立（防过度泛化）。
- 标题即检索入口：写清症状/场景关键词，不要只写抽象主题。

## 命令

先把正文写入一个临时 Markdown 文件（纯正文，不要带 front matter），再用 Bash 工具执行：

    "{{EXE}}" propose --title <标题> --type pitfall --tags <逗号分隔> --file <正文.md>

正文很短时可直接用 `--body`（与 `--file` 二选一）：

    "{{EXE}}" propose --title <标题> --type note --body <正文>

type 取值：rule（规则）| pitfall（踩坑）| note（笔记）| reference（参考资料）。

提议成功后告诉用户："已记为草稿，待批准"（草稿不参与检索注入，用户在 GUI 点"采纳"或执行 `ok approve` 后转正）。
````

- [ ] **Step 5: setupx.go 改 go:embed**

`internal/setupx/setupx.go` 中，在既有 wiki embed 块旁加：

```go
//go:embed skills/openknowledge-propose/SKILL.md
var proposeSkillTemplate string
```

并在 `skillTemplates` map 初始化之后（`skillTemplates["openknowledge-wiki"] = wikiSkillTemplate` 同处）加：

```go
skillTemplates["openknowledge-propose"] = proposeSkillTemplate
```

然后**删除** map 字面量里 `openknowledge-propose` 的内联长字符串（约 :392）。capture 模板保持内联不动（本任务不改它的内容）。

- [ ] **Step 6: 自省提醒文案加琐碎反例**

`internal/hook/core.go:386` 的 reason 字符串改为：

```go
reason = "本会话修改过文件。请回顾是否有值得记录的经验（非显而易见的坑或解法；琐碎任务不要沉淀：闲聊、一次性问答、例行操作），有则立即运行 ok propose 记录草稿条目；没有则继续。"
```

- [ ] **Step 7: 跑测试确认变绿**

Run: `go test ./internal/setupx/ ./internal/hook/ -v -run "TestProposeSkillTemplateDiscipline|TestCheckStopReminderTriviaGate"`
Expected: 两个 PASS
Run: `go test ./internal/setupx/ ./internal/hook/`
Expected: 整包 PASS（注意 setupx 既有测试若断言模板内容需同步修正——只允许同步内容断言，不得放松语义断言）

- [ ] **Step 8: Commit**

```bash
git add internal/setupx/skills/openknowledge-propose/SKILL.md internal/setupx/setupx.go internal/setupx/setupx_test.go internal/hook/core.go internal/hook/core_test.go
git commit -m "feat(setupx): propose 技能提示词升级——价值自评低门槛/琐碎反例/查重路由/第三人称/作用域，auto 自省提醒加琐碎门控"
```

---

### Task 2: 注入段序固定——检索命中块永远沉底

源报告 §3 A5（prompt-cache 稳定性）。当前 `InjectForPrompt` 的段序是 [分支上下文行] + mandatory + INDEX + 检索命中块 + nudge/merged/语义退化提示——一次性提示行排在检索块**之后**，提示消失时令前缀字节变化；且检索块（每轮变动）不在最末，其后的提示行每次出现/消失都破坏缓存后缀。改造为固定段序：[分支上下文行] → mandatory → INDEX → 一次性提示行 → 检索命中块（沉底）。Task 3 的查询净化依赖"检索块恒为末段"这一约定。

**Files:**
- Modify: `internal/hook/core.go:91`（声明区加 `hitsText` builder）、:213-237（检索块写入 `hitsText` 而非 `restText`）、:238-248（分段截断装配）、:321-322（return 前追加沉底检索块）
- Test: `internal/hook/core_test.go`

**Interfaces:**
- Consumes: 现有 `store.TruncateToBudget` / `store.EstimateTokens`（`internal/store/store.go:37,54`，已确认 rune 安全——`[]rune` 切片截断）。
- Produces: 注入文本段序约定（Task 3 依赖）：检索命中块以 `## 相关知识（需要全文时读取对应文件）` 开头、恒为注入文本最后一个段。

- [ ] **Step 1: 写失败测试**

在 `internal/hook/core_test.go` 追加。夹具仿 `hook_test.go` 的 wiki nudge 测试（`initGitRepo` + `wiki.SaveState` + `stale_commits=1`）：游标记在首个 commit、再空提交一次使 Behind=1，触发"wiki 已落后"nudge；同时 prompt 命中检索条目。断言 nudge 行在检索块**之前**（旧代码 nudge 在检索块之后 → 变红）：

```go
// TestInjectHitsBlockLast 段序固定：一次性提示行（wiki nudge）在检索命中块之前，
// 检索块永远沉底（保 prompt cache 前缀稳定；净化约定依赖此序）。
func TestInjectHitsBlockLast(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	initGitRepo(t, projDir, 1)
	first := gitHead(t, projDir)
	runGit(t, projDir, "commit", "-q", "--allow-empty", "-m", "second")
	if err := wiki.SaveState(filepath.Join(kbRoot, "state"), &wiki.State{
		BaseBranch: "master",
		Cursors:    map[string]wiki.BranchCursor{"master": {LastCommit: first}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[wiki]\nstale_commits = 1\n[retrieve]\ndedup_turns = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\n独角兽紫晶 OrderQuirk 词。\n")
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	out := InjectForPrompt(pc, "s-order", projDir, "OrderQuirk 是什么")
	nudgeIdx := strings.Index(out, "wiki 已落后")
	hitsIdx := strings.Index(out, "## 相关知识")
	if nudgeIdx < 0 {
		t.Fatalf("应触发 wiki 落后 nudge，got: %q", out)
	}
	if hitsIdx < 0 {
		t.Fatalf("应有检索命中块，got: %q", out)
	}
	if hitsIdx < nudgeIdx {
		t.Errorf("检索命中块应沉底（在 nudge 之后），got: %q", out)
	}
}
```

注意：`wiki` 包导入、`initGitRepo`/`gitHead`/`runGit` 的实际签名先读 `hook_test.go` 确认（若 `runGit` 无返回值或分支名不是 master，按实际夹具调整）。

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/hook/ -run TestInjectHitsBlockLast -v`
Expected: FAIL（旧代码 nudge 在检索块之后）

- [ ] **Step 3: 实现段序固定**

`internal/hook/core.go` 三处改动：

a) :91 声明区：

```go
var mandatoryText, restText, hitsText strings.Builder
```

b) :213-225 检索块写入目标由 `restText` 改为 `hitsText`（两处置换 `restText.` → `hitsText.`，即 `"## 相关知识..."` 头与指针行循环、`restText.WriteString("\n")`）；`names` 收集、`RecordEvents`、`state.Update` 挂账逻辑原地不动（事件在截断前记录，语义同旧）。

c) :238-248 装配段替换为：

```go
	// L4：mandatory 永不被截断；INDEX 与检索块在剩余预算内分段截断。
	// 段序固定（保上游 prompt cache 前缀跨轮字节一致）：
	// [分支上下文行] → mandatory → INDEX → 一次性提示行 → 检索命中块（恒沉底，
	// 每轮变动的内容放最后；净化约定依赖此序，见 retrieve.CleanQuery）。
	budget := pc.Config.Inject.MaxTokens
	out := ""
	restBudget := budget
	if mandatoryText.Len() > 0 {
		out = mandatoryText.String()
		restBudget = budget - store.EstimateTokens(out)
	}
	idxOut := ""
	if restBudget > 0 {
		idxOut = store.TruncateToBudget(restText.String(), restBudget)
		out += idxOut
	}
```

d) :321 `return out` 之前追加沉底段（在分支上下文行、nudge、merged、语义退化各段之后）：

```go
	// 检索命中块沉底：预算 = 其余段预算减 INDEX 实耗；一次性提示行不占检索预算
	// （与旧行为一致：提示行在预算截断之外追加）。
	if hitsText.Len() > 0 {
		if hitsBudget := restBudget - store.EstimateTokens(idxOut); hitsBudget > 0 {
			out += store.TruncateToBudget(hitsText.String(), hitsBudget)
		}
	}
	return out
```

- [ ] **Step 4: 跑测试确认变绿 + 整包回归**

Run: `go test ./internal/hook/ -run TestInjectHitsBlockLast -v`
Expected: PASS
Run: `go test ./internal/hook/`
Expected: 整包 PASS（若有旧断言依赖"nudge 在检索块之后"的顺序，只允许调整顺序相关断言，语义断言不得放松）

- [ ] **Step 5: e2e 兼容扫荡**

Run: `go test ./tests/e2e/`
Expected: PASS（注入格式变化的兼容扫荡必须含 e2e 层）

- [ ] **Step 6: Commit**

```bash
git add internal/hook/core.go internal/hook/core_test.go
git commit -m "feat(hook): 注入段序固定——检索命中块沉底，一次性提示行前置，保 prompt cache 前缀稳定"
```

---

### Task 3: 检索查询净化——剥离已知注入块

源报告 §3 A1（防自污染，本次调研对 OK 最具体的一条）。依赖 Task 2 的段序约定（检索块恒沉底 → 头部截到文末是确定性剥离）。

**Files:**
- Create: `internal/retrieve/clean.go`
- Test: `internal/retrieve/clean_test.go`、`internal/hook/core_test.go`
- Modify: `internal/hook/core.go:177-189`（embed 与 Terms 改用净化后的查询文本）

**Interfaces:**
- Consumes: Task 2 的段序约定；注入块标记（`## 相关知识（需要全文时读取对应文件）`、`## 必守规约（全文见文件，必要时读取）`、`[OpenKnowledge]` 行前缀——均已在 core.go/hook.go 存在）。
- Produces: `func CleanQuery(prompt string) string`（retrieve 包导出，hook 调用）。

- [ ] **Step 1: 写失败测试（单元）**

创建 `internal/retrieve/clean_test.go`：

```go
package retrieve

import (
	"strings"
	"testing"
)

func TestCleanQueryStripsKnownBlocks(t *testing.T) {
	prompt := "真正的查询 RetrievalQuirk\n" +
		"[OpenKnowledge] wiki 已落后 3 个 commit，建议更新。\n" +
		"## 必守规约（全文见文件，必要时读取）\n\n- **架构规约** (rule)（/kb/规约.md）\n\n" +
		"## 相关知识（需要全文时读取对应文件）\n\n- **干扰条目** (note) — DistractionQuirk（/kb/干扰.md）\n"
	got := CleanQuery(prompt)
	if strings.Contains(got, "DistractionQuirk") || strings.Contains(got, "干扰条目") {
		t.Errorf("检索命中块应整体剥离，got: %q", got)
	}
	if strings.Contains(got, "必守规约") || strings.Contains(got, "架构规约") {
		t.Errorf("粘性指针块应剥离，got: %q", got)
	}
	if strings.Contains(got, "[OpenKnowledge]") {
		t.Errorf("提示行应剥离，got: %q", got)
	}
	if !strings.Contains(got, "RetrievalQuirk") {
		t.Errorf("真实查询应保留，got: %q", got)
	}
}

func TestCleanQueryStickyBlockKeepsFollowingSection(t *testing.T) {
	// 粘性指针块在中间、后面还有别的 "## " 小节：只剥指针块，后续小节保留
	prompt := "问题\n## 必守规约（全文见文件，必要时读取）\n\n- **甲** (rule)（/kb/甲.md）\n\n## 用户自己写的章节\n\n保留我 KeepMe\n"
	got := CleanQuery(prompt)
	if strings.Contains(got, "必守规约") || strings.Contains(got, "（/kb/甲.md）") {
		t.Errorf("粘性指针块应剥离，got: %q", got)
	}
	if !strings.Contains(got, "KeepMe") {
		t.Errorf("后续用户章节应保留，got: %q", got)
	}
}

func TestCleanQueryPlainPromptUntouched(t *testing.T) {
	p := "普通 prompt 没有注入块"
	if got := CleanQuery(p); got != p {
		t.Errorf("普通 prompt 不应改变，got: %q", got)
	}
}
```

- [ ] **Step 2: 写失败测试（hook 集成侧）**

在 `internal/hook/core_test.go` 追加（钉 `dedup_turns = 0`；判据限定在检索块内——首轮 INDEX 合法列出全部标题）：

```go
// TestInjectQueryPurified 回传的注入块不参与检索：干扰词只出现在回传的
// 检索块里时，不得把干扰条目检索回来（防自污染，TencentDB 实测命中率可归零）。
func TestInjectQueryPurified(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	writeEntry(t, kbRoot, "干扰.md", "---\ntitle: 干扰条目\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\nDistractionQuirk 唯一词。\n")
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\nRetrievalQuirk 唯一词。\n")
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[retrieve]\ndedup_turns = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	prompt := "RetrievalQuirk 是什么\n\n## 相关知识（需要全文时读取对应文件）\n\n- **干扰条目** (note) — DistractionQuirk 摘要（/kb/干扰.md）\n"
	out := InjectForPrompt(pc, "s-clean", projDir, prompt)
	i := strings.Index(out, "## 相关知识")
	if i < 0 {
		t.Fatalf("应有检索命中块，got: %q", out)
	}
	hits := out[i:]
	if strings.Contains(hits, "干扰条目") {
		t.Errorf("回传注入块里的干扰词不应检索回干扰条目，got: %q", hits)
	}
	if !strings.Contains(hits, "检索经验") {
		t.Errorf("真实查询词应命中检索经验，got: %q", hits)
	}
}
```

- [ ] **Step 3: 跑测试确认变红**

Run: `go test ./internal/retrieve/ -run TestCleanQuery -v`
Expected: FAIL（`CleanQuery` 未定义，编译错误）
Run: `go test ./internal/hook/ -run TestInjectQueryPurified -v`
Expected: FAIL（干扰条目被检索回来——top_n=2 下两条都命中）

- [ ] **Step 4: 实现 CleanQuery**

创建 `internal/retrieve/clean.go`：

```go
package retrieve

import "strings"

const (
	// hitsHeader / stickyHeader / noticePrefix 是 hook 注入文本的固定包裹标记
	// （internal/hook/core.go 的注入模板），净化按标记确定性剥离。
	hitsHeader   = "## 相关知识（需要全文时读取对应文件）"
	stickyHeader = "## 必守规约（全文见文件，必要时读取）"
	noticePrefix = "[OpenKnowledge]"
)

// CleanQuery 剥离 prompt 中已知的 OpenKnowledge 注入块，防止检索词被自身注入
// 污染（外部实测：harness 噪声块可把 FTS/向量命中率打到零——框架把上一轮注入
// 回传进 prompt 时，注入内容会变成检索词的一部分）。
// 覆盖：检索命中块（段序固定后恒为末段，头部截到文末）、必守规约粘性指针块、
// 所有 [OpenKnowledge] 前缀行。不覆盖（无法确定性剥离）：mandatory 全文与 INDEX
// 原文——它们是稳定内容，若被回传只引入稳定关键词，污染有限。
func CleanQuery(prompt string) string {
	s := prompt
	if i := strings.Index(s, hitsHeader); i >= 0 {
		s = s[:i]
	}
	s = stripSection(s, stickyHeader)
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), noticePrefix) {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

// stripSection 删除从 header 起到下一个 "## " 小节头（不含，保留该小节）或文末的整块。
func stripSection(s, header string) string {
	i := strings.Index(s, header)
	if i < 0 {
		return s
	}
	rest := s[i+len(header):]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		return s[:i] + rest[j+1:]
	}
	return s[:i]
}
```

- [ ] **Step 5: 接入 InjectForPrompt**

`internal/hook/core.go` 门控判定行（:125）之后、`if gated {` 分支的 `else` 块开头（:170-171 区域）插入：

```go
		// 检索词净化：剥离 prompt 里回传的已知注入块（自身注入防自污染），
		// 门控判定用原文（确认短语匹配不受净化影响）；剥空则回退原文。
		queryPrompt := retrieve.CleanQuery(promptText)
		if strings.TrimSpace(queryPrompt) == "" {
			queryPrompt = promptText
		}
		if queryPrompt != promptText {
			logErr("prompt clean: 已剥离回传注入块后检索")
		}
```

然后把该 else 块内 `client.EmbedQuery(context.Background(), promptText)`（:178）改为 `client.EmbedQuery(context.Background(), queryPrompt)`，`db.QueryExBranch(retrieve.Terms(promptText), ...)`（:189）改为 `db.QueryExBranch(retrieve.Terms(queryPrompt), ...)`。

- [ ] **Step 6: 跑测试确认变绿 + 全量回归**

Run: `go test ./internal/retrieve/ ./internal/hook/ -v -run "TestCleanQuery|TestInjectQueryPurified"`
Expected: 全部 PASS
Run: `go test ./...`
Expected: 全绿

- [ ] **Step 7: Commit**

```bash
git add internal/retrieve/clean.go internal/retrieve/clean_test.go internal/hook/core.go internal/hook/core_test.go
git commit -m "feat(retrieve): 检索查询净化——剥离回传的已知注入块，防自身注入污染检索词"
```

---

### Task 4: per-entry 注入预算（单条指针行截断 + 记账）

源报告 §3 A3。现状只有 `inject.max_tokens` 总量截断，一条超长 summary 的条目能吃掉整个检索段预算。加 `[inject] entry_max_tokens`（缺省 0=不限制，保持旧语义），超限指针行截断并记 ok.log（沿用 mandatory 超预算告警的记账风格）。`store.TruncateToBudget` 已核实 rune 安全（`[]rune` 切片），本任务顺带用测试钉死 CJK 截断不断字。

**Files:**
- Modify: `internal/config/config.go:128-139`（Inject 加字段）
- Modify: `internal/hook/core.go`（Task 2 后的检索指针行循环内截断）
- Test: `internal/hook/core_test.go`、`internal/store/store_test.go`（若存在则追加；不存在则新建）

**Interfaces:**
- Consumes: `store.EstimateTokens` / `store.TruncateToBudget`（`internal/store/store.go:37,54`）；Task 2 的 `hitsText` 循环。
- Produces: 配置项 `inject.entry_max_tokens`（int，toml key `entry_max_tokens`，0=关闭）。GUI 设置卡**不在本任务**（后续单独排期）。

- [ ] **Step 1: 写失败测试（截断生效）**

`internal/hook/core_test.go` 追加：

```go
// TestInjectEntryBudgetTruncatesLongSummary 单条指针行超 entry_max_tokens 时截断
// （一条超长 summary 不得吃掉整个检索段预算）；缺省 0 不限制保持旧语义。
func TestInjectEntryBudgetTruncatesLongSummary(t *testing.T) {
	projDir, kbRoot := setupProject(t)
	longSummary := strings.Repeat("这是一段很长的摘要用来占用预算。", 20)
	writeEntry(t, kbRoot, "检索.md", "---\ntitle: 检索经验\ntype: note\ntags: []\nsummary: "+longSummary+"\ncreated: 2026-01-01\nupdated: 2026-01-01\ndraft: false\n---\n\nBudgetQuirk 唯一词。\n")
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[retrieve]\ndedup_turns = 0\n[inject]\nentry_max_tokens = 30\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, err := project.FromCwd(projDir)
	if err != nil {
		t.Fatal(err)
	}
	out := InjectForPrompt(pc, "s-budget", projDir, "BudgetQuirk 是什么")
	i := strings.Index(out, "## 相关知识")
	if i < 0 {
		t.Fatalf("应有检索命中块，got: %q", out)
	}
	if !strings.Contains(out[i:], "…(已截断)") {
		t.Errorf("超限指针行应被截断并带标记，got: %q", out[i:])
	}
	// 对照：缺省（0=不限制）不截断
	if err := os.WriteFile(filepath.Join(kbRoot, "config.toml"), []byte("[retrieve]\ndedup_turns = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pc2, err2 := project.FromCwd(projDir)
	if err2 != nil {
		t.Fatal(err2)
	}
	out2 := InjectForPrompt(pc2, "s-budget2", projDir, "BudgetQuirk 再问")
	j := strings.Index(out2, "## 相关知识")
	if j < 0 {
		t.Fatalf("对照组应有检索命中块，got: %q", out2)
	}
	if strings.Contains(out2[j:], "…(已截断)") {
		t.Errorf("缺省不限制时不应截断指针行，got: %q", out2[j:])
	}
}
```

（注意：若 `setupProject` 夹具的项目级 config 写法不同，按 `TestMandatoryBudgetWarn` 的既有写法对齐。）

- [ ] **Step 2: 写 store 截断 CJK 钉死测试**

在 `internal/store` 的测试文件（先 Glob 确认 `store_test.go` 是否存在及既有命名）追加：

```go
// TestTruncateToBudgetCJKRuneSafe 钉死 CJK 截断的 rune 安全与标记成本预扣：
// 结果必须是合法 UTF-8（不断字）且含标记不超预算。
func TestTruncateToBudgetCJKRuneSafe(t *testing.T) {
	s := strings.Repeat("汉字混排ab", 50)
	got := TruncateToBudget(s, 20)
	if !utf8.ValidString(got) {
		t.Errorf("截断结果必须是合法 UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "…(已截断)") {
		t.Errorf("截断结果应带标记: %q", got)
	}
	if est := EstimateTokens(got); est > 20 {
		t.Errorf("含标记不应超预算: est=%d > 20", est)
	}
}
```

（按文件实际补 `strings`/`unicode/utf8` 导入。）

- [ ] **Step 3: 跑测试确认变红**

Run: `go test ./internal/hook/ -run TestInjectEntryBudgetTruncatesLongSummary -v`
Expected: FAIL（无截断标记）
Run: `go test ./internal/store/ -run TestTruncateToBudgetCJKRuneSafe -v`
Expected: PASS（既有实现已 rune 安全——本测试是钉死，不是驱动新代码；若为红说明截断有 bug，先修再继续）

- [ ] **Step 4: 实现配置字段**

`internal/config/config.go` 的 `Inject` 结构体（:128-139）追加字段：

```go
	// EntryMaxTokens 检索注入单条指针行的 token 上限（默认 0=不限制，保持旧
	// 语义）。一条超长 summary 的条目此前可吃掉整个检索段预算；超限时指针行
	// 截断（…(已截断) 标记）并记 ok.log，GUI 日志页可按"entry budget"过滤。
	EntryMaxTokens int `toml:"entry_max_tokens"`
```

- [ ] **Step 5: 实现指针行截断**

`internal/hook/core.go` 检索指针行循环（Task 2 后写入 `hitsText` 的 `for _, h := range hits` 循环）改为：

```go
	names := make([]string, 0, len(hits))
	for _, h := range hits {
		p := index.StripControls(filepath.ToSlash(filepath.Join(pc.Store.KnowledgeDir(), h.Filename)))
		var line string
		if h.Summary != "" {
			line = fmt.Sprintf("- **%s** (%s) — %s（%s）\n", index.SanitizeInline(h.Title), index.SanitizeInline(h.Type), index.SanitizeInline(h.Summary), p)
		} else {
			line = fmt.Sprintf("- **%s** (%s)（%s）\n", index.SanitizeInline(h.Title), index.SanitizeInline(h.Type), p)
		}
		// 单条预算：一条超长指针行不得吃掉整个检索段预算（缺省 0=不限制）。
		// 截断记账沿用 mandatory 超预算告警风格；TruncateToBudget 已 rune 安全。
		if limit := pc.Config.Inject.EntryMaxTokens; limit > 0 {
			if est := store.EstimateTokens(line); est > limit {
				logErr("prompt entry budget: 条目 %s 指针行约 %d token 超单条上限 %d，已截断", h.Filename, est, limit)
				line = strings.TrimRight(store.TruncateToBudget(line, limit), "\n") + "\n"
			}
		}
		hitsText.WriteString(line)
		names = append(names, h.Filename)
	}
```

- [ ] **Step 6: 跑测试确认变绿 + 全量回归**

Run: `go test ./internal/hook/ ./internal/store/ -v -run "TestInjectEntryBudget|TestTruncateToBudgetCJK"`
Expected: 全部 PASS
Run: `go test ./...`
Expected: 全绿

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/hook/core.go internal/hook/core_test.go internal/store/
git commit -m "feat(inject): 新增 entry_max_tokens 单条指针行预算——超限截断并记账，缺省 0 保持旧语义"
```

---

### Task 5: ok propose 同域重复候选提示

源报告 §2.2（TencentDB A6 形式）：草稿落盘后用现有混合检索召回同域条目展示给审批者，**不做自动合并裁决**，人审闸门不变。纯关键词检索（nil 向量，不调 embedding——propose 路径本来就不需要网络）。

**Files:**
- Modify: `internal/cli/cli.go:730-748`（Propose 的 Sync 成功块之后、return 0 之前）
- Test: `internal/cli/cli_test.go`

**Interfaces:**
- Consumes: `db.QueryExBranch(terms []string, queryVec []float32, cfg config.Retrieve, branch string, exclude map[string]bool) ([]index.Hit, index.QueryInfo, error)`（`internal/index/query.go:146`）；`retrieve.Terms`（`internal/retrieve/retrieve.go:9`）；`index.Hit` 有 `Title`/`Filename` 字段。Propose 函数内已持有 `db`（:731 打开）、`pc.Config`、`title`/`sum`。
- Produces: Propose stdout 新增"疑似同域条目"提示段（仅在有候选时）。

- [ ] **Step 1: 写失败测试**

`internal/cli/cli_test.go` 追加（环境变量隔离块完整照抄 `TestInitAddSearchList` 开头——agent home 全套变量 + `KIMI_CODE_HOME` 目录创建 + `OPENAI_API_KEY` 置空）：

```go
func TestProposeShowsSimilarEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, "kimi"))
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
	if err := os.MkdirAll(filepath.Join(home, "kimi"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "")
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, proj)
	var out, errBuf bytes.Buffer
	if code := Init([]string{"demo"}, &out, &errBuf); code != 0 {
		t.Fatalf("init code=%d err=%q", code, errBuf.String())
	}
	// 已有同域条目
	if code := Add([]string{"--title", "Git 提交规范", "--type", "note", "--body", "使用 Conventional Commits，SimHashQuirk 唯一词。"}, &out, &errBuf); code != 0 {
		t.Fatalf("add code=%d err=%q", code, errBuf.String())
	}
	// propose 新草稿：正文与已有条目同域
	out.Reset()
	code := Propose([]string{"--title", "提交信息常见坑", "--type", "pitfall", "--body", "Conventional Commits 的 SimHashQuirk 坑。"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("propose code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "疑似同域条目") || !strings.Contains(out.String(), "Git 提交规范") {
		t.Errorf("propose 应提示疑似同域条目，got: %q", out.String())
	}
}
```

- [ ] **Step 2: 跑测试确认变红**

Run: `go test ./internal/cli/ -run TestProposeShowsSimilarEntries -v`
Expected: FAIL（输出无"疑似同域条目"）

- [ ] **Step 3: 实现候选提示**

`internal/cli/cli.go` 的 Propose 函数中，`fmt.Fprintln(stdout, "INDEX 已更新（草稿不参与检索与向量）")`（:747）之后、`return 0` 之前插入：

```go
	// 同域候选提示（TencentDB A6 形式）：给新草稿召回 top-N 同域条目展示给
	// 审批者，不做自动合并裁决——人审闸门不变。纯关键词检索（nil 向量，
	// 不调 embedding）；分支未知传空串（分支过滤恒等）；失败仅警告不影响主流程。
	cands, _, qerr := db.QueryExBranch(retrieve.Terms(*title+" "+sum), nil, pc.Config.Retrieve, "", nil)
	if qerr != nil {
		fmt.Fprintln(stderr, qerr)
	} else if len(cands) > 0 {
		fmt.Fprintln(stdout, "疑似同域条目（内容重叠时请考虑更新已有条目，而非平行新建）:")
		for _, c := range cands {
			fmt.Fprintf(stdout, "  - %s（%s）\n", c.Title, c.Filename)
		}
	}
	return 0
```

并在文件头部 import 块加 `"openknowledge/internal/retrieve"`。

注意：刚落盘的草稿带 `draft: true`，Sync 不将其纳入检索（既有行为），不会自命中；`top_n` 默认 2，候选天然限量。

- [ ] **Step 4: 跑测试确认变绿 + 全量回归**

Run: `go test ./internal/cli/ -run TestProposeShowsSimilarEntries -v`
Expected: PASS
Run: `go test ./...`
Expected: 全绿

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go
git commit -m "feat(cli): ok propose 输出疑似同域条目候选（纯关键词召回，人审裁决不变）"
```

---

### Task 6: 全量回归与文档收尾

**Files:**
- Modify: `docs/2026-08-23-memory-systems-synthesis.md`（§6 行动表标注已落地项）

- [ ] **Step 1: 全量测试（双平台语义）**

Run: `go test ./...`
Expected: 全绿
Run: `go test ./tests/e2e/`
Expected: 全绿（注入格式变更的兼容扫荡）

- [ ] **Step 2: 汇总文档标注落地状态**

`docs/2026-08-23-memory-systems-synthesis.md` §6 行动建议表的优先级 1-5 行，各加"（已落地 v.next）"或对应标注；优先级 6 与延后/备用项保持原样。

- [ ] **Step 3: Commit**

```bash
git add docs/2026-08-23-memory-systems-synthesis.md
git commit -m "docs: 外部调研借鉴第一批落地标注（提示词/段序/净化/单条预算/同域候选）"
```

---

## Self-Review 记录

- **Spec 覆盖**：源报告 §6 行动表优先级 1（Task 1）、2（Task 3）、3（Task 4）、4（Task 5）、5（Task 2）均有对应任务；6（GUI 统计页）为独立子系统，按 scope check 拆分留待单独计划；延后项（feedback v2）与备用项明确列入"不在本计划内"。
- **Placeholder 扫描**：各测试与实现代码均为完整代码；仅两处需实现者先核对既有夹具签名（`writeCaptureConfig`、`initGitRepo`/`runGit`/`gitHead`、`setupProject` 的 config 写法），已标注核对位置。
- **类型一致性**：`CleanQuery(prompt string) string`、`EntryMaxTokens int`、`QueryExBranch` 签名与各任务引用一致；`hitsText` 在 Task 2 引入、Task 4 复用，顺序已在依赖中说明（Task 2 → Task 3 有段序依赖，Task 4 依赖 Task 2 的 hitsText 循环）。
- **任务顺序**：Task 1 独立；Task 2 → Task 3 严格顺序；Task 4 须在 Task 2 之后；Task 5 独立；Task 6 收尾。
