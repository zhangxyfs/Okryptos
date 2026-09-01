# 团队同步方案设计：git 传输 + 双层空间 + PR 人审 + 服务端 AI wiki 管线

- 日期：2026-08-25（同日修订：并入服务端 AI wiki 管线与内容二分；再修订：并入 OpenViking/TencentDB 两仓服务端实现借鉴；再修订：并入个人层双层空间与用户身份模型；2026-08-28 修订：**okserver 扩展为服务端管理面**——自建轻量认证 + 用户/组织/仓库管理，推翻"零认证代码"；仓粒度统一为每项目一仓；部署定为 NAS Docker 双容器；同日再修订：§6 冲突设计并入 **LLM 辅助选项**——merge driver 层仅本地 LLM、GUI 层 local/server 两档。翻转与细化见 P1 实施设计 `2026-08-25-personal-sync-p1-design.md` §2/§9/§11）
- 状态：待评审（未批准，未进入实施计划）
- 背景讨论：基于四份外部记忆系统调研（`docs/2026-08-23-memory-systems-synthesis.md` 及三份源文档）+ 四轮设计对话 + OpenViking/TencentDB 两仓服务端视角二次调研（队列语义、LLM 生成纪律、git 操作安全、重启恢复）
- 反向印证：两仓均无"commit 真实性核验"与"提案-评审工作流"，且都以"数据集中服务器"消解多主问题——§5A 的 commit 核实、§5B 的 PR 人审、§5C 的个人层是本设计的自有增量，无现成参照

## 1. 目标与定位

为 OpenKnowledge 增加**团队多人多设备**的知识库同步能力：

- **双层知识空间**：
  - **个人层**：每人每项目一个 Gitea 私有仓（`<user>/ok-<project>`），放自己的规则/踩坑/按分支组织的 wiki；多机自由同步、无审批；知识先私有验证；
  - **团队层**：共享库（`ok-<org>/<project>`），内容经审批生效；个人条目可晋升进团队层；
- **团队层内容按"代码可核实性"二分处理**：
  - wiki / reference（代码派生、可对着 git 记录核实）：成员本地不直接写，走**服务端 AI 管线**——本地提交线索，服务器 AI 核实 commit 后对着真实代码再生正式 wiki；
  - pitfall / rule / note（经验派生、无 git 依据可对照）：走 **git 分支 + PR 人审 diff**，管理者批准后生效；
- **用户身份 = okserver 账号**（2026-08-28 翻转原"用户 = Gitea 账号、OK 零认证代码"决策）：okserver 自建轻量认证与用户/组织/仓库管理，超级管理员 root 首启自动生成；Gitea 退居底层 git 托管实现，**个人层日常对终端用户透明**（例外：团队版 PR 审批场景管理者仍用 Gitea 网页端看 diff，§5B）；权限终点仍外包给 git 服务端（个人仓 ownership + 团队仓 org/分支保护），okserver 是它的 provisioning 与治理层，**不自建第二套权限判定**；
- 服务端能力封装为**独立可选组件 okserver**（NAS Docker 部署）：先交付**管理面**（认证、用户/组织/仓库 provisioning、审计），AI wiki 管线作为后期模块并入同一进程；单机版 OK 完全不受影响；
- 同步/服务端失败不影响本地任何功能（延续 fail-open），离线可正常工作。

明确不做：

- 不自建完整 ACL/权限判定体系（权限终点 = Gitea：个人仓 ownership 即权限；团队仓 = org/team + 分支保护）；okserver 只做轻量认证 + provisioning——原"零认证代码"决策 2026-08-28 被推翻，动机 = 团队版免迁移 + 集中管理面；
- 不做 okd 中心化审批服务端（TencentDB MemoryPanel 路线，否决：复杂度主体且丢 git 白送的历史/回滚）；
- 不做 NAS 裸共享目录（SQLite/state/config 走网络盘是锁腐败场景；NAS 的角色 = Docker 双容器：okserver + Gitea）；
- 踩坑类修改不走服务器 AI 评审——无 ground truth 可对照，LLM 评 LLM 的文本是意见不是验证，人审才是对的闸门。

## 2. 关键事实依据（方案为什么长这样）

