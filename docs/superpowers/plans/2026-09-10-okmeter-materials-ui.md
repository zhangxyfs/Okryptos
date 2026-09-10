# OkMeter Plan 2b：材质真实感 + 动画流畅性 + 玻璃菜单与设置背板 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Plan 2 交付的"结构正确但视觉失真"的 dock 拉到原型气质：真实玻璃材质（透出桌面）、丝滑动画、自绘玻璃右键菜单（含每球指标映射）、背板式设置面板（两段式保存）、其余形态与多屏/拖拽换边。

**Architecture:** 在现有单线程 UI 模型上叠三层：①WGC 背景捕获管线（实时采样窗口背后的屏幕内容，排除自身）；②渲染重构——dock_scene 拆出"材质皮肤 + 形态布局"两个注册表接口（复用 Plan 1 `Registry<T>`），四材质（暗夜/毛玻璃/液态玻璃/沉浸光感）与三形态（球体弧线/胶囊量表/星环罗盘）模块化；③UI 层补自绘玻璃菜单与背板设置面板。动画流畅性以诊断为先行任务，数据说话后选方案。

**Tech Stack:** 现有栈不变（MSVC C++20 + Win32 + D3D11/D2D1/DWrite/DComp + WGC）。零第三方依赖。

**Spec 源:** `docs/2026-09-08-token-dock-design.md` 全文；视觉基准 = `docs/prototypes/okmeter-dock-prototype.html` 的渲染效果（不是只读代码——每个视觉任务必须截图与原型渲染图并排对比）。

## Global Constraints

- **视觉验收新规（本计划每条视觉任务强制）**：实现者必须用 Edge headless 截图原型（`msedge --headless --screenshot`）+ 真实运行 OkMeter.exe 截图（CopyFromScreen），两张图都经 ReadMediaFile 亲眼看过后才允许报 DONE。**材质效果只能比原型好、不得比原型差（规格 §1 硬性条款）**——并排对比中任何肉眼可辨的差距点都算不合格，逐条写入报告并修到达标。
- 线程契约不变：采集 poll 与渲染读都在 UI 线程；WGC 帧回调不得直接触碰 Aggregator/Store，只交换纹理/标志位。
- 动画必须丝滑：帧间隔抖动 >4ms 或肉眼可辨的顿挫都算不合格；方案选择以 Task 1 的诊断数据为准。
- 右键菜单与设置面板禁止假功能：每个可见入口必须有真实能力（灰化 = 合法的可见反馈，文案不得承诺未实现的东西）。
- 两段式保存约定：设置面板勾选 + 保存按钮，未保存关闭丢弃。
- 降级链（规格 §4/§8）：WGC 不可用（锁屏/RDP/安全桌面）→ 静态模糊背景；`WDA_EXCLUDEFROMCAPTURE` 缺失（< Win10 2003）→ 液态玻璃退化为毛玻璃。
- 构建命令固定 `okmeter\build.bat`；零警告；commit `feat(okmeter): 中文描述`。
- 收缩态规格 §3.2：球只露出一半、弧线细而淡——当前实现只见球帽不见弧线，本计划修正（差异化 collapsedX 或露出条内细弧线，以视觉验收为准）。

## 已知归口（前几轮评审留档，本计划显式认领）

- tuck 的 24/12 耦合常量收拢（Task 3 拆形态时处理）
- 详情卡三项：垂直夹取负 maxY、cardin 出现动画 140ms、长模型 id 省略号
- 拖拽换边、多显示器按窗口中心所在屏（Task 5）
- pollData 零新事件时不 flush state.json（Task 6 顺手）
- minjson `\u` 转义用例、week 边界与 modelWeek 断言（测试补强，Task 8 顺手）
- 设备丢失路径零实证（Task 2 强制一次 DEVICE_REMOVED 演练）

---

### Task 1: 动画流畅性诊断（先数据后方案）

**Files:**
- Modify: `okmeter/ui/app.cpp`（临时帧间隔统计，诊断后移除或留 `#if DEBUG`）
- Create: `docs/superpowers/plans/../okmeter-anim-diagnosis.md`（诊断报告）

