# 客户端版本升级 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 客户端自动检测 GitHub 新版本，其他页版本卡一键升级（Windows 静默覆盖安装），启动弹窗 + 跳过版本 + 侧栏红点 + 托盘检查更新，服务端版本高于客户端时提示。

**Architecture:** 更新检测逻辑全部放 okd 的 GUI HTTP 层（`internal/gui`），前端 `web/app.js` 只负责展示与轮询；下载复用 `internal/embed.Download` 的 dlJob 模式（`.part` 续传 + 1s 轮询）；Windows 升级通过调起 Inno 安装器 `/VERYSILENT` 完成，期间用熔断标记防 hook 复活旧 okd。

**Tech Stack:** Go（okd）、原生 JS（web/app.js）、Inno Setup（既有安装器）

**Spec:** `docs/superpowers/specs/2026-08-31-client-update-design.md`

---

## 关键约定（所有任务共享）

- 更新源写死 `https://api.github.com/repos/zhangxyfs/OpenKnowledge/releases/latest`，包级变量 `githubAPI` 供测试替换；HTTP 15s 超时；任何失败 fail-open（返回 `{"update_available": false}`，不报错）。
- 状态存 `~/.openknowledge/gui.json`（既有 `guiState` 先例，0600），不进 config.toml。
- 熔断标记 `~/.openknowledge/update/.upgrading`：apply 时写、装完删；`daemon.Ensure()/EnsureCurrent()` 开头检查它存在则直接跳过拉起。
- 工作区有用户未提交改动（`internal/agentx/kimi.go` 等），一律不碰；提交只 add 本计划涉及的文件。
- 行号可能随版本漂移，Edit 对不上时用符号名 Grep 重定位。

## 文件结构

- `internal/gui/changelog.go`（改）：guiState 扩展字段 + helper
- `internal/gui/api_update.go`（新）：check / download / apply 三个端点
- `internal/gui/api_update_test.go`（新）
- `internal/gui/api.go`（改）：注册 `registerUpdateAPI(api)`
- `internal/daemon/client.go`（改）：Ensure/EnsureCurrent 熔断
- `web/app.js`（改）：i18n `u*` 键、其他页版本卡、启动弹窗、侧栏红点、服务器页提示、go=misc 解析
- `internal/tray/tray_windows.go` + `internal/tray/tray_other.go`（改）：托盘「检查更新」菜单
- `internal/daemon/run.go`（改）：托盘接线
- `docs/changelogs/` 对应版本 changelog（改）

---

### Task 1: guiState 扩展（UpdateCheck 缓存 + SkippedVersion）

**Files:**
- Modify: `internal/gui/changelog.go`
- Test: `internal/gui/changelog_test.go`（若无则新建）

- [ ] **Step 1: 写失败测试**

```go
func TestGuiStateUpdateFieldsRoundTrip(t *testing.T) {
    dir := t.TempDir()
    t.Setenv("OK_HOME", dir) // 按 registry.Home() 的实际覆盖方式调整
    st := guiState{
        LastSeenVersion: "2.24.2",
        SkippedVersion:  "2.25.0",
        UpdateCheck: &UpdateCheck{
            CheckedAt: time.Now().Unix(),
            Latest:    "2.25.0",
            Body:      "release notes",
            InstallerURL: "https://github.com/.../OpenKnowledge-Setup-2.25.0.exe",
            DebURL:       "https://github.com/.../openknowledge_2.25.0_amd64.deb",
            TarURL:       "https://github.com/.../openknowledge_2.25.0_linux_amd64.tar.gz",
        },
    }
    if err := writeGuiState(st); err != nil { t.Fatal(err) }
    got, err := readGuiState()
    if err != nil { t.Fatal(err) }
    if got.SkippedVersion != "2.25.0" || got.UpdateCheck == nil || got.UpdateCheck.Latest != "2.25.0" {
        t.Fatalf("round trip failed: %+v", got)
    }
}
```

（readGuiState/writeGuiState 若不存在，按 `apiChangelogSeen` 内联读写模式抽成 helper；若已有等价函数直接用。）

