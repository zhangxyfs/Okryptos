# OpenMemory 项目分析与借鉴清单

> 分析对象：`D:\develop\OpenMemory`（CaviraOSS OpenMemory，代码快照 2025-08，README 注明正在重写）
> 目的：对照 OpenKnowledge 当前架构，找出可借鉴的设计与需要规避的坑。

## 一、OpenMemory 是什么

面向 LLM/Agent 的"认知记忆引擎"：Node/TypeScript 服务端（`packages/openmemory-js`，引擎即 SDK）+ Python 平行移植（`packages/openmemory-py`）+ MCP server + VS Code 扩展 + Next.js dashboard。存储双后端（SQLite/Postgres），向量后端 pgvector / SQLite 内存暴力余弦 / Valkey。自我定位是"不是 RAG、不是向量库"，核心卖点是**认知分区（sector）+ 衰减 + 关联图（waypoint）+ 检索即强化**。

定位差异先说清：OpenMemory 是**给应用/embeddings 用的通用记忆中间件**（user_id 多租户、connector 摄入外部数据源）；OpenKnowledge 是**给 coding agent 用的工程经验知识库**（人工审批、文件即事实源、hook 注入）。目标场景不同，借鉴取其机制不取其形态。

## 二、核心机制对照

| 维度 | OpenMemory | OpenKnowledge | 差距判断 |
|---|---|---|---|
| 条目模型 | 5 sector（episodic/semantic/procedural/emotional/reflective），正则自动分类，每类独立 λ/权重 | 4 类型（rule/pitfall/note/reference），人工指定 | 我们的人工分类更可靠；sector 的**独立衰减率**思路可取 |
| 写入 | SimHash 近重复合并（Hamming ≤3 → 不加新条目，旧条目 salience+0.15） | 无去重，propose 全靠人审 | **可借鉴** |
| 检索打分 | 相似度 boost（τ=3）+ token overlap + waypoint 边权 + recency + tag 匹配，sigmoid 后 z-score 归一 | FTS5 BM25 + 余弦，RRF 融合，**按通道独立准入阈值** | 我们的准入更严谨（他们的 `OM_MIN_SCORE` 是死配置）；他们的**信号清单**可补充 |
| 图检索 | waypoint 单向边 + 自适应 BFS 扩展（高置信跳过，低置信 ×0.8 衰减扩展） | 无 | **可借鉴（轻量版）** |
| 衰减 | 每日定时跑 `s' = s·e^(-λ·d) + 0.08·(1-e^(-λ·d))`，长期收敛到 0.08 不归零 | 只有排序侧 recency 系数（下限 0.85，不卡准入） | 思路接近；他们的"衰减到地板而非零"更稳 |
| 检索副作用 | **检索即强化**：命中 salience+0.18×(1-s)，沿路径边权+0.05，共激活对自动建边 | 有注入/采纳事件流，但反馈降权默认关闭 | **最值得借鉴的一条** |
| 巩固 | 定时反射合并：同 sector Jaccard>0.8 聚类 → 模板拼接生成 reflective 条目（不调 LLM） | 无（wiki 是人工触发） | 机制可取，他们的模板拼接实现太糙 |
| 可解释性 | 查询返回 score 分解 + waypoint path；无独立审计存储 | 有 entry_events 事件流，但无 score 分解输出 | 可互补 |
| 多租户 | API key → SHA-256 前缀派生 tenant，客户端传的 user_id 不可信 | registry.toml 路径前缀匹配多项目 | 场景不同，但他们的"不信任客户端身份"原则可取 |
| 注入面 | MCP 6 工具 + IDE events API；工具描述里嵌 prompt 工程 | 10+ agent 适配器 hook 直注 | 我们的接入更深；他们的 MCP 描述写法可抄 |

## 三、可借鉴清单（按价值排序）

### 高价值

1. **SimHash 写入去重**（`packages/openmemory-js/src/memory/hsg.ts:1146`）
   64 位 SimHash + Hamming ≤ 3 判重：重复写入不加新条目，给旧条目加 salience 并刷新 last_seen。我们目前 auto capture 模式下同类坑可能被反复 propose，草稿层加一个 SimHash 判重（命中已有条目则提示"已有相似条目 X"）成本低收益直接。

2. **检索即强化 / 用进废退闭环**（`hsg.ts:1046-1100`）
   命中就强化（salience 增量）、共激活条目对自动建边。我们已有 `entry_events` 注入/采纳事件流和反馈降权（默认关闭）的基础设施，缺的是**正向**：被采纳的条目应在排序中获得小幅提升，让"好用的经验自然浮上来"。现有 `applyFeedback` 只做降权，补一个对称的升权即可，不需要引入 salience 字段。

