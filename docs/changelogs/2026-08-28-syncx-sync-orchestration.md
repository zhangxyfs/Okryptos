# syncx Sync 编排、冲突检测与 single-flight

日期：2026-08-28

`internal/syncx` 补齐一次同步的完整编排（设计文档 §7）：`Repo.Sync(msg)` = add -A → 有变更则 commit →（有远端）pull --rebase → push，返回结构化 `Outcome`（Committed/Pulled/Pushed/Conflicts/Err/NoRemote/NotRepo）。冲突不是错误——`PullRebase` 检出 rebase 半途（REBASE_HEAD 存在）时返回冲突文件列表且 err=nil，`Sync` 随即停止 push，工作区保留本地改动待人解决。`SyncOnce(dir, msg)` 提供包级 single-flight：同 dir 并发调用合并为一次执行，后到者等待并共享结果标志（Err/Conflicts 等），计数（Committed/Pulled/Pushed）归唯一执行者，并发聚合不重复计数。

实现要点：Sync 在 Status 前先 `fetch origin`（失败忽略）——behind 计数读的是本地远程跟踪引用，不 fetch 看不到别人新推的提交，Pulled 会恒为 0；真网络错误由后续 pull 暴露并落入 `Outcome.Err`。

随附修复（先行提交）：`CloneToDir` 临时目录改到目标目录同卷（`filepath.Dir(r.Dir)`），修 Windows 上数据目录在 D: 而系统临时盘在 C: 时 `os.Rename` 跨卷必失败；移入 .git 后仓级 `config core.autocrlf false`，行尾行为不再依赖全局配置，后续 CommitAll/Status 行为恒定。
