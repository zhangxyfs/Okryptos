# P1 个人多端同步实施设计：git 单机引擎 + 团队版预留口

- 日期：2026-08-25（2026-08-28 修订：GUI 手动同步按钮 + 冲突解决页纳入本期；同日再修订：服务器侧 Gitea on NAS/Docker + 服务器管理 GUI 页纳入本期）
- 状态：待评审（未批准，未进入实施计划）
- 上游文档：`docs/superpowers/specs/2026-08-25-team-sync-design.md`（总体方案，本文档是其 §11 阶段表 P1 的细化）
- 范围声明：本文档只覆盖**个人多端数据同步**（个人层第一刀）；团队仓、PR 人审、merge driver、okserver（AI wiki 管线）均不在本期，但按 §11 预留扩展口。

## 1. 目标

- 同一用户的多台设备之间同步每个项目的知识库（`knowledge/` + wiki 相关文件）；
- 本地 git 历史 + 可选远端双层防丢防改；远端首选 **NAS 上 Docker 部署的 Gitea**（§7），也兼容任意 git 服务/裸仓；
- 无远端也可用（仅本地历史）；同步失败不影响本地任何功能（fail-open）；
- 代码与配置结构为团队版（`[sync.team]`、team 策略、双层检索、okserver）留好口子，本期不实现。

明确不做（本期）：merge driver（P2，智能合并策略）、PR 流程、okserver（AI wiki 管线，上游 §8/P6）、团队层目录。

## 2. 关键事实依据

- `store.Root` = 项目数据目录（`~/.openknowledge/projects/<name>/`），内含 `knowledge/`、`INDEX.md`、`kb.db`、`state/`、`config.toml`（`internal/store/store.go:9-17`）；
- 文件是真相源，kb.db 是派生索引，多写者一致性靠 mtime 增量 `db.Sync`（`docs/2026-08-21-okd-contract.md` §数据一致性模型）→ pull 引起的文件变更会被现有机制自动重建索引，本期**索引侧零新增代码**（但须测试验证闭环，见 §13）；
- okd 已有 ticker 模式（`internal/daemon/run.go:96` 自检查 ticker），同步定时器照此挂载；
- 项目级 `config.toml` 覆盖机制现成，同步配置直接放项目级；
- web GUI 是 vanilla JS 无构建链（`web/app.js` 单文件，`web/vendor/` 仅有图谱组件 g6/sigma/vis-network），**无现成编辑器/合并组件**——§9 的三向合并编辑器据此定实现路线；
- GUI 按钮纪律（既有知识条目）：假功能按钮必须有可见反馈；开关/配置类用"勾选+保存按钮"两段式，动作类按钮（同步、测试连接）即点即执行 + 明确反馈；
- 上游总体方案已决：**用户身份 = Gitea 账号，权限外包**（个人仓 ownership 即权限），OK 零认证代码——§7 服务器选型由此而来；
- okd HTTP 已有 X-Ok-Token 鉴权 + Host 校验（本机服务安全三件套），§8 的服务器配置 API 直接复用。

## 3. 仓库布局

`git init` 落在项目数据目录（`store.Root`）原地成仓，文件不挪动，OK 现有读写路径零改动。

`.gitignore`（随 `git init` 生成）：

```
kb.db
kb.db-*
state/
*.log
```

进仓内容：`knowledge/`、`INDEX.md`、wiki 相关文件。一个项目一个仓——对应团队版"一个项目一个团队仓"的映射。远端仓命名约定 `ok-<project>`（private 强制——知识条目可能含敏感信息）。

## 4. `internal/syncx`（新叶子包）

职责：在指定目录里跑 git。不依赖其他 internal 包（除 fsx 若需原子写状态）。

- **执行方式**：exec 参数数组调**系统 `git`**，不走 shell（防注入，借 TencentDB `git-fetcher.ts` 纪律）。启动探测 `git` 不存在 → 明确提示安装，同步禁用，本地功能不受影响。
  - 决策（默认，可翻）：**用系统 git 而非 go-git**——凭据 helper/SSH/rebase 语义现成；目标用户为开发者，git 基本必有。
