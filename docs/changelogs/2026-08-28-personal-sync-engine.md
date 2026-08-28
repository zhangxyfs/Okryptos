# 个人多端同步引擎（P1-A）

日期：2026-08-28

新增个人多端同步引擎：项目知识库可经 git 在多设备间同步。`internal/syncx` 叶子包以参数数组 exec 系统 git（不走 shell、GIT_TERMINAL_PROMPT=0 防挂起、commit 身份内置），提供仓库原语与 Sync 编排（add -A → commit → pull --rebase → push），包级 single-flight 合并并发触发。`ok sync` / `ok sync init` 命令覆盖三情形初始化（clone / 首台设备推送 / 双有内容报错指引手动合并）；okd 每分钟检查一轮、按项目 `auto_interval_min` 到点同步，条目写入后 30s 防抖补一轮，把未同步窗口压到分钟级。冲突时停止推送、写 `state/sync-conflict.json` 并指引 GUI 冲突页或手动解决；同步状态按层建模落 `state/sync-status.json`（为团队版 `[sync.personal]` 预留）。项目级 `config.toml` 新增 `[sync]` 段（enabled/remote/auto_interval_min，`config.SetSync` 行级写入）。失败一律 fail-open，仅记日志与状态文件。E2E 覆盖双设备闭环（clone 后检索命中，验证 mtime 重建链路）与冲突路径（报冲突、写状态、内容不丢）。GUI 同步按钮/冲突解决页与 okserver 服务端为后续阶段。
