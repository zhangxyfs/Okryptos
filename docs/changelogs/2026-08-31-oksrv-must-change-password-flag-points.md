# okserver 强制改密置位点：初始/重置密码置标记，重置踢掉目标用户全部会话

日期：2026-08-31

在上一篇存储地基之上，四个置位点接线完毕：`EnsureRoot` 首启建 root 后置 `must_change_password=1`（初始 root 密码强制首登改密）；`ResetRoot` 重置 root 密码时同样置标记，并调 `DeleteUserSessionsExcept` 踢掉 root 全部旧会话（旧会话持有者不得沿用旧凭据）；管理面 `apiUserCreate` 建用户（随机或自选密码）一律置标记；`apiUserResetPassword` 管理员重置时置标记并踢掉目标用户全部旧会话。重置端点的会话清理由 HTTP 层在改密成功后执行，root 侧由 `ResetRoot` 存储层自带。本任务只把标记写对，HTTP 拦截（强制改密 gate）在后续任务上线，存量管理流行为不受影响。

TDD 开发：先给 `TestPasswordAndRoot`、`TestResetRoot`、新增 `TestUserCreateAndResetSetFlag`（建用户随机/自选密码置标记、双会话、清标记后重置、重置后旧会话全失效且新密码可登录）写断言，聚焦跑确认三处变红；实现后 `internal/oksrv` 全包 16 个测试一次通过。