- **原语**：`IsRepo / Init / Status(dirty, ahead, behind) / CommitAll(msg) / PullRebase / Push / RemoteURL`。
- **冲突解决原语**（供 GUI 冲突页使用，CLI 不消费）：
  - `ConflictFiles()` — 冲突文件列表（`git diff --name-only --diff-filter=U`）；
  - `ConflictVersions(file) → (base, local, remote)` — 经 index stage 取三版本（`git show :1: :2: :3:`，映射见 §9.2 的 rebase 语义坑）；
  - `ResolveFile(file, content)` — 写入解决结果 + `git add`；
  - `ContinueRebase() / AbortRebase()` — 全部解决后续推 / 一键回滚到同步前。
- **串行化**：包内 single-flight 锁——okd ticker、CLI、GUI 都可能触发同步，并发请求合并为一次执行；执行中再到的请求拿到"进行中"句柄并等待同一结果，不重复跑 git。
- **冲突检测**：`PullRebase` 返回结构化结果（成功 / 冲突文件列表 / 错误），不就地解决。

## 5. `internal/serverx`（新叶子包）

职责：与 git 托管服务器（本期 = Gitea）的 HTTP API 打交道。与 syncx 并列、互不依赖——syncx 保持纯 git。

- **API 面（刻意保持最小）**：
  - `TestConnection(url, token) → (user, version)` — `GET /api/v1/user`（+ `/api/v1/version`），供 GUI"测试连接"；
  - `RepoExists(url, token, repo)` — `GET /api/v1/repos/{owner}/{repo}`；
  - `CreatePrivateRepo(url, token, repo)` — `POST /api/v1/user/repos`（`private: true`）；
- 不依赖 Gitea 高级特性（org/hook/branch 保护均团队版再用）；换任意兼容 Gitea API 的服务（Gogs 等）也可用；
- token 只经服务器管理页（§8）录入，存全局配置（§10），**不进项目仓、不进日志**。

## 6. `ok sync` 命令

一次执行 = `add -A` → 有变更则 commit（消息 `sync: <hostname> <RFC3339>`，主机名标识来源设备，排障与未来团队归因两用）→ `pull --rebase` → `push`。

输出人话状态：`推送 2 个提交 / 拉取 1 个提交 / 已是最新`。

冲突时：

1. 停止后续 push；
2. 冲突文件列表写入 `state/sync-conflict.json`；
3. 非零退出，提示冲突文件列表，并指引两条路：Web GUI 冲突解决页（§9，主路径）或手动 git 解决。

**P1 兜底原则：不丢内容、不自动猜。** add/add 改名、union 合并等智能策略属 P2 merge driver。

## 7. 服务器侧：Gitea on NAS（Docker）

**选型理由**（上游总体方案 §1 已决）：权限外包给 Gitea（私有仓 ownership 即"只有本人能读写"），OK 零认证代码；建仓走 Gitea API（裸仓做不到自动创建）；团队版的 PR 网页审批也依赖它。裸 git+SSH 容器更轻但失去 API 与网页端，否决。

**部署形态（默认，可翻）**：Gitea 官方 Docker 镜像，NAS 上一个容器。参考 `docker-compose.yml`（随文档/安装包附带，非 OK 二进制职责）：

```yaml
services:
  gitea:
    image: gitea/gitea:latest
    restart: unless-stopped
    volumes:
      - ./data:/data          # 仓库存储 + 配置，NAS 上持久化
    ports:
      - "3000:3000"           # Web / HTTP API
      - "2222:22"             # SSH git 推拉
```

**一次性初始化**（用户在 Gitea 网页端完成，OK 不参与）：

1. 首访 `http://<nas>:3000` 走完安装向导（数据库选 SQLite 即可）；
2. 建管理员账号 → 建普通用户（或仅用管理员）；
3. 用户设置 → Applications → 生成 access token——**服务器管理页（§8）要填的就是 地址 + 这个 token**。

**OK 侧消费方式**：

- git 推拉走 SSH（`ssh://git@<nas>:2222/<user>/ok-<project>.git`）或 HTTP，凭据走用户既有 git 凭据体系（SSH key / credential helper），OK 不存 git 密码；
- token 仅用于 §5 的三个管理 API（测连接/查仓/建仓），与 git 推拉凭据是两件事；
- **与 okserver 的边界**：本期"服务器"仅指 git 托管（Gitea 容器）；okserver（AI wiki 管线，上游 P6/P7）是另一个容器、另一个组件，本期不做，但服务器管理页与配置按"多服务"留扩展（§11 口子 5）。

