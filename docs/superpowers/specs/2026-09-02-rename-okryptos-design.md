# OpenKnowledge 改名 Okryptos 迁移方案（设计定稿）

日期：2026-09-02
状态：定稿，待实施

## 背景

项目品牌由 **OpenKnowledge** 更名为 **Okryptos**，含仓库名。约束：

- 所有 `ok` 简写不动：二进制 `ok/okd/OkManager/okserver/okdeploy`、`OK_HOME` 环境变量、`ok init` 等 CLI 命令、镜像内非 root 用户 `okserver`。
- 不强行修改本地开发目录 `D:\develop\OpenKnowledge`。
- 存量用户升级到新版后，该改的全部自动迁移，无手动步骤。

改名面盘点（2026-09-02 全仓扫描）：不分大小写共 2058 处 / 330 文件含 "openknowledge"；剔除 docs 历史与 ok 简写后，实际要动的活文件约 60~80 个。

## 决策记录（四项已定）

| 决策点 | 结论 |
|---|---|
| Go module 名 | `openknowledge` → `okryptos`，345 处 import / 129 个 Go 文件全量替换 |
| 数据目录 | `~/.openknowledge` → `~/.okryptos`，新版本首启自动迁移 |
| 技能名 | `openknowledge-*` → **`ok-*`**（ok-init / ok-on / ok-off / ok-propose / ok-wiki / ok-capture），旧副本自动清理 |
| 发布产物名 | `OpenKnowledgeSetup-<ver>.exe` → `OkryptosSetup-<ver>.exe`，在线升级做新旧双前缀兼容 |
| 版本号与迁移门控 | 改名版定为 **2.25.0**；所有迁移逻辑统一以"安装前版本 < 2.25.0"为触发条件 |

另：Inno AppId GUID（`{9F4C3A2E-7B1D-4A5F-9E2C-6D8B1A3F5E70}`）**保持不变**，否则旧版无法被升级识别、新旧并存。

## 命名映射

| 场景 | 旧 | 新 |
|---|---|---|
| 品牌显示名 | `OpenKnowledge` | `Okryptos` |
| 小写标识（module/包名/数据目录） | `openknowledge` | `okryptos` |
| 技能名 | `openknowledge-init` 等六个 | `ok-init` 等六个 |
| 安装器产物 | `OpenKnowledgeSetup-<ver>.exe` | `OkryptosSetup-<ver>.exe` |
| 在线升级 repo URL 前缀 | `updateURLPrefix`（api_update.go:170）只认旧仓前缀 | 校验同时接受新旧两仓前缀（双前缀兼容）；asset 名匹配是宽松规则（Contains "Setup"），天然兼容无需改 |
| 安装目录（新装） | `%LOCALAPPDATA%\Programs\OpenKnowledge` | `...\Programs\Okryptos` |
| 安装目录（存量升级） | — | **保留旧目录不动**（见下文"故意不搬"） |
| 仓库名 | `zhangxyfs/OpenKnowledge`（Gitea + GitHub） | `zhangxyfs/Okryptos` |
| Docker Hub 镜像 | `z7dream/openknowledge-okserver` | `z7dream/okryptos-okserver`（过渡期双发） |
| GHCR 镜像 | `ghcr.io/zhangxyfs/openknowledge/okserver` | `ghcr.io/zhangxyfs/okryptos/okserver`（过渡期双发） |
| GUI 窗口标题 | `OpenKnowledge 配置中心` | `Okryptos 配置中心` |
| 托盘 | `OpenKnowledgeTray*` / `OpenKnowledge v<x>` | `OkryptosTray*` / `Okryptos v<x>` |

## 执行清单

### Phase 0 · 仓库层（推送前最后做）

- Gitea（origin）+ GitHub 两个远端后台改名为 `zhangxyfs/Okryptos`；本地 `git remote set-url` 更新两个 remote。
- **本地开发目录不改名**：git 不关心目录名；知识库注册表项目名取自目录名，不改则不断链（功能无损，仅显示旧名）。将来若要对齐，是独立小操作：重命名注册表项目 + 同步改 `~/.okryptos/projects/OpenKnowledge/` 知识目录名，写路径必须走 `registry.Update`（裸读改写丢项目教训）。

### Phase 1 · Go module（机械全量）

- `go.mod:1` → `module okryptos`。
- 129 个 Go 文件 import `openknowledge/...` → `okryptos/...`。
- 验证：`go build ./... && go test ./...` 全绿。

