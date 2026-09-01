# 默认模型目录不可写时回退 OK_HOME/models

日期：2026-09-01

修复 .deb 安装（`/usr/lib/openknowledge/` 为 root 所有）下内置 embedding 模型下载必报 `permission denied` 的问题（Linux 实证：向导页下载 610MB 模型失败）。根因：`DefaultModelsDir()` 写死「exe 所在目录/models」，未考虑系统目录安装普通用户不可写。

现在默认目录按可用性决策：`<exe>/models` 可写（探测写临时文件）或已装有模型（含 .gguf，只读也用，防遮蔽已装模型导致重复下载）→ 沿用；否则回退 `<OK_HOME>/models`（`~/.openknowledge/models`，用户可写）。显式配置 `models_dir` 的行为不变（配置优先）。

测试：`TestDefaultModelsDirFrom`（可写 exe 目录跟随 exe / 不可写回退 OK_HOME / 已装模型不切换 三情形）。
