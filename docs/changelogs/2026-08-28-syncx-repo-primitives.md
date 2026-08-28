# 个人多端同步：syncx Repo 基础原语（init/remote/branch）

日期：2026-08-28

`internal/syncx` 增加 `Repo` 仓视图原语——同步引擎对项目数据目录 git 仓的最小操作面，后续提交快照、远端推拉任务都建立在这组方法上：`Open(dir)` 取视图（不校验）、`IsRepo()` 判定是否已成仓、`Init()` 原地成仓（`git init -b main` + 写 `.gitignore`，索引/状态/日志不入仓，文件不挪动）、`RemoteURL()`/`SetRemote(url)` 读写 origin（无 remote 返回 `""`，SetRemote 无则 add 有则 set-url，重复调用覆盖不报错）、`CurrentBranch()` 返回当前分支（unborn HEAD 尚无提交时返回 `main`）。

所有 git 调用走 Task 1 的 `execGit` 入口，本地操作沿用 10s `localTimeout`。同日移交修复：`execGit` 的 env 追加 `LC_ALL=C` 固定 C locale，防本地化 git 输出导致输出解析与测试断言漂移。

当前仅基础设施，无用户可见行为变化。

测试：`TestRepoLifecycle` 单用例走完整生命周期——空目录非仓 → Init 成仓 → 分支为 main → `.gitignore` 含 `kb.db`/`kb.db-*`/`state/`/`*.log` 四条 → remote 空 → SetRemote 设置与覆盖更新均生效。`go test ./internal/syncx/ -v` 全绿（含 Task 1 的 4 个用例），`go vet` 干净。
