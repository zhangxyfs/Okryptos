# 客户端版本升级：版本检查 + 一键升级 + 启动弹窗 + 托盘入口

日期：2026-09-02

客户端内置完整的「发现新版本 → 一键升级」闭环，全部改动集中在 `internal/gui/api_update.go`（新增 438 行）与 `web/app.js`。版本检查走 GitHub Releases API，结果本地缓存 6 小时（`updateCheckTTL`），请求失败/超时/解析失败一律 fail-open 返回 200 + `update_available:false`，绝不阻塞 GUI 任何页面。

其他页（非服务器页）顶栏新增版本卡：显示当前版本，点「检查更新」命中新版本后弹出升级弹窗，支持进度下载（安装器下载任务走 `.part` 断点续传，Range/206 优先、200 降级，前端轮询快照渲染进度条）与一键安装。Windows 上 `POST /api/update/apply` 触发静默覆盖安装：apply 序列移出 okd 进程，由独立安装器收尾并重新拉起 okd；拉起失败走熔断自愈（标记文件 `upgradeMarkPath` 判定），避免升级后守护进程起不来变砖。`/api/update/download` 与 `/api/update/apply` 均带并发防护，重复点击安全。

GUI 启动时若有新版本且未跳过，弹「升级 / 跳过此版本 / 知道了」三键弹窗：跳过低版本经 `POST /api/update/skip` 持久化，之后同版本不再打扰；有新版本未处理时侧栏「其他」菜单挂红点提示。服务器页检测到服务端版本高于客户端时提示升级客户端，避免新旧协议错配。Windows 托盘菜单新增「检查更新」项，点击直接开 GUI 并直达版本卡。

测试：`internal/gui/api_update_test.go`（712 行）覆盖检查缓存与 fail-open、下载续传路径（Range/206/200 降级）、apply 并发防护与熔断标记、跳过版本持久化；`internal/gui/changelog_test.go`、`internal/daemon/client_test.go` 补配套断言。前端无测试框架，验证靠 `node --check web/app.js` 与全量 `go test ./...`、`GOOS=linux go build ./...`（tray_other.go 路径）；Windows 实机升级流程按简报清单人工走查。
