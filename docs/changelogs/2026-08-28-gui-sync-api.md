# GUI 同步 API（六端点 + 项目列表带同步状态）

日期：2026-08-28

okd 新增个人多端同步的 HTTP 端点（设计文档 §11.4）：`POST /api/project/sync` 触发一次同步（未建仓且带 remote 时先走三情形初始化，远端非空返回 409 指引手动合并）；`GET /api/project/sync/status` 返回 personal 层状态（enabled/is_repo/ahead/behind/conflict/last_sync/last_error/冲突文件列表）；`GET /api/project/sync/conflict-file` 返回冲突文件的 base/local/remote/working 四版本全文；`POST /api/project/sync/resolve` 按 me/theirs/merged 落盘并 git add；`POST /api/project/sync/finish` 校验全部解决后 rebase --continue + push；`POST /api/project/sync/abort` rebase --abort 回滚到同步前状态（本地内容不丢）。全部走 syncx single-flight，sync-conflict.json 由 syncx 独占写。

`/api/projects` 与 `/api/status` 的项目对象新增 `sync` 字段（enabled/is_repo/ahead/behind/conflict），fail-open——状态读取失败一律给零值，不影响列表接口。

同随修复：`config.SetSync` 重写 [sync] 段时保留段内手写的 `llm_assist` 行（此前整段重写会静默丢键）；`Sync.LLMAssist` 显式非空时按值重写。
