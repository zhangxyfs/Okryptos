# Acontext 调研：Agent Skills as a Memory Layer 对 OpenKnowledge 的借鉴价值

- 日期：2026-08-23
- 调研对象：[Acontext](https://github.com/memodb-io/acontext)（本地仓库 `D:\develop\Acontext`，工作区当前状态；Apache 2.0。"Agent Skills as a Memory Layer"，Go API + Python Core + RabbitMQ + PostgreSQL/pgvector + S3，Python/TS 双 SDK，OpenClaw 与 Claude Code 插件）
- 调研目的：评估其"从 agent 运行中自动学习并沉淀为 skill 文件"的管线设计，哪些可移植到 OpenKnowledge（Go 单体、本地知识库、hooks 注入 + propose 人审沉淀）
- 阅读范围：README.md、ROADMAP.md、AGENTS.md 全文；`src/server/core/` 的学习管线（task agent、蒸馏、skill learner agent、消息缓冲）与 prompt 全文；`src/server/api/go` 的 handler 路由面；两个插件包的 README/入口；e2e conftest；docs/content 的 learn/quick 等。未逐行读 OpenKnowledge，模块级理解来自 README、`internal/hook/core.go`、既有评审文档

> 独立第三方调研，非官方对比。文中路径均相对 Acontext 仓库根目录，未标注行号的引用基于本次阅读时的文件内容。

---

## 1. Acontext 是什么

Acontext 把"agent 记忆"做成**纯 Markdown 技能文件**：后台监听会话消息流，用 LLM 从中提取任务、在任务完成/失败时蒸馏出可复用经验，由一个 skill learner agent 决定写入哪个 skill（或新建），产物是符合 `SKILL.md` 规范（YAML front matter + 文件组织约定）的目录树，可 ZIP 导出、可 git 管理、无 embedding 锁定（README.md "The Philosophy of Acontext"）。核心主张是 **Skill is Memory**：记忆不该是不可检查的向量，而该是人和 agent 都能读、能改、能跨框架复用的文件。召回不做语义 top-k 检索，而是 progressive disclosure——agent 自己通过 `get_skill`/`get_skill_file` 等工具按需取文件（README.md "How It Works"）。

## 2. 架构与核心机制

### 2.1 部署形态

双服务 + 消息队列（README.md "Architecture"，AGENTS.md "MODULES"）：

- **API**（`src/server/api/go/`，Go + Gin + GORM/PostgreSQL + Redis + RabbitMQ + S3 + Swagger）：唯一的对外 REST 面，负责鉴权（project bearer token）、会话/消息持久化、skill 文件管理，不碰 LLM。
- **Core**（`src/server/core/`，Python + FastAPI + SQLAlchemy + RabbitMQ + S3）：所有 AI 逻辑——消息缓冲、任务提取、蒸馏、skill 写入 agent。经 MQ 消费 API 发布的事件。
- 周边：Python/TS SDK（`src/client/acontext-py|ts`）、CLI（部署 + 模板下载）、双 Dashboard（商业版 `dashboard/` + OSS 版 `src/server/ui/`，AGENTS.md 要求双写同步）、OpenClaw/Claude Code 插件（`src/packages/`）、Helm chart（`charts/`）。

API 端点面（`src/server/api/go/internal/modules/handler/*.go` 的 `@Router` 注解）：session CRUD + `messages` + `flush` + `token_counts` + `observing_status`；`learning_spaces` CRUD + `learn` + `skills` 挂载 + 学习状态查询；`agent_skills` CRUD + `file`（单文件读取）+ `download_zip` + `download_to_sandbox`；另有 disk/artifact（虚拟文件系统）、sandbox、metrics、admin。Swagger 注解内嵌 `@x-code-samples` 双语言 SDK 示例（如 `handler/learning_space.go:306`）。

### 2.2 Store 链路（学习管线）

四个阶段，全部异步、MQ 驱动：

**a) 消息缓冲与去抖**（`src/server/core/acontext_core/service/session_message.py`）。`store_message` 后 API 发 `session.message.insert` MQ 事件；Core 消费时若 session 未处理消息数 < `buffer_max_turns`（project 级配置），把消息转入**延迟队列**——RabbitMQ message TTL + DLX 死信回流实现，注释明言是为了"survives restarts, no fire-and-forget"（`session_message.py:128-139`）；攒够轮次或收到 `flush` 才进入处理。处理全程持 Redis session 锁，锁冲突走 retry 队列再次 TTL 回流（`:73-86, 142-152`）。

