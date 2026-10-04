# OkMeter 横向 Dock（顶部 / 状态栏上方）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 OkMeter dock 增加"吸附屏幕顶部 / 状态栏上方（底部）"的横向形态：居中簇状条 + mini chip 收缩样式 + 逐项 morph 动画 + 方向适配的详情卡/菜单/设置面板，左/右竖条既有行为逐像素不变。

**Architecture:** 绑定规格是两份文档——`docs/2026-09-13-dock-horizontal-design.md`（设计定论）与 `docs/prototypes/prototype-dock-horizontal-v2.html`（用户逐轮拍板的原型，几何/动画数值以此为准）。实现上把"横向"收敛为少数纯函数（`isHorizEdge` / `layoutMini` / `morphGeom`），渲染层通过 `DrawContext.mini` + `ctx.e` 做逐项插值 morph，app 层所有横向逻辑挂在 `isHorizEdge(cfg_.edge)` 分支下。

**Tech Stack:** C++20 / Win32 / D3D11→D2D1→DWrite（零依赖，MSVC 编译，`okmeter/build.bat` 一条命令编译+跑单测+出 exe）。

## Global Constraints

- **左/右竖条一切既有行为完全不变**（收缩露半条 + 55% 降暗、弹簧滑出、wave 流向镜像、级联菜单、详情卡）——横向功能全部挂 `isHorizEdge()` 分支。
- **收缩不是截断、不沉出屏外**：横向收缩 = 独立 mini chip 排（名称+读数，居中项 accent，条高 ~32×scale）。
- **展开⇄收缩 = 逐项 morph**：框几何（w/h/圆角）+ 位置按 e 插值；形态细节（波形/量条/数码/轨道）随 e 渐隐；chip 文字随 (1-e) 渐入。展开 = 收缩精确反向，无独立入场动画。
- **形变进行中禁弹详情卡**：弹簧落定（emerged_）后按指针实际落点补弹。
- **形态切换 mini 中继**：横向展开态在设置里保存 → 先 morph 缩回 chip 排 → 落定后静默重建（`createModules()` 直接换模块，**绝对禁止任何淡入淡出/透明度过渡**）。边缘变化与竖向维持原样直接重建。
- 鼠标离开 → 600ms 迟滞回缩（沿用）。
- 详情卡方向：顶部 → 向下弹；底部 → 向上弹；水平居中对齐条目并夹取视口。
- glow 材质环境光源换缘（上缘 → 光在屏顶外；下缘 → 光在任务栏外）；arc 弧线朝屏心鼓（上缘向下弯/下缘向上弯）；compass 保持径向。
- 比例法不变：`uiScale(screenH)` 以屏高为基准，所有原型几何值 × uiScale。
- 构建验证统一命令：`cd okmeter && cmd //c build.bat`（先编译并运行单测，再出 `build/OkMeter.exe`；任一失败即停）。
- 提交信息沿用仓库风格（`okmeter: ...`，中文简述）。

## 关键接口契约（跨 Task 一致，先读这里）

`okmeter/ui/geometry.h` 新增/修改（Task 2 落地）：

```cpp
// 吸附边判定：top/bottom = 横向条
inline bool isHorizEdge(const std::string& edge) {
  return edge == "top" || edge == "bottom";
}

struct ItemGeom {
  double x = 0, y = 0;     // 项中心（dock 盒内坐标）
  double r = 0;            // 半径（胶囊为半高）
  double hw = 0;           // 水平半宽（>r 时项为胶囊；0=圆形项）
  double scale = 1;        // 悬停放大
  double dy = 0;           // 悬停让位纵向偏移（+ 向下）
  double dx = 0;           // 悬停让位横向偏移（+ 向右；横向条专用）
};

// 签名全部加 screenW（横向 arc 步长公式用屏宽；竖向行为不变）
DockGeom layoutArc(int n, int radius, int gap, int screenW, int screenH, const std::string& edge);
DockGeom layoutCapsule(int n, int screenW, int screenH, const std::string& edge);
DockGeom layoutCompass(int n, double rotDeg, int screenH);  // 不变（径向，方向无关）

// applyHover 加 horizontal：true 时让位写 dx 不写 dy
void applyHover(DockGeom& g, int hoverIdx, double hoverScale, double push,
                double dimShrink = 1.0, bool horizontal = false);

// 横向 mini chip 排（收缩样式）：widths = 各 chip 宽（已含 22×scale 内边距）。
// 原型 buildMini：gap=6 padX=20 chipH=26 盒高=32（均 ×scale）。
// 返回盒 w/h；items[i] = chip 中心 x/y、hw=半宽、r=13×scale（pill 半高）
DockGeom layoutMini(const std::vector<double>& widths, double scale);

// 横向 morph：mini(收缩 e=0) ↔ stage(展开 e=1) 逐项插值，输出当前盒坐标系。
// 盒 w/h 同步插值；水平两盒居中轴对齐；anchorBottom=true（底缘）时两盒底对齐
DockGeom morphGeom(const DockGeom& stage, const DockGeom& mini, double e, bool anchorBottom);
```

`okmeter/render/form.h` 修改（Task 4 落地）：

```cpp
// IForm::layout 签名加 screenW
virtual DockGeom layout(int n, int screenW, int screenH, const std::string& edge) const = 0;

struct DrawContext {
  // ……既有字段不变，新增：
  const DockGeom* mini = nullptr;        // 横向 mini chip 几何（nullptr=竖向）
  IDWriteTextFormat* miniNameFmt = nullptr;  // 横向 chip 名：Segoe UI 9.5 左对齐
  IDWriteTextFormat* miniValFmt = nullptr;   // 横向 chip 值：Consolas 11 左对齐
};

// 横向 chip 文字（名 + 6px + 值，整体居中于 cx；中心项值 accent 0x5FE0A8）
void drawChipText(const DrawContext& ctx, ID2D1DeviceContext* dc, size_t i,
                  float cx, float cy, float alpha);
```

`okmeter/render/dock_scene.h/.cpp` 修改（Task 4）：

```cpp
// ensure() 增建 miniNameFmt_（Segoe UI 9.5 LEADING）/ miniValFmt_（Consolas 11 LEADING）
// 新增：chip 宽测量（名 miniNameFmt + 6 + 值 miniValFmt + 22 内边距，均已 ×scale）
std::vector<double> measureChipWidths(const std::vector<DockItem>& items);
// draw() 签名加 mini（nullptr=竖向）
void draw(..., float backdropDX, float backdropDY, int pressIdx, const DockGeom* mini);
// drawCard 签名加 winW（横向水平夹取用）
void drawCard(D3DContext&, IMaterial&, const DetailCard&, const std::string& edge,
              const DockGeom& g, float dx, double winW, double winH,
              int hoverIdx, double cardRadius);
```

`okmeter/ui/app.h` 新增成员/帮手（Task 3/4/5/7 落地）：

```cpp
DockGeom barGeom(double e);        // 竖向=layout+applyHover；横向=morphGeom(stage,mini,e,edge=="bottom")
void barBox(double e, int& w, int& h) const;  // 条盒尺寸（不含卡区）
DockGeom miniG_;                   // 横向 mini 几何（rebuildLayout 重算）
std::vector<double> chipW_;        // chip 宽缓存（rebuildItems 经 scene_.measureChipWidths 重算）
int winX_ = 0;                     // 横向水平居中 x（rebuildLayout 重算）
int baseW_ = 0, baseH_ = 0;        // applyWindowPos 记录的基础矩形尺寸（穿透/命中用）
bool pendingApply_ = false;        // mini 中继：弹簧落定后应用设置草稿
Config pendingDraft_{};
```

---

### Task 1: Config 四边 + 设置面板吸附边 chips

**Files:**
- Modify: `okmeter/core/config.h:13`、`okmeter/core/config.cpp:21`
- Modify: `okmeter/ui/settings.cpp:291-307`（chips 建）、`:381-383`（click）、`:700-713`（高亮+文字）
- Test: `okmeter/tests/test_config.cpp:25,29`

**Interfaces:**
- Produces: `Config::edge` 合法域 = `left/right/top/bottom`；设置面板 4 枚 EdgeChip（`c.a`：0=左 1=右 2=上 3=下）。

- [ ] **Step 1: 改失败测试**

`okmeter/tests/test_config.cpp` 的 `config_defaults_and_normalize`：`c.edge = "top"; // v1 不支持：回退` 改为：

```cpp
  c.count = 4;              // 偶数：钳回奇数
  c.edge = "diag";          // 非法边：回退 right
  c.form = "unknown";
  c.normalize();
  CHECK(c.count == 3);
  CHECK(c.edge == "right");
  CHECK(c.form == "arc");
  c.edge = "top";           // 横向边：合法保留
  c.normalize();
  CHECK(c.edge == "top");
  c.edge = "bottom";
  c.normalize();
  CHECK(c.edge == "bottom");
```