- [ ] **Step 2: 跑测试确认红** — `go test ./internal/gui/ -run TestGuiStateUpdate -v`，预期编译失败（字段不存在）。
- [ ] **Step 3: 实现**

`internal/gui/changelog.go` 的 `guiState`（约 :97）加字段：

```go
type UpdateCheck struct {
    CheckedAt    int64  `json:"checked_at"`
    Latest       string `json:"latest"`
    Body         string `json:"body,omitempty"`
    InstallerURL string `json:"installer_url,omitempty"`
    DebURL       string `json:"deb_url,omitempty"`
    TarURL       string `json:"tar_url,omitempty"`
}

type guiState struct {
    LastSeenVersion string       `json:"last_seen_version,omitempty"`
    SkippedVersion  string       `json:"skipped_version,omitempty"`
    UpdateCheck     *UpdateCheck `json:"update_check,omitempty"`
}
```

读写沿用既有 `guiStatePath()` + 0600 模式，抽 `readGuiState()/writeGuiState()` helper 并让 `apiChangelogSeen` 改用它。

- [ ] **Step 4: 跑测试确认绿** — `go test ./internal/gui/ -v`
- [ ] **Step 5: 提交** — `git add internal/gui/changelog.go internal/gui/changelog_test.go && git commit -m "feat(gui): guiState 扩展更新检查缓存与跳过版本字段"`

---

### Task 2: GET /api/update/check

**Files:**
- Create: `internal/gui/api_update.go`
- Modify: `internal/gui/api.go`（约 :108 `registerServerAPI(api)` 旁加一行注册）
- Test: `internal/gui/api_update_test.go`

- [ ] **Step 1: 写失败测试**（httptest 假 GitHub + 替换包级 `githubAPI` 变量）

```go
func TestUpdateCheckNewVersion(t *testing.T) {
    fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        json.NewEncoder(w).Encode(map[string]any{
            "tag_name": "v99.0.0",
            "body":     "notes",
            "assets": []map[string]any{
                {"name": "OpenKnowledge-Setup-99.0.0.exe", "browser_download_url": "https://x/setup.exe"},
                {"name": "openknowledge_99.0.0_amd64.deb", "browser_download_url": "https://x/a.deb"},
                {"name": "openknowledge_99.0.0_linux_amd64.tar.gz", "browser_download_url": "https://x/a.tar.gz"},
            },
        })
    }))
    defer fake.Close()
    old := githubAPI; githubAPI = fake.URL; defer func() { githubAPI = old }()
    t.Setenv("OK_HOME", t.TempDir()) // 隔离 gui.json
    // 调 handler，断言 update_available=true、latest=99.0.0、三个 URL 透传
    // 再调一次，断言第二次命中缓存（可给 fake 加计数器，应仍为 1）
}

func TestUpdateCheckFailOpen(t *testing.T) {
    old := githubAPI; githubAPI = "http://127.0.0.1:1"; defer func() { githubAPI = old }()
    t.Setenv("OK_HOME", t.TempDir())
    // 断言 200 且 update_available=false，不是 5xx
}
```

- [ ] **Step 2: 跑测试确认红** — 编译失败（函数不存在）。
- [ ] **Step 3: 实现**

`internal/gui/api_update.go`：

```go
package gui

var githubAPI = "https://api.github.com/repos/zhangxyfs/OpenKnowledge/releases/latest"

type updateCheckResp struct {
    UpdateAvailable bool   `json:"update_available"`
    Latest          string `json:"latest,omitempty"`
    Current         string `json:"current"`
    Body            string `json:"body,omitempty"`
    InstallerURL    string `json:"installer_url,omitempty"`
    DebURL          string `json:"deb_url,omitempty"`
    TarURL          string `json:"tar_url,omitempty"`
}

func registerUpdateAPI(api func(string, func(http.ResponseWriter, *http.Request))) {
    api("GET /api/update/check", h.apiUpdateCheck) // 按 api.go 实际闭包签名调整
    // ...
}
```

