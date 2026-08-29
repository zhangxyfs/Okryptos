# okserver GitBackend 接口与 fake 实现

日期：2026-08-29

okserver 管理面新增 git 托管后端抽象：`GitBackend` 接口（`internal/oksrv/gitbackend.go`）覆盖用户/令牌/启停、个人库与组织库创建、组织与成员管理、仓库存在性查询十项操作，返回 `GitRepo{Owner, Name, CloneURL}` 三元组；`ErrBackendDown` 表达后端不可达，响亮失败、不静默 fallback。配套内存态 `FakeBackend`（`internal/oksrv/gitbackend_fake.go`，仅供测试与离线开发）镜像真实后端失败语义：CreateUser/CreatePersonalRepo 重名拒绝，AddOrgMember 对未知 org/未知用户拒绝，`SetDown(true)` 后全部方法返回 `ErrBackendDown`；clone URL 形如 `http://gitea.fake/<owner>/<name>.git`。fake 刻意不进 `_test.go`，供 API 层集成测试跨文件复用。

顺手修：`go mod tidy` 将直接依赖 `golang.org/x/crypto` 从误标的 `// indirect` 挪正（Task 2 遗留）。