`config_roundtrip` 尾部追加一段（remove_all 之前）：

```cpp
  c.edge = "bottom";
  CHECK(saveConfig(d, c));
  CHECK(loadConfig(d, back));
  CHECK(back.edge == "bottom");
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd okmeter && cmd //c build.bat`
Expected: 单测 FAIL（`config_defaults_and_normalize`：`c.edge == "top"` 断言失败——normalize 仍回退 right）。

- [ ] **Step 3: 实现 normalize 四边**

`okmeter/core/config.cpp:21`：

```cpp
  if (edge != "right" && edge != "left" && edge != "top" && edge != "bottom")
    edge = "right";
```

`okmeter/core/config.h:13` 注释改为 `// right / left / top / bottom（top=屏幕顶部，bottom=状态栏上方）`。

- [ ] **Step 4: 设置面板 4 枚 chips**

`okmeter/ui/settings.cpp:294-296`：

```cpp
    const wchar_t* names[] = {L"左", L"右", L"上", L"下"};
    for (int i = 0; i < 4; ++i) {
```

`settings.cpp:381-383`（Ctrl::EdgeChip click）：

```cpp
  case Ctrl::EdgeChip:
    draft.edge = c.a == 0 ? "left" : c.a == 1 ? "right" : c.a == 2 ? "top" : "bottom";
    return 1;
```

`settings.cpp:703`（高亮判定）：

```cpp
            : (c.a == 0 ? draft.edge == "left"
               : c.a == 1 ? draft.edge == "right"
               : c.a == 2 ? draft.edge == "top" : draft.edge == "bottom");
```

`settings.cpp:713`（chip 文字）：

```cpp
            : (c.a == 0 ? L"左" : c.a == 1 ? L"右" : c.a == 2 ? L"上" : L"下");
```

节标题 `L"吸附边"` 改为 `L"吸附边（上/下 = 顶部/状态栏上方）"`。

- [ ] **Step 5: 跑测试确认通过**

Run: `cd okmeter && cmd //c build.bat`
Expected: 单测全过 + OkMeter.exe 编译成功。

- [ ] **Step 6: Commit**

```bash
git add okmeter/core/config.h okmeter/core/config.cpp okmeter/ui/settings.cpp okmeter/tests/test_config.cpp
git commit -m "okmeter: 吸附边配置域扩为 left/right/top/bottom（设置面板四枚 chips）"
```

---

### Task 2: 几何层——横向布局 + mini 布局 + morph 插值

**Files:**
- Modify: `okmeter/ui/geometry.h`、`okmeter/ui/geometry.cpp`
- Modify: `okmeter/render/form.h:108`（IForm::layout 签名）
- Modify: `okmeter/render/forms/arc.cpp`、`capsule.cpp`、`level.cpp`、`wave.cpp`、`nixie.cpp`、`compass.cpp`（仅 layout 实现）
- Modify: `okmeter/ui/app.cpp` 全部 `form_->layout(cfg_.count, screenH, cfg_.edge)` 调用点（约 6 处：rebuildLayout:347、renderOnce:970、WM_MOUSEMOVE:1354、WM_LBUTTONDOWN:1445、WM_RBUTTONUP:1517、run:1681、shot:1266/1293）——本任务只补 screenW 实参（`work.right - work.left`），行为不变
- Test: `okmeter/tests/test_geometry.cpp`（既有调用补 screenW + 新增横向用例）

**Interfaces:**
- Consumes: Task 1 的四边 edge。
- Produces: 「关键接口契约」节的全部 geometry.h 函数；`IForm::layout(n, screenW, screenH, edge)`。

原型数值基准（`prototype-dock-horizontal-v2.html` geom()，全 ×uiScale）：

| 形态 | 横向 step | 横向盒 W | 横向盒 H | 项半宽 hw | 项半高 r |
|---|---|---|---|---|---|
| arc | min(92, (screenW*0.6/2−58)/mid) | 2·(mid·step+58) | 150 | 0（圆形） | 30 |
| capsule | 186 | 2·(mid·186+98) | 96 | 87 | 20 |
| level | 196 | 2·(mid·196+103) | 116 | 92 | 26 |
| wave | 206 | 2·(mid·206+109) | 136 | 98 | 32 |
| nixie | 118 | 2·(mid·118+64) | 128 | 53 | 32 |
| compass | — | 252 | 252 | 0 | hub 43 / 卫星 23 |

注意：原型横向 row() 的 W/H 与项半宽半高有的和竖向版不同（如 level 竖向块 184 宽、横向 206 宽）——横向以**上表**为准；现有 C++ 各 form 内联 layout 里的 hw/r 常量继续用于竖向。若某项半宽无法从上表直接对应 C++ 常量，按 `W = 2·(mid·step + hw + 11)` 反推校验（capsule 原型 174+22=196 → hw=87 即竖向同款）。

arc 横向：项中心 `x = W/2 + (i−mid)·step`，`y = 30 + 46·(1−fr²)`（edge=top，向下鼓）；edge=bottom 时 `y = H − y`。r = radius×s。

- [ ] **Step 1: 写失败测试（geometry.h 新函数 + 横向布局）**

`okmeter/tests/test_geometry.cpp` 既有 `layoutArc(3, 30, 14, 900, "right")` 等调用全部补屏宽实参（如 `layoutArc(3, 30, 14, 1920, 900, "right")`、`layoutCapsule(3, 1920, 1080, "right")`）。文件尾追加：

```cpp
TEST(geom_arc_horizontal_top_bottom_mirror) {
  auto t = layoutArc(3, 30, 14, 1920, 1080, "top");
  auto b = layoutArc(3, 30, 14, 1920, 1080, "bottom");
  CHECK(t.w > t.h);                        // 横向：宽 > 高
  CHECK(t.items[0].x < t.items[1].x && t.items[1].x < t.items[2].x);  // 横排
  CHECK(t.items[1].y > t.items[0].y);      // top：中心项向下鼓（朝屏心）
  for (size_t i = 0; i < 3; ++i) {
    CHECK(std::abs(t.items[i].y + b.items[i].y - t.h) < 1e-9);  // bottom = 垂直镜像
    CHECK(t.items[i].x == b.items[i].x);
  }
}

TEST(geom_wave_horizontal_row) {
  auto g = createWaveLikeRowForTest();  // 见下：直接调 IForm 太重，改为校验 layoutCapsule 横向
  (void)g;
  auto c = layoutCapsule(3, 1920, 1080, "top");
  CHECK(c.w > c.h);
  CHECK(std::abs(c.items[0].y - c.h / 2) < 1e-9);   // 全部垂直居中
  CHECK(std::abs(c.items[1].x - c.w / 2) < 1e-9);   // 中心项水平居中
  CHECK(std::abs(c.items[1].x - c.items[0].x - 186.0) < 1e-6);  // step 186 ×scale(=1)
}

TEST(geom_layout_mini_accumulates) {
  std::vector<double> w{80, 100, 80};   // scale=1：gap6 padX20 chipH26 盒32
  auto g = layoutMini(w, 1.0);
  CHECK(std::abs(g.w - (40 + 80 + 6 + 100 + 6 + 80)) < 1e-9);  // 312
  CHECK(std::abs(g.h - 32) < 1e-9);
  CHECK(std::abs(g.items[0].x - (20 + 40)) < 1e-9);
  CHECK(std::abs(g.items[1].x - (20 + 80 + 6 + 50)) < 1e-9);
  CHECK(std::abs(g.items[0].y - 16) < 1e-9);
  CHECK(std::abs(g.items[1].hw - 50) < 1e-9);
  CHECK(std::abs(g.items[0].r - 13) < 1e-9);
}

TEST(geom_morph_endpoints_and_anchor) {
  auto stage = layoutCapsule(3, 1920, 1080, "top");
  std::vector<double> w{80, 100, 80};
  auto mini = layoutMini(w, 1.0);
  auto g0 = morphGeom(stage, mini, 0.0, false);
  auto g1 = morphGeom(stage, mini, 1.0, false);
  CHECK(std::abs(g0.w - mini.w) < 1e-9 && std::abs(g0.h - mini.h) < 1e-9);
  CHECK(std::abs(g1.w - stage.w) < 1e-9 && std::abs(g1.h - stage.h) < 1e-9);
  for (size_t i = 0; i < 3; ++i) {
    CHECK(std::abs(g0.items[i].x - mini.items[i].x) < 1e-9);   // e=0 完全 mini 坐标
    CHECK(std::abs(g1.items[i].x - stage.items[i].x) < 1e-9);  // e=1 完全 stage 坐标
    CHECK(std::abs(g1.items[i].y - stage.items[i].y) < 1e-9);
  }
  auto gb = morphGeom(stage, mini, 0.0, true);   // 底缘锚定：盒底对齐
  CHECK(std::abs(gb.items[0].y - mini.items[0].y) < 1e-9);
  auto gb5 = morphGeom(stage, mini, 0.5, true);
  // 底对齐：各项到底边距离 = mini/stage 到底边距离的插值
  const double dMini = mini.h - mini.items[0].y, dStage = stage.h - stage.items[0].y;
  CHECK(std::abs((gb5.h - gb5.items[0].y) - (dMini + dStage) / 2) < 1e-9);
}

TEST(geom_apply_hover_horizontal_uses_dx) {
  auto g = layoutCapsule(3, 1920, 1080, "top");
  applyHover(g, 1, 1.0, 10.0, 1.0, true);
  CHECK(g.items[0].dx < 0 && g.items[2].dx > 0);   // 横向让位
  CHECK(g.items[0].dy == 0 && g.items[2].dy == 0);
  applyHover(g, -1, 1.0, 10.0, 1.0, true);
  CHECK(g.items[0].dx == 0 && g.items[2].dx == 0);
}
```

