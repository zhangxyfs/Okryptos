# okd 同步 ticker

日期：2026-08-28

daemon 挂同步 ticker（`startSyncJanitor`）：每分钟检查一轮注册表，对 `[sync] enabled = true` 且到 `auto_interval_min` 点的项目执行一次 `SyncOnce`（commit → pull --rebase → push），结果经 `RecordOutcome` 回写 `state/sync-status.json`（personal 层 last_sync/ahead/behind/冲突标记）。同步失败仅向 daemon stdout 记一行日志，绝不影响 hooks 本地链路；冲突未解决时 syncx 自身守卫（返回 Conflicts 不吞标记），ticker 只记"待人工解决"。

`runSyncCycle(out, force)` 的 force=true 路径跳过 interval 判断，留给写入防抖触发（设计文档 §8，后续任务复用）；检查周期是包级 var `syncCheckInterval`，测试可调小。
