# 团队同步方案设计：git 传输 + 双层空间 + PR 人审 + 服务端 AI wiki 管线

- 日期：2026-08-25（同日修订：并入服务端 AI wiki 管线与内容二分；再修订：§8 并入 OpenViking/TencentDB 两仓服务端实现借鉴；再修订：并入个人层双层空间与用户身份模型）
- 状态：待评审（未批准，未进入实施计划）
- 背景讨论：基于四份外部记忆系统调研（`docs/2026-08-23-memory-systems-synthesis.md` 及三份源文档）+ 四轮设计对话 + OpenViking/TencentDB 两仓服务端视角二次调研（队列语义、LLM 生成纪律、git 操作安全、重启恢复）
- 反向印证：两仓均无"commit 真实性核验"与"提案-评审工作流"，且都以"数据集中服务器"消解多主问题——§5A 的 commit 核实、§5B 的 PR 人审、§5C 的个人层是本设计的自有增量，无现成参照

## 1. 目标与定位

为 OpenKnowledge 增加**团队多人多设备**的知识库同步能力：

- **双层知识空间**：
  - **个人层**：每人一个 Gitea 私有仓，放自己的规则/踩坑/按分支组织的 wiki；多机自由同步、无审批；知识先私有验证；
  - **团队层**：共享库，内容经审批生效；个人条目可晋升进团队层；
- **团队层内容按"代码可核实性"二分处理**：
  - wiki / reference（代码派生、可对着 git 记录核实）：成员本地不直接写，走**服务端 AI 管线**——本地提交线索，服务器 AI 核实 commit 后对着真实代码再生正式 wiki；
  - pitfall / rule / note（经验派生、无 git 依据可对照）：走 **git 分支 + PR 人审 diff**，管理者批准后生效；
- **用户身份 = Gitea 账号**，OK 只存身份配置（用户名 + 个人/团队 remote），不建认证/密码体系；权限外包给 git 服务端（个人仓 ownership + 团队仓分支保护）；
- 服务端 AI 能力封装为**独立可选组件 okserver**（NAS 部署优先），单机版 OK 完全不受影响；
- 同步/服务端失败不影响本地任何功能（延续 fail-open），离线可正常工作。

明确不做：

- 不自建用户体系/认证/ACL（用户 = Gitea 账号；个人仓 ownership 即权限；团队仓权限 = Gitea org/team + 分支保护）；
- 不做 okd 中心化审批服务端（TencentDB MemoryPanel 路线，否决：复杂度主体且丢 git 白送的历史/回滚）；
- 不做 NAS 裸共享目录（SQLite/state/config 走网络盘是锁腐败场景；NAS 的角色是放 git 裸仓 / 跑 Gitea / 部署 okserver）；
- 踩坑类修改不走服务器 AI 评审——无 ground truth 可对照，LLM 评 LLM 的文本是意见不是验证，人审才是对的闸门。

## 2. 关键事实依据（方案为什么长这样）

- **条目是纯 markdown 文件，SQLite 是派生索引**：okd 契约（`docs/2026-08-21-okd-contract.md` §数据一致性模型）已确立"条目多写者 + mtime 增量 `db.Sync`"，文件是真相源 → git 直接可搬，clone 后索引自动重建。
- **草稿闸门已存在**：`draft: true` 不参与检索待人批准。团队审批是同一道闸门在团队维度的延伸。
- **AI 会高频更新已有条目**：update-vs-create 决策树落地后（汇总文档 §2.2），同域条目被反复改写是常态 → 冲突设计按"高频同条目并发修改"做。
- **wiki 技能已具备 git 历史增量扫描能力**：团队模式下扫描逻辑复用，仅把输出物从"本地写 wiki"换成"提交线索包"。
- **ACL 是复杂度主体**（TencentDB 调研 C2 的教训）：权限外包，不自建；个人层的权限需求（只有本人能读写）恰好是 Gitea 私有仓 ownership 的原生语义。
- **wiki 由服务端单写后，wiki 类冲突在结构上消失**——成员本地 wiki 只读，唯一写者是 okserver。
- **知识成熟度天然分层**：经验先在个人空间验证、再晋升共享，比"直接往团队库写"门槛低（呼应"沉淀门控太严比太松更伤"——个人层是零门槛兜底）。