逻辑：
1. 读 gui.json 的 `UpdateCheck`，若 `CheckedAt` 距今 < 6h 直接用缓存结果比较。
2. 否则 GET `githubAPI`（15s 超时 client，`Accept: application/vnd.github+json`）。失败 → fail-open 返回 `update_available:false`（仍写缓存 CheckedAt 防雪崩，Latest 留空）。
3. 解析 tag_name 去 `v` 前缀，body，按资产名后缀匹配三 URL（`.exe` 含 `Setup` / `.deb` / `linux_amd64.tar.gz`，宽松匹配防命名微调）。
4. 用 `parseVersion` + `versionLess`（changelog.go :71/:87）与 `version.Version` 比较，仅严格更高才 `update_available:true`。
5. 写回 gui.json，响应 `updateCheckResp`。

- [ ] **Step 4: 跑测试确认绿** — `go test ./internal/gui/ -run TestUpdateCheck -v`
- [ ] **Step 5: 提交** — `git add internal/gui/api_update.go internal/gui/api_update_test.go internal/gui/api.go && git commit -m "feat(gui): GET /api/update/check 检查 GitHub 最新版本（6h 缓存/fail-open）"`

---

### Task 3: POST/GET /api/update/download

**Files:**
- Modify: `internal/gui/api_update.go`
- Test: `internal/gui/api_update_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestUpdateDownloadJob(t *testing.T) {
    // httptest 假文件服务器吐固定内容
    // POST /api/update/download {"url": fake.URL, "version": "99.0.0"} → 200（幂等，重复 POST 不起第二个）
    // 轮询 GET /api/update/download → state 从 running 到 done，path 非空，无 .part 后缀
}
```

- [ ] **Step 2: 跑测试确认红**
- [ ] **Step 3: 实现**

照 `internal/gui/embedding.go` 的 dlJob 模式（:23/:36/:290-352）：

- 包级 `updJob{State, Done, Total, Path, Err, cancel, mu}` + `updSnapshot()`。
- `POST /api/update/download`：body `{url, version}`；**校验 url 前缀必须是 `https://github.com/zhangxyfs/OpenKnowledge/releases/download/`**（SSRF 纪律，check 端点给的 URL 之外的一律 400）；已有 running 任务直接返回当前快照（幂等）；否则 goroutine 里 `embed.Download(ctx, defaultClient 式无整体 Timeout 的 client, ...)` 到 `~/.openknowledge/update/`。
- `GET /api/update/download`：返回快照 `{state, done, total, path, err}`。
- embed.Download 的签名是 `(ctx, hc, m, mirror, dir, progress)`——它面向模型文件，若不直接适配安装器下载，就在 api_update.go 内写一个小的 `downloadFile(ctx, url, dir, progress)` 复用其 `.part` + 进度回调思路，不要硬套模型下载。

- [ ] **Step 4: 跑测试确认绿** — `go test ./internal/gui/ -run TestUpdateDownload -v`
- [ ] **Step 5: 提交** — `git commit -m "feat(gui): /api/update/download 安装器下载任务（.part 续传 + 轮询快照）"`

---

### Task 4: POST /api/update/apply + daemon 熔断

**Files:**
- Modify: `internal/gui/api_update.go`
- Modify: `internal/daemon/client.go`（`Ensure()` :34 / `EnsureCurrent()` :62 开头）
- Test: `internal/gui/api_update_test.go`、`internal/daemon/client_test.go`（若有）

- [ ] **Step 1: 写失败测试**

```go
func TestApplyCircuitBreaker(t *testing.T) {
    dir := t.Setenv OK_HOME 隔离
    // 写 ~/.openknowledge/update/.upgrading → Ensure() 应直接返回不拉起
    // 删掉 → 恢复正常路径（只断言熔断分支，不真起进程）
}
```

apply 本身不真跑安装器：把"执行安装器"抽成包级 `var runInstaller = func(path string) error {...}`，测试替换为记录调用的假实现，断言顺序：写熔断 → StopDaemon → runInstaller → 删熔断。

- [ ] **Step 2: 跑测试确认红**
- [ ] **Step 3: 实现**

`POST /api/update/apply`（仅 Windows，其他平台 400 `{err:"unsupported"}`）：

