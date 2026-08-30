# P1 个人多端同步实施设计：git 同步引擎 + okserver 管理面 + 团队版预留口

- 日期：2026-08-25（2026-08-28 修订：GUI 手动同步 + 冲突解决页纳入；服务器侧定为 NAS/Docker 双容器并加服务器管理页；同日再修订：**自建服务端管理面 okserver**——用户/组织/仓库管理、root 首启生成、GUI 向导登录，推翻上游"零认证代码"决策；同日再修订：冲突解决并入 **LLM 辅助选项**，本地/服务端来源分档；补 §9.5 部署流程与运维、§9.6 仓库目录约定）
- 状态：P1 全部实施完成（2026-08-29，A 引擎 + B GUI 同步面 + C 服务端与本地接入）；P2 merge driver 起为后续阶段
- 上游文档：`docs/superpowers/specs/2026-08-25-team-sync-design.md`（总体方案，本文档是其 §11 阶段表 P1 的细化；本文档对上游的三处决策翻转见 §2 与 §17）
- 范围声明：本文档覆盖**个人多端数据同步 + 服务端管理面首版**；团队仓工作流、PR 人审、merge driver、AI wiki 管线均不在本期，但用户/组织/命名空间模型从第一天按团队版终态建模，免未来迁移。

## 1. 目标

- 同一用户的多台设备之间同步每个项目的知识库（`knowledge/` + wiki 相关文件）；
- 本地 git 历史 + 远端双层防丢防改；远端 = NAS 上 Docker 部署的 **okserver + Gitea** 双容器（§9）；
- **okserver 管理面首版**：用户/组织/仓库集中管理，超级管理员 root 首启自动生成；个人层与团队层的仓命名空间从第一天就位，未来升级团队版**个人层数据零迁移**；
- 无服务器也可用（仅本地历史）；同步/服务器失败不影响本地任何功能（fail-open）；
- 代码与配置结构为团队版（team 策略、双层检索、AI wiki 管线）留好口子，本期不实现。

明确不做（本期）：merge driver（P2，智能合并策略——含其 LLM 档位）、PR 人审流程、AI wiki 管线（okserver 后期模块）、团队层写入工作流（团队仓本期只在 okserver 里可建，同步策略后续阶段接入）。

## 2. 关键事实依据

- `store.Root` = 项目数据目录（`~/.openknowledge/projects/<name>/`），内含 `knowledge/`、`INDEX.md`、`kb.db`、`state/`、`config.toml`（`internal/store/store.go:9-17`）；
- 文件是真相源，kb.db 是派生索引，多写者一致性靠 mtime 增量 `db.Sync`（`docs/2026-08-21-okd-contract.md` §数据一致性模型）→ pull 引起的文件变更会被现有机制自动重建索引，本期**索引侧零新增代码**（但须测试验证闭环，见 §15）；
- okd 已有 ticker 模式（`internal/daemon/run.go:96` 自检查 ticker），同步定时器照此挂载；
- 项目级 `config.toml` 覆盖机制现成，同步配置直接放项目级；
- web GUI 是 vanilla JS 无构建链（`web/app.js` 单文件，`web/vendor/` 仅有图谱组件），**无现成编辑器/合并组件**——§11 的三向合并编辑器据此定实现路线；
- 本地 LLM 能力现成：`internal/llmx`（条目 ✨优化等功能在用），"超时按场景区分 + temperature 缺省不传"是既有纪律——§11.2 的 LLM 冲突辅助复用同一客户端与纪律；
- GUI 按钮纪律（既有知识条目）：假功能按钮必须有可见反馈；开关/配置类用"勾选+保存按钮"两段式，动作类按钮（同步、测试连接、登录）即点即执行 + 明确反馈；
- okd HTTP 已有 X-Ok-Token 鉴权 + Host 校验（本机服务安全三件套），本地新增 API 直接复用；
- **对上游的三处决策翻转**（用户指示，2026-08-28，动机：团队版免迁移 + 集中管理面）：
  1. 上游"用户 = Gitea 账号，OK 零认证代码"→ 本期**自建 okserver 轻量认证与用户/组织管理**（Gitea 退居底层 git 托管实现，用户对 Gitea 无感知）；
  2. 上游"okserver 只做 AI wiki 管线、不碰个人仓"→ **okserver 定义扩展为服务端管理面**（本期），AI 管线作为后期模块并入同一进程；
  3. 上游 §5C"每人一个私有仓"→ **每项目一仓，两层同构**（§4 命名空间），避免单仓历史耦合/冲突面放大/与团队仓结构不同构。

## 3. 总体架构

```
成员设备（多台）                          NAS（Docker）
┌────────────────────┐   管理面 HTTPS API   ┌──────────────────────────┐
│ ok / okd / OkManager│ ───────────────────→ │ okserver 容器             │
│  · syncx（git 操作） │  登录/建仓/用户组织管理 │  · 用户/组织/仓库/审计 (SQLite)│
│  · serverx（客户端） │                      │  · GitBackend 接口        │
└───────┬────────────┘                      └───────────┬──────────────┘
        │ git push/pull (HTTPS+token)                    │ admin API
        ↓                                                ↓
                          ┌──────────────────────────┐
                          │ Gitea 容器（git 托管实现）  │
                          │  <user>/ok-<project>      │
                          │  ok-<org>/<project>       │
                          └──────────────────────────┘
```