### Phase 2 · 品牌名替换（活文件，两条精确规则）

- 规则：`OpenKnowledge`→`Okryptos`、`openknowledge`→`okryptos`。
- 范围：`cmd/`、`internal/`、`web/`、`site/`、`installer/`、`scripts/`、`server/`、`README.md`、`README_EN.md`、`LICENSE`、`.github/`、`.gitea/`。
- winres.json × 4（`cmd/{ok,okd,okmanager,okdeploy}/winres.json`）：ProductName/CompanyName/FileDescription/LegalCopyright。
- 托盘窗口类名 → `OkryptosTray*`（`internal/tray/tray_windows.go:160,172,214`）；GUI 窗口标题（`internal/gui/open_windows.go:23`）；daemon 输出（`internal/daemon/run.go:153`）；部署页标题（`internal/deployx/webui/web/app.js:8,98`）；reasonix sidecar 描述（`internal/agentx/reasonix.go:70,136`）。
- **冻结不改**：`docs/superpowers/` 全部 spec/plan、`docs/changelogs/`、`docs/2026-*` 分析文档（历史记录，约占大写出现量六成）。`docs/ARCHITECTURE.md` 是活文档，要改。
- **Logo 资产**：
  - 横版锁标（图标+字标）三份须改字标：`site/assets/logo.svg`、`site/assets/logo-dark.svg`、`docs/assets/logo.svg`——内含 `<text>` 渲染的 "OpenKnowledge"（两段双色，`:22/:36/:40`），改为 "Okryptos" 后字宽收窄，`viewBox`（580×120）与第二段 x 坐标（276）要重排；`aria-label` 同步。
  - 纯图标两份**图形不用改**：`installer/assets/logo.svg`、`logo-small.svg`（96×96 书本 mark，左页镂空 O、右页镂空 K——O/K 恰好仍是 Okryptos 的首字母组合，品牌图形天然延续），只改 `aria-label`。
  - 派生产物链：图标不变则 `installer/assets/make_logo.py` 无需重跑（logo.png/logo.ico/`web/favicon.ico` 不变）；脚本 docstring 品牌名顺手改。`render-deploy-logo.py` / `logo-deploy.svg` 属 okdeploy（ok 简写），不受影响。

### Phase 3 · 数据目录迁移 `~/.openknowledge` → `~/.okryptos`

触发点：`registry.Home()` 早期（或 main 启动早期，先于任何文件打开）。

- **版本门控**：仅当安装前版本 < 2.25.0 时执行迁移。判定依据双通道——安装器侧 `IsUpgrade` 已能从注册表 Uninstall 键读出旧 DisplayVersion；运行时侧以"旧构件存在性"为等价门控（2.25.0 是首个 Okryptos 版，`~/.openknowledge` 存在即来自 < 2.25.0），迁移操作本身幂等。迁移完成后在新根写版本标记（如 `migrated-from: <旧版本>`），便于排查与防重入。

- 优先级：`OK_HOME` 环境变量 > `~/.okryptos` 已存在 > 仅 `~/.openknowledge` 存在则迁移 > 都没有则新建 `~/.okryptos`。
- 迁移方式：`os.Rename` 整体搬（内部相对结构不变）。搬完后扫描修正 config 内指向旧根的绝对路径引用。
- 守卫：旧 daemon 存活时**不迁**（经旧根下 daemon.json 探测），等升级流程停掉旧 daemon 后的首次启动再迁；Rename 失败（占用等）回退旧目录，下次启动重试。
- `installer/okryptos.iss` 卸载删数据逻辑（现 `openknowledge.iss:162`）同步指向 `~/.okryptos`。
- 先例：`internal/credmig`（凭证迁移）、`config.migrateLegacy`、`index` 的 vectorsJSON 迁移。
- 注入路径无需修库：hook 注入里的条目全路径是运行时拼的（`internal/hook/core.go:228`，`KnowledgeDir()+文件名`，索引只存文件名），根目录迁移后注入自动变为 `~/.okryptos/...`；项目段 `projects/OpenKnowledge/` 取自注册表项目名，本地目录不改名则保持原样。

### Phase 4 · 技能名 `openknowledge-*` → `ok-*`

