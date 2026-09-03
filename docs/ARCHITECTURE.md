# Okryptos 项目架构与技术说明

## 目录

- [1. 项目概述](#1-项目概述)
- [2. 技术栈](#2-技术栈)
- [3. 模块架构](#3-模块架构)
- [4. 目录结构](#4-目录结构)
- [5. 核心组件详解](#5-核心组件详解)
- [6. 核心业务架构](#6-核心业务架构)
- [7. 数据流与事件流](#7-数据流与事件流)
- [8. 存储层](#8-存储层)
- [9. 外部集成](#9-外部集成)
- [10. 性能与可靠性策略](#10-性能与可靠性策略)
- [11. 依赖关系图](#11-依赖关系图)
- [12. 构建配置与命令](#12-构建配置与命令)
- [13. CLI 命令面](#13-cli-命令面)
- [14. 测试与验证](#14-测试与验证)
- [15. 常见问题排查](#15-常见问题排查)
- [16. 后续维护建议](#16-后续维护建议)
- [17. 检索算法实现（深度）](#17-检索算法实现深度)
- [18. 配置参数参考](#18-配置参数参考)

---

## 1. 项目概述

**Okryptos** 是一个为 AI 编程助手提供项目知识库的命令行工具，客户端编译产物为 `ok`（+ 常驻 `okd`、GUI 拉起器 `OkManager`）。知识按项目隔离存储，通过 **Kimi Code 的 hooks 机制**在 AI 会话中自动注入项目约定与踩坑经验，并能对"必须写变更日志"这类强制工作流做机制级检查。v2.23 起另有服务端面：`okserver`（NAS/Docker 部署的多端同步与管理服务端）与 `okdeploy`（一键部署器，独立分发）。

| 功能 | 说明 |
|------|------|
| **基础注入** | 每会话首次提问时，把 mandatory 知识条目全文 + 知识索引注入 AI 上下文 |
| **检索注入** | 每次提问时做关键词 + 向量语义混合检索，注入最相关的知识条目 |
| **强制检查** | 跟踪 AI 修改过的文件，回合结束时检查强制规则（如"改代码必须写变更日志"），不满足则阻断 |
| **知识管理** | `ok init/add/search/index/list/doctor` 命令维护知识库 |
| **首次引导** | `ok setup` 一键写入 hooks 配置、安装 kimi 技能、配置 embedding |
| **全局开关** | `ok on` / `ok off` 随时启停全部 hooks |
| **多端同步** | `ok sync`/`ok sync init` + okd 自动触发（ticker/写入防抖），知识库即 git 仓，GUI 冲突解决页 |
| **服务端** | okserver 管理面（账号/仓库/凭证/强制改密）+ okdeploy 一键部署到 NAS（docker compose） |

**模块名**: `okryptos`
**二进制名**: `ok`（Windows 为 `ok.exe`）
**设计文档**: `docs/superpowers/specs/2026-07-22-openknowledge-design.md`

---

## 2. 技术栈

**表格 A — 核心技术栈**：

| 类别 | 技术/版本 |
|------|-----------|
| 语言 | **Go**（go.mod 声明 `go 1.25.0`） |
| 构建 | Go 标准工具链，单二进制产出，无 CGO（SQLite 用纯 Go 的 modernc.org/sqlite） |
| CLI 解析 | 标准库 `flag`（刻意不引 cobra） |
| HTTP | 标准库 `net/http`（embedding API 调用） |
| 测试 | 标准库 `testing` + `net/http/httptest` |

**表格 B — 第三方依赖清单**（直接依赖 7 个，与 `go.mod` 一致）：

| 依赖 | 版本 | 用途 |
|------|------|------|
| `github.com/BurntSushi/toml` | v1.6.0 | registry.toml 与各层 config.toml 解析 |
| `gopkg.in/yaml.v3` | v3.0.1 | 知识条目 frontmatter 解析 |
| `github.com/bmatcuk/doublestar/v4` | v4.10.0 | 强制规则的 `**` glob 匹配 |
| `modernc.org/sqlite` | v1.54.0 | kb.db 索引库（FTS5 全文检索 + 向量存储）与 okserver 管理面存储，纯 Go 无 CGO |
| `golang.org/x/crypto` | v0.55.0 | oksrv bcrypt 口令散列 + deployx SSH 客户端（x/crypto/ssh） |
| `golang.org/x/sys` | v0.47.0 | Windows 系统调用（托盘/窗口/已知目录解析） |
| `github.com/jchv/go-webview2` | v0.0.0-2026… | OkManager/okdeploy 的 WebView2 内嵌窗口（仅 Windows 构建路径） |

---

## 3. 模块架构

单 module（`okryptos`），internal/ 下 34 个包（含 `rxext/sdk`、`deployx/webui` 子包）+ `cmd/` 五入口（ok/okd/okmanager/okserver/okdeploy），严格单向依赖、无环：

```
┌─────────────────────────────────────────────────────────────────┐
│ cmd/ok·okd·okmanager（客户端三 exe）· okserver（服务端）· okdeploy │
└───────┬───────────────────┬─────────────────┬───────────┬───────┘
        │                   │                 │           │
┌───────▼────────┐  ┌───────▼────────┐  ┌─────▼──────┐ ┌──▼───────────┐
│ internal/cli   │  │ internal/gui   │  │internal/hook│ │internal/oksrv│
│ （人用的命令）  │  │ （Web GUI）    │  │（hooks 入口）│ │（服务端管理面）│
└───────┬────────┘  └───────┬────────┘  └─────┬──────┘ └──────────────┘
        │                   │         ┌───────▼────────┐
        │                   │         │ internal/project│（cwd→项目）
        │                   │         └───────┬────────┘
   ┌────▼───────────────────▼─────────────────▼───────────────────┐
   │ 基础层（被上层直接组合）                                       │
   │ registry · entry · config · store · embed · embedx ·         │
   │ embedsidecar · index · retrieve · state · enforce · setupx · │
   │ agentx（宿主适配）· rxext（Reasonix 扩展）· daemon · daemonx │
   │ tray · webdir · fsx（原子写/文件锁）· logx（日志/轮替）·     │
   │ llmx · backup · wiki · procx · version ·                     │
   │ syncx（个人多端同步）· serverx（okserver 客户端）· credmig · │
   │ deployx（一键部署器，okdeploy 独占）                         │
   └───────────────────────────────────────────────────────────────┘
```

**依赖关系**（→ 表示 import；仅列主干，完整以 `go list -deps` 为准）：

- `cmd/ok` → `cli`、`gui`、`hook`、`daemon`、`rxext`；`cmd/okd`（daemon 常驻）→ `daemon`、`gui`、`tray`；`cmd/okmanager`（GUI 拉起器 + WebView2 窗口宿主）→ `daemon`、`gui`、`registry`；`cmd/okserver`（服务端，Linux/NAS）→ `oksrv`、`logx`、`version`；`cmd/okdeploy`（部署器，独立分发）→ `deployx`、`gui`（BrowserOptions 共用浏览器回退）
- `cli` → `registry`、`entry`、`store`、`embed`、`index`、`retrieve`、`project`、`config`、`setupx`、`backup`、`agentx`、`syncx`
- `gui` → `registry`、`entry`、`store`、`index`、`retrieve`、`config`、`setupx`、`agentx`、`llmx`、`wiki`、`webdir`、`syncx`、`serverx`、`credmig`、`daemonx`、`version`
- `hook` → `project`、`registry`、`store`、`embed`、`index`、`retrieve`、`state`、`enforce`、`setupx`、`wiki`
- `agentx`（多 agent 宿主适配层，hook/cli/gui/setupx 共享）→ `fsx`、`config`、`registry`、`daemonx`
- `rxext`（+`rxext/sdk`，Reasonix 扩展协议 sidecar）→ `hook`、`agentx`、`logx`
- `daemon`/`daemonx` → `gui`、`embedsidecar`、`tray`、`webdir`、`syncx`、`credmig`（单实例端口锁、指纹校验、同步 ticker）
- `syncx`（个人多端同步引擎，叶子包）→ `fsx`、`procx`；`serverx`（okserver 薄客户端）→ 仅标准库；`credmig`（凭证统一迁移）→ `config`、`registry`、`serverx`、`store`、`syncx`
- `oksrv`（okserver 服务端管理面）→ 标准库 + modernc.org/sqlite + x/crypto(bcrypt)
- `deployx`（+`webui` 内嵌前端）→ `gui`（仅 BrowserOptions 类型）、x/crypto(ssh)
- `setupx` → `registry`、`config`、`embed`、`embedsidecar`、`agentx`（setup 引导共享逻辑，cli 与 gui 复用）
- `project` → `registry`、`config`、`store`
- `index` → `entry`、`embed`、`retrieve`、`config`（+ modernc.org/sqlite）
- `enforce` → `config`、`state`（+ doublestar）
- `fsx`（原子写 tmp+fsync+rename、WithFileLock 文件锁）、`logx`（按行时间戳 Writer + 大小轮替归档）、`llmx`、`procx`、`version`、`backup`、`wiki`、`embedx`、`embedsidecar` → 仅标准库 + 少量上述基础包

**分层原则**：`hook`、`cli`、`gui` 是三个互不 import 的应用层；`project` 是 hook 与 cli 共享的项目解析层；`setupx` 是 cli 与 gui 共享的引导逻辑层；`syncx`/`serverx` 是 cli、gui、daemon 三方共享的同步引擎与服务端客户端；`oksrv`（服务端）与 `deployx`（部署器）各自独立成面、不进客户端依赖链；其余为单一职责的基础包。

---

## 4. 目录结构

```
Okryptos/
├── go.mod / go.sum                # 模块定义（4 个第三方依赖）
├── cmd/ok/
│   ├── main.go                    # 入口：子命令调度，hook 路径 panic-recover 兜底
│   └── integration_test.go        # 端到端测试（编译真实二进制驱动）
├── cmd/okd/                       # 常驻 daemon 入口（生产形态；cmd/ok 的 daemon 子命令仅为兼容残留）
├── cmd/okmanager/                 # GUI 拉起器 + WebView2 原生窗口宿主（窗口状态记忆/token 注入）
├── cmd/okserver/                  # 服务端管理面入口（env 配置 + root 首启 + reset-root 子命令）
├── cmd/okdeploy/                  # 一键部署器入口（双击启动 + 随机回环端口 + 内嵌窗口/浏览器回退）
├── internal/
│   ├── registry/                  # ★ 项目注册表与路由
│   │   ├── registry.go            #   Registry/Project、NormalizePath、FindByCwd、HooksDisabled
│   │   └── registry_test.go
│   ├── entry/                     # ★ 知识条目
│   │   ├── entry.go               #   frontmatter 解析/序列化、Load/LoadTolerant、Slug
│   │   └── entry_test.go
│   ├── config/                    # 配置
│   │   ├── config.go              #   Config 各节、Default、Load/LoadMerged、ResolvedAPIKey
│   │   └── config_test.go
│   ├── store/                     # KB 存储布局
│   │   ├── store.go               #   目录路径、token 预算截断
│   │   └── store_test.go
│   ├── embed/                     # 向量
│   │   ├── embed.go               #   Client 接口、OpenAI 兼容客户端、Cosine
│   │   └── embed_test.go
│   ├── index/                     # ★ SQLite+FTS5 索引库（kb.db）
│   │   ├── db.go                  #   Open/Close/Count、旧版 vectors.json 迁移
│   │   ├── sync.go                #   增量同步（filename+mtime+size）、损坏条目跳过、INDEX.md 重建
│   │   ├── query.go               #   FTS5 BM25 + 余弦混合查询、Mandatory
│   │   └── index_test.go
│   ├── retrieve/                  # 检索分词
│   │   ├── retrieve.go            #   Terms（CJK 二元组）
│   │   └── retrieve_test.go
│   ├── state/                     # 会话状态
│   │   ├── state.go               #   Session（触碰文件/已阻断规则/已基础注入/wiki 已提示）、Clean
│   │   └── state_test.go
│   ├── wiki/                      # wiki 游标与落后计数（叶子包：stdlib + procx + 外部 git 命令）
│   │   ├── wiki.go                #   State 读写（state/wiki.json：base_branch + cursors + merges 谱系，旧格式惰性识别）、CheckStatus
│   │   ├── status.go              #   CurrentBranch、commitExists/isAncestor/mergeBase（git 可达性判定）
│   │   └── *_test.go
│   ├── enforce/                   # 强制规则
│   │   ├── enforce.go             #   changelog_required 判定（doublestar 匹配）
│   │   └── enforce_test.go
│   ├── project/                   # 项目解析（hook 与 cli 共享）
│   │   ├── project.go             #   Context{Project,Store,Config}、FromCwd
│   │   └── project_test.go
│   ├── hook/                      # ★ hooks 事件处理
│   │   ├── hook.go                #   Event 解析、HandlePrompt/HandlePostTool/HandleStop
│   │   └── hook_test.go
│   ├── cli/                       # 管理命令
│   │   ├── cli.go                 #   Init/Add/Search/Index/List/Doctor
│   │   ├── setup.go               #   Setup 引导（编排 setupx，交互收集 embedding）
│   │   ├── toggle.go              #   On/Off 全局开关
│   │   └── *_test.go
│   ├── setupx/                    # setup 共享逻辑（cli 与 gui 复用）
│   │   ├── setupx.go              #   HooksBlockFor/UpsertHooksBlock/InstallSkills/EnsureSkills/SkillsInstalled/SaveEmbedding/TestEmbedding/Enable/Disable
│   │   ├── skills/                #   内嵌技能模板（SKILL.md，{{EXE}} 烘焙）
│   │   └── setupx_test.go
│   ├── agentx/                    # ★ 多 agent 宿主适配层（kimi/claude/codex/qoder/zcode/opencode/pi/reasonix/dsh…）
│   │   ├── agentx.go              #   Agent 接口、Register/Detected/All、SkillsHome、CLIExe
│   │   └── <宿主>.go              #   各适配器 Install/Remove/EnsureHooks（宿主 settings 读-改-写一律 WithFileLock）
│   ├── rxext/                     # Reasonix 扩展协议 sidecar（sdk/ 子包为扩展 SDK）
│   │   └── serve.go               #   input.receive/tool.after/compaction.complete 三拦截器，fail-open
│   ├── daemon/ + daemonx/         # GUI 常驻 daemon（端口即单实例锁、指纹校验、Ensure/Stop）
│   ├── embedsidecar/ + embedx/    # llama-server sidecar 生命周期（want 标记调和、崩溃计数）
│   ├── fsx/                       # ★ 原子写（tmp+fsync+rename）与 WithFileLock 文件锁——全仓库文件写出口
│   ├── logx/                      # 按行时间戳 Writer + 日志大小轮替归档（RotateIfOversize/CleanArchives）
│   ├── llmx/                      # LLM 调用（超时按场景区分、temperature 缺省不传）
│   ├── backup/                    # 导出/导入 zip 包（防 zip-slip/zip-bomb、路径与项目名校验）
│   ├── syncx/                     # ★ 个人多端同步引擎（叶子包：fsx + procx + 外部 git 命令）
│   │   ├── git.go                 #   git 执行器：参数数组不走 shell、错误三分类（ErrGitNotFound/ErrTimeout/*ExitError）
│   │   ├── repo.go                #   Repo 原语：IsRepo 纯文件系统判断、Status/CommitAll/Push/CloneToDir
│   │   ├── sync.go                #   Sync 编排（commit→pull --rebase→push、冲突守卫、single-flight SyncOnce）
│   │   ├── status.go              #   分层同步状态文件（sync-status.json/sync-conflict.json/syncing 标记）
│   │   ├── conflict.go            #   冲突解决原语（三版本按用户语义映射 local/remote，不直译 ours/theirs）
│   │   ├── init.go                #   sync init 三情形编排（本地 init / 克隆 / 首推，cli 与 gui 共用）
│   │   └── credential.go          #   git 凭据写系统 credential helper（无 helper 回退 URL 内嵌）
│   ├── serverx/                   # okserver 薄客户端（Bearer + 全端点，照 llmx 形态，叶子包）
│   ├── credmig/                   # 凭证统一迁移：机器级 git 凭证 ensure/覆盖 remote/迁移标记
│   ├── oksrv/                     # ★ okserver 服务端管理面（SQLite 存储/bcrypt 认证/GitBackend/HTTP API）
│   │   ├── store.go               #   users/sessions/orgs/org_members/repos/audit/meta 七表（must_change_password 幂等迁移）
│   │   ├── auth.go                #   会话 token、root 首启、登录限流
│   │   ├── gitbackend.go + gitea.go  # GitBackend 接口（fake 实现供测试）与 Gitea admin API 实现
│   │   └── http.go                #   管理面 API：Bearer 鉴权 + admin 门控 + 强制改密 gate + 审计
│   ├── deployx/                   # ★ okdeploy 部署器内核（okdeploy 独占）
│   │   ├── executor.go + ssh.go   #   Executor 接口（流式执行/上传/下载）+ x/crypto/ssh 实现、LogHub 广播
│   │   ├── task.go                #   任务编排框架（步骤序列 + 掩码敏感输出）
│   │   ├── probe/deploy/manage/backup.go  # 远端探测 / 部署编排 / 管理任务（升级·卸载·重置 root）/ 备份恢复
│   │   ├── templates/ + templates.go      # compose 模板与 .env 渲染（full/external 双模式）
│   │   ├── api.go                 #   本地 HTTP API + SSE 日志服务（token 鉴权）
│   │   └── webui/                 #   go:embed 内嵌前端（四页 + 中英切换）
│   ├── tray/ · webdir/ · procx/ · version/   # 托盘、embed 前端资源、进程、版本
│   └── gui/                       # ★ 配置中心 API + 静态页分发（okd 承载；ok gui / OkManager 双入口）
│       ├── server.go              #   hostGuard/withAuth 路由管线、令牌 fragment 下发
│       ├── api.go                 #   管理 API（条目 CRUD/检索/setup/toggle/终端白名单执行/attach/detach）
│       ├── api_sync.go            #   同步七端点（sync/status/conflict-file/resolve/finish/abort/ai-merge）
│       ├── api_server.go          #   服务器页本地端点（[server] 配置/登录/建仓一条龙/拉取/管理类透传）
│       ├── api_update.go          #   版本升级四端点（check 6h 缓存/skip/download .part 续传/apply 静默安装+熔断）
│       ├── graph.go               #   GET /api/graph 图谱数据（ref/struct/sem 三类边、sem 动态阈值）
│       ├── gui_state.go · changelog.go  # 窗口状态（gui-state.json）与 gui.json 小状态（更新缓存/跳过版本）
│       ├── token_script.go        #   内嵌窗口 token 注入脚本（window.__okToken）
│       ├── llm.go · embedding.go · api_enforce.go · api_hookstimeout.go
│       ├── browser.go · browser_windows.go · browser_unix.go · open_windows.go · open_other.go
│       ├── window_other.go        #   非 Windows 平台无操作实现
│       └── *_test.go
├── web/                           # 配置中心前端（零依赖原生 HTML/JS/CSS，七菜单：管理/图谱/引导/服务器/设置/日志/其他）
│   ├── index.html                 #   页面骨架（{{TOKEN}} 占位符由服务端注入令牌）
│   ├── app.js                     #   条目 CRUD、检索预览、引导流程、图谱力导向引擎（自绘 SVG，无图库依赖）、服务器页、版本升级
│   ├── style.css
│   └── vendor/                    #   图谱选型期的 vendored 对照库（vis-network/G6/sigma，仅原型用，生产页面不加载）
├── server/nas/                    # okserver NAS 部署包（Dockerfile/docker-compose.yml/systemd/backup）
├── installer/                     # Inno Setup 安装包脚本（[Run] 段装完拉起 okd/OkManager）
├── scripts/
│   ├── build-dist.sh              # 发布构建：-ldflags "-s -w" + 版本注入，产出 dist/ok.exe + dist/web/ + dist/deploy/okdeploy
│   ├── build.py                   # 一键构建（dist/ + Inno 安装包）；--test 产出版本号带 _test 后缀的测试安装包（不改 iss）
│   ├── build-linux.sh · sync-version.sh   # Linux 发布（tar+deb）；版本三处同步（README/站点/四个 winres.json）
│   └── publish-release.py · verify-deb.py
├── .github/workflows/docker.yml   # v* tag 触发 okserver 镜像多架构构建，GHCR + Docker Hub 双发
├── dist/                          # 发布产物（.gitignore 忽略）：ok.exe/okd.exe/OkManager.exe + web/ + deploy/okdeploy
├── docs/
│   ├── ARCHITECTURE.md            # 本文档
│   ├── changelogs/                # 变更日志（强制规则要求的落点）
│   └── superpowers/
│       ├── specs/                 # 设计文档
│       └── plans/                 # 实施计划
└── .superpowers/sdd/              # 执行过程草稿（brief/report/diff，不入库）
```

---

## 5. 核心组件详解

### 5.1 registry — 项目注册表与路由（registry.go + home_windows.go + home_other.go）

知识库的全局定位层。`Home()` 返回 KB 根目录：`OK_HOME` 环境变量优先（全仓测试隔离依赖），否则真实用户目录下的 `~/.okryptos`（v2.25.0 改名版：`Home()` 按存在性在新根 `~/.okryptos` 与旧根 `~/.openknowledge` 间选择，三入口启动早期 `MigrateLegacyHome` 把旧根整体 rename 为新根并修 config 内旧根绝对路径，幂等）——真实目录解析对 `HOME`/`USERPROFILE` 重定向**免疫**（Windows 走 `windows.KnownFolderPath(FOLDERID_Profile)`，其他平台 `os/user.Current()` 解析，失败回退 `os.UserHomeDir()`）：CodePilot 等宿主以 DB provider 运行时会把子进程 HOME 重定向到 shadow 临时目录做 provider 隔离，跟随重定向会看到空数据根，hook 注入静默失效（v2.11.1 修复）。`Registry` 持久化在 `registry.toml`，核心是 **最长前缀匹配** 的项目路由：

```go
func (r *Registry) FindByCwd(cwd string) *Project  // 规范化后最长前缀匹配
func NormalizePath(p string) string                 // "\"→"/"、全小写、去尾 "/"
func HooksDisabled() bool                           // 全局开关标志文件存在性
```

Windows 下大小写不敏感与分隔符混乱问题全部收敛到 `NormalizePath` 一处。匹配不到项目的目录静默放行（fail-open 的起点）。

### 5.2 entry — 知识条目（145 行）

知识的最小单位：`---\n<yaml frontmatter>\n---\n<body>` 格式的 Markdown 文件。frontmatter 含 `title/type/tags/mandatory/summary`，`Body` 与磁盘路径 `Path` 不序列化（`yaml:"-"`）。解析容忍 CRLF 与 UTF-8 BOM；`type` 限定 `rule|pitfall|note|reference` 四种。

**出生分支溯源（v2.8.0）**：`ok add`/`ok propose` 落笔时按当前分支自动补 `born:<分支>` 溯源标签（`[provenance] auto_born` 可关，默认开；非 git/探测失败 fail-open 不阻断写入；用户显式传入的 born 不被覆盖，`ok approve` 转正不改写 tags）。born 与 `branch:` 正交：branch 管"在哪生效"（注入过滤），born 管"在哪出生"（只展示不过滤）。`ok backfill-born` 按当前分支给无 born 的存量条目回填（预览确认后写入）。

- `Load(dir)` — 严格模式，任何文件解析失败即整体报错（`ok list` 用，错误要暴露给用户）
- `LoadTolerant(dir)` — 宽容模式，坏文件跳过并收集错误；已不在生产路径上——注入路径的容错由 `index.Sync` 的损坏跳过实现（见 5.6），保留为可用 API

### 5.3 config — 三层配置合并（95 行）

```go
func LoadMerged(projectPath, globalPath string) (Config, error)
```

生效配置 = **内置默认 ← 全局 `~/.okryptos/config.toml` ← 项目 `config.toml`**，后者覆盖前者（TOML 依次解码到同一 struct 实现）。两个数组例外：`embedding.profiles` 按 name 合并；`[[enforce]]` 全局与项目追加合并（全局在前）——GUI 规则卡写全局层，用户手改项目层补的规则同样生效。配置解析失败（如手改写坏 toml）整条 hook 链路 fail-open：规则不生效、不阻断，错误记 `ok.log`。API key 解析收敛在一处：

```go
func (e Embedding) ResolvedAPIKey() string  // api_key 字段 > api_key_env 环境变量 > ""
```

### 5.4 store — KB 存储布局（39 行）

纯路径计算层：`KnowledgeDir()/IndexPath()/KbPath()/StateDir()/ConfigPath()`；另有 `TruncateToBudget` 按"字符数(rune) ÷ 2"保守估算 token 并截断注入文本。INDEX.md 的生成已移交给 `index` 包。

### 5.5 embed / embedx / embedsidecar — 向量客户端与内置推理 sidecar

- `embed.Client` 接口隔离 HTTP 细节，测试用 `httptest` fake server，不碰真实网络。查询与建索引是**两条路径**：`EmbedQuery`（查询侧前缀）/ `EmbedDocument` + `EmbedDocuments`（文档侧前缀 + 批量，`ok index` 重建按 32 条/批）——指令感知模型只在对应路径加前缀（qwen3 查询侧 Instruct 前缀、nomic `search_query`/`search_document`）；`ModelIdentity()` 返回建索引的模型身份串（写入 kb.db meta 供切换检测，空串=旧式构造不参与判定）
- `OpenAIClient` 实现 OpenAI 兼容协议：`POST {base_url}/embeddings`，key 空则不带 `Authorization`（适配无鉴权本地服务），带 context 超时——线上服务 / Ollama / 内置 llama-server **三形态共用**同一客户端
- **内置模型清单**（`manifest.go` 的 `BuiltinModels`，4 档）：repo/文件名/size/sha256/维度/pooling/双路径前缀全部钉死（默认 qwen3-emb-0.6b-q8 639MB 1024 维；bge-m3 Q4_K_M/Q8_0；nomic-embed-text v1.5 768 维），变更新增条目即可；`MirrorBase` 解析镜像源（默认 hf-mirror 国内镜像 / huggingface / 自定义 base）；`Download` 为 `.part` 断点续传（Range）→ 整文件 sha256 校验 → 原子改名，校验不符删 `.part` 防循环续传坏文件
- `embedx` 是**三形态唯一构造点**（CLI/hook/GUI 共用）：openai 直连；ollama 在 base_url 后补 `/v1`；builtin 经 embedsidecar 状态文件发现端口——未就绪写 want 标记请求 daemon 拉起并**立即返回 nil**（调用方走纯关键词降级，绝不等待冷启动）；`QueryVec` 在客户端身份与索引 meta 不符时拦截语义通道并返回中文提示（展示层级由调用方定：CLI stderr / hook 日志）
- `embedsidecar` 管理内置推理 sidecar（llama.cpp `llama-server`）：状态文件 `<KB根>/embed-sidecar.json`（pid/port/model_id/last_used；800ms 预算快探 `/health`）、want 标记 `embed-sidecar.want`、日志 `embed-sidecar.log`；`Manager` 仅 daemon 持有，生命周期见 17.4
- 条目向量不再存 vectors.json，而是存于 kb.db 的 `vectors` 表（float32 小端 blob，见 5.6）；旧版 vectors.json 在首次打开 kb.db 时自动导入并改名为 `.bak`

### 5.6 index/retrieve — 索引化混合检索（db.go 138 + sync.go 240 + query.go 138 + retrieve.go 44 行）

检索不再逐文件扫描 Markdown，而是查询 SQLite 索引库 `kb.db`（位于各项目 KB 根目录；entries/entries_fts/vectors 之外另有 `meta(key,value)` 表记录建向量的模型身份 `embedding_model`/`embedding_dim`，见 17.4）。同步按 filename+mtime+size 增量（枚举优先、只解析变化文件）；查询为准入按通道独立判定 + 融合排序：融合默认 RRF（`score = Σ 1/(rrf_k+rank)`，只看名次不看分数），`fusion = "weighted"` 回滚旧加权（`α·归一BM25 + β·余弦`）。**草稿条目（frontmatter `draft: true`，由 `ok propose` 写入）不进 FTS 与向量，检索与注入一律排除；INDEX.md 中以【草稿】标记，批准（`ok approve` / GUI 采纳）后才参与检索**。**算法实现细节（分词、BM25、归一化、混合、降级矩阵、实测性能）见第 17 章**，配置参数见第 18 章。

### 5.7 state — 会话状态（96 行）

`Session{SessionID, Touched, BlockedRules, BaseInjected}` 持久化到 `state/session-<净化id>.json`（sessionID 净化为安全文件名，防路径穿越）。三个职责：记录触碰文件（enforce 的证据）、阻断记忆（同会话同规则只阻断一次，防死循环）、基础注入标记（每会话只注入一次）。`Clean` 清理 7 天前的状态文件。

### 5.8 enforce — 强制规则判定（34 行）

v1 仅 `changelog_required`：触碰文件中存在匹配 `code_globs` 的 且 不存在匹配 `changelog_glob` 的 → 阻断并返回用户配置的 message。用 doublestar 做 `**` glob 匹配（`**/*.go` 可匹配根目录文件）。刻意**不理解**变更日志的细则——细则写在知识条目里由注入教给 AI，hook 只做机械检查。判定层兼容短写法别名 `changelog`（2026-08-22~08-27 GUI 规则卡误用的存量写法；hook/core.go 归一，GUI 落盘已统一为 `changelog_required`）。

### 5.9 hook — hooks 事件处理（337 行）

三个 handler 共享同一套防御结构：**第一行检查全局开关 → 解析事件 → 路由项目 → 各自逻辑 → 任何错误只记 ok.log 并 exit 0**。

- hook 入口自愈：开关开启时仅在 `HandlePrompt`（hook prompt）入口先跑 `selfHealHooks`——遍历 `agentx.Detected()` 逐 agent 调 `EnsureHooks`（kimi 标记块被 kimi-code 清掉时自动备份并重写；pi 扩展内容过期时重写，文件不存在则不动），错误只记 ok.log（fail-open）

- `Event` 的 `Prompt` 是 `json.RawMessage`，`PromptText()` 兼容两种真实载荷形态（字符串 / `[{"type":"text","text":"..."}]` 数组）
- `FilePath()` 取 `tool_input.path`（kimi 实际字段），兼容 `file_path`
- `HandlePrompt`：打开 kb.db → **查询前增量同步**（`Sync`，无 key 时跳过向量；返回 `*CorruptEntriesError` 时记 ok.log 后继续注入）→ 每会话首次提问做基础注入（`Mandatory()` 全文 + INDEX.md，标记 `BaseInjected` 且仅当内容非空才置位）→ 每次提问 `Query` 混合检索注入；embedding 失败降级纯关键词
- `HandlePostTool`：记录触碰文件（经 `relativize` 转项目相对、小写、`/` 分隔）
- `HandleStop`：先按 `[capture]` 配置评估 **auto 自省**——`mode = "auto"` 且本轮有触碰文件、距上次提醒满 `turn_interval` 个 Stop 时，输出自省提醒并以 exit 2 阻断一次（强制 AI 复盘本轮经验、值得沉淀则当场 `ok propose` 草稿）；随后评估 enforce 规则，命中即 `MarkBlocked` → 保存状态 → stderr 输出 message → **exit 2**（全项目唯一非零出口）

### 5.10 cli — 管理命令（cli.go 1294 行 + setup.go + toggle.go）

- `cli.go`：`Init`（项目名缺省取目录基名；**同名项目幂等补挂工作目录**——服务器拉取/备份恢复产生的空壳项目（无 paths）`ok init` 同名时经 `registry.AddPath` 补挂，防串库收窄）、`Add`（重复条目拒绝；后接索引库同步）、`Propose`（AI 面向的草稿写入：`draft:true`、只同步 INDEX 不算向量）、`Approve`（草稿转正，同步 INDEX 并补算向量；同一秒内 mtime 不变时手动推进一秒防 diff 漏判）、`CaptureCmd`（打印或设置项目 `[capture]` 模式，整段替换幂等写入）、`Search`（检索预览，走 `index.Query`；克隆/拉取后索引滞后时先按需增量同步再检索）、`Index`（索引库增量同步并打印条目数）、`List`（文件扫描，人用命令开销可忽略）、`Doctor`（注册表/配置/embedding 连通性/hooks 安装状态/开关状态）
- `Sync`：`ok sync`（一次执行 = commit → pull --rebase → push，编排与守卫全在 syncx；冲突时列文件并指引到 GUI 冲突解决页）与 `ok sync init [remote-url]`（三情形：无远端仅本地历史 / 本地无内容克隆远端 / 本地有内容首推；远端已有内容报 `ErrRemoteNotEmpty` 不自动合并）
- `setup.go`：见第 6.4 节
- `toggle.go`：`On`/`Off` 即删除/创建 `~/.okryptos/hooks-disabled` 标志文件

### 5.11 gui — 配置中心 Web UI（api.go + api_sync/api_server/api_update/graph 等专题文件 + browser/window/open 平台件）

配置中心是七页单页应用（管理/图谱/引导/服务器/设置/日志/其他），供不熟悉命令行的用户完成首次引导与日常知识维护。gui 包只出 HTTP API 与静态页，进程生命周期由 internal/daemon 托管（okd 常驻，页面关闭不退出）。**双入口**：`ok gui`（或无参数运行）与 `OkManager.exe` 同为薄启动器——EnsureCurrent 确保 okd 在线后打开窗口即退；Windows 上 **内嵌窗口优先**（`OpenPreferred` 决策层：WebView2 常青运行时在位且 OkManager.exe 同目录时由 cmd/okmanager 自建原生窗口嵌 WebView2——离屏可见创建避白帧、窗口状态记忆（gui-state.json）、导航前注入 `window.__okToken`（token_script.go，origin 门控）；不可用时回退应用模式浏览器（`daemon.OpenGUI`）；托盘双击同链路。

- **server.go**：仅剩包注释（gui-split 后监听/托管全在 daemon）。web 资源目录由入口定位：`<exe目录>/web` 优先，其次 `<当前目录>/web`（dist/ 布局正好满足前者）；资源不内嵌、实时读盘分发（no-cache）。
- **api.go**：`/` 原样返回 index.html（token 不再内嵌 HTML，由前端从 URL `#token=` fragment 读取并转存 sessionStorage）；静态资源仅白名单 `app.js`/`style.css`/`favicon.ico`/`help.md`；全 mux 外层有 Host/Origin 校验（Host 必须回环、`/api/*` 的 Origin/Referer 存在则必须同源、永不输出 CORS 头，见 daemon/server.go hostGuard）；`/api/*` 全部经 `X-Ok-Token` 头鉴权（缺失/错误 401）；条目文件名参数必须是不含 `..` 与路径分隔符的 `.md` 基本名（防路径穿越）；写操作（POST/PUT/DELETE entry）落盘后自动 `index.Sync` 同步索引库。项目列表（`/api/status`、`/api/projects`）附 `last_update`（kb.db mtime）并按其降序——最近有知识写入的项目排最前。`DELETE /api/project` 删除项目知识库：**先注销注册表**（`registry.RemoveProject` + Save，失败 500 中止、目录不动）**再 `os.RemoveAll` 项目目录**（失败 200 + `warning`/`dir`——兜底永远偏向留数据）；目录名取注册表匹配后的 `p.Name`，不接受用户原始输入拼路径。

端点面（按页分组）：

| 页面 | 端点 |
|------|------|
| 管理 | `GET /api/projects`（附同步状态：enabled/ahead/behind/conflict + dirty（mtime 比对）/syncing（标记文件））、`GET /api/entries?project=`、`GET/POST/PUT/DELETE /api/entry`（标题 slug 定文件名、重复 409、写后同步索引）、`POST /api/approve`（草稿转正）、`POST /api/entry/archive`（归档/取消）、`POST /api/entry/optimize`（LLM 优化对照，未配置 409 `no_llm`）、`GET /api/search?project=&q=`、`GET /api/project/branch-info?project=`（继承徽标 hover 数据）、`POST /api/project/attach` / `POST /api/project/detach`（空壳项目补挂/解除工作目录，registry.AddPath/RemovePath） |
| 同步 | `POST /api/project/sync`（force 同步；含 sync init 三情形）、`GET /api/project/sync/status`、`GET /api/project/sync/conflict-file`、`POST /api/project/sync/resolve`（me/theirs/merged）、`POST /api/project/sync/finish`、`POST /api/project/sync/abort`、`POST /api/project/sync/ai-merge`（LLM 辅助合并，只返回不落盘） |
| 图谱 | `GET /api/graph?project=`（nodes/edges/categories；ref 边来自正文 md 链接、struct 边挂 wiki 目录节点、sem 边来自向量 top-3 近邻经动态阈值过滤） |
| 引导 | `GET /api/status`（agents[id/name/detected/hooksInstalled]、skillsInstalled、hooksTimeout、rxEnforceMode、disabled、app_version、home）、`POST /api/setup/hooks`（`{"agent":id}` 单装 / 缺省全装）、`POST /api/setup/hooks/remove`（单 agent 卸载）、`POST /api/setup/skills`、`POST /api/reasonix/enforce-mode`（mixed/soft/hard，落盘即生效） |
| 设置 | `POST /api/toggle`（全局开关）；`POST /api/hooks/timeout`（独立写 `[hooks] timeout_sec`，1~60，**不重装 hooks**）；`GET/POST /api/retrieve`（dedup_turns 跨轮冷却）；`GET/POST /api/capture`（沉淀 mode/turn_interval；`turn_interval:0`=保持不变）；`GET/POST /api/gate`（泛化门控开关+短语表）；`GET/POST /api/enforce/rules`（`[[enforce]]` 整体读写，type 仅 changelog、code_globs/message 非空，空数组=清空）——retrieve/capture/gate/enforce-rules 四族的 `project` 参数可选：**缺省读写全局 `config.toml`**（经 `resolveConfigTarget` 分流，读 `config.Load` 缺文件按默认值），显式传项目名保持项目级合并读、项目 config 写的旧行为；embedding：`GET /api/setup/embedding` + `profile`/`DELETE profile`/`active`/`test`/`download`（断点续传+sha256，前端 1s 轮询进度）/`download/cancel`（留 .part 续传）/`models-dir`/`open-models-dir`/`ollama-models`；LLM：`GET /api/llm` + `profile`/`delete`/`active`/`max-tokens`/`test` |
| 服务器 | `GET/PUT /api/server/config`（全局 `[server]` 段读写，token 空串保留旧值；空 URL 整段清空即 logout）、`POST /api/server/test`、`POST /api/server/login`、`GET /api/server/me`、`POST /api/server/change-password`、`POST /api/server/repos`（建仓一条龙）、`POST /api/server/pull`（新机器注册空壳项目 + clone 服务器仓）、`POST /api/server/credential/ensure`（本机 git 凭证 ensure）、管理类透传（users/orgs/repos/audit/tokens 列删，serverx 转发 okserver） |
| 更新 | `GET /api/update/check`（GitHub 最新版本，gui.json 缓存 6h fail-open；`?force=1` 绕过）、`POST /api/update/skip`（记录跳过版本）、`POST/GET /api/update/download`（安装器下载任务，.part 断点续传 + 轮询快照）、`POST /api/update/apply`（Windows 静默安装 + 升级熔断标记） |
| 日志 | `GET /api/logs?tail=&sig=`（ok/daemon/sidecar 三来源，行带 `src`/`semantic` 标记；sig 命中返回 `unchanged` 前端跳过重绘） |
| 其他 | `GET /api/export?project=`（zip）、`POST /api/import`（multipart 32MB，`Report{imported,skipped,projects}`）、`GET /api/changelog`（`current/pending/all`，pending 只算严格大于 last_seen 且不超过 current 的版本）、`POST /api/changelog/seen`（仅升级首弹关闭才标已读，写 `~/.okryptos/gui.json`）、`DELETE /api/project`、`GET /help.md`（静态） |
| 横切 | `POST /api/heartbeat?project=`（返回该项目 kb.db mtime 作 `version`；beats 通道生产传 nil——存活感知由 daemon.json 自省 + 托盘承担，新前端不再 5s 轮询）、`POST /api/shutdown`、`POST /api/uninstall`、`GET/POST /api/inject`（注入预算） |

前端 `web/`（零依赖原生 HTML/JS/CSS，无构建链；index.html 骨架 + app.js + style.css）：左右栏七菜单 + `location.hash` 路由（刷新恢复当前菜单）+ 中英切换（只翻界面 chrome 不翻数据）+ 昼夜 CSS 变量双主题 + 整页重渲保持各滚动容器 scrollTop。「管理」=项目→条目两级树（类型徽标/mandatory★/draft/归档置灰 + 标题过滤 + 计数）+ markdown 详情 + 右上操作组（编辑/批准/归档/删除）+ 行内同步状态点（五态：dirty 黄/syncing 黄闪/冲突红/落后/同步）与同步按钮 + 新建/编辑弹窗内 ✨优化（loading→对照预览→逐字段回填，409 弹「尚未配置模型」）；「图谱」=项目知识图谱（自绘 SVG 力导向引擎：中心双锚辐射布局、拖拽缩放、悬停邻居淡化、双击关联条目跳管理页定位；>400 条目自动分层模式——骨架（is_dir 或 deg≥6）常显 + 类目下钻，叶子不进 DOM 不参与物理；收敛后自动二次取景）；「引导」=agent 卡片（品牌字形 data-URI、未检测不渲染、安装/卸载双态、明细展开）+ Reasonix 强制检查三档卡 + Codex 信任门说明卡；「服务器」=okserver 连接三步向导（地址/登录/建仓）+ 管理/成员双视图（账号/仓库/凭证管理）+ 强制改密弹窗（全局 403 `must_change_password` 钩子）+ 服务器仓拉取（单拉/全部拉取/登录自动提示未注册仓）；「设置」=八卡（全局开关/语义检索/模型配置/Hook 超时/跨轮注入冷却/经验沉淀/泛化门控/规则配置；后四卡为全局配置，不带 project、无项目也可用），开关即存、简单输入行内保存改回原值变灰、弹窗确定生效闪 ✓；「日志」=深色控制台（来源 chips+仅语义+过滤，贴底滚动，2s 轮询 + sig 跳过重绘）；「其他」=导出/导入/更新日志/使用帮助/版本升级卡（检查/进度下载/一键安装/跳过版本）/删除项目知识库（备份+ack+输名三重解锁）/关于。冲突解决页（hash 路由子页）=卡片流 + 顶部钉住操作条 + 三向合并编辑器（marker 块采纳三栏）+ AI 合并。启动横切：升级后首次打开自动弹更新日志（pending 非空 → body 级弹窗，不进 render 周期）；新版本启动弹窗（升级/跳过/知道了）+ 侧栏红点；`/api/projects` 为空时落「引导」页（旧 GUI"无项目隐藏管理 tab"语义的等价形态）。daemon 被替换致 token 过期 401 时自动刷新一次页面取新 token（sessionStorage 标志防循环）。

### 5.12 backup — 知识库导出/导入（251 行）

GUI「其他」tab 背后的备份包（叶子包：stdlib zip + registry/entry/store/index）：

- `Export(w io.Writer, project string)`：registry.toml + 各项目 `knowledge/*.md` + `config.toml` 打成 zip；`project="all"` 全导，单项目时 registry 随之过滤
- `Import(r io.ReaderAt, size int64)`：`MaxSize` 32MB 上限、zip-slip 防护（拒绝 `..`/绝对路径）、只接受 `registry.toml`/`projects/<名>/knowledge/*.md`/`projects/<名>/config.toml` 三类路径；条目 .md 过 `entry.Parse` 失败计 skipped 不阻断；同名覆盖、缺失项目自动注册（同名已注册则合并进现有目录）；最后逐项目 `index.Sync` 重建索引
- 返回 `Report{imported, skipped, projects}`；客户端侧错误统一包 `ErrBadPackage`（HTTP 层映射 400）

### 5.13 version — 构建期注入的应用版本号（6 行）

`var Version = "dev"`；`scripts/build-dist.sh` 用 sed 从 `installer/okryptos.iss` 的 `#define AppVersion` 提取版本，经 `-ldflags -X okryptos/internal/version.Version=` 注入——版本事实源只有 .iss 一处，裸 `go build` 为 `dev`。经 `/api/status` 的 `app_version` 暴露给前端。四个 exe（ok/okd/okmanager/okdeploy）与 okserver 镜像共用同一注入机制。

### 5.14 syncx — 个人多端同步引擎（v2.23，git.go + repo.go + sync.go + status.go + conflict.go + init.go + credential.go，约 760 行）

在项目数据目录（`~/.okryptos/projects/<名>/`）里直接执行系统 git 的单机引擎——知识库即 git 仓，同步 = 普通 git 工作流，无自建协议。叶子包（仅 fsx + procx），cli/gui/daemon 三方共用：

- **git 执行器（git.go）**：参数数组调 `git -C dir`，不走 shell；env 固定 `GIT_TERMINAL_PROMPT=0`（防凭据提示挂起）、`LC_ALL=C`（防本地化输出影响解析）、`GIT_EDITOR=true`（防唤起编辑器）；`-c core.quotepath=false` 保中文路径可读。错误三分类：`ErrGitNotFound`（未装 git，同步禁用但本地功能不受影响）/ `ErrTimeout`（本地 10s、网络 60s）/ `*ExitError`（退出码 + 合并输出）
- **Repo 原语（repo.go）**：`IsRepo` 纯文件系统判断（.git 目录或 worktree gitfile 指针，不起子进程——/api/projects 对每项目都调，Windows 进程创建是管理页加载主要开销）；Status/CommitAll/Push/CloneToDir（同卷临时目录、仓级 autocrlf=false）
- **Sync 编排（sync.go）**：一次执行 = `add -A` → 有变更则 commit（身份内置 `-c user.name=Okryptos Sync`，无全局 git 身份的环境可跑）→（有远端先 fetch）`pull --rebase` → push。**冲突守卫**：rebase/merge 进行中（上次冲突未解决）直接报告未决冲突返回，不做任何提交/拉取——否则 `add -A` 会把冲突标记提交进历史；MERGE_HEAD 同样拦（init 情形 3 的手工 merge 中途）；pull 冲突 = 结构化结果（`Outcome.Conflicts`，err=nil）而非错误，停在半途等人解决并停止 push。`SyncOnce` 为 single-flight：同 dir 并发合并为一次执行，后到者拿同一结果（计数归执行者）
- **分层状态文件（status.go）**：`state/sync-status.json`（`layers.personal` = last_sync/ahead/behind/conflict/last_error，从第一天按层建模，团队层演进时平移）、`state/sync-conflict.json`（GUI 冲突页数据源，syncx 独占写）、`state/syncing`（同步进行中标记，stale 5 分钟守卫防进程死亡残留）
- **冲突解决原语（conflict.go，供 GUI 冲突页）**：pull --rebase 期间 stage 语义反转——`:1:`=base、`:2:`=远端、`:3:`=本地；对外只暴露用户语义（local=本机改动/remote=远端改动），严禁直译 git ours/theirs；ResolveFile 写解决结果并 add，Continue/Abort 走 rebase --continue/--abort
- **init 三情形（init.go，cli 与 gui 共用）**：无远端 = 仅本地历史；本地无内容 = 克隆远端；本地有内容 = init+commit+关联 remote+首推。两边各自初始化过且均有内容报 `ErrRemoteNotEmpty`（不自动合并，指引手工 `merge --allow-unrelated-histories` 一次）
- **凭据（credential.go）**：git 凭据写系统 credential helper；无 helper 时 approve 会静默丢弃（git 语义），显式返回 `ErrNoCredentialHelper` 由调用方回退 URL 内嵌。同步遇 git 认证失败自愈：`HasStoredCredential` 探测 → 经 serverx 自助重发 token（okserver `POST /api/v1/git-token`）→ 刷新凭据重试一次
- **触发面**：CLI `ok sync`；daemon 每分钟 ticker 检查按项目 `auto_interval_min` 到点触发 + 条目写入后 30s 防抖触发（均走 SyncOnce，见 6.5）；GUI 管理页同步按钮与冲突解决页（七端点，见 5.11）

### 5.15 oksrv / serverx / credmig — okserver 服务端管理面（v2.23~v2.24）

okserver 是独立的**服务端程序**（cmd/okserver，NAS/Docker 部署，Linux 形态，不进 Windows 安装包）：SQLite 存储 + bcrypt 认证 + Gitea GitBackend + 管理面 HTTP API。薄 main：env 配置（`OKSERVER_LISTEN` 默认 `:3100`、`OKSERVER_DATA_DIR`、`OKSERVER_GITEA_URL`/`OKSERVER_GITEA_ADMIN_TOKEN`）→ 存储 → root 首启（明文写 `<dataDir>/INITIAL_ROOT_PASSWORD` 0600 + 日志打印一次）→ HTTP 服务；`reset-root` 子命令供部署器远程重置 root 密码。

- **存储（store.go）**：users/sessions/orgs/org_members/repos/audit/meta 七表，时间列一律 TEXT 存 UTC RFC3339；`users.must_change_password` 列幂等迁移（初始/重置密码置位，重置时踢掉目标用户全部会话）
- **认证（auth.go）**：bcrypt + 会话 token + 登录限流
- **GitBackend（gitbackend.go + gitea.go + gitbackend_fake.go）**：接口隔离 Gitea admin API（建用户/建仓/发 token；建 token 带 `write:repository` scope，失败回滚不留半截），fake 实现供测试不碰真实 Gitea
- **HTTP API（http.go）**：`Bearer` 鉴权 + `admin` 角色门控 + **强制改密 gate**（带标记的会话只放行 me/change-password 白名单，其余 403 `must_change_password`）+ 审计随事实落盘。端点：login/me/change-password、`POST /api/v1/repos/personal`（建仓）、`POST /api/v1/git-token`（自助重发 git token——按机器分名 `ok-sync-r-<hostname>`，删本机同名旧 token 再建，多端互不吊销）、tokens 列删（自助 + 管理员任意用户）、users/orgs/repos/audit 管理面。无 cookie/静态页 → CSRF 结构免疫（LAN 服务不做 Origin/Host 白名单）
- **凭证统一（v2.24.2）**：git 凭证唯一发放通道 = `apiGitToken` 自助重发（建仓/建用户不再下发 token）；**每台机器一条** `ok-sync-r-<hostname>`。`internal/credmig` 负责本机迁移：daemon 同步周期发现已配服务器且未迁移（`~/.okryptos/cred-migrated.json` 标记）时先 ensure 本机新凭证、覆盖所有已绑定项目 remote 再同步（失败仅记日志下轮重试）；GUI 凭证页「清理旧版凭证」按钮同走此包
- **serverx（客户端，338 行叶子包）**：ok/okd/GUI 侧打 okserver 管理 API 的薄 Bearer 客户端（15s 超时，`Error{Code,Msg}` 透传状态码；`GitToken` 回显 token_name）；GUI `/api/server/*` 端点大部分为其转发 + 本地编排（建仓一条龙、拉取注册空壳项目 + clone、绑定管线共用 serveBindRepo）

### 5.16 deployx / okdeploy — 一键部署器（v2.24，okdeploy 独占）

把 okserver 经 SSH 一键部署到 NAS 的**独立程序**（cmd/okdeploy，不进客户端安装包，`dist/deploy/` 单独分发）：双击启动 → 监听 127.0.0.1 随机端口 → 内嵌 WebView2 窗口（不可用回退浏览器，与 GUI 共用 `gui.BrowserOptions` 形态）→ 四页前端（连接/探测/部署/管理，go:embed 内嵌 + 中英切换）。

- **Executor 接口（executor.go + ssh.go）**：流式执行/上传/下载三原语；`SSHClient` 用 `x/crypto/ssh` 实现（go.mod 已有依赖，零新增）；`LogHub` 收集日志并向订阅者广播（保留全量历史，迟到订阅者先补历史）
- **任务编排（task.go）**：Task = 步骤序列，掩码步骤的失败可附 stdout 尾（gitea CLI 日志走 stdout）；前端经本地 HTTP API + SSE 实时日志（api.go，token 鉴权）
- **探测/部署/管理（probe/deploy/manage/backup.go）**：远端环境探测（docker/compose 分两档、端口、已有 Gitea/已有部署，sudo 自动回退）；compose 模板 + .env 渲染双模式——`full`（全新部署：Gitea + okserver 两容器，Gitea 无人值守初始化，root 密码安全读取）/ `external`（接入已有 Gitea）；管理页任务 = 状态/升级（新版本检测 + 重拉当前版本）/容器日志/备份/恢复（停机一致性语义）/重置 root（`okserver reset-root`）/卸载（默认保留数据）
- **发布线**：`.github/workflows/docker.yml` 打 v* tag 构建 okserver 多架构镜像，GHCR + Docker Hub（`z7dream/okryptos-okserver`）双发；部署侧默认拉 Docker Hub，不可达时 `.env` 的 `OKSERVER_IMAGE` 可切 GHCR 全名；registry 未发布前优先用本地 `docker load` 的镜像（tag 白名单防线）；拉镜像 3 次重试 + 60min 超时（NAS 直连 Docker Hub 限速）

---

## 6. 核心业务架构

### 6.1 注入链路（知识 → AI 上下文）

```
用户发消息
  → kimi 触发 UserPromptSubmit
  → 执行 "ok.exe hook prompt"，stdin 喂事件 JSON
  → ok：开关检查 → 项目路由 → 打开 kb.db → 查询前增量同步（filename+mtime+size）
  → 首次提问？→ 输出 mandatory 全文 + INDEX.md（置 BaseInjected）
  → 每次提问 → embed 提问(失败降级) → index.Query 混合打分 → top-N 全文
  → TruncateToBudget 截断 → stdout
  → kimi 把 stdout 追加进模型上下文
```

**目标**：AI 在被问之前就已知项目约定与相关经验。基础注入每会话一次（内容随后存在于会话历史），检索注入每次都有。

### 6.2 强制链路（规则 → 机制保证）

```
AI 用 Write/Edit 改文件
  → PostToolUse → "ok.exe hook post-tool" → 文件记入 Session.Touched

AI 回合结束
  → Stop → "ok.exe hook stop" → EvalChangelog 判定
  → 命中：stderr 输出 message + exit 2 → kimi 要求 AI 继续执行（补日志）
  → 同会话同规则只阻断一次（BlockedRules 记忆）
  → AI 补写日志后 PostToolUse 记录，下次 Stop 自然放行
```

**目标**：把"每次代码修改必须立即记录变更日志"从靠 AI 自觉变为机制强制。

### 6.3 知识维护链路（人 → 知识库）

```
ok init [名字]   → registry 注册 + KB 骨架（项目名缺省取目录基名）+ 幂等写入/更新 hooks 配置
ok add --title … → 写条目 → 同步索引库（INDEX.md + 有 key 则为变化条目算向量）
手工编辑条目     → 下次提问时 hook 查询前自动增量同步；或 ok index 手动同步
ok search <词>   → 命令行预览检索效果（调试注入质量）
```

### 6.4 首次引导（ok setup）

```
ok setup [--agent <id>]
  → os.Executable 取自身绝对路径（hooks 命令不依赖 PATH）
  → 对目标 agent 写入 hooks 集成：缺省 = 全部已检测 agent（agentx.Detected()）；
    --agent 指定单个（未知 id 报错并列出可用 id；未检测到该 agent 也写入并提示）；
    一个都未检测到时跳过 hooks 写入继续后续步骤
      kimi：备份 ~/.kimi-code/config.toml → 标记块幂等写入 3 条 hook
      pi：渲染 TS 扩展写入 ~/.pi/agent/extensions/okryptos.ts（既有非本工具文件先备份）
  → 安装 ok-propose / ok-wiki 两个技能到各 agent 技能目录（烧入 exe 路径）
  → 交互（或 flags）收集 embedding：三选一（线上 OpenAI 兼容 / Ollama / 内置本地模型，
      内置含清单选择与镜像下载进度）→ 写全局 ~/.okryptos/config.toml（0600）→ 立即连通性验证
  → 打印引导
```

幂等性由"先清除存量 ok hooks + 标记块原位替换"保证：写入前 `StripLegacyOKHooks` 移除所有指向 ok hook 的无标记 `[[hooks]]` 表（历史手动粘贴遗留），重复执行或更换 exe 路径只覆盖更新、绝不重复堆积；标记损坏（有头无尾）时报错拒绝修改，不破坏用户配置。`ok init` 复用同一写入逻辑（`writeHooks`，写失败不阻断注册）；卸载遍历 `agentx.All()` 逐 agent `RemoveHooks`（kimi 清除标记块与无标记存量，pi 只删本工具生成的扩展文件）。daemon 化后 `ok gui` 不再阻塞，GUI 页面关闭不退出进程（原 30s 心跳看门狗已随 daemon 化移除）。

### 6.5 常驻 daemon（单进程架构）

全系统只有一个 okd.exe 常驻进程（cmd/okd，gui-split 后 GUI 子系统从 ok.exe 分离），承载配置中心与 hook 请求：

- internal/daemonx（叶子包）：daemon.json 凭证（pid/port/token/exe指纹）、健康检查、版本判定
- internal/daemon（编排包）：HTTP mux（/api/health、/api/hook/* + gui.Handler）、Run（端口即单实例锁，
  默认 17888，占用回退随机）、spawn（DETACHED 后台拉起，15s 防抖）、ForwardHook（瘦客户端转发，
  9s 超时 fail-open）、OpenGUI（ensure + 开浏览器即退；窗口最大化为同步调用——开浏览器即退的进程里协程会随之死亡）
- exe 指纹 = 路径|size|mtime：exe 升级后客户端发现指纹不一致 → 旧 daemon shutdown → 拉起新版
- hook 兜底：daemon 不在时本次请求本地直接处理（hook.Handle* 原逻辑），同时后台拉起 daemon
- 安装器写 HKCU Run 登录自启；卸载/ok daemon stop/setupx.Uninstall 均可停 daemon
- kb.db 所有写入收敛到 daemon 单进程；index.Open 另加 busy_timeout(3000) 兜底短暂并发
- 系统托盘（internal/tray）内嵌 daemon 进程：右下角图标，单击弹菜单（版本号 + 检查更新 + 退出）、双击打开/聚焦唯一 GUI 窗口；"检查更新"打开 GUI 直达版本卡（内含窗口长轮询，异步派发避免卡死消息线程）；菜单"退出"与 `ok daemon stop` 同走 /api/shutdown 链路
- embedding sidecar janitor（10s 周期调和）：active=内置且模型就绪 → 拉起/保持 llama-server；空闲 10 分钟回收、崩溃有界重启 ×3、切换/停用即回收、daemon 退出兜底回收（实现见 5.5/17.4）
- 同步 janitor（internal/daemon/sync.go）：每分钟 ticker 遍历注册项目，启用 `[sync]` 且到点（`auto_interval_min`）的跑 `syncx.SyncOnce`（`auto_interval_min=0` 关闭全部自动触发）；条目写入（approve、GUI 编辑保存等）经 `NotifyWrite` 30s 防抖后对启用同步的项目 force 跑一轮——未同步窗口压到分钟级。两路并发经 syncCycleMu 串行化（RecordOutcome 对状态文件的读-改-写不加锁存在 lost-update 窗口）。每轮先做凭证统一迁移（credmig，已配服务器且未迁移时换新 `ok-sync-r-<hostname>` 凭证，失败仅记日志下轮重试）。失败仅记 daemon 日志，绝不影响本地链路
- 升级熔断：`~/.okryptos/update/.upgrading` 存在期间 `daemon.Ensure/EnsureCurrent` 一律不拉起 daemon（升级安装期间 hook/托盘的拉起被挡住）；okd 启动时自愈删除残留标记（升级收尾/异常中断兜底，见 6.9）

### 6.6 wiki（项目 wiki 的生成驱动与落后提醒）

```
ok-wiki 技能（AI 驱动）
  → 扫描项目，ok add --type reference --tags wiki 写 wiki 条目（直接转正，参与检索）
  → ok wiki mark：游标按当前分支写入 state/wiki.json（cursors[branch] = last_commit + generated_at + entry_count）

ok wiki status
  → git rev-list --count <游标>..HEAD 得落后计数（无游标按全历史；git 不可用 behind=-1）
  → 与 [wiki] stale_commits 阈值（默认 20，0=关闭）比较得出 stale
  → 输出附 branch/base_branch/branch_state（ok/inherited/no_cursor/diverged/gone/legacy_orphan；inherited = 读时继承可达游标）

ok wiki base [分支名]
  → 无参查看基准分支；带参设置并落盘

index.Sync 重建 INDEX.md
  → 追加「## Wiki 目录」节（tags LIKE '%wiki%' 且 draft=0，按 title 排序，描述取 summary）
  → 无 wiki 条目时省略该节（输出与之前逐字节一致）

hook prompt（基础注入之后）
  → wikiContextLine：非基准分支且有 wiki 注入时，输出开头附一行 wiki 出处上下文
    （"wiki 基于 master@…；当前分支 dev"，分叉时另附分叉点）
  → wikiNudge：stale 且本会话未提示过（session.WikiNudged）→ 输出末尾追加 nudge
  → 从未生成：建议用 ok-wiki 技能生成 wiki；已生成：提示落后 N 个 commit
  → 游标失效（gone）/旧游标归属存疑（legacy_orphan）显式提示，不受 stale_commits 阈值门控
  → 每会话最多一次；非 git 项目/git 不可用 fail-open 静默
```

**分支感知（v2.6.0）**：wiki 游标按分支记录（`state/wiki.json`：`base_branch` + `cursors` 表，旧单游标格式读取时按 merge-base 可达性惰性迁移，不可达报疑不归错）；CheckStatus 三态检测（分叉/无基线/失效），非基准分支注入附一行 wiki 出处上下文；`ok wiki base` 查看/设置基准分支。分支差异条目属二期。CheckStatus 只读 git 与游标文件、绝不写盘——迁移落盘只发生在 mark/base 写入路径；基准分支上的行为与旧版完全一致。

**分支差异条目（v2.7.0，二期）**：长期并行分支只维护与基准的结构 delta（tags 含 `branch:<名>`）；注入按当前分支过滤（含 INDEX 差异小节裁剪，分支未知不过滤）；`ok wiki diff` 给技能供结构变化素材，非基准分支只写差异条目（写侧防呆）；基准分支检测 merged_branches 提示清理（status 输出 + prompt 每会话一次 nudge）；GUI 管理页分支列+过滤器+sticky 操作列；CheckStatus git 调用收敛为 merge-base 判别。无 `branch:` 标签条目的项目行为与旧版完全一致。

**合并谱系落盘（v2.8.0）**：`ok wiki status`/`mark` 在基准分支检出"tip 已并入且有差异条目"的分支时，向 wiki.json `merges` 数组追加合并谱系 `{from, to, commit, time}`——from+commit 判重（重复检出不重复记录），to 取基准分支；检出/落盘失败 fail-open 仅记日志，不影响 status/mark 主流程。GUI 管理页显示谱系行（"dev → master"）与 born 徽标，工具条显示"基准分支 · 当前分支"上下文、不一致时警示；`[provenance] auto_born` 由 GUI 沉淀卡 checkbox 或手改配置控制（写盘复用 SetCapture 同款小节替换，其余内容原样保留）。

**GUI 打磨与项目删除（v2.8.1~v2.9.0）**：表头/类型显示中文化（存储值保持英文）；摘要列 `line-clamp:2` 两行截断 + 单例浮窗跟随鼠标（溢出视口自动翻转、滚动收起）；项目列表接口附 `last_update`（kb.db mtime）降序，打开默认选中最近写入的项目；「刷新」从只重拉条目改为全量 `refreshStatus` + 三态反馈；v2.9.0 落地「删除项目知识库」——GUI 三重确认（影响面计数 + 默认勾 zip 备份 + 勾选/输名解锁）+ 后端先注销后删目录，删除当前选中项目时前端先清 `state.project` 再刷新，避免 capture 接口 404 误报。

**目标**：wiki 由 AI 技能生成、但"该不该更新"由机制提醒——游标 + 阈值把 wiki 新鲜度变成可检查的状态，提示复用现有 prompt 注入通道，不增加新 hook。

### 6.7 个人多端同步链路（v2.23）

```
ok sync init [remote-url]（或 GUI 管理页建仓一条龙）
  → 三情形编排（syncx.InitForSync）：仅本地历史 / 克隆远端 / 首推
  → 写项目 config.toml [sync]（enabled/remote/auto_interval_min/llm_assist，随仓同步）

日常触发（三路均收敛到 syncx.SyncOnce single-flight）
  → ok sync（手动）
  → okd 同步 ticker：每分钟检查，按项目 auto_interval_min 到点触发
  → 写入防抖：approve/GUI 保存后 30s 防抖 force 一轮

一次同步 = add -A → commit（身份内置）→ fetch → pull --rebase → push
  → 冲突：停在 rebase 半途，Outcome.Conflicts 非空、err=nil，停止 push
  → state/sync-conflict.json 落冲突清单 → GUI 冲突解决页（卡片流/三向合并/AI 合并）
  → resolve → finish（rebase --continue + push）或 abort 回到同步前
```

**目标**：知识库即 git 仓，多端同步复用成熟 git 语义；冲突不是错误而是等人解决的状态；所有自动触发失败仅记日志，绝不影响本地链路。git 认证失败自愈：探测本机凭据缺失 → 经 serverx 向 okserver 自助重发 token（`ok-sync-r-<hostname>`）→ 刷新凭据重试一次。

### 6.8 okserver 服务端与空壳项目（v2.23~v2.24）

```
GUI 服务器页（三步向导：地址 → 登录 → 建仓）
  → [server] 段（全局 config.toml：url/username/token）+ serverx 客户端
  → 建仓一条龙：okserver 建个人仓（Gitea）→ 本地 sync init + 绑定 + 首推

新机器接入
  → 登录后自动提示本机未注册的服务器仓 → 一次确认批量绑定
  → POST /api/server/pull：注册空壳项目（无 paths）+ clone 服务器仓到 KB 目录

空壳项目（服务器拉取/备份恢复产生：注册表有条目但无工作目录关联）
  → ok init 同名项目幂等补挂工作目录（registry.AddPath，幂等/冲突哨兵）
  → GUI 行内「关联目录」按钮 / 一键批量关联；挂错可 detach 解除（RemovePath）

强制改密（v2.24.0）
  → 初始/重置密码置 must_change_password（重置时踢掉目标用户全部会话）
  → 服务端 gate 中间件拦截已认证端点（me/change-password 白名单除外，403）
  → GUI 全局 403 must_change_password 钩子 → 强制改密弹窗
```

### 6.9 客户端版本升级链路（v2.24.3）

```
检查：GET /api/update/check（gui.json 缓存 6h，fail-open；启动自动检查走缓存，
      手动「检查更新」/进其他页带 ?force=1 真查；GitHub 不可达如实报 error）
提示：新版本 → 启动弹窗（升级/跳过/知道了）+ 侧栏红点 + 其他页版本卡；
      服务器页另检测服务端版本高于客户端时提示升级；托盘菜单「检查更新」直达版本卡
下载：POST /api/update/download 任务化（.part 断点续传，Range/206，200 降级整下），
      前端轮询 GET 快照进度
安装：POST /api/update/apply（Windows）
      → 先写升级熔断 ~/.okryptos/update/.upgrading（200 之前写好，写失败 500 中止）
      → goroutine detached 拉起安装器 /VERYSILENT /SUPPRESSMSGBOXES /NORESTART 后 okd 自退
      → 安装期间 hook/托盘的 daemon 拉起全被熔断挡住（daemon.Ensure 检查标记）
      → 安装器 [Run] 段收尾拉起新 okd；okd 启动自愈删除残留熔断标记
      → 升级成功 5s 后前端自动 reload
跳过：POST /api/update/skip 记 gui.json SkippedVersion，该版本不再弹窗/红点
```

apply 序列刻意移出 okd 进程（安装器收尾 + 自愈兜底），并发防护防重复安装；安装器没起来而熔断已写时回滚熔断 + 复位 guard，避免永久锁死。

---

## 7. 数据流与事件流

### 7.1 一次提问的完整时序

```
用户        kimi                ok.exe hook prompt         存储
 │ 提问      │                       │                      │
 │──────────►│ UserPromptSubmit     │                      │
 │           │────stdin JSON───────►│                      │
 │           │                      │──读 registry.toml────►│
 │           │                      │──打开 kb.db 并 Sync───►│
 │           │                      │──(首次)读 INDEX.md────►│
 │           │                      │──(可选)embedding API──►│ (OpenAI 兼容服务)
 │           │                      │──Query/截断            │
 │           │◄──stdout 注入文本────│                      │
 │           │──追加进上下文────────►│                      │
 │           │───────调用 LLM──────►│                      │
```

### 7.2 会话生命周期中的状态

```
会话开始(首次提问)          会话中                    会话结束(每次回合末)
     │                       │                          │
  state.Clean(7天)      Touched 累积                enforce 判定
  BaseInjected=true     (Write|Edit 触发)           BlockedRules 记忆
     │                       │                          │
     └────────► state/session-<id>.json ◄─────────────┘
```

状态文件按 session_id 隔离，超过 7 天在下次首次提问时清理。hook 不持有任何内存状态——每次触发都是独立进程，全部状态在磁盘。

---

## 8. 存储层

集中存储于 `~/.okryptos/`（`OK_HOME` 可覆盖，测试靠它隔离）：

```
~/.okryptos/
├── ok.log                  # hook/CLI/GUI 侧错误与优化日志（fail-open 的唯一痕迹）
├── daemon.log              # daemon（okd）输出日志
├── logs/                   # 大小轮替归档（R3 接入）：ok/daemon/sidecar 日志超 8MB 时在
│                           #   句柄释放窗口改名归档为 <名>-<日期>_<时分秒>.log，保留 7 天
│                           #   （CleanArchives 在轮替发生与 daemon 启动时清理；Windows 上被
│                           #   占用的文件改不了名，失败跳过本轮 fail-open）
├── registry.toml           # 项目注册表：[[project]] name + paths
├── config.toml             # 全局配置：[[embedding.profiles]]/inject/retrieve 默认值/[server] 段
├── hooks-disabled          # 全局开关标志文件（存在即全部静默）
├── gui.json                # GUI 持久化小状态：last_seen_version / update_check（6h 缓存）/ skipped_version
├── gui-state.json          # 内嵌窗口状态（maximized + normal 矩形，机器本地不进同步面）
├── cred-migrated.json      # 凭证统一迁移标记（username/hostname/token_name/at，credmig）
├── update/.upgrading       # 升级熔断标记（存在期间 daemon.Ensure 不拉起 okd；okd 启动自愈删除）
├── models/                 # 内置 embedding 模型旧默认位置（GGUF，约 146MB–639MB/档，sha256 钉死校验）；现默认 <安装目录>/models（[embedding] models_dir 可配）
├── embed-sidecar.json      # 内置 sidecar 状态（pid/port/model_id/last_used；hook/cli 只读发现）
├── embed-sidecar.want      # want 拉起标记（hook/cli 写，daemon 调和时见到拉起后清除）
├── embed-sidecar.log       # llama-server stdout/stderr
└── projects/<项目名>/
    ├── config.toml         # 项目配置（覆盖全局；[[enforce]] 仅这里有）
    ├── knowledge/*.md      # 知识条目（一文件一条，frontmatter + 正文）
    ├── INDEX.md            # 机器生成的轻量索引（标题+类型+tags+摘要，由 index.Sync 重建）
    ├── kb.db               # SQLite 索引库：entries（原文）+ entries_fts（FTS5）+ vectors（向量 blob）+ meta（embedding_model/embedding_dim 身份）
    └── state/
        ├── session-*.json  # 会话状态（Touched/BlockedRules/BaseInjected/WikiNudged，超 7 天 GC）
        ├── wiki.json       # wiki 游标（base_branch + cursors 按分支记录 last_commit/generated_at/entry_count + merges 合并谱系数组，旧单游标格式读取时惰性迁移；固定文件名，不受 session 7 天 GC 影响）
        ├── sync-status.json   # 分层同步状态（layers.personal = last_sync/ahead/behind/conflict/last_error）
        ├── sync-conflict.json # 当前冲突文件清单（GUI 冲突页数据源，syncx 独占写）
        └── syncing            # 同步进行中标记（RFC3339 时间戳；stale 5 分钟视为进程死亡残留忽略）
```

**写入纪律**：INDEX.md 与 kb.db 由工具维护，不手改；knowledge/ 是人工维护区；config.toml 项目级手写（模板含注释示例）。旧版 vectors.json 首次打开 kb.db 时自动导入并改名为 `.bak`。

**一致性策略**：全部 JSON/TOML 读取对"文件不存在"宽容（视为空）；损坏文件按层处理——entry 解析失败在 `index.Sync` 中跳过单文件（已索引旧行保留，提交后返回 `*CorruptEntriesError` 警告）；state 损坏回退空状态（最坏情况是重复阻断一次，fail-safe 方向正确）；kb.db 损坏/打开失败时 hook 记 ok.log 后静默放行（fail-open）。

---

## 9. 外部集成

### 9.1 Kimi Code hooks（0.28.1 实测校准）

| 事件 | ok 子命令 | 载荷关键字段（实测） | 作用 |
|------|-----------|---------------------|------|
| `UserPromptSubmit` | `ok hook prompt` | `prompt` 是**内容块数组** `[{"type":"text","text":"…"}]` | stdout 追加进上下文 |
| `PostToolUse`（matcher `Write\|Edit`） | `ok hook post-tool` | `tool_input.path`（**不是** `file_path`） | 记录触碰文件 |
| `Stop` | `ok hook stop` | — | exit 2 阻断，stderr 为原因 |

关键实测结论（记录在规格附录 A）：**SessionStart 的 stdout 不进入上下文**（观察型事件），因此基础注入放在首次 UserPromptSubmit；Windows 上 hook 命令由系统 shell 执行，`sh -c` 不可用，绝对路径 exe 可用。

### 9.2 多 agent 抽象（agentx）

`internal/agentx` 把"AI 编码 agent 的 hook 集成"抽象为适配器注册表：CLI（`ok setup`/`ok init`）、GUI API 与 hook 入口自愈统一经注册表驱动；新增 agent = 实现 `Agent` 接口并在适配器文件的 `init` 中 `Register`。

```go
type Agent interface {
    ID() string                   // 稳定标识："kimi" / "pi"，CLI/GUI/API 统一使用
    DisplayName() string          // 展示名："Kimi Code" / "Pi"
    Detect() bool                 // 本机是否已安装该 agent
    HooksInstalled() bool         // hooks 集成是否已安装且为当前版本
    InstallHooks(exe string) error
    RemoveHooks() (bool, error)   // 返回是否真的移除了内容
    EnsureHooks(exe string) error // hook 入口自愈；错误由调用方 fail-open 处理
    HooksTarget() string          // hook 写入目标的展示路径
    SkillsDir() string            // 技能目录（kimi/pi/reasonix/opencode/codex/dsh 共享 SkillsHome；zcode 为 ~/.zcode/skills；claude 为 ~/.claude/skills；qoder 为 ~/.qoder-cn/skills；qoder-ide 为 ~/.lingma/skills）
}
```

注册表：`Register` / `All` / `Find(id)` / `Detected()`（本机已安装的 agent）。技能安装目标为**已检测 agent 的 SkillsDir 并集**（`setupx.SkillDirs()`，kimi/pi/reasonix/opencode/codex/dsh 共享 `SkillsHome()`（`OK_SKILLS_HOME` 优先，默认 `~/.agents/skills`），zcode 独立目录（`~/.zcode/skills`），claude 独立目录（`~/.claude/skills`），qoder 独立目录（`~/.qoder-cn/skills`），qoder-ide 独立目录（`~/.lingma/skills`））；卸载按全部注册 agent 的并集清理（`setupx.AllSkillDirs()`）。

十种注入形态：

| agent | 注入形态 | 写入目标 | "已安装且为当前版本"判定 |
|-------|----------|----------|--------------------------|
| kimi | TOML 标记块（3 条 `[[hooks]]`） | `~/.kimi-code/config.toml`（`KIMI_CODE_HOME` 优先） | 标记块 `# >>> okryptos hooks >>>` 存在 |
| pi | TypeScript 扩展（三事件回调） | `~/.pi/agent/extensions/okryptos.ts`（`PI_CODING_AGENT_DIR` 优先） | 头标记 + `// fingerprint:` 行与当前模板指纹一致 |
| zcode | 合并写 JSON 配置（`hooks.events` 三事件，`type:"process"`） | `~/.zcode/cli/config.json`（`OK_ZCODE_HOME` 优先，ok 自留测试口） | 三事件的 ok hook 均为当前 exe + `claude` 参数 + 当前 timeoutMs |
| reasonix | Extension Protocol 插件包（manifest v1 + 信任门登记） | `<reasonix home>/plugins/okryptos/reasonix-plugin.json` + `<reasonix home>/plugin-packages.json`（`OK_REASONIX_HOME`/`REASONIX_HOME` 优先） | 登记条目 enabled/root 正确且 manifest command/args 为当前 exe |
| opencode | TypeScript 插件（三钩子：`chat.message` / `tool.execute.after` / `event: session.idle`） | `~/.config/opencode/plugins/okryptos.ts`（`OK_OPENCODE_HOME` 优先，ok 自留测试口；`OPENCODE_CONFIG_DIR` / `XDG_CONFIG_HOME` 次之） | 头标记 + `// fingerprint:` 行与当前模板指纹一致 |
| claude | 合并写 JSON 配置（`hooks` 三事件组，`type:"command"` shell 串） | `~/.claude/settings.json`（`OK_CLAUDE_HOME` 优先，ok 自留测试口） | 三事件的 ok hook 均为当前 exe + `claude` 参数 + 当前 timeout（秒） |
| codex | 合并写 JSON 配置（hooks.json 三事件组，Windows 为 .cmd 包装裸路径，其他平台 quoted shell 串）+ config.toml 特性开关与信任记录 | `~/.codex/hooks.json`（`OK_CODEX_HOME` 优先，ok 自留测试口；`CODEX_HOME` 次之） | 三事件的 ok hook 均为当前 exe + `claude` 参数 + 当前 timeout（秒） |
| qoder | 合并写 JSON 配置（settings.json `hooks` 三事件组，Windows 为 .cmd 包装裸路径，其他平台 quoted shell 串）+ 顶层 `hooksConfig.enabled` 开关 | `~/.qoder-cn/settings.json`（`OK_QODER_HOME` 优先，ok 自留测试口；`QODERCN_CONFIG_DIR` 次之） | 三事件的 ok hook 均为当前 exe + `claude` 参数 + 当前 timeout（秒）+ `hooksConfig.enabled` 为 true |
| qoder-ide | 合并写 JSON 配置（settings.json `hooks` 三事件组，Windows 为 .cmd 包装裸路径，其他平台 quoted shell 串；无 enabled 门） | `~/.lingma/settings.json`（`OK_QODER_IDE_HOME` 优先，ok 自留测试口） | 三事件的 ok hook 均为当前 exe + `claude` 参数 + 当前 timeout（秒） |
| dsh | 本地 JS 插件（家目录 `cordis.patch.yml` 绝对路径挂载） | `<dsh home>/plugins/okryptos/index.js` + `<dsh home>/cordis.patch.yml` 标记块（`OK_DSH_HOME` 优先，ok 自留测试口；`DSH_HOME` 次之） | 插件头标记 + `// fingerprint:` 行与当前模板指纹一致、内容等于当前 exe 渲染，且 patch 含 `id: ok-hooks` |

zcode 适配器（`zcode.go`）：ZCode 的 hook 输入契约是 Claude 风格 snake_case，与 `hook.ParseEvent` 天然兼容；但**输出侧要求 stdout 为协议 JSON**（纯文本只当诊断不进上下文），故 hook 命令带第三参数 `claude`——`HandlePrompt` 把注入包成 `{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":...}}`，`HandleStop` 阻断改写 stdout `{"decision":"block","reason":...}` + exit 0（kimi/pi 的 stderr + exit 2 语义不变）；daemon 转发经 `?format=` query 透传。配置合并写保留未知字段与用户自有 hook（ok 条目按 `args:["hook",<事件>,...]` 识别，与 exe 路径无关），写前备份 `.bak-openknowledge`；`hooks.enabled` 置 true（ZCode 要求显式开启）。自愈语义：曾装过且内容过期才重写，从未安装不复活。技能进 `~/.zcode/skills`（ZCode 不自动读 `~/.agents/skills`）。

pi 扩展由内嵌模板 `pi_extension.ts`（`go:embed`）渲染：`{{EXE}}` 占位替换为 ok 绝对路径，文件头写头标记（`// okryptos hooks (managed by ok.exe; do not edit)`）与指纹行（指纹 = 模板内容 sha256 前 12 位十六进制，随模板升级变化）。安装时若目标已存在**非本工具生成**的同名文件，先备份为 `.bak-openknowledge`（备份失败则中止安装）；`RemoveHooks` 只删本工具生成的文件，非本工具文件不动。扩展对 ok 的调用全部 fail-open（超时/异常静默），不拖累 pi 会话。

claude 适配器（`claude.go`）：覆盖 Claude Code 本体与 CodePilot 等 claude-agent-sdk 兼容宿主——它们经 `settingSources` user 层加载 `~/.claude/settings.json`（CodePilot 实测 UserPromptSubmit/Stop 原生执行；其 provider 隔离的 shadow HOME 只剥 `ANTHROPIC_*` env 键，hooks 原样继承）。配置为 Claude Code 原生结构（`hooks.<事件>` 组数组，无 enabled 开关），hook 命令是 **shell 字符串**（正斜杠 exe + 双引号包裹，cmd.exe 与 bash 均可执行）而非 zcode 的 process+args；输出协议与 zcode 相同（args 末尾 `claude`，hook.go 零改动）。ok 条目按**命令串后缀**（` hook <prompt|post-tool|stop> claude`）识别，不看 exe basename；合并写纪律同 zcode（写前 `.bak-openknowledge` 备份、第三方条目保留、损坏文件不覆盖、map 合并写 key 重排代价可接受）。`Detect()` 看 `~/.claude` 或 `~/.codepilot`（`OK_CODEPILOT_HOME` 测试口，`CLAUDE_GUI_DATA_DIR` 次之）——后者覆盖只装 CodePilot 的机器。自愈语义不变：曾装过且过期才重写，从未安装不复活。注意 hook 子进程在 shadow HOME 下运行时 `ClaudeHome()` 跟随重定向（自愈最坏只写 shadow 副本，被宿主清理，真实配置无风险），而数据根解析是免疫的（见 §5.1 `registry.Home()`）。

codex 适配器（`codex.go`）：Codex 的 hook 契约逐字兼容 Claude Code（官方文档核实）——stdin JSON 同字段、注入走 `hookSpecificOutput.additionalContext`、Stop 阻断 `decision:block`，故 hook 命令继续以 `claude` 为输出协议参数，`hook.go` 输出层零改动；唯一新逻辑在输入侧——Codex 写盘走 `apply_patch`（`tool_input.command` 载补丁文本，无 Write/Edit），`Event.PatchPaths()` 解析 `*** Add File:` / `*** Update File:` / `*** Delete File:` / `*** Move to:` 头标记，`HandlePostTool` 合并 `FilePath()` 与 `PatchPaths()` 多路径记录（补丁路径相对会话 cwd，先 join 再 relativize），auto 自省与 enforce 规则与 Claude 同档。配置目标为用户层独立 `~/.codex/hooks.json`（官方建议每层一种机制，config.toml 仅行级手术写入 `[features] codex_hooks = true`（0.118 起 hooks 为 under-development 特性、默认关闭，不开则全部 hooks 静默不派发——备份+其余内容逐字节保留；`HooksInstalled` 判定与自愈均纳入开关状态）；合并写纪律、后缀识别、备份与自愈语义均同 claude）。Codex hook 命令在 Windows 为 `ok-hook-*.cmd` 包装文件裸路径（规避上游 #38168 外层引号静默不执行 bug；hooks.json 与 exe 解耦，迁移自愈不再破信任），非 Windows 维持引号 shell 串；安装/自愈自动确保 `[features] codex_hooks = true`（0.118 起实验特性默认关闭）并写入 hooks.state 信任记录（归一化身份 canonical JSON 的 SHA-256，PostToolUse 含 matcher、prompt/stop 不含——经 codex 源码与本机记录双向验证），内容变更不再被静默跳过；卸载清理包装文件与信任节。版本矩阵实证：exec 不派发 hooks；桌面端 26.707 与 CLI 0.147.0 在三修复（特性+信任+包装）后均实证可用——早期「26.707 不派发」判断系信任过期静默跳过的误判。PostToolUse matcher 只追 `apply_patch` 不追 `Bash`（与 claude 不追 Bash 对齐）。技能零适配：Codex 原生扫描 USER 作用域 `~/.agents/skills`（共享 `SkillsHome()`）。`CodexHome()` 优先级：`OK_CODEX_HOME` > `CODEX_HOME`（官方重定位）> `~/.codex`。

qoder 适配器（`qoder.go`）：Qoder CN CLI 的 hook 契约逐字兼容 Claude Code（官方 hooks-reference 文档 + `@qodercn-ai/qoderclicn` 1.1.20 bundle 源码双向核实）——settings.json `hooks` 分组同构、command 类型 shell 串、stdin JSON 同字段、退出码 0/2、注入走 `hookSpecificOutput.additionalContext`、Stop 阻断 `decision:block`，hook 命令以 `claude` 为输出协议参数，`hook.go` 输出层零改动；PostToolUse matcher 追 `Write|Edit`（Qoder 与 Claude 同款写盘工具），与 claude 不追 Bash 对齐。两个 Qoder 专属点：①顶层 `hooksConfig.enabled` 默认关闭（bundle 实证 `enableHooks = !disableAllHooks && hooksConfig.enabled`，settings schema 默认 `{}` → enabled 未定义 → false）——安装/自愈必须置 true 否则 hooks 静默不派发（codex 特性开关同款教训），`HooksInstalled` 与自愈均纳入开关状态，卸载不关闭（关掉会连带停掉用户第三方 hooks）；`disableAllHooks` 是用户全局 kill switch，ok 不读取不修改。②Windows 上 command 型 hook 经 `cmd.exe /d /s /c` 执行，`/s` 会剥首尾引号、quoted 命令串静默不执行（codex #38168 同源问题）——command 用 `ok-hook-*.cmd` 包装文件裸路径，与 exe 解耦（迁移自愈只改包装内容，settings.json 逐字节不动）。合并写纪律、后缀识别、备份与自愈语义均同 claude。技能零适配：Qoder 用户级技能目录为 `<配置目录>/skills`（bundle 源码 `getUserSkillsDir`），SKILL.md 格式与 Claude 逐字一致（本机 bundled skills 实证），共享模板直接分发。`QoderHome()` 优先级：`OK_QODER_HOME` > `QODERCN_CONFIG_DIR`（官方重定位）> `~/.qoder-cn`。**覆盖范围仅终端 CLI**：QoderCN IDE（通义灵码内核）是另一套 hooks 实现（读 `~/.lingma/settings.json`），由独立适配器覆盖（见下）。

qoder-ide 适配器（`qoderide.go`）：Qoder CN IDE（通义灵码内核）的 hooks 契约与 CLI 同构（官方 IDE hooks 文档核实：settings.json `hooks` 分组、command 类型、stdin JSON、退出码 0/2、stdout JSON continue/stopReason/suppressOutput/hookSpecificOutput），但能力降级——仅 5 事件、**Stop 与 PostToolUse 不可阻断**（enforce/auto 自省在 IDE 上不走通：Stop 的 decision:block 输出被忽略、静默放行）、无 enabled 门、改配置需重启 IDE 生效（无热加载）。配置目标 `~/.lingma/settings.json`（IDE 无目录重定位环境变量，`OK_QODER_IDE_HOME` 测试口 > `~/.lingma`）；三事件与 claude 同款（UserPromptSubmit `*` / PostToolUse `Write|Edit` / Stop `*`），PostToolUse matcher 无空格形态在"| 拆分"与"正则"两种 IDE 匹配语义下均正确；IDE 工具名双套（原生 run_in_terminal/create_file/search_replace ↔ 兼容 Bash/Write/Edit 运行时映射，matcher 两套都认）。Windows 命令为 `ok-hook-*.cmd` 包装裸路径（IDE 执行模型未文档化，裸路径在 cmd 外壳语义下最稳；与 qoder CLI 的包装同名不同目录），exe 迁移自愈只改包装内容。技能目录 `~/.lingma/skills`（官方 IDE 文档），共享模板零适配。合并写纪律、后缀识别、备份与自愈语义均同 claude。

pi 事件 → ok hook 映射：

| pi 事件 | ok 子命令 | 对应 kimi 事件 | 语义差异 |
|---------|-----------|----------------|----------|
| `before_agent_start` | `ok hook prompt` | `UserPromptSubmit` | stdout 非空时以 `display:false` 自定义消息注入上下文 |
| `tool_result`（toolName = `write`/`edit`） | `ok hook post-tool` | `PostToolUse`（matcher `Write\|Edit`） | 无 |
| `agent_settled` | `ok hook stop` | `Stop` | pi 无法阻断已结束的回合：ok 以 exit 2 + stderr 表达"阻断"时，扩展改为 `sendMessage({content: stderr}, {triggerTurn: true})` 把提示注入会话，驱动 agent 当场完成自省/补日志 |

reasonix 适配器（`reasonix.go`）：不写 settings.json hook（其 UserPromptSubmit 不注入 stdout），改为安装 Extension Protocol 插件包——`plugins/okryptos/reasonix-plugin.json`（runtime.command 直指 ok.exe，`args=["extension-serve"]`，`required=false`，sidecar 崩溃宿主降级不阻断）+ `plugin-packages.json` 信任门登记（备份 + temp+rename 原子写）。sidecar（`ok extension-serve`，`internal/rxext`）拦截 input.receive（检索注入 + enforce 三档：mixed 默认 = auto 自省软提醒/规则硬阻断，soft = 全软提示，hard = 全硬阻断；软路径把提醒与注入合并为一个 `<ok-context>` 块，block 优先于注入）与 tool.after（写工具成功执行才记 touched）；注入/检查核心与 hook 子命令共用 `internal/hook/core.go`（`InjectForPrompt`/`TrackTouched`/`CheckStop`），各 hook 子命令系 agent 语义一致。拦截器 fail-open：panic/错误一律 Continue。技能目录共享 SkillsHome（机制零改动）。SDK 为 `internal/rxext/sdk` vendor 快照。自愈语义同 zcode：曾登记且内容过期才重写，从未登记不复活；卸载清理插件目录与信任门登记两点位。

opencode 适配器（`opencode.go` + 内嵌模板 `opencode_plugin.ts`）：opencode 无 hooks 配置字段，其 hooks 形态是"插件文件返回 hooks 对象"——对每个配置目录 glob `{plugin,plugins}/*.{ts,js}` 单文件直接 import（Bun 原生跑 TS，免 package.json）。安装/幂等/自愈机制与 pi 同款（头标记 + 模板 sha256 前 12 位指纹 + 外部文件先备份 `.bak-openknowledge`；曾安装且过期才重写，显式移除不复活）。插件三钩子：`chat.message` ≈ UserPromptSubmit（`ok hook prompt` 纯文本 stdout 以 `synthetic:true` text part push 进 `output.parts` 注入——parts 按引用传入且 hook 后继续使用并持久化；自建 part 的 id 必须 `prt` 前缀，PartID schema 强制，否则 prompt_async 校验 Die 卡死会话）；`tool.execute.after` ≈ PostToolUse（`write`/`edit` 取 `args.filePath`，`apply_patch` 从 `patchText` 解析 `*** Add/Update/Delete File:` 行——gpt 系新模型 apply_patch 与 write/edit 互斥，必须覆盖；相对路径按 directory 绝对化后逐路径调 `ok hook post-tool`）；`event: session.idle` ≈ Stop（exit 2 + stderr 时经 SDK `client.session.promptAsync` 把 reason 作为用户消息补发回该会话，驱动当场自省——idle 无法拒绝停止，与 pi 的 `sendMessage(triggerTurn)` 同构；防重靠 ok 侧 `CheckStop` 的 LastExtractReminder/MarkBlocked 语义，插件侧与 pi 一致不计数）。子进程走 `node:child_process` execFile（内建 timeout 10s/5s/5s + windowsHide；Node/Bun 双运行时兼容——桌面端服务器跑在 Electron/Node 里，`"bun"` 模块导入会让插件整个加载失败，v2.11.0 修复实报），全程 fail-open。技能共享 SkillsHome（opencode 原生扫描 `~/.agents/skills`，机制零改动）。

dsh 适配器（`deepharness.go` + 内嵌模板 `dsh_plugin.js`）：DeepSeek Harness 无插件目录自动扫描，其 hooks 形态是"本地 JS 插件 + 家目录级 `cordis.patch.yml` 绝对路径挂载"——patch 行 `- insert: [{id: ok-hooks, name: '<插件绝对路径>'}]`（cordis patch 的 name 字段接受绝对路径；YAML 单引号 + 正斜杠，规避 Windows 反斜杠转义；家目录级 patch 层所有 profile 共享）。安装/幂等/自愈机制与 pi/opencode 同款（头标记 + 模板 sha256 前 12 位指纹 + 外部文件先备份 `.bak-openknowledge`；曾安装且过期才重写，显式移除不复活），patch 行复用 kimi 的 `UpsertHooksBlock` 标记块管理（`#` 标记在 YAML 是合法注释；`StripLegacyOKHooks` 只认 TOML `[[hooks]]` 表，对 YAML 是安全 no-op）。插件三事件：`agent/pre-step` ≈ UserPromptSubmit（`ok hook prompt` stdout 注入 messages）；`tools/post-execute` ≈ PostToolUse（`write`/`edit` 追踪）；`agent/turn-stopping` ≈ Stop（exit 2 + stderr 时经 `agent.steer()` 把 reason 作为用户消息补发续跑，与 pi 的 `sendMessage(triggerTurn)` 同构）。子进程走 `node:child_process` execFile 直 exec（无 shell 层，天然免疫 Windows pwsh 引号问题），全程 fail-open。技能共享 SkillsHome（DSH 原生扫描 `~/.agents/skills`，机制零改动）。`DSHHome()` 优先级：`OK_DSH_HOME` > `DSH_HOME`（官方重定位）> `~/.dsh`。

### 9.3 embedding 服务三形态（OpenAI 兼容协议统一）

配置为多 profile（`[[embedding.profiles]]` + `active` 指定使用中；≤v2.13 的平铺字段读取时自动迁移为"默认" openai profile），三种形态统一走 OpenAI 兼容协议：

| 形态 | 端点 | 说明 |
|------|------|------|
| openai（自定义） | 用户给的 `base_url` | 线上或自建 OpenAI 兼容服务；`api_key` 可留空适配无鉴权本地服务；key 解析：profile `api_key` 字段 → `api_key_env` 环境变量 → 无 |
| ollama | `base_url` + `/v1`（构造点自动补） | 本机/局域网 Ollama，免 key；CLI `ok setup` 经 `/api/tags` 自动探测模型列表（GUI 设置页手输模型名） |
| builtin（内置） | sidecar 状态文件给出的 `http://127.0.0.1:<port>/v1` | ok 托管 llama.cpp `llama-server`，**完全离线、知识不出本机**；仅安装版可用（runtime 随安装包分发） |

- 协议：`POST {base_url}/embeddings`，请求 `{model, input:[...]}`，响应 `{data:[{embedding,index}]}`（按 index 重排）
- 超时：客户端 `timeout_sec`（默认 5s）< hook 配置的 10s 上限，保证任何情况下 hook 不会拖累会话；builtin 未就绪**立即**降级（写 want 标记），不占超时预算

### 9.4 okserver 与 Gitea（服务端二依赖）

okserver（见 5.15）把"团队/多端 git 仓托管"外包给 **Gitea**：`GitBackend` 接口的 gitea.go 实现走 Gitea admin API（建用户/建个人仓/组织仓/发删 token）；部署形态为 docker compose 双容器（okserver + Gitea，`server/nas/` 或 okdeploy full 模式），也可 `external` 模式接入已有 Gitea。Gitea 版本兼容点实测钉住：1.22 起 token 必须带 scope（`write:repository`）、从 `[security]` 段读安装锁、1.24+ token 列表才有 `last_used_at` 时间字段（≤1.23 零值输出空串前端回落）；容器内 gitea CLI 需 `-u git`（官方镜像主进程 root，默认 exec 用户触发运行用户检查 fatal）。

---

## 10. 性能与可靠性策略

| 优化项 | 位置 | 说明 |
|--------|------|------|
| **单二进制 + 进程内无状态** | 全项目 | hook 冷启动 ~10ms；状态全在磁盘，daemon 不在时本地直接处理（fail-open） |
| **SQLite 索引 + mtime 增量同步** | `index` | 检索不逐文件扫描 Markdown；未变化条目不重算向量，每次调用最多为提问算 1 次 embedding |
| **embedding 失败降级** | `hook.HandlePrompt` | 超时/失败自动退化为纯关键词检索，注入永不缺席 |
| **同步失败隔离** | `syncx`/`daemon` | 同步（ticker/防抖/手动）任何失败仅记日志，本地链路零影响；冲突是状态不是错误，守卫防吞冲突标记 |
| **token 预算截断** | `store.TruncateToBudget` | 注入文本按 `inject.max_tokens` 截断（字符数÷2 保守估算） |
| **全面 fail-open** | 所有 hook handler | 任何内部错误 → ok.log + exit 0；`main.runHook` 还有 panic-recover 兜底 |
| **损坏条目跳过** | `index.Sync` | 变化条目解析失败跳过该文件（已索引旧行保留）并返回 `*CorruptEntriesError`：一个坏条目不拖垮全部注入 |
| **阻断防死循环** | `state.BlockedRules` | 同会话同规则只阻断一次 |
| **状态 GC** | `state.Clean` | 7 天前的会话状态自动清理 |
| **测试零网络** | 全测试套件 | `OK_HOME` 隔离 + `OPENAI_API_KEY` 置空 + `httptest` fake server |

---

## 11. 依赖关系图

（早期核心链路快照；完整包清单与主干依赖以 §3 为准——agentx/rxext/daemon/fsx/logx/syncx/serverx/oksrv/deployx 等新增层未画入本图。）

```
                 ┌─────────┐
                 │ cmd/ok  │
                 └────┬────┘
              ┌───────┴───────┐
              ▼               ▼
          ┌───┴───┐       ┌───┴────┐
          │  cli  │       │  hook  │
          └───┬───┘       └───┬────┘
              └───────┬───────┘
                      ▼
                 ┌─────────┐
                 │ project │
                 └────┬────┘
        ┌─────────────┼──────────────┬──────────┐
        ▼             ▼              ▼          ▼
   ┌─────────┐   ┌────────┐    ┌─────────┐ ┌────────┐
   │registry │   │ config │    │  index  │ │ enforce│
   └───┬─────┘   └────────┘    └────┬────┘ └───┬────┘
       │                            │          │
       ▼           ┌────────────────┤          ▼
     [toml]        ▼                ▼     ┌───────┐
               ┌───────┐       ┌────────┐ │ state │
               │ entry │       │ embed  │ └───────┘
               └───┬───┘       └────────┘
                   ▼                │
               [yaml.v3]            ▼
                          [modernc.org/sqlite]（index）
                          [doublestar]（enforce）
```

第三方库：`toml`（registry/config）、`yaml.v3`（entry）、`doublestar`（enforce）、`modernc.org/sqlite`（index）。
注：`index` 还依赖 `retrieve`（Terms 分词）与 `config`（检索权重）；`retrieve`/`embed`/`store`/`state` 无内部依赖。

---

## 12. 构建配置与命令

### 12.1 构建

```bash
go build -o ok.exe ./cmd/ok   # Windows
go build -o ok ./cmd/ok       # Linux/macOS
bash scripts/build-dist.sh    # 发布构建：dist/ok.exe·okd.exe·OkManager.exe（-ldflags "-s -w -H windowsgui" + 版本注入）+ dist/web/ + dist/deploy/okdeploy-windows-amd64.exe
python scripts/build.py       # 一键构建：dist/（ok.exe + web/ + changelogs/ + runtime/）+ Inno 安装包；--test 产测试包（版本号追加 _test，临时 iss 不改原文件）
bash scripts/build-linux.sh   # Linux 发布：tar + deb（含 runtime/）
```

无构建标签、无代码生成；前端资源仅 deployx/webui 内嵌（go:embed），客户端 GUI 的 web 资源不内嵌、由 `dist/web/` 随二进制分发。应用版本号由 build-dist.sh 用 sed 从 `installer/okryptos.iss` 的 `#define AppVersion` 提取，经 `-ldflags -X okryptos/internal/version.Version=<版本>` 注入 `internal/version.Version`（事实源只有 .iss 一处；裸 `go build` 为 `dev`）。**版本 bump 三处同步**：`scripts/sync-version.sh` 统一改写 README 徽标、官网（site/ 的 VER 变量/直链/文案）与四个 `cmd/*/winres.json` 的 exe 版本资源（ok/okd/okmanager/okdeploy，四段式 = 三段版本号 + ".0"；v2.9.0 起曾漏改 winres.json 漂移停在 2.8.0.0，v2.16.0 起脚本兜底，pre-push 钩子也会跑）。

**okdeploy 独立分发**：构建进 `dist/deploy/`（windows + linux 双平台），**不进客户端安装包**（iss 不打 dist/deploy）。

**安装器收尾（iss）**：`[Run]` 段静默覆盖安装后拉起新 okd（`nowait runhidden`，不带 skipifsilent——配合升级熔断的收尾，见 6.9），交互安装另给「打开配置中心」勾选项拉起 OkManager。

**okserver 镜像发布（CI）**：`.github/workflows/docker.yml` 打 `v*` tag（或 workflow_dispatch 手动单发）构建 okserver 多架构镜像，GHCR + Docker Hub（`z7dream/okryptos-okserver`）双发；NAS 侧默认拉 Docker Hub，`OKSERVER_IMAGE` 可切 GHCR。

**runtime 随包分发（内置 embedding 推理运行时）**：`build.py`/`build-linux.sh` 从 llama.cpp release 下载预编译 `llama-server`（版本钉死 b10405 CPU 版，win `bin-win-cpu-x64` zip / linux `bin-ubuntu-x64` tar；`LLAMA_CPP_BASE_URL` 可换源）到 `dist/runtime/`，iss 装到 `{app}\runtime`、linux 包装进 tar/deb 同目录——安装包体积因此约 50MB 级。运行时定位 `<exe 所在目录>/runtime/llama-server`，缺失则内置形态不可用（裸 exe 便携形态）并在 GUI/CLI/doctor 明确提示。**模型不随包分发**：首次启用内置形态时按清单从镜像源下载（默认 hf-mirror，约 146MB–639MB/档，断点续传 + sha256 校验）默认下载到 `<安装目录>/models/`（`[embedding] models_dir` 可改；GUI 配置弹窗可直接修改并打开文件夹，已有模型文件不随迁）。

### 12.2 常用开发命令

```bash
go test ./...          # 全部测试（42 包）
go vet ./...           # 静态检查
go build ./...         # 编译检查
```

### 12.3 依赖解析失败排查

本机 `proxy.golang.org` 不可达时：`GOPROXY=https://goproxy.cn,direct go get ...`

---

## 13. CLI 命令面

| 命令 | 作用 | 关键行为 |
|------|------|----------|
| `ok setup` | 首次引导 | 写 hooks 配置（标记块幂等）+ 装 2 个技能（propose/wiki）+ 交互配 embedding + 连通性验证 |
| `ok gui` | 打开配置中心 | 无参数运行同效；与 OkManager.exe 同为薄启动器——确保 okd 在线后开浏览器即退；127.0.0.1:17888 + 令牌鉴权（由常驻 okd 承载）；页面关闭不退出进程 |
| `ok daemon [stop]` | 常驻进程管理 | 无参启动常驻 daemon（承载 GUI 与 hook 转发，端口 17888 即单实例锁）；`stop` 停止 daemon |
| `ok init [名字]` | 注册当前项目 | 名字缺省取目录基名；建 KB 骨架；幂等写入/更新 hooks 配置（复用 setup 逻辑，失败仅提示）；同名空壳项目（无 paths）幂等补挂工作目录（registry.AddPath，救拉取/恢复空壳） |
| `ok sync` | 项目知识库多端同步 | 一次执行 = commit → pull --rebase → push（syncx.SyncOnce）；冲突时列文件并指引 GUI 冲突解决页或手动 rebase --continue；同步失败不影响本地功能 |
| `ok sync init [remote-url]` | 初始化同步 | 三情形：无远端仅本地历史 / 本地无内容克隆远端 / 本地有内容首推；写项目 `[sync]` 段；远端已有内容报 ErrRemoteNotEmpty 不自动合并 |
| `ok add --title …` | 新建条目 | `--type/--tags/--mandatory/--file`；自动同步索引库（无 key 时向量跳过） |
| `ok propose --title …` | AI 提议草稿条目 | `--type/--tags/--summary/--file|--body`；写 `draft:true`，只同步 INDEX 不算向量，不参与检索 |
| `ok approve <文件>` | 批准草稿转正 | draft=false 并同步 INDEX 与向量；非草稿/缺文件报错 |
| `ok backfill-born` | 回填存量条目 born 标签 | 按当前分支给无 born 的条目补 `born:<分支>`；预览确认后写入，已有值不覆盖；非 git 项目报错 |
| `ok capture [propose\|auto]` | 查看/切换沉淀模式 | 无参打印当前模式与 turn_interval；带参写项目 `[capture]` 小节（幂等替换） |
| `ok wiki status` / `ok wiki mark [commit]` / `ok wiki base [分支名]` | wiki 游标管理 | `status` 输出 JSON（has_wiki/behind/stale/threshold + branch/base_branch/branch_state：ok/inherited/no_cursor/diverged/gone/legacy_orphan，inherited = 读时继承可达游标；git 不可用 behind=-1）；`mark` 记游标（缺省 HEAD，按当前分支记录）并统计 wiki 条目数；`base` 查看/设置基准分支 |
| `ok search <词>` | 检索预览 | 命令行输出打分排序（调试用） |
| `ok index` | 同步索引库 | 增量同步 kb.db 并重建 INDEX.md、打印条目数（无 key 时向量跳过，退出码 0） |
| `ok list` | 列出项目与条目 | `*` 标记 mandatory |
| `ok doctor` | 体检 | 注册表/配置/embedding 连通性/hooks 安装状态/开关状态 |
| `ok on` / `ok off` | 全局开关 | 删除/创建 hooks-disabled 标志文件 |
| `ok hook prompt` | UserPromptSubmit 入口 | 基础注入 + 检索注入 |
| `ok hook post-tool` | PostToolUse 入口 | 记录触碰文件 |
| `ok hook stop` | Stop 入口 | enforce 判定，唯一可能 exit 2 的入口 |

退出码约定：hook 路径一律 0（唯一例外：stop 阻断为 2）；CLI 错误为 1 且信息到 stderr。

---

## 14. 测试与验证

### 14.1 自动化测试

- **单元测试**（`internal/` 各包白盒单测，共 144 个 `*_test.go` 文件）：registry 路由、entry 解析（含 CRLF/BOM）、config 三层合并、store 截断、embed（httptest fake server）、index（同步/查询/mandatory/迁移/2k 条目）、retrieve 分词、state 持久化、enforce 全分支、project 解析、hook 三入口、cli 各命令、setup 幂等写入、syncx（git 执行器/三情形 init/冲突守卫/single-flight，内置 git 身份无需全局配置）、oksrv（存储/认证/端点 + fake GitBackend）、deployx（任务编排/模板渲染/sudo 回退）、gui（同步/服务器/升级端点真 HTTP 测试）
- **端到端测试**（`tests/e2e/`，integration/daemon/wiki/okserver/sync 共 5 个文件）：`TestMain` 编译真实二进制，驱动完整流程——init → add → 首次提问基础注入 → 二次提问不重复 → 手改条目后 hook 查询前增量同步命中并重建 INDEX → enforce 阻断一次后放行 → 未注册目录静默 → 开关 off/on；okserver 部署全链路（root 首启→建用户→建仓→权限负例→审计）；同步双设备闭环与冲突路径（bare 仓 + 双工作目录）
- **隔离保证**：`OK_HOME` + `KIMI_CODE_HOME` + `OK_SKILLS_HOME` 指向 `t.TempDir()`，`OPENAI_API_KEY` 置空，全程零网络

运行：`go test ./... -v`（42 包全绿）；`go vet ./...` 干净。

### 14.2 真实环境验证（曾执行的手动验收）

1. `ok setup` 写入配置后 `kimi doctor` 校验通过
2. `kimi -p "git 提交规范是什么"` → 会话 wire 中出现 mandatory 全文、知识索引、检索命中
3. `kimi -p` 让 AI 写 `.go` 文件 → Stop 阻断并提示补变更日志，AI 补写后放行
4. `ok off` → hook 全静默；`ok on` → 恢复

---

## 15. 常见问题排查

### 15.1 知识完全没注入

检查：
- `ok doctor` 看"hooks 已安装"与开关状态
- `~/.okryptos/hooks-disabled` 是否存在（存在即全静默，`ok on` 恢复）
- 当前目录是否已注册（`ok list`）；hooks 只在注册项目内生效
- `~/.okryptos/ok.log` 是否有报错（如条目 YAML 损坏）

### 15.2 有注入但检索不到该命中的条目

检查：
- `ok search <关键词>` 命令行复现打分，确认是检索问题还是注入问题
- 条目 tags/summary 是否覆盖该关键词（关键词分依赖它们）
- 语义分是否为 0：embedding 未配置或失败（`ok doctor` 验证连通性）；kb.db 向量是否过期（`ok index` 同步补齐）

### 15.3 语义检索不生效

检查：
- 全局 `~/.okryptos/config.toml` 的 `[embedding]` 是否配置了 `active` 指向的 profile（旧平铺字段会自动迁移）
- 项目 config.toml 是否覆盖了全局（项目级配置优先级最高，旧模板可能有写死的 embedding 段）
- 切换 embedding 模型/服务后：身份不符时语义通道显式跳过并在 search/doctor 提示——`ok index` 检测切换自动清向量全量重建（无需再手删 kb.db）
- 内置形态：`ok doctor` 看 sidecar 状态（daemon 是否在跑、模型是否已下载、runtime 是否随安装包存在）

### 15.4 强制检查不触发

检查：
- 项目 config.toml 里 `[[enforce]]` 块是否存在且未被注释
- glob 是否**全小写**（路径统一按小写比较）
- PostToolUse 的 matcher 是否覆盖实际工具名（`Write|Edit`）
- 同会话同规则只阻断一次——是否已被阻断过（看 state/session-*.json 的 blocked_rules）

### 15.5 ok setup 后 hooks 不执行

检查：
- 是否新开了会话（hooks 配置在会话启动时加载）
- config.toml 里标记块是否完整（`# >>> okryptos hooks >>>` 成对）
- hooks command 指向的 ok.exe 路径是否还存在（移动过 exe 需重跑 `ok setup`）

### 15.6 同步不工作 / 冲突卡住

检查：
- `git --version` 是否在 PATH（syncx 调系统 git；缺失时同步禁用，本地功能不受影响）
- 项目 config.toml 的 `[sync]`：`enabled = true`、`remote` 已配、`auto_interval_min > 0`（0 = 关闭全部自动触发，手动 `ok sync` 不受影响）
- `state/sync-status.json` 的 `last_error`（daemon 每轮同步的失败落在这里与 daemon.log）
- `state/sync-conflict.json` 非空 = 有未决冲突：GUI 管理页状态点红色，进冲突解决页处理，或手动 `git -C <KB目录> rebase --continue` / `--abort`
- 认证失败反复出现：本机 git credential helper 是否配置（无 helper 时凭据只能 URL 内嵌）；GUI 服务器页「凭证管理」确认本机 `ok-sync-r-<hostname>` token 存在

---

## 16. 后续维护建议

1. **清理 go.mod 标记**：执行 `go mod tidy` 去掉间接依赖上过期的 `// indirect` 标记。
2. **防御性钳制**：`config.Load` 对 `max_tokens < 0`、`top_n < 0` 做钳制（当前手改配置为负数时 `TruncateToBudget` 会 panic——hook 路径虽有 recover 兜底，CLI 路径没有）。
3. **写盘原子化**：INDEX.md / state / registry 改为临时文件 + rename，避免崩溃半截文件。
4. **ok.log 治理**（✅ 已落地）：大小滚动已实现——超 8MB 在句柄释放窗口轮替归档 `logs/`，保留 7 天（logx.RotateIfOversize/CleanArchives，见 §8）；embedding 错误响应体裁剪部分若仍存余量可后续收尾。
5. **Doctor 校验 enforce glob**：用 doublestar 预编译用户配置的 glob，格式错误提前暴露（当前 malformed glob 静默不生效）。
6. **CRLF 归一**：仓库在 Windows 下全量 CRLF，`gofmt -l` 全报未格式化；建议加 `.gitattributes`（`* text=auto eol=lf`）统一为 LF。
7. **v2 候选方向**（当前为非目标，勿提前实现）：hooks 自动沉淀经验、其他 AI 工具适配、~~知识库远程同步~~（v2.23.0 已实现个人多端同步：syncx 引擎 + daemon ticker/防抖 + GUI 冲突解决页，见 5.14/6.7；v2.23~v2.24 另落地 okserver 服务端管理面与 okdeploy 一键部署器，见 5.15/5.16——团队组织仓已有管理面地基但团队同步能力未上线）。（v2.14.0 已实现原候选"本地 embedding"：内置 llama.cpp sidecar 形态，见 5.5/17.4；原"模型漂移检测"与"ok index 强制重算"也由 meta 身份管理落地——身份不符显式跳过，`ok index` 自动清向量全量重建。）
8. **本文档更新记录**：2026-09 增量修订至 v2.24.3——新增 §5.14（syncx）、§5.15（oksrv/serverx/credmig）、§5.16（deployx/okdeploy）、§6.7（个人多端同步链路）、§6.8（okserver 服务端与空壳项目）、§6.9（客户端版本升级链路）、§9.4（okserver 与 Gitea）；就地修订 §1/§2（依赖 4→7）/§3（包数与依赖主干）/§4（目录结构）/§5.10（ok sync）/§5.11（七页 GUI + 新端点）/§6.5（同步 janitor 与升级熔断）/§8（新状态文件）/§12（发布线变化）/§13（CLI 命令面）/§14（测试规模）。

---

## 17. 检索算法实现（深度）

本章是检索链路的实现级说明，对应代码：`internal/retrieve/retrieve.go`（分词）、
`internal/index/sync.go`（同步）、`internal/index/query.go`（查询）。

### 17.1 检索流水线总览

```
[写入侧]  Markdown 文件（唯一真相源）
            │  Sync：枚举 → diff → 只解析变化项
            ▼
        kb.db ──┬── entries（原文 + mtime + mandatory）
                ├── entries_fts（FTS5，切分后文本）
                ├── vectors（float32 blob）
                └── meta（embedding_model/embedding_dim 模型身份）
[查询侧]  用户提问
            │  Terms 分词 ──► FTS5 BM25 ─┐（准入：归一 BM25 ≥ MinScoreFloor(min_score, N)）
            │  embedding  ──► 余弦相似度 ─┤（准入：cos ≥ SemanticFloor(cos 分布, floor, min_gap)）
            ▼                             ▼
        准入任一通道达标即可 → RRF 名次融合（weighted 可回滚）只排序 → top_n → 摘要注入
```

### 17.2 分词器（`retrieve.Terms`）

规则（44 行，无第三方分词库）：

- 全部转小写后逐 rune 扫描：
  - `unicode.Han` 汉字 → 进入 CJK 缓冲，冲刷时**两两切二元组**（孤字单独成词）
  - 其他字母/数字 → 进入拉丁缓冲，冲刷时 **≥2 字符**才成词
  - 其余字符（空格、标点）视为分隔
- 示例：
  - `"Git 提交规范"` → `[git, 提交, 交规, 规范]`
  - `"rm -rf 怎么恢复"` → `[怎么, 么恢, 恢复]`（`rm` 成词，单字 `f` 被丢弃）
- **入库与查询同口径**：FTS 表里存的是 `strings.Join(Terms(text), " ")`，
  MATCH 查询也用 `Terms(提问)`——保证两边词元集合一致，这是中文可命中的关键。
- 固有局限：二元组有歧义（"提交规范"切出的"交规"也是"交通规定"的词元）；
  不处理同义词（"删除"与"rm"互不知道对方）。这是零依赖取舍，见 17.9。

### 17.3 关键词通道：FTS5 + BM25

**索引结构**（`internal/index/db.go`）：

```sql
CREATE TABLE entries(filename PK, title, type, tags, summary, body,
                     mandatory, mtime, size);         -- 原文（mtime+size 双判变化）
CREATE VIRTUAL TABLE entries_fts USING fts5(
    title, tags, summary, body, filename UNINDEXED); -- 切分后文本，独立维护
CREATE TABLE vectors(filename PK, dim, blob);        -- float32 小端
CREATE TABLE meta(key PK, value);                    -- embedding_model/embedding_dim 模型身份
```

FTS 表为**独立内容表**（非 external-content + 触发器），由 Sync 显式
delete+insert 维护——换来的是对切分预处理的完全控制。

**查询构造**（`query.go`）：

```
MATCH 串 = Terms(提问) 每个词元双引号包裹后用 " OR " 连接
SELECT e.filename, e.title, e.type, e.body,
       bm25(entries_fts, 10.0, 8.0, 3.0, 1.0) AS rank
FROM entries_fts f JOIN entries e ON e.filename = f.filename
WHERE entries_fts MATCH ? AND e.mandatory = 0
```

- **列权重 10 / 8 / 3 / 1**（title / tags / summary / body）：标题信号最强，
  tags 次之，正文最弱（长文本噪音多）。
- **BM25 相对旧方案（命中计数 +3/+2/+1）的三处修正**：
  1. **IDF**：稀有词权重高——"sqlite"命中比"配置"命中更值钱；
  2. **词频饱和**：一个词重复出现收益递减，防刷屏；
  3. **长度归一**：长 summary/body 不再天然占优。
- **归一化**：SQLite 的 `bm25()` 返回**负值**（越小越好），取 `kw = -rank`
  后用 `kw/(kw+6)` 压缩到 [0,1)——与余弦同量纲，α/β 才有真实意义。
  注：归一化仍用于关键词通道准入判定；v2.17.0 起融合默认改 RRF（只看名次），α/β 仅 `fusion = "weighted"` 回滚档生效。

### 17.4 语义通道：向量余弦

- **三形态接入**（配置 `[[embedding.profiles]]` + `active`，`embedx` 唯一构造点，形态细节见 9.3）：openai 直连、ollama 补 `/v1`、builtin 经 sidecar 状态文件发现端口；三形态共用 `OpenAIClient`，仅以 `QueryPrefix`/`DocPrefix` 区分双路径前缀。
- **写入**：条目向量 = `EmbedDocument(标题+摘要+正文)`，float32 小端 blob 存 `vectors` 表；mtime 未变的条目不重算（增量），缺向量的未变化条目在有 client 时补齐（backfill）；`ok index` 全量重建按 **32 条/批**调 `EmbedDocuments`。
- **查询**：`EmbedQuery(提问)` 与全量向量逐条算余弦（万条约 60MB 内存、毫秒级），维度不匹配返回 0；先收集本查询的余弦分布再准入（见 17.5 的 `SemanticFloor`）。
- **模型身份管理（kb.db meta）**：client 的 `ModelIdentity()`（如 `builtin:qwen3-emb-0.6b-q8`、`ollama:bge-m3@http://…`）与维度在确有向量写入后落 `meta(embedding_model/embedding_dim)`。**查询侧**身份/维度不符 → `embedx.QueryVec` 拦截：语义通道显式跳过并返回中文提示（"运行 ok index 重建后恢复"，替代以往维度不等静默归零）；**同步侧**身份不符 → `Sync` 阻断全部向量写（INDEX/FTS 照常），杜绝新旧模型向量混合；`ok index` 检测到切换先 `ClearVectors` 再全量重建。
- **内置形态 sidecar 生命周期**（daemon 托管，`embedsidecar.Manager`）：daemon 内 10s 周期 `Reconcile`——active=内置且模型文件就绪时才允许在线；拉起条件 = 激活刚变化或 want 标记 pending（hook/cli 发现未就绪时写 `embed-sidecar.want`，自己绝不等待冷启动）；`Ensure` 幂等拉起（随机回环端口 + `-m <gguf> --embeddings --pooling <清单值>`，90s 就绪等待，状态写 `embed-sidecar.json`）；**空闲 10 分钟回收**（按 `last_used`，每次成功调用经 `Touch` 刷新）；**崩溃有界重启 ×3**（连续失败进入冷却，直到配置变化重试）；模型切换/停用内置 → 立即回收；**daemon 退出时回收 sidecar**（跨进程残留按状态文件 PID 杀）。
- **成本边界**：hook 路径每次最多为**提问**算 1 次 embedding（5s 超时），条目向量只在同步时算。

### 17.5 准入与排序（v2.16.0 起分离）

```
融合（只用于排序）：rrf（默认）score = Σ_channel 1/(rrf_k + rank)，rrf_k 默认 60
                  weighted（回滚档）score = α · normBM25 + β · cosine
```

融合分之后、排序之前乘时效系数（`retrieve.recency`，默认启用）：mtime age ≤ fresh 天 → 1.0，≥ stale 天 → floor 0.85，中间线性；窗口按条目类型分（[retrieve.recency.windows]）。不参与准入。

**准入按通道独立判定**（`QueryEx`，满足其一即注入）：

1. **关键词通道**：归一 BM25 分（未乘 α）≥ `MinScoreFloor(min_score, N)`——
   `min_score` 默认 0.5、≤0 关闭；阈值随可检索条目数 N 缩放：<10 条关闭、
   10→30 线性过渡、≥30 取全值（FTS5 bm25 的 idf 在小库下趋近 0，N=2 时恰为 0，
   固定绝对阈值会误伤小库真实命中）。
2. **语义通道**：余弦 ≥ `SemanticFloor(coses, floor, min_gap)`——**模型无关**
   相对门槛：余弦绝对分布随 embedding 模型漂移（实测同组查询 bge-m3 跨域噪声
   0.52、qwen3 仅 0.26），固定绝对阈值要么漏噪声要么误杀低对比度模型。故以本次
   查询的余弦分布为参照：头部（max）相对中位数有显著分离（相对 gap ≥ `min_gap`
   默认 0.25，BGE/Qwen 四模型 12 场景标定）时门槛 = max(floor, median+0.5·gap)；
   **无显著头部则语义通道整体不准入**（宁缺毋滥，关键词通道兜底）。低对比度
   自定义模型调低 `min_gap` 放宽、≤0 关闭 gap 判定（仅绝对下限）。
3. 已获关键词准入的条目语义通道只加总分（排序用），不受语义门槛影响。

过滤与排序（严格确定顺序）：`mandatory=0 AND draft=0` → 任一通道达标 → 总分
降序（同分标题升序）→ 截 `top_n`（默认 2，**不强行凑满**）→ 注入文本按
`inject.max_tokens` 预算截断。

**诊断**（`QueryInfo`）：语义通道参与但全部候选被拒时返回
`SemanticRejected` + 分布统计（样本数/max/median/relGap）——hook 记
`prompt semantic` 日志（GUI「日志」页可按"仅语义"过滤）、`ok search` 打
stderr 并附 `min_gap` 调节指引；语义退化（模型身份缺失/切换，见 17.4）时注入
末尾每会话一次附 `[Okryptos] 语义检索退化：…` 提示。

**打分实例**（提问"git 提交规范"，条目《Git 提交规范》tags:[git]）：

- 词元：`git, 提交, 交规, 规范`
- BM25：title 全命中 + tag 命中 → kw≈7.4 → 归一 ≈0.55 ≥ 0.5 → 关键词通道准入
- 余弦（假设语义高度相关）≈0.8 ≥ 语义门槛 → 语义通道亦准入
- weighted 回滚档下：**score = 1.0×0.55 + 1.0×0.8 = 1.35**；无关提问两通道都不达标 → 零注入

### 17.6 同步算法（热路径性能来源）

`Sync` 每次 hook prompt 都会执行，其设计决定了每次提问的延迟：

```
os.ReadDir(knowledge/)                # 只拿文件名，不读内容
  ├─ DirEntry.Info()                  # Windows 下复用 readdir 数据，零额外系统调用
  ├─ 与 entries 表 filename+mtime+size 对比
  ├─ 新增/变化 → 仅这些文件 ReadFile+Parse+upsert(+重算向量)
  ├─ 已删除 → 连带清理 entries/fts/vectors
  └─ 有变化才重写 INDEX.md（缺失时必写）
```

- **单事务**：三表写入在一个 tx 内，失败整体回滚，不会出现半同步状态。
- **坏文件容错**：变化文件解析失败 → 跳过该文件（旧索引行保留），其余
  照常提交，提交后返回 `*CorruptEntriesError` 警告（`errors.As` 区分）；
  SQL 失败等致命错误才回滚。
- **复杂度**：热路径 = 1 次目录枚举 + N 次元数据对比（内存），
  与条目正文大小无关。

### 17.7 降级矩阵

| 场景 | 行为 |
|------|------|
| 未配置 embedding（active 空） | 纯关键词检索（`queryVec=nil`），一切正常 |
| openai/ollama 连接失败/超时 | 同步先失败 → `Sync(nil)` 重试 → 关键词检索；注入不缺席 |
| builtin 模型未下载 | profile 可保存不可激活；GUI/CLI 明确提示去下载 |
| sidecar 未运行/冷启动中 | hook/cli 立即降级纯 BM25 + 写 want 标记，daemon 10s 周期见到拉起；**绝不等待冷启动** |
| sidecar 崩溃 | daemon 有界重启 ×3（连续失败冷却至配置变化），期间降级；daemon 退出时回收 sidecar |
| sidecar 空闲 10 分钟 | daemon 回收（杀进程删状态文件），下次需要时经 want 再拉起 |
| 模型下载失败/校验不符 | 保留 `.part` 可续传重试；sha256 不符删 `.part`；不激活 |
| 模型身份与索引不符 | 语义通道显式跳过 + 提示 `ok index`（替代以往维度不等静默归零）；Sync 阻断向量写杜绝混合；`ok index` 自动清向量全量重建（32/批） |
| 查询余弦分布无显著头部 | 语义通道整体不准入（宁缺毋滥，关键词兜底），记 `prompt semantic` 日志（样本/max/median/relGap）；低对比度模型调低 `min_gap` 放宽 |
| 删除"使用中"的 profile | 允许删除，`active` 置空退回纯关键词，GUI/CLI 明确提示 |
| 条目缺向量（未 index） | 该条目语义分为 0，仍可被关键词命中 |
| 单个条目文件损坏 | 跳过并保留其旧索引（`CorruptEntriesError` 警告），其余正常 |
| 草稿条目（draft: true） | 不进 FTS 与向量：同步只写入 INDEX.md（标【草稿】），检索与注入排除；批准后正常参与 |
| kb.db 损坏/丢失 | hook 记 ok.log 后 exit 0（fail-open）；`ok index` 可重建 |
| hook 超时预算 | embedding `timeout_sec=5s` 不变，绝不等待 sidecar 拉起 |

### 17.8 实测性能（本机，1 万条目）

| 路径 | 耗时 | 说明 |
|------|------|------|
| 首次全量同步 | 9.8s | 一次性（含 1 万次解析与入库） |
| **hook 热路径** | **36ms** | Open + 增量同步(8ms) + 查询(27ms) |
| 旧方案（逐文件扫描） | ≥1.1s | 每次提问全量读取+解析，已退役 |

热路径由"目录枚举 + 内存 diff"构成，与正文大小无关；万级到 5 万级
预计仍在 100ms 量级。embedding API 调用（200-500ms）是另一笔网络开销，
与检索无关且失败自动降级。

### 17.9 已知局限与演进方向

- **无同义词/查询改写**："删除文件"与"rm"不互相召回 → 可在 Terms 层加
  静态映射表，或查询时并行两路改写
- **二元组歧义**："交规"误配 → 引入更小颗粒度的字典分词（会带依赖）
- **列权重与 k=6 归一常量为硬编码**：数据量大后可按命中率回归调参
- **提问向量无缓存**：连续相似提问重复调 API → 可加短 TTL 缓存
- **无反馈调权**：不记录"哪些条目被注入后真正有用" → 需要埋点，属 v2 议题

---

## 18. 配置参数参考

所有可调参数一览。合并规则：**内置默认 ← 全局 ← 项目**（后者覆盖前者）。

### 18.1 全局配置 `~/.okryptos/config.toml`

| 参数 | 默认 | 作用与调优 |
|------|------|-----------|
| `embedding.active` | 空 | 使用中 profile 名；空 = 未配置（纯关键词检索） |
| `[[embedding.profiles]]` | 无 | 可保存多套服务配置：`name`/`type`（`openai` 线上兼容 · `ollama` 本机/局域网免 key · `builtin` 内置 llama.cpp 离线）+ `base_url`/`model`/`api_key`（0600；与 `api_key_env` 二选一，字段优先）/`api_key_env`/`mirror`（builtin 下载源，默认 hf-mirror）。≤v2.13 的平铺 `base_url`/`api_key`/`api_key_env`/`model` 读取时自动迁移为"默认" openai profile |
| `embedding.timeout_sec` | `5` | 必须小于 hook 配置的 10s，保证 prompt hook 不超时；builtin 未就绪立即降级不占预算 |
| ~~换模型重建~~ | — | 不再需要手删 kb.db：身份不符自动跳过语义通道，`ok index` 检测切换自动清向量全量重建 |
| `inject.max_tokens` | `1500` | 单次注入预算（字符数÷2 估算）；mandatory 多/条目长则调大 |
| `retrieve.alpha` | `1.0` | 关键词分权重。术语精确的场景（错误码、命令名）可调大（仅 fusion=weighted 生效） |
| `retrieve.beta` | `1.0` | 语义分权重。问法多变的场景可调大（前提是 embedding 质量好）（仅 fusion=weighted 生效） |
| `retrieve.fusion` | `rrf` | 融合方式：rrf（默认，按名次融合，模型无关）或 weighted（旧 α/β 加权回滚档） |
| `retrieve.rrf_k` | `60` | RRF 名次平滑常数（仅 fusion=rrf 生效） |
| `retrieve.recency.enabled` / `floor` | `true` / `0.85` | 时效信号：按 mtime 新鲜度给融合分乘系数（不参与准入）；floor 为陈旧系数下限 |
| `retrieve.recency.windows.*` | rule/reference `[180,730]`、pitfall `[90,365]`、note `[60,180]` | 各类型 `[fresh_days, stale_days]` 窗口；全零或非法 = 该类型不衰减 |
| `retrieve.feedback.enabled` | `false` | 注入→采纳反馈闭环：窗口内持续注入但从未被读的条目降权（v1 只降不升）；默认 false——宿主 read 派发未接通前采纳信号恒零，降权默认关闭（事件照常记录），read 派发接通后恢复 `true` |
| `retrieve.feedback.window_days` / `min_injections` / `demote` | `30` / `4` / `0.8` | 统计窗口（天）/ 触发降权的最低注入次数 / 降权系数 |
| `retrieve.top_n` | `3` | 每次最多注入条数；调大注意挤占 `max_tokens` 预算 |
| `server.url` / `server.username` / `server.token` | 空 | okserver 管理面连接（GUI 服务器页写入；token 空串保留旧值，空 URL 整段清空即 logout）；[server] 仅全局层有意义 |

### 18.2 项目配置 `~/.okryptos/projects/<名>/config.toml`

可覆盖以上全部参数（`[[enforce]]` 全局层同样可配，GUI 规则卡写全局层）：

| 参数 | 说明 |
|------|------|
| `capture.mode` | 经验沉淀模式：`propose`（默认，AI 主动提议草稿人批准）或 `auto`（Stop hook 周期阻断强制自省）；`ok capture <mode>`（写项目层）或 GUI 设置页沉淀卡（写全局层） |
| `capture.turn_interval` | auto 模式的自省间隔（Stop 次数，默认 3）；GUI 设置页沉淀卡（写全局层）或手改 |
| `sync.enabled` / `sync.remote` | 多端同步开关与远端 URL（`ok sync init` 或 GUI 建仓写入；项目 config.toml 随仓同步，多端共享同一配置） |
| `sync.auto_interval_min` | daemon 自动同步间隔（分钟）；`0` = 关闭全部自动触发（ticker 与写入防抖），手动 `ok sync` 不受影响 |
| `sync.llm_assist` | 冲突 AI 合并档位：`off`/`local`；空 = auto（有本地 LLM 则 local，否则 off）；`server` 档预留未开放 |
| `provenance.auto_born` | 新建条目自动记录 born 分支溯源标签（默认 true）；手改配置文件（新配置中心未暴露该键） |
| `wiki.stale_commits` | wiki 落后多少 commit 触发 prompt 提示（默认 20，0 = 关闭；游标失效 gone/归属存疑 legacy_orphan 提示不受此阈值门控） |
| `[[enforce]].type` | 规则类型，v1 仅 `changelog_required` |
| `[[enforce]].code_globs` | "算改代码"的 glob 列表。**一律小写**；doublestar 语法，`**/*.go` 可匹配根目录文件 |
| `[[enforce]].changelog_glob` | "算写日志"的 glob，如 `docs/changelogs/**` |
| `[[enforce]].message` | 阻断时输出给 AI 的提示（会进入模型上下文，写清楚要做什么） |

### 18.3 环境变量

| 变量 | 作用 |
|------|------|
| `OK_HOME` | KB 根目录（默认 `~/.okryptos`）；测试隔离也用它 |
| `KIMI_CODE_HOME` | kimi 配置目录（`ok setup` 写 hooks 时定位 config.toml） |
| `OK_SKILLS_HOME` | 技能安装目录（默认 `~/.agents/skills`） |
| `PI_CODING_AGENT_DIR` | pi 配置根目录（默认 `~/.pi/agent`；`ok setup` 写扩展时定位 extensions/） |
| `OK_DSH_HOME` | dsh 家目录测试隔离口（默认 `~/.dsh`；`DSH_HOME` 为官方重定位变量，次之） |
| `OKSERVER_LISTEN` / `OKSERVER_DATA_DIR` / `OKSERVER_GITEA_URL` / `OKSERVER_GITEA_ADMIN_TOKEN` | okserver 服务端配置（默认 `:3100` / `./okserver-data`；部署器写进 compose `.env`） |
| `api_key_env` 指向的变量 | embedding key 的环境变量通道（如 `OPENAI_API_KEY`） |

### 18.4 hooks 配置（由 `ok setup` 维护）

**kimi**：写入 `~/.kimi-code/config.toml`（`KIMI_CODE_HOME` 优先）的标记块：

| 字段 | 当前值 | 说明 |
|------|--------|------|
| `event` | `UserPromptSubmit` / `PostToolUse` / `Stop` | 三个注入/追踪/强制时机 |
| `matcher` | 仅 PostToolUse 用 `"Write\|Edit"` | 工具名正则过滤 |
| `command` | `"<exe> hook prompt\|post-tool\|stop"` | `ok setup` 烧入绝对路径 |
| `timeout` | 三条统一，默认 `10` 秒 | 取全局配置 `[hooks] timeout_sec`（GUI 设置页可调，1~60，只写配置不重装 hooks）；prompt 必须 > `embedding.timeout_sec`（默认 5），否则慢 API 会被 kimi 强杀；post-tool/stop 过短会在高负载下被 kimi 静默杀死（2026-08-04 整会话 touched 丢失事故） |

**pi**：写入 `~/.pi/agent/extensions/okryptos.ts`（`PI_CODING_AGENT_DIR` 优先）。文件头为头标记（`// okryptos hooks (managed by ok.exe; do not edit)`）+ `// fingerprint: <模板 sha256 前 12 位>` 行；`HooksInstalled` 要求头标记存在且指纹等于当前模板指纹——模板升级后旧扩展判为"非当前版本"，由 hook 入口 `EnsureHooks` 自愈重写。安装时既有非本工具生成的同名文件先备份为 `.bak-openknowledge`，卸载只删本工具生成的文件。

### 18.5 合并与解析顺序速查

```
配置值：  内置默认  ←  ~/.okryptos/config.toml  ←  项目 config.toml
API key： 项目 api_key → 全局 api_key → api_key_env 环境变量 → 无(纯关键词)
开关：    ~/.okryptos/hooks-disabled 存在 = 全静默（ok on 恢复）
```