- 本地只跟 okserver 的管理 API 打交道；Gitea 对普通用户不可见（不直接登录 Gitea 网页）；
- git 推拉仍直连 Gitea（传输层），凭据 = okserver 代发的 Gitea access token（§9.4）；
- 权限终点仍在 Gitea（仓 ownership/org 成员），okserver 是它的 provisioning 与治理层——不自建第二套权限判定。

## 4. 仓库布局与命名空间

`git init` 落在项目数据目录（`store.Root`）原地成仓，文件不挪动，OK 现有读写路径零改动。

`.gitignore`（随 `git init` 生成）：

```
kb.db
kb.db-*
state/
*.log
```

进仓内容：`knowledge/`、`INDEX.md`、wiki 相关文件、`config.toml`（2026-08-28 终审定案：项目配置随仓分发，全设备一致；设备特定配置属全局配置、不在仓内）。**一个项目一个仓**，命名空间两层同构、从第一天就位：

| 层 | 远端仓 | 权限语义 | 本期状态 |
|---|---|---|---|
| 个人层 | `<user>/ok-<project>` | Gitea 用户 ownership，私有仓仅本人 | 本期实现同步 |
| 团队层 | `ok-<org>/<project>` | Gitea org 成员 | 本期仅 okserver 可建仓，同步策略后期接入 |

- 个人层用 Gitea 用户原生命名空间（不另建 personal org），org 加 `ok-` 前缀避免与既有组织撞名；
- 全部仓强制 private——知识条目可能含敏感信息；
- **迁移叙事**（本设计的核心理由）：团队版上线 = root 在 okserver 建组织 + 建仓 + 成员加组织，个人层仓与本地数据原样不动。

## 5. `internal/syncx`（新叶子包）

职责：在指定目录里跑 git。不依赖其他 internal 包（除 fsx 若需原子写状态）。

- **执行方式**：exec 参数数组调**系统 `git`**，不走 shell（防注入，借 TencentDB `git-fetcher.ts` 纪律）。启动探测 `git` 不存在 → 明确提示安装，同步禁用，本地功能不受影响。
  - 决策（默认，可翻）：**用系统 git 而非 go-git**——凭据 helper/rebase 语义现成；目标用户为开发者，git 基本必有。
- **原语**：`IsRepo / Init / Status(dirty, ahead, behind) / CommitAll(msg) / PullRebase / Push / RemoteURL`。
- **冲突解决原语**（供 GUI 冲突页使用，CLI 不消费）：
  - `ConflictFiles()` — 冲突文件列表（`git diff --name-only --diff-filter=U`）；
  - `ConflictVersions(file) → (base, local, remote)` — 经 index stage 取三版本（`git show :1: :2: :3:`，映射见 §11.2 的 rebase 语义坑）；
  - `ResolveFile(file, content)` — 写入解决结果 + `git add`；
  - `ContinueRebase() / AbortRebase()` — 全部解决后续推 / 一键回滚到同步前。
- **串行化**：包内 single-flight 锁——okd ticker、CLI、GUI 都可能触发同步，并发请求合并为一次执行；执行中再到的请求等待同一结果，不重复跑 git。
- **冲突检测**：`PullRebase` 返回结构化结果（成功 / 冲突文件列表 / 错误），不就地解决。

## 6. `internal/serverx`（新叶子包，本地侧）

职责：本地（ok/okd/GUI）与 okserver 管理 API 打交道的客户端。与 syncx 并列、互不依赖——syncx 纯 git，serverx 纯 HTTP。

- **API 面（对应 §9.3 服务端端点的本地消费侧）**：`Ping / Login / Me / ListMyRepos / ProvisionPersonalRepo(project)`；
- 管理类端点（用户/组织管理）也由它承载，供 GUI 服务器页使用；
- token 存全局配置（§12），401 → GUI 引导重新登录；**token 不进项目仓、不进日志**。

## 7. `ok sync` 命令

一次执行 = `add -A` → 有变更则 commit（消息 `sync: <hostname> <RFC3339>`，主机名标识来源设备，排障与未来团队归因两用）→ `pull --rebase` → `push`。

输出人话状态：`推送 2 个提交 / 拉取 1 个提交 / 已是最新`。

冲突时：

1. 停止后续 push；
2. 冲突文件列表写入 `state/sync-conflict.json`；
3. 非零退出，提示冲突文件列表，并指引两条路：Web GUI 冲突解决页（§11，主路径）或手动 git 解决。

**P1 兜底原则：不丢内容、不自动猜。** add/add 改名、union 合并等智能策略属 P2 merge driver；LLM 辅助（§11.2）只在 GUI 冲突页有人确认的场景开放，不改变这条兜底。

## 8. okd auto-sync