（`geom_wave_horizontal_row` 里的 `createWaveLikeRowForTest` 行删除——保留 capsule 校验即可，wave/level/nixie 横向由 Task 2 Step 4 的实现者自验编译。）

- [ ] **Step 2: 跑测试确认失败**

Run: `cd okmeter && cmd //c build.bat`
Expected: 编译失败（layoutMini/morphGeom/isHorizEdge 未定义，layoutArc 实参数不符）。

- [ ] **Step 3: geometry.h/.cpp 实现**

`geometry.h`：加 `isHorizEdge` inline、`ItemGeom.dx`、`applyHover` 加 `horizontal` 默认参、layoutArc/layoutCapsule 加 screenW、声明 layoutMini/morphGeom（契约节代码原样）。

`geometry.cpp`：

```cpp
DockGeom layoutArc(int n, int radius, int gap, int screenW, int screenH,
                   const std::string& edge) {
  DockGeom g;
  if (n < 1) return g;
  const int mid = (n - 1) / 2;
  const double s = uiScale(screenH);
  g.scale = s;
  if (isHorizEdge(edge)) {
    // 横向（原型 geom() arc 横排）：弧线朝屏心鼓（top 向下 / bottom 向上）
    const double step = mid > 0
        ? std::min(92.0 * s, (screenW * 0.6 / 2 - 58 * s) / mid)
        : 0.0;
    g.w = 2 * (mid * step + 58 * s);
    g.h = 150 * s;
    g.items.resize((size_t)n);
    for (int i = 0; i < n; ++i) {
      const double fr = mid == 0 ? 0.0 : (double)(i - mid) / mid;
      double y = (30 + 46 * (1 - fr * fr)) * s;
      if (edge == "bottom") y = g.h - y;
      g.items[(size_t)i].x = g.w / 2 + (i - mid) * step;
      g.items[(size_t)i].y = y;
      g.items[(size_t)i].r = radius * s;
    }
    return g;
  }
  // ……原竖向实现逐行保留（step 公式、g.w/g.h、edge=="left" x 镜像）……
}

DockGeom layoutCapsule(int n, int screenW, int screenH, const std::string& edge) {
  DockGeom g;
  if (n < 1) return g;
  const int mid = (n - 1) / 2;
  const double s = uiScale(screenH);
  g.scale = s;
  g.connector = false;
  g.items.resize((size_t)n);
  if (isHorizEdge(edge)) {
    const double step = 186 * s;  // 原型横向 capsule：step 186
    g.w = 2 * (mid * step + 98 * s);
    g.h = 96 * s;
    for (int i = 0; i < n; ++i) {
      g.items[(size_t)i].x = g.w / 2 + (i - mid) * step;
      g.items[(size_t)i].y = g.h / 2;
      g.items[(size_t)i].r = 20 * s;
      g.items[(size_t)i].hw = 87 * s;
    }
    return g;
  }
  // ……原竖向实现逐行保留……
}

DockGeom layoutMini(const std::vector<double>& widths, double scale) {
  DockGeom g;
  if (widths.empty()) return g;
  const double gap = 6 * scale, padX = 20 * scale;
  g.scale = scale;
  g.connector = false;
  g.h = 32 * scale;
  g.w = padX * 2;
  for (size_t i = 0; i < widths.size(); ++i)
    g.w += widths[i] + (i ? gap : 0);
  g.items.resize(widths.size());
  double x = padX;
  for (size_t i = 0; i < widths.size(); ++i) {
    g.items[i].x = x + widths[i] / 2;
    g.items[i].y = g.h / 2;
    g.items[i].hw = widths[i] / 2;
    g.items[i].r = 13 * scale;  // chipH/2（pill 半高）
    x += widths[i] + gap;
  }
  return g;
}

static double lerpD(double a, double b, double t) { return a + (b - a) * t; }

DockGeom morphGeom(const DockGeom& stage, const DockGeom& mini, double e,
                   bool anchorBottom) {
  if (e < 0) e = 0;
  if (e > 1) e = 1;
  DockGeom g;
  g.scale = stage.scale;
  g.connector = stage.connector;
  g.w = lerpD(mini.w, stage.w, e);
  g.h = lerpD(mini.h, stage.h, e);
  const size_t n = std::min(stage.items.size(), mini.items.size());
  g.items.resize(n);
  for (size_t i = 0; i < n; ++i) {
    const ItemGeom& a = mini.items[i], &b = stage.items[i];
    // 水平：两盒居中轴对齐（各自中心 − 各自半宽插值后回到新盒中心系）
    g.items[i].x = lerpD(a.x - mini.w / 2, b.x - stage.w / 2, e) + g.w / 2;
    g.items[i].y = anchorBottom
        ? g.h - lerpD(mini.h - a.y, stage.h - b.y, e)   // 底缘：盒底对齐
        : lerpD(a.y, b.y, e);                            // 顶缘：盒顶对齐
    g.items[i].r = lerpD(a.r, b.r, e);
    g.items[i].hw = lerpD(a.hw, b.hw, e);
    g.items[i].scale = b.scale;
  }
  return g;
}
```

`applyHover`：签名加 `bool horizontal`，让位行改为：

```cpp
    if (horizontal) it.dx = ((int)i < hoverIdx ? -amount : amount);
    else            it.dy = ((int)i < hoverIdx ? -amount : amount);
```

（复位分支 `it.dy = 0;` 处同时 `it.dx = 0;`。）

- [ ] **Step 4: 六个 form 的 layout 签名 + 横向分支**

`render/form.h:108` 改契约签名。各 form 的 `layout(int n, int screenH, const std::string& edge)` 改 `layout(int n, int screenW, int screenH, const std::string& edge)`：

- **arc.cpp**：改为 `return layoutArc(n, 30, 14, screenW, screenH, edge);`（原本就委托 geometry，只补实参；若它传自己的 radius 常量则保留原值）。
- **capsule.cpp**：改为 `return layoutCapsule(n, screenW, screenH, edge);`（同委托）。
- **wave.cpp**：竖向分支原样保留；`if (isHorizEdge(edge))` 横向分支按基准表：`step=206·s`、`g.w=2·(mid·step+109·s)`、`g.h=136·s`、`x=g.w/2+(i−mid)·step`、`y=g.h/2`、`hw=98·s`、`r=32·s`。
- **level.cpp**：横向 `step=196·s`、`g.w=2·(mid·step+103·s)`、`g.h=116·s`、`hw=92·s`、`r=26·s`。
- **nixie.cpp**：横向 `step=118·s`、`g.w=2·(mid·step+64·s)`、`g.h=128·s`、`hw=53·s`、`r=32·s`。
- **compass.cpp**：`layout(n, screenW, screenH, edge)` 直接 `return layoutCompass(n, rotDeg_, screenH);`（横向同样径向 252×252；`(void)screenW; (void)edge;`）。注意 compass 现有 layout 是否已委托 layoutCompass——按现状最小改动。

- [ ] **Step 5: app.cpp 调用点补 screenW**

所有 `form_->layout(cfg_.count, screenH, cfg_.edge)` 改为 `form_->layout(cfg_.count, screenW, screenH, cfg_.edge)`，每个调用点已有 `RECT work`，在 `screenH` 上一行加 `const int screenW = (int)(work.right - work.left);`。本任务不改任何行为。

- [ ] **Step 6: 跑测试确认通过 + 竖向回归**

Run: `cd okmeter && cmd //c build.bat`
Expected: 全部 PASS（含既有竖向用例）+ exe 编译成功。

- [ ] **Step 7: Commit**

