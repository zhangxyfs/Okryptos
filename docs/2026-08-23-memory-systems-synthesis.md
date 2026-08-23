# 外部记忆系统调研横向汇总：OpenViking / OpenMemory / Acontext / TencentDB Agent Memory

- 日期：2026-08-23
- 源文档：
  - `docs/2026-08-19-openviking-retrieval-review.md`（OpenViking，检索维度）
  - `docs/2026-08-23-openmemory-analysis.md`（OpenMemory，认知记忆引擎）
  - `docs/2026-08-23-acontext-analysis.md`（Acontext，Skill is Memory）
  - `docs/2026-08-23-tencentdb-agent-memory-analysis.md`（TencentDB Agent Memory，团队记忆中枢）
- 目的：四份独立调研横向交叉，找收敛结论（多系统独立演化出的相同设计），剔除已被 OK 实现或与定位冲突的部分，得出一份按优先级排序的借鉴清单。

---

## 1. 时效修正：已经落地的建议

源文档各自成文于不同时间，部分建议已完成，不再列入待办：

- **跨轮注入冷却台账**（OpenViking 调研列最高优先级）：已实现。`internal/hook/core.go` 的 `dedup_turns`（默认 3 轮）+ `internal/state` 的 `Session.CoolingSet`，且压缩事件时随 `ResetBaseInjection` 一并清空。

本批已落地：提示词升级/段序固定/查询净化/entry_max_tokens/propose 同域候选（commits d6902e9..4b364ce）。

## 2. 收敛结论：多份文档独立指向的四个方向

收敛本身是信号：定位、技术栈、场景都不同的系统独立演化出同一设计，大概率是对的。

### 2.1 沉淀侧提示词太薄（三份收敛）⭐

| 来源 | 具体内容 |
|---|---|
| Acontext B1-B4 | skip_learning 琐碎门控（显式反例）、applies_when 作用域字段、第三人称铁律、report_thinking 强制前置 |
| TencentDB A4 | priority 打分门（<70 直接丢弃）、"建议≠决策""AI 输出未经采纳不得提取为事实" |
| OpenMemory §三.4 | 把行为约束写进工具/命令描述本身，不只指望文档 |

OK 现状：auto 自省提醒只有一句"非显而易见的坑或解法"（`internal/hook/core.go:383-388`），propose 技能提示词无上述约束。

这是**纯提示词改动、零代码风险**的一项，改 `internal/setupx` 安装的 propose/capture 技能源文件即可。四个子项可合并为一次改动：价值自评打分 + 琐碎任务反例 + 作用域声明 + 第三人称。

### 2.2 条目碎片化没有任何防线（三份收敛）

| 来源 | 机制 |
|---|---|
| OpenMemory §三.1 | SimHash 64 位 + Hamming ≤3 写入判重 |
| TencentDB A6 | 新草稿落盘时召回 top-K 同域候选展示给人（两阶段裁决的第一阶段） |
| Acontext A2 | update-vs-create 决策树：同域更新优先于新建，禁止窄用途条目 |

OK 现状：propose 只会新增，无去重/合并/路由；同名靠文件名唯一性拒绝覆盖。auto 模式跑久了同域坑会被反复 propose，长期导致条目碎片化。

三家中最适合 OK 人审哲学的是 **TencentDB A6 的形式**：草稿落盘时用现有混合检索找 top-5 同域条目，在 `ok list`/GUI 草稿详情展示"疑似重复/可合并"，由人决定——复用现有检索、无 LLM 裁决、保留草稿闸门。决策树（Acontext A2）可作为提示词层补充，与 §2.1 同批落地。

### 2.3 反馈"只降不升"缺正向一半（两份收敛）

- OpenViking 3.2 给出防固化的升权公式：`freq = sigmoid(log1p(active_count))`（天然饱和）× `exp(-(ln2/half_life)·age)`（不用自动过期）——正好回应 `internal/index/feedback.go:17` 注释里"加分会自我强化造成条目固化"的担忧。
- OpenMemory §三.2：检索即强化（命中即小幅升权）。

**前置条件**：OK 的采纳信号依赖宿主 read 工具派发，feedback 默认关闭正是因为信号恒零。此方向等派发接通后再做，两份源文档都标注了此前提。落地时注意 OpenMemory 的坑：读路径不要同步写库，走 `entry_events` 挂账模式（现有采纳归因已是此形态）。

### 2.4 可观测性缺口（两份收敛）

- Acontext A4：提醒→propose→approve 转化归因——"提醒过没有、agent 理没理"目前无账可查。
- OpenViking 3.4：检索健康统计（zero-result 率、SemanticRejected 聚合、回退率）。

OK 已有原料：`QueryInfo` 结构化字段（SemanticRejected/RecencyShifted/FeedbackDemoted 落 ok.log）、`LastExtractReminder`、`entry_events` 事件流。缺的是**聚合与展示**，落点在 GUI。属补课性质，非新设计。

## 3. 单份独有但质量高的条目（TencentDB 调研）

这三条只有一份文档提出，但都建立在对方实测踩坑上，且对 OK 非常具体：

