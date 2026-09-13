# OkMeter 发布化（Plan 4）

日期：2026-09-13

OkMeter 从开发版变为随安装包发布、托盘可拉起的成品：

- **GUI 子系统**：链接改 `/SUBSYSTEM:WINDOWS /ENTRY:mainCRTStartup`（PE 头验证 subsystem=2），双击不再弹控制台窗口。
- **单实例守卫**：`Global\OkMeter.SingleInstance` 命名 mutex，重复拉起静默退出（实测二次启动 exit 0 不叠实例）；`--shot` 自检系列不受限。
- **版本资源接入 sync-version.sh**：`okmeter/version.rc` 纳入统一版本同步（FILEVERSION/PRODUCTVERSION 逗号四段 + StringFileInfo 字符串段，幂等），随 iss 单一事实源走。
- **build-dist.sh 加 MSVC 步骤**：发布构建编译 okmeter（单测 + exe 一体）并拷入 `dist/OkMeter.exe`，本机有运行实例占用时明确报错指路。
- **安装器打包**：`installer/okryptos.iss` [Files] 新增 OkMeter.exe。
- **okd 托盘拉起**：托盘菜单在「检查更新」与「退出」之间新增「Token 监视器」项，拉起与 okd 同目录的 OkMeter.exe（不存在时菜单项隐藏，拉起失败写 stderr）。

测试：C++ 51 单测全绿；`go build ./...` 与 `go test ./internal/tray ./internal/daemon` 全过；`scripts/build-dist.sh` 端到端跑通并验证 `dist/OkMeter.exe`（subsystem=2）；`dist/okd.exe` 内确认含菜单项与拉起路径字符串；sync-version.sh 连跑两次验证幂等。