## 3. 总体架构

```
个人层（无审批，多机自由同步）：
成员 A 机器 1 ── push/pull ──→ Gitea 私有仓 ok-<userA> ←── push/pull ── 成员 A 机器 2
（个人规则/踩坑/分支 wiki；ownership 即权限）

团队层（审批生效）：
成员 A 本地                     服务端（NAS）                     成员 B 本地
┌──────────────┐  push 分支  ┌───────────────────────┐  pull   ┌──────────────┐
│ knowledge/*.md│ ─────────→ │ Gitea 团队仓            │ ──────→ │ knowledge/*.md│
│ wiki/ (只读)  │ ←── pull ─ │ 分支保护 main + PR 人审 │ ← push ─│ wiki/ (只读)  │
│ 线索生成(AI)  │            └───────────────────────┘         │ 线索生成(AI)  │
└──────┬───────┘                                              └──────────────┘
       │ 线索包(建议+commit 引用)         ↑ 管理者 Gitea 网页端看 diff 审 PR
       ↓                                (pitfall/rule/note + 个人晋升)
┌──────────────────────────────────────────────┐
│ okserver（独立组件，NAS 部署，自配 LLM）         │
│  项目仓克隆 → 核实 commit → AI 对着代码再生 wiki │
│  → 正式 wiki 提交团队仓 main → 全员 sync 回同步  │
└──────────────────────────────────────────────┘

本地检索：个人层 + 团队层合并索引，条目标记来源层。
```

- **传输与历史**：git。NAS 上放裸仓或跑 Gitea；需要网页审批界面与个人仓托管时用 Gitea（项目已有 `.gitea/` 工作流）。
- **权限模型**：个人仓 = Gitea 私有仓 ownership；团队仓 = org/team + main 分支保护。普通成员只能 push 分支、提 PR；管理者 merge。团队 wiki 目录额外由 okserver 独占写入（成员本地只读）。
- **OK 侧新增**：`ok sync` / `ok clone`、身份配置、merge driver、okd 定时同步、GUI 同步状态与冲突页、双层检索合并；**零用户管理代码**。
- **okserver 新增**：线索接收 API、任务队列、项目仓克隆管理、wiki 再生管线、结果回写团队仓 main。

## 4. 同步面边界

| 层 | 同步内容 | 不同步（机器本地） |
|---|---|---|
| 个人层（私有仓） | 个人 `knowledge/*.md`、个人 wiki（含按分支组织的 wiki） | SQLite 索引（派生，本地重建）、state/、daemon.json、日志 |
| 团队层（团队仓） | 团队 `knowledge/*.md`（含草稿与已转正）、团队 `wiki/`（服务端生成、本地只读；INDEX.md 由 okserver 随 wiki 生成） | registry、config.yaml、成员个人 git 凭据 |
| — | 团队模式配置（remote、主分支定义——按仓库级 git config 或首次 clone 下发） | 身份配置本机缓存 |

个人层与团队层在磁盘上是两个独立目录/仓，本地 db.Sync 合并索引；条目在检索注入时带来源层标记（个人/团队）。

## 5. 写入工作流

### 5A. 团队 wiki / reference（代码可核实）——服务端 AI 管线

1. 成员的代码开发并**合并到管理员指定的主分支**（团队模式配置定义，不必是 master）；
2. 成员本地 wiki 技能做增量扫描发现新合并内容 → **团队模式下不写本地团队 wiki**，改由本地 AI 生成**线索包**：建议新增/更新的信息 + 相关 commit 引用；
3. 线索包提交到 okserver（HTTP API）；
4. okserver 在该项目的克隆中**核实 commit 存在且已并入主分支**；
5. 核实通过 → 以线索为种子 + 项目相关代码上下文 → 服务器 AI **生成正式 wiki**；
6. 正式 wiki 由 okserver 提交到团队仓 main → 提交人及全员下次 `ok sync` 自动同步回本地。

设计约束：

- **fail-open**：okserver 不可用 / LLM 挂了 → 线索排队，成员本地一切功能照常，wiki 更新是异步管线绝不阻塞开发；
- 线索里 commit 核实不通过（不存在/未入主分支）→ 拒收并回执原因，提交人本地可见状态；
- 条目 front matter 记录 source commit 范围，天然溯源。