- **条目是纯 markdown 文件，SQLite 是派生索引**：okd 契约（`docs/2026-08-21-okd-contract.md` §数据一致性模型）已确立"条目多写者 + mtime 增量 `db.Sync`"，文件是真相源 → git 直接可搬，clone 后索引自动重建。
- **草稿闸门已存在**：`draft: true` 不参与检索待人批准。团队审批是同一道闸门在团队维度的延伸。
- **AI 会高频更新已有条目**：update-vs-create 决策树落地后（汇总文档 §2.2），同域条目被反复改写是常态 → 冲突设计按"高频同条目并发修改"做。
- **wiki 技能已具备 git 历史增量扫描能力**：团队模式下扫描逻辑复用，仅把输出物从"本地写 wiki"换成"提交线索包"。
- **ACL 是复杂度主体**（TencentDB 调研 C2 的教训）：权限判定外包给 Gitea，不自建；okserver 管理面只做认证与 provisioning（建账号/组织/仓并映射到 Gitea ownership/org），个人层"只有本人能读写"恰好是 Gitea 私有仓 ownership 的原生语义。
- **wiki 由服务端单写后，wiki 类冲突在结构上消失**——成员本地 wiki 只读，唯一写者是 okserver AI 管线。
- **知识成熟度天然分层**：经验先在个人空间验证、再晋升共享，比"直接往团队库写"门槛低（呼应"沉淀门控太严比太松更伤"——个人层是零门槛兜底）。

## 3. 总体架构

```
个人层（无审批，多机自由同步）：
成员 A 机器 1 ── push/pull ──→ Gitea 私有仓 <userA>/ok-<project> ←── push/pull ── 成员 A 机器 2
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
│ okserver（独立组件，NAS Docker，自配 LLM）        │
│  管理面：认证 / 用户组织仓库 provisioning / 审计   │
│  AI 管线（后期模块）：项目仓克隆 → 核实 commit    │
│  → AI 对着代码再生 wiki → 提交团队仓 main → 回同步│
└──────────────────────────────────────────────┘

本地检索：个人层 + 团队层合并索引，条目标记来源层。
```

- **传输与历史**：git。NAS 上 Docker 部署 Gitea（git 托管实现，对终端用户透明）+ okserver（管理面 + 后期 AI 管线）双容器。
- **权限模型**：个人仓 = Gitea 私有仓 ownership（`<user>/ok-<project>`）；团队仓 = org/team + main 分支保护（`ok-<org>/<project>`）。普通成员只能 push 分支、提 PR；管理者 merge。团队 wiki 目录额外由 okserver AI 管线独占写入（成员本地只读）。okserver 管理面只做 provisioning 与认证，权限判定仍在 Gitea。
- **OK 侧新增**：`ok sync` / `ok clone`、merge driver、okd 定时同步、GUI 服务器页（向导登录/管理视图）与同步冲突页、双层检索合并。
- **okserver 新增**：管理面（轻量认证、root 首启生成、用户/组织/仓库管理、审计、GitBackend 接口——Gitea 首实现）；后期 AI 管线模块：线索接收 API、任务队列、项目仓克隆管理、wiki 再生管线、结果回写团队仓 main。

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

- 每人每项目一个 Gitea 私有仓 `<user>/ok-<project>`（首次建仓经 okserver 管理面 provisioning——它调 Gitea admin API 创建并代发 git token；ownership 即权限，只有本人能读写）；
- 本地写入**直接生效**（个人层无草稿闸门——自己的库自己负责，保持零门槛）；多机经单分支 push/pull + auto-sync 同步，冲突走 §6 同款 merge driver；
- **晋升**：个人条目值得共享时（`ok propose --to-team` 或 GUI 操作）→ 复制到团队仓个人分支 → 走 §5B PR 人审；批准后个人层原条目去留由人决定（默认保留并标记"已晋升"，避免重复晋升）；
- 个人分支 wiki 由本地 AI 生成（它是"我眼中的代码地图"，无需团队共识）；团队 wiki 仍由 okserver AI 管线统一生成，两者互不覆盖。

## 6. 冲突设计（三层吸收）

前提：knowledge/ 下同条目并发修改是常态（§2）。团队 wiki/ 因服务端单写已无并发冲突；个人层冲突只发生在本人多机之间。

**第一层：压缩冲突窗口。** okd 后台定时 auto-sync（分钟级 pull + 本地有提交即 push，个人层/团队层同策略）。

**第二层：领域感知 merge driver。** `.gitattributes` 给 `knowledge/**/*.md` 指定 OK 自带合并器（个人仓与团队仓同款）：

- front matter **字段级合并**（不按行）：tags 取并集；summary 取较新；draft 按规则裁决（已转正优先）；其余标量冲突取较新并记日志；
- 正文**追加语义合并**：union 合并两边都保留；偶发近重复行由已有"疑似重复/可合并"候选提示收尾；
- 只有同行互斥改写才升级为冲突。

本地已配置 LLM 时，用户可选择让 merge driver 改用 LLM 按两边内容语义合并（`[sync] llm_assist = local`）：**无人值守场景只允许本地 LLM**（个人内容不自动出设备群）；LLM 只合并正文，front matter 仍按上述字段级规则确定性裁决；调用纪律沿用既有知识条目（超时按场景区分 + temperature 缺省不传），失败/超时一律回退规则合并——LLM 是增强不是依赖。

