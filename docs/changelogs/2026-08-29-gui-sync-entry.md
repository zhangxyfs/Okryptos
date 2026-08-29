# GUI 同步入口：toast 基建 + 带参 hash 路由 + 管理页同步按钮与状态点

日期：2026-08-29

管理页项目行新增同步入口（P1-B 前端第一块）：每行常显循环箭头按钮 + 四态色点（绿=已同步 / 黄=有未同步变更 / 红=冲突 / 灰=未启用），色点数据源为 `/api/projects` 项目对象内嵌的 `sync` 字段（fail-open，读不到即灰）。点击按钮即执行 `POST /api/project/sync`：转圈防连点、结果走底部 toast（后端人话 message 直出），冲突时置 `state.syncConflict` 并把 hash 写为 `#/sync-conflict?project=<name>`，冲突页渲染器由后续任务提供（分发链带 `typeof` 存在性守卫，中间态回退管理页不白屏）。未初始化项目点击后给确认提示（建仓入口属后续版本，本期仅指向 `ok sync init`）。

基建：`toast(msg, isErr)` 底部居中胶囊、2.5s 自消，供一切动作类按钮复用；hash 路由升级为带参解析（`#/name?query`），启动 IIFE 可经 hash 读回冲突页会话态。中英 I18N 双写同步键全量就位（含 Task 6/7 预留的 syncToastPull/Push/Local 与 syncNavConflict）。