1. 校验下载任务 state=done 且 path 存在。
2. 先 `writeJSON(200, {ok:true})` 并 flush——之后再异步动手，保证前端能收到响应。
3. goroutine：写 `~/.openknowledge/update/.upgrading` → `daemonx.StopDaemon()`（:126，尽力）→ `exec.Command(path, "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART").Run()` 等待退出 → 删熔断标记 → `SpawnDetached` 拉起新 okd.exe → `os.Exit(0)`（当前旧 okd 退出，安装器已把它覆盖）。

`internal/daemon/client.go` 的 `Ensure()`/`EnsureCurrent()` 开头：

```go
if _, err := os.Stat(filepath.Join(registry.Home(), "update", ".upgrading")); err == nil {
    return nil // 升级安装进行中，不拉起
}
```

（按函数实际签名返回零值。）

- [ ] **Step 4: 跑测试确认绿** — `go test ./internal/gui/ ./internal/daemon/ -v`
- [ ] **Step 5: 提交** — `git commit -m "feat(gui): /api/update/apply 静默安装 + daemon 拉起熔断"`

---

### Task 5: web/app.js 其他页「版本升级」卡

**Files:**
- Modify: `web/app.js`

- [ ] **Step 1: i18n 键**（zh 字典约 :56 起、en 约 :330 起，`u*` 前缀双份）：

```
uVerCard:"版本升级"/"Version Update"
uVerCur:"当前版本"/"Current"
uVerLatest:"最新版本"/"Latest"
uVerCheck:"检查更新"/"Check for Updates"
uVerNone:"已是最新"/"Up to date"
uVerNew:"发现新版本"/"New version available"
uVerUpgrade:"立即升级"/"Upgrade Now"
uVerSkip:"跳过此版本"/"Skip This Version"
uVerLater:"知道了"/"Got It"
uVerDl:"下载中"/"Downloading"
uVerInstalling:"正在安装，程序将自动重启…"/"Installing, app will restart…"
uVerLinuxHint:"请下载对应包手动升级"/"Download the package to upgrade manually"
uSrvNewer:"服务端版本 {v} 高于客户端，建议升级客户端"/"Server version {v} is newer, consider upgrading the client"
```

- [ ] **Step 2: JS semver 比较 helper**（仿 Go 端 parseVersion/versionLess）：

```js
function verParse(s){const m=String(s||"").replace(/^v/,"").match(/^(\d+)\.(\d+)\.(\d+)/);return m?[ +m[1],+m[2],+m[3] ]:null;}
function verLess(a,b){for(let i=0;i<3;i++){if(a[i]!==b[i])return a[i]<b[i];}return false;}
```

- [ ] **Step 3: renderMisc 版本卡**：落点在使用帮助卡后、danger 卡前（约 :5133），照惯例裸 `{}` 块 + `el("div","pcard")` + h3 + pdesc + prow + `rightWrap()` + miscFbSpan/flashMiscFb：
  - 显示当前版本（`/api/status` 的 `app_version` 已有）/最新版本/检查更新按钮。
  - `update_available` 时：Windows 显示「立即升级」+ release body 用 renderMd 展示；Linux 显示 deb/tar.gz 链接 + 手动提示。
  - 点升级 → 确认弹窗（renderMd body）→ POST download → 照 `embPoll()/paintEmbDl()`（:4519/:4473）1s 轮询画进度条 → done 后 POST apply → 显示 uVerInstalling。
- [ ] **Step 4: 验证** — `node --check web/app.js` 无语法错误。
- [ ] **Step 5: 提交** — `git commit -m "feat(web): 其他页版本升级卡（检查/弹窗/进度下载/一键安装）"`

---

### Task 6: 启动自动弹窗 + 跳过版本 + 侧栏红点

**Files:**
- Modify: `web/app.js`

- [ ] **Step 1: 启动检查**：app.js 初始化流程里（token 就绪后）调 `GET /api/update/check`；`update_available && latest !== guiState.skipped_version`（skipped 由 check 响应透传或前端另取，优先让 check 响应带 `skipped_version` 字段——若 Task 2 未加，这里补上）→ 仿 `openUpgradeModal(title, entries)`（:5216，挂 document.body 不进 render 周期）弹三按钮：
  - 立即升级 → 跳 misc 页版本卡并触发下载流程；
  - 跳过此版本 → POST 一个写 skipped_version 的小端点（或并入 check 端点 `POST /api/update/skip {version}`，Task 2 文件里补，含测试）；
  - 知道了 → 关闭。
