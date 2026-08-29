# okserver 主程序

日期：2026-08-29

`cmd/okserver/main.go` 落地薄主程序（骨架照 `cmd/okd`：`main` → `os.Exit(run())`），串起已就绪的 oksrv 内部包：`env 配置 → OpenStore → EnsureRoot 首启 → NewMux 监听`。env 驱动四项配置——`OKSERVER_LISTEN`（缺省 `:3100`）、`OKSERVER_DATA_DIR`（缺省 `./okserver-data`，容器挂 `/data`）、`OKSERVER_GITEA_URL`（空 = FakeBackend 离线开发模式，meta 的 `git_backend.type=fake`；非空则 `NewGitea(url, OKSERVER_GITEA_ADMIN_TOKEN)`）。root 首启时明文初始密码写 `<dataDir>/INITIAL_ROOT_PASSWORD`（0600）并日志打印一次，重启不重复生成。版本号走 `internal/version.Version`（ldflags 注入，裸 build 为 dev）。okserver 是 Linux 服务端，不生成 winres.json/syso，不进任何客户端构建脚本。