**b) 任务提取**（`llm/agent/task.py` + `llm/prompt/task.py`）。一个 LLM agent 分析缓冲的消息批次，用工具集 `insert_task` / `update_task` / `append_messages_to_task`（消息区间挂接）/ `append_task_progress` / `submit_user_preference` / `finish` / `report_thinking` 维护任务列表。Prompt 的关键设计：**任务 = 用户请求，不是 agent 执行步骤**（"Book a reservation" 是一个任务，拆成搜索/填表是错误的）；任务描述用用户原话；状态机 `pending|running|success|failed`；progress 只记里程碑级、每条 ≤150 字符；用户偏好（task-independent）单独走 `submit_user_preference`。支持 project 级自定义 success/failure 判定标准（`pack_task_input` 的 `## Task Evaluation Criteria`）。

**c) 蒸馏**（`service/controller/skill_learner.py:28-148` + `llm/prompt/skill_distillation.py`）。任务变为 success/failed 时 task agent 发布 `learning.skill.distill` 事件（`llm/agent/task.py:261-278`）；蒸馏消费者取任务原文消息，单次 LLM 调用产出结构化结果，**三分支工具设计**：`skip_learning`（琐碎任务直接不学：闲聊、一次性问答）/ `report_success_analysis`（task_goal、approach、key_decisions、generalizable_pattern、applies_when）/ `report_factual_content`（纯事实记录，禁止把事实" inflate 成假流程"）；失败任务走 `report_failure_analysis`（failure_point、flawed_reasoning、what_should_have_been_done 标注为"most valuable field"、prevention_principle）。`applies_when` 字段显式要求写明作用域（"if the task was about flower-sunshine.com, say so"）防止过度泛化。蒸馏不值得学时整个流程终止并标记 completed（`service/skill_learner.py:91-95`）。用户偏好**绕过蒸馏**直接以 `## User Preferences Observed` 文本发给 skill agent（`llm/agent/task.py:279-297`）。

**d) skill 写入路由**（`llm/agent/skill_learner.py` + `llm/prompt/skill_learner.py`）。skill learner agent 拿到蒸馏文本 + 现有 skill 清单，持 Redis 锁（per learning space）跑工具循环，工具集：`get_skill` / `get_skill_file` / `str_replace_skill_file` / `create_skill` / `create_skill_file` / `delete_skill_file` / `mv_skill_file` / `finish` / `report_thinking`（`llm/tool/skill_learner_tools.py:51-68`）。这是整个系统最值得读的 prompt，核心规则：

- **update-vs-create 决策树**：同域→更新；部分重叠→更新并拓宽；零覆盖才新建；**禁止创建窄用途 skill**（"login-401-token-expiry" 禁止，"authentication-patterns" 鼓励）；"Prefer updating over creating — fewer rich skills > many thin ones"。
- **三种 entry 模板**：Success（Principle/When to Apply/Steps）、Failure（Symptom/Root Cause/Correct Approach/Prevention）、User Preference（第三人称单行事实）。
- **第三人称铁律**：记忆会被其他 agent 读，"I" 会被误认成它自己（`skill_learner.py:134`）。
- **SKILL.md 权威**：改任何 skill 前必须先读其 SKILL.md，skill 内的自定义书写约定（如 "daily-log 要求 yyyy-mm-dd.md"）必须遵守——即**用户可以上传一个"工作上下文 skill"来定义整个记忆库的 schema**（README "You design the structure"）。
- **report_thinking 强制前置**：任何修改前必须显式报告思考（哪个现有 skill 相关、更新还是新建、准备写入的 entry 原文）。

