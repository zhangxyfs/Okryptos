# Token 监视桌面小工具（OkMeter）需求与方案

日期：2026-09-08 · 2026-09-09 修订（技术选型 Go+WebView2 → C++ native；新增材质效果层；架构改模块化框架 + 采集适配器；设置面板改背板式） · 状态：设计中（未实施） · 平台：仅 Windows

UI 原型：`docs/prototypes/prototype-token-dock-v8.html`（球体弧线形态）、`docs/prototypes/prototype-token-dock-v5.html`（胶囊量表 / 星环罗盘形态）。视觉以原型为准，本文档以功能与架构为准

## 1. 背景与目标

Kimi Code CLI 的 token 用量只能进 TUI 敲 `/usage` 看，hooks 事件不携带 token 字段，无法被外部实时监听。但每次 LLM 调用的用量都会落盘到会话日志，这给了旁路监听的可能。

目标：做一个常驻桌面的小工具 **OkMeter.exe**，从 okd 托盘图标拉起，吸附在屏幕边缘，以可切换视觉形态的边缘吸附 dock 实时展示 token 用量（默认形态为"弧线 + 奇数个球"）；鼠标悬停时展开显示明细。视觉上参考 Circle Dock / QQ 贴边隐藏一类边缘吸附工具，具体样式以原型评审结论为准，用户手绘草图仅表达"弧线为主"的意向，不作依据。

**视觉保真度目标 ≥95%**：材质效果对标 iOS 26 液态玻璃与鸿蒙沉浸光感（真机级折射、跟手光、辉光粒子、弹簧动效）。**原型是下限不是天花板：实现出来的材质效果只能比原型图好，不得比原型差**——任何并排对比中肉眼可辨的差距点都算不合格。该目标是技术选型的第一约束——纯 Web 栈无法采样窗口背后的实时像素，保真度天花板约 90%，故接受脱离 Go 技术栈、用 C++ native 实现（见 §4）。

**框架化目标**：实现为可持续扩展的运行框架，而非一次性小工具——采集层按 agent 工具一个适配模块（v1 仅内置 Kimi Code，未来 Claude Code / Codex 等新工具只加模块），渲染层形态/材质注册式可插拔。新增监听对象或新视觉不动框架本体（见 §3.6）。

## 2. 名词与数据约定

- **统一 usage 事件**：框架内部的事件模型（model / agentId / inputOther / inputCacheRead / inputCacheCreation / output / time / scope）。各采集适配器把所监听工具的自有落盘格式映射为该事件；下方 usage.record 是 Kimi Code 适配器的第一个映射来源。
- **usage.record**：`wire.jsonl` 中的用量记录，实测格式（2026-09-08，`kimi-code/k3-256k`）：

```json
{"type":"usage.record","agentId":"main","model":"kimi-code/k3-256k","usage":{"inputOther":6243,"output":141,"inputCacheRead":18432,"inputCacheCreation":0},"usageScope":"turn","time":1788865482021}
```

- **文件布局**：`~/.kimi-code/sessions/wd_<项目哈希>/session_<uuid>/agents/<agentId>/wire.jsonl`。主 agent 为 `main`，子 agent 为 `agent-N`，各自独立文件，**全部都要计入**。
- **home 解析**：`KIMI_CODE_HOME` 环境变量优先，否则 `~/.kimi-code`（对齐 `internal/agentx/kimi.go:25-31` 的既有约定）。
- **一次调用的 token 数**：`inputOther + inputCacheRead + inputCacheCreation + output`。是否在 UI 上区分 cache 命中属于显示层配置项，统计层四字段全存。
- **游标**：对每个 wire.jsonl 记录已读取字节偏移，重启后从偏移续读，避免重复累加。游标与累计结果持久化到 `~/.okryptos/okmeter/state.json`（原子写入，沿用项目 fsx 惯例）。

## 3. 功能需求

### 3.1 入口