```bash
git add okmeter/ui/geometry.h okmeter/ui/geometry.cpp okmeter/render/form.h okmeter/render/forms/ okmeter/ui/app.cpp okmeter/tests/test_geometry.cpp
git commit -m "okmeter: 几何层横向化——横向布局/layoutMini/morphGeom/applyHover 横向让位"
```

---

### Task 3: app 窗口几何横向化（定位/卡区/拖拽/换边/穿透）

**Files:**
- Modify: `okmeter/ui/app.h`（成员：winX_/baseW_/baseH_/miniG_/chipW_ + barGeom/barBox 声明；`kCardZoneH` 常量）
- Modify: `okmeter/ui/app.cpp`：rebuildLayout:344、applyWindowPos:361、syncClickThru:410、flipEdge:454、renderOnce:968-986（screenW/barGeom 替换 + 详情卡调用见 Task 5）、WM_MOUSEMOVE:1352-1357、WM_LBUTTONDOWN:1443-1449、WM_LBUTTONUP 落点:1483-1501、WM_RBUTTONUP:1515-1520、WM_NCHITTEST:1541、run 初始窗口:1679-1693、shot 路径:1266/1288-1296

**Interfaces:**
- Consumes: Task 2 的 isHorizEdge/morphGeom/layoutMini、新 IForm::layout 签名。
- Produces: `barGeom(double e)` / `barBox(double e, int& w, int& h)` / `winX_` / `baseW_` / `baseH_` / `miniG_` / `chipW_`；`constexpr int kCardZoneH = 300;`（横向卡区高，未缩放——与 kCardZoneW 同款策略）。

关键决策（实现者照做，勿自由发挥）：
- 横向卡区 = 垂直方向 +300：edge=top 卡区在条下方（y 不变 h+300）；edge=bottom 卡区在条上方（y−300，zoneDY_ += 300）。
- `flipEdge()` 改为换到**对侧**：left↔right、top↔bottom。
- 拖拽落点：窗口中心到所在屏工作区四边距离取最近边。
- 本任务 chip 宽未知时 `chipW_` 为空 → rebuildLayout 用估值 `96×scale`/项搭 miniG_（Task 4 才会填真实测量值；此处保证编译与形态可用）。

- [ ] **Step 1: barGeom/barBox 帮手**

app.cpp（`hitItem` 之后加）：

```cpp
// 当前条几何：竖向 = form layout + 悬停让位；横向 = mini⇄stage 按 e 插值（morph）。
// 返回坐标系 = 条盒（左上角原点）；调用方自行叠加 zoneDX_/zoneDY_
DockGeom DockApp::barGeom(double e) {
  const RECT work = workArea();
  const int screenW = (int)(work.right - work.left);
  const int screenH = (int)(work.bottom - work.top);
  DockGeom g = form_->layout(cfg_.count, screenW, screenH, cfg_.edge);
  applyHover(g, hoverIdx_, form_->hoverScale(), form_->hoverPush(),
             form_->hoverDimShrink(), isHorizEdge(cfg_.edge));
  if (!isHorizEdge(cfg_.edge) || miniG_.items.empty()) return g;
  return morphGeom(g, miniG_, e, cfg_.edge == "bottom");
}

// 条盒尺寸（不含卡区）：竖向 = g.w/g.h（高有 120 地板）；横向 = morph 插值尺寸
void DockApp::barBox(double e, int& w, int& h) const {
  const RECT work = workArea();
  const int screenW = (int)(work.right - work.left);
  const int screenH = (int)(work.bottom - work.top);
  DockGeom g = form_->layout(cfg_.count, screenW, screenH, cfg_.edge);
  if (isHorizEdge(cfg_.edge) && !miniG_.items.empty()) {
    DockGeom m = morphGeom(g, miniG_, e, cfg_.edge == "bottom");
    w = (int)std::lround(m.w);
    h = (int)std::lround(m.h);
    return;
  }
  w = (int)g.w;
  h = (int)g.h;
  if (h < 120) h = 120;
}
```

注意：barGeom 是**非 const**（workArea 用 hwnd_），app.h 声明不带 const；barBox 可 const（workArea 是 const）。barGeom 替换 5 处 `layout + applyHover` 重复段（WM_MOUSEMOVE/LBUTTONDOWN/RBUTTONUP/renderOnce/shot 两处），每处保留 `zoneDY_` 修正：横向 morph 后项坐标已在条盒系，`hitItem(g, mx, my - zoneDY_, (float)zoneDX_)` 调用方式不变。

- [ ] **Step 2: rebuildLayout / applyWindowPos 横向分支**

rebuildLayout：

```cpp
void DockApp::rebuildLayout() {
  const RECT work = workArea();
  const int screenW = (int)(work.right - work.left);
  const int screenH = (int)(work.bottom - work.top);
  const DockGeom g = form_->layout(cfg_.count, screenW, screenH, cfg_.edge);
  if (isHorizEdge(cfg_.edge)) {
    // mini 几何：chip 宽未测量（启动首期）按 96×scale 估值，Task 4 起为实测
    std::vector<double> w = chipW_;
    if (w.size() != (size_t)cfg_.count)
      w.assign((size_t)cfg_.count, 96.0 * g.scale);
    miniG_ = layoutMini(w, g.scale);
    dockW_ = (int)g.w;        // 展开盒宽（卡水平夹取基准）
    winH_ = (int)g.h;         // 展开盒高
    winX_ = work.left + (screenW - dockW_) / 2;  // 水平居中
    winY_ = cfg_.edge == "top" ? work.top : work.bottom - winH_;
    applyWindowPos();
    return;
  }
  // ……原竖向三行（h 地板/dockW_/winH_/winY_）逐行保留……
  applyWindowPos();
}
```

applyWindowPos 在 `const double base = ...` 之前插横向分支：

```cpp
  if (isHorizEdge(cfg_.edge)) {
    int w, h;
    barBox(emerge_.value, w, h);
    baseW_ = w;
    baseH_ = h;
    int hh = h + (wide_ ? kCardZoneH : 0);
    int x = work.left + ((int)(work.right - work.left) - w) / 2;
    int y = cfg_.edge == "top" ? work.top : work.bottom - hh;
    backdrop_.setLumaRegion(x, y, w, h);
    int ux = x, uy = y, ur = x + w, ub = y + hh;
    if (menu_.open) { /* 同竖向并集四行 */ }
    if (settings_.open) { /* 同竖向并集四行 */ }
    zoneDX_ = x - ux;
    zoneDY_ = (wide_ && cfg_.edge == "bottom" ? kCardZoneH : 0) + (y - uy);
    if (menu_.open) menu_.place(...同竖向...);
    if (settings_.open) settings_.place(...同竖向...);
    SetWindowPos(...同竖向...);
    if (menu_.open && (menu_.parent1 >= 0 || menu_.parent2 >= 0)) placeSubColumns();
    return;
  }
```

竖向路径里 `w`/`h` 计算后补 `baseW_ = dockW_; baseH_ = winH_;`（供穿透/命中用）。注意竖向 zoneDX_ 现有逻辑（卡区 +268）保持不变。

`setWide` 注释更新（卡区方向随 edge）。

- [ ] **Step 3: flipEdge 对侧 + 拖拽最近边 + 穿透/命中矩形**

```cpp
void DockApp::flipEdge() {
  cfg_.edge = cfg_.edge == "right" ? "left"
            : cfg_.edge == "left"  ? "right"
            : cfg_.edge == "top"   ? "bottom" : "top";
  saveConfig(okmeterDir(), cfg_);
  rebuildLayout();
  render();
}
```

WM_LBUTTONUP 落点（:1492-1499）：

```cpp
        if (miOk) {
          const LONG cx = c.x, cy = c.y;
          struct { const char* e; LONG d; } cands[4] = {
              {"left", cx - mi.rcWork.left}, {"right", mi.rcWork.right - cx},
              {"top", cy - mi.rcWork.top}, {"bottom", mi.rcWork.bottom - cy}};
          const char* newEdge = cands[0].e;
          LONG best = cands[0].d;
          for (int k = 1; k < 4; ++k)
            if (cands[k].d < best) { best = cands[k].d; newEdge = cands[k].e; }
          if (newEdge != cfg_.edge) {
            cfg_.edge = newEdge;
            saveConfig(okmeterDir(), cfg_);
          }
        }
```

syncClickThru 的 ballZone（:422-425）改为屏幕坐标基础矩形（横竖统一）：

```cpp
    RECT wr2{};
    GetWindowRect(hwnd_, &wr2);
    const RECT ballZone{ wr2.left + zoneDX_, wr2.top + zoneDY_,
                         wr2.left + zoneDX_ + baseW_, wr2.top + zoneDY_ + baseH_ };
```

