# okserver 密码修改与强制改密设计

> 日期：2026-08-31　状态：已实施
> 背景：okserver 已有用户体系与管理员重置密码，但缺自助改密；初始/重置密码没有"必须改掉"的安全语义。

## 1. 背景与目标

现状（`internal/oksrv/`）：

- `users` 表无"强制改密"字段；登录 `POST /api/v1/login` 返回 `{token, user:{name, role}}`。
- 已有管理员重置 `POST /api/v1/users/{name}/reset-password`（`internal/oksrv/http.go:288`），生成随机密码一次性返回明文。
- **没有任何自助改密端点**；okserver 无自带 Web 页面，管理 UI 在客户端本地 GUI（`web/`），经 `internal/gui/api_server.go` 的 `/api/server/*` 代理 + `internal/serverx` 客户端调 okserver。

目标：

1. 用户（含 root）用初始密码登录后，**必须**重设新密码才能继续用任何功能。
2. root/admin 重置某用户密码后，该用户下次登录同样被强制改密。
3. 任何已登录用户可随时自助修改自己的密码。

### 已定决策（用户拍板）

| 决策点 | 结论 |
|---|---|
| 拦截层 | 服务端强制拦截：带标记会话除白名单端点外全部 403 |
| root 范围 | root 初始密码同样强制改（含 `reset-root` 运维重置后） |
| 会话处理 | 改密/被重置成功后踢掉该用户其他会话；改密者当前会话保留 |
| 客户端范围 | 只做 GUI 页面；CLI 收到 403 时给明确报错文案，不做 CLI 交互改密 |
| 实现方案 | 布尔标记列 + 中间件拦截（不做 password_version 代际，YAGNI） |

## 2. 服务端设计（`internal/oksrv/`）

### 2.1 schema 迁移

- `users` 表加列：`must_change_password INTEGER NOT NULL DEFAULT 0`（`store.go:24-31` 建表语句同步更新）。
- 存量库迁移：`OpenStore` 时用 `PRAGMA table_info(users)` 检测列是否存在，缺失则 `ALTER TABLE users ADD COLUMN ...`（幂等，不动 meta 表）。
- `User` 结构体（`store.go:106-112`）加 `MustChangePassword bool`，`json:"must_change_password"`。
- 存储层新增（两个独立小方法，不动现有 `SetUserPasswordHash` 签名；键一律用 username，与 `SetUserPasswordHash`/`SetUserDisabled` 风格一致）：
  - `SetMustChangePassword(username string, v bool)`：单独置/清标记。
  - `DeleteUserSessionsExcept(userID, keepTokenHash string)`：删除该用户除指定会话外的全部 sessions；`keepTokenHash` 传空串即删除全部（管理员重置场景复用同一方法，不再单开 `DeleteUserSessions`）。

### 2.2 标记置位/清除点

| 时机 | 位置 | 标记 |
|---|---|---|
| `EnsureRoot` 生成初始 root 密码 | `auth.go:36-49` | 置 1 |
| `reset-root` 子命令（`Store.ResetRoot`） | `store.go:239-256` | 置 1，且 `DeleteUserSessionsExcept(rootID, "")` 踢掉 root 全部会话（与"重置后踢会话"决策一致） |
| `apiUserCreate` 建用户（自选或随机密码都算"他人代设"） | `http.go:217` 附近 | 置 1 |
| `apiUserResetPassword` 管理员重置 | `http.go:288-311` | 置 1，且 `DeleteUserSessionsExcept(userID, "")` 踢掉该用户全部会话 |
| 自助改密成功 | 新端点 | 清 0 |

### 2.3 新端点 `POST /api/v1/change-password`

- 已认证即可（不需 admin）。请求体 `{old_password, new_password}`。
- 校验顺序：旧密码 bcrypt 校验（失败走与登录相同的限流计数，防爆破；错误响应不区分"旧密码错误"细节以外的信息）→ 新密码规则与建用户一致（≥8 位，`http.go:247-253` 同款校验）→ 新密码不得等于旧密码。
- 成功后：更新哈希、清标记、`DeleteUserSessionsExcept(userID, 当前 tokenHash)`（保留当前会话，客户端无需重新登录）。
- 响应 `200 {ok:true}`；旧密码错误 `401`；规则不符 `400`。

