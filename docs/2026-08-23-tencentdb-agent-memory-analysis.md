# TencentDB Agent Memory 调研：代理接入与四层记忆管线对 OpenKnowledge 的借鉴价值

- 日期：2026-08-23
- 调研对象：[TencentDB-Agent-Memory](https://github.com/TencentCloud/TencentDB-Agent-Memory)（本地仓库 `D:\develop\TencentDB-Agent-Memory`，工作区当前状态；MIT。腾讯开源的"团队版 Agent 记忆"系统：MemoryProxy + MemoryCore + MemoryKnowledge + MemoryPanel 四件套，TypeScript/Node ≥22，Python/TS 双 SDK）
- 调研目的：评估其"LLM 代理拦截 + L0→L3 分层沉淀 + 团队资产装配"的设计，哪些可移植到 OpenKnowledge（Go 单二进制、本地知识库、hooks 注入 + propose 人审沉淀）
- 阅读范围：README_CN.md、ROADMAP_CN.md、CHANGELOG.md、INSTALL_CN.md 全文；MemoryProxy 的注入管线/注入器/extraction-gate/session 初始化/mem-command/skill 回写胶水；MemoryCore 的 L1 提取与去重 prompt、auto-capture/auto-recall hooks、pipeline-manager、persona 触发器、skill 抽取/版本化/review prompt、memory-prompt 护栏、RRF；MemoryKnowledge 的 wiki ingest-v2 与 tools 路由；MemoryCore README、SDK 目录、deploy 脚本。未逐行读 OpenKnowledge，模块级理解来自 README、`internal/hook/core.go`、既有评审文档

> 独立第三方调研，非官方对比。文中路径均相对 TencentDB-Agent-Memory 仓库根目录，未标注行号的引用基于本次阅读时的文件内容。

---

## 1. 项目速览

TencentDB Agent Memory 的定位是**面向"Agent 团队"的记忆中枢**（README_CN.md "Memory Hub"）：不是一个 agent 的私人记忆，而是 Team/User/Agent/Task 四级身份下统一登记、审核、配装的"记忆资产"（Chat Memory / Skill / Wiki / CodeGraph 四类）。口号是"让 Agent 沉淀经验，让人专注创造"，核心叙事是冷启动——新 Agent"先读档，再开工"。

**技术栈与形态**：

- 全 TypeScript（Node ≥22），Hono 框架；MemoryCore 以 standalone 形式开源（默认 `127.0.0.1:8420`，SQLite + 本地文件，默认关闭远程 embedding、BM25 兜底——`MemoryCore/README_CN.md`），但生产依赖可见 ClickHouse、Redis、COS（对象存储）、Langfuse/OTel、腾讯向量库（tcvdb）等适配代码（`MemoryProxy/package.json`、`MemoryCore/src/core/store/tcvdb*.ts`），显然是从内部云系统裁剪开源的。
- 部署是三个 docker 镜像一键起（`deploy/global-images/start-all.sh`：memory-core + memory-hub + proxy）。

**成熟度信号**（本地可见）：

- 本地 git 仅 17 个提交、最新 2026-08-15——公开仓库是 squash 后的发布镜像，看不到真实开发历史；版本号 v2.0.1-beta.2（CHANGELOG 从 v2.0.0-beta.1 起），README 挂 Trendshift badge、Discord、微信社群，运营痕迹重。
- README 宣称 PersonaMem benchmark 48%→76%（+59%），但**仓库内未见该 benchmark 的评测代码或数据**（仅找到无关的 token-estimate benchmark 脚本）——仅文档宣称，未见实现。
- ROADMAP 自我披露多处未完成："Hub 已支持人工绑定资产；全自动记忆路由仍在迭代"（README_CN.md "注意事项"）、"L1-L3 记忆面板只能查看和删除，无法修正"（ROADMAP_CN.md，v2.0.1 才补编辑）。
- 代码质量信号偏正面：设计决策直接写进文件头注释（含被否决方案，如 `MemoryCore/src/core/skill/skill-fast-path.ts:17-24` 记录"暂不接入"的完整评估理由），handler-glue、extraction-gate 的注释直白记录重构动机。

## 2. 架构与核心机制

### 2.1 MemoryProxy：协议级接入（与 hooks 路线最大的分歧点）

这是与 OpenKnowledge 架构差异最大的地方：**不写插件/hook/MCP，而是让 agent 把 LLM base URL 指向 proxy**（README_CN.md "一套 Proxy，协议不变，零代码接入"）。Proxy 拦截 Anthropic/OpenAI/Responses 三种协议的请求体，做两件事：

- **读侧（注入）**：`MemoryProxy/src/injection/pipeline.ts` 是一条 "parse → hooks → serialize" 管线。注入点枚举 9 个（`pipeline.ts:165-175`：system.prefix/before_tools/after_tools/suffix、tools.prepend/append、user.first_turn/before/after），每个注入器是声明 `point + priority + cacheStrategy` 的 hook，失败仅记日志不影响主流程（`pipeline.ts:229-250`）。启用哪些注入器走 yaml 白名单 `injection.injectors: ["skill","knowledge","tdai-memory"]`（`MemoryProxy/config.example.yaml:436`）。
- **写侧（回流）**：每轮真人对话结束（assistant 给出无 tool_use 的最终回复）时，把本 round 的对话切片 push 给 core 的 `/v3/skill/conversation/add`，core 攒够阈值自己归档（`MemoryProxy/src/skill/handler-glue.ts:14-26,101-120`）。round-level 语义明确："1 次真人问答 = 1 次 add"，tool-use 循环的中间态直接跳过。

**extraction-gate**（`MemoryProxy/src/extraction-gate.ts:1-73`）是给写侧补的语义总闸，注释把动机写得很清楚：读侧早有白名单管线，写侧的触发调用" sprinkled across handler.ts，没有 kill switch"。设计极简——一个 yaml 段 + 一个纯谓词 + 每个调用点包一层 if；**缺配置时宽容放行**（保持历史行为），被拦时打结构化 debug 日志。读侧/写侧对称门控这个思想直接可借鉴。

**注入缓存与 prompt cache 命中**（`pipeline.ts:269-328`）：hook 可声明 `cacheStrategy: session_init`——session 初始化时预暖注入块进缓存，后续每轮直接读缓存跳过执行，目的是**让上游 Anthropic 的 KV/prompt cache 按字节命中**（`config.example.yaml:440` 注释甚至警告：多 pod 不配统一 gateway URL 会导致缓存互覆、"KV cache 每次 miss 费钱+慢"）。细节讲究：fork（readOnly）请求 cache miss 时**不**自愈写缓存，怕写出不一致内容污染主会话（`pipeline.ts:298-318`）。

**session-init 身份识别**（`MemoryProxy/src/session/extractor.ts`）：proxy 模式没有本地项目概念，会话归属哪个 team/agent/task 要靠会话开头的交互式表单（借用 agent 的 ask_followup_question 能力）让用户选，然后**纯结构化解析**用户回答——注释明确"历史上有过 LLM 提取兜底，已删除"，宁可重试后 bypass 也不让 LLM 猜（`extractor.ts:503-513`）。还有一个典型的坑记录：数字回复只接受纯数字匹配，"严禁用 parseInt 容忍前缀数字——ULID 以 01 开头会被映射成第一个 agent"（`extractor.ts:461-472`）。

**mem: 会话指令**（`MemoryProxy/src/mem-command/`）：用户在对话里输入 `mem:sync` / `mem:create-skill` / `mem:help`，proxy 拦截就地响应，不出会话（ROADMAP_CN.md "mem: 会话指令"）。与 OK 的技能路线等价但入口更轻。

### 2.2 MemoryCore：L0→L3 四层记忆管线

**分层模型**（README_CN.md "记忆不是平铺记录，而是逐层生长"；`MemoryCore/src/utils/pipeline-manager.ts:1-50`）：

- **L0 Conversation**：原始对话落盘（JSONL + 向量），auto-capture hook 只记不提炼（`MemoryCore/src/core/hooks/auto-capture.ts:1-10`）。
- **L1 Atom**：LLM 从对话提取原子记忆。触发 = 攒够 N 轮 **或** idle 超时 **或** 关机 flush（`pipeline-manager.ts:37-50`）。
- **L2 Scenario**：LLM agent 持工具把 L1 组织成 scene block Markdown 文件，**沙箱到 scene_blocks/ 目录**（`MemoryCore/src/core/scene/scene-extractor.ts:1-17`）。调度用"downward-only timer"：触发时间只能提前不能推迟，minInterval 做地板、maxInterval 做兜底，会话冷了取消（`pipeline-manager.ts:25-35`）。
- **L3 Persona**：全局互斥（concurrency=1）生成用户画像。触发器有 5 个优先级条件：主动请求 > 冷启动 > persona.md 丢失恢复 > 首次 scene > 阈值（`MemoryCore/src/core/persona/persona-trigger.ts:36-101`）——比单一阈值细腻得多。

**L1 提取 prompt**（`MemoryCore/src/core/prompts/l1-extraction.ts`）是可直接抄作业的部分：

- 单次 LLM 调用同时做"情境切分 + 记忆提取"，背景消息只供理解、**严禁从中提取**（`l1-extraction.ts:172-175`）。
- 三原则：宁缺毋滥（过滤琐碎闲聊/一次性操作）、**独立完整**（"跳出当前对话依然成立"，禁止"这个/那个/上面说的"）、归纳合并（强关联消息必须合并成一条，不可碎片化）（`l1-extraction.ts:33-36,160-161`）。
- **priority 打分制**：每类记忆给出分段打分指引，低于阈值"直接丢弃"（如 instruction <70 丢弃，`l1-extraction.ts:56`）——把"值不值得沉淀"变成显式分数而非模糊判断。
- 面向编程/团队协作场景有独立的 work prompt，四类：work_fact / work_task / work_method / work_artifact，且强调**准确归因**——"某人建议 ≠ 团队决策，未确认内容必须写成'仍待确认'"（`l1-extraction.ts:163-165`）、AI 输出未经人类采纳不得提取为事实（`:177-180`）。

**L1 冲突裁决**（`MemoryCore/src/core/record/l1-dedup.ts` + `prompts/l1-dedup.ts`）：两阶段——先用向量（降级 FTS5 BM25）给每条新记忆召回 top-K 候选，再**单批 LLM 调用**对所有新记忆裁决 `store/skip/update/merge`，支持跨类型合并与多对多合并（`prompts/l1-dedup.ts:20-24`）。工程防御值得记录：无任何召回能力时**直接跳过去重**全部 store（`l1-dedup.ts:93-97`）；LLM 失败/解析失败全部默认 store（`:186-195,399-408`）；LLM 返回空 record_id 视为幻觉直接丢弃（`:356-361`）。合并时 priority 酌情**提升**、timestamps 取并集保留完整时间线（`prompts/l1-dedup.ts:45-47,69`）。

**召回预算**（`MemoryCore/src/core/hooks/auto-recall.ts:835-916`）：两级字符预算——`maxCharsPerMemory` 单条截断 + `maxTotalRecallChars` 总预算，超预算的条目整条丢弃并记 debug 日志；截断按 Unicode code point 切，"绝不落在 surrogate pair 中间"（`:906-916`）。检索融合是教科书 RRF（k=60，`MemoryCore/src/core/store/search-utils.ts:19-59`）。召回注入的文案明确"仅用于辅助回答当前这一轮，**不要视为永久系统规则**"，并给每条标 `[self]` / `[from <agent>]` 来源（`MemoryProxy/src/injection/injectors/tdai-l1-recall-injector.ts:93-106`）。

**检索查询净化**：L1 召回注入器注释记录了一个实测坑——必须用"干净的真实 user_query"做检索词，整条原始消息含 `<user_info>` 等 harness 噪声块会"让 FTS5/向量检索命中率极低甚至 0"（`tdai-l1-recall-injector.ts:51-55`）。

**Skill 子系统**：对话攒够阈值后由 Skill Review Agent 评审整个 transcript 决定建/改 skill（`MemoryCore/src/core/skill/prompts/skill-review-prompt.ts`）。prompt 头部记录了 v1→v2 的理念翻转：v1 三重门控在评测集上只覆盖 46%，v2 改为"when in doubt, capture"（`:4-37`）——**过滤太严比太松更伤**。安全设计：transcript 用 `<<past-user>>` 等标记包裹并明示"你是评审者不是参与者"，防 transcript 内指令劫持（`:40-51`）。skill 有版本化：内容 hash 幂等 + 目录 copyTree + DB 事务，失败 best-effort 清理（`MemoryCore/src/core/skill/skill-versioning.ts:1-15`）。另有一个已写好但**决定不启用**的 name 快速通道，注释完整记录了否决理由（`skill-fast-path.ts:17-24`）。

**自定义 prompt 护栏**（`MemoryCore/src/core/memory-prompt/composer.ts`）：允许 team/agent 级自定义 L1/L2/L3 提取策略（按 Agent > Team > Instance > 内置解析），但自定义文本被包进 `<CUSTOM_MEMORY_STRATEGY>` 块、尾部追加"不得修改输出协议，冲突时以系统约束为准"的 GUARD，并对自定义文本做 closing-tag 转义防注入。配套有生成溯源日志（记录每次生成用的 prompt id/版本/hash，不存正文——`MemoryCore/README_CN.md` "生成溯源"）。

### 2.3 MemoryKnowledge：Wiki + CodeGraph 即工具

Wiki 摄取支持两阶段（先"分析"产出结构化抽取计划，再"生成"落盘页面，解耦"抽什么"与"落盘格式"，`MemoryKnowledge/src/engines/wiki/ingest-v2/prompts.ts:7-13`）；全部摄取完再让 LLM 写一篇 overview.md 全局综述，用 `[[wikilink]]` 串各页（`ingest-v2/overview.ts`）。CodeGraph 模块自述复用自开源项目 codegraph（README_CN.md 致谢）。召回不整库注入：agent 先 `POST /tools/list` 发现能力，再 `/tools/call` 按需读（`MemoryKnowledge/src/routes/tools.ts:1-12`），另有 MCP server 形态。

### 2.4 MemoryPanel：资产管理面板

Hono + React 的"团队记忆管控平台"（`MemoryPanel/package.json` description）：team/user/agent/task CRUD、四类资产的 Owner/版本/状态/可见性（private/team/restricted/agent 四级 + ACL）/使用次数/Agent 绑定。这是"团队版"价值的主体，也是与 OK 定位差异最大的部分。

## 3. 与 OpenKnowledge 的对照

| 维度 | TencentDB Agent Memory | OpenKnowledge |
|---|---|---|
| 接入方式 | **LLM 代理**（改 base URL，协议级拦截，零插件） | hooks/技能写入各 agent（`internal/agentx`） |
| 身份模型 | Team/User/Agent/Task 四级 + ACL + 资产配装 | 项目级隔离，单用户 |
| 沉淀触发 | round 级回流 + core 阈值归档；L1 轮数/idle/关机三触发；L3 五条件触发 | 轮次间隔自省提醒（`internal/hook/core.go:381-385`）+ AI 主动 propose |
| 沉淀质量门 | LLM 提取时 priority 打分（低分丢弃）+ 两阶段去重裁决（store/skip/update/merge） | 人审草稿闸门（draft 不参与检索），无自动去重合并 |
| 知识形态 | L0 原文 + L1 原子 + L2 场景 md + L3 画像 + Skill（含版本） | 单文件条目 + frontmatter（rule/pitfall/note/reference） |
| 检索 | BM25 + 向量 + RRF（k=60），降级链完整 | SQLite FTS5 + 向量 + RRF，通道独立准入 |
| 注入预算 | 两级字符预算（单条截断 + 总量，code point 安全） | token 总预算 + mandatory 护栏（`internal/hook/core.go:132-142,238-247`） |
| 注入稳定性 | session_init 预暖缓存保字节一致，专为上游 prompt cache | 每轮检索结果直接拼入，未做稳定性分层 |
| 检索查询 | 显式净化 user_query（去 harness 噪声块） | 直接用用户 prompt 检索，未见净化处理 |
| 人审 | 默认私有 + 面板审核分享；记忆编辑能力 v2.0.1 才补 | propose 草稿必须人批准（核心差异化设计） |
| 冷启动 | 导入文档/代码库/历史会话自动建资产 | `ok init` + wiki 技能扫描代码与 git 历史 |
| 基础设施 | docker 三件套（云侧还有 ClickHouse/Redis/COS/tcvdb 适配） | 单二进制 + SQLite，零依赖 |

一个值得注意的对照点：对方 proxy 模式下**没有项目目录概念**，会话归属要靠会话内表单问用户（§2.1），这正是 hooks 模式天然免费获得的信息（cwd）；反之 hooks 模式做不到的，proxy 能拿到完整对话原文做离线提炼——OK 明确不存会话原文，原料差异决定了对方的 LLM 提取管线在 OK 大多无米下锅。

## 4. 可借鉴清单

### 值得做

**A1. 检索查询净化（防自污染）。** 他们用实测证明了噪声块能把 FTS/向量命中率打到 0（`tdai-l1-recall-injector.ts:51-55`）。OK 现状：`UserPromptSubmit` 直接拿用户 prompt 当检索词，而 OK 自己注入的 mandatory/索引/上一轮检索块会进入后续会话上下文，若 agent 框架把它们回传进 prompt，检索词就被污染。落地：在 `internal/hook` 检索入口剥离已知注入标记块（OK 注入有自己的包裹格式，剥离是确定性的），成本小、收益直接。**这是本次调研对 OK 最具体的一条。**

**A2. 读/写对称的细粒度门控。** extraction-gate 的核心不是实现（一个纯谓词），是语义：读侧注入白名单、写侧沉淀白名单、缺省宽容、被拦打结构化 debug 日志（`extraction-gate.ts:35-73`）。OK 现状：全局开关 + capture propose/auto 是粗粒度总闸，没有"只注入不沉淀"或"按条目类型分别开关"的能力。落地：`config.toml` 增加 `[inject]` 按类型/通道的启用列表与 `[capture]` 独立于注入的开关，GUI 设置页对应暴露。与 OK 现有配置分层完全兼容。

**A3. 召回两级预算 + code point 安全截断。** 对方单条截断 + 总量预算、溢出整条丢弃并记账（`auto-recall.ts:835-899`）。OK 现状：`inject.max_tokens` 只有总量截断，一条超长条目能吃掉整个检索段的预算；`TruncateToBudget` 的截断粒度未核实是否 rune 安全。落地：给检索注入段加 per-entry 上限（如 500 token），截断/丢弃写日志，与现有"mandatory 超预算告警"（`internal/hook/core.go:141`）同款记账风格。

**A4. 沉淀时的"打分门"。** L1 prompt 把"宁缺毋滥"落成具体分数区间与"低于阈值直接丢弃"（`l1-extraction.ts:43-56`），work 版还有"准确归因"（建议≠决策）和"AI 输出未经采纳不得提取"（`:163-180`）。OK 现状：auto 自省提醒只有一句"非显而易见的坑或解法"。落地：propose/capture 技能提示词加入价值自评（这条经验 90+ 核心规则 / 70+ 有复用价值 / <70 不要沉淀）+ "未验证的猜测不得写成结论"——纯提示词改动，与 acontext 调研 B1（skip_learning）同族互补。

**A5. 注入内容的 prompt-cache 稳定性分层。** 对方为保上游 KV cache 字节命中做了 session 预暖缓存（`pipeline.ts:269-328`）。OK 每轮把检索结果拼进 user prompt，mandatory/INDEX 这些**稳定段**若与变动的检索段混排，会持续破坏 prompt cache。落地：注入模板固定段序（稳定内容在前、检索结果在后且包裹标记固定），强制首轮注入的内容跨轮保持字节一致——零架构改动，只调 `internal/hook/core.go` 的拼装顺序与文案。

**A6. propose 批准侧的重复候选提示。** 对方的两阶段裁决（候选召回 + store/skip/update/merge）全自动，不适合 OK 的无人在环原则；但**第一阶段（给新草稿召回同域 top-K 候选）本身与人审天然兼容**。落地：`ok propose` 落草稿时用现有混合检索找 top-5 同域条目，在 `ok list`/GUI 草稿详情里展示"疑似重复/可合并"提示，由人决定合并——复用现有检索，无 LLM 裁决，保留人审闸门。

### 可选

**B1. L3 触发器的多条件设计。** 冷启动首轮即触发、内容丢失自动恢复、阈值兜底（`persona-trigger.ts:36-101`）比 OK 的单一 turn_interval 细腻；可给 capture 加"首次启用后首轮即提醒"与"超 N 轮未沉淀兜底提醒"。改动在 `internal/state` + `internal/hook/core.go`，价值一般。

**B2. 自定义提取策略的护栏格式。** `<CUSTOM_MEMORY_STRATEGY>` + 系统 GUARD 尾部压制 + closing-tag 转义（`memory-prompt/composer.ts`）。OK 若将来开放用户自定义自省/提取提示词，此格式直接可抄；当前无此功能，标记备用。

**B3. 生成溯源。** 对方记录每条记忆由哪个 prompt 版本、哪批输入生成（不存正文）。OK 可给 propose 草稿 frontmatter 加来源会话标识（OK 会话状态已有 session 概念），回答"这条经验哪来的"，对 pitfall 类尤其有用。

**B4. wiki 两阶段摄取与 overview 综述页。** "分析（抽什么）→ 生成（怎么落盘）"解耦 + 全量完成后生成 wikilink 综述页（`ingest-v2/prompts.ts:7-13`、`overview.ts`）。OK wiki 技能已有 INDEX.md 与增量维护；overview 叙事页是低成本增益，两阶段解耦待 wiki 质量成为瓶颈时再引。

**B5. 评审者角色隔离写法。** "你是评审者不是参与者" + transcript 标记包裹 + 防 transcript 内指令劫持（`skill-review-prompt.ts:40-51`）。OK 未来若用 LLM 评审会话记录/批量回填，这套防注入写法必备；当前无对应场景，备用。

### 不建议

**C1. proxy 接入路线本身。** 改 base URL 的代理模式与 OK "hooks + 技能、零常驻中间人、fail-open 绝不影响会话"的定位冲突：proxy 一旦挂掉/延迟就是 LLM 请求路径上的故障点与延迟源，且 HTTPS 各家 base URL 可配性参差。OK 的 hooks 模式拿不到对话原文是事实，但换来的部署与信任成本优势是核心卖点，不应动摇。

**C2. 团队/ACL/资产配装体系。** Team/User/Agent/Task 四级身份 + 四级可见性 + ACL 是对方价值主体，也是复杂度主体（session-init 表单、身份解析、binding 仓库等大量代码都为此服务）。OK 是单机单用户本地库，引入任何一件都是净负担。

**C3. L0 原文留存 + LLM 自动提取管线。** 与 OK "不存会话原文"的隐私定位冲突（同 acontext 调研结论）；且对方自己的 ROADMAP 已承认自动提取的准确率需要"面板可编辑"兜底——"给人修正入口，比追求抽取百分百正确更现实"（ROADMAP_CN.md）——这句话本身倒是值得 OK 记住。

**C4. CodeGraph。** 代码符号/调用图索引超出 OK 知识库的问题域；对方该模块也是直接复用第三方开源项目（README 致谢），无独特经验可借鉴。

**C5. mem: 指令与 session-init 表单。** OK 的六个技能已覆盖会话内操作，且 hooks 模式天然知道项目身份，无需问用户。等价能力已有。

## 5. 风险与需注意的点

- **公开仓库是 squash 镜像**（17 个提交、无开发历史），且带有明显的内部云系统裁剪痕迹（ClickHouse/Redis/COS/tcvdb 适配、腾讯云网关配置注释）。读到的设计注释（含否决记录）可信度较高，但**无法判断开源代码与生产版本的差距**，README 的 benchmark 数据（PersonaMem +59%）无仓库内证据。
- **v2 理念翻转的教训要完整吸收**：skill review 从"严格门控"翻到"when in doubt, capture"是因为 46% 覆盖率（`skill-review-prompt.ts:4-37`）——但这发生在他们**无人在环、自动写入**的语境。OK 有人审草稿闸门，两者结论不能直接互换：OK 应吸收的是"门控太严会饿死沉淀"（A4 的打分门提示词要给出明确低门槛），而非放弃人审。
- **对方的 fail-open 默认与 OK 一致**（dedup 失败全 store、gate 缺配置放行、hook 失败仅记日志），说明这条路线在两边都被验证，OK 后续新增任何自动链路应继续保持。
- 对方大量精心设计（注入缓存、身份表单、ACL）都是 proxy/团队定位的衍生物，借鉴时**只取语义层**（门控、预算、打分、净化、稳定性），不抄实现形态。

## 6. 行动建议

| 优先级 | 事项 | 落地位置 |
|---|---|---|
| 高 | 检索查询净化：剥离已知注入标记块再做混合检索（A1） | `internal/hook` 检索入口 |
| 高 | propose/capture 技能提示词加价值打分门 + 归因规则（A4） | 技能源文件（`internal/setupx` 安装） |
| 中 | 读/写对称门控：注入按类型白名单 + 沉淀独立开关，缺省宽容 + debug 日志（A2） | `internal/config`、`internal/hook`、GUI 设置页 |
| 中 | 检索注入段 per-entry 截断 + rune 安全 + 丢弃记账（A3） | `internal/hook/core.go` 拼装段、`internal/store` 截断函数 |
| 中 | 注入内容稳定段/变动段分层，段序与包裹标记固定（A5） | `internal/hook/core.go` |
| 低 | propose 草稿展示"疑似重复/可合并"同域候选（A6） | `internal/cli`（propose）、`internal/gui`、`web/` |
| 备用 | 自定义提示词护栏（B2）、草稿来源会话溯源（B3）、wiki overview 页（B4）、评审者角色隔离（B5） | 待对应功能启动时引用本节 |