WM_NCHITTEST 的 dz（:1541）同样改 `{ zoneDX_, zoneDY_, zoneDX_ + baseW_, zoneDY_ + baseH_ }`。

- [ ] **Step 4: run() 初始窗口横向**

:1679-1693 在算完 `dockW_/winH_/winY_`（竖向）后加横向分支：

```cpp
  int x, w = dockW_, h0 = h;
  if (isHorizEdge(cfg_.edge)) {
    std::vector<double> cw((size_t)cfg_.count, 96.0 * g.scale);
    miniG_ = layoutMini(cw, g.scale);
    DockGeom m = morphGeom(g, miniG_, 0.0, cfg_.edge == "bottom");  // 初始收缩
    w = (int)std::lround(m.w);
    h0 = (int)std::lround(m.h);
    winX_ = work.left + (screenW - dockW_) / 2;
    winY_ = cfg_.edge == "top" ? work.top : work.bottom - (int)g.h;
    x = work.left + ((int)(work.right - work.left) - w) / 2;
  } else {
    x = cfg_.edge == "left" ? work.left + dockW_ / 2 - dockW_
                            : work.right - dockW_ / 2;
  }
  const int y0 = isHorizEdge(cfg_.edge)
      ? (cfg_.edge == "top" ? work.top : work.bottom - h0) : winY_;
```

CreateWindowExW 用 `x, y0, w, h0`；`d3d_.init(hwnd_, w, h0)`。

- [ ] **Step 5: 编译 + 竖向回归 shot**

Run: `cd okmeter && cmd //c build.bat`（单测全过 + 编译成功）。
再跑竖向自检截图确认左/右无回归（先 `taskkill //F //IM OkMeter.exe` 停掉运行实例）：

```bash
okmeter/build/OkMeter.exe --shot /tmp/v-arc.png
okmeter/build/OkMeter.exe --shotcap /tmp/v-arc-cap.png
```

Expected: 两张图与改动前一致（右缘弧线展开/收缩露半条）。恢复运行实例：`(cd /d/software/OpenKnowledge && ./OkMeter.exe &)`。

- [ ] **Step 6: Commit**

```bash
git add okmeter/ui/app.h okmeter/ui/app.cpp
git commit -m "okmeter: 窗口几何横向化——居中簇状条定位/垂直卡区/四边拖拽/对侧换边"
```

---

### Task 4: mini chip 测量/绘制 + 六形态逐项 morph

**Files:**
- Modify: `okmeter/render/dock_scene.h/.cpp`（mini 格式 + measureChipWidths + draw 加 mini 参 + ctx 装配）
- Modify: `okmeter/render/form.h`（DrawContext 加 mini/miniNameFmt/miniValFmt + drawChipText 声明；`drawChipText` 实现放 `render/forms/chip.cpp`，build.bat 的 `render\forms\*.cpp` 通配自动收编）
- Modify: `okmeter/render/forms/{arc,capsule,level,wave,nixie,compass}.cpp`（drawItems 横向分支）
- Modify: `okmeter/ui/app.cpp`（chipW_ 实测 + rebuildLayout 用实测 + renderOnce 传 mini + needsFrames 罗盘门控）

**Interfaces:**
- Consumes: Task 2/3 全部契约。
- Produces: `DockScene::measureChipWidths` / `drawChipText(ctx, dc, i, cx, cy, alpha)` / DrawContext.mini。

**morph 绘制规则（六形态统一，实现者照做）：**
- `const bool hz = ctx.mini != nullptr;`——geom（baked）已是 morph 插值结果（app 传入）。
- 框：osc.r/halfW 直接读 geom（已插值）；`osc.cornerR = hz ? lerp(mini.r, 形态角R, e) : 形态角R`（mini.r = `(*ctx.mini).items[i].r` = pill 半高 → e=0 纯 pill，e=1 形态角）。
- 不透明度：横向 `dimBase = 1.0f`（无 55% 降暗——mini 是全额 chip）；竖向原式不动。悬停他项降暗横竖同款。
- 形态内容（名/值/波形/量条/数码/轨道/卫星文本）：横向乘 `e` 渐隐（`const float fe = hz ? (float)e : 1.0f;`，所有内容墨色 alpha ×fe）。
- chip 文字：横向调 `drawChipText(ctx, dc, i, cx, cy, (float)(1.0 - e))`（cx/cy = geom 项中心）。
- arc：圆形项 morph 后 hw>r 自动走材质胶囊分支（无需特判）；球内双行文本 ×fe。
- compass：虚线轨道圆 ×fe；hub/卫星底框走通用 morph；`wantsTick()` 横向收缩期由 app 门控（见 Step 4）。
- 文本格式：chip 用 `ctx.miniNameFmt/miniValFmt`。

- [ ] **Step 1: dock_scene 加 mini 格式与测量**

`dock_scene.h` 加成员 `miniNameFmt_`/`miniValFmt_`（ComPtr<IDWriteTextFormat>）与方法声明；`ensure()` 的缓存判定与 Reset 清单补这两个格式，makeFmt 链尾追加：

```cpp
         makeFmt(dw, L"Segoe UI", 9.5f * scale, DWRITE_FONT_WEIGHT_NORMAL,
                 DWRITE_TEXT_ALIGNMENT_LEADING, &miniNameFmt_) &&
         makeFmt(dw, L"Consolas", 11.0f * scale, DWRITE_FONT_WEIGHT_NORMAL,
                 DWRITE_TEXT_ALIGNMENT_LEADING, &miniValFmt_);
```

`measureChipWidths`（注意 SetParagraphAlignment 是 CENTER，测量用 GetMetrics 不受影响）：

```cpp
std::vector<double> DockScene::measureChipWidths(const std::vector<DockItem>& items) {
  std::vector<double> out(items.size(), 0.0);
  if (!miniNameFmt_ || !miniValFmt_) return out;
  for (size_t i = 0; i < items.size(); ++i) {
    double w = 22.0;  // chip 内边距 11×2（原型 buildMini）
    auto meas = [&](IDWriteTextFormat* fmt, const std::wstring& s) -> double {
      if (s.empty()) return 0.0;
      ComPtr<IDWriteTextLayout> tl;
      if (FAILED(dwrite_->CreateTextLayout(s.c_str(), (UINT32)s.size(), fmt,
                                           4096.0f, 64.0f, &tl)))
        return 0.0;
      DWRITE_TEXT_METRICS m{};
      return SUCCEEDED(tl->GetMetrics(&m)) ? (double)m.width : 0.0;
    };
    // ensure 阶段格式已 ×scale；22/6 是原型值，需乘同一 scale
    const double s = (double)fmtScale_;
    w = 22.0 * s + meas(miniNameFmt_.Get(), items[i].label) + 6.0 * s +
        meas(miniValFmt_.Get(), items[i].value);
    out[i] = w;
  }
  return out;
}
```

（dwrite_ 是 DockScene 私有的话改为经 `D3DContext&` 入参取 `d3d.dwrite()`——签名定为 `measureChipWidths(D3DContext& d3d, const std::vector<DockItem>& items)`，内部先 `ensure(d3d, lastScale)` 保证格式存在；实现者按现场字段名对齐。）

`draw()` 签名加 `const DockGeom* mini`，ctx 装配补 `ctx.mini = mini; ctx.miniNameFmt = miniNameFmt_.Get(); ctx.miniValFmt = miniValFmt_.Get();`。

- [ ] **Step 2: drawChipText（render/forms/chip.cpp 新文件）**