**背景：** 用户实测滑出过程卡顿。嫌疑源（按可能性排序）：①`SetTimer(16ms)` 系统粒度 ~15.6ms 抖动且不与 vsync 对齐；②弹簧 step 若用固定 dt（1/60）而非真实 elapsed，timer 抖动直接变成动画抖动；③每帧 `SetWindowPos` + DComp `Commit` 双通道重排；④`Present(1,0)` 阻塞与 timer 交错；⑤每帧重建 TextLayout/几何。

- [ ] **Step 1: 加帧间隔直方图统计**

动画激活期间记录每帧 `QueryPerformanceCounter` 间隔与 step 使用的 dt 来源，结束（settled）时输出：min/p50/p95/max/抖动方差 + 每帧耗时分解（step / 绘制 / Present / SetWindowPos）。控制台 printf 即可（--scan 同款控制台路径或直接 OutputDebugString + DbgView 也行，选简单的）。

- [ ] **Step 2: 跑数据，定位抖动源**

展开/收起各跑 5 次，报告数据。判定标准：
- 若帧间隔 p95−p50 > 4ms 且 dt 用固定值 → 根因 ①+②；
- 若 dt 已是真实 elapsed 且绘制耗时 >8ms → 根因 ⑤或绘制效率；
- 若 Present 阻塞占比高 → 根因 ④。

- [ ] **Step 3: 写诊断报告 + 修复方案二选一**

方案 A（小改）：timeBeginPeriod(1) + 弹簧用真实 elapsed dt + 绘制缓存审计（TextLayout 是否每帧重建）。
方案 B（推荐若 A 不够）：两态位移动画移交 DComp——`IDCompositionAnimation`/visual `SetOffsetX` 由合成器驱动（消除 SetWindowPos 抖动），球体缩放/让位保留每帧绘制但只在动画激活帧渲染。
报告里用数据选方案，写明理由。

- [ ] **Step 4: 按报告实施修复，复测同口径数据**

验收：帧间隔 p95−p50 ≤ 2ms；肉眼对照修复前后截图/录屏（间隔抽帧对比）无顿挫。

- [ ] **Step 5: Commit**

```bash
git add okmeter docs
git commit -m "fix(okmeter): 动画流畅性诊断与修复（帧间隔数据实证）"
```

---

### Task 2: WGC 背景捕获管线 + 设备丢失实证

**Files:**
- Create: `okmeter/render/backdrop.h`、`okmeter/render/backdrop.cpp`
- Modify: `okmeter/ui/app.cpp`（接线、降级）、`okmeter/render/d3d.cpp`（共享 DXGI 设备互操作）

**行为规格：**
- 启动：`SetWindowDisplayAffinity(hwnd, WDA_EXCLUDEFROMCAPTURE)`（API 缺失 → 标记降级，液态玻璃不可用）；`GraphicsCaptureItem::CreateFromMonitor`（窗口所在屏 HMONITOR）→ `Direct3D11CaptureFramePool::CreateFreeThreaded`（BGRA8，尺寸=显示器）→ `StartCapture`。
- 帧到达回调：只交换"最新帧纹理指针 + dirty 标志"（SRWLock 保护，纹理生命周期由池管理，取 `ID3D11Texture2D` 需从 `IDirect3DSurface` 互操作——`Windows.Graphics.DirectX.Direct3D11.IDirect3DDxgiInterfaceAccess::GetInterface(IID_ID3D11Texture2D)`）。**回调线程不得触碰 Aggregator/Store/Config。**
- 渲染侧：dirty 时把最新帧纹理注册为 D2D 位图（共享句柄/A11 纹理直接包 `ID2D1Bitmap1` via `CreateBitmapFromDxgiSurface`）；球体区域采样窗口矩形对应的背景区域。
- 降级：StartCapture/CreateFramePool 失败（锁屏/RDP/安全桌面）→ `backdropOk=false`，材质全部回落"静态模糊"（启动时抓一次 DWM 缩略图或干脆纯色毛玻璃），恢复事件（会话解锁）时重建捕获。
- 设备丢失恢复（DEVICE_REMOVED 重建）的代码路径保留，但**不做强制崩溃演练**（不搞 dxcap -forcetdr / 强制锁屏这类会打断用户工作的验证）——按用户裁决，真实驱动崩溃极少发生，该路径以代码审查为准。