## 8. GUI 服务器管理页

OkManager 左侧导航新增"服务器"菜单（置于"设置"之前），一张页三卡片：

**卡片 1 · 连接配置**（配置类，遵循"填写+保存按钮"两段式）：

- 字段：服务器地址（`http://<nas>:3000`）、access token（密码框，保存后脱敏显示）、SSH 端口（默认 2222，用于拼 remote URL）；
- `测试连接` 按钮（动作类，即点即执行）：转圈 → 成功显示"已连接：<用户名> · Gitea <版本>"，失败显示原因（网络不通/401 token 无效等）——假功能按钮教训，必须有真实反馈。

**卡片 2 · 项目仓库管理**：

- 项目列表，每行：项目名、远端仓状态（`未创建 / 已创建未绑定 / 已绑定`）、操作按钮；
- `一键建仓并绑定`（动作类）：serverx 建私有仓 → 本地走 §12 init 流程（git init + 首 commit + remote add + 首 push）→ 行内状态即时刷新为"已绑定"；已绑定的行显示 remote URL 与最后同步时间（取自 `sync-status.json`）。

**卡片 3 · 部署指引**：

- 未配置服务器时常显：§7 的 docker-compose 片段 + 初始化三步 + 排障提示（端口、token 权限）；
- 已配置后默认折叠，可展开复查。

**后端 API**（okd HTTP，复用 X-Ok-Token）：

| 端点 | 语义 |
|---|---|
| `GET /api/server/config` / `PUT /api/server/config` | 读/写服务器配置；读取时 token 脱敏返回 |
| `POST /api/server/test` | 测连接 → `{ok, user, version, error}` |
| `GET /api/server/repos` | 每项目远端仓状态聚合 |
| `POST /api/server/repos` `{project}` | 建仓 + 绑定（内部 serverx 建仓 → syncx init + 首 push） |

## 9. GUI 手动同步与冲突解决（本期新增）

自动同步（ticker + 写入防抖）不是实时触发，用户必然有手动同步诉求；且冲突必须有人介入——这两件事都落在管理页。

### 9.1 管理页项目行同步按钮

- **位置**：管理页项目列表每行（项目名右侧），常显小图标按钮；
- **点击行为**：
  1. 项目未初始化同步（非 git 仓）→ 弹初始化确认卡：可填 remote URL（留空 = 仅本地历史；若 §8 已配服务器，提供"使用服务器建仓"选项跳卡片 2 流程），确认后走 §12 init 流程，成功后再接着同步；
  2. 已初始化 → 直接触发一次 §6 同逻辑同步（走后端 single-flight，与 ticker/CLI 触发的同步去重合并）；
- **反馈纪律**（吸取"假功能按钮必须有可见反馈"教训）：点击即转圈并禁用（防连点），结束 toast 人话结果——`推送 n 个提交 / 拉取 n 个提交 / 已是最新 / 失败原因`；
- **行内状态点**：项目行同步图标旁显示状态点（绿 = 已同步，黄 = 有未推变更，灰 = 未启用，红 = 冲突），数据来自 `sync-status.json`，同步操作后即时刷新，平时随管理页轮询刷新；
- **冲突时**：toast 提示 + 状态点变红，点击状态点/按钮进入冲突解决页（§9.2）。

### 9.2 冲突解决页

- **入口与路由**：管理页项目行红状态点 / 同步结果 toast 链接；路由 `#/sync-conflict?project=<name>`；
- **布局**：冲突文件列表，每行 = 文件名 + 三个按钮：
  - **Accept Me** — 保留我（本设备）的改动；
  - **Accept Theirs** — 采用远端版本；
  - **Merge** — 打开三向合并编辑器（§9.3），逐块手工合并；
- **底部操作**：
  - `完成同步` — 仅当列表全部文件标记已解决才可点 → `rebase --continue` + `push`，成功后清 `sync-conflict.json`、跳回管理页；
  - `放弃本次同步` — `rebase --abort` 回滚到同步前状态（本地内容原样保留，不丢）；
- **rebase 语义坑（实现纪律，必须遵守）**：`pull --rebase` 冲突期间 git 的 ours/theirs 是**反转**的——index stage `:2:`（`--ours`）是刚拉下来的**远端**版本，`:3:`（`--theirs`）是正在 replay 的**本地**提交。UI 的 Accept Me / Accept Theirs 必须按用户语义映射（Me → `:3:`，Theirs → `:2:`），严禁直译 git ours/theirs。