```cpp
// render/forms/chip.cpp —— 横向 mini chip 文字：名 + 6px + 值，整体居中于项中心；
// 中心项值 accent（原型 .face-mini：k 9.5px ink-dim / v 11px mono ink，center accent）
#include "../form.h"
#include "../material.h"

using Microsoft::WRL::ComPtr;

namespace okmeter::render {

void drawChipText(const DrawContext& ctx, ID2D1DeviceContext* dc, size_t i,
                  float cx, float cy, float alpha) {
  if (!dc || !ctx.d3d || !ctx.brush || !ctx.miniNameFmt || !ctx.miniValFmt ||
      !ctx.items || i >= ctx.items->size() || alpha <= 0.003f)
    return;
  const DockItem& di = (*ctx.items)[i];
  auto meas = [&](IDWriteTextFormat* fmt, const std::wstring& s) -> float {
    if (s.empty()) return 0.0f;
    ComPtr<IDWriteTextLayout> tl;
    if (FAILED(ctx.d3d->dwrite()->CreateTextLayout(s.c_str(), (UINT32)s.size(),
                                                   fmt, 4096.0f, 64.0f, &tl)))
      return 0.0f;
    DWRITE_TEXT_METRICS m{};
    return SUCCEEDED(tl->GetMetrics(&m)) ? m.width : 0.0f;
  };
  const float gap = 6.0f * (float)ctx.geom->scale;
  const float nw = meas(ctx.miniNameFmt, di.label);
  const float vw = meas(ctx.miniValFmt, di.value);
  const float total = nw + (nw > 0 && vw > 0 ? gap : 0.0f) + vw;
  float x = cx - total * 0.5f;
  const float sc = (float)ctx.geom->scale;
  const bool center = (int)i == ctx.mid;
  if (nw > 0) {
    ctx.brush->SetColor(inkLight(0.66f * alpha));
    const D2D1_RECT_F tr = D2D1::RectF(x, cy - 8.0f * sc, x + nw + 1.0f, cy + 8.0f * sc);
    dc->DrawText(di.label.c_str(), (UINT32)di.label.size(), ctx.miniNameFmt, &tr,
                 ctx.brush);
    x += nw + gap;
  }
  if (vw > 0) {
    ctx.brush->SetColor(center ? D2D1::ColorF(0x5FE0A8, alpha)
                               : inkLight(0.93f * alpha));
    const D2D1_RECT_F tr = D2D1::RectF(x, cy - 8.5f * sc, x + vw + 1.0f, cy + 8.5f * sc);
    dc->DrawText(di.value.c_str(), (UINT32)di.value.size(), ctx.miniValFmt, &tr,
                 ctx.brush);
  }
}

} // namespace okmeter::render
```

（`inkLight` 在 material.h；`D2D1::ColorF(0x5FE0A8, a)` 的重载各 form 已用同款。）

- [ ] **Step 3: 六形态 drawItems 横向分支（wave 为范本，其余同构）**

wave.cpp drawItems 改造（其余形态按「morph 绘制规则」同构改）：

```cpp
    const bool hz = ctx.mini != nullptr;
    const double e = ctx.e < 0 ? 0 : ctx.e > 1 ? 1 : ctx.e;
    const float dimBase = hz ? 1.0f : (float)(0.55 + 0.45 * e);
    const float fe = hz ? (float)e : 1.0f;  // 形态内容渐隐
    // ……循环内：
    osc.cornerR = (hz ? (float)((*ctx.mini).items[i].r + (10.0 * g.scale - (*ctx.mini).items[i].r) * e)
                      : 10.0f) * 1.0f;  // pill→10×scale 插值（e=0 纯 pill）
    // 名/值文本颜色 alpha ×fe；sparkline 面积/折线 alpha ×fe
    // 循环尾（横向）：drawChipText(ctx, dc, i, c.x, c.y, (float)(1.0 - e));
```

- arc.cpp：球内 value/label 两行 alpha ×fe；dimBase 横向 1.0；chip 文字同款。arc 项 hw=0、mini hw>0 → morph 后 hw 渐增，材质胶囊分支自动接管，osc.cornerR 用 `lerp(mini.r, 球r, e)`（圆形项 cornerR 不生效，直接传 mini.r→it.r 插值即可，材质对圆形忽略 cornerR——按材质实现核对，若 drawOrbBack 圆形分支不看 cornerR 则无需改）。
- capsule.cpp/level.cpp/nixie.cpp：顶行名值/占比条/电平段/数码管 alpha ×fe；cornerR 插值（各自既有角 R）；chip 文字同款。
- compass.cpp：虚线轨道圆 alpha ×fe；hub/卫星底框 cornerR 插值、文本 ×fe；chip 文字同款（chip 中心 = morph 后项中心，卫星也会各自 morph 成 chip——与原型一致：每个条目一个 chip）。

- [ ] **Step 4: app 接线——chipW_ 实测 + renderOnce 传 mini + 罗盘门控**

`rebuildItems()` 尾部（rebuildCard() 之前）：

```cpp
  if (isHorizEdge(cfg_.edge) && hwnd_ && d3d_.ok()) {
    chipW_ = scene_.measureChipWidths(d3d_, items_);
    if (chipW_.size() != items_.size()) chipW_.clear();  // 测量失败 → 估值回退
  }
```

renderOnce：现有 `DockGeom g = form_->layout(...)` 段改为：

```cpp
  DockGeom g = barGeom(emerge_.value);
  if (zoneDY_ != 0)
    for (ItemGeom& it : g.items) it.y += zoneDY_;
  if (isHorizEdge(cfg_.edge) && zoneDX_ != 0)
    for (ItemGeom& it : g.items) it.x += zoneDX_;
```

scene_.draw 调用尾加 `isHorizEdge(cfg_.edge) && !miniG_.items.empty() ? &miniG_ : nullptr`。（注意：横向时 draw 内烘焙 `x += dx` 的 dx 参数传 0——横向让位已含在 barGeom 的 morphGeom 之前……**修正**：barGeom 里 applyHover 在 morph 之前对 stage 施加，morphGeom 插值带走 dx；renderOnce 不再叠加。dx 参数横向传 0。）

needsFrames 罗盘门控：

```cpp
  if (form_ && form_->wantsTick() &&
      !(isHorizEdge(cfg_.edge) && emerge_.value < 0.999))
    return true;  // 横向收缩 = mini chip 排，罗盘旋转不可见不驱帧
```

（animTick 里 `form_->tick(dt, emerge_.value)` 保留——横向展开时罗盘若自转仍生效。）

- [ ] **Step 5: 编译 + 横向端点 shot 验证**

```bash
cd okmeter && cmd //c build.bat
taskkill //F //IM OkMeter.exe
python -c "import json,pathlib; p=pathlib.Path.home()/'.okryptos/okmeter/config.json'; c=json.loads(p.read_text(encoding='utf-8')); c['edge']='top'; p.write_text(json.dumps(c),encoding='utf-8')"
okmeter/build/OkMeter.exe --shot /tmp/h-wave-open.png          # 展开态（当前形态）
okmeter/build/OkMeter.exe --shotcap /tmp/h-wave-mini.png       # mini chip 排
python -c "import json,pathlib; p=pathlib.Path.home()/'.okryptos/okmeter/config.json'; c=json.loads(p.read_text(encoding='utf-8')); c['edge']='right'; p.write_text(json.dumps(c),encoding='utf-8')"
(cd /d/software/OpenKnowledge && ./OkMeter.exe &)
```

Expected：展开图 = 顶部居中横排条目；mini 图 = 顶部 32px 高 chip 排（名+读数，居中项 accent），无 55% 降暗、无半遮。用 ReadMediaFile 逐张核对。edge 恢复 right。

- [ ] **Step 6: Commit**

```bash
git add okmeter/render/dock_scene.h okmeter/render/dock_scene.cpp okmeter/render/form.h okmeter/render/forms/ okmeter/ui/app.cpp okmeter/ui/app.h
git commit -m "okmeter: mini chip 排 + 六形态逐项 morph（框插值/细节渐隐/chip 文字）"
```

---

### Task 5: 详情卡横向（方向/夹取/形变禁卡/落定补弹）

**Files:**
- Modify: `okmeter/render/dock_scene.h/.cpp`（drawCard 加 winW + 横向分支）
- Modify: `okmeter/ui/app.cpp` renderOnce 卡条件与 cardin 方向:988-1013、animTick 落定补弹:1146-1149

**Interfaces:**
- Consumes: Task 3/4。
- Produces: `drawCard(..., double winW, double winH, ...)` 新签名。

规则：
- 横向卡定位：水平 = 悬停项中心 − cardW/2，夹 `[8, winW−cardW−8]`；垂直：edge=top → `y = (winH − kCardZoneH) + 12`（条区之下，截底保头同竖向逻辑，availH = kCardZoneH − 24）；edge=bottom → 卡区在条之上，`y` 在 `[12, kCardZoneH−12−drawH]` 内尽量贴条（`y = kCardZoneH − 12 − drawH`）。
- 显示条件（仅横向改）：`emerged_ && emergeTarget_ > 0.5`（形变中禁卡），替代竖向的 `emerge_.value > 0.5`；竖向条件不动。
- cardin 滑入方向：横向 = 垂直 6px（top：自上而下 → off 为 −(1−ct)·6；bottom：+(1−ct)·6）；竖向原水平 6px 不动。
- 落定补弹：弹簧 snap 后若横向且 target=1，按 GetCursorPos 实点重算 hoverIdx_ + rebuildCard。

- [ ] **Step 1: drawCard 横向分支**

`dock_scene.h/.cpp` drawCard 签名加 `double winW`（在 winH 前）。横向分支插在水平定位（:142-160）处：

```cpp
  float x, y;
  if (isHorizEdge(edge)) {
    const double anchorX = it.x + dx;
    x = (float)(anchorX - (double)kCardW / 2);
    const float maxX = (float)winW - kCardW - 8.0f;
    if (x > maxX) x = maxX;
    if (x < 8.0f) x = 8.0f;
  } else if (edge == "right") { ……原逻辑…… } else { ……原逻辑…… }
```