并发处理也很讲究：锁内运行期间新到的蒸馏结果进 pending 队列，agent 每轮迭代后 drain 并作为 follow-up user message 注入同一轮对话（`llm/agent/skill_learner.py:153-176`），附更新后的 skill 清单，并奖励额外迭代次数；异常时把已 drain 的上下文重新推回队列不丢数据（`:187-200`）。会话学习全程有状态机可观测：`pending → distilling → skill_writing → completed/failed`，锁冲突时 `queued`（`schema/session/learning_space.py:4-16`，注释要求 Go/Python/双 SDK 四处同步）。

**e) 存储形态**：skill 文件存在 S3  backed 的虚拟 disk/artifact 系统里（`llm/tool/skill_learner_lib/get_skill_file.py` 经 `disk_id + path` 查 artifact），天然支持 `download_zip` 导出与挂载进 sandbox。

### 2.3 Recall 链路

没有服务端检索。两条实际路径：

- **工具调用**：给 agent 配 skill content tools（`get_skill`/`get_skill_file`），agent 按 reasoning 自取（README "Progressive disclosure, not search"）。
- **同步进宿主原生技能目录**（更务实的一条）：Claude Code 插件把学到的 skills 下载到 `~/.claude/skills/`，由 Claude Code 自己的技能加载机制在后续会话原生加载（`src/packages/claude-code/README.md` "Skill sync"，按 `updated_at` 增量、30 分钟 manifest TTL）；OpenClaw 插件同构（`~/.openclaw/skills/`，`src/packages/openclaw/index.ts` 头注释）。插件自带的 `acontext_search_skills` 工具是**关键词 grep**（openclaw 版经 `artifacts.grepArtifacts`），不是语义检索。

### 2.4 embedding 的真实处境（与 README 的对照）

README 宣称 "No embeddings, no API lock-in"。核实结果：**对当前学习/召回链路成立**，但仓库里有一套未接线的 embedding 设施：`llm/embeddings/`（openai/jina 两个 provider + `embedding_sanity_check`）在 Core 里唯一的引用是 `di.py:9` 一行**被注释掉的**启动检查；service 层零调用；Go API 无语义检索端点；pgvector 仅在 `infra/db.py:196` 初始化扩展。配置项名是 `block_embedding_*`（`llm/embeddings/__init__.py:14`），暗示这是更早"context block 检索"的遗留（此为命名推断）。ROADMAP v0.1 又把 "Session search: support session search by embedding similarity" 列为未完成项——即 embedding 检索是**被搁置后计划以限定场景回归**，而非理念上彻底拒绝。宣传口径与代码现状之间有这个落差，值得记录。

### 2.5 插件与 SDK 生态

- **Claude Code 插件**（`src/packages/claude-code/`）：两个独立 Node bundle——`hook-handler.cjs`（session-start/post-tool-use/stop 三个生命周期 hook，从 transcript JSONL 增量捕获，游标存 `.session-state.json`）+ `mcp-server.cjs`（stdio MCP，5 个工具）；经 `plugin/data/` 目录共享状态。**Auto-learn 触发是轮次阈值**（`ACONTEXT_MIN_TURNS_FOR_LEARN=4`），stop hook 时最终捕获并触发学习。
- **OpenClaw 插件**（`src/packages/openclaw/index.ts`）：同构，3 个工具 + CLI 子命令。
- 发布工程（AGENTS.md "Version Bump"）：每个组件独立 tag 前缀（`api/vX.Y.Z`、`sdk-py/...`）映射独立 workflow 与发布目标；changelog 由脚本按路径 scope 从 conventional commits 生成；npm 用 OIDC trusted publisher。
- 开发纪律（AGENTS.md）：**plan-driven development**——`plans/`（gitignored）先写计划否则拒绝实现；计划必须含 features/designs/TODOS/new deps/test cases；改 API 必查双 SDK（Python 同步+异步双份）；改 SDK 必查文档；API 与 Core 双 ORM 手工同步；swagger 注解的 `swaggertype` 坑直接写进规则。

### 2.6 测试组织