3. **轻量关联扩展（waypoint 思想的低配版）**
   他们的完整图（建边 O(N)、共激活缓冲区、边衰减）对我们过重，但"检索高分命中时，顺带带出该条目显式关联的 1-2 条"很有用。可以不做隐式图，只做**条目 front matter 里的 `related: [slug]`** 显式关联 + 注入时跟随一跳——实现量极小，先试这个再决定要不要隐式图。

4. **MCP 工具描述内嵌行为约束**（`packages/openmemory-js/src/ai/mcp.ts`）
   他们在 store 工具描述里直接写"If unsure if the content is project-specific vs global, YOU MUST ASK THE USER"。我们给 agent 的 propose 提示词也可以把"什么该沉淀、什么不该"写进工具/命令描述本身，而不是只指望 wiki/skill 文档。

### 中价值

5. **分类型衰减率**：不同类型条目配不同新鲜度窗口——我们已有分类型 fresh/stale 窗口（`applyRecency`），本质一致；可参考他们 reflective（规则类）衰减最慢、emotional 最快的**排序**校准我们的窗口参数（rule 应该最抗衰减）。

6. **鉴权 fail-closed + 显式 dev 逃逸**：`OM_REQUIRE_AUTH` 未配置时全 503，dev 绕过必须显式 `OM_DEV_ALLOW_NO_AUTH=true`。我们的 hostGuard + token 已经不错，但"客户端传入的身份不可信、服务端派生 tenant"这条原则，在 okd 未来开放非回环或多用户前值得提前立规矩。

7. **connector 摄入基类**（`sources/base.ts`）：模板方法 + token bucket + 指数退避 + 异常分层。如果将来做"从 git history / issues / 外部文档自动摄入 reference 条目"，这个骨架可直接参考。

8. **dashboard 纯代理架构**：前端零鉴权逻辑，一个 catch-all 代理注入 api key。我们 GUI 目前 token 经 fragment 注入、页面直连 okd，已够简单；但若以后 web/ 前端变复杂，这个模式可收编所有鉴权到一处。

9. **治理文档**：GOVERNANCE.md 的决策分级（小改 1 人批 / API 2 人批 / 架构走 RFC）和 MIGRATION.md 的"自动迁移 + 双手动 SQL 方言"双轨写法，等项目开源/有外部贡献者时直接当模板。

### 低价值 / 暂不借鉴

- 多 sector embedding（他们 simple 模式下各 sector 向量实为同一份，宣传大于实际）
- 定时反射合并（模板拼接太糙；我们的 propose+人审质量更高；若做自动合并至少要用 LLM 生成）
- 双语言全量移植（他们 JS/Py 两侧已明显 API 漂移，是反面教材）

## 四、OpenMemory 的坑（避免重蹈）

1. **配置自洽性**：默认 tier=hybrid + 默认 synthetic embedding，一旦配了真实 embedding provider 但 tier 没切，写入真向量、查询假向量，**语义检索静默失效只打警告**。我们 embedx 的"模型身份不一致 → 退化纯关键词并提示"已经是正确解法，可作为正面案例保留。
2. **读路径有状态写**：查询会写 salience/边权/共激活缓冲，高并发下 SQLite 写争用 + 全局事务互斥。我们若做"检索即强化"，务必走 `entry_events` 异步/挂账模式（现有采纳归因已是这个形态），不要在查询路径同步写库。
3. **死配置与误导命名**：`OM_MIN_SCORE` 全代码库无人使用；`filters.min_score` 实际过滤的是 salience。警醒：我们的配置项也要定期核查是否真被消费（评审报告已有此习惯）。
4. **三套衰减并存**：主路径一套、decay-2.0 一套（JS 侧死代码、Python 侧在用）、dynamics.ts 又一套。双实现项目的熵增警示——我们只有 Go 单实现，天然免疫，但若未来出 SDK 切记薄客户端路线。
5. **add 是 O(N)**：每次写入拉最近 1000 条算余弦建边。我们若做 SimHash 判重，注意判重本身也应走索引/哈希而非全量向量比对。
6. **过期 Makefile / 伪代码集成示例**：Streamlit"集成" import 全是注释。文档与代码同步的纪律我们已有（README 徽标漂移坑、AGENTS.md 同步约定），继续保持。

## 五、结论

OpenMemory 最值得拿的是三样东西：**SimHash 写入去重、检索即强化的正向反馈、显式关联一跳跟随**。三者都能在我们现有的 entry/SQLite/事件流基础上以小增量实现，不需要引入图数据库或改变人工审批的沉淀主线。它的复合打分信号清单（tag 匹配、recency 双参数曲线）可作为检索调参的参考配方。它的衰减/巩固/双移植实现则是"想法好、实现乱"的典型，取思想不取代码。

> 附：检索维度的另一份对照见 `docs/2026-08-19-openviking-retrieval-review.md`，两份可交叉参考。
