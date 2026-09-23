# ok-wiki-pitfall：wiki + 挖坑一体技能（wiki fixes 子命令 + pitfall 条目录入）—— 设计

日期：2026-09-23
状态：已评审通过（待实施）

> 修订（2026-09-23 评审后）：曾考虑独立 `ok-pitfall` 技能 + 独立游标，因游标独立成本高、改动大被否决。定稿：**单技能双职**——`ok-wiki` 改名 `ok-wiki-pitfall`，wiki 生成/更新与挖坑共用 wiki 游标，一个技能干两件事。

## 背景与目标

ok-wiki 技能目前只写 `reference` 型、tags `wiki,<主题>` 的 wiki 条目（架构总览 / 主题域 / 演进历程）。项目 git 历史里的"坑"（fix/bug/revert/踩坑修复类提交）没有被系统挖掘——只有会话中的 capture/propose 流在沉淀经验。

目标：ok-wiki 的三种流程（全量 / 增量 / 分支差异）各加一步"挖坑"，把 git 历史里有普遍教训的修复录成 **pitfall 条目**，与 wiki 条目一起维护。

## 已定决策

| 决策点 | 结论 |
|---|---|
| 素材范围 | 以 git 历史为主（fix/bug/revert 类提交） |
| 条目形态 | **标准坑条目**：`type=pitfall`，**不打 wiki 标签**——进 INDEX 主列表 + 混合检索注入；打了 wiki 标签会退出主列表只进 Wiki 目录（`internal/index/indexmd.go` 现有渲染逻辑），注入能力反而降级 |
| 录入节奏 | **直接转正**（`ok add`），与 wiki 条目同信任模式，用户 GUI 随时修订；不走 propose 草稿 |
| 代码改动 | 新增 `ok wiki fixes` 子命令，给技能一份可靠的"疑似修复提交"摘要 |
| 技能形态 | **单技能双职**：`ok-wiki` 改名 `ok-wiki-pitfall`，wiki 生成/更新与挖坑一体；游标共用 wiki 游标（`wiki mark` 统一推进），不建独立坑游标 |

数据写入路径（`ok add --type pitfall`）现状已支持，不改。

## 方案

### 1. 总体行为

改名后的技能（`ok-wiki-pitfall`）三种流程（全量 / 增量 / 分支差异）各加一步"挖坑"：取本流程对应 git 区间的**疑似修复提交摘要**（新增 `ok wiki fixes` 子命令输出），AI 从中甄别出有普遍教训的坑，写成**标准坑条目**（`type=pitfall`，不打 wiki 标签，直接转正），与 wiki 条目一起汇报。坑挖掘与 wiki 共用游标——`wiki mark` 推进游标后，下轮增量只挖新区间，天然不重不漏。

### 2. Go 侧：`ok wiki fixes [<起点|区间>]`

**分发**：`internal/cli/cli.go` WikiCmd（现 status|mark|base|diff）加 `fixes` 分支。

**range 语义**：

- 无参数 → 当前分支 wiki 游标以来：`<last_commit>..HEAD`（复用 LoadState + 继承逻辑，与 CheckStatus 同口径）；游标不可用（无游标 / gone / diverged / 非 git 时间戳游标为空）→ 回退全量历史，输出注明"游标不可用，已回退全量"。
- 传一个裸 commit `<X>` → `<X>..HEAD`。
- 传 `A..B` 形式 → 原样作为 git revision range。
- 非 git 项目 → 报错退出码 1，与 `backfill-born` 同口径。

**实现**（新文件 `internal/wiki/fixes.go`，函数 `FixesSummary(srcDir, rangeSpec string) (string, error)`，风格对齐 `DiffSummary`）：

1. `git log --no-merges -n 2000 --pretty=%H%x00%s <range>` 取提交（2000 为扫描护栏，超出注明"仅扫描最近 2000 提交"）。
2. Go 侧按标题过滤疑似修复提交。模式常量表，英文按词边界前缀匹配（`fix` 命中 fix/fixes/fixed/fixing；词表：`fix`、`bug`、`hotfix`、`revert`、`regression`、`workaround`、`crash`、`panic`、`leak`、`deadlock`、`race`、`fixup`），中文按子串（`修复`、`缺陷`、`回滚`、`坑`、`崩溃`、`泄漏`、`死锁`、`竞态`；`坑` 已覆盖 `踩坑`）。误报容忍：这是给 AI 消化的候选清单，宁可稍多不可漏。
3. 增删行数同趟取：`git log --no-merges -n 2000 --numstat --pretty=...` 一次拿全，不逐条 spawn。
4. 输出文本（对齐 DiffSummary 风格）：

```
扫描区间: <range 描述>（命中 X / 扫描 Y 提交）
  <短hash> <标题>（+a/-d）
  ...
仅列前 50 条，可自行 git log 续查   ← 超 50 时
```

- 空 result：输出"区间内无可识别为修复的提交"，退出码 0。
- 过滤模式写死在 Go 常量表，口径收敛一处；后续调整只改常量。