### 9.3 三向合并编辑器（仿 Android Studio Merge Revisions）

交互对标 Android Studio / IntelliJ 的 Merge 窗口：

- **三栏**：左 = 本地版本（只读）、中 = 合并结果（可编辑，预填 git 自动合并结果）、右 = 远端版本（只读）；
- 左/右与中间做行级 diff，冲突块高亮；每个冲突块提供 `«` / `»` 箭头采纳单侧内容到中间；工具栏有"采纳全部非冲突块"；
- 底部：`应用`（写回中间内容 + `git add`，回列表标记已解决）/ `取消`；
- **实现路线（默认，可翻）**：自研轻量三栏——`<pre>` + 行号渲染，行级 LCS diff 约百行 JS，不引入 CodeMirror/Monaco 级依赖。理由：GUI 无构建链、知识条目是 Markdown 不需要语法高亮、vendor 现无编辑器组件；若日后需要语法高亮再 vendor CodeMirror 6（§15 待决）。

### 9.4 后端 API（okd HTTP，复用 X-Ok-Token 鉴权）

| 端点 | 语义 |
|---|---|
| `POST /api/project/sync` `{project}` | 触发同步（未 init 且带 `remote` 参数则先 init）；返回 `{status: ok/conflict/error, pushed, pulled, conflicts[], message}` |
| `GET /api/project/sync/status?project=` | 该项目 personal 层状态 + 冲突文件列表（供行内状态点与冲突页） |
| `GET /api/project/sync/conflict-file?project=&file=` | 返回 `{base, local, remote}` 三版本全文（Merge 编辑器数据源） |
| `POST /api/project/sync/resolve` `{project, file, action: me/theirs/merged, content?}` | `me`/`theirs` 取对应 stage 版本落盘，`merged` 用提交的 content；均 `git add` |
| `POST /api/project/sync/finish` `{project}` | 校验全部解决 → `rebase --continue` + `push` |
| `POST /api/project/sync/abort` `{project}` | `rebase --abort` |

- 所有端点走 syncx single-flight；同步执行中重复 `POST sync` 等待同一结果而非并发跑；
- GUI 只经 API 读冲突状态，`state/sync-conflict.json` 由 syncx 独占写。

## 10. 配置面

项目级 `config.toml`（复用现有覆盖机制）：

```toml
[sync]
enabled = true
remote = "ssh://git@nas:2222/you/ok-myproject.git"   # 留空 = 仅本地 commit，不推远端
auto_interval_min = 5
```

全局配置（服务器连接，一台机器一份，不随项目仓同步）：

```toml
[server]
url = "http://nas:3000"
token = "***"          # Gitea access token，文件权限 0600；不入日志、脱敏回显
ssh_port = 2222
```

- `remote` 留空也可用：本地历史是防丢/防改第一层，远端是第二层 → **P1 不依赖服务器即可交付**；
- 项目级解析实现为团队版按层预留（§11 口子 1）：内部结构即按 `personal` 层建模，序列化为 `[sync]` 是 P1 的简写形态；
- 全局 `[server]` 按多服务建模（§11 口子 5）：内部挂 `server.git.*`，为将来 `server.okserver.*` 留位。

## 11. 团队版扩展口（本期必须留，但不实现）

1. **配置按层**：P1 的 `[sync]` 在团队版演进为 `[sync.personal]` + `[sync.team]`；解析代码现在就把字段挂在层结构下，不摊平到顶层；
2. **策略参数化**：`syncx.Sync(dir, policy)`；P1 仅 `personal` 策略（直推 main）；团队版加 `team` 策略（推分支 + 建 PR），pull/rebase/冲突处理共用；
3. **状态/GUI 按层**：`sync-status.json` 的 `layers` 结构（§6）；GUI 行内状态点与冲突解决页（§9）按层渲染，本期只渲染 personal 层；
4. **不新增"单知识源"假设**：团队层将来是项目下独立第二目录/仓（总体方案 §4）；本期不改检索合并路径，但新代码不得假设项目只有一个知识源，此类判断收敛到 store 层；
5. **服务器按多服务建模**：全局配置的 `[server]`、服务器管理页卡片、serverx 客户端均为"git 托管服务"落地，结构上将 `server.git` 与将来的 `server.okserver`（上游 P6）并列；服务器页预留第二张服务卡片的位置。