- okd 托盘菜单新增一项"Token 监视器"（`internal/tray/tray_windows.go` 菜单区，现为版本号/检查更新/退出三项），点击后按 `internal/gui/open_windows.go` 的既有模式在**自身 exe 同目录**找 `OkMeter.exe` 拉起；找不到则静默忽略并记日志（fail-open，同托盘既有风格）。
- OkMeter 自身不注册全局热键、不加自己托盘图标（v1），退出与设置都走球体右键菜单。

### 3.2 窗口行为（贴边吸附，非 AppBar）

- 透明、无边框、始终置顶、不在任务栏出现、不进 Alt-Tab 列表（WS_EX_TOOLWINDOW）。
- **不用** SHAppBarMessage 注册 AppBar：AppBar 会永久占用屏幕工作区挤压其他窗口，对一个装饰性监视器是不可接受的 UX。采用 QQ 贴边隐藏式：普通置顶悬浮窗 + 贴边自动收缩。
- 支持吸附左/右边缘（v1 不做顶/底）；拖到屏幕另一侧松手即换边；多显示器按"窗口中心所在屏"处理。
- **两态**：
  - 收缩态（默认）：球只露出一半，其余被屏幕边缘裁掉，弧线细而淡，整体宽度约 24px，尽量不抢注意力。
  - 展开态（鼠标进入窗口或点击）：整体向屏幕内滑出，显示完整的球与弧线，各球上显示其绑定指标的缩略文本（如 `128.6K`）；鼠标离开窗口区域约 600ms 后收回。
- 收起/展开用**弹簧物理动画**（stiffness/damping 积分器，调校到 150~200ms 等效时长），渲染循环对齐 vblank；禁止闪烁与抖动（hover 边界加迟滞）。

### 3.3 视觉形态与数据映射

- **形态与材质是正交的两层**：形态决定项怎么画、沿什么路径排布；材质决定光照质感（折射、高光、光晕、粒子）。两者可任意组合，设置里各自独立切换。
- v1 内置材质（均已有原型，`okmeter-dock-prototype.html`；**每种材质的实现效果以原型为验收下限，只能更好不得更差**）：
  - **标准深色**：基础毛玻璃，无动态光效（兜底材质，低配/降级场景用）；
  - **液态玻璃**（对标 iOS 26）：边缘环带真实折射（采样窗口背后的实时内容）、不均匀边缘光、跟随指针的镜面高光（方向 + 距离衰减）、边缘色散、浮动阴影；
  - **沉浸光感**（对标鸿蒙）：内容自适应毛玻璃、柔光跟随指针连续移动并汇聚到所指元素、全场景统一光源（邻近元素朝向光源一侧出现边缘亮边）、粒子汇聚/消散（additive 辉光）、按压弹性光晕。
- 数据映射（每个位置显示什么指标）与形态渲染、材质渲染是三层解耦；新增形态或材质只动渲染层，不改统计层与映射语义。
- v1 内置形态（均已有原型）：
  - **球体弧线**（默认）：`prototype-token-dock-v8.html`，项沿弧线排布；
  - **胶囊量表**：`prototype-token-dock-v5.html`，带用量占比条的胶囊；
  - **星环罗盘**：同 v5 原型，中心罗盘 + 卫星环绕。
- 设置里可切换形态，遵循两段式保存约定，保存后立即生效。
- **项数必须是奇数**：允许 1 / 3 / 5 / 7，默认 3。设置里只允许选奇数（下拉枚举，不做自由输入，从交互上杜绝偶数）。
- **位置不绑定角色**：中间项不是最大的，也不非得显示总量；尺寸策略由形态决定，默认各项等大。
- **默认映射：全部项按最近使用排序的模型累计**——最近一次 usage.record 的模型占中心位，次近的交替向两侧排开，依次向外。
- 每个项可单独配置显示指标（右键该项 → 选择），分两种模式：
  - **总量模式**（全局口径）：当前会话用量（最近活跃 session 的累计）/ 今日用量（本机自然日）/ 本周用量 / 全部累计；
  - **模型模式**：指定模型累计（按 model 字段分组，下拉列出已观测到的模型）。
- 项上数字用紧凑格式（`9.9K` / `128.6K` / `2.1M`），精确值留给悬停详情。

### 3.4 悬停交互与详情

