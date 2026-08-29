# okserver 认证层（bcrypt + root 首启 + 会话 token + 登录限流）

日期：2026-08-29

okserver 管理面（`internal/oksrv`）在 Task 1 SQLite 存储层之上补齐认证层。密码用 bcrypt（DefaultCost）哈希落库，存储层全程不碰明文；`EnsureRoot` 处理首次启动引导——无 root 时生成 root 账号和 32 位随机密码，明文只在这一次返回给调用方落盘/打印，二次调用幂等不再生成。登录校验 `VerifyLogin` 对"无此账号/密码错误/已禁用"一律返回 nil 不区分原因，防用户名枚举。

会话 token 为 32 字节随机的 hex 串，明文只下发一次，库里存 SHA-256 哈希（泄库不泄可登录 token）；有效期 30 天（spec §17 待决建议值），`SessionUser` 对过期/不存在/已禁用一律 nil。登录限流 `LoginLimiter` 为纯内存态（重启清零），每用户名失败 5 次锁 5 分钟，成功登录后 `Reset` 清零。

依赖：新增 `golang.org/x/crypto`（bcrypt），为 spec §16 批准的唯一新依赖；间接依赖 `x/sys` 随之升到 v0.47.0。时间列沿用存储层约定，一律 TEXT 存 UTC RFC3339。
