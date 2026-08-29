# okserver 管理面 HTTP API

日期：2026-08-29

`internal/oksrv/http.go` 落地完整管理面 mux（`NewMux(store, backend, version)`，15 个端点，`jsonx.go` 的 writeJSON/writeErr/decodeJSON 照 gui 版复刻）。鉴权 `Authorization: Bearer <token>`，无 cookie/静态页，CSRF 结构免疫；角色门控两级——成员端点（`/me`、建个人仓）仅要登录，管理端点（用户/组织/团队仓/仓库列表/审计）要 root 或 admin。契约要点：`GET /meta` 公开返回版本、初始化状态与 git 后端健康（type 按实现断言 fake/gitea）；登录每用户名失败 5 次锁 5 分钟（429）；建用户一次性下发明文初始密码 + git token，`role:"admin"` 仅 root 可授；个人仓 `ok-<project>` 幂等（已存在返回现有记录且 `git_token` 空串，token 不重复下发，丢失留待 v1.1 reset 联动）；组织映射 Gitea `ok-<org>`、团队仓归属该 org；root 保护——不可被禁用、不可被 admin 重置；全部写操作落审计（actor=当前用户）。状态码统一：401 未认证 / 403 需管理员或 root 保护 / 404 对象不存在 / 409 重名 / 429 限流 / 502 git 后端故障 / 201 建组织 / 204 空成功。测试用 httptest + FakeBackend 端到端跑通元信息、登录限流、建用户到建组织/团队仓的完整管理流（`http_test.go`）。