- [ ] **Step 2: 侧栏红点**：`MENUS.forEach` 构造导航处（:5385-5402），`b.innerHTML` 里当 key==="misc" 且有可用更新且未跳过时追加 `<span class="nav-dot"></span>`；CSS 加 `.nav-dot{width:6px;height:6px;border-radius:50%;background:#e5484d;display:inline-block;margin-left:6px;vertical-align:middle}`（style 在 web/style.css 或 app.js 内联处，按现状）。
- [ ] **Step 3: 验证** — `node --check web/app.js`；Go 侧 `go test ./internal/gui/ -run TestUpdateSkip -v`。
- [ ] **Step 4: 提交** — `git commit -m "feat(web): 新版本启动弹窗（升级/跳过/知道了）+ 侧栏红点"`

---

### Task 7: 服务器页 srvVer 高于客户端提示

**Files:**
- Modify: `web/app.js`

- [ ] **Step 1**: 服务器页渲染处取 `SRV.srvVer`（已透传），`verParse(srvVer)` 与 `verParse(appVersion)` 都有效且 `verLess(app, srv)` 时，在服务器页顶部状态区加一个提示 chip：t("uSrvNewer").replace("{v}", srvVer)，点击跳 misc 页版本卡。**仅**严格高于才显示（用户拍板）。
- [ ] **Step 2: 验证** — `node --check web/app.js`
- [ ] **Step 3: 提交** — `git commit -m "feat(web): 服务端版本高于客户端时服务器页提示升级"`

---

### Task 8: 托盘「检查更新」菜单

**Files:**
- Modify: `internal/tray/tray_windows.go`、`internal/tray/tray_other.go`（若有）
- Modify: `internal/daemon/run.go`（:129-133 接线处）
- Modify: `web/app.js`（启动 hash 解析 :6399-6409）

- [ ] **Step 1: 托盘**：`Tray` struct（tray_windows.go :114）加 `onCheckUpdate func()`；菜单在"版本"和"退出"之间加「检查更新 / Check for Updates」项，点击回调。`Run(ctx, version, openGUI, onQuit)` 签名加参数——同步改 `tray_other.go` 的等价签名与所有调用点。
- [ ] **Step 2: 接线**：run.go 处 `onCheckUpdate = func() { OpenBrowserFunc(info.URL() + "/#token=" + info.Token + "&go=misc") }`（index.html :14-17 的正则 `[#&]token=([^&]+)` 只吃 token，go 参数会留在 hash 里）。
- [ ] **Step 3: app.js 启动解析**（:6399-6409）：现有 `raw.replace(/^#\/?/,"")` split 逻辑扩展认 `go=misc`（以及 `token=..&go=misc` 形态），落到 misc 页并触发一次版本卡检查。
- [ ] **Step 4: 验证** — `go build ./...` + `node --check web/app.js`
- [ ] **Step 5: 提交** — `git commit -m "feat(tray): 托盘菜单加检查更新，开 GUI 直达版本卡"`

---

### Task 9: 全量验证 + changelog + 提交推送

- [ ] **Step 1:** `go test ./...` 全绿
- [ ] **Step 2:** `node --check web/app.js`
- [ ] **Step 3:** `go build ./...`（含 windows 目标可选 `GOOS=windows go build ./...` 验证 tray_windows.go 编译）
- [ ] **Step 4:** 在 `docs/changelogs/` 当前版本 changelog 追加功能记录（中英按现有格式）
- [ ] **Step 5:** 提交 changelog 并推双远端：`git push origin master && git push github master`（凭据 `git credential fill`）
- [ ] **Step 6:** 向用户报告完成，列出可手测清单：其他页版本卡、伪造高版本 tag 的启动弹窗、跳过版本持久化、红点、托盘检查更新、Windows 实机升级流程