Core 内大量单测（`src/server/core/tests/`，含蒸馏/prompt/触发器/并发专项）；e2e 在 `src/server/tests/e2e/`，docker-compose 起全栈（API+Core+PG+Redis+MQ+S3），conftest **直插 DB 造 project** 并把 `buffer_max_turns=1` 关掉缓冲以缩短异步等待，断言靠轮询学习状态机（`tests/e2e/conftest.py:50-71`）。

## 3. 与 OpenKnowledge 的对照

| 维度 | Acontext | OpenKnowledge |
|---|---|---|
| 部署 | 云服务/自托管五件套（PG+Redis+MQ+S3+双服务） | 单二进制 + SQLite 单文件，零基础设施 |
| 原始输入 | 会话消息全文上云存储 | 不存会话原文，只存提炼后的条目 |
| 学习触发 | **任务完成/失败**（LLM 从消息流提取任务） | 轮次间隔自省提醒（`internal/hook/core.go:383`）+ AI 主动提议 |
| 提炼方式 | 两段式 LLM 管线（蒸馏 → 写入 agent），全自动 | agent 会话内自行提炼，`ok propose` 写草稿 |
| 人审 | 无（自动写入；靠文件可读可改兜底） | 草稿 `draft: true` 不参与检索，人批准后转正（README "草稿流程"） |
| 知识形态 | skill 目录树（SKILL.md + 多文件，schema 可由用户 skill 定义） | 单文件条目 + front matter（type: rule/pitfall/note/reference） |
| 更新语义 | **更新已有 skill 优先于新建**（决策树强制） | propose 只新增；wiki 技能有增量维护；普通条目靠人/GUI 改 |
| 召回 | 工具调用 progressive disclosure / 同步进宿主技能目录 | hook 强制注入（mandatory 全文 + 混合检索指针式注入） |
| 失败利用 | 失败任务是**一等学习源**（独立蒸馏分支与 Warning 模板） | pitfall 类型靠 agent 自觉，无失败检测 |
| 多 agent | SDK + 框架模板 + 两个官方插件 | hooks 适配器（`internal/agentx`）+ 六个技能 |

两边其实在一个点上殊途同归：**正文不直接进上下文**。Acontext 靠工具调用按需取文件，OK 注入的只是"标题+摘要+路径"指针（`docs/2026-08-19-openviking-retrieval-review.md:78` 已总结）。

## 4. 可借鉴点

### 4.1 架构级

**A1. 以"任务完成/失败"为学习触发器，失败是一等公民。** Acontext 的触发器是任务结局而非时间流逝（`llm/agent/task.py:261-278`），且失败任务有独立蒸馏 prompt 与 Warning entry 模板——`what_should_have_been_done` 被标注为"most valuable field"（`llm/prompt/skill_distillation.py:32-42`）。OK 现状：auto 模式按轮次间隔软提醒（`internal/hook/core.go:383-388`），提醒文案只有"非显而易见的坑或解法"一句，成功/失败无分别。落地：在 `internal/hook` 的 Stop 评估与会话状态（`internal/state`）里增加轻量"任务结局信号"——例如本会话出现过测试失败/命令报错（PostToolUse 已能观察工具结果）、或 enforce 阻断曾被触发，则自省提示词切换为失败导向变体（要求按 Symptom/Root Cause/Correct Approach/Prevention 结构回顾）。不需要引入 LLM 任务提取，利用 hook 已观察到的信号即可，成本集中在提示词与状态字段。

**A2. 两段式"蒸馏 → 写入路由"分离，写入侧强制 update-vs-create 决策树。** Acontext 把"从轨迹提炼结论"和"结论写去哪"拆成两个 LLM 阶段，后者拿着现有 skill 清单做路由，决策树与反窄命名规则直接写在 prompt 里（`llm/prompt/skill_learner.py:48-62,133`）。OK 现状：`ok propose` 只产生新草稿，没有"这个经验应该更新到已有条目 X"的路由机制，长期会导致同域条目碎片化。落地：propose/capture 技能（技能文件在 agent 技能目录，由 `internal/setupx` 安装）的提示词中加入决策树——先 `ok list`/`ok search` 查同域条目，同域则用"建议更新已有条目"的草稿形式（或追加到既有条目正文）而非新建；蒸馏侧将来若引入 LLM 后台提炼（`internal/llmx` 已有客户端），沿用两段拆分而非一次出稿。