### 5B. 团队 pitfall / rule / note（代码不可核实）——PR 人审

1. 成员本地：AI propose/修订团队条目 → 写入本地团队 `knowledge/`（draft）→ `ok sync` 提交到**个人/任务分支**并 push；
2. `ok sync` push 后自动（或提示）在 Gitea 建 PR；
3. 管理者在 Gitea 网页端**看 diff 审批**（修订审批体验优于现状：只看变化而非整篇）；
4. merge = 批准 → 其他成员下次 sync 拉取后 `db.Sync` 增量重建索引生效。

要点：

- 新建与修订统一走 PR → main 上所有变更经服务端串行合并 → **main 永远无冲突**；
- 冲突面压缩到唯一一处：成员本地 rebase/pull 主线那一刻（§6）；
- 单成员多设备视同"成员"，走同一流程。

### 5C. 个人层（私有规则/踩坑/分支 wiki）——多机自由同步 + 晋升

- 每人一个 Gitea 私有仓 `ok-<username>`（首次 `ok sync` 经 Gitea API 自动创建；ownership 即权限，只有本人能读写）；
- 本地写入**直接生效**（个人层无草稿闸门——自己的库自己负责，保持零门槛）；多机经单分支 push/pull + auto-sync 同步，冲突走 §6 同款 merge driver；
- **晋升**：个人条目值得共享时（`ok propose --to-team` 或 GUI 操作）→ 复制到团队仓个人分支 → 走 §5B PR 人审；批准后个人层原条目去留由人决定（默认保留并标记"已晋升"，避免重复晋升）；
- 个人分支 wiki 由本地 AI 生成（它是"我眼中的代码地图"，无需团队共识）；团队 wiki 仍由 okserver 统一生成，两者互不覆盖。

## 6. 冲突设计（三层吸收）

前提：knowledge/ 下同条目并发修改是常态（§2）。团队 wiki/ 因服务端单写已无并发冲突；个人层冲突只发生在本人多机之间。

**第一层：压缩冲突窗口。** okd 后台定时 auto-sync（分钟级 pull + 本地有提交即 push，个人层/团队层同策略）。

**第二层：领域感知 merge driver。** `.gitattributes` 给 `knowledge/**/*.md` 指定 OK 自带合并器（个人仓与团队仓同款）：

- front matter **字段级合并**（不按行）：tags 取并集；summary 取较新；draft 按规则裁决（已转正优先）；其余标量冲突取较新并记日志；
- 正文**追加语义合并**：union 合并两边都保留；偶发近重复行由已有"疑似重复/可合并"候选提示收尾；
- 只有同行互斥改写才升级为冲突。

**第三层：真冲突进 GUI。** `ok sync` 停下并在 GUI 冲突页列出条目：留我的 / 留对方的 / 手动合并。兜底原则**不丢内容**（被弃方存为副本草稿）。

三种结构性冲突的具体策略：

| 场景 | 策略 |
|---|---|
| add/add 同名撞车 | 自动改名其一落为草稿（个人层：改名保留），两份都保留，交给"疑似重复"候选由人裁决 |
| both modified | merge driver 吸收；吸收不了的进 GUI 冲突页 |
| modify/delete | 保守保留修改版并标草稿，由人确认删除 |

PR 级冲突（两个 PR 改同一条目）：管理者在 Gitea 网页端解决，md 文件小，网页编辑器够用。

## 7. INDEX.md

团队 wiki 的 INDEX.md 由 okserver 随 wiki 生成并提交 main，成员本地不生成 → 原"高频冲突热点"在结构上消除。个人层 wiki 的 INDEX.md 由本地生成、随个人仓同步（个人多机冲突由 merge driver 兜底）。单机模式维持现状。

## 8. okserver 组件

独立可选二进制（NAS 部署优先），单机版 OK 不依赖：

