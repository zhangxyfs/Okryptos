# 配置 [sync] 段与 SetSync 行级写入

日期：2026-08-28

`internal/config` 新增项目级 `[sync]` 配置段（个人多端同步）：`enabled`（开关，默认 false）、`remote`（远端仓库 URL，默认空）、`auto_interval_min`（自动同步间隔分钟，默认 5）。`Default()` 登记默认值，`LoadMerged` 走既有"默认 ← 全局 ← 项目"字段级合并，项目未覆盖的字段继承全局。

配套新增 `SetSync(path, Sync)`：照 `SetEnforceRules` 先例的行级小节读-改-写，fsx 文件锁内完成——重写既有 `[sync]` 段（幂等，不重复追加段头），无段时在文件末尾追加，其余内容（含注释）原样保留，供 CLI/daemon 写配置使用。