### 2.4 拦截中间件

- 在 `auth` 之后插 `gate` 中间件：`MustChangePassword=true` 的会话只放行：
  - `GET /api/v1/me`
  - `POST /api/v1/change-password`
  （oksrv 无 logout 端点——GUI 的退出登录只清本地配置，故白名单仅此两项。）
- 其余已认证端点一律 `403 {"error":"must_change_password"}`。
- 中间件按路由白名单判断，形态与现有 `admin()` 中间件（`http.go:62-70`）一致。

### 2.5 标记下发

- `apiLogin`（`http.go:137-140`）与 `apiMe`（`http.go:145-157`）响应的 user 对象带 `must_change_password` 字段（User 结构体加 json tag 后自然带出）。

## 3. 客户端设计

### 3.1 转发层

- `internal/serverx`：`Client` 加 `ChangePassword(oldPassword, newPassword string) error`。
- `internal/gui/api_server.go`：加 `POST /api/server/change-password` 转发（仿 `fwdUserResetPassword`，`api_server.go:299-311`）；403 响应原样透传状态码与 body，让前端能识别 `must_change_password`。

### 3.2 GUI 页面（`web/`）

- **强制改密弹窗**：登录态下满足任一条件即弹出**不可取消**的改密对话框：
  1. 拉取服务器状态（me 等价物）返回 `must_change_password=true`；
  2. 任何 `/api/server/*` 请求收到 `403 must_change_password`。
  对话框内容：旧密码 + 新密码 + 确认新密码；前端先本地校验两次输入一致与 ≥8 位，再提交。成功后提示"密码已修改"并正常继续（当前会话保留，无需重登）。
- **自助改密入口**：服务器管理区加"修改我的密码"按钮，任何已登录用户可见（不限管理员），同样的对话框但可取消。
- 假功能按钮纪律：按钮接真实端点，失败有可见错误反馈（本项目既有教训）。

### 3.3 CLI 行为（本期无需改动）

- 经核实 `serverx` 仅被 `internal/gui` 使用，CLI（`ok`）不直连 okserver 管理 API（同步走 git 协议，由 Gitea 侧鉴权）。因此 CLI 无改造点；若未来 CLI 直连遇到 403，`serverx.Error` 会原样透传 `must_change_password` 文案。

## 4. 错误处理与安全

- 旧密码校验复用登录限流（每用户名失败 5 次锁 5 分钟，`auth.go:104-138`），防止在线爆破。
- 所有 401/403 响应不泄露用户是否存在等额外信息（沿用现有防枚举约定）。
- 密码明文只存在于请求体内存，存储层只经手 bcrypt 哈希（既有纪律）。
- 管理员重置与自助改密都会话级踢出，杜绝旧密码泄露窗口期的残留会话。

## 5. 测试

TDD，行为变化类测试先验证对旧代码变红。

- `internal/oksrv/store_test.go`：列迁移幂等（新库/旧库各跑一次 OpenStore）；标记读写；`DeleteUserSessions(Except)` 语义。
- `internal/oksrv/auth_test.go`：改密成功（哈希更新、标记清 0、其他会话失效、当前会话保留）；旧密码错误计限流。
- `internal/oksrv/http_test.go` 端到端：
  - 带标记会话访问普通端点 → 403 `must_change_password`；白名单三端点放行；
  - 建用户/管理员重置后登录响应带标记；改密后恢复全通；
  - 管理员重置后该用户旧 token 全部失效；
  - root 初始密码场景（EnsureRoot 后标记为 1）。
- `internal/serverx/serverx_test.go`：`ChangePassword` 请求体与错误透传。
- `internal/gui/api_server_test.go`：转发端点 + 403 标记透传。

## 6. 明确不做（YAGNI）

- 不做 CLI 交互式改密（仅报错指引）。
- 不做密码定期过期 / password_version 代际。
- 不做密码复杂度规则加强（维持 ≥8 位现状）。
- 不同步 Gitea 侧密码（沿用 `http.go:286-287` 既有注释语义：Gitea 密码用户不持有）。