## 12. 设备初始化流（`ok sync init [remote-url]`，GUI 确认卡/建仓绑定同走此逻辑）

| 情形 | 行为 |
|---|---|
| 目录非仓、无知识内容 | clone remote 到该目录 |
| 目录非仓、已有内容 | `git init` + 首个 commit + 关联 remote + push（首台设备路径） |
| 两边各自初始化过、均有内容 | **不自动合并**，报错并指引手动 `--allow-unrelated-histories` 合并一次（低频一次性场景） |

## 13. 测试策略

- 不依赖 Gitea：测试内 `git init --bare` 临时裸仓，`file://` 协议当 remote；
- 核心 E2E：两个临时 `OK_HOME` 模拟双设备——"设备 A 写条目 → sync → 设备 B sync → B 能检索到该条目"闭环（专门验证 §2 的 mtime 重建链路，防"单元测试绿、链路断"的已知坑模式）；
- 手动跑 E2E 必须导出全套 agent home 隔离变量（既有知识条目教训）；
- 冲突路径用例：双设备改同一文件 → 第二台 sync 报冲突、写 conflict 状态、内容不丢；
- GUI 冲突解决链路：走**真 HTTP API**（不直调核心函数——吸取"hook 集成测试拦不住派发层断链"教训）——双设备改同一文件 → `POST sync` 报 conflict → `GET conflict-file` 三版本齐全且 Me/Theirs 映射正确（§9.2 语义坑专项断言）→ `resolve merged` → `finish` → 双端内容一致；`abort` 路径回滚验证；
- serverx：`httptest` mock Gitea API 三个端点（含 401/404/网络错误分类）；建仓绑定全流程对真 Gitea 的验收为**手动验收项**（测试环境不起 Docker 容器）。

## 14. 工作量与交付边界

新增：`internal/syncx` 包（含冲突解决原语）、`internal/serverx` 包（Gitea API 客户端）、`ok sync` / `ok sync init` 命令、okd 同步 ticker + 写入防抖触发、状态/冲突两个状态文件、项目级/全局配置段、§8 四个服务器 API 端点 + 服务器管理页（三卡片）、§9.4 六个同步 API 端点、管理页项目行同步按钮与状态点、冲突解决页（文件列表 + 三向合并编辑器）、docker-compose 参考文件、E2E。无新第三方依赖（系统 git 除外；三向编辑器自研，不引前端依赖）。

## 15. 已决与待决

已决（默认值，评审时可翻）：

- 用系统 git，不用 go-git（§4）；
- 服务器 = NAS 上 Docker 部署的 Gitea 官方镜像（§7；可翻项：裸 git+SSH 容器——但团队版网页审批/API 建仓依赖 Gitea，翻需连带改上游方案）；
- 远端仓自动创建纳入本期，经 Gitea API，仓命名 `ok-<project>` 强制 private（§3、§5）；
- serverx 独立叶子包，API 面刻意最小（§5）；
- 服务器管理 GUI 页纳入本期，配置类两段式、动作类即点即执行（§8）；
- token 存全局配置（0600），不进项目仓/日志，脱敏回显（§10）；
- 冲突不自动猜合并，智能合并归 P2 merge driver（§6）；
- GUI 手动同步按钮 + 冲突解决页纳入本期（§9，替代原"P1 无 GUI 解决页"）；
- UI 的 Me/Theirs 按用户语义映射 rebase 反转的 stage（§9.2 纪律）；
- 三向合并编辑器自研轻量三栏，不引前端依赖（§9.3）。

待决：

- `auto_interval_min` 默认值（建议 5）与写入防抖时长（建议 30s）；
- commit 消息格式是否需含项目名（多项目并行排障场景）；
- 管理页状态点刷新：随现有轮询即可，还是要操作后 SSE/短轮询即时化；
- GUI 初始化确认卡的 remote 输入是否记忆最近使用值；
- 仓命名撞车（`ok-<project>` 已存在且非本机创建）的处置：报错 vs 提示换名；
- docker-compose 参考文件的落点：docs/ 附带 vs 安装包 assets。