垂直定位（:161-169）横向分支：

```cpp
  if (isHorizEdge(edge)) {
    const float barH = (float)winH - 300.0f;  // kCardZoneH（app.cpp 常量同款值）
    const float availH = 300.0f - 24.0f;
    const float drawH = cardH > availH ? availH : cardH;
    y = edge == "top" ? barH + 12.0f : 300.0f - 12.0f - drawH;
  } else { ……原 anchorY 居中夹取逻辑…… }
```

（`300.0f` 引用处加注释指向 app.cpp kCardZoneH；drawH/contentBot 变量提升复用，勿重复定义。isHorizEdge 经 form.h→geometry.h 可用。）

- [ ] **Step 2: renderOnce 卡条件 + cardin 方向**

:988 条件改为：

```cpp
  const bool cardUp = isHorizEdge(cfg_.edge)
      ? (emerged_ && emergeTarget_ > 0.5)      // 形变中禁卡（落定补弹见 animTick）
      : (emerge_.value > 0.5);
  if (card_.valid && !menu_.open && hoverIdx_ >= 0 && hoverIdx_ < (int)g.items.size() &&
      cardUp) {
```

cardin off（:998-999）：

```cpp
      const bool hz = isHorizEdge(cfg_.edge);
      const float offX = hz ? 0.0f
          : (float)((1.0 - ct) * 6.0) * (cfg_.edge == "right" ? 1.0f : -1.0f);
      const float offY = !hz ? 0.0f
          : (float)((1.0 - ct) * 6.0) * (cfg_.edge == "top" ? -1.0f : 1.0f);
      dc->SetTransform(D2D1::Matrix3x2F::Translation(offX, offY));
```

两处 drawCard 调用补 `(double)(cr.right - cr.left)` 作 winW。

- [ ] **Step 3: animTick 落定补弹**

:1146-1149 snap 块后追加：

```cpp
  if (emerge_.settled(emergeTarget_)) {
    emerge_.snap(emergeTarget_);
    emerged_ = true;
    // 横向形变落定补弹：形变期禁卡（原型 morphing 期 hideCard），落定后按指针
    // 实际落点重算悬停（等价原型 cardAfterMorph 的 elementFromPoint 命中）
    if (isHorizEdge(cfg_.edge) && emergeTarget_ > 0.5 && !menu_.open &&
        !settings_.open) {
      POINT pt{};
      GetCursorPos(&pt);
      RECT wr{};
      GetWindowRect(hwnd_, &wr);
      const int mx = pt.x - (int)wr.left, my = pt.y - (int)wr.top;
      DockGeom g = barGeom(1.0);
      const int idx = (mx >= 0 && my >= 0)
          ? hitItem(g, mx, my - zoneDY_, (float)zoneDX_) : -1;
      if (idx != hoverIdx_) {
        hoverIdx_ = idx;
        rebuildCard();
      }
    }
  }
```

- [ ] **Step 4: 编译 + shot 验证**

```bash
cd okmeter && cmd //c build.bat
taskkill //F //IM OkMeter.exe
# config edge=top（同 Task 4 Step 5 的 python 一行）
okmeter/build/OkMeter.exe --shot /tmp/h-card.png    # 展开+悬停中心项 → 卡在条下方
# 恢复 edge=right，重启运行实例
```

Expected：顶部横条，详情卡在条下方、水平居中于中心项。ReadMediaFile 核对。再竖向 `--shot` 一张确认卡无回归。

- [ ] **Step 5: Commit**

```bash
git add okmeter/render/dock_scene.h okmeter/render/dock_scene.cpp okmeter/ui/app.cpp
git commit -m "okmeter: 详情卡横向——上缘下弹/下缘上弹、形变禁卡、落定补弹"
```

---

### Task 6: 菜单级联方向 + 设置面板横向定位

**Files:**
- Modify: `okmeter/ui/app.cpp` openMenu:618-661（menuDir_/落点）、openSettings:873-891（panelScreen_）

**Interfaces:**
- Consumes: Task 3 的 barBox/winY_。

规则：
- 横向球区菜单：`menuDir_ = 点击点 x < 工作区中线 ? +1 : -1`（朝屏心侧级联）；主列落点 sx 按 menuDir_ 取现有左/右两套逻辑中对应那套（dir<0 = 现 right 逻辑，dir>0 = 现 left 逻辑）；sy 夹取不变。
- 设置下拉（openSettingsMenu）：`menuDir_ = gsel 屏幕中心 x < 工作区中线 ? +1 : -1`。
- 设置面板：横向时水平居中（`x = work.left + (workW − kSettingsPanelW)/2`）；edge=top：`y = work.top + 展开条高 + 14`，高到 `work.bottom − 58`；edge=bottom：`y = work.top + 14`，高到 `work.bottom − 展开条高 − 14`。展开条高 = rebuildLayout 存的 winH_。

- [ ] **Step 1: openMenu 横向方向**

:624 改为：

```cpp
  POINT pt0{ clientX, clientY };
  ClientToScreen(hwnd_, &pt0);
  if (isHorizEdge(cfg_.edge)) {
    const RECT w0 = workArea();
    menuDir_ = pt0.x < (w0.left + w0.right) / 2 ? 1 : -1;  // 朝屏心侧级联
  } else {
    menuDir_ = cfg_.edge == "right" ? -1 : 1;
  }
```

（:626-627 的 ClientToScreen 复用 pt0，勿重复转换。）:632 的 `if (cfg_.edge == "right")` 改 `if (menuDir_ < 0)`（语义等价且覆盖横向两套落点）。

- [ ] **Step 2: openSettingsMenu 方向**

:735 改为：

```cpp
  {
    const RECT w0 = workArea();
    const D2D1_RECT_F gr = settings_.gselRect(gselCtrl);
    RECT wr0{};
    GetWindowRect(hwnd_, &wr0);
    const int gx = (int)wr0.left + (int)((gr.left + gr.right) / 2);
    menuDir_ = isHorizEdge(cfg_.edge)
        ? (gx < (w0.left + w0.right) / 2 ? 1 : -1)
        : (cfg_.edge == "right" ? 1 : -1);
  }
```

- [ ] **Step 3: openSettings 面板定位**

:879-883 改为：

```cpp
  const RECT work = workArea();
  int x, yTop, yBot;
  if (isHorizEdge(cfg_.edge)) {
    x = work.left + ((int)(work.right - work.left) - kSettingsPanelW) / 2;
    yTop = cfg_.edge == "top" ? work.top + winH_ + 14 : work.top + 14;
    yBot = cfg_.edge == "top" ? work.bottom - 58 : work.bottom - winH_ - 14;
  } else {
    x = cfg_.edge == "right" ? work.left + 14 : work.right - 14 - kSettingsPanelW;
    yTop = work.top + 14;
    yBot = work.bottom - 58;
  }
  const int h = yBot - yTop;
  panelScreen_ = RECT{ x, yTop, x + kSettingsPanelW, yTop + h };
```

- [ ] **Step 4: 编译 + shot 验证**

```bash
cd okmeter && cmd //c build.bat
taskkill //F //IM OkMeter.exe
# config edge=top
okmeter/build/OkMeter.exe --shotmenu 1 --shot /tmp/h-menu.png        # 球区菜单
okmeter/build/OkMeter.exe --shotsettings --shot /tmp/h-settings.png  # 设置面板
# 恢复 edge=right，重启运行实例
```

Expected：菜单朝屏心侧展开不遮条；面板水平居中、在条下方不重叠。ReadMediaFile 核对。

- [ ] **Step 5: Commit**

```bash
git add okmeter/ui/app.cpp
git commit -m "okmeter: 菜单级联朝屏心侧 + 设置面板横向居中避让条区"
```

---

### Task 7: 形态切换 mini 中继（无淡入淡出）

**Files:**
- Modify: `okmeter/ui/app.h`（pendingApply_/pendingDraft_ 成员）
- Modify: `okmeter/ui/app.cpp` closeSettings:896-922、animTick 落定块（Task 5 已加补弹，本任务在同块追加）

**Interfaces:**
- Consumes: Task 3/5。

规则（设计定论硬约束）：
- 触发条件：apply=true 且 `draft.edge == cfg_.edge`（未换边）且 `isHorizEdge(cfg_.edge)` 且当前展开（`emergeTarget_ > 0.5`）→ 中继：草稿入 pendingDraft_、置 pendingApply_、`setEmergeTarget(0)`，面板照常关闭；**不做** createModules/saveConfig（延后）。
- 落定应用：animTick 弹簧 snap 块内（Task 5 补弹代码之前）：