- [ ] **Step 1: 实现 backdrop 管线 + 降级链**
- [ ] **Step 2: 构建零警告 + 手动验收**：捕获帧真实到达（帧计数/内容哈希递增，调试验证后移除残留）；锁屏/会话切换走 WTS 解锁重建路径（代码审查为准，不强制演练）；静止时 CPU ≈0。
- [ ] **Step 3: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): WGC 实时背景捕获管线与降级链"
```

---

### Task 3: 渲染模块化重构——形态/材质注册表 + 玻璃基底

**Files:**
- Modify: `okmeter/render/dock_scene.h/.cpp`（拆分）
- Create: `okmeter/render/material.h`（IMaterial 接口）、`okmeter/render/form.h`（IForm 接口）
- Create: `okmeter/render/materials/dark.cpp`（暗夜玻璃化）
- Create: `okmeter/render/forms/arc.cpp`（从 dock_scene 迁出弧线形态）
- Modify: `okmeter/build.bat`（新目录通配）

**接口（后续任务依赖，逐字）：**

```cpp
// render/form.h
class IForm {
public:
  virtual ~IForm() = default;
  virtual std::string id() const = 0;                    // "arc"
  virtual DockGeom layout(int n, int screenH, const std::string& edge) const = 0;
  virtual void drawItems(ID2D1DeviceContext* dc, const DrawContext& ctx) const = 0;
};

// render/material.h
class IMaterial {
public:
  virtual ~IMaterial() = default;
  virtual std::string id() const = 0;                    // "dark"
  virtual void drawOrbBack(ID2D1DeviceContext*, const OrbStyleCtx&) const = 0;  // 球体底（玻璃/折射/光）
  virtual void drawCardBack(ID2D1DeviceContext*, const D2D1_RECT_F&, float radius) const = 0;
  virtual void drawArcStroke(ID2D1DeviceContext*, const DockGeom&) const = 0;
  virtual void onPointer(float x, float y) const = 0;    // 跟手光/镜面高光的光源位置
};
```

**行为规格：**
- `Registry<IForm>` / `Registry<IMaterial>` 注册；app 按 `cfg.form/material` 创建，未知名称回退 arc/dark。
- 暗夜材质玻璃化（本任务就把"实心黑板"修掉）：球体底 = 背景模糊区（D2D GaussianBlur effect 作用于背景位图，球体圆域裁剪）+ 半透明染色（desk-deep 74%）+ hairline 描边 + 顶部内高光渐变 + 底部外阴影。WGC 不可用时退化：纯色 74% 透明底（无模糊）。
- tuck 的 24/12 常量收拢为 `geometry.h` 的共享常量（kCollapsedPx / kCollapsedCapPx）。
- 数值字号校准原型：值 12.5px→13px 加粗感（600 字重）、短名 8.5px；弧线带 accent 微光描边（原型 `.dock-arc .glow` 同款）。
- 视觉验收按 Global Constraints 新规：原型截图 vs 实物截图并排，肉眼差距点逐条列。

- [ ] **Step 1: 重构 + 暗夜玻璃化**
- [ ] **Step 2: 构建零警告 + 单测全绿（40 cases）+ 并排视觉验收**
- [ ] **Step 3: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 形态/材质注册表重构与暗夜材质玻璃化"
```

---

### Task 4: 毛玻璃 / 液态玻璃 / 沉浸光感三材质

**Files:**
- Create: `okmeter/render/materials/frost.cpp`、`liquid.cpp`、`glow.cpp`

**行为规格（逐项对齐原型，视觉验收强制并排）：**
- **frost**：乳白磨砂——背景强模糊（blur 22~28px 等效）+ 白 12% 染色 + 提饱和（D2D Saturation effect）。
- **liquid**（对标 iOS 26）：边缘环带折射（球体边缘 15% 环带内对背景纹理做法线偏移采样——D3D 自定义 pixel shader 或 D2D DisplacementMap effect 预烘法线图）、上左亮/下右暗的不均匀边缘光、跟随指针的镜面高光（方向角 + 距离衰减，原型 Task 已完成 web 版语义）、边缘 1px 红蓝错位色散、浮动阴影。WGC 缺失 → 整体退化为 frost。
- **glow**（对标鸿蒙沉浸光感）：内容自适应毛玻璃（极低底色 + 强模糊）、柔光层跟随指针连续移动并汇聚到所指项（白色光芯 + accent 色边双层径向渐变，绘制在球底层）、统一光源（邻近球朝向指针一侧边缘亮边，方向/距离衰减与 liquid 同一套光源计算）、粒子汇聚/消散（additive 混合，D2D 里用 `D2D1_BLEND_MODE_PLUS` 或预乘加法）、按压光晕（按下 scale .9 + 扩散环）。
- 材质切换在设置面板保存后即时生效（走 Plan 2 已实证的 cfg→save→rebuild→render 路径）。

