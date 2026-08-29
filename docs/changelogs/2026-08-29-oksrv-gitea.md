# okserver Gitea 后端实现

日期：2026-08-29

`GitBackend` 落地 Gitea 实现（`internal/oksrv/gitea.go`，admin API，`NewGitea(baseURL, adminToken)`）：HTTP 客户端 15s 超时，网络错误一律 wrap `ErrBackendDown` 响亮失败，≥400 返回结构化 `giteaError{Code, Body}`；普通调用带 `Authorization: token <admin-token>` 头。十个端点逐一对照 Gitea 官方 swagger（1.22 与 next）与源码核实，三处与预想不同，按实际形状实现（接口不变）：发用户 token 的真实端点是 `POST /users/{username}/tokens` 且强制 Basic 认证（admin API token 直接调会 401），实现用"token 当 Basic 用户名"的 Gitea 惯例通过校验；组织加成员没有直达端点，实现先 `GET /orgs/{org}/teams` 拿建组织时自动创建的 Owners 队 id、再 `PUT /teams/{id}/members/{username}`；组织减成员走 `DELETE /orgs/{org}/members/{username}`。另两个核实细节：建用户必填 email（接口无邮箱语义，合成 `<username>@okserver.invalid` 占位）；启停用户的 `PATCH /admin/users/{username}` 在 1.22 要求带 `source_id`/`login_name`。测试用 httptest 假 Gitea（`gitea_test.go`）镜像核实后的真实形状，含对非 Basic 调用 token 端点返回 401 的断言。