```cpp
    if (pendingApply_ && emerged_) {
      pendingApply_ = false;
      cfg_ = pendingDraft_;
      cfg_.normalize();
      saveConfig(okmeterDir(), cfg_);
      createModules();      // 静默重建：同位置同内容（mini chip 与形态无关），禁止任何透明度动画
      hoverIdx_ = -1;
      pressIdx_ = -1;
      rebuildItems();
      rebuildLayout();
    }
```

- 其余路径（竖向/换边/收缩态下保存）走现有同步逻辑逐行不动。
- 中继期间用户再 hover 展开：弹簧 target 翻 1，emerged_ 落定后同样应用（chip 排⇄展开都连续，可接受——与原型 380ms 定时重建等价语义）。

- [ ] **Step 1: closeSettings 分流**

:899-907 改为：

```cpp
  if (apply) {
    const bool edgeChanged = settings_.draft.edge != cfg_.edge;
    if (!edgeChanged && isHorizEdge(cfg_.edge) && emergeTarget_ > 0.5) {
      // mini 中继（设计定论）：先 morph 缩回 chip 排，落定后静默重建
      pendingDraft_ = settings_.draft;
      pendingApply_ = true;
    } else {
      cfg_ = settings_.draft;
      cfg_.normalize();
      saveConfig(okmeterDir(), cfg_);
      createModules();
      hoverIdx_ = -1;
      pressIdx_ = -1;
      rebuildItems();
    }
  }
```

:915-919 指针/迟滞逻辑前插：

```cpp
  if (pendingApply_) {
    setEmergeTarget(0);   // 缩回 chip 排（落定应用由 animTick 完成）
  } else if (cfg_.pinned) {
```

- [ ] **Step 2: animTick 应用点**（代码见上规则块，插在补弹代码之前）

- [ ] **Step 3: needsFrames 覆盖中继期**：弹簧未稳本身已驱帧（`!emerged_`），落定当帧应用 → render 在应用路径内显式调一次（rebuildLayout → applyWindowPos → WM_SIZE 或显式 `render();` 收尾，补一行 `render();`）。

- [ ] **Step 4: 编译 + 行为验证**

```bash
cd okmeter && cmd //c build.bat
taskkill //F //IM OkMeter.exe
# config edge=top、pinned=true（保持展开），启动 build/OkMeter.exe 实测：
#   右键 → 设置 → 切形态 → 保存并生效
# 预期：条先 morph 缩回 chip 排 → 落定后瞬间换成新形态（仍 chip 排），全程无淡入淡出
```

（人工目检；无法 headless 断言则在本步记录"已目检"。）

- [ ] **Step 5: Commit**

```bash
git add okmeter/ui/app.h okmeter/ui/app.cpp
git commit -m "okmeter: 形态切换 mini 中继——展开态先缩 chip 排再静默重建（零淡入淡出）"
```

---

### Task 8: glow 材质横向 + wave 流向横向

**Files:**
- Modify: `okmeter/render/materials/glow.cpp`（drawArcStroke :68-206、updateMotes/motePose/burstAt :390-465）
- Modify: `okmeter/render/forms/wave.cpp:106`（横向 flip 规则）

**Interfaces:**
- Consumes: geometry.h isHorizEdge（经 form.h 可用）。

规则：
- glow 环境光源换缘：edge=top → 光源锚在项群**上缘之外** 170×scale px（屏顶之外）；edge=bottom → 下缘之外。气态光两椭圆的 rx/ry 互换（340/480 → 480/340，300/430 → 430/300），21s 正弦漂移的 x/y 分量互换。`side` 概念扩展：横向用 `vside = edge=="top" ? -1 : +1`，edgeY = 项群最外缘（top：最小 y − extent；bottom：最大 y + extent），gasY = edgeY + vside·170。
- 粒子迸散偏向：竖向 `bx = -side·…`（朝屏内）；横向改 `by = -vside·…`（bx 不置）。updateMotes/motePose 里凡 `side` 驱动水平分量的地方按 horiz 分流到垂直分量。实现者通读 glow.cpp:390-465 后逐点改造，竖向路径逐行保留。
- 跟指针柔光/亮边（ptrLight/drawOrbBack）与方向无关，不动。
- wave 流向：横向 `flip = false`（最新点恒在右，左→右流；横条无"屏内侧"水平语义）。`:106` 改 `const bool flip = !isHorizEdge(ctx.edge) && ctx.edge == "right";`。

- [ ] **Step 1: glow drawArcStroke 横向分支**（规则如上；竖向逐行保留）

- [ ] **Step 2: 粒子系统横向分量**

- [ ] **Step 3: wave flip 横向**

- [ ] **Step 4: 编译 + shot 验证**

```bash
cd okmeter && cmd //c build.bat
taskkill //F //IM OkMeter.exe
# config edge=top、material=glow、form=wave
okmeter/build/OkMeter.exe --shot /tmp/h-glow.png
# config edge=bottom 再一张 /tmp/h-glow-b.png
# 恢复 edge=right material=dark，重启运行实例
```

Expected：上缘光源在屏顶外（亮心不露进条内）、下缘在任务栏外；wave sparkline 最新点在右。ReadMediaFile 核对。

- [ ] **Step 5: Commit**

```bash
git add okmeter/render/materials/glow.cpp okmeter/render/forms/wave.cpp
git commit -m "okmeter: glow 环境光源换缘（上/下）+ wave 横向流向恒左→右"
```

---

### Task 9: 全量回归 + 文档收尾

**Files:**
- Modify: `C:/Users/Administrator/.okryptos/projects/OpenKnowledge/knowledge/OkMeter-token-悬浮条.md`（经 `ok add --force`，手法见 Step 3）
- Modify: `docs/2026-09-13-dock-horizontal-design.md`（文末补"已实现"注记：版本/提交号）

- [ ] **Step 1: 全量回归矩阵（shot 自检，逐一 ReadMediaFile 核对）**

竖向（edge=left 与 right 各一组，确认逐像素无回归）+ 横向（edge=top 与 bottom）：

```bash
cd okmeter && cmd //c build.bat
taskkill //F //IM OkMeter.exe
# 六形态 × {--shot, --shotcap}（横向 top）；arc/wave/compass 加 bottom 一组
# 竖向 right：六形态 × {--shot, --shotcap}；left：arc 一组
# 菜单 --shotmenu 1、设置 --shotsettings 各一张（top 与 right）
```

核对清单：竖向收缩露半条+降暗、展开卡区/详情卡、级联菜单方向与锚定（主列不跳）、wave 流向镜像；横向 mini chip 排、展开居中簇状条、arc 朝屏心鼓、compass 径向、卡方向、菜单朝屏心。全部通过后恢复 `edge=right form=arc material=dark`，重启运行实例。

- [ ] **Step 2: 部署到运行实例**

```bash
taskkill //F //IM OkMeter.exe; sleep 1
cp okmeter/build/OkMeter.exe /d/software/OpenKnowledge/OkMeter.exe
cp okmeter/build/OkMeter.exe dist/OkMeter.exe   # 保持 dist 暂存同步（防下次打包装回旧版）
(cd /d/software/OpenKnowledge && ./OkMeter.exe &)
```

- [ ] **Step 3: wiki 更新**（`OkMeter-token-悬浮条` 条目补"横向 dock（四边吸附）"段：mini chip 排/morph/mini 中继/方向适配要点；`演进历程`最近版本段子条目补一行里程碑——均用 `ok add --force` 覆盖写，正文先落临时 md）

- [ ] **Step 4: 设计文档补已实现注记 + Commit**

```bash
git add docs/2026-09-13-dock-horizontal-design.md
git commit -m "docs: 横向 dock 设计定论补 C++ 实现落点注记"
```

---

## Self-Review 记录

- **Spec 覆盖**：居中簇状条(T3)、mini chip(T4)、逐项 morph 双向(T4)、形变禁卡+落定补弹(T5)、卡方向(T5)、mini 中继+禁淡入淡出(T7)、glow 换缘(T8)、arc 朝屏心鼓(T2)、compass 径向(T2)、级联菜单/设置面板/拖拽/换边适配(T3/T6)、竖向不变(T2/T3 分支隔离 + T9 回归矩阵)。设计文档每条有对应 Task。
- **类型一致**：layoutMini(widths, scale)、morphGeom(stage, mini, e, anchorBottom)、IForm::layout(n, screenW, screenH, edge)、drawCard(..., winW, winH, ...)、measureChipWidths(d3d, items)、drawChipText(ctx, dc, i, cx, cy, alpha)、barGeom(e)/barBox(e,w,h)——各 Task 引用与本契约一致。
- **已知留白（有意）**：compass 横向展开态是否旋转沿用竖向语义（tick 不断）；shot 验证的 config 改写用 python 就地编辑真实 config.json 并恢复（既往自检同款手法）。
