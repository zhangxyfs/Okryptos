# GUI POST /api/project/attach：空壳项目补挂工作目录端点

日期：2026-09-01

服务器拉取与备份恢复会注册空 Paths 的壳项目，用户在本机打开对应工作目录时 hooks 的 `FindByCwd` 不命中、注入静默失效。Task 1 已落注册表 `AddPath`，本次接上 GUI 端点 `POST /api/project/attach`：请求 `{project, path}`，成功 200 返回 `{"name", "paths"}`；空路径/目录不存在 400，项目未注册 404，路径已挂其他项目 409，锁/IO 失败 500。前端项目页据此给壳项目提供「关联工作目录」入口（后续任务接入）。

实现复用既有助手：请求体走 `decodeJSON`（4MB 上限），错误/成功走 `writeErr`/`writeJSON`，写操作一律 `registry.Update`（跨进程文件锁），响应的 paths 经 `findProject` 现读注册表返回。`AddPath` 的两类哨兵用 `errors.Is` 映射 404/409。

一处与计划稿的偏差：`AddPath` 采用两阶段扫描（先全量判冲突再找目标项目），直接调用时「未知项目 + 路径冲突」会报 409 而非契约要求的 404。端点在 `Update` 闭包内先做项目存在性检查（未注册即返回 `ErrProjectNotFound`）再调 `AddPath`，保证未知项目一律 404，且全程仍在同一把锁内、无 TOCTOU 窗口。

TDD 开发：`TestApiProjectAttach`（补挂成功 + FindByCwd 命中 / 幂等重入不重复 / 404 未知项目 / 400 空路径与不存在目录 / 409 跨项目冲突）先跑确认变红（路由未注册 → 404），实现后聚焦测试与 `internal/gui` 全包通过，`go build ./... && go vet ./...` 干净。路径比较依赖 `NormalizePath` 的分隔符统一，Windows/Linux 双平台可跑。