- **悬停强化（球体形态）**：鼠标移到哪个球上，哪个球就完整浮现并放大；相邻球沿排布路径让位（挤开），其余球略微回缩，避免遮挡。动画 150~200ms 缓动，与展开动画共用同一套时长/缓动配置。其他形态的悬停反馈由各自形态定义。
- 悬停某球时，在该球内侧浮出详情卡（不遮挡其他球），内容随该球的配置模式而变：
  - 该球绑定的指标名与精确值（千分位）
  - **若为模型模式**：模型名、今日 / 本周 / 累计三行、最近一次调用时间（"3 分钟前"式相对时间）
  - **若为总量模式**：当前会话 / 今日 / 全部累计三行 + input（含 cache 拆分）与 output 的构成

详情卡跟随悬停球，切换球时切换内容，移出即消失。

- **亮背景自适应墨色**：对背景帧做 8×8 网格点采样均摊亮度（luma > 0.55 判亮底），亮底时球内/卡内文本与 hairline 自动切深墨、暗底保持浅墨——白底应用前不再"白底白字不可见"。
- **胶囊形态悬停不推挤邻项**（hoverPush=0）：胶囊间距本身已大，推挤只造成扫动抖动。
- **收缩态**：dock 平移露出左半 50%（原型 .dock.ready translate(50%）同款），项以 55% 不透明度呈现；展开时 100% 浮出。

### 3.5 设置

右键任意球或弧线 → 菜单：设置 / 换边 / 退出。

设置面板（D2D/DWrite 自绘，不走原生对话框，与 dock 同一渲染管线）：

- **背板式形态**：常显侧边玻璃背板，停靠在 dock 对侧屏幕边缘，分节滚动、顶部标题栏 + 底部操作条——形态参照 `prototype-token-dock-v8.html` 配置面板；不用居中模态弹窗。
- **形态/材质选项用带 SVG 缩略图的选项卡**——样式参照 `okmeter-dock-prototype.html` 设置面板；即"v8 的背板 + okmeter 原型的选项卡"两者结合。
- **视觉体系一致**：设置界面与 dock 共用同一套设计语言——深色玻璃质感、同一配色/圆角/描边/控件样式（chips 单选、自绘玻璃下拉、玻璃开关），禁止出现与 dock 割裂的独立皮肤（如浅色弹窗 + 原生 select）。
- **设置面板跟随当前生效材质**：材质模块同时为 dock 与设置面板供皮——液态玻璃生效时面板同为液态玻璃（含边缘折射），沉浸光感生效时面板同为通透光感质感。草稿态切换材质不即时换肤（遵守两段式语义），保存生效后面板随之切换。
- 材质效果：标准深色 / 液态玻璃 / 沉浸光感（枚举来自渲染模块注册表，新增材质自动出现）
- 视觉形态：球体弧线 / 胶囊量表 / 星环罗盘 / 电平柱 / 波形流 / 辉光数码（同上，来自注册表；波形流 sparkline 取该绑定最近 26 个周期的**增量**序列）
- 球数量：1 / 3 / 5 / 7 单选
- 每个球的指标映射（按位置列出，总量模式 / 模型模式）
- 吸附边：左 / 右
- 数字是否合并 cache 命中进 input
- 保持显示：开启后鼠标移开不再自动收回，dock 常显展开（默认关闭=自动隐藏）

遵循项目既定交互约定：**勾选 + 保存按钮两段式**，不用 change 即存；保存后立即生效并写盘，未保存关闭则丢弃。

### 3.6 模块化框架（可扩展性是第一约束）

- **三层架构**：
  - **核心框架**：事件调度、聚合统计、游标与配置持久化、窗口与渲染宿主；不含任何具体 agent 工具的知识。
  - **采集适配器**：每种 agent 工具一个独立模块，把该工具的用量落盘格式映射为统一 usage 事件流（§2），并自管扫描与游标。**新增监听对象 = 新增适配器模块，框架代码零改动**。
  - **渲染模块**：视觉形态与材质效果走注册表，设置面板的选项枚举直接来自注册表；**新增形态/材质 = 注册新渲染模块**，不动框架与设置面板代码。
- v1 仅内置 Kimi Code 适配器（`~/.kimi-code/sessions/**/wire.jsonl`）；其余 agent 工具（Claude Code、Codex 等）预留接口、不实现。
- 配置持久化按模块划分命名空间，适配器与渲染模块各自读写自己的配置节，互不感知。

### 3.7 数据采集

- 启动时全量扫描 `sessions/**/wire.jsonl`，按游标增量读；运行期用 `ReadDirectoryChangesW` 监听 sessions 目录，2s 轮询兜底。
- 解析容错：逐行 JSON，坏行跳过；未知 `type` 跳过；缺 `usage` 字段跳过。
- 掉线场景：CLI 没在跑时没有新数据，属正常；OkMeter 自身崩溃重启后从游标恢复，总量不丢。
- 只读 `~/.kimi-code`，不写、不删；OkMeter 不开放任何网络端口，与 okd 之间**无 IPC**（数据各读各的盘）。

## 4. 技术选型

结论：**C++（MSVC）+ Win32 + D3D11 + 分层窗口（WS_EX_LAYERED/UpdateLayeredWindow）上屏，单 exe 全自含**。

> 修订（Plan 2b 收尾期实机实证）：原方案的 DirectComposition 上屏在"与 `WDA_EXCLUDEFROMCAPTURE` 共存"时被 DWM 把透明区合成成不透明黑底（A/B 对照实验：同一渲染内容，DComp+排除=黑底，分层窗口+排除=正常透明；DComp 不加排除则正常）。呈现层改为分层窗口逐像素 alpha 上屏——迅雷悬浮球/QQ 贴边/游戏启动器镂空同款路径，GPU 渲染管线（D3D11/D2D/着色器）与 WGC 捕获均不变，仅"最后一公里"上屏换成 RT 纹理回读 + UpdateLayeredWindow。

95% 保真度的三个必要条件（技术选型只按这三条裁决）：

1. **实时背景像素**：液态玻璃的折射对象是窗口背后的真实屏幕内容。路径：`SetWindowDisplayAffinity(hwnd, WDA_EXCLUDEFROMCAPTURE)`（Win10 2003+）把自身从截屏排除 + **Windows Graphics Capture** 捕获所在显示器，GPU 纹理零拷贝直接进着色器，帧率跟随显示器。
2. **自定义像素着色器**：法线图折射（逐像素 UV 偏移采样背景纹理）、RGB 微偏移色散、菲涅尔边缘光、背景高斯模糊预 pass、additive 粒子辉光——只有 HLSL 这一层能做全。
3. **vblank 渲染循环 + 弹簧物理**：动画帧对齐刷新率，动效用 stiffness/damping 弹簧积分器而非 cubic-bezier 近似。

| 方案 | 体积 | 保真度 | 与本仓库契合度 | 结论 |
|---|---|---|---|---|
| **C++ Win32 + D3D11 + 分层窗口** | 2~5MB，零运行时依赖 | **95~98%**（渲染与系统合成器同一条 GPU 管线） | 构建链加 MSVC 一步，Inno/托盘拉起透明 | **推荐** |
| C# + WinUI 3（WinAppSDK） | 自含 60~90MB | ~95%（Composition 弹簧 + PixelShaderEffect + WGC） | 新语言新工具链，体积违背"小工具"定位 | 否决：体积与运行时 |
| Go + WebView2 + WebGL2 升级 | ~10MB | ~90%（帧数据过 PostWebMessage 序列化，合成时机不可控） | 依赖现成 | 否决：摸不到 95% |
| Electron | ~150MB | ~90% | 重量级 | 否决 |
| 纯 Web/CSS/SVG（原型现状） | — | ~70%（无法采样背后内容） | 原型阶段产物，不作交付形态 | 否决 |

方案要点（C++ 路线）：

- **窗口**：`WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE | WS_EX_LAYERED` 分层窗口；D2D 渲到离屏 RT 纹理，帧末回读 DIB 经 `UpdateLayeredWindow` 逐像素 alpha 上屏（修订见 §4 结论注记；DComp swapchain 路线已废弃）。不注册 AppBar。
- **背景捕获**：WGC `GraphicsCaptureItem::CreateFromMonitor` 全屏捕获，着色器里按窗口区域裁剪采样；自身窗口靠 `WDA_EXCLUDEFROMCAPTURE` 排除。
- **渲染**：D3D11 片元着色器做折射/色散/菲涅尔/粒子；文本与详情卡用 D2D + DWrite（UI 总量 = 若干球 + 一张卡 + 一个菜单 + 设置面板，不需要 UI 框架）。
- **数据层**：`ReadDirectoryChangesW` 监听 + 逐行 JSON 解析（格式固定，手写解析或单头库）+ 游标原子写盘，约 200 行，与 UI 同进程——维持"无 IPC"裁决不变。
- **代码结构**（对应 §3.6 三层）：`okmeter/` 内部按 `core/`（事件调度、聚合、持久化、注册表）、`adapters/kimi/`（v1 唯一采集适配器）、`render/forms/` 与 `render/materials/`（可插拔渲染模块）、`ui/`（dock 窗口与设置背板）分目录；适配器与渲染模块以静态注册表接入 core。
- **降级链**：WGC 不可用（锁屏 / 远程桌面 / 安全桌面）→ 静态模糊背景；`WDA_EXCLUDEFROMCAPTURE` 缺失（< Win10 2003）→ 液态玻璃退化为毛玻璃；D3D11 设备丢失 → 重建设备继续。
- 原"Go 不一定最合适"的判断升级为最终结论：Go 的舒适区（文件监听、统计）在 C++ 里只有约 200 行，而保真度目标要求整条渲染链路自控，故不再保留 Go 壳。

## 5. 构建与分发改动点

1. 新增 `okmeter/`（顶层 C++ 工程：源码 + `CMakeLists.txt` + `version.rc`），产物仍名 `OkMeter.exe`；不建 `cmd/okmeter/` Go 包。
2. `scripts/build-dist.sh` 加 MSVC 编译步骤（cmake 或 cl，Release/x64）；版本资源走 `version.rc`（rc.exe 编译），**不进** winres.json 循环。`scripts/sync-version.sh` 需同步适配 `.rc` 里的版本字段——注意既有教训：winres 段 sed 分隔符与分组 alternation 冲突会静默中断，改 `.rc` 段时一并验证。
3. 构建机依赖：MSVC Build Tools + Windows SDK ≥ 10.0.20348（`WDA_EXCLUDEFROMCAPTURE` / WGC 需要），版本在构建脚本里钉死。
4. `installer/okryptos.iss` [Files] 段加 `OkMeter.exe`；不加 [Run] 自启、不加 [Icons] 快捷方式（按需从托盘拉起）。
5. `installer/nfpm.yaml` / `build-linux.sh` 不动（Windows-only）。
6. `internal/tray/tray_windows.go` 加菜单项 + 拉起回调（同目录取 exe，仿 `open_windows.go:47-59`）——托盘仍是 Go，与 OkMeter 实现语言无关。

发布边界纪律不变：okserver 与 server/ 不进包；OkMeter 属客户端组件，正常进 Windows 安装包。

## 6. 测试与验收

- 数据层（无 UI 静态库）：表驱动单测（GoogleTest 或微型断言宏）——伪造 wire.jsonl 行，验证累加、模型分组、游标续读、坏行容错；纯 Windows 工程，不再适用"Linux 跑 go test"的平台双态约定，单测只在 Windows 构建机跑。
- 框架/适配器契约测试：伪造适配器喂统一 usage 事件，验证核心框架的聚合、游标与多适配器并存——保证"新增适配器不动框架"这条约束有测试兜底。
- 渲染层不做自动化断言，用验收清单人工过：三种材质 × 三种形态 × 左/右缘 × Win10/Win11。
- 验收：真实跑一段 Kimi CLI 会话，观察球上数字随 `/usage` 口径同步增长；重启 OkMeter 后总量不回退；换边/改球数/改映射/切换形态与材质在保存后立即生效；切换材质保存后设置面板与 dock 同步换肤；液态玻璃下拖动背景窗口，折射内容实时跟随。

## 7. 范围外（YAGNI）

- Linux / macOS 版本
- Kimi Code 以外的 agent 工具适配（框架预留接口，v1 仅内置 Kimi 适配器）
- 历史曲线图、按小时分布、配额预警、声音提醒
- 多机汇总、okserver 侧统计
- 从 OkMeter 反向控制 CLI（纯只读监视器）
- AppBar 模式（占工作区）、顶/底边吸附、全局热键

## 8. 风险与开放问题

- **wire.jsonl 是非公开内部格式**，Kimi Code 升级可能改字段名（`usage.record` / `inputOther` 等）。对策：解析层集中一处 + 字段缺失时降级跳过，并在日志里记录首次见到的未知结构。
- **游标 rewind 已知限制**：wire.jsonl 被截断/轮换（文件变小）时适配器从头重读，历史事件会重复计入，总量膨胀且无自动修复路径（无事件级去重）。实测 wire.jsonl 为 append-only，触发概率低；恢复手段 = 删除 `~/.okryptos/okmeter/state.json` 全量重建。poll 路径对 rewind 事件打 stderr 日志以便观测。
- **WGC 在锁屏 / 远程桌面 / 安全桌面下不可用**：背景捕获失败时液态玻璃退化为静态模糊背景，不能黑窗或闪退；恢复可用时自动切回实时折射。
- **`WDA_EXCLUDEFROMCAPTURE` 需 Win10 2003+**：更低版本捕获画面会包含 dock 自身（折射出自己，产生反馈伪影），检测到缺失则液态玻璃整体降级为毛玻璃材质。
- **多显示器与 DPI**：PerMonitorV2 逐屏缩放，跨屏拖动时重建交换链与捕获会话；窗口贴边位置按物理像素重算。
- **置顶窗与全屏独占应用共存**：全屏独占（游戏等）场景 dock 会被盖住，属可接受行为，不做穿透强求。
- **MSVC 工具链进入构建链**：构建机需装 Build Tools + Windows SDK 并钉版本；`sync-version.sh` 对 `version.rc` 的适配是已知的 sed 易碎点，改版本号后要验证资源字段确实更新。
- 透明 + 置顶 + 穿透三个标志位在 Windows 各版本组合行为有差异，需在 Win10/Win11 各验一遍。
- **WGC 显示器捕获触发 Win11 黄色捕获提示边框**（全屏四缘常亮黄框，Plan 2b 实测）：必须 `IGraphicsCaptureSession3::put_IsBorderRequired(false)`（Win11 22H2+）；旧版 Windows 无此接口则保留黄框并记 backdrop.log，不降级。光标捕获同步关闭（`IGraphicsCaptureSession2`），玻璃底不含指针。
- **`WDA_EXCLUDEFROMCAPTURE` 的副作用**：GDI `BitBlt`/`CopyFromScreen` 截图与 WGC 录屏都看不到 dock 本体——自检截图必须走 `--shot`/`--shotcap`/`--shotmenu`/`--shotsettings` 的回读 backbuffer 路径，不能用系统截屏验收。
- **DComp + `WDA_EXCLUDEFROMCAPTURE` 组合被 DWM 合成成不透明黑底**（Plan 2b 收尾期"黑边"根因，A/B 实证）：DComp 透明 swapchain 单独用正常、分层窗口加排除也正常，唯独 DComp+排除黑底；且运行时用 `SetWindowDisplayAffinity` 撤除标志不恢复。故呈现层弃用 DComp 改分层窗口（§4 修订）。
- **WUC `CompositionBackdropBrush` 在非 UWP 桌面进程渲染为空**（spike 实证：Compositor 需先建 DispatcherQueue 才能激活；SpriteVisual 色刷正常显示但 BackdropBrush 是 no-op）——"系统合成器逐 visual 背景模糊"在纯 Win32 不可用，官方矩形亚克力（WinAppSDK DesktopAcrylicController）不满足逐球形态且拖重型运行时，故背景模糊维持自持 WGC 像素路线。