- `internal/setupx/setupx.go` 技能模板：4 个内联（`setupx.go:439-442`）+ 2 个 go:embed 独立文件（`skills/openknowledge-wiki/`、`skills/openknowledge-propose/`，SKILL.md front matter 的 `name:` 也要改）→ 全部改名 `ok-*`。
- **存量技能全量枚举迁移（升级后首启/`ok setup` 执行）**：遍历全部已检测 agent 的 `SkillsDir()` 并集——共享 `~/.agents/skills`（kimi/pi/reasonix/opencode/codex/dsh）+ 独立 `~/.zcode/skills`、`~/.qoder-cn/skills`、`~/.lingma/skills`、`~/.claude/skills`（`setupx.go:41-55`、`agentx.go:55-61`）——**逐个 glob `openknowledge-*` 目录并删除**（不留判断死角：即使某 agent 当前未检测到 hooks 接入，只要技能目录存在就扫），随后安装 `ok-*` 新技能（`{{EXE}}` 重新烘焙）。不能只覆盖安装新技能而不删旧目录——旧副本会被 agent 照常扫描发现，成幽灵技能且新版卸载删不掉。
- 迁移挂在 InstallSkills + EnsureSkills 同窗口（daemon 启动的 hooks 自愈通道，先例 `hook.go:182 selfHealHooksThrottled`）；rxext 自愈通道（serve.go:214-228）不含 EnsureSkills，纯 Reasonix 用户需补挂。
- 引用更新：`README.md:39`、`README_EN.md:40`、`web/help.md:18-22`、`site/docs.html:152`、`web/app.js:149,371` 等处的 `/openknowledge-*` → `/ok-*`。

### Phase 5 · 安装器与发布链

- `installer/openknowledge.iss` → `okryptos.iss`（连带 `scripts/build-installer.sh`、`build.py`、`sync-version.sh` 引用）：AppName/AppPublisher/DefaultDirName/DefaultGroupName/OutputBaseFilename/Icons/卸载显示名全改；**AppId 不变**。
- 注册表 Run 键写新值名 `Okryptos`，并加代码删除旧值 `"OpenKnowledge"`，避免双自启。
- **安装目录故意不搬**：存量用户升级仍装回 `Programs\OpenKnowledge`（Inno 升级默认行为 + hooks 烧入 exe 绝对路径 + PATH 都指着旧目录；路径用户无感知，搬动需联动改 PATH 与重烤全部 agent hooks，收益低风险高）。新装用户进 `Programs\Okryptos`。
- `internal/gui/api_update.go`：双前缀兼容改的是 `:170` 的 `updateURLPrefix`（SSRF 白名单同时接受 `.../OpenKnowledge/releases/download/` 与 `.../Okryptos/releases/download/`）；`:230` 的本地落盘名顺手改新名即可（不参与匹配）。
- `scripts/publish-release.py:88` 仓库名改 `zhangxyfs/Okryptos`；`:143` 上传产物名同步。
- `installer/nfpm.yaml`：**只改 homepage**；包名保留 `openknowledge` 不动（dpkg 视新包名为全新软件包：文件冲突 + 旧包永久遗留，详见 Phase 5 补充）。
- `sync-version.sh:50` 的徽标/文件名正则同步。

### Phase 6 · 服务端与镜像（过渡期新旧双发）

- `.github/workflows/docker.yml`：同一构建推 8 个 tag——新名 `z7dream/okryptos-okserver:{tag,latest}` + `ghcr.io/zhangxyfs/okryptos/okserver:{tag,latest}`，同时继续推旧名两组。**存量 NAS 的 .env 固化旧镜像名，直接改名等于掐断其升级渠道。**
- GHCR 新包一次性人工步骤：首推后到 GitHub Packages 把新包设 Public，否则 NAS 匿名拉取被拒（workflow 注释已有此前车之鉴）。
- `server/nas/docker-compose.yml:18` 的 `OKSERVER_IMAGE` 默认值、`.env.example`、`server/nas/README.md` 改新名；旧部署的 `.env` 无需动。
- `internal/deployx` 管理页默认镜像名改新名（实施时确认具体位置）；已部署实例存的旧名继续有效。
- 旧名停发时机：过渡期 2~3 个大版本后在 changelog 宣布废弃，再从 workflow 摘掉旧 tag。
- 发布边界纪律不变：`cmd/okserver` 产物与 `server/` 目录不进任何客户端安装包。

### Phase 7 · 验证