**A3. 召回向"宿主原生技能目录"借力。** Acontext 插件最务实的设计不是工具调用，而是把学到的 skill 同步进 `~/.claude/skills/` 让宿主自己加载（`src/packages/claude-code/README.md:81-83`）。OK 已经把六个管理技能写进各 agent 的技能目录，但知识条目本身仍走 hook 强制注入。启示是渐进的：可以考虑把 **wiki/reference 类条目以技能目录形态导出**（每个条目一个 SKILL.md 片段，或按域聚合），让宿主的原生 progressive disclosure 替代一部分每轮检索注入，省 token 且让模型按需拉取。这与 OK 现有"指针式注入"哲学一致，落地位置在 `internal/setupx`（技能写入）与 `internal/wiki`，属中期方向而非紧急事项。

**A4. 学习过程状态机 + 可观测。** Acontext 的学习有显式状态机（pending/distilling/skill_writing/queued/completed/failed）且要求四端同步（`schema/session/learning_space.py:4-16`），SDK 提供 `wait_for_learning` 轮询。OK 的沉淀链路（提醒 → propose → approve）目前无状态跟踪，auto 模式提醒过没有、agent 理没理，无账可查。落地：`internal/state` 的会话状态已有 `LastExtractReminder` 字段，可扩展记录"提醒后 N 轮内是否出现 propose 事件"（`ok propose` 经 CLI 落盘，可归因到会话），在 GUI（`internal/gui` + `web/`）管理页展示沉淀转化率——这同时回应了 OK 已有的"检索健康统计"缺口（见 openviking 调研 §3.4）。

### 4.2 细节级

**B1. skip_learning 琐碎门控。** 蒸馏第一分支是"不值得学就跳过"（`llm/prompt/skill_distillation.py:11-14`），且给了具体反例（闲聊、一次性计算）。OK 的 auto 自省提示词可以照抄这条：提醒文案显式列出"琐碎任务不要沉淀"的反例，减少低质量草稿。成本是一行提示词，落 `internal/hook/core.go:386`。

**B2. applies_when 作用域字段防过度泛化。** "if the task was about flower-sunshine.com, say so"（`llm/prompt/skill_distillation.py:20`）。OK 条目的 front matter 有 summary/tags 但无"适用条件"约定；可在 propose 技能与 pitfall/note 条目的书写指引中要求写明作用域（哪个项目/工具/环境下成立），与 OK 已有的"泛化门控"（README 设置页）在思想上正好互补：一个管注入时的泛化，一个管沉淀时的泛化。

**B3. 第三人称铁律。** "这些记忆会被其他 agent 读，'I' 会让它误以为是自己的经历"（`llm/prompt/skill_learner.py:134`）。OK 条目同样是写给"未来会话里可能是另一个 agent"看的，propose 技能/书写指引可直接吸收此规则。

**B4. report_thinking 强制前置推理。** 任何写入前必须先显式回答"相关现有条目有哪些/更新还是新建/准备写入的 entry 原文"（`llm/prompt/skill_learner.py:136-146`）。OK 的 propose 技能流程里加一步"先复述将写入的草稿全文并说明为何不合入已有条目"即可，无需代码改动。

**B5. original_date 透传。** 蒸馏时用会话原始日期而非"今天"，避免重放旧会话时日期污染（`llm/agent/skill_learner.py:80-82`）。OK 若未来做历史会话/批量回填类功能，此细节直接可抄；当前无对应场景，标记备用。

**B6. 缓冲去抖的实现取舍。** Acontext 用 MQ TTL+DLX 替代内存定时器以求重启安全（`service/session_message.py:128-152` 的注释直白记录了这次重构动机）。OK 单体无此问题，但"攒够 N 轮再处理 + flush 强制立即处理"的语义 OK 已有对应物（turn_interval + Stop 时最终提醒）；e2e 测试中把 `buffer_max_turns=1` 关掉异步等待的技巧（`tests/e2e/conftest.py:57-59`）可用于 OK 将来任何异步管线的测试加速。

