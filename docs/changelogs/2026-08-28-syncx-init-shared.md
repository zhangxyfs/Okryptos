# sync init 三情形编排下沉 syncx（cli/gui 共用）

日期：2026-08-28

`ok sync init` 的三情形编排（仅本地历史 / 无知识内容克隆 / 首台设备初始化推送）从 cli 下沉到 `syncx.InitForSync`，GUI 可直接复用同一编排，无需重抄 git 调用序列。编排按 `InitKind`（InitLocalOnly/InitCloned/InitPushed）回报走了哪条路径；两边各自初始化过且远端已有内容时返回哨兵错误 `ErrRemoteNotEmpty`，不自动合并——仓此时已 init+commit+关联 remote，用户按指引手动合并一次后 `ok sync` 即可续上。cli 的 syncInit 改写为薄壳：判定 hasContent（knowledge/ 下有无 .md）→ 调编排 → 按 kind/错误出文案 → 写 [sync] 配置，行为与 P1-A 版本兼容（E2E 双设备/冲突回归全绿）。

config 新增 `[sync] llm_assist` 字段：off|local|server，空 = auto（有本地 LLM 则 local，否则 off）；server 档本期不开放（okserver 管理面无 LLM，设计文档 §11.2）。

同日加固（上一任务评审移交）：execGit 环境追加 `GIT_EDITOR=true`（env 优先级高于 core.editor，daemon 从导出过 GIT_EDITOR 的 shell 继承环境时 rebase --continue 也不会挂编辑器）；`MergeInProgress` 的状态路径判定经 `rev-parse --git-path` 取并按 `filepath.IsAbs` 处理绝对路径，linked worktree（`.git` 为文件）下不再假阴。