- 照 `run.go:96` ticker 模式加同步 ticker：默认 5 分钟（`auto_interval_min` 可配，**0 = 关闭全部自动触发**——ticker 与写入防抖都关，手动 `ok sync` 不受影响），到点对每个已启用同步的项目跑 §7 同逻辑；
- **写入后尽快推**：条目写入动作（approve、GUI 编辑等）触发一次 30s 防抖的延迟 sync，把未同步窗口压到分钟级（未同步窗口是本方案唯一的残余丢失面）；
- 失败仅记日志 + 更新状态文件，绝不影响 hook 注入等本地链路。

状态文件 `state/sync-status.json`，**从第一天就按层建模**（§13 口子 3）：

```json
{"layers": {"personal": {"last_sync": "...", "ahead": 0, "behind": 0, "conflict": false, "last_error": ""}}}
```

## 9. 服务器侧：okserver + Gitea on NAS

### 9.1 部署形态

双容器一个 compose（落点 `server/nas/docker-compose.yml`，目录约定见 §9.6；okserver 镜像由本项目构建发布，见 §9.5）：

```yaml
services:
  gitea:
    image: gitea/gitea:latest
    restart: unless-stopped
    volumes: [./gitea-data:/data]
    ports:
      - "3000:3000"     # git over HTTP(S)（Web 界面对普通用户不开放使用）
      - "2222:22"       # SSH（本期可选，见 §17 待决）
  okserver:
    image: openknowledge/okserver:latest
    restart: unless-stopped
    volumes: [./okserver-data:/data]
    environment:
      OKSERVER_GITEA_URL: http://gitea:3000
      OKSERVER_GITEA_ADMIN_TOKEN: <部署时从 Gitea 生成一次>
    ports:
      - "3100:3100"     # 管理面 API（本地 GUI 连的是它）
```

### 9.2 okserver 本体（`cmd/okserver` + `internal/oksrv`，首版刻意简单）

- **存储**：SQLite 单文件 `/data/okserver.db`（与 OK 单文件哲学一致；备份 = 拷文件）。表：`users / sessions / orgs / org_members / repos / audit`——所有管理信息落库；
- **首启初始化**：库中无 root 时自动生成超级管理员 `root` + **32 位随机密码**（bcrypt 存库），明文写入 `/data/INITIAL_ROOT_PASSWORD`（0600）并打印一次到容器日志；部署人取走后建议删除该文件；
- **角色模型（v1 从简）**：`root`（唯一、不可禁用）/ `admin`（同 root，除不能动 root）/ `member`（只能管理自己的个人仓、查看所属组织）。org 内角色下放（org owner 管成员）留待 v1.1（§17 待决）；
- **GitBackend 接口**：`CreateUser / CreateOrg / CreateRepo / AddOrgMember / CreateAccessToken / DeleteRepo`，Gitea 为首实现（admin API）；GitLab 等后续可加——用户/组织/仓的治理逻辑与具体 git 托管解耦；
- **认证**：`POST /login` 颁 token（bcrypt 校验，会话表；会话时长可配，默认建议 30 天——§17 待决）；管理 API 走 Bearer token；登录接口限流防爆破；
- **审计**：管理操作（建用户/建组织/建仓/成员变更/禁用）写 `audit` 表（谁、何时、做了什么），root/admin 可查；
- **安全边界**：默认假设部署在内网 NAS；公网暴露须自行加反代 TLS（部署指引写明）。token 不落日志，密码只存 bcrypt；
- **无 LLM**：管理面不含 LLM 配置——服务端 LLM 属 AI 管线模块（上游 P6），届时其配置同时作为冲突解决 server 档的后端（§11.2）。

### 9.3 okserver API 面（v1）

| 端点 | 鉴权 | 语义 |
|---|---|---|
| `GET /api/v1/meta` | 公开 | `{version, initialized, git_backend: {type, ok}}`——GUI 向导第一步探测用 |
| `POST /api/v1/login` | 公开（限流） | `{username, password}` → `{token, user: {name, role}}` |
| `GET /api/v1/me` | 登录 | 我的身份、所属组织、可见仓库列表 |
| `POST /api/v1/repos/personal` `{project}` | 登录 | 建/取我的个人仓 `<user>/ok-<project>`（幂等）；**git token 仅首次建仓时下发**，重复调用只返回仓信息（防 token 堆积），token 丢失本期需重建仓，v1.1 走重置联动重发 |
| `GET /api/v1/repos` | root/admin | 全量仓列表（层/owner/项目/创建人/创建时间）——§10.2 仓库总览卡数据源 |
| `GET /api/v1/users` / `POST /api/v1/users` `{username}` | root/admin | 用户列表 / 建用户（生成初始密码 + Gitea 账号与 git token，**一次性返回明文**） |
| `POST /api/v1/users/{name}/reset-password` | root/admin | 重置密码，新密码一次性返回 |
| `POST /api/v1/users/{name}/disable` `/enable` | root/admin | 禁用/启用（联动 Gitea 账号） |
| `GET /api/v1/orgs` / `POST /api/v1/orgs` `{name, desc}` | root/admin | 组织列表 / 建组织（Gitea 侧建 `ok-<name>`） |
| `POST /api/v1/orgs/{org}/members` `{user, role}` / `DELETE` | root/admin | 成员增减（联动 Gitea org team） |
| `POST /api/v1/repos/team` `{org, project}` | root/admin | 建团队仓 `ok-<org>/<project>`（本期仅建仓，同步策略后期） |
| `GET /api/v1/audit` | root/admin | 审计日志（简单分页列表） |

