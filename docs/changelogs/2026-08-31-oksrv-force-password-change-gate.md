# okserver 强制改密 HTTP 层：gate 中间件 + 自助改密端点 + 标记下发

日期：2026-08-31

强制改密链路在 HTTP 层闭环。新增 `gate` 中间件：带 `must_change_password` 标记的会话访问非白名单端点一律 403 `{"error":"must_change_password"}`，客户端据此弹强制改密框；白名单只有 `GET /api/v1/me`（客户端拿标记）与新端点 `POST /api/v1/change-password`，注册时不套 gate，其余已认证端点（含全部管理端点与个人建仓）全部拦截。自助改密端点凭旧密码换新密码：旧密码错误 401 且计入登录限流（与 login 共用同一 `LoginLimiter`，防在线爆破）；新密码不足 8 位或与旧密码相同 400；成功后清标记、踢掉除当前会话外的全部会话（`bearerTokenHash` 从请求头算出当前 token 的库存哈希做保留项，客户端无需重登）、落 `change-password` 审计。login 与 me 响应新增 `must_change_password` 布尔字段下发标记。

存量测试适配是预期设计而非绕过：gate 上线后带标记的 root/alice/op1 会话会被拦截，故 `newTestServer` 建 root 后清标记（存量管理流默认"root 已自助改密"），`TestFullManagementFlow`、`TestAdminWriteEndpoints` 两处对 alice/op1 同样清标记；强制改密行为本身由新增的 `TestForcePasswordChangeFlow`（全链路：置标记 → 403 拦截 → 白名单放行 → 改密错误分支 401/400/400 → 成功 → 标记清除、端点恢复、旧密码失效）与 `TestChangePasswordKeepsCurrentKicksOthers`（改密保当前会话、踢其他会话）钉住。

TDD 开发：两个新测试先跑确认变红（login 响应缺字段 / 端点 404），实现后 `internal/oksrv` 全包 18 个测试一次通过，`go vet` 干净。
