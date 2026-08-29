# syncx 冲突解决原语

日期：2026-08-28

syncx 新增冲突解决原语（设计文档 §5，供 GUI 冲突页使用，CLI 不消费）：`ConflictFiles`（列出冲突文件，非冲突态返回 nil）、`ConflictVersions`（取 base/local/remote 三版本）、`ResolveFile`（写入解决结果并 git add）、`ContinueRebase`（`-c core.editor=true` 行内配置防唤起编辑器挂起）、`AbortRebase`、`MergeInProgress`。

纪律：`pull --rebase` 冲突期间 stage 语义反转——`:1:`=base（共同祖先）、`:2:`=远端（刚拉下来的）、`:3:`=本地（正在 replay 的提交）。`ConflictVersions` 对外只暴露用户语义（local=`:3:` 本机改动、remote=`:2:` 远端改动），严禁直译 git ours/theirs；测试含专项断言防回退。

实现注记：`MergeInProgress` 不能用 `rev-parse --verify REBASE_HEAD`——rebase 成功结束后该引用仍残留（仅 `--abort` 会清除），会误判为仍在进行中。实际以 `rebase-merge`/`rebase-apply` 状态目录（rebase）与 `MERGE_HEAD`（merge）的存在性为准。
