# 2026-09-18 同步引擎：空远端首推死锁自愈（Orosus 实证）

## 现象与根因

用户报同步 toast：`git 退出码 1: Your configuration specifies to merge with the ref
'refs/heads/main' from the remote, but no such ref was fetched.`。追查（Gitea API
`updated_at==created_at`、本地 reflog/config birth 时间线）定案为两个 bug 叠加：

1. **CloneToDir 克隆空仓必败**：`InitForSync` 克隆路径（本地无知识内容时）对刚建的
   空远端 clone 后执行 `checkout -- .`，unborn HEAD 上必然报 pathspec 错——初始化
   报"失败"，但 `.git` 已移入，仓处于"已克隆、零提交"中间态；
2. **Sync 首推死锁**：编排为 commit → pull --rebase → push，空远端 pull 必报
   "no such ref was fetched"，引擎在 pull 失败处直接返回——push 永远执行不到，
   远端永远等不来第一次推送，每次同步报同一错误，本地提交无限堆积。

附证伪：远端从未"变成 master"——Gitea `default_branch=main`、零分支、从未收到
推送；空仓页面示例命令里的 `git push -u origin master` 是文案不是仓库状态。

## 改动

- `syncx/sync.go`：pull 报"远端缺跟踪分支"（`no such ref was fetched`，空仓首推/
  远端仓重建）或"无跟踪信息"（`no tracking information`，旧版 git 克隆空仓不写
  `branch.*`）时降级为直接推——`Push` 走 `push -u` 在远端重建分支与跟踪，一次同步
  自愈；首推计数取全部本地提交（上游引用尚不存在时 `Status` 算不出 ahead）。
  认证/网络等其余错误仍原样上报。新增 `isMissingUpstream` 判定（execGit 固定
  LC_ALL=C，文案稳定）；
- `syncx/repo.go`：`CloneToDir` 在 unborn HEAD（空远端）跳过 checkout——没有内容
  可还原，克隆初始化不再误报失败。

## 测试

- `sync_test.go` 新增 3 例：`TestSyncEmptyRemoteFirstPushHeals`（克隆空仓 → 本地
  首提 → 一次 Sync 推出全部历史并在远端建 main → 二次 Sync 恢复常态）、
  `TestSyncHealsWipedRemoteBranch`（远端 main 被清后一次 Sync 重推回去）、
  `TestIsMissingUpstream`（表驱动：认证/网络/non-fast-forward 不误判）；
- `go test ./internal/syncx/ ./internal/gui/ -count=1` 全绿，`go vet`、
  `go build ./...` 通过；ARCHITECTURE.md `Sync` 条目补自愈行为。