### 9.4 git 凭据分发

- v1：okserver 建用户时经 Gitea admin API 为其生成 access token，`repos/personal` 与建用户响应中下发；本地 OK 把它写入**系统 git credential helper**（不写 OK 配置文件），git over HTTPS 推拉即用；
- 用户不持有 Gitea 密码、不登录 Gitea 网页——Gitea 对终端用户完全透明；
- token 泄露处置：root/admin 重置用户密码时联动吊销重发（v1.1，本期手动）。

### 9.5 部署流程与运维

**前置形态矩阵**（部署文档给双路径）：

- **首选 Docker**：群晖 Container Manager / 威联通 Container Station / 绿联极空间 / 任意 Linux 主机；
- **备选裸二进制**：无 Docker 的老 NAS 或极简环境——okserver 与 Gitea 都是单二进制 + 数据目录（systemd unit 或直接进程守护），部署文档给出对等步骤。功能无差异，Docker 只是省心。

**图形化一键部署**：除下述手工路径外，另有独立程序 okdeploy（`cmd/okdeploy` + `internal/deployx`，设计见 `docs/superpowers/specs/2026-08-28-okdeploy-design.md`）——双击启动、浏览器向导、SSH 完成部署/升级/备份/恢复/卸载，使用文档 `server/deploy/README.md`。它是独立 release artifact（`dist/deploy/`），**不进客户端安装包**。

**逐步部署流程**（GUI 部署指引卡与 `server/nas/README.md` 同内容）：

1. 建持久化目录 `./gitea-data`、`./okserver-data`；
2. `docker compose up -d gitea` → 访问 `http://<nas>:3000` 走完安装向导：数据库选 SQLite、建管理员账号、**关闭开放注册**；
3. 管理员 → 设置 → Applications → 生成 admin token，填入 compose 的 `OKSERVER_GITEA_ADMIN_TOKEN`；
4. `docker compose up -d` 起 okserver → `docker exec <容器> cat /data/INITIAL_ROOT_PASSWORD` 取 root 初始密码（或看容器日志），取走后删除该文件；
5. 成员设备 OkManager → 服务器页三步向导（§10.1）连 `http://<nas>:3100` → root 登录建用户/组织 → 成员各自登录、建仓绑定、开始同步。

**Gitea 侧治理配置**（部署文档写明，缺一不可）：

- 关闭开放注册（`[service] DISABLE_REGISTRATION = true`）——okserver 是唯一建用户入口；
- 禁止普通用户自行建仓（`[repository] MAX_CREATION_LIMIT = 0`）——仓一律经 okserver provisioning，命名空间治理才不被绕开；
- 新仓默认 private；
- `ROOT_URL` 指向 NAS 实际地址（如 `http://<nas>:3000/`）——否则 Gitea 生成的 clone URL 是容器内地址，成员拿到的 remote 是错的。

**镜像与架构**：okserver 镜像多架构（`linux/amd64` + `linux/arm64`——ARM NAS 是常态），随 OK release 流水线双发 **GHCR + Docker Hub**，版本号与 ok/okd 对齐（sync-version 纪律，bump 时同步）。

**升级**：`docker compose pull && docker compose up -d`；okserver 启动时自动跑 SQLite schema migrate（只增不毁）；Gitea 按其官方升级路径。升级前先备份数据卷。

**备份**（知识历史的最后一道防线，部署指引必须含；脚本示例落 `server/nas/backup/`）：

- `okserver-data`：SQLite 单文件，停机拷贝或用 SQLite `.backup`；
- `gitea-data`：全部仓库存储——NAS 快照 / 定时 rsync / `gitea dump`；
- 记住每台成员设备本身就是一份完整 git 副本——双层防丢的题中之义，服务器重建后任一设备 push 即可恢复历史。

**网络与 TLS**：默认内网 HTTP 即可；公网暴露必须加反代 TLS（Caddy / Nginx Proxy Manager），`3100`（管理面）与 `3000`（git over HTTPS）都要罩住，`2222`（SSH）按需开放。

**资源底线**：okserver 常驻约 20MB 内存；Gitea 约 200–500MB；1C1G 入门 NAS 可跑。

### 9.6 仓库目录约定

服务器相关交付物在本仓库内的落点（用户指示 2026-08-28：服务器的东西集中在 `server/` 下可见；Go 代码守单一 go.mod 布局）：