**第三层：真冲突进 GUI。** `ok sync` 停下并在 GUI 冲突页列出条目：留我的（Accept Me）/ 留对方的（Accept Theirs）/ 三向合并编辑器手动合并 / **AI 合并**（LLM 可用时）。兜底原则**不丢内容**（被弃方存为副本草稿）。

AI 合并的 LLM 来源两档（`[sync] llm_assist`）：`local` = 成员本地配置的 LLM（默认，内容不出设备群）；`server` = okserver 服务端 LLM（AI 管线模块落地后开放）——选 server 即个人条目内容发送服务器处理，属成员显式选择，服务端记审计且管理员可全局禁用。无论哪档，LLM 结果只预填三向编辑器中间栏，**由人确认后才落盘**。wiki/reference 类条目不经此路——它们由 AI 管线对着代码重生成（§5A），服务端单写天然无冲突。

三种结构性冲突的具体策略：

| 场景 | 策略 |
|---|---|
| add/add 同名撞车 | 自动改名其一落为草稿（个人层：改名保留），两份都保留，交给"疑似重复"候选由人裁决 |
| both modified | merge driver 吸收；吸收不了的进 GUI 冲突页 |
| modify/delete | 保守保留修改版并标草稿，由人确认删除 |

PR 级冲突（两个 PR 改同一条目）：管理者在 Gitea 网页端解决，md 文件小，网页编辑器够用。

## 7. INDEX.md

团队 wiki 的 INDEX.md 由 okserver AI 管线随 wiki 生成并提交 main，成员本地不生成 → 原"高频冲突热点"在结构上消除。个人层 wiki 的 INDEX.md 由本地生成、随个人仓同步（个人多机冲突由 merge driver 兜底）。单机模式维持现状。

## 8. okserver 组件

独立可选组件（NAS Docker 部署，与 Gitea 双容器同 compose），单机版 OK 不依赖。进程内按模块划分：**管理面**（先行交付，P1）+ **AI wiki 管线**（后期模块，本节原有内容）。

**管理面模块**（2026-08-28 新增，细化见 P1 实施设计 §9）：

- **认证与用户体系**：SQLite 单文件库（users/sessions/orgs/org_members/repos/audit）；超级管理员 `root` 首启自动生成 + 32 位随机密码（0600 文件 + 日志一次）；角色 v1 = root/admin/member；登录限流；管理操作全量审计；
- **GitBackend 接口**：`CreateUser / CreateOrg / CreateRepo / AddOrgMember / CreateAccessToken` 等，Gitea admin API 为首实现，GitLab 等后续接入——治理逻辑与具体 git 托管解耦；
- **仓命名空间**：个人层 `<user>/ok-<project>`、团队层 `ok-<org>/<project>`，每项目一仓强制 private——团队版上线时个人层零迁移；
- **git 凭据分发**：建用户时经 Gitea admin API 代发 access token，本地写系统 credential helper；用户不持有 Gitea 密码、不登录 Gitea 网页。

**AI wiki 管线模块**（原设想，后期交付）：

- **部署形态**：Docker 镜像（2026-08-28 已决，原 §12 开放问题 1 关闭）；配置项：监听地址、LLM provider/key、git 仓存储目录、Gitea 凭据。LLM 无配置时**响亮失败、不静默 fallback**（TencentDB config.ts:19-29 的纪律）。
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
- **LLM 客户端**：复用 `internal/llmx`（超时按场景区分 + temperature 缺省不传的既有纪律适用）；管线配置的 LLM 同时作为成员冲突解决 server 档的后端（§6 第三层），受管理员开关与审计约束。
- **失败语义**：拒收必须回执原因；transient 失败重排，permanent 失败标记并回执——绝不静默丢线索。
- **职责边界（AI 管线模块）**：AI 管线只碰团队仓 wiki 与项目代码仓；个人层**内容**完全不经 AI 管线（个人知识不出本人设备群，除非主动晋升）。注：个人层仓库的 provisioning（建仓/token 代发）经管理面模块，但管理面不读取仓内容。

## 9. CLI / GUI 面

