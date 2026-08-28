# syncx 提交/推送/克隆（Status/CommitAll/Push/CloneToDir）

日期：2026-08-28

个人多端同步 P1：syncx 补齐工作区状态与远端交互原语。`Status()` 返回 dirty 与 ahead/behind 计数（`rev-list --left-right --count @{upstream}...HEAD`，左=behind 右=ahead；无 upstream 或失败一律 fail-open 零值，状态展示不影响主链路）。`CommitAll(msg)` 暂存全部变更、有内容才提交，提交身份内置（`OpenKnowledge Sync <sync@openknowledge.local>`），不依赖用户全局 git 配置。`Push()` 无 upstream 时自动 `push -u origin <branch>` 建立跟踪，已有 upstream 走普通 push。`CloneToDir(url)` 支持往含骨架文件（config.toml/state/ 等）的目录克隆：clone 到临时目录 → 移 `.git` 进目标目录 → `checkout -- .` 覆盖工作区（设计文档 §14"无知识内容"情形）；clone 与 checkout 强制 `core.autocrlf=false`，Windows 全局 autocrlf=true 时也能逐字节还原 LF 内容。

同期修正：`Repo.Init()` 现在会在写完 `.gitignore` 后生成初始提交（"sync: init"），原地成仓即保持工作区干净，避免初始化后的 `.gitignore` 被误计为未提交变更。