**接入面零改动**：daemon 兼容转发（`internal/daemon/forward_cli.go`）与 GUI 终端白名单（`internal/gui/api.go`）都按一级子命令 `wiki` 匹配，`wiki fixes` 自动透传并享用 wiki 长超时。

**测试**：`internal/wiki/fixes_test.go`（模式过滤中英文/词边界/误报样例如 "apply patch"、range 解析、50 条截断、2000 扫描护栏说明、非 git 报错）；CLI 层 WikiCmd fixes 分发冒烟。

### 3. 坑条目规范（SKILL.md 侧）

```bash
"D:/software/OpenKnowledge/ok.exe" add --title <教训式标题> --type pitfall \
  --tags <主题tag> --summary <检索线索> --file <正文.md> [--force]
```

- **不传 wiki 标签**；`born:<分支>` 由 add 自动补（provenance）。
- 同名已存在用 `--force` 覆盖（增量重跑幂等，与 wiki 条目同纪律）。
- 正文：现象 / 根因 / 正确做法 / 出处（引 commit 短 hash），300 字内。
- 软护栏：单轮录 0~5 条；只录有普遍教训的——一次 fix 不必然是坑，批量同类小修复合并成一条或放弃。
- 标题风格与 capture 流坑条目一致（教训式断言，非事件叙述）。

### 4. 技能改名与 SKILL.md 改动（ok-wiki → ok-wiki-pitfall）

**改名机制（`internal/setupx`）**：

- 技能源文件 `internal/setupx/skills/ok-wiki/SKILL.md` → `internal/setupx/skills/ok-wiki-pitfall/SKILL.md`；`go:embed` 变量与 `skillTemplates` 注册键同步改（`setupx.go:480-489`）。
- **旧副本清理**：`RemoveLegacySkills` 现只清 `openknowledge-*` 前缀（`setupx.go:79-101`），`ok-wiki` 改名后已装在 `~/.zcode/skills/ok-wiki/`、`~/.agents/skills/ok-wiki/` 等的旧副本会成幽灵技能（agent 照常扫描，与新技能并存抢触发）。扩展清理：加旧名清单 `ok-wiki`，沿用既有安全纪律（目录名 + SKILL.md `name:` 双匹配才删，幂等，外来目录不动）。
- 状态页按 `SkillNames()`（即 skillTemplates 键）查存在性，改名后自动查新名，无需另改。

**用户可见文案同步**（引用旧技能名的 6 处）：

- `internal/hook/hook.go` 5 处 nudge（游标失效/分叉/无 wiki/落后/分支已并入，411-436 行）：`ok-wiki 技能` → `ok-wiki-pitfall 技能`。
- `internal/cli/cli.go:465-468` 检索兜底提示：同改。
- `internal/wiki/diff.go:13` 注释：同改。

**SKILL.md 内容**：

- front matter `name: ok-wiki-pitfall`；`description` 同时覆盖两类触发：既有 wiki 触发词 + 挖坑触发词（如"给项目挖坑""挖掘历史坑"）——遵守"行为触发条件必须写进 description"的既有教训（正文触发前不可见）。
- 三种流程（全量 / 增量 / 分支差异）各插一步"挖坑"：
  - 全量流程：`ok wiki fixes`（无游标 → 全量历史）；
  - 增量流程：`ok wiki fixes`（游标以来）；
  - 分支差异流程：`ok wiki fixes <分叉点>..HEAD`。
- 新增坑条目规范小节；汇报清单包含坑条目。
- 游标推进仍由 `wiki mark` 统一负责——坑挖掘与 wiki 共用游标，mark 后下轮增量只挖新区间，天然不重不漏。

**测试同步**：`setupx_test.go`、`skills_heal_test.go` 中 ok-wiki 相关用例改新名并补旧副本清理断言。

**CLI 子命令不改名**：`ok wiki status|mark|base|diff|fixes` 保持 `wiki` 组；`ok wiki status` 输出字段不变。

## 错误处理

| 场景 | 行为 |
|---|---|
| 非 git 项目 | fixes 报错退出码 1 |
| 游标 gone/diverged | 回退全量历史，输出注明 |
| 区间无命中 | 输出说明文字，退出码 0 |
| 超大仓 | 2000 提交扫描护栏 + 50 条输出护栏，均注明 |
| 技能侧 add 失败 | 沿用 add 现有报错；坑失败不阻断 wiki 条目写入 |

## 不做的事（YAGNI）

- **不建独立 `ok-pitfall` 技能与独立坑游标**（评审否决：游标独立成本高、改动大）——单技能双职，共用 wiki 游标。
- 不给坑条目建独立游标/状态——与 wiki 共用。
- 不改 INDEX.md 渲染、GUI 图谱树（标准坑条目走主列表与 pitfall 类别，现状已正确）。
- 不做"已入库坑"自动去重提示——同名 `--force` 与技能侧检索已覆盖。
- 不改 capture/propose 流——两条沉淀线并行、形态一致（type=pitfall）。
- CLI 子命令 `ok wiki` 组名不改——技能名与 CLI 命令名解耦，改命令名会破坏 daemon 转发、GUI 终端白名单与用户习惯，无收益。
