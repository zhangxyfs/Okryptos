# 客户端版本升级设计：检测 → 更新内容弹窗 → 进度下载 → 静默安装

- 日期：2026-08-31
- 状态：已批准（用户确认：Windows 静默覆盖安装；Linux 只提示+链接；服务器仅更高版本提示；界面 = 其他页卡片 + 启动自动弹窗 + 左导航徽标 + 托盘入口）

## 1. 目标与边界

客户端（okd/OkManager GUI）增加版本升级闭环：

- 检测 GitHub releases 最新版，有新版本时**启动自动弹窗**（含更新内容）、左导航「其他」红点徽标、「其他」页版本升级卡、OkManager 托盘「检查更新」入口四个触点；
- Windows：点升级 → 弹窗显示更新内容 → 确认 → 进度条下载 → 完成后调起 Inno 安装器**静默覆盖**（`/VERYSILENT`，安装器自身会停 okd、升级跳过目录页）；
- Linux：同一张卡只提示 + deb/tar.gz 下载链接，不做自动下载安装；
- 服务器页：okserver 版本**高于**客户端时提示建议升级客户端（仅更高才提示）。

明确不做：自动后台定期检查（进 GUI/其他页按缓存策略检查）、macOS、Gitea 镜像仓作更新源（GitHub 是公开事实源）、第三方自更新库（签名信任链不必要）。

## 2. 关键事实依据（探码结论）

- 版本号事实源：`installer/openknowledge.iss` AppVersion → ldflags 注入 `internal/version.Version`；`/api/status` 已返回 `app_version`。版本比较工具 `parseVersion/versionLess` 在 `internal/gui/changelog.go:71/87`（同包私有，直接复用）。
- 现有升级弹窗：`checkUpgrade()`（web/app.js:5205）+ `openUpgradeModal` + `renderMd` markdown 渲染——是"更新内容弹窗"的模板；但它读的 changelogs 是本机已装版本，**远端新版本的更新内容须取 GitHub release body**。
- 下载+进度先例：`internal/embed/download.go`（`.part` 续传、不设整体 Timeout 只设 ResponseHeaderTimeout、每 256KB 回调进度）；任务状态 `dlJob` + 快照模式在 `internal/gui/embedding.go`；前端 1s 轮询进度条 `embPoll/paintEmbDl`（web/app.js:4473/4519）。无 SSE，全照抄轮询。
- Inno 静默：`/VERYSILENT /SUPPRESSMSGBOXES /NORESTART` 可用；`PrivilegesRequired=lowest` 免 UAC；`CurStepChanged(ssInstall)` 自动 `okd.exe stop`；升级判定跳过目录页（openknowledge.iss:61/79）。**装完静默模式不自启任何进程**（[Run] 带 skipifsilent）——apply 须自行拉起新 okd。
- **竞态**：安装窗口内 hook 触发 `EnsureCurrent`（internal/daemon/client.go:62）可能把旧 okd 重新拉起锁住文件，致覆盖失败——需升级熔断标记。
- okserver 版本已透传前端：`apiServerTest` 写 `out["version"]`（api_server.go:122），前端存 `SRV.srvVer`（web/app.js:5457），状态卡已显示。
- 检查状态持久化：扩展 `~/.openknowledge/gui.json`（guiState 先例，changelog.go:97），不进 config.toml（避免全局/项目合并层级语义歧义）。
- SSRF 纪律：GitHub 端点写死 `api.github.com`，不接受任意 URL 参数。

## 3. 后端（新文件 internal/gui/api_update.go，照 registerServerAPI 形态）

`registerUpdateAPI(api)` 挂进 `NewHandler`（api.go:108 旁）：