```
cmd/okserver/          # 二进制入口（与 cmd/ok、cmd/okd、cmd/okmanager 同构）
internal/oksrv/        # 服务端实现（DB schema/认证/GitBackend/API/审计/migrate）
server/                # 服务器侧非 Go 交付物总入口
└── nas/               # NAS 部署包
    ├── docker-compose.yml
    ├── .env.example        # OKSERVER_GITEA_ADMIN_TOKEN 等占位
    ├── Dockerfile          # okserver 镜像（多架构构建上下文）
    ├── README.md           # 部署/运维文档（§9.5 全文落地）
    ├── systemd/            # 裸二进制备选路径的 unit 示例
    └── backup/             # 两数据卷备份脚本示例
```

- **Go 代码必须在单一 go.mod 的 cmd/internal 布局内**：okserver 要复用 `internal/` 既有件（fsx 原子写、llmx 等），拆独立 module 会失去 `internal/` 可见性，否决；
- `server/<目标>/` 按部署目标分目录，nas 是第一个；未来其它目标（如云主机一键包）并列新增；
- `server/nas/README.md` 是部署文档的单一事实源，GUI 部署指引卡从它渲染或与其同源维护；
- **发布边界（实现纪律）**：`cmd/okserver` 产物与 `server/` 目录**不进入任何客户端安装包**——Inno iss、build.py 测试包、nfpm（deb/tar.gz）三条构建路径都只打 ok/okd/OkManager 三 exe + web/runtime/changelogs，打包含清单时显式排除 `server/` 与 `okserver` 产物（既有"构建双路径漂移"前科：iss 与 build.py 两条路径各自维护暂存清单，新增/排除产物必须两条路径同步核对）；okserver 的分发渠道只有两个——Docker 镜像（§9.5）与 release 页独立二进制下载（`okserver_<版本>_linux_amd64/arm64`，裸机备选路径用），由 `server/nas/README.md` 指引。

## 10. GUI 服务器页（向导式）

OkManager 左侧导航"服务器"菜单，按配置状态分两种形态：

### 10.1 未配置：三步引导

1. **连接**：填服务器地址 + 端口 → `测试连接`（动作类，转圈 → 显示 okserver 版本 + git 后端状态 ok/异常，失败显示原因）；
2. **登录**：连接成功后出现登录框（用户名 + 密码）→ 成功则 token 存全局配置；
3. **按角色落地**：root/admin → 管理视图（§10.2）；member → 成员视图（§10.3）。

### 10.2 管理视图（root/admin）

- **状态卡**：okserver 版本、git 后端连通、用户数/组织数/仓库数（由 list 端点聚合，不单设 stats 端点）；
- **用户卡**：创建用户（填用户名 → 生成初始密码 + git token，弹窗**一次性显示**并提示复制）、列表、禁用/启用、重置密码；
- **组织卡**：创建组织（名称 + 描述）、成员分配（加/减成员）、团队仓创建；
- **仓库总览卡**：全量仓列表（层/owner/项目/创建人/创建时间）；
- **审计卡**：管理操作流水（简单列表，倒序）。

### 10.3 成员视图（member）

- **我的项目绑定卡**：本地项目列表，每行项目名 + 远端仓状态（未创建/已绑定）+ `一键建仓并绑定`（serverx → okserver 建仓 → syncx init + 首 push，复用 §14 流程）；
- **我的组织卡**：所属组织与团队仓列表（只读，本期）。

### 10.4 通用

- **部署指引卡**：未配置时常显（§9.1 compose + §9.5 部署步骤）；已配置后折叠；
- 已登录态显示当前用户与角色，提供`退出登录`（清本地 token）；
- 本地 okd API：连接/成员类五个——`GET/PUT /api/server/config`、`POST /api/server/test`、`POST /api/server/login`、`GET /api/server/me`、`POST /api/server/repos` `{project}`；管理类一组透传转发（users/orgs/members/repos/audit，对应 §9.3 的 root/admin 端点，供 §10.2 各卡使用）——全部转发 okserver，本地不缓存管理数据。

## 11. GUI 手动同步与冲突解决

自动同步（ticker + 写入防抖）不是实时触发，用户必然有手动同步诉求；且冲突必须有人介入——这两件事都落在管理页。

### 11.1 管理页项目行同步按钮

- **位置**：管理页项目列表每行（项目名右侧），常显小图标按钮；
- **点击行为**：
  1. 项目未初始化同步（非 git 仓）→ 弹初始化确认卡：可填 remote URL（留空 = 仅本地历史；若 §10 已登录服务器，提供"使用服务器建仓"选项跳 §10.3 流程），确认后走 §14 init 流程，成功后再接着同步；
  2. 已初始化 → 直接触发一次 §7 同逻辑同步（走后端 single-flight，与 ticker/CLI 触发的同步去重合并）；
- **反馈纪律**：点击即转圈并禁用（防连点），结束 toast 人话结果——`推送 n 个提交 / 拉取 n 个提交 / 已是最新 / 失败原因`；
- **行内状态点**：同步图标旁状态点（绿 = 已同步，黄 = 有未推变更，灰 = 未启用，红 = 冲突），数据来自 `sync-status.json`，操作后即时刷新，平时随管理页轮询刷新；
- **冲突时**：toast 提示 + 状态点变红，点击进入冲突解决页（§11.2）。