**B7. AGENTS.md 工程纪律条款。** Acontext 的 AGENTS.md 把跨模块同步义务写成硬规则：改 API 必查双 SDK 与文档、双 ORM 手工同步、plan-driven 开发（`plans/` gitignored）。OK 的对应风险是"hooks 协议/okd API 改了，agentx 适配器、web/ 前端、docs 契约不同步"——`docs/2026-08-21-okd-contract.md` 已是契约文档，可把"改契约必查 N 个消费方"写进项目 AGENTS.md。plan-driven 一条 OK 已有 docs/superpowers/specs 实践，可不照搬 gitignored plans/ 形式。

**B8. 发布矩阵与路径 scope 的 changelog。** 每组件独立 tag 前缀 → 独立 workflow → changelog 由脚本按目录 scope 聚合 conventional commits（AGENTS.md "Version Bump" 节）。OK 目前是单产物发布（`scripts/build-dist.sh`），暂不需要矩阵；若未来 ok/okd/OkManager 或技能包分拆版本，此模式可直接参考。当前标记备用。

## 5. 不适合借鉴 / 需警惕的点

- **重基础设施栈**（PG + Redis + RabbitMQ + S3 + pgvector + 双语言双服务）。这与 OK "单二进制、单 SQLite 文件、零运行时依赖"的根本定位冲突（README）。Acontext 大量复杂度（MQ 延迟队列、Redis 锁、双 ORM 同步、双 dashboard 双写）都是这套栈的衍生物，OK 引入任何一件都是净亏损。借鉴应停留在**设计语义层**（触发器、决策树、状态机），不抄实现。
- **全自动写入、无人在环。** Acontext 蒸馏结果直接改知识库，靠"文件可读可改"兜底纠错。OK 的 propose 草稿 + 人批准是有意的差异化设计（README "草稿流程"），且 OK 条目会被**强制注入**到后续会话——低质量自动写入的破坏力比 Acontext 的按需拉取模式大得多。即使引入 LLM 蒸馏，也必须保留草稿闸门。
- **会话全文持久化。** Acontext 的价值前提是完整消息流上云（还有 KEK 加密一整套）。OK 明确不存会话原文（隐私与本地定位），蒸馏素材只有 hook 观察到的信号与条目本身，这决定了 Acontext 的 LLM 任务提取管线在 OK 无原料可用——A1 才强调用轻量信号替代。
- **session 编辑/摘要、sandbox、disk 等上下文工程功能**（docs/content 的 engineering/、store/ 章节）超出 OK 的问题域，不评估。
- **embedding 的摇摆口径**（§2.4）：README 宣称 no embeddings，代码里有未接线的设施、ROADMAP 又计划加 session search embedding。提醒：OK 自己的文档宣称（如"混合检索"）也应与实际默认配置（可纯关键词）保持口径一致，避免类似落差。

## 6. 行动建议

| 优先级 | 事项 | 落地位置 |
|---|---|---|
| 高 | propose/capture 技能提示词加入 update-vs-create 决策树 + 第三人称 + applies_when 要求（A2/B2/B3/B4 合并，纯提示词改动） | 技能源文件（`internal/setupx` 安装的技能） |
| 中 | auto 自省提醒分化：检测到失败信号时用失败导向提示词（A1/B1） | `internal/hook/core.go` + `internal/state` |
| 中 | 沉淀转化状态跟踪：提醒 → propose → approve 归因与 GUI 展示（A4） | `internal/state`、`internal/gui`、`web/` |
| 低 | wiki/reference 条目以技能目录形态导出，借力宿主原生加载（A3） | `internal/wiki` + `internal/setupx`（中期方向，先验证宿主加载成本） |
| 备用 | AGENTS.md 增加"改契约必查消费方"条款（B7）；original_date 与发布矩阵（B5/B8）待对应场景出现 | 项目 AGENTS.md |