- [ ] **Step 1: 三材质实现**
- [ ] **Step 2: 构建零警告 + 单测全绿 + 三材质各自的并排视觉验收（原型截图 vs 实物）**
- [ ] **Step 3: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 毛玻璃/液态玻璃/沉浸光感三材质"
```

---

### Task 5: 胶囊量表 / 星环罗盘形态 + 拖拽换边 + 多显示器

**Files:**
- Create: `okmeter/render/forms/capsule.cpp`、`compass.cpp`
- Modify: `okmeter/ui/app.cpp`（拖拽换边、多显示器）

**行为规格：**
- **胶囊量表**（原型 capsule）：174px 胶囊，左模型名右紧凑值，底部 3px 占比条（该项值/最大值），中心项占比条 accent 色；直线排布（step 56）。
- **星环罗盘**（原型 compass）：中心 86px 罗盘（总量口径）+ 卫星 46px 沿 R=84 圆周均布，收缩态缓慢旋转（展开态静止），虚线圆环轨道。
- **拖拽换边**：`WM_LBUTTONDOWN` 起拖（`SetCapture`），拖到屏幕另一侧松手（`WM_LBUTTONUP` 时窗口中心 x < 屏宽/2 → left）即换边 + `saveConfig`；拖动过程窗口实时跟随；规格 §3.2 原文。
- **多显示器**：窗口位置/屏高改用 `MonitorFromWindow` 的 `MONITORINFO.rcWork`（按窗口中心所在屏）；`WM_DPICHANGED` 重建布局。
- 详情卡/悬停几何在两形态下各自成立（胶囊的卡半径 22、罗盘的卡半径 43/23，原型 RADII 表同款）。

- [ ] **Step 1: 实现两形态 + 拖拽 + 多屏**
- [ ] **Step 2: 构建零警告 + 单测全绿 + 视觉验收（三形态 × 左右缘截图）**
- [ ] **Step 3: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 胶囊/星环形态、拖拽换边与多显示器"
```

---

### Task 6: 自绘玻璃右键菜单

**Files:**
- Create: `okmeter/ui/menu.h`、`okmeter/ui/menu.cpp`
- Modify: `okmeter/ui/app.cpp`（WM_RBUTTONUP 改走自绘菜单）

**行为规格（规格 §3.3 + §3.5 合并）：**
- **不用原生 Win32 菜单**。自绘 D2D 玻璃菜单：材质跟随当前材质（液态/光感下同质感），深色底 + hairline + 圆角 10 + 弹出动画 120ms；菜单区纳入 dock 窗口（同 swapchain 绘制，参照详情卡的卡区扩展做法）。
- **球上右键** → 菜单内容：标题行"第 N 项 · 位置"、指标映射子项组（`默认 · 按最近使用` / `总量 · 当前会话|今日|本周|全部累计` / `模型 · <每个已观测模型>`，当前值打 ✓）、分隔线、`设置…`、`换边`、`退出`。
- **弧线/空白右键** → `设置…` / `换边` / `退出`。
- 菜单外点击/Escape 收起；换边/退出/改映射全部真实生效（改映射 → 改 cfg.mapping[i] → saveConfig → rebuild）；本任务仍无设置面板时，`设置…` 项**灰化**（合法可见反馈），Task 7 点亮。
- 顺手修：pollData 零新事件时不 flush（`kimi.poll` 返回 0 则跳过 `store_->flush()`）。

