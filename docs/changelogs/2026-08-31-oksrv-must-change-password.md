# okserver 强制改密存储地基：users 表 must_change_password 列 + 会话删除方法

日期：2026-08-31

`internal/oksrv` 存储层为"强制改密"功能打地基：users 表新增 `must_change_password INTEGER NOT NULL DEFAULT 0` 列；存量旧库由 `OpenStore` 自动补列迁移（`PRAGMA table_info` 检测 + `ALTER TABLE ADD COLUMN`，老行默认 0，重开幂等）。`User` 结构体新增 `MustChangePassword bool`，`scanUser` 与四处 SELECT（`getUserByID`/`GetUser`/`ListUsers`/`SessionUser`）同步加列。新增两个方法：`SetMustChangePassword` 置/清改密标记（初始/重置密码置 1，自助改密成功清 0）；`DeleteUserSessionsExcept` 删除用户除指定会话外的全部会话，`keepTokenHash` 传空串即删全部（管理员重置/reset-root 场景）。后续任务将在此之上做标记置位与 HTTP 拦截，本次不含行为变化。

TDD 开发：迁移测试（手工构造六列旧库→OpenStore 自动补列→老行默认 0→标记读写→重开验证幂等）与会话删除测试（保留指定会话/删其余/空 keep 删全部）先行，先编译失败确认变红，实现后全包 15 个测试一次通过。
