# GUI 三向合并编辑器：marker 块采纳三栏

日期：2026-08-29

P1-B 前端最后一块：三向合并编辑器落地（仿 Android Studio 三栏，对 §11.3 收敛的 v1 简化）。冲突页卡片点 Merge（或 AI 合并成功后）进入：`state.merge = {project, file, ai?}`，renderBody 分发链 merge 分支优先。左/右栏只读展示 local/remote 全文，中间栏可编辑 textarea（预填 working——git 自动合并结果）；冲突块以 git 冲突标记（`<<<<<<<`/`=======`/`>>>>>>>`）解析（parseConflictBlocks），不做行级 LCS diff。每个标记块给「采纳远端 / 采纳本地」按钮，全部采纳后「应用合并结果」解禁，应用 = applyMergeBlocks 按行号替换块 + `POST resolve {action:"merged", content}`，成功后回冲突页卡片标已解决。反转纪律：标记块上半（HEAD 侧）= 远端，下半 = 本地——与 rebase 冲突现场实测一致。

三条随本编辑器落地的防线：

- **分发截胡修复**：冲突页 Merge/AI 合并点击处设 `state.merge` 的同时清 `state.syncConflict`（CF 缓存保留）；取消/应用返回时恢复 `state.syncConflict = {project}`，回冲突页 resolved 标记不丢。renderBody 里 merge 分支提到 syncConflict 分支之前，双保险。
- **退化判断**：用户手编可能让块行号漂移——apply 前校验每个块的标记行仍在原位（`working.split("\n")[b.start].startsWith("<<<<<<<")`），失配则 finalText 直接取 textarea 全文，以最终文本为准。
- **取数兜底**：renderMerge 拉 conflict-file 失败（如已解决卡上再点 Merge，后端 409）时 toast 报错并回冲突页，不再 unhandled rejection 留白。

滚动保持同 renderSyncConflict 先例：`.mgwrap` 进 render() 的 keep 选择器，async 挂载后消费 `render._keep` 自行恢复。AI 合并进入（`m.ai != null`）时中间栏预填 AI 文本、无块、标题条显示「未经确认不落盘」提示。Windows 实测：git 写出的冲突文件为 CRLF，块解析/采纳替换对 `\r` 行尾无损（真实 daemon 端到端验证：采纳远端 → 落盘 → finish → push 全通）。