### 11.2 冲突解决页

- **入口与路由**：管理页项目行红状态点 / 同步结果 toast 链接；路由 `#/sync-conflict?project=<name>`；
- **布局**：冲突文件卡片流——每个冲突文件一张卡，卡内并排"本地版本/远端版本"片段预览 + 四个操作按钮 + 解决状态标记（原型定稿 2026-08-28 选定变体 C）：
  - **Accept Me** — 保留我（本设备）的改动；
  - **Accept Theirs** — 采用远端版本；
  - **Merge** — 打开三向合并编辑器（§11.3），逐块手工合并；
  - **AI 合并** — 调用 LLM 按三版本语义合并，结果预填三向编辑器中间栏（§11.3），**由人确认后才落盘**；仅当 LLM 可用时启用（来源选择见下），否则置灰并注明原因；
- **LLM 来源分档**（`[sync] llm_assist`，§12）：
  - `local`（默认，有本地 LLM 配置时）：走本地 `internal/llmx`——个人条目内容不出设备群；
  - `server`：**本期不开放**——okserver 管理面无 LLM（§9.2），待 AI 管线模块（上游 P6）落地后开放。选 server 的语义届时为：个人条目内容发送服务器处理，属用户显式选择，服务端记审计，okserver 管理员可全局禁用（成本/合规闸）；
  - `off`：不显示 AI 合并按钮；
- **LLM 辅助纪律**（沿用既有知识条目）：
  - **永不自动落盘**：LLM 结果只进编辑器中间栏，人确认后才经 resolve 落盘；失败/超时回退人工三向合并，错误可见，绝不静默吞错；
  - **front matter 不让 LLM 碰**：tags 并集、draft 裁决等按确定性规则处理（与 P2 merge driver 同一套字段级规则），LLM 只合并正文；
  - 调用纪律：超时按场景区分（GUI 交互场景给足但可取消）、temperature 缺省不传；
  - wiki/reference 类条目不提供 AI 合并——它们由服务端 AI 管线对着代码重生成（P6 后），服务端单写天然无冲突；
- **顶部操作条（钉死，不随滚动）**：`已解决 n/N` 进度 + `完成同步` + `放弃本次同步`，滚动文件列表时始终可见（原型定稿 2026-08-28，`docs/prototypes/prototype-sync-p1-final.html`）：
  - `完成同步` — 仅当全部文件标记已解决才可点（未全部解决时禁用并注明原因）→ `rebase --continue` + `push`，成功后清 `sync-conflict.json`、跳回管理页；
  - `放弃本次同步` — `rebase --abort` 回滚到同步前状态（本地内容原样保留，不丢）；
- **rebase 语义坑（实现纪律，必须遵守）**：`pull --rebase` 冲突期间 git 的 ours/theirs 是**反转**的——index stage `:2:`（`--ours`）是刚拉下来的**远端**版本，`:3:`（`--theirs`）是正在 replay 的**本地**提交。UI 的 Accept Me / Accept Theirs 必须按用户语义映射（Me → `:3:`，Theirs → `:2:`），严禁直译 git ours/theirs。

### 11.3 三向合并编辑器（仿 Android Studio Merge Revisions）

- **三栏**：左 = 本地版本（只读）、中 = 合并结果（可编辑，预填 git 自动合并结果）、右 = 远端版本（只读）；
- 左/右与中间做行级 diff，冲突块高亮；每个冲突块提供 `«` / `»` 箭头采纳单侧内容到中间；工具栏有"采纳全部非冲突块"；
- 经"AI 合并"进入时：中间栏预填 LLM 合并结果（替换 git 自动合并结果），用户可继续编辑后再应用；
- 底部：`应用`（写回中间内容 + `git add`，回列表标记已解决）/ `取消`；
- **实现路线（默认，可翻）**：自研轻量三栏——`<pre>` + 行号渲染，行级 LCS diff 约百行 JS，不引入 CodeMirror/Monaco 级依赖。理由：GUI 无构建链、知识条目是 Markdown 不需要语法高亮、vendor 现无编辑器组件；若日后需要语法高亮再 vendor CodeMirror 6（§17 待决）。

### 11.4 本地 okd 同步 API（复用 X-Ok-Token 鉴权）

| 端点 | 语义 |
|---|---|
| `POST /api/project/sync` `{project}` | 触发同步（未 init 且带 `remote` 参数则先 init）；返回 `{status: ok/conflict/error, pushed, pulled, conflicts[], message}` |
| `GET /api/project/sync/status?project=` | 该项目 personal 层状态 + 冲突文件列表 |
| `GET /api/project/sync/conflict-file?project=&file=` | `{base, local, remote}` 三版本全文 |
| `POST /api/project/sync/ai-merge` `{project, file}` | 取三版本 → LLM 合并正文 → 返回 `{merged}`（**只返回不落盘**；落盘走 resolve `merged`） |
| `POST /api/project/sync/resolve` `{project, file, action: me/theirs/merged, content?}` | 落盘 + `git add` |
| `POST /api/project/sync/finish` `{project}` | 校验全部解决 → `rebase --continue` + `push` |
| `POST /api/project/sync/abort` `{project}` | `rebase --abort` |