- [ ] **Step 1: 实现自绘菜单**
- [ ] **Step 2: 构建零警告 + 单测全绿（41 cases）+ 手动验收**：球上右键改映射立即生效（截图：改后球值变化）、✓ 标记正确、菜单外点击收起、设置项灰化
- [ ] **Step 3: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 自绘玻璃右键菜单与每球指标映射"
```

---

### Task 7: 背板式设置面板

**Files:**
- Create: `okmeter/ui/settings.h`、`okmeter/ui/settings.cpp`
- Modify: `okmeter/ui/app.cpp`（菜单"设置…"点亮）

**行为规格（规格 §3.5 + 修订条款）：**
- **背板式**：停靠在 dock 对侧屏幕边缘的玻璃面板（宽 328px、顶标题栏 + 滚动区 + 底部操作条），非居中模态；面板皮肤跟随当前材质（材质模块供皮，规格 §3.5"设置面板跟随当前生效材质"）。
- 控件全部自绘（与原型 `okmeter-dock-prototype.html` 设置面板同款）：
  - 材质效果：4 个带缩略图的选项卡（2 列网格）
  - 视觉形态：3 个带缩略图选项卡（arc/capsule/compass）
  - 球数量：1/3/5/7 chips
  - 指标映射：每位置一行自绘玻璃下拉（默认/总量四口径/模型列表）
  - 吸附边：左/右 chips
  - 数字合并 cache：玻璃开关
- **两段式保存**：所有控件只改 draft；`保存并生效` → normalize + saveConfig + 全量 rebuild；`取消`/✕/Escape → 丢弃。面板打开期间 dock 保持展开。
- 面板出现时带 180ms 滑入动画；详情卡三项留档（cardin 动画、长 id 省略号、垂直夹取）在本任务一并处理。

- [ ] **Step 1: 实现背板面板**
- [ ] **Step 2: 构建零警告 + 单测全绿 + 手动验收**：改材质保存后面板+dock 同步换肤；改映射/球数/吸附边保存生效；取消丢弃；两段式语义完整
- [ ] **Step 3: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 背板式设置面板（两段式保存，跟随材质）"
```

---

### Task 8: 视觉校准与综合验收

**Files:**
- 视前面任务产出微调（dock_scene/materials）
- Modify: `docs/2026-09-08-token-dock-design.md` §8（如有新发现的风险）

**内容：**
- 测试补强（留档归口）：minjson `\u4e2d\ud83d\ude00` 转义用例、week 周一/周日交界与 modelWeek 断言（预期 43 cases）。
- 逐项肉眼校准（原型并排对比表）：球体层次（内高光/外阴影）、弧线微光、详情卡玻璃化与 cardin 140ms、收缩态细边+弧线微光入露出条、数值字重。
- 综合验收矩阵：4 材质 × 3 形态 × 左右缘 ×（收缩/展开/悬停/详情卡/右键菜单/设置面板）截图存档 `.superpowers/sdd/tmp/p2b-matrix/`。
- 长时间运行观察：dock 静置 10 分钟无内存泄漏迹象（任务管理器内存稳定）、无 CPU 空转（静止帧 CPU ≈ 0）。

- [ ] **Step 1~3：补强测试 / 视觉校准 / 综合验收矩阵**

- [ ] **Step 4: Commit**

```bash
git add okmeter docs
git commit -m "feat(okmeter): 原型级视觉校准与综合验收"
```

---

## 后续（原 Plan 4）

托盘菜单拉起、build-dist.sh MSVC 步骤、version.rc 接 sync-version、iss [Files]、/SUBSYSTEM:WINDOWS、发布验收。

## Self-Review 记录

- 用户三条反馈全部归口：材质感 → Task 2/3/4；动画卡顿 → Task 1（诊断先行）；右键菜单 → Task 6（含每球映射子项）+ Task 7（设置项点亮）。
- 规格覆盖：§3.2 拖拽/多屏 → Task 5；§3.3 右键映射 → Task 6；§3.4 → Task 3/8 校准；§3.5 设置 → Task 7；§4 降级链 → Task 2。
- 线程契约：Task 2 的 WGC 回调边界写明只交换纹理；Task 4 粒子/光源计算在渲染帧内。
- 风险：D2D DisplacementMap 对折射的表现力若不足，liquid 的折射允许改用 D3D pixel shader（Task 4 内自行选择，报告里写理由）。