- `go build ./... && go test ./...` 全绿（注意 agentx 测试夹具的平台双态约定）。
- `scripts/build.py` 全量构建；`scripts/sync-version.sh` 同步版本徽标（注意 winres 段 sed 分隔符坑）。
- 实测两条升级路径：
  1. 旧版覆盖升级：AppId 不变应识别，装回旧目录，Run 键换名无双自启。
  2. 数据目录自动迁移：旧 `~/.openknowledge` 在首启后变为 `~/.okryptos`，知识条目、registry、config 完好；旧 daemon 存活时首启不迁。
- 实测技能迁移：`ok setup` 后 `~/.agents/skills/` 下只有 `ok-*`，无 `openknowledge-*` 残留。
- 实测在线升级：旧版客户端能匹配到新 asset（双前缀）。

## 已知坑（实施时核对）

1. **产物命名"三处不一致"经核实为虚惊**：真实链路是 iss `OutputBaseFilename=OpenKnowledgeSetup-<ver>`（无连字符）= `publish-release.py:143` 上传名 = GitHub asset 实名；`api_update.go:230` 的有连字符写法只是本地落盘名，不参与匹配——匹配在 `api_update.go:157` 用宽松规则（`.exe` 后缀 + Contains `"Setup"`），改名后 `OkryptosSetup-*` 天然兼容，无需对齐。
2. **托盘窗口类名改名**：旧实例与新实例类名不同可短暂共存，安装器已先停旧 okd，无实际冲突。
3. **hooks 烧入 exe 绝对路径**：安装目录不变则无需处理；这是"安装目录故意不搬"的根本原因。
4. **发布纪律**：先推 master+tag 再 publish-release.py（tag 错位教训）；docker.yml 须 git push tag 触发；README 徽标 bump 时跑 sync-version.sh。
5. 知识库内沉淀条目里的旧名（`~/.openknowledge/projects/OpenKnowledge/knowledge/*`）随目录迁移自然带过去，内容里的旧名不改，不影响运行。

## 代码级深查补充（2026-09-02，按代码逻辑全链路审查）

### 新增 Phase -1 · 过渡兼容版（改名前必须先行发版铺量）

**在线升级存在跨版本硬断点**：`internal/gui/api_update.go:170` 的 `updateURLPrefix` 是 SSRF 前缀白名单（`:222` 校验），repo 改名后 GitHub API 返回的 `browser_download_url` 全部变为 `.../zhangxyfs/Okryptos/...`，**所有存量旧客户端前缀校验 400 拒绝，一键升级通道在改名瞬间永久失效**（检查更新本身靠 GitHub 301 重定向大概率仍通，但下载必断）。

对策（顺序不可颠倒）：

1. 先以 OpenKnowledge 身份发一版过渡版（版本号 2.24.x，即 < 2.25.0），唯一关键改动：`updateURLPrefix` 校验同时接受新旧两个前缀。
2. 铺量（等装机面大部分升到过渡版）后再执行仓库改名（Phase 0）与后续 Phase。
3. 不接受铺量等待的代价就是：未升级用户只能手动下载新包装一次。

### Phase 2 补充（隐藏契约，必须与品牌替换同一次完成）

- `[OpenKnowledge]` 注入前缀是**检索剥离的确定性契约**：`internal/retrieve/clean.go:10`（剥离侧）↔ `internal/hook/core.go:356`、`internal/hook/hook.go:407-463`（注入侧），两侧不同步改则检索词被自身注入污染（FTS/向量命中率归零的既有事故）。存量 AI 会话 transcript 里的旧前缀行改名后不再被剥离，跨版本有轻度检索噪声，可接受。
- 窗口标题是**跨进程子串匹配契约**：`internal/gui/browser_windows.go:40` `maximizeWindowByTitle` 靠标题找浏览器窗口；改名须同步 `internal/gui/open_windows.go:23`、`cmd/okmanager/host_windows.go:330`、`cmd/okdeploy/main.go:49` 与 `host_windows.go:171` 的 `BrowserOptions.WindowTitle`。

### Phase 4 补充（插件文件改名 → 双注入风险，比技能残留更严重）

技能目录清理之外，还有四处**按名识别/按 glob 加载**的插件残留，迁移逻辑必须"认旧名、删旧件"：