- 所有端点走 syncx single-flight；GUI 只经 API 读冲突状态，`state/sync-conflict.json` 由 syncx 独占写。

## 12. 配置面

项目级 `config.toml`（复用现有覆盖机制）：

```toml
[sync]
enabled = true
remote = "http://nas:3000/<user>/ok-myproject.git"   # 留空 = 仅本地 commit，不推远端
auto_interval_min = 5
llm_assist = "local"    # off|local（server 档待 okserver AI 管线模块落地后开放，§11.2）
```

全局配置（服务器连接，一台机器一份，不随项目仓同步）：

```toml
[server]
url = "http://nas:3100"      # okserver 管理面地址
username = "alice"
token = "***"                # okserver 会话 token，文件权限 0600；不入日志、脱敏回显
```

- `remote` 留空也可用：本地历史是防丢/防改第一层，远端是第二层 → **无服务器也可交付使用**；
- `llm_assist` 缺省行为：本地已配置 LLM（`internal/llmx` 可用）则默认 `local`，否则 `off`；`server` 档在本期配置即视为无效并提示；
- git 推拉凭据不进 OK 配置：写系统 git credential helper（§9.4）；
- 项目级解析按层预留（§13 口子 1）：内部结构即按 `personal` 层建模，序列化为 `[sync]` 是 P1 的简写形态。

## 13. 团队版扩展口（本期必须留，但不实现）

1. **配置按层**：P1 的 `[sync]` 演进为 `[sync.personal]` + `[sync.team]`；解析代码现在就把字段挂在层结构下；
2. **策略参数化**：`syncx.Sync(dir, policy)`；P1 仅 `personal` 策略（直推 main）；团队版加 `team` 策略（推分支 + 建 PR），pull/rebase/冲突处理共用；
3. **状态/GUI 按层**：`sync-status.json` 的 `layers` 结构（§8）；GUI 状态点与冲突页按层渲染，本期只渲染 personal 层；
4. **不新增"单知识源"假设**：团队层将来是项目下独立第二目录/仓；新代码不得假设项目只有一个知识源，此类判断收敛到 store 层；
5. **okserver 模块化**：进程内按模块划分——本期 `mgmt`（用户/组织/仓库/审计），后期 `wiki-pipeline`（上游 §8 AI 管线）作为第二模块并入，共享 DB、GitBackend 与 LLM 配置（其 LLM 同时是冲突解决 server 档后端，§11.2）；
6. **GitBackend 多实现**：接口已抽象（§9.2），GitLab 等后端后续以新实现接入，治理逻辑不变。

## 14. 设备初始化流（`ok sync init [remote-url]`，GUI 确认卡/建仓绑定同走此逻辑）

| 情形 | 行为 |
|---|---|
| 目录非仓、无知识内容 | clone remote 到该目录 |
| 目录非仓、已有内容 | `git init` + 首个 commit + 关联 remote + push（首台设备路径） |
| 两边各自初始化过、均有内容 | **不自动合并**，报错并指引手动 `--allow-unrelated-histories` 合并一次（低频一次性场景） |

经服务器的路径：`一键建仓并绑定` = serverx 向 okserver 申请建仓（幂等）→ 拿 clone URL → 走上表 init 流程 → git token 写 credential helper。

## 15. 测试策略

- 不依赖 Gitea：测试内 `git init --bare` 临时裸仓，`file://` 协议当 remote；
- 核心 E2E：两个临时 `OK_HOME` 模拟双设备——"设备 A 写条目 → sync → 设备 B sync → B 能检索到该条目"闭环（验证 §2 的 mtime 重建链路）；
- 手动跑 E2E 必须导出全套 agent home 隔离变量（既有知识条目教训）；
- 冲突路径用例：双设备改同一文件 → 第二台 sync 报冲突、写 conflict 状态、内容不丢；
- GUI 冲突解决链路：走**真 HTTP API**（不直调核心函数——派发层断链教训）——冲突制造 → `POST sync` 报 conflict → `GET conflict-file` 三版本齐全且 Me/Theirs 映射正确（§11.2 语义坑专项断言）→ `resolve merged` → `finish` → 双端一致；`abort` 路径回滚验证；
- AI 合并链路：`ai-merge` 端点用 mock LLM 客户端（`internal/llmx` 接口注入）——断言三版本正确传入、front matter 不经 LLM、**结果不落盘**、LLM 失败/超时返回可见错误而非静默；
- okserver：GitBackend 用 fake 实现做单测（用户/组织/仓 provisioning、首启 root 生成、登录/token/限流、审计写入、schema migrate 幂等）；对真 Gitea + 真 okserver 容器的全流程（部署 → root 登录 → 建用户 → 成员建仓绑定 → 双设备同步）为**手动验收项**（测试环境不起 Docker）。

## 16. 工作量与交付边界

