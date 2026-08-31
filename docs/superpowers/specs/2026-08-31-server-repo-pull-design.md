# 服务器仓拉取到本机 + 同步状态点实时化 设计

日期：2026-08-31
状态：已定稿（待实施）

## 背景与问题

v2.23.0 个人多端同步上线后，新机器全新安装的 onboarding 是断的：

1. 服务器页成员视图「我的项目绑定」卡（`web/app.js` bindCard）只遍历本机 okd 已注册项目（`/api/projects`）；登录后的自动批量拉取（`maybeAutoBind`）同样只覆盖"本机已注册、未绑定、条目为空"的项目。全新机器本地注册表为空 → 列表空、自动拉取不触发，服务器上已有的个人仓无任何入口可拉。
2. git 凭据断链：okserver 的 `git_token` 只在建仓时下发一次（`internal/oksrv/http.go` apiPersonalRepo，幂等重入返回空串，设计语义"丢失走 reset-password 联动重发"）。新机器拉取已有仓时 provision 返回空 token，现有流程在本机无已存凭据时直接报错让用户找管理员重置密码——自助链路缺失。

次要问题（同区域顺手解决）：管理页项目行同步状态点的 `ahead/behind` 只在每次同步跑完由 `RecordOutcome` 刷新，写入后 30s 防抖窗口内状态点仍是绿色，"正在同步"无任何信号。

## 关键事实（实施前已验证）

- 知识库存储自包含：`store.New(OK_HOME/projects/<项目名>)`，与源码目录无关；项目↔源码目录关联只在本机注册表 `registry.json`（不随同步走）。条目 frontmatter 无任何路径字段（`internal/entry/entry.go`）。
- 注册表 `Project.Paths` 是数组，数据模型本就支持一个项目关联多个目录；备份恢复已有"空路径项目"先例（`internal/backup/backup.go`，`Project{Name}` 无 Paths 原样还原）。空 Paths 容忍度已核实：`FindByCwd`、分支探测、cwd 解析等全部 `len(paths)>0` 守卫。
- clone 路径现成：`syncx.InitForSync(remote, hasContent=false, …)` → `CloneToDir`（设计文档 §14）。
- 管理页已有 ~4s 轮询 `pollManage`，但只在 `last_update`/条目变化时重渲——同步状态变化当前不触发重渲。
- daemon 写入防抖：`gh.OnWrite = NotifyWrite`（30s 防抖后 `runSyncCycle(force=true)`）；分钟级 ticker 兜底。CLI 直写不经 OnWrite，由 ticker 按 interval 收。

## 功能一：服务器有仓、本机无项目 → 一键拉取

### oksrv（唯一服务端改动）

新增 `POST /api/v1/git-token`：已认证用户自助重发 git token。

- 行为：Gitea 侧删除该用户同名旧 token 后重建（避免撞名报错），明文返回一次；审计落库（action `reissue-git-token`）。
- 鉴权：走默认认证链，**不进**强制改密白名单——`must_change_password` 账号须先改密（语义正确：新机器用初始密码登录 → 先强制改密 → 再拉取）。
- 注意回归风险（已沉淀 pitfall）：新增已认证端点必须确认挂在套了强制改密 gate 的路由组内。

### serverx

新增 `GitToken(ctx) (string, error)`，镜像上述端点。

### GUI 层（internal/gui）

- 从 `apiServerRepos` 抽出共用函数 `bindProjectRepo(ctx, st, project)`：provision → 凭据 → init/clone → SetSync → 首次同步，返回与原端点相同的结果结构。`apiServerRepos` 改为薄封装。
- 凭据环节新增分支：provision 返回 `git_token` 为空时，先查本机 credential helper 是否已存该仓凭据；没有则调 `serverx.GitToken` 自助重发 → `StoreCredential`（无 helper 时沿用现有内嵌 URL 回退与 credNote 警告语义）。仅当重发本身失败才报错（文案给出终端手工绑定指引）。替代现在"直接报错让用户找管理员"的死路。
- 新端点 `POST /api/server/pull` `{project}`：
  1. 项目名形状校验（沿用 `validProjectName`）；已注册 → 409。
  2. `registry.Update` 锁内注册空 Paths 壳项目（`Project{Name}`，不加路径）。
  3. 调 `bindProjectRepo`：本地无内容 → `InitForSync` 自动走 clone 路径。
  4. 失败语义：注册成功但 clone 失败时壳项目保留（用户可重试拉取或走既有绑定按钮），返回 error 状态与原因。

### web/app.js

- 绑定卡新增分组「服务器仓（本机未拉取）」：数据源 `me.repos`（personal 层）减去本机已注册项目名。每行「拉取」按钮（调 `/api/server/pull`）；分组头「全部拉取」按钮，逐个串行执行，单个失败不阻断，结束汇总 toast（成功 n 个 / 失败列表）。
- `maybeAutoBind` 扩展候选集：现有"本地空壳未绑定" ∪ 新增"本机未注册的服务器仓"。确认框文案合并展示；确认后未注册的走 `/api/server/pull`，已注册的走 `/api/server/repos`。新机器登录即弹"服务器有 N 个项目数据，是否拉取到本机"。fail-open 语义不变。
- i18n：中英文字典同步加 key。

