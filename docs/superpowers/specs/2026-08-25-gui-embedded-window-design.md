# GUI 内嵌窗口化设计：go-webview2 原生窗口（Windows）+ 浏览器回退

- 日期：2026-08-25
- 状态：已实施（2026-08-25，commits cffd781..5db5d8c；含终审修复波：回退链防重入 + token 注入 origin 门控）
- 背景：GUI 现状是 okd  serving `web/` 五页 + 系统浏览器 `--app=` 模式打开（`cmd/okmanager/main.go` 薄启动器 + `internal/daemon/run.go:164` OpenGUI）。不满两点：依赖浏览器的形态；窗口"先小再最大化"的闪烁。决策已出：**Windows 用 go-webview2 内嵌窗口，Linux 保留浏览器形态，全程保留浏览器回退**。

## 1. 目标

- Windows 端 GUI 以 OkManager 自有的原生窗口呈现：自己的任务栏/Alt-Tab 图标、自己的标题；
- **启动即最大化**——创建窗口第一帧就是最大化，消除"小窗口闪一下再最大化"；
- 窗口状态（位置/尺寸/最大化）跨会话记忆恢复；
- token 不再走 URL fragment（内嵌路径），安全纪律顺势升级；
- `web/` 五页前端与 okd HTTP API **零改动**（仅 token 获取处加一行优先级，见 §5）；
- 构建链零冲击：保持 `CGO_ENABLED=0`、纯 Go、现有交叉编译与 Inno 打包不变。

明确不做：Linux 原生窗口（继续浏览器 `--app=`）；Wails/webview_go（见下"方案取舍"）；前端重设计（独立议题，可后续排期，与本方案正交）。

## 2. 方案取舍（已决）

| 候选 | 结论 | 理由 |
|---|---|---|
| **go-webview2（jchv）** | **采用** | 纯 Go（go-ole 调 COM），无 cgo，构建链不动；直接操作 WebView2 |
| webview_go | 否决 | 三平台统一但引入 cgo（mingw/webkitgtk 进构建链），Linux 端收益不值 |
| Wails | 否决 | 完整框架，需按它的约定重组现有前后端；okd HTTP API 是资产不是负担 |
| 原生 GUI 框架（Fyne/walk/Qt） | 否决 | 五页 CRUD 界面重写收益近零，丢浏览器可达性 |

## 3. 进程模型

OkManager 从"拉起即退的薄启动器"变为**窗口宿主**：

1. 确保 okd 在线（沿用 `EnsureCurrent` 现有逻辑，含版本切换）；
2. 创建窗口（go-webview2 自建窗口，创建尺寸=主屏，见 §4 的实际机制）；
3. 窗口内初始化 WebView2，导航到 okd GUI URL（**不带 token**，见 §5）；
4. 驻留消息循环直至窗口关闭，随后退出。okd 驻留职责不变；
5. WebView2 运行时缺失/初始化失败 → 回退现有浏览器 `--app=` 路径（同一 URL 同一前端，GUI 永远打得开；初始化失败分支直调 `gui.OpenBrowser` 不经 OpenPreferred，防回退重入内嵌路径形成进程派生链）。

托盘"双击聚焦唯一 GUI"：**托盘代码零改动**——`daemon.OpenBrowserFunc` 切换为 `gui.OpenPreferred` 后，内嵌窗口 hwnd 进入既有 `guiHwnd` 缓存与 `FocusWindow` 聚焦链（与进程无关）；`openEmbeddedDefault` 经约定标题（"OpenKnowledge 配置中心"）查找已运行窗口复用。

## 4. 窗口行为

- **启动最大化**（实际机制）：go-webview2 的 `NewWithOptions` 建窗即 `SW_SHOW`（宿主不可干预），故采用"创建尺寸=主屏 + 立即 `ShowWindow(SW_MAXIMIZE)`/`SetWindowPlacement`（均在 Navigate 之前）"——首帧已是全屏尺寸，小窗闪烁消除；有状态恢复时或见"全屏→记忆矩形"一跳，可接受；
- **状态记忆**：窗口存活期每 2s 采样 placement（窗口销毁后不可查），退出时落盘最后一次有效值到 `~/.openknowledge/gui-state.json`（机器本地，不进任何同步面）；下次启动原样恢复——上次最大化则最大化，上次普通尺寸则还原矩形；
- 自定义最小尺寸（防止拖到不可用）；标题固定"OpenKnowledge 配置中心"；图标用 OkManager 现成的 winres 图标。

已知限制：go-webview2 的公开 WebView 接口未暴露导航事件（NavigationStarting/NewWindowRequested 需访问内部 edge.Chromium），**外部链接拦截不实现**；GUI 页面无外部链接依赖，`_blank` 链接按 WebView2 默认行为处理。若未来需要，再评估 fork 或换绑定层。

## 5. token 注入（替代 URL fragment）

内嵌路径不再用 `URL#token=`：

1. WebView2 经 `AddScriptToExecuteOnDocumentCreated` 在页面脚本执行前注入，**按 origin 门控**（脚本对每个文档生效，导航离开本机地址不得带出 token）：`if(location.origin==="<okd origin>"){window.__okToken = "<token>";}`；
2. `web/index.html` 的 inline script 取 token 处加一级优先级：`__okToken` > sessionStorage（前端唯一改动；hash 解析段保留给浏览器路径）；
3. 浏览器路径（含 Linux 与回退）维持现有 hash 方式不变。

token 获取仍读本机 `daemon.json`（0600），不引入新凭证流。注入脚本在每次导航创建时执行，刷新页面不丢 token。

## 6. 代码结构

- `cmd/okmanager/main.go`：保持薄入口，分发到平台实现；
- `cmd/okmanager/host_windows.go`（`//go:build windows`）：窗口宿主——窗口创建、WebView2 初始化、token 注入脚本、placement 读写、消息循环；
- `cmd/okmanager/host_other.go`（`//go:build !windows`）：现状行为（`daemon.OpenGUI`）原样保留；
- 新依赖仅 `github.com/jchv/go-webview2`（及其 go-ole 依赖），均纯 Go；
- WebView2 运行时检测：注册表/API 查询；缺失时 Inno 安装器可选静默装 evergreen bootstrapper（Win10 兜底），未装则运行时回退浏览器。

## 7. 安全

- token 体系不变（仍 `X-Ok-Token` + daemon.json 0600）；内嵌路径消灭了"token 出现在地址栏/历史记录"的暴露面；
- Host/Origin 校验等现有 okd 侧加固不变（内嵌窗口的请求与浏览器同构）。

## 8. 测试

- 可自动化：placement 读写（含损坏文件回退默认）、token 注入脚本生成、运行时检测回退分支（mock 检测失败 → 走浏览器路径）、前端 token 优先级；
- 手工清单（Windows 实机）：首启最大化无闪烁、关闭后重开恢复状态、多显示器、刷新页面 token 不丢、托盘双击聚焦内嵌窗口、卸载 WebView2 运行时回退浏览器；
- go-webview2 库成熟度风险：由"任意失败回退浏览器"兜底，最坏情况退化为现状。

## 9. 交付边界

单阶段可交付：OkManager 宿主化 + 窗口行为（§4）+ token 注入（§5）+ 回退（§3.5）+ 托盘聚焦适配。前端重设计（颜值/交互）不在本期，完成后浏览器回退与内嵌窗口**同时受益**于任何前端改进——两条线互不阻塞。
