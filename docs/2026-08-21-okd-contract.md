# okd 进程契约（跨语言共享，改动需同步全部消费方）

日期：2026-08-21 · 消费方：ok.exe（Go）、OkManager.exe（Go，未来可能 C++）

## 实例凭证

- 路径：`~/.openknowledge/daemon.json`（`OK_HOME` 环境变量可覆盖根目录）
- 内容：`{"pid":int,"port":int,"token":"hex（16 字节随机，32 字符）","fingerprint":"路径|size|mtimeUnixNano","started_at":"RFC3339"}`
- 写入：okd 启动时原子写（tmp+rename），权限 0600；退出时仅当 PID 仍是自己才删除
- 端口：首选 17888；被占且已有健康 okd 则新实例直接退出；否则回退随机端口并写回本文件

## 健康检查

`GET http://127.0.0.1:<port>/api/health`，请求头 `X-Ok-Token: <token>`，200 为健康，401 为 token 不符。

## 拉起约定

- 客户端发现顺序：读 daemon.json → 健康则直接用 → 不健康/不存在则拉起
- 拉起目标：当前 exe 同目录的 `okd.exe`（Linux 为 `okd`），无参数；不存在则回退"自身 exe + `daemon` 参数"（旧部署/单二进制开发）
- 拉起方式：DETACHED 后台进程，stdio 追加写入 `~/.openknowledge/daemon.log`（按行带时间戳）
- 防抖：拉起前写 `daemon.json.spawning` 标记，15s 内不重复拉起；daemon 健康后客户端删除该标记（2026-08-22 增补：此前无删除路径，标记永久滞留会误导排障）
- 版本切换：daemon.json 指纹 ≠ 拉起目标指纹 → 客户端先 `POST /api/shutdown` 再删凭证再拉起

## 子命令兼容转发（okd，2026-08-22 增补）

- gui-split（2026-08-21）前的注册形态是 `okd.exe hook/on/off/setup ...`；okd 现仅自有 `stop`，其余**已知 ok 子命令转发给同目录 `ok.exe`**（`internal/daemonx.CliTargetFor` 解析；孤儿 okd——无同目录 ok——报错退出 1），stdio 与退出码原样透传
- 未知参数维持"启动 daemon"语义；新增 ok 子命令必须同步 `internal/daemon/forward_cli.go` 的子命令表
- hooks/技能注册一律写 ok 路径：daemon 进程内 `os.Executable()` 是 okd，直写会注册出失效命令（okd 收到 hook 子命令只会空转启动 daemon，破坏 hook JSON 协议）

## 停服

`POST /api/shutdown`（带 token，尽力而为），随后客户端删除 daemon.json；或执行 `okd stop` / `ok daemon stop`（同一逻辑的 CLI 封装）。

## hook 转发语义（ok.exe 专属）

- `POST /api/hook/<name>[?format=]`，body 为 hook 原始 payload，响应 `{"stdout","stderr","code"}`
- 超时视作已受理（handled）：请求已被接收且处理不可取消，本地兜底会双执行，宁缺毋双
- 仅连接类失败（拒绝连接/不可达/不健康）才走 ok.exe 本地兜底——兜底是有意保留的例外，见设计文档 §4.1

## 数据一致性模型

条目/配置文件为多写者（CLI、GUI、hook 兜底都可写）；一致性不靠单写者，靠各消费方按需 mtime 增量 `db.Sync`。okd "唯一写者"仅指：sidecar 托管、管理 API、托盘。
