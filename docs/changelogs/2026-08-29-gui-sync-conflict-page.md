# GUI 冲突解决页：卡片流 + 顶部钉住操作条

日期：2026-08-29

P1-B 前端第二块：冲突解决页落地。`state.syncConflict` 命中时整页渲染为卡片流——每个冲突文件一张卡，卡头文件名 + 解决状态标记，卡体并排「本地版本 / 远端版本」前 10 行等宽预览，卡脚四按钮：Accept Me / Accept Theirs / Merge / AI 合并（409 no_llm 时弹错误 toast 并恢复按钮）。顶部操作条 `.cf-stick` 以 `position:sticky` 钉在滚动容器顶：标题、实时进度「已解决 n / N」、完成同步（未全部解决时禁用并 title 注明原因）、放弃本次同步（原生 confirm 二次确认）。完成 = `POST finish`（rebase --continue + push），放弃 = `POST abort`（rebase --abort，本地内容不丢），两者成功后回管理页并刷新状态点。

会话缓存 `CF = {list, resolved, versions, project}`：整页重渲（render 外壳）重走 renderSyncConflict 时项目相同不重拉列表，`conflict-file` 结果按文件缓存进 `CF.versions`，解决卡片触发的重渲不再逐卡重拉（实测 12 卡重渲后 conflict-file 请求数不翻倍）。

两处随本页落地的修正：侧栏菜单点击补 `state.syncConflict=null; state.merge=null` 逃生口（冲突页上点侧栏不再被分发链弹回）；`doProjectSync` 防连点从按钮 spinning class（轮询重渲换节点后丢失）改为模块级 `syncInflight` 集合。冲突页保留侧栏（renderSyncConflict 只清 main 内容区不清 `.side`），红点（sync 状态点 conflict 态）可点击直达冲突页。三向合并编辑器（Merge/AI 合并的落点，state.merge 入口已产出）属下一任务。