- **opencode**：`internal/agentx/opencode.go:41` 写 `plugins/openknowledge.ts`，opencode 对 `plugins/*.{ts,js}` 全量 import；旧文件不删则新旧双插件双注入。
- **pi**：`internal/agentx/pi.go:39` 写 `extensions/openknowledge.ts`，同理。
- **dsh**：`internal/agentx/kimi.go:16-17` 标记常量若改名，`UpsertHooksBlock` 认不出旧块、`removeDSHMarkerBlock`（deepharness.go:147-167）剥不掉 → cordis 双挂载双注入；插件目录 `~/.dsh/plugins/openknowledge/`（deepharness.go:37）成孤儿。
- **reasonix**：登记按 `name=="openknowledge"` 匹配（reasonix.go:119-127），改名后旧条目残留 + 新条目重复，宿主加载两个 sidecar；删目录守卫（:259）、`reasonixPluginDir()`（:35）、manifest name（:68）、`internal/rxext/serve.go:32` sidecar `Name` 全部绑在这一个字符串上，须同改。
- **kimi 标记块**：改名半自愈（不会双派发），但两条旧标记注释行成孤儿永久残留，迁移期需双标记识别。
- **rxext 自愈通道缺口**：`internal/rxext/serve.go:214-228` 只跑 EnsureHooks 不含 EnsureSkills——纯 Reasonix 用户的技能迁移/旧件清理不会自动发生，需补挂或接受其走 hook 通道。
- **prompt 文案引用技能名**：`internal/hook/hook.go:407-432`（wikiNudge 五种变体）与 `internal/cli/cli.go:468`（search 兜底）写死"用 openknowledge-wiki 技能"，不改则 AI 幻觉调用不存在的技能。
- 改名免疫确认：hooks 归属识别不看品牌/exe basename（kimi 靠命令正则、claude/codex/qoder 靠 `" hook <okHook> claude"` 后缀、zcode 靠 args 形态）；`.bak-openknowledge` 备份后缀、插件内 marker 注释、`opencode_plugin.ts` 导出名、pi `customType` 均为纯内部串，可改可不改（改则清理逻辑须双名识别）。

### Phase 5 补充

- **deb 包名保留 `openknowledge` 不动**（决策变更）：`nfpm.yaml:1` 包名若改 `okryptos`，dpkg 视为全新包——文件冲突报错（`/usr/bin/ok` 两包占用）且旧包永久遗留不再收升级。homepage 可改，包名不动；如坚持改包名须加 `replaces`/`breaks`/`provides: [openknowledge]` 并写迁移说明。连带 `scripts/verify-deb.py:43-56` 路径校验与 `internal/setupx/autostart*.go` 的 `.desktop` Exec 路径 `/usr/lib/openknowledge/ok`——安装路径不动则均不动（Linux 自启 .desktop 为 setup 期写入、无启动自愈，路径若改需新增重写逻辑）。
- **ldflags 版本注入路径**：`scripts/build-dist.sh:23-25`、`scripts/build-linux.sh:14-22,46` 的 `-X openknowledge/internal/version.Version` 随 module 改名必须同步为 `okryptos/...`，**漏改版本号静默回 dev**。
- **iss 文件名本身是六处脚本输入**：publish-release.py:31、build.py:30、sync-version.sh:10、build-linux.sh:7、build-dist.sh:4、build-installer.sh:17——iss 改名六处全断，须同步。
- Run 键旧值清理具体化：`[Code]` 的 `CurStepChanged(ssPostInstall)` 里 `RegDeleteValue(HKCU, Run键, 'OpenKnowledge')`；`uninsdeletevalue` 只认新名，不清理则卸载后残留指向已删 okd.exe 的自启项。
- `sync-version.sh:50-52` 三条 sed 正则写死旧 asset 名，asset 改名时先改正则再跑脚本。
- 数据目录迁移后 config 唯一真实旧根绝对路径键确认是 `[embedding] models_dir`（`internal/gui/embedding.go:380-394` → setupx.SaveEmbeddingModelsDir，.deb 回退场景落盘），迁移扫描覆盖此键即可。

### Phase 6 补充