## 功能二：同步状态点五态实时化

### 状态模型

状态点五态（优先级从高到低）：

1. 红（冲突）：`conflict || MergeInProgress()` —— 现状不变，可点进解决页。
2. 黄闪（syncing）：同步进行中。
3. 黄（有变更）：`dirty || ahead>0 || behind>0`。
4. 绿（已同步）：启用且无上述状态。
5. 灰（未启用/未建仓）：现状不变。

### syncx

`SyncOnce` 开始时写 `state/syncing` 标记文件（内容为开始时间），结束 defer 删除。所有触发路径（daemon ticker、写入防抖、CLI `ok sync`、GUI 绑定）都经 `SyncOnce`，一处打点全覆盖。读侧 stale 守卫：标记 mtime 超 5 分钟视为进程死亡残留，忽略（下次成功同步会清）。

### gui

`projectSyncStatus` 新增两字段：

- `dirty`：`knowledge/` 下最新 `.md` 的 mtime 晚于 `LayerStatus.LastSync`（一次 readdir 的纯文件系统检查，不起 git 子进程；status 文件缺失但有内容时视为 dirty）。
- `syncing`：`state/syncing` 标记存在且未超 stale 阈值。

### web/app.js + style.css

- `syncDotClass`/`syncDotKey` 按上述五态优先级改写；字典加 `syncDotSyncing`（"正在同步"）。
- `pollManage` 变化检测补上 `p.sync` 序列化比对——否则同步状态变化不重渲，黄点/闪烁/变绿都不生效。
- `.pj-sync-dot.syncing`：琥珀色（与 ahead 同色 `#d4a72c`）+ 1s 透明度闪烁动画。

效果链：写入条目 → ≤4s 后黄点 → 30s 防抖到期自动同步 → 黄点闪烁 → 成功变绿。

## 边界（本期不做）

- 团队仓（org 层）拉取：团队内容同步是后续版本能力。
- "给已注册项目追加关联目录"入口：数据模型已支持，留后续小增强。
- syncing 标记只在 daemon/CLI 进程存活期间有意义；daemon 不在时自动同步本就不跑，无标记无闪烁，属正确语义。

## 测试

- oksrv：`POST /api/v1/git-token`——未认证 401；认证后返回非空 token 且审计落库；重复调用撞名重建成功（旧 token 删除新建）。
- gui：`POST /api/server/pull`——未注册项目注册空壳 + clone 成功（fake okserver 镜像契约，沿用 serverx_test/gui api_server_test 模式）；已注册 409；token 空 → 自助重发路径（helper 无存凭据时调用 GitToken）。
- gui：`projectSyncStatus` dirty/syncing 判定——mtime 早于/晚于 LastSync、status 文件缺失、syncing 标记新鲜/stale/不存在。
- syncx：`SyncOnce` 标记文件写入/清除（含出错路径 defer 清除）。
- 前端无测试框架，沿用既有手工走查清单（拉取分组、全部拉取、登录弹窗、五态点）。

## 发布口径

- 服务端有新增端点（git-token）：okserver 镜像需与客户端同版本齐发；旧客户端 + 新服务端无影响（旧客户端不调用新端点）；新客户端 + 旧服务端时拉取链路在自助重发步骤报 404 → 文案提示升级服务端。

---

## 补充定稿（2026-08-31 终审后）：git token 生命周期与凭证管理

终审发现：固定名 `ok-sync-reissue` 按用户唯一，第二台新机器重发会吊销第一台机器已存凭据（静默失效、GUI 无自愈）。裁决如下：

### token 命名：按机器分名

- 客户端调 `POST /api/v1/git-token` 时带 `name_hint`（hostname）；服务端 token 名 = `ok-sync-r-<sanitized hint>`（hint 空/非法回落 `unknown`），删旧只删本机同名再建。
- 每台机器各持一份凭据，互不吊销；Gitea token 数随机器数累积（量小，可控），配套凭证管理入口（下文）供清理。

### 凭证管理（自助 + 管理）

- oksrv 新端点（全部过 auth+gate；管理类再过 admin）：
  - `GET /api/v1/tokens`：列自己的 git token（name、created_at、updated_at=最近使用，Gitea 对每次使用刷新 updated_at；无则展示 created_at）。
  - `DELETE /api/v1/tokens/{name}`：删自己的指定 token。
  - `GET /api/v1/users/{name}/tokens`、`DELETE /api/v1/users/{name}/tokens/{token}`：管理员查看/删除任意用户 token。
- 删除用户时 Gitea 侧 DeleteUser 级联清掉该用户全部 token（在 apiUserDelete 补注释钉住此语义）。
- 客户端：服务器页成员视图加「我的凭证」卡（列表按时间倒序：最近使用(updated_at) 优先、回落创建时间；每条带删除按钮）；root/admin 管理视图加「凭证管理」卡（选用户 → 列表 → 删除）。

### 终审另两项修复（合并前）

- serveBindRepo 的"旧服务端 404"分支补 fake 回归测试（发布口径变成可回归契约）。
- apiServerPull 把 serverClient 未配置检查前置到注册之前（避免孤儿壳项目）；registry.Update 错误分类："已存在" 409，其他（锁/IO）500。