| 端点 | 行为 |
|---|---|
| `GET /api/update/check` | 读 gui.json 缓存，6h 内直接返回缓存；否则求 `https://api.github.com/repos/zhangxyfs/OpenKnowledge/releases/latest`（15s 超时、fail-open 返回 `{"error":...}` 不 500）。解析 tag_name/body/assets（找 `OpenKnowledgeSetup-<ver>.exe`、deb、tar.gz 三个浏览器下载地址），与 `version.Version` 比较，写缓存 `{checked_at, latest, body, assets}`。返回 `{current, latest, has_update, body, assets, checked_at, from_cache}` |
| `POST /api/update/download` | 幂等起后台 goroutine 下载 exe 到 `~/.openknowledge/update/`（`.part` + 续传 + 原子 rename，复用 embed.Download 形态）；已有同名完成文件直接返 done |
| `GET /api/update/download` | 进度快照 `{state, done, total, err}`（照 dlSnapshot） |
| `POST /api/update/apply` | 仅 Windows（Linux 400）：见 §4 |

gui.json 扩展：`UpdateCheck{CheckedAt, Latest, Body, ExeURL, DebURL, TarURL}` + `SkippedVersion`。

## 4. apply 流程（Windows）

1. 写熔断标记 `~/.openknowledge/update/.upgrading`；
2. `daemonx.StopDaemon()` 停自己（先 200 响应前端再异步执行）；
3. 起子进程 `OpenKnowledgeSetup-<ver>.exe /VERYSILENT /SUPPRESSMSGBOXES /NORESTART` 并等待退出；
4. 安装器退出后删除熔断标记，detached 拉起新 `okd.exe`；
5. 自退。

熔断：`internal/daemon/client.go` 的 `Ensure/EnsureCurrent` 开头检查 `.upgrading` 标记存在则跳过拉起（防 hook 在安装中途复活旧 okd 锁文件）。标记由 apply 兜底删除（安装器超时/失败也要删，defer 语义）。

## 5. 前端

- **其他页「版本升级」卡**（renderMisc，插在使用帮助卡与 danger 卡之间）：当前版本 + 最近检查时间 + 检查按钮；有新版本显示「升级 vX.Y.Z」。点击 → 更新内容弹窗（renderMd 渲染 release body）→「确认升级」→ 卡内进度条（1s 轮询 GET /api/update/download，照 embPoll）→ 完成后「立即安装」→ POST apply → toast 提示即将重启，5s 后 reload（新版 changelog 弹窗自然出现）。Linux 下卡内只显示提示与 deb/tar.gz 链接。
- **启动自动弹窗**：GUI 加载时调 check（缓存友好）；`has_update && latest != skippedVersion` → 弹更新内容弹窗，三按钮：「立即升级」（跳 #misc 版本卡开始下载）/「跳过此版本」（写 gui.json skipped_version，不再弹）/「知道了」（本次关闭）。
- **左导航徽标**：`has_update` 且未跳过时「其他」导航项显示红点；升级完成或跳过后消失。
- **服务器页提示**：新增 10 行 JS semver 比较；`SRV.srvVer > appVersion` 时状态卡/连接卡显示「服务器已更新到 vX.Y.Z，建议升级客户端（其他 → 版本升级）」chip，点击跳 #misc。
- i18n：`u*` 前缀键中英双份（updCheck/updNew/updSkip/updNow/updLater/updDownloading/updInstall/updSrvNewer 等）。

## 6. OkManager 托盘

托盘右键菜单加「检查更新」：打开 GUI 并直达 `#misc`（版本卡在页内顶部区域，有更新时自动弹窗由 GUI 侧兜底）。不另起原生通知。

## 7. 错误处理与 fail-open

- GitHub 不可达/限流：check 返回 error 字段，卡片显示「检查失败 + 重试」，自动弹窗不弹；
- 下载中断：`.part` 续传；sha256 不在首版（release 未发 checksum——记为后续项，不阻塞）；
- apply 失败（安装器退出码非 0）：删熔断标记、尝试拉起旧 okd、前端 toast 报错；
- 一切异常不影响本地功能（fail-open 纪律）。

## 8. 测试

- 版本比较：表驱动（含 v 前缀、dev、双位段）；
- check 端点：httptest 假 GitHub（latest 200/403/超时；缓存 6h 命中不发请求；skipped 语义）；
- download 任务：假 server 分段下载 + 快照状态机 + 幂等；
- apply 熔断标记：生命周期（写/删）与 Ensure 跳过逻辑单测；
- 前端：手工验收四触点（弹窗/徽标/卡片/托盘）。