- deployx 镜像名**真源共 4 处**（不是只有 compose 默认值）：`internal/deployx/deploy.go:66-67`（部署/升级共用拉取）、`manage.go:99`（升级时整行 sed 重写 .env 的 OKSERVER_IMAGE，切新名即静默迁移，无阻塞）、`templates.go:50`（RenderEnv 全新部署默认值）、`version.go:14`（Docker Hub tag 查询定"最新版本"——**fail-open**：旧名停推后静默失效，管理页永不提示新版本）。
- 离线包场景：`deploy.go:68` `docker image inspect <名>:<tag>` 本地探测，离线包构建侧 tag 名必须同步改，否则"本地已有镜像"判定落空变成联网拉取。
- 改名免疫确认：存量容器识别是子串匹配 "okserver"（probe.go:157-161），`okryptos-okserver` 仍命中；okdeploy 管理页不持久化镜像名；syncx 同步仓命名为 `ok-<project>`/`<project>`（oksrv/http.go:247,252），远端 URL 全部指向用户自有 NAS 的 Gitea，与本次改名零关系。
- 部署目录默认 `~/openknowledge`（templates.go:20）可改 `~/okryptos`，只影响新部署，无风险。
- `web/app.js:6649` GUI 服务端指引卡、`internal/deployx/webui/web/app.js:763` 部署表单提示均硬编码旧镜像名，属功能性文档串，须同步。
- syncx 提交身份 `OpenKnowledge Sync <sync@openknowledge.local>`（repo.go:69,128 等）写进每个同步仓 git 历史，纯标识，改了不影响已有提交。

#### okdeploy 一键部署工具专项

- **产物名免疫**：`okdeploy-windows-amd64.exe` / `okdeploy-linux-amd64`（`build.py:103,106`、`build-dist.sh:28`、`build-linux.sh:23`）是 ok 简写，不动；不进客户端安装包（iss 不打 dist/deploy），发布边界纪律保持。
- **服务端升级即迁移**：okdeploy 管理页把存量 NAS 从 <2.25.0 升到 ≥2.25.0 时，`manage.go:99` 的 sed 整行重写 `.env` 的 `OKSERVER_IMAGE`——2.25.0 起默认值切新镜像名，**升级动作本身即完成服务端镜像迁移**，与客户端"安装前版本 < 2.25.0 自动迁移"门控对齐（服务端侧等价物就是这次升级）。旧镜像残留本地磁盘，无害。
- **容器识别免疫已确认**：probe.go:157-161 子串匹配 "okserver"，`okryptos-okserver` 命中；存量容器名 `openknowledge-okserver-1`（compose 项目名=目录 basename）与镜像名脱钩，不受改名影响。
- **新部署默认值**：部署目录 `~/openknowledge` → `~/okryptos`（templates.go:20），只影响新部署；存量目录不重命名、不迁移（服务端数据目录 `./okserver-data` 是 ok 简写，本就不动）。
- **显示层**：任务名 "部署/卸载 OpenKnowledge 服务端"（deploy.go:98、manage.go:141）、webui 标题（webui/web/app.js:8,98）、部署表单镜像提示（webui/web/app.js:763）、`web/app.js:6649` 指引卡 compose 示例，随 Phase 2 规则统一替换。
- 测试夹具里的 `/home/u/openknowledge`、容器名样例等随全局替换机械更新。

### 待实测清单（改名当天逐项验证）→ 实测结果（2026-09-03 已验）

1. ✅ GitHub API 旧仓 `releases/latest` 301 → `/repositories/1334068923/...`，跟随重定向返回 v2.25.0，响应体内 `browser_download_url` 已是新仓名。旧版一键升级断链担忧消除。
2. ✅ GitHub web 下载 URL（`releases/download/...`）301 → 新仓，site/ 与 README 历史旧链接继续可用。
3. ❌ **Pages 旧站 URL 不跟随重定向**：`zhangxyfs.github.io/OpenKnowledge/` 直接 **404**；新站 `zhangxyfs.github.io/Okryptos/` 200。后果：所有外链到旧 Pages 的书签/引用失效，无补救手段（除非旧仓名被重新占用建 Pages）。已在官网/README 全面换新地址。
4. ✅ Gitea 三项全通：web 301 → 新仓、`/api/v1/repos/...` 301 → 新仓、git ls-remote 旧 URL 跟随重定向成功（返回 HEAD 8992a05）。
5. ✅ GHCR 老包推权限保持：v2.25.0 Docker 工作流四组 tag 全部推送成功且 digest 相同（`docker.io z7dream/okryptos-okserver`、`docker.io z7dream/openknowledge-okserver`、`ghcr.io/zhangxyfs/okryptos/okserver`、`ghcr.io/zhangxyfs/openknowledge/okserver`，各 latest+v2.25.0）——仓改名不断包-仓绑定，旧名双发在 GHCR 侧同样可行。
6. ⏳ 双名并存下 okdeploy 升级实测——需真实 NAS 环境，留待用户侧验证；compose/.env sed 改写逻辑已有单测覆盖。

