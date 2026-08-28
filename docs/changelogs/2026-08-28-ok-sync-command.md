# ok sync / ok sync init 命令

日期：2026-08-28

新增 `ok sync` CLI（个人多端同步的手动入口，设计文档 §7）：一次执行 = commit → pull --rebase → push，薄壳编排 `syncx.SyncOnce` + `RecordOutcome` 回写状态。输出按 Outcome 分情形：冲突列文件并指引 GUI 冲突页或 `rebase --continue`；真错误原样打印并注明"本地功能不受影响"；无远端退化为仅本地历史。未启用（`[sync] enabled = false`）时提示先 init 并返回 1。

`ok sync init [remote-url]` 覆盖三情形（§14）：remote 为空 → 仅本地历史（init + 首个提交）；无知识内容（knowledge/ 下无 .md）且 remote 非空 → clone 进项目数据目录；本地有内容且 remote 非空 → init + commit + 关联远端 + 推送（首台设备路径），推送遭 non-fast-forward 时报错并指引手动合并，不自动合并。成功后经 `config.SetSync` 写 enabled/remote（auto_interval_min 保留原值）。

注册三处同步：`cmd/ok/main.go` 分发 case、`internal/daemon/forward_cli.go` 的 `cliSubcommands`（okd 兼容转发）、`internal/cli/cli.go` 实现。