- `ok sync`：对当前启用层分别 pull --rebase →（merge driver）→ push（团队层 push 分支 + 可选建 PR；个人层直推单分支）；冲突时非零退出并写冲突状态文件供 GUI 展示；
- `ok clone <url>` / `ok remote`：初始化团队库、配置 remote 与团队模式（主分支定义）；clone 后 `db.Sync` 全量重建；
- 身份与服务器：GUI 服务器页三步向导（连接 → 登录 okserver → 按角色分派管理/成员视图）；个人仓经 okserver 管理面一键建仓并绑定；
- `ok propose --to-team`：个人条目晋升团队层（走 §5B）；
- okd：定时 auto-sync（频率可配，默认建议 5 分钟）+ 线索任务状态轮询；同步失败仅记日志不打断本地；
- GUI：服务器页（向导登录；root/admin 管理视图：用户/组织/仓库总览/审计；member 视图：项目绑定/我的组织）、管理页项目行同步按钮与状态点（分层显示）、冲突解决页（Accept Me / Accept Theirs / Merge / AI 合并 + 三向合并编辑器）、线索任务状态（排队/生成中/被拒原因）、团队 wiki 只读标识、条目来源层标记与晋升入口。沿用"勾选+保存按钮两段式"与"假功能按钮必须有可见反馈"交互约定。

## 10. 凭据与安全

- 成员认证 = okserver 账号（bcrypt + token 会话）；git 推拉凭据 = okserver 经 Gitea admin API 代发的 access token，写本机 git credential helper，OK 配置文件不存 git 凭据；成员不持有 Gitea 密码；
- okserver 持有 Gitea admin token（provisioning）、项目仓只读凭据与 Gitea 团队仓写凭据（提交 wiki），部署时配置；AI 管线模块不访问个人仓内容（§8 职责边界）；
- LLM API key 仅存在 okserver 本机配置；
- 知识条目可能含敏感信息：Gitea/裸仓须为私有仓库，个人仓默认 private，文档需明确此前提；
- okserver HTTP API 鉴权沿用本机服务安全三件套纪律的适用部分（token、Host 校验）；默认假设部署在内网 NAS，公网暴露须自行加反代 TLS。

## 11. 阶段划分（建议）

| 阶段 | 内容 | 依赖 |
|---|---|---|
| P1 | 个人层先行 + 服务端管理面：okserver（认证/root 首启/用户组织仓库管理/审计/GitBackend-Gitea）+ NAS Docker 双容器 + GUI 服务器页向导 + `ok sync`（单仓 push/pull）+ auto-sync + GUI 手动同步与冲突解决页（不丢内容） | 无 |
| P2 | merge driver（front matter 字段级 + 正文 union，个人/团队仓同款） | P1 |
| P3 | 团队层：团队仓 clone/remote + 双层检索合并（来源层标记）+ 晋升入口 | P1 |
| P4 | PR 人审流（团队层 push 分支 + 自动建 PR + 修订 diff 审批引导） | P3 |
| P5 | GUI 来源层展示与团队视图增强（同步状态/冲突页已在 P1 交付个人层版） | P3 |
| P6 | okserver AI 管线骨架：任务队列、LLM 配置、项目仓克隆管理（管理面已在 P1 交付） | P1 |
| P7 | wiki 线索管线：本地线索生成 → 提交 → 核实 → 再生 → 回同步 | P3 + P6 |

P1 单独交付即解决"个人多机同步"，并把团队版所需的命名空间/用户体系一次就位（升级零迁移）；P3+P4 补齐团队人审；P6+P7 是服务端 AI 管线的增量交付。

## 12. 开放问题

1. ~~okserver NAS 部署形态：Docker 镜像 vs 裸二进制~~（2026-08-28 已决：Docker 镜像，与 Gitea 双容器同 compose，见 P1 实施设计 §9.1）；
2. 本地如何检测"我的代码已合并到主分支"并触发线索生成：git log 轮询的时机与频率；
3. 线索包格式与提交协议细节（含批量线索、去重）；
4. 线索部分核实失败（多个 commit 中个别找不到）的粒度：整包拒收还是部分采纳；
5. auto-sync 默认频率与"本地有提交即 push"是否默认开启；
6. 草稿条目是否也出现在团队仓 main（轻量可见）还是只活在分支（严格隔离）——影响成员能否检索到未批准草稿；
7. Gitea 服务端 merge PR 时是否执行自定义 merge driver——若不执行，PR 级冲突退回文本级，可接受但需写明；
8. 双层检索的注入策略：个人层与团队层同域条目并存时的排序/去重/预算分配（个人优先还是分数优先）；
9. 个人分支 wiki 的目录组织约定（按分支名建子目录？分支删除后的清理策略）；
10. 晋升后个人层原条目的默认处置（保留标记 vs 删除）与重复晋升防护。

已关闭的原开放问题：INDEX.md 处置（§7 分层生成）；wiki 同步范围（§4 团队 wiki 整体同步、本地只读）；服务器形态（§8 独立组件 okserver，NAS Docker 双容器）；用户系统形态（§1 用户 = okserver 账号，管理面轻量认证，2026-08-28 翻转）；okserver 部署形态（Docker 镜像，2026-08-28）。