### 确认无雷区（逐包核实，免改）

- 无 CreateMutex/命名管道/共享内存；单实例 = 端口 17888 + daemon.json 凭证 + 自省 ticker，全程无品牌串。
- daemon.json/gui.json/gui-state.json/embed-sidecar.json/registry.toml/state/session-*.json 的字段名与值均无 openknowledge 字符串；fsx 锁文件派生命名随 Home 迁移。
- kb.db（entries、meta）、wiki.json、cred-migrated.json、token 名 `ok-sync-r-<hostname>`、oksrv 的 `okserver.db`/`./okserver-data`/容器内 `/data`/OKSERVER_* 环境变量——全部无品牌串。
- HTTP 头 `X-Ok-Token`、探针文件 `.ok-wprobe`、wrappers `ok-hook-*.cmd`——ok 简写，按规则不动。
- daemon.json 指纹 `exe路径|size|mtime`（daemonx.go:74-83）——"旧 daemon 存活不迁"守卫覆盖，实施时确认迁移点先于任何 daemon.json 读写。

## 实施完成记录（2026-09-03，工作区未提交）

全部 Phase 已落地，`go build ./...` / `go test ./...` 全绿，`scripts/sync-version.sh` 已把 2.25.0 同步到 README 徽标/官网/winres，`python scripts/build.py` 产出 `installer/output/OkryptosSetup-2.25.0.exe`（23.5 MB）。实施中超出原清单的发现与处理：

- **server/nas/Dockerfile:10 ldflags 漏网**：Phase 1 的 module 替换没覆盖 Dockerfile 内 `-X openknowledge/internal/version.Version`（不在 .go 文件里），已改 `okryptos/...`——不改则 okserver 镜像版本号注入静默失效。
- **插件模板内嵌名字段**：`dsh_plugin.js` 的 `plugin`/`name`、`pi_extension.ts` 的 `customType`、`opencode_plugin.ts` 的导出符号 `OpenKnowledgePlugin`→`OkryptosPlugin`，原清单未列，随 Phase 4 一并改（均为本工具自闭合协议字段，无外部消费者）。
- **XDG 自启文件名不动**：`~/.config/autostart/openknowledge.desktop` 保留旧名（改名会产生双自启项），仅桌面条目 `Name=` 显示为 Okryptos；Windows 侧旧 Run 值由 iss 清理。
- **sync-version.sh 顺序坑**：站点直链的版本号正则先于资产名正则命中，旧资产名 `OpenKnowledgeSetup-2.24.3.exe` 会被改成 `v2.25.0/OpenKnowledgeSetup-2.24.3.exe` 的坏链——实施时手工修为三处 `OkryptosSetup-2.25.0.exe`；之后版本 bump 走新正则无此问题。
- **deployx 测试样例目录**：`/home/u/openknowledge` 等任意样例已机械更新为 okryptos；`probe_test.go` 的 legacy 容器名/镜像名夹具**故意保留**（模拟真实存量 NAS 探测，回归价值）。
- **Logo 字标**：三份字标 SVG（site 明暗 + docs）改 `O + K(翻转) + ryptos` 三段，viewBox 580→415，书形 mark 的 O/K 镂空与新名天然对齐；坐标修正过一次（K 组 translate 162、ryptos x=183，消除重叠/空隙）；installer 两份纯图标 SVG 仅改 aria-label；README `<img width>` 同步 415。
- **官网 changelog**：新增 v2.25.0 中英条目（badge-latest 从 v2.21.0 移到 v2.25.0）；历史条目（含旧插件路径描述）按冻结原则不动。
- **`docs/changelogs/2.25.0.md`** 已写（GUI 更新弹窗数据源，build.py 拷入 dist/changelogs）。
- 待实测清单已逐项验证（结果见上节）：#1/#2/#4/#5 通过，**#3 Pages 旧站 404 不跟随重定向**（唯一实质性损失），#6 留用户侧 NAS 实测。
- **Logo 入场动画初始偏移要随文宽重调**：右侧文字组的初始 translate（-150）是按旧字宽调的"贴住书本"起点，字标改短后不收缩会把首字母压进书本图标；改为 -57（=期望起点 84 − K 组绝对终点 141），中间关键帧等比收缩。截图验证必须覆盖 t≈0 初始帧与定格帧两个状态（virtual-time-budget 600 / 4000），只看定格态会漏掉入场穿模。
