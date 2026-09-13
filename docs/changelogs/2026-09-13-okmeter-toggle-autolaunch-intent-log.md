# OkMeter 设置开关（token 统计工具）+ okd 自动拉起 + 意图模型日志

日期：2026-09-13

**设置中心新增「token 统计工具」开关**（设置 → 泛化门控下方，即开即存同款交互，默认开启）：全局配置 `okmeter_enabled`（bool，缺省/不存在即为 true，写路径走 `fsx.WithFileLock` 锁内读-改-原子写，顶层键插入首个小节头之前防 TOML 段落归属错误）。API：`GET/POST /api/okmeter`（照 apiGate 模板，enabled 为 null = 不变）。GUI 文案中英双语。

**okd 自动拉起保活**：okd 启动即查一轮 + 每 15s 自省轮询——开关开 + 同目录 `OkMeter.exe` 存在 + `FindWindowW("OkMeterDock")` 未命中 → `exec.Command` 拉起（OkMeter 侧单实例守卫兜底，重复拉起静默退出；GUI 子系统 exe 不弹终端窗口）。开关关 → 不拉起且不强杀已运行实例；拉起/探测失败一律 fail-open 写 stderr。非 Windows 平台空转不报错（`okmeter_other.go`）。顺带修复 `tray_other.go` 与 windows 侧 6 参签名不一致导致的 Linux 编译断裂。

**意图模型调用日志**：检索后置过滤（`filterx.Filter`）的 note 文本全部带上模型身份（`profile名/模型名`），调用失败 / 输出截断 / 输出解析失败 / 裁决结果各路径统一经 `prompt filter: 意图模型(...) ...` 落日志。

测试：新增 `internal/config/okmeter_test.go`（默认值语义：缺文件/缺键=true、显式 false、合并加载；Set 幂等）、`internal/gui/api_test.go:TestOKMeterRoundTrip`、`internal/daemon/okmeter_test.go`（decide 六组合表驱动）；`go build ./...`、`GOOS=linux go build ./...`、`go test ./...` 全绿；`node --check web/app.js` 通过。