- **A1 检索查询净化（防自污染）**：OK 注入的 mandatory/INDEX/检索块会进入会话上下文，若被框架回传进后续 prompt，检索词被自身注入污染——对方实测噪声块能把 FTS/向量命中率打到 0（`tdai-l1-recall-injector.ts:51-55`）。剥离是确定性的（OK 注入有固定包裹格式），落 `internal/hook` 检索入口。**成本最小、收益最直接的一条。**
- **A3 per-entry 注入预算**：现状只有 `inject.max_tokens` 总量截断，一条超长条目能吃光整个检索段预算。加单条上限（如 500 token）+ 丢弃记账（沿用 mandatory 超预算告警的记账风格）。顺手核实 `TruncateToBudget`（`internal/store/store.go:54`）截断是否 rune 安全——知识库已有"len() 是字节不是字符"同类坑。
- **A5 prompt-cache 稳定段分层**：mandatory/INDEX 等稳定内容若与每轮变动的检索结果混排，会持续破坏上游 KV/prompt cache。固定段序（稳定在前、检索在后）、包裹标记固定、首轮内容跨轮字节一致。只调 `internal/hook/core.go` 拼装顺序，零架构改动。

## 4. 明确不借鉴（四份一致或互相印证）

- **proxy 接入路线**（TencentDB C1）：与"hooks + 技能、零常驻中间人、fail-open"定位冲突，proxy 是 LLM 请求路径上的故障点。
- **重基础设施栈**（Acontext §5、TencentDB C2）：PG/Redis/MQ/S3/ClickHouse 与"单二进制 + 单 SQLite、零依赖"根本冲突。
- **L0 会话原文留存 + LLM 后台提取**（Acontext §5、TencentDB C3）：与"不存会话原文"的隐私定位冲突，且 OK 无原料（hook 只能观察信号）。
- **全自动写入无人审**（Acontext §5、TencentDB §5）：OK 的草稿闸门是差异化设计，且条目会被强制注入，低质量自动写入破坏力远大于对方的按需拉取模式。对方"给人修正入口，比追求抽取百分百正确更现实"（ROADMAP 自述）这句话本身印证了人审路线。
- **OpenViking 目录递归检索 / 分层 DAG 摘要**：前提是海量异构资源 + 异步 LLM 加工管线，OK 扁平万条库收益为零。
- **hook 路径加 LLM（rerank/意图分析）**：`docs/superpowers/specs/2026-08-16-retrieval-evolution.md:26` 已定方针不做；若做只放 `ok search` CLI（护栏参数：扩展 ≤3 条、5s 超时、失败闭环回原 query）。
- **团队四级身份 + ACL**（TencentDB C2）：单用户本地场景净负担。

## 5. 反向印证：OK 已有设计被外部独立验证（不改，增强信心）

- **分通道独立准入**：OpenViking 实测分数聚在 0.38-0.50 窄带故不设绝对阈值；OpenMemory 的 `OM_MIN_SCORE` 是死配置。OK 的动态 SemanticFloor（按查询分布的相对门槛）是更优解。
- **正文不进上下文**：OpenViking 只给 256 字符摘要、Acontext 靠 progressive disclosure 工具调用，OK 只注指针由模型读文件——三家殊途同归。
- **fail-open**：TencentDB dedup 失败全 store、gate 缺配置放行、hook 失败仅记日志；OK hook 全链路 panic 也放行。新增自动链路应继续保持。
- **宁缺毋滥**：OpenViking 无相关记忆整块置空；OK 无显著头部语义通道整体拒绝。注意 TencentDB 的反面教训：v1 门控太严覆盖率仅 46%，v2 翻转为 "when in doubt, capture"——**过滤太严比太松更伤**。OK 有人审闸门兜底，沉淀侧门槛应给出明确的低门槛表述，别把 §2.1 的打分门做成饿死沉淀的闸门。

## 6. 行动建议（按优先级）

| 优先 | 事项 | 性质 | 落地位置 |
|---|---|---|---|
| 1 | propose/capture 技能提示词升级：价值打分门 + 琐碎反例 + applies_when 作用域 + 第三人称（§2.1，附 update-vs-create 决策树 §2.2）（已落地，本批） | 纯提示词 | `internal/setupx` 安装的技能源文件 |
| 2 | 检索查询净化：剥离已知注入标记块再检索（§3 A1）（已落地，本批） | 小代码 | `internal/hook` 检索入口 |
| 3 | per-entry 注入预算 + 丢弃记账 + `TruncateToBudget` rune 安全核实（§3 A3）（已落地，本批） | 小代码 | `internal/hook/core.go`、`internal/store` |
| 4 | propose 草稿疑似重复/可合并同域候选提示（§2.2，TencentDB A6 形式）（已落地，本批） | 复用现有检索 | `internal/cli`（propose）、`internal/gui`、`web/` |
| 5 | 注入稳定段/变动段分层，段序与包裹标记固定（§3 A5）（已落地，本批） | 调拼装顺序 | `internal/hook/core.go` |
| 6 | 沉淀转化率 + 检索健康统计聚合展示（§2.4） | 补聚合展示 | `internal/state`、`internal/gui`、`web/` |
| 延后 | feedback v2 升权：sigmoid(log1p(count))·exp 衰减形式（§2.3） | 等 read 派发接通 | `internal/index/feedback.go` |
| 备用 | wiki/reference 条目以技能目录形态导出（Acontext A3）；自定义提示词护栏（TencentDB B2）；草稿来源会话溯源（TencentDB B3）；wiki overview 综述页（TencentDB B4） | 待对应场景出现 | 见各源文档 |

## 7. 元教训：工程纪律

Acontext 的 AGENTS.md 把跨模块同步义务写成硬规则（改 API 必查双 SDK 与文档）。OK 的对应风险是"hooks 协议/okd API 改了，agentx 适配器、web/ 前端、docs 契约不同步"——`docs/2026-08-21-okd-contract.md` 已是契约锚点，可把"改契约必查消费方"条款写进项目 AGENTS.md。