新增：`internal/syncx`、`internal/serverx`（本地客户端）、`cmd/okserver` + `internal/oksrv`（服务端管理面：SQLite schema、认证、GitBackend/Gitea 实现、API、审计、schema migrate）、`server/nas/` 部署包（compose/.env.example/Dockerfile/README/systemd/backup，§9.6）、okserver 多架构 Docker 镜像构建与发布流水线、`ok sync` / `ok sync init` 命令、okd 同步 ticker + 写入防抖、状态/冲突状态文件、项目级/全局配置段、§10.4 连接/成员类五个 + 管理类透传一组 + §11.4 七个本地 API（含 ai-merge）、GUI 服务器页（三步向导 + 管理/成员双视图）、管理页同步按钮与状态点、冲突解决页（列表 + 三向编辑器 + AI 合并入口）、E2E。依赖新增仅 `golang.org/x/crypto`（bcrypt 用，Go 官方扩展库，仅 okserver 二进制引入）；其余无新第三方依赖（系统 git 除外；三向编辑器自研；LLM 复用 `internal/llmx`）。

## 17. 已决与待决

已决（默认值，评审时可翻）：

- 用系统 git，不用 go-git（§5）；
- **自建 okserver 管理面**：用户/组织/仓库/认证/审计，推翻上游"零认证代码"（用户指示，动机 = 团队版免迁移 + 集中管理面，§2）；
- okserver 定义扩展 = 管理面（本期）+ AI wiki 管线（后期模块并入同进程，§13 口子 5）；
- 仓命名：个人层 `<user>/ok-<project>`、团队层 `ok-<org>/<project>`，每项目一仓、强制 private（§4）；
- **项目 config.toml 随仓同步**（2026-08-28 终审定案）：[sync] 与检索调优全设备一致，设备特定配置在全局配置不受影响（§4）；
- 服务器 = NAS 双容器（okserver + Gitea），Gitea 对终端用户透明（§3、§9）；
- **仓库目录约定**：Go 代码守 cmd/internal 布局（`cmd/okserver` + `internal/oksrv`），非 Go 部署资产归 `server/nas/`（§9.6）；
- **发布边界**：okserver 不进任何客户端安装包（iss/build.py/nfpm 三路径显式排除），只经 Docker 镜像 + release 独立二进制分发（§9.6 纪律）；
- 部署双路径：首选 Docker，备选裸二进制（§9.5）；
- okserver 镜像多架构（amd64 + arm64），随 release 流水线双发 GHCR + Docker Hub，版本号与 ok/okd 对齐（§9.5）；
- Gitea 治理配置：关注册（DISABLE_REGISTRATION）、建仓限额 0（禁普通用户建仓）、默认 private、ROOT_URL 指向 NAS 地址（§9.5）；
- GitBackend 接口抽象，Gitea 首实现（§9.2）；
- root 首启自动生成 + 32 位随机密码（0600 文件 + 日志一次，§9.2）；
- git 凭据 = okserver 代发 Gitea token，写系统 credential helper，不进 OK 配置（§9.4）；
- v1 角色从简：root/admin/member 三级，org 内权限下放留 v1.1（§9.2）；
- GUI 服务器页向导式：连接 → 登录 → 按角色分派（§10）；
- 冲突不自动猜合并，智能合并归 P2 merge driver（§7）；
- GUI 手动同步按钮 + 冲突解决页纳入本期（§11）；
- **LLM 冲突辅助为可选增强**：默认 local（`internal/llmx`，内容不出设备群）；server 档待 P6 后开放，届时需显式选择 + 服务端审计 + 管理员可禁用（§11.2）；
- **LLM 结果永不自动落盘**：GUI 层进中间栏由人确认；P2 merge driver 层（无人值守）只允许 local 且失败回退规则合并（§11.2、上游 §6）；
- **front matter 冲突规则裁决，LLM 只合并正文**（§11.2）；
- UI 的 Me/Theirs 按用户语义映射 rebase 反转的 stage（§11.2 纪律）；
- 三向合并编辑器自研轻量三栏，不引前端依赖（§11.3）。

待决：

- `auto_interval_min` 默认值（建议 5）与写入防抖时长（建议 30s）；
- commit 消息格式是否需含项目名（多项目并行排障场景）；
- 管理页状态点刷新：随现有轮询，还是操作后短轮询即时化；
- root 密码是否强制首登修改；初始密码文件的删除提醒策略；
- org owner 权限下放（v1.1 范围）；
- SSH 公钥管理是否进 v1.1（本期 git 走 HTTPS + token）；
- okserver token 会话时长（建议 30 天）与刷新策略；
- 审计日志保留期；
- AI 合并的 LLM 超时阈值（GUI 交互场景，建议 60s 可取消）与 P2 merge driver 场景阈值（建议 30s）；
- server 档开放时的成本语义提示（谁的服务器谁的 key，成员侧需可见）。
- ~~INDEX.md 由各消费方按需重建，跨设备配置不一致会产生自冲突~~（2026-08-28 已决：配置随仓分发钉死，不一致只剩手工本地改配置一途，INDEX.md 不入 .gitignore、冲突走正常冲突解决路径）；
