# hooks 去重正则兼容 okd 存量形态

日期：2026-08-28

修复 kimi-code hooks 去重正则 `okHookCommand` 只认 `ok`/`ok.exe`、不认 `okd.exe` 的漏洞：gui-split 注册 bug（2026-08-22 实证）写入的 `okd.exe hook ...` 存量块永远剥不掉，与指向 `ok.exe` 的标记块并存，宿主每轮 prompt 重复派发、同一注入重复执行（同一秒两条相同"注入 3 条"日志实证 2026-08-28）。`StripLegacyOKHooks`/`EnsureHooksBlock` 现在能识别并清理 okd 形态存量注册（含裸 `okd hook prompt` 形态），`ok setup` 自愈可将其迁移收敛到标记块；`myokd` 词边界、`okd deploy` 等非 hook 子命令形态仍不命中，防误伤用户自装同名工具。

测试：`TestOKHookCommandRegex` 新增 6 条用例（4 条 okd 形态命中 + 2 条防误伤），先对旧正则验证变红再修复。
