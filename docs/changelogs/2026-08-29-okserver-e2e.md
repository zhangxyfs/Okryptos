# okserver 部署链路 E2E 测试

日期：2026-08-29

`tests/e2e/okserver_test.go` 新增 `TestOkserverDeploy`：起真 okserver 进程（`t.TempDir()` 隔离数据目录 + 随机空闲端口，不配 `OKSERVER_GITEA_URL` 走 fake 后端），跑通部署全链路——轮询 `/api/v1/meta` 等就绪（`initialized=true`）→ 从 `<dataDir>/INITIAL_ROOT_PASSWORD` 读 root 初始密码（校验 32 字符）→ root 登录 → root 建用户 alice（校验一次性返回明文密码 + git_token）→ alice 登录建个人仓（`ok-demo`）→ 权限负例（member 访问 `GET /api/v1/users` 须 403）→ root 查审计非空。`tests/e2e/integration_test.go` 的 TestMain 照 ok/okd 先例加第三段构建：okserver 二进制（windows 自动补 `.exe` 后缀），构建失败即 panic。okserver 进程不读 OK_HOME，env 只追加 `OKSERVER_LISTEN`/`OKSERVER_DATA_DIR` 两项。
