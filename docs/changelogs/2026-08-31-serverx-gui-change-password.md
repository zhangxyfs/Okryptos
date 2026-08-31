# serverx 客户端 + GUI 转发：自助改密接线

日期：2026-08-31

自助改密链路在客户端侧闭环。`serverx.MeInfo` 新增 `must_change_password` 字段解析 me 响应中的强制改密标记；`Client` 新增 `ChangePassword` 方法调 `POST /api/v1/change-password`，服务端错误（401 旧密码错误、403 must_change_password 等）以既有 `*Error` 原样透传状态码与 message。GUI 本地端点 `POST /api/server/change-password` 转发之：请求体同契约 `{old_password, new_password}`，成功 204，错误经 `writeServerErr` 原样透传——前端据 403 弹强制改密框。

TDD 开发：`TestChangePassword`（serverx：401 透传 / 成功 / me 标记解析）与 `TestApiServerChangePassword`（GUI：401 透传 / 403 must_change_password 透传 / 成功 204）先跑确认变红（serverx 编译失败、GUI 404），实现后两包全量测试通过，`go vet` 干净。