- **部署形态**：NAS 优先（具体 Docker 镜像还是裸二进制见 §12 开放问题）；配置项：监听地址、LLM provider/key、git 仓存储目录、Gitea 凭据。LLM 无配置时**响亮失败、不静默 fallback**（TencentDB config.ts:19-29 的纪律）。
- **项目仓克隆管理**：每个团队项目一个克隆；私有仓凭据配置；定时 fetch 保持新鲜（成员 MR 合并后需可见）；磁盘与多项目隔离。git 操作纪律（借 TencentDB `git-fetcher.ts`）：exec 参数数组不走 shell；URL 仅 https + 私网/环回黑名单（NAS 内网部署留开关）；浅克隆 + fetch/reset；`code-graph-service.ts` 的异步任务 API 形态可抄——提交立即返回、后台串行构建、状态轮询、busy 返 409、任意状态可取消。
- **任务队列**：SQLite 持久化队列（与 OK 单文件哲学一致；OpenViking QueueFS 已实证 SQLite 足以承载 LLM 异步队列）。消息带 `status` + `processing_started_at`，**dequeue → process → ack** 三步，处理中崩溃由启动时 recover_stale 重置回 queued（`queuefs/backend.rs:451-472`）；任务状态机：queued → verifying → generating → committing → done / rejected(reason)。配套三条：
  - **错误三分类**（OpenViking `semantic_processor.py:470-505`）：permanent 丢弃+回执 / input-too-large 丢弃+回执 / transient 重排重试；LLM 故障期熔断 + 重排节流，防重试风暴；
  - **重触发去重**：同一线索主题在处理期间又来了新提交，用 running/pending 双标志（TencentDB L3 的 `l3Running/l3Pending`，988-1022，最简正确写法）或 coalesce_key+version 丢弃过期版本（OpenViking `semantic_queue.py:22-30`）；
  - **重启语义**（TencentDB `manager.ts:810`）：重启时 `verifying/generating/committing` 一律重置回 queued 或标 error——"进行中"状态不可能是真的；队列游标单调推进绝不回退，tmp+rename 原子落盘。
- **commit 核实**（两个仓库均无现成机制，本设计的增量）：`git cat-file -t <sha>` 验存在 + `git merge-base --is-ancestor <sha> <主分支>` 验已并入；不通过 → 拒收回执原因。
- **wiki 再生管线的 LLM 纪律**（重点借 TencentDB ingest-v2 与 OpenViking）：
  - **结构性 grounding**：可用 commit/文件编号 `[1] [2] [3]` 喂给模型，后处理把编号映射回真实 SHA/路径——模型无法编造引用，凡 `[n]` 必在已核实集合内（OpenViking `semantic_processor.py:992-999` 的索引回指手法）；叠加 Faithfulness 提示词条款 + temperature=0；
  - **不信任 LLM 的清单**：LLM 输出走文本协议（FILE 块）而非直接写盘；路径白名单防穿越；结构性文件（INDEX/schema）禁写；关键元数据（source commit、front matter sources）由代码强制补齐而非信 LLM 填写；解析失败原文落 `_debug/` 供排查；每类失败都有"不丢数据"的默认动作（TencentDB `file-protocol.ts` / `index.ts:69-75,368-411` / `l1-writer.ts:222`）；
  - **并行核实生成、串行落盘**（TencentDB ingest-v2 `index.ts:211-283`）：多成员线索可并行过 LLM，写 git main 必须串行单写者；
  - **回写前内容比对**：与现有 wiki 内容相等则不产生 commit，避免 git diff 噪音（OpenViking `semantic_sidecar.py:395-402`）；回写带 base-hash 前置条件，不匹配重拉重试；
  - **半成品哨兵**：生成中的占位内容必须有明确哨兵标记，同步给成员前可识别过滤（OpenViking issue #2434 教训：占位文本曾被当真实摘要向量化）。
- **对账命令**：wiki 是派生数据（源头 = 代码 + 线索），提供 doctor/reconcile：从主数据推导应有产物、比对缺漏（OpenViking `index_consistency.py` 思路）。
- **API**：线索提交、任务状态查询（供成员本地轮询/GUI 展示）。
- **LLM 客户端**：复用 `internal/llmx`（超时按场景区分 + temperature 缺省不传的既有纪律适用）。
- **失败语义**：拒收必须回执原因；transient 失败重排，permanent 失败标记并回执——绝不静默丢线索。
- **职责边界**：okserver 只碰团队仓 wiki 与项目代码仓；个人层完全不经 okserver（个人知识不出本人设备群，除非主动晋升）。

## 9. CLI / GUI 面

