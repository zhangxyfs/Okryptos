# syncx 状态文件（sync-status.json 分层 + sync-conflict.json）

日期：2026-08-28

同步状态落盘：`state/sync-status.json` 按层建模（personal/team 从第一天就分层，`LayerStatus` 记 last_sync/ahead/behind/conflict/last_error），`state/sync-conflict.json` 记未决冲突文件列表与起始时间（GUI 冲突页数据源，syncx 独占写）。两个文件均经 fsx.WriteFile 原子写；读取侧 fail-open——文件不存在或损坏返回空骨架，不让状态文件问题阻断同步主流程。

`RecordOutcome(dir, stateDir, o Outcome)` 是 CLI/daemon 共用的状态回写收敛函数：成功则落 last_sync、清 conflict 与冲突文件；冲突则置 conflict 并写 sync-conflict.json；真错误只记 last_error、不动冲突态；每次回写顺带刷新 ahead/behind。回写失败仅尽力而为，不向上抛错。

同日移交修复：`Sync` 入口新增 rebase 守卫——冲突未解决（REBASE_HEAD 存在）时再次 Sync 直接返回未决冲突列表，不做任何提交/拉取；此前会把冲突标记 add -A 提交进历史。