- `ok sync`：对当前启用层分别 pull --rebase →（merge driver）→ push（团队层 push 分支 + 可选建 PR；个人层直推单分支）；冲突时非零退出并写冲突状态文件供 GUI 展示；
- `ok clone <url>` / `ok remote`：初始化团队库、配置 remote 与团队模式（主分支定义）；clone 后 `db.Sync` 全量重建；
- `ok whoami` / 身份配置：绑定 Gitea 用户名与个人仓 remote；个人仓不存在时首次 sync 经 Gitea API 自动创建；
- `ok propose --to-team`：个人条目晋升团队层（走 §5B）；
- okd：定时 auto-sync（频率可配，默认建议 5 分钟）+ 线索任务状态轮询；同步失败仅记日志不打断本地；
- GUI：同步状态（落后/领先/冲突，分层显示）、冲突解决页、线索任务状态（排队/生成中/被拒原因）、团队 wiki 只读标识、条目来源层标记与晋升入口。沿用"勾选+保存按钮两段式"交互约定。

## 10. 凭据与安全

- 成员 git 认证走各自既有 git 凭据（SSH key / credential helper），OK 不存储密码；Gitea API 个人仓自动创建用成员自己的 token；
- okserver 持有项目仓只读凭据与 Gitea 团队仓写凭据（提交 wiki），部署时配置；okserver 无任何个人仓凭据（§8 职责边界）；
- LLM API key 仅存在 okserver 本机配置；
- 知识条目可能含敏感信息：Gitea/裸仓须为私有仓库，个人仓默认 private，文档需明确此前提；
- okserver HTTP API 鉴权沿用本机服务安全三件套纪律的适用部分（token、Host 校验）。

## 11. 阶段划分（建议）

| 阶段 | 内容 | 依赖 |
|---|---|---|
| P1 | 个人层先行：身份配置 + 个人私有仓自动创建 + `ok sync`（单仓 push/pull）+ auto-sync + 基础冲突兜底（不丢内容） | 无 |
| P2 | merge driver（front matter 字段级 + 正文 union，个人/团队仓同款） | P1 |
| P3 | 团队层：团队仓 clone/remote + 双层检索合并（来源层标记）+ 晋升入口 | P1 |
| P4 | PR 人审流（团队层 push 分支 + 自动建 PR + 修订 diff 审批引导） | P3 |
| P5 | GUI 同步状态页 + 冲突解决页 + 来源层展示 | P3 |
| P6 | okserver 骨架：部署形态、任务队列、LLM 配置、项目仓克隆管理 | 无（可与 P1 并行） |
| P7 | wiki 线索管线：本地线索生成 → 提交 → 核实 → 再生 → 回同步 | P3 + P6 |

P1 单独交付即解决"个人多机同步"（最小可用、零审批、无需团队仓）；P3+P4 补齐团队人审；P6+P7 是服务端 AI 管线的增量交付。

## 12. 开放问题

1. okserver NAS 部署形态：Docker 镜像 vs 裸二进制 + 配置文件（NAS 型号 Docker 支持度参差）；
2. 本地如何检测"我的代码已合并到主分支"并触发线索生成：git log 轮询的时机与频率；
3. 线索包格式与提交协议细节（含批量线索、去重）；
4. 线索部分核实失败（多个 commit 中个别找不到）的粒度：整包拒收还是部分采纳；
5. auto-sync 默认频率与"本地有提交即 push"是否默认开启；
6. 草稿条目是否也出现在团队仓 main（轻量可见）还是只活在分支（严格隔离）——影响成员能否检索到未批准草稿；
7. Gitea 服务端 merge PR 时是否执行自定义 merge driver——若不执行，PR 级冲突退回文本级，可接受但需写明；
8. 双层检索的注入策略：个人层与团队层同域条目并存时的排序/去重/预算分配（个人优先还是分数优先）；
9. 个人分支 wiki 的目录组织约定（按分支名建子目录？分支删除后的清理策略）；
10. 晋升后个人层原条目的默认处置（保留标记 vs 删除）与重复晋升防护。

已关闭的原开放问题：INDEX.md 处置（§7 分层生成）；wiki 同步范围（§4 团队 wiki 整体同步、本地只读）；服务器形态（§8 独立组件 okserver，NAS 优先）；用户系统形态（§1 用户 = Gitea 账号，OK 零认证代码）。
