# OkMeter 渲染层（Plan 2）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Plan 1 的核心框架之上交付**可见可用的 OkMeter dock**：Win32+D3D11+D2D+DirectComposition 透明渲染管线、球体弧线形态、暗夜材质、贴边两态（收缩/展开）弹簧动画、真实数据接线、悬停放大与邻球让位、悬停详情卡、目录变更监听。

**Architecture:** 单线程 UI 模型（遵守 Plan 1 终审钉死的线程契约：采集 poll 与渲染读都在 UI 线程由 timer 驱动）。纯逻辑层（fmt/spring/config/binding/geometry）全部 TDD 单测；渲染层（d3d/app/dock_scene/watch）以手动验收清单为准。高级材质（frost/liquid/glow + WGC 背景捕获）归 Plan 2b，其余形态（胶囊/星环）归 Plan 2b，设置背板面板归 Plan 3，托盘/打包归 Plan 4。

**Tech Stack:** MSVC + Windows SDK（d3d11/d2d1/dwrite/dcomp/dxgi/wrl，全为系统自带，非第三方），C++20。

**Spec 源:** `docs/2026-09-08-token-dock-design.md` §3.2（窗口行为）、§3.3（球体与映射）、§3.4（悬停交互）、§3.7（采集监听）；视觉参照 `docs/prototypes/okmeter-dock-prototype.html` 的 arc 形态与 dark 材质、`prototype-token-dock-v8.html` 的两态位移。

## Global Constraints

- 平台仅 Windows；零第三方依赖（WRL/STL/Win32/D3D11/D2D1/DWrite/DComp 均为系统自带）。
- **线程契约（Plan 1 终审钉死，逐字遵守）**：Aggregator/Store 非线程安全；采集 poll 与渲染读都在 UI 线程由 timer 驱动串行进行。
- 窗口：透明、无边框、始终置顶、不在任务栏出现、不进 Alt-Tab（WS_EX_TOOLWINDOW）、不抢焦点（WS_EX_NOACTIVATE）；**不用** SHAppBarMessage。
- 贴边两态：收缩态整体露出约 24px；展开态完整滑出；鼠标离开窗口区域约 600ms 后收回；动画用弹簧积分器，调校到 150~200ms 等效时长，禁止闪烁抖动。
- 球数仅奇数 1/3/5/7；默认映射 = 按最近使用模型排序（最近占中心，次近交替向两侧）。
- 球上数字紧凑格式（`9.9K`/`128.6K`/`2.1M`），精确值留给悬停详情。
- 数据采集：ReadDirectoryChangesW 监听 sessions 目录，2s 轮询兜底；只读 `~/.kimi-code`。
- 源码 UTF-8；构建命令固定 `okmeter\build.bat`（编译+全部单测+OkMeter.exe，任一失败非零退出）；commit `feat(okmeter): 中文描述`。
- 渲染任务（6~9）的代码为**骨架 + 关键 API 序列**，实现者可补全 Win32 样板，验收以手动清单为准；纯逻辑任务（1~5）代码完整、必须逐字。

## 文件结构

```
okmeter/
  core/
    fmt.h/.cpp            # Task 1：紧凑/千分位/相对时间
    spring.h/.cpp         # Task 2：弹簧积分器
    atomic.h/.cpp         # Task 3：原子写抽取（store.cpp 重构复用）
    config.h/.cpp         # Task 3：配置模型 + config.json 读写
    binding.h/.cpp        # Task 4：映射解析 + recency 槽位交替
  ui/
    geometry.h/.cpp       # Task 5：弧线布局 + 两态位移 + 悬停放大/让位
    app.h/.cpp            # Task 6/7/9：窗口、消息循环、timer 调度、右键菜单（换边/退出）
    watch.h/.cpp          # Task 9：ReadDirectoryChangesW 目录监听
  render/
    d3d.h/.cpp            # Task 6：D3D11+D2D+DWrite+DComp 设备/交换链/设备丢失重建
    dock_scene.h/.cpp     # Task 7/8：场景绘制（球/弧线/文本/两态/详情卡）
  app/
    main.cpp              # 改：--scan 保留旧冒烟；默认启动 DockApp
  tests/                  # 每任务一个 test_*.cpp
```

接口约定（任务间依赖的精确签名）：

- `okmeter::fmtCompact(int64_t)→std::string`、`fmtExact(int64_t)→std::string`、`relTime(int64_t thenMs,int64_t nowMs)→std::string`
- `okmeter::Spring`：`value/velocity/stiffness/damping`、`step(double dt,double target)`、`settled(double target,double eps=1e-3) const`、`snap(double v)`
- `okmeter::atomicWriteText(const std::filesystem::path&, const std::string&)→bool`
- `okmeter::Config`：`form/material/count/edge/mergeCache/mapping` + `normalize()`；`loadConfig(dir,&cfg)→bool`、`saveConfig(dir,cfg)→bool`
- `okmeter::Binding`：`bool isModel; Scope scope; std::string modelId;`；`enum class Scope{Session,Today,Week,All}`；`resolveBindings(const Config&,const Aggregator&)→std::vector<Binding>`
- `okmeter::ItemGeom{x,y,r,scale,dy}`、`DockGeom{w,h,items}`；`layoutArc(int n,int radius,int gap,int screenH,const std::string& edge)→DockGeom`；`applyHover(DockGeom&,int hoverIdx,double hoverScale,double push)`
- 渲染层：`okmeter::render::D3DContext{init(HWND,w,h),resize,begin,end,dc(),ok()}`；`okmeter::DockApp{run(HINSTANCE)}→int`

---

### Task 1: 数字与时间格式化（core/fmt）

**Files:**
- Create: `okmeter/core/fmt.h`、`okmeter/core/fmt.cpp`
- Test: `okmeter/tests/test_fmt.cpp`

**Interfaces:**
- Consumes: tests/framework.h
- Produces: `fmtCompact/fmtExact/relTime`——Task 7/8 渲染文本用。

- [ ] **Step 1: 写失败测试**

`okmeter/tests/test_fmt.cpp`：

```cpp
#include "../core/fmt.h"
#include "framework.h"

using namespace okmeter;

TEST(fmt_compact_thresholds) {
  CHECK(fmtCompact(0) == "0");
  CHECK(fmtCompact(999) == "999");
  CHECK(fmtCompact(9900) == "9.9K");
  CHECK(fmtCompact(128590) == "128.6K");
  CHECK(fmtCompact(2134756) == "2.1M");
  CHECK(fmtCompact(1000000) == "1.0M");
}

TEST(fmt_exact_grouping) {
  CHECK(fmtExact(0) == "0");
  CHECK(fmtExact(141) == "141");
  CHECK(fmtExact(86412) == "86,412");
  CHECK(fmtExact(17029868) == "17,029,868");
}

TEST(fmt_rel_time) {
  const int64_t now = 1000000000000LL;
  CHECK(relTime(now - 30000, now) == "刚刚");                    // 30s 前
  CHECK(relTime(now - 3 * 60000LL, now) == "3 分钟前");
  CHECK(relTime(now - 47 * 60000LL, now) == "47 分钟前");
  CHECK(relTime(now - 380 * 60000LL, now) == "6 小时前");
  CHECK(relTime(now - 4300 * 60000LL, now) == "2 天前");
  CHECK(relTime(now + 60000, now) == "刚刚");                    // 未来/乱序钳制
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd /d/develop/OpenKnowledge/okmeter && cmd //c build.bat`
Expected: 编译错误 `cannot open include file: '../core/fmt.h'`（红）。

- [ ] **Step 3: 实现 fmt**

`okmeter/core/fmt.h`：

```cpp
// core/fmt.h —— 数字紧凑/精确格式与相对时间（显示层口径，原型同款）
#pragma once
#include <cstdint>
#include <string>

namespace okmeter {

std::string fmtCompact(int64_t v);  // 9.9K / 128.6K / 2.1M；<1000 原样
std::string fmtExact(int64_t v);    // 千分位：17,029,868
std::string relTime(int64_t thenMs, int64_t nowMs);  // 刚刚 / N 分钟前 / N 小时前 / N 天前

} // namespace okmeter
```

`okmeter/core/fmt.cpp`：

```cpp
#include "fmt.h"
#include <cstdio>

namespace okmeter {

std::string fmtCompact(int64_t v) {
  char buf[24];
  if (v >= 1000000) { std::snprintf(buf, sizeof buf, "%.1fM", v / 1e6); return buf; }
  if (v >= 1000)    { std::snprintf(buf, sizeof buf, "%.1fK", v / 1e3); return buf; }
  std::snprintf(buf, sizeof buf, "%lld", (long long)v);
  return buf;
}

std::string fmtExact(int64_t v) {
  if (v < 0) v = 0;
  char digits[24];
  std::snprintf(digits, sizeof digits, "%lld", (long long)v);
  std::string out;
  int len = (int)std::strlen(digits);
  for (int i = 0; i < len; ++i) {
    if (i > 0 && (len - i) % 3 == 0) out += ',';
    out += digits[i];
  }
  return out;
}

std::string relTime(int64_t thenMs, int64_t nowMs) {
  int64_t s = (nowMs - thenMs) / 1000;
  if (s < 0) s = 0;
  if (s < 45) return "刚刚";
  const int64_t m = s / 60;
  if (m < 60) return std::to_string(m) + " 分钟前";
  const int64_t h = m / 60;
  if (h < 24) return std::to_string(h) + " 小时前";
  return std::to_string(h / 24) + " 天前";
}

} // namespace okmeter
```

注意：`fmt.cpp` 用了 `std::strlen`，include 区加 `<cstring>`。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd /d/develop/OpenKnowledge/okmeter && cmd //c build.bat`
Expected: 3 个 fmt_* + 既有 21 = `24 cases, 0 failures`，零警告。

- [ ] **Step 5: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 数字紧凑/精确格式与相对时间"
```

---

### Task 2: 弹簧积分器（core/spring）

**Files:**
- Create: `okmeter/core/spring.h`、`okmeter/core/spring.cpp`
- Test: `okmeter/tests/test_spring.cpp`

**Interfaces:**
- Consumes: tests/framework.h
- Produces: `Spring`——Task 5/7 动画驱动。

- [ ] **Step 1: 写失败测试**

`okmeter/tests/test_spring.cpp`：

```cpp
#include "../core/spring.h"
#include "framework.h"
#include <cmath>

using namespace okmeter;

TEST(spring_converges_to_target) {
  Spring s;                       // 默认刚度/阻尼：150~200ms 等效时长
  for (int i = 0; i < 240; ++i)   // 2s @ 1/120
    s.step(1.0 / 120, 1.0);
  CHECK(s.settled(1.0));
  CHECK(std::abs(s.value - 1.0) < 0.01);
}

TEST(spring_zero_dt_noop) {
  Spring s;
  s.step(0, 1.0);
  CHECK(s.value == 0 && s.velocity == 0);
}

TEST(spring_huge_dt_does_not_blow_up) {
  Spring s;
  s.step(100.0, 1.0);             // timer 卡顿：内部钳 dt
  CHECK(std::isfinite(s.value) && std::isfinite(s.velocity));
  for (int i = 0; i < 240; ++i)
    s.step(1.0 / 120, 1.0);
  CHECK(s.settled(1.0));
}

TEST(spring_snap) {
  Spring s;
  s.snap(0.5);
  CHECK(s.value == 0.5 && s.velocity == 0);
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd /d/develop/OpenKnowledge/okmeter && cmd //c build.bat`
Expected: 编译错误 `cannot open include file: '../core/spring.h'`（红）。

- [ ] **Step 3: 实现 spring**

`okmeter/core/spring.h`：

```cpp
// core/spring.h —— 弹簧积分器（stiffness/damping 模型，非贝塞尔近似）
#pragma once

namespace okmeter {

struct Spring {
  double value = 0;
  double velocity = 0;
  double stiffness = 400;   // ωn=20 → 2% 整定约 0.23s，贴合 150~200ms 规格
  double damping = 40;      // 临界阻尼 = 2√stiffness

  void step(double dt, double target);   // dt 秒；内部钳上限防卡顿爆炸
  bool settled(double target, double eps = 1e-3) const;
  void snap(double v);
};

} // namespace okmeter
```

`okmeter/core/spring.cpp`：

```cpp
#include "spring.h"
#include <cmath>

namespace okmeter {

void Spring::step(double dt, double target) {
  if (dt <= 0) return;
  if (dt > 0.05) dt = 0.05;  // timer 卡顿防护：大步长切片由调用方继续推进
  const double force = -stiffness * (value - target) - damping * velocity;
  velocity += force * dt;
  value += velocity * dt;
}

bool Spring::settled(double target, double eps) const {
  return std::abs(value - target) < eps && std::abs(velocity) < eps;
}

void Spring::snap(double v) {
  value = v;
  velocity = 0;
}

} // namespace okmeter
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd /d/develop/OpenKnowledge/okmeter && cmd //c build.bat`
Expected: 4 个 spring_* + 既有 24 = `28 cases, 0 failures`，零警告。

- [ ] **Step 5: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 弹簧积分器（stiffness/damping 模型）"
```

---

### Task 3: 原子写抽取 + 配置模型（core/atomic + core/config）

**Files:**
- Create: `okmeter/core/atomic.h`、`okmeter/core/atomic.cpp`
- Modify: `okmeter/core/store.cpp`（flush 换用 atomicWriteText）
- Create: `okmeter/core/config.h`、`okmeter/core/config.cpp`
- Test: `okmeter/tests/test_config.cpp`

**Interfaces:**
- Consumes: minjson（Plan 1）、store.cpp 既有 flush 逻辑
- Produces: `atomicWriteText`；`Config{form,material,count,edge,mergeCache,mapping}` + `normalize()` + `loadConfig/saveConfig`——Task 4/6/9 依赖。

- [ ] **Step 1: 写失败测试**

`okmeter/tests/test_config.cpp`：

```cpp
#include "../core/config.h"
#include "framework.h"
#include <fstream>
#include <process.h>

using namespace okmeter;

static std::filesystem::path tempDir(const char* name) {
  auto d = std::filesystem::temp_directory_path() /
           ("okmeter-test-" + std::string(name) + "-" + std::to_string(_getpid()));
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
  std::filesystem::create_directories(d);
  return d;
}

TEST(config_defaults_and_normalize) {
  Config c;
  CHECK(c.form == "arc");
  CHECK(c.material == "dark");
  CHECK(c.count == 3);
  CHECK(c.edge == "right");
  CHECK(c.mergeCache);
  c.count = 4;              // 偶数：钳回奇数
  c.edge = "top";           // v1 不支持：回退
  c.form = "unknown";
  c.normalize();
  CHECK(c.count == 3);
  CHECK(c.edge == "right");
  CHECK(c.form == "arc");
  c.count = 7;
  c.mapping = {"auto", "total:today"};
  c.normalize();
  CHECK(c.count == 7);
  CHECK_EQ(c.mapping.size(), (size_t)7);        // 长度对齐 count，填 auto
  CHECK(c.mapping[1] == "total:today");
  CHECK(c.mapping[6] == "auto");
}

TEST(config_roundtrip) {
  auto d = tempDir("config");
  Config c;
  c.count = 5;
  c.material = "liquid";
  c.edge = "left";
  c.mergeCache = false;
  c.mapping = {"auto", "model:kimi-code/k3", "total:all", "auto", "auto"};
  CHECK(saveConfig(d, c));
  Config back;
  CHECK(loadConfig(d, back));
  CHECK(back.count == 5);
  CHECK(back.material == "liquid");
  CHECK(back.edge == "left");
  CHECK(!back.mergeCache);
  CHECK_EQ(back.mapping.size(), (size_t)5);
  CHECK(back.mapping[1] == "model:kimi-code/k3");
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
}

TEST(config_missing_or_broken_returns_defaults) {
  auto d = tempDir("config-broken");
  Config c;
  c.count = 7;
  CHECK(!loadConfig(d, c));   // 文件缺失 → false 且回默认
  CHECK(c.count == 3);
  {
    std::ofstream f(d / "config.json", std::ios::binary);
    f << "{broken";
  }
  Config c2;
  c2.count = 7;
  CHECK(!loadConfig(d, c2));
  CHECK(c2.count == 3);
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd /d/develop/OpenKnowledge/okmeter && cmd //c build.bat`
Expected: 编译错误 `cannot open include file: '../core/config.h'`（红）。

- [ ] **Step 3: 实现 atomic + config，重构 store**

`okmeter/core/atomic.h`：

```cpp
// core/atomic.h —— 原子文本写：tmp + MoveFileExW(REPLACE_EXISTING)
#pragma once
#include <filesystem>
#include <string>

namespace okmeter {

bool atomicWriteText(const std::filesystem::path& file, const std::string& content);

} // namespace okmeter
```

`okmeter/core/atomic.cpp`：

```cpp
#include "atomic.h"
#include <fstream>

#define WIN32_LEAN_AND_MEAN
#include <windows.h>

namespace okmeter {

bool atomicWriteText(const std::filesystem::path& file, const std::string& content) {
  std::error_code ec;
  std::filesystem::create_directories(file.parent_path(), ec);
  auto tmp = file;
  tmp += L".tmp";
  {
    std::ofstream out(tmp, std::ios::binary | std::ios::trunc);
    if (!out) return false;
    out << content;
    out.flush();
    if (!out) return false;
  }
  if (!MoveFileExW(tmp.c_str(), file.c_str(),
                   MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)) {
    std::filesystem::remove(tmp, ec);
    return false;
  }
  return true;
}

} // namespace okmeter
```

`store.cpp` 的 `flush()` 改造：删除 tmp/MoveFileExW 手写段，改为

```cpp
bool Store::flush() {
  // ...组 json（不变）...
  return atomicWriteText(dir_ / "state.json", json::dump(rv));
}
```

`okmeter/core/config.h`：

```cpp
// core/config.h —— 配置模型与 config.json 持久化
#pragma once
#include <filesystem>
#include <string>
#include <vector>

namespace okmeter {

struct Config {
  std::string form = "arc";        // arc（v1 渲染）；capsule/compass 属 Plan 2b
  std::string material = "dark";   // dark（v1 渲染）；frost/liquid/glow 属 Plan 2b
  int count = 3;                   // 仅奇数 1/3/5/7
  std::string edge = "right";      // right / left（v1 不做顶底）
  bool mergeCache = true;
  std::vector<std::string> mapping;  // "auto" | "total:session|today|week|all" | "model:<id>"

  void normalize();  // 非法值回退默认；count 钳奇数集；mapping 长度对齐 count
};

bool loadConfig(const std::filesystem::path& dir, Config& out);  // 缺/坏 → false 且 out=默认
bool saveConfig(const std::filesystem::path& dir, const Config& cfg);

} // namespace okmeter
```

`okmeter/core/config.cpp`：

```cpp
#include "config.h"
#include "atomic.h"
#include "minjson.h"
#include <fstream>

namespace okmeter {
namespace {

bool isValidCount(int n) { return n == 1 || n == 3 || n == 5 || n == 7; }

} // namespace

void Config::normalize() {
  if (form != "arc" && form != "capsule" && form != "compass" &&
      form != "level" && form != "wave" && form != "nixie")
    form = "arc";
  if (material != "dark" && material != "frost" &&
      material != "liquid" && material != "glow")
    material = "dark";
  if (!isValidCount(count)) count = 3;
  if (edge != "right" && edge != "left") edge = "right";
  mapping.resize(count, "auto");
  for (auto& m : mapping) if (m.empty()) m = "auto";
}

bool loadConfig(const std::filesystem::path& dir, Config& out) {
  out = Config{};
  std::ifstream in(dir / "config.json", std::ios::binary);
  if (!in) return false;
  std::string text((std::istreambuf_iterator<char>(in)), std::istreambuf_iterator<char>());
  json::Value v;
  if (!json::parse(text, v) || !v.isObject()) return false;
  if (const json::Value* x = v.find("form")) out.form = x->str();
  if (const json::Value* x = v.find("material")) out.material = x->str();
  if (const json::Value* x = v.find("count")) out.count = (int)x->num(3);
  if (const json::Value* x = v.find("edge")) out.edge = x->str();
  if (const json::Value* x = v.find("mergeCache")) out.mergeCache = x->num(1) != 0;
  if (const json::Value* m = v.find("mapping"); m && m->isObject()) {
    out.mapping.clear();
    for (const auto& [k, val] : m->obj()) out.mapping.push_back(val.str());
  }
  out.normalize();
  return true;
}

bool saveConfig(const std::filesystem::path& dir, const Config& cfg) {
  Config c = cfg;
  c.normalize();
  json::Object root;
  root["form"] = json::str(c.form);
  root["material"] = json::str(c.material);
  root["count"] = json::num(c.count);
  root["edge"] = json::str(c.edge);
  root["mergeCache"] = json::num(c.mergeCache ? 1 : 0);
  json::Array arr;
  for (const auto& m : c.mapping) arr.push_back(json::str(m));
  json::Value av;
  av.v = std::move(arr);
  root["mapping"] = std::move(av);
  json::Value rv;
  rv.v = std::move(root);
  return atomicWriteText(dir / "config.json", json::dump(rv));
}

} // namespace okmeter
```

注意两处实现修正（必须照做）：
1. `loadConfig` 的 mapping 在 JSON 里存**数组**不是对象：minjson 的 Value 缺 `isArray()`/`arr()` 访问器——实现时给 `minjson.h` 的 Value 补两个访问器（`bool isArray() const` 与 `const Array& arr() const`，形态仿照 `isObject()`/`obj()`），loadConfig 改为：
   ```cpp
   if (const json::Value* m = v.find("mapping"); m && m->isArray()) {
     out.mapping.clear();
     for (const auto& val : m->arr()) out.mapping.push_back(val.str());
   }
   ```
2. `mergeCache` 用布尔存储更自然：minjson 的 Value 无 `boolean(dflt)` 访问器——存取统一走 `num()`（0/1）即可，如上面代码所示，不必加访问器。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd /d/develop/OpenKnowledge/okmeter && cmd //c build.bat`
Expected: 3 个 config_* + 既有 28 = `31 cases, 0 failures`，零警告；store 既有 4 用例保持绿（原子写重构无回归）。

- [ ] **Step 5: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 原子写抽取与配置模型持久化"
```

---

### Task 4: 映射解析（core/binding）

**Files:**
- Create: `okmeter/core/binding.h`、`okmeter/core/binding.cpp`
- Test: `okmeter/tests/test_binding.cpp`

**Interfaces:**
- Consumes: `Config`（Task 3）、`Aggregator`（Plan 1）
- Produces: `Scope` 枚举、`Binding`、`resolveBindings`——Task 7 渲染数据接线依赖。规格 §3.3：默认映射 = 全部项按最近使用排序的模型累计，最近占中心，次近交替向两侧；每个项可单独配置总量模式/模型模式。

- [ ] **Step 1: 写失败测试**

`okmeter/tests/test_binding.cpp`：

```cpp
#include "../core/binding.h"
#include "framework.h"
#include <ctime>

using namespace okmeter;

static UsageEvent ev(const char* model, int64_t tokens, int64_t t) {
  UsageEvent e;
  e.model = model; e.sessionId = "s1";
  e.inputOther = tokens; e.timeMs = t;
  return e;
}

static Aggregator threeModels() {
  Aggregator a;
  a.add(ev("m/old", 10, 1000));
  a.add(ev("m/mid", 20, 2000));
  a.add(ev("m/new", 30, 3000));   // 最近
  return a;
}

TEST(binding_auto_recency_alternation) {
  Config c;
  c.count = 5;
  c.normalize();
  auto agg = threeModels();
  auto b = resolveBindings(c, agg);
  CHECK_EQ(b.size(), (size_t)5);
  CHECK(b[2].isModel && b[2].modelId == "m/new");  // 中心 = 最近
  CHECK(b[1].isModel && b[1].modelId == "m/mid");  // 次近 → 中心上/左
  CHECK(b[3].isModel && b[3].modelId == "m/old");  // 再次 → 中心下/右
  CHECK(!b[0].isModel && b[0].scope == Scope::All);  // 模型不够 → 全部累计兜底
  CHECK(!b[4].isModel && b[4].scope == Scope::All);
}

TEST(binding_explicit_total_and_model) {
  Config c;
  c.count = 3;
  c.mapping = {"total:today", "model:m/keep", "auto"};
  c.normalize();
  auto agg = threeModels();
  auto b = resolveBindings(c, agg);
  CHECK(!b[0].isModel && b[0].scope == Scope::Today);
  CHECK(b[1].isModel && b[1].modelId == "m/keep");   // 显式模型保留（即使不在数据里）
  CHECK(b[2].isModel && b[2].modelId == "m/old");    // auto 位按槽位固定 rank（slot2←rank2）
}

TEST(binding_invalid_mapping_falls_back_auto) {
  Config c;
  c.count = 3;
  c.mapping = {"garbage", "total:wrong", "auto"};
  c.normalize();
  auto agg = threeModels();
  auto b = resolveBindings(c, agg);
  CHECK(b[0].isModel && b[0].modelId == "m/mid");  // garbage → auto；slot0←rank1
  CHECK(b[1].isModel && b[1].modelId == "m/new");  // total:wrong → auto；slot1←rank0（最近占中心）
}

TEST(binding_empty_data_all_total) {
  Config c;
  c.count = 3;
  c.normalize();
  Aggregator empty;
  auto b = resolveBindings(c, empty);
  for (const auto& x : b) { CHECK(!x.isModel); CHECK(x.scope == Scope::All); }
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd /d/develop/OpenKnowledge/okmeter && cmd //c build.bat`
Expected: 编译错误 `cannot open include file: '../core/binding.h'`（红）。

- [ ] **Step 3: 实现 binding**

`okmeter/core/binding.h`：

```cpp
// core/binding.h —— 指标映射解析：位置 → 总量模式/模型模式
#pragma once
#include "aggregator.h"
#include "config.h"
#include <string>
#include <vector>

namespace okmeter {

enum class Scope { Session, Today, Week, All };

struct Binding {
  bool isModel = false;       // false=总量模式，true=模型模式
  Scope scope = Scope::All;   // 总量模式有效
  std::string modelId;        // 模型模式有效
};

std::vector<Binding> resolveBindings(const Config& cfg, const Aggregator& agg);

} // namespace okmeter
```

`okmeter/core/binding.cpp`：

```cpp
#include "binding.h"

namespace okmeter {
namespace {

// r 名的槽位：最近(r=0)占中心，次近交替向两侧（原型 slotForRank 同款）
int slotForRank(int r, int n) {
  const int mid = (n - 1) / 2;
  if (r == 0) return mid;
  return r % 2 == 1 ? mid - (r + 1) / 2 : mid + r / 2;
}

bool parseScope(const std::string& s, Scope& out) {
  if (s == "session") { out = Scope::Session; return true; }
  if (s == "today")   { out = Scope::Today;   return true; }
  if (s == "week")    { out = Scope::Week;    return true; }
  if (s == "all")     { out = Scope::All;     return true; }
  return false;
}

Binding totalAll() { return Binding{}; }

} // namespace

std::vector<Binding> resolveBindings(const Config& cfg, const Aggregator& agg) {
  Config c = cfg;
  const_cast<Config&>(c).normalize();
  const int n = c.count;
  const auto ranked = agg.modelsByRecency();
  std::vector<Binding> out((size_t)n);
  // auto 槽位按 rank 消费 recency 列表（显式槽位不占名额，与原型一致：
  // auto 位独立按 rank 顺延）
  int nextRank = 0;
  // 先标 auto 槽位的 rank 顺序：slotForRank 定义 rank→槽位，反转得槽位→rank
  std::vector<int> rankOf((size_t)n, -1);
  for (int r = 0; r < n; ++r) rankOf[(size_t)slotForRank(r, n)] = r;
  for (int i = 0; i < n; ++i) {
    const std::string& m = c.mapping[(size_t)i];
    if (m.rfind("total:", 0) == 0) {
      Scope s;
      if (parseScope(m.substr(6), s)) { out[(size_t)i] = Binding{false, s, ""}; continue; }
    }
    if (m.rfind("model:", 0) == 0 && m.size() > 6) {
      out[(size_t)i] = Binding{true, Scope::All, m.substr(6)};
      continue;
    }
    // auto（含非法值回退）：按槽位 rank 取 recency；rank 耗尽 → 全部累计
    const int r = rankOf[(size_t)i];
    int pick = nextRank <= r ? r : nextRank;   // 显式槽不占 rank，直接用槽位 rank
    (void)pick;
    if (r >= 0 && r < (int)ranked.size())
      out[(size_t)i] = Binding{true, Scope::All, ranked[(size_t)r]};
    else
      out[(size_t)i] = totalAll();
  }
  return out;
}

} // namespace okmeter
```

注意实现修正（必须照做）：上面 `nextRank`/`pick` 两行是废笔——auto 语义就是"槽位 rank 直接索引 recency"（原型 bySlot 同款），删除这两行，只留 `rankOf` 反转表分支。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd /d/develop/OpenKnowledge/okmeter && cmd //c build.bat`
Expected: 4 个 binding_* + 既有 31 = `35 cases, 0 failures`，零警告。

- [ ] **Step 5: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 指标映射解析与 recency 槽位交替"
```

---

### Task 5: 布局引擎（ui/geometry）

**Files:**
- Create: `okmeter/ui/geometry.h`、`okmeter/ui/geometry.cpp`
- Test: `okmeter/tests/test_geometry.cpp`

**Interfaces:**
- Consumes: tests/framework.h
- Produces: `ItemGeom/DockGeom/layoutArc/applyHover`——Task 7 渲染依赖。规格 §3.2 两态与 §3.4 悬停强化（放大 + 邻球让位）的几何部分。

- [ ] **Step 1: 写失败测试**

`okmeter/tests/test_geometry.cpp`：

```cpp
#include "../ui/geometry.h"
#include "framework.h"
#include <cmath>

using namespace okmeter;

TEST(geom_arc_right_edge_symmetry) {
  auto g = layoutArc(3, 30, 14, 900, "right");
  CHECK_EQ(g.items.size(), (size_t)3);
  CHECK(g.w > 0 && g.h > 0);
  CHECK(g.items[0].y < g.items[1].y && g.items[1].y < g.items[2].y);
  CHECK(std::abs(g.items[0].y + g.items[2].y - 2 * g.items[1].y) < 1e-9); // 中心对称
  CHECK(g.items[1].x < g.items[0].x);   // 中心球最靠屏内（弧线弓形）
}

TEST(geom_arc_left_edge_mirror) {
  auto r = layoutArc(3, 30, 14, 900, "right");
  auto l = layoutArc(3, 30, 14, 900, "left");
  for (size_t i = 0; i < 3; ++i) {
    CHECK(std::abs(r.items[i].x + l.items[i].x - r.w) < 1e-9);  // 镜像
    CHECK(r.items[i].y == l.items[i].y);
  }
}

TEST(geom_single_item) {
  auto g = layoutArc(1, 30, 14, 900, "right");
  CHECK_EQ(g.items.size(), (size_t)1);
  CHECK(std::isfinite(g.items[0].x) && std::isfinite(g.items[0].y));
}

TEST(geom_hover_enlarge_and_squeeze) {
  auto g = layoutArc(5, 30, 14, 900, "right");
  applyHover(g, 2, 1.34, 10.0);
  CHECK(std::abs(g.items[2].scale - 1.34) < 1e-9);
  CHECK(g.items[1].dy < 0);   // 中心上方项被向上挤
  CHECK(g.items[3].dy > 0);   // 中心下方项被向下挤
  CHECK(std::abs(g.items[1].dy) > std::abs(g.items[0].dy));  // 近者挤得多
  applyHover(g, -1, 1.34, 10.0);   // 取消悬停
  CHECK(std::abs(g.items[2].scale - 1.0) < 1e-9);
  CHECK(g.items[1].dy == 0 && g.items[3].dy == 0);
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd /d/develop/OpenKnowledge/okmeter && cmd //c build.bat`
Expected: 编译错误 `cannot open include file: '../ui/geometry.h'`（红）。

- [ ] **Step 3: 实现 geometry**

`okmeter/ui/geometry.h`：

```cpp
// ui/geometry.h —— 弧线布局与悬停几何（纯函数，渲染无关）
#pragma once
#include <string>
#include <vector>

namespace okmeter {

struct ItemGeom {
  double x = 0, y = 0;     // 项中心（dock 盒内坐标）
  double r = 0;            // 半径
  double scale = 1;        // 悬停放大
  double dy = 0;           // 悬停让位纵向偏移（+ 向下）
};

struct DockGeom {
  double w = 0, h = 0;
  std::vector<ItemGeom> items;
};

// 球体弧线：中心项最靠屏内，两侧弓形回退；edge=left 时 x 镜像
DockGeom layoutArc(int n, int radius, int gap, int screenH, const std::string& edge);

// 悬停强化：hoverIdx 项放大，其余项沿排布方向让位（近多远少，指数衰减）；
// hoverIdx<0 复位全部
void applyHover(DockGeom& g, int hoverIdx, double hoverScale, double push);

} // namespace okmeter
```

`okmeter/ui/geometry.cpp`：

```cpp
#include "geometry.h"
#include <algorithm>
#include <cmath>

namespace okmeter {

DockGeom layoutArc(int n, int radius, int gap, int screenH, const std::string& edge) {
  DockGeom g;
  if (n < 1) return g;
  const int mid = (n - 1) / 2;
  const double step = mid > 0
      ? std::min(92.0, (screenH * 0.8 / 2 - 58) / mid)
      : 0.0;
  g.w = 150;
  g.h = 2 * (mid * step + 58);
  g.items.resize((size_t)n);
  for (int i = 0; i < n; ++i) {
    const double fr = mid == 0 ? 0.0 : (double)(i - mid) / mid;
    double x = 120 - 46 * (1 - fr * fr);
    if (edge == "left") x = g.w - x;
    g.items[(size_t)i].x = x;
    g.items[(size_t)i].y = g.h / 2 + (i - mid) * step;
    g.items[(size_t)i].r = radius;
  }
  (void)gap;  // 弧线步长由屏高决定，gap 预留给直线形态（Plan 2b）
  return g;
}

void applyHover(DockGeom& g, int hoverIdx, double hoverScale, double push) {
  for (size_t i = 0; i < g.items.size(); ++i) {
    ItemGeom& it = g.items[i];
    if (hoverIdx < 0 || (int)i == hoverIdx) {
      it.scale = hoverIdx < 0 ? 1.0 : hoverScale;
      it.dy = 0;
      continue;
    }
    const double d = std::abs((double)i - hoverIdx);
    const double amount = push * std::exp(-0.9 * (d - 1));  // 近多远少
    it.scale = 1.0;
    it.dy = ((int)i < hoverIdx ? -amount : amount);
  }
}

} // namespace okmeter
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd /d/develop/OpenKnowledge/okmeter && cmd //c build.bat`
Expected: 4 个 geom_* + 既有 35 = `39 cases, 0 failures`，零警告。

- [ ] **Step 5: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 弧线布局引擎与悬停让位几何"
```

---

### Task 6: 渲染基建与透明窗口（render/d3d + ui/app 骨架）

**Files:**
- Create: `okmeter/render/d3d.h`、`okmeter/render/d3d.cpp`
- Create: `okmeter/ui/app.h`、`okmeter/ui/app.cpp`
- Modify: `okmeter/app/main.cpp`（`--scan` 保留旧冒烟；默认启动 DockApp）
- Modify: `okmeter/build.bat`（链接系统库）

**Interfaces:**
- Consumes: Win32/D3D11/D2D1/DWrite/DComp 系统 API
- Produces: `render::D3DContext{init,resize,begin,end,dc,ok}`、`DockApp::run(HINSTANCE)→int`——Task 7 依赖。

**本任务代码为骨架 + 关键 API 序列**；实现者可补全样板，但下列 API 序列与参数必须遵守：

窗口：`RegisterClassExW`（类名 `OkMeterDock`）+ `CreateWindowExW(WS_EX_TOPMOST|WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE, WS_POPUP)`；启动时 `SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2)`（失败忽略）。

D3D/D2D/DComp 初始化序列：
1. `D3D11CreateDevice(nullptr, D3D_DRIVER_TYPE_HARDWARE, ..., D3D11_CREATE_DEVICE_BGRA_SUPPORT, feature levels 11_1/11_0)`；硬件失败回落 `D3D_DRIVER_TYPE_WARP`。
2. `D2D1CreateFactory(D2D1_FACTORY_TYPE_SINGLE_THREADED)` → 从 DXGI device `CreateDevice` 得 `ID2D1Device` → `CreateDeviceContext`。
3. `DWriteCreateFactory(DWRITE_FACTORY_TYPE_SHARED)`。
4. `DCompositionCreateDevice(dxgiDevice)` → `CreateTargetForHwnd(hwnd, TRUE)` → `CreateVisual` → target `SetRoot(visual)`。
5. `IDXGIFactory2::CreateSwapChainForComposition`：Format `DXGI_FORMAT_B8G8R8A8_UNORM`、AlphaMode `DXGI_ALPHA_MODE_PREMULTIPLIED`、BufferCount 2、SwapEffect `DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL`；visual `SetContent(swapchain)`。
6. 每帧：从 swapchain buffer 建 `ID2D1Bitmap1`（`D2D1_BITMAP_OPTIONS_TARGET | CANNOT_DRAW`，`D2D1_ALPHA_MODE_PREMULTIPLIED`）→ `SetTarget` → `BeginDraw` → `Clear(transparent)` → 绘制 → `EndDraw` → `Present(1,0)` → dcomp `Commit()`。
7. `DXGI_ERROR_DEVICE_REMOVED/DEVICE_RESET` → 释放全部 COM 对象按序重建（`init` 重入安全）。

`build.bat` 的两条 cl 命令链接行尾追加系统库：`d3d11.lib d2d1.lib dwrite.lib dcomp.lib dxgi.lib windowscodecs.lib`（tests 链接不受影响，因为测试不含 render/ui 源文件——**注意：build.bat 的 tests 编译行不能含 render/ 与 ui/app.cpp、ui/watch.cpp**；改为显式源文件清单或分组变量，把 tests 行限定为 `tests\*.cpp core\*.cpp adapters\kimi\*.cpp`，app 行追加 `ui\*.cpp render\*.cpp`——但 `ui\*.cpp` 含 app/watch（Win32 代码）也只进 app 行。ui/geometry.cpp 有单测，必须同时进 tests 行：在 build.bat 用变量列出：
```bat
set "CORE=core\*.cpp"
set "ADAPT=adapters\kimi\*.cpp"
set "UI_CORE=ui\geometry.cpp"
set "UI_WIN=ui\app.cpp ui\watch.cpp"
set "RENDER=render\*.cpp"
set "SYSLIBS=d3d11.lib d2d1.lib dwrite.lib dcomp.lib dxgi.lib windowscodecs.lib"
```
tests 行：`cl %FLAGS% tests\*.cpp %CORE% %ADAPT% %UI_CORE% /Fo:build\ /Fe:build\okmeter-tests.exe`
app 行：`cl %FLAGS% app\main.cpp %CORE% %ADAPT% %UI_CORE% %UI_WIN% %RENDER% build\version.res %SYSLIBS% /Fo:build\ /Fe:build\OkMeter.exe`
（ui/watch.cpp 本任务先建空壳 `namespace okmeter {}`，Task 9 填实现。）

DockApp 骨架（本任务只要窗口能显示测试图案）：
- `run(HINSTANCE)`：DPI → 注册类 → 计算窗口矩形（右缘、宽 150、高按 layoutArc(3)）→ CreateWindowExW → `D3DContext::init` → 消息循环（`GetMessageW`）→ 退出码。
- WndProc：`WM_DESTROY` → PostQuitMessage；`WM_PAINT`/定时器 → 渲染测试画面：清透明 → 画一个 accent 色实心圆 + 一行 DWrite 文本（验证文本管线）→ Present。
- DWrite 文本：`CreateTextFormat(L"Consolas", ..., 12)`、`ID2D1SolidColorBrush`（白）→ `DrawTextW` 或 `CreateTextLayout`。字体回落：Consolas 一定存在。
- accent 色：`D2D1::ColorF(0x5FE0A8)`（原型 accent 绿）。

`app/main.cpp` 改造：

```cpp
// 用法：OkMeter.exe          → 启动 dock（Plan 2 起为产品形态）
//       OkMeter.exe --scan   → 控制台冒烟（Plan 1 保留）
int main(int argc, char** argv) {
  if (argc > 1 && std::string(argv[1]) == "--scan") { /* 旧冒烟代码原样保留 */ return 0; }
  okmeter::DockApp app;
  return app.run(GetModuleHandleW(nullptr));
}
```

- [ ] **Step 1: 按上面序列实现 d3d/app/main/build.bat 改造**

- [ ] **Step 2: 构建**

Run: `cd /d/develop/OpenKnowledge/okmeter && cmd //c build.bat`
Expected: `39 cases, 0 failures` 保持 → `完成：build\OkMeter.exe`；/W4 零警告。

- [ ] **Step 3: 手动验收（实现者必须真实执行并记录）**

Run: `cmd //c build\\OkMeter.exe`
Expected（逐条写进报告）：
1. 屏幕右缘出现透明无边框窗口，内有一个 accent 绿实心圆与一行白色 Consolas 文本；
2. 窗口置顶、不在任务栏、Alt-Tab 列表无 OkMeter；
3. 不抢焦点（点击后原焦点窗口保持激活）；
4. 拖动其他窗口从其下方经过无残影/闪烁；
5. 关闭方式：任务管理器结束进程（本任务无右键菜单，属预期）；进程退出无报错。
另跑 `OkMeter.exe --scan` 确认旧冒烟仍工作（+0 new records，all 与 Plan 1 末次一致）。

- [ ] **Step 4: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): D3D11/D2D/DComp 透明渲染基建与 dock 窗口"
```

---

### Task 7: dock 场景渲染与数据接线（render/dock_scene + ui/app 调度）

**Files:**
- Create: `okmeter/render/dock_scene.h`、`okmeter/render/dock_scene.cpp`
- Modify: `okmeter/ui/app.cpp`（timer 调度、两态、悬停、数据接线）
- Modify: `okmeter/ui/app.h`

**Interfaces:**
- Consumes: `D3DContext`（Task 6）、`layoutArc/applyHover`（Task 5）、`resolveBindings`（Task 4）、`fmtCompact/relTime`（Task 1）、`Spring`（Task 2）、`Store/KimiAdapter/Aggregator`（Plan 1）
- Produces: 完整可见 dock（球体弧线 + 暗夜材质）。

**骨架与必须遵守的行为规格：**

数据接线（ui/app.cpp，单线程 timer 模型）：
- 启动：`Store store(okmeterDir()); store.load();` `KimiAdapter kimi(kimiHome(), &store);` `Config cfg; loadConfig(okmeterDir(), cfg);`
- `WM_TIMER` 三个计时器：动画帧 16ms（弹簧 step + 需要时重绘 + SetWindowPos）、数据轮询 2000ms（`kimi.poll(e→agg.add)` → `store.flush()` → 重建 bindings/文本缓存 → 请求重绘）、相对时间刷新 30s（仅刷新文本缓存）。
- bindings：每次 poll 后 `resolveBindings(cfg, agg)`；显示值：`Binding.isModel ? agg.model(id)->all.total() : agg.{session|today(now)|week(now)|all}().total()`（v1 auto 位一律展示模型累计 all，与原型一致）。

场景绘制（dock_scene.cpp，暗夜材质）：
- 每帧按 `layoutArc(cfg.count, 30, 14, 屏高, cfg.edge)` 重算几何（屏高用 `GetSystemMetrics(SM_CYSCREEN)`，换 DPI/显示器时重建）；`applyHover(g, hoverIdx, 1.34, 10)`。
- 弧线：从首项到末项依次连线（`ID2D1PathGeometry` 或直接画线段序列），hairline 色（白 13%）、宽 1。
- 球：实心圆 fill = 深玻璃色（`D2D1::ColorF(0.06,0.07,0.09,0.92)`）+ 1px 描边（白 13%）；中心项（mid）描边 accent + 外圈 accent 10% 光晕（半径+3 的圆环）。悬停项：`SetTransform(绕中心 scale)` 放大绘制。
- 文本：DWrite TextFormat Consolas 11px（值，白 93%）与 8.5px（模型短名，白 66%），水平/垂直居中于球内；短名 = modelId 最后一段（`/` 后）或口径名（当前会话/今日/本周/累计）。
- 两态：emerge 弹簧 e∈[0,1]；窗口 x = `屏宽 - lerp(24, g.w, e)`（右缘；左缘 x = `lerp(24, g.w, e) - g.w`）；鼠标进入窗口 e→1，离开 600ms 后 e→0（迟滞 timer，进入时取消）。
- 悬停：`WM_MOUSEMOVE` 按 y 坐标找最近项（< 28px 命中），`TrackMouseEvent` 注册 `WM_MOUSELEAVE`；hoverIdx 变化 → 重绘。悬停详情卡属 Task 8，本任务不做。
- 抗锯齿：`dc->SetAntialiasMode(D2D1_ANTIALIAS_MODE_PER_PRIMITIVE)`；文本 `SetTextAntialiasMode(D2D1_TEXT_ANTIALIAS_MODE_CLEARTYPE)`。

- [ ] **Step 1: 实现 dock_scene + app 调度接线**

- [ ] **Step 2: 构建**

Run: `cd /d/develop/OpenKnowledge/okmeter && cmd //c build.bat`
Expected: `39 cases, 0 failures` → `完成：build\OkMeter.exe`，零警告。

- [ ] **Step 3: 手动验收（实现者真实执行，逐条记录）**

前置：先跑 `OkMeter.exe --scan` 记下 all 值。然后启动 `OkMeter.exe`：
1. 右缘初始为收缩态：只露出约 24px 一条细边，不抢注意力；
2. 鼠标移入：150~200ms 内平滑滑出完整弧线 + 奇数个球（默认 3）；中心球显示全部累计紧凑值（与 --scan 的 all 一致），两侧球显示最近使用模型的累计；
3. 悬停某球：该球放大 ~1.34×，邻球沿弧线让位（近多远少），其余不动；移开复位；
4. 鼠标离开窗口约 600ms 后收回收缩态；
5. 数字随真实会话增长（后台跑一段 Kimi CLI，≤2s 轮询周期后看到变化）；
6. 换 DPI/拖拽到另一显示器（若有）不花屏；窗口管理器开关透明效果无异常。

- [ ] **Step 4: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): dock 场景渲染、两态弹簧动画与真实数据接线"
```

---

### Task 8: 悬停详情卡

**Files:**
- Modify: `okmeter/render/dock_scene.h/.cpp`（详情卡绘制）
- Modify: `okmeter/ui/app.cpp`（窗口宽度扩展 + 悬停卡数据组装）

**Interfaces:**
- Consumes: `fmtExact/relTime`（Task 1）、`Binding`（Task 4）、Aggregator 查询（Plan 1）
- Produces: 规格 §3.4 详情卡（模型模式/总量模式两种内容）。

**行为规格（必须遵守）：**
- 悬停项时，在该球屏内侧（右缘 → 球左侧）浮出详情卡，宽 252px、圆角 12、深玻璃底（白 6% + blur 替代：90% 不透明深底，v1 无 backdrop blur，Plan 2b 材质层统一解决）、1px hairline 描边；不遮挡其他球。
- 窗口布局调整：展开态窗口宽 = `g.w + 268`（卡区），卡区在球区与屏缘之间（右缘：球区靠右，卡区靠左）；透明区不绘制。窗口为矩形命中区，卡区悬停保持 dock 展开（可接受，与原型一致）。
- 内容随绑定模式：
  - **模型模式**：模型 id（10.5px dim）、精确累计（21px mono 千分位）、今日/本周/累计三行（label dim + 值 mono 右对齐）、底部 `最近调用 {relTime} · 模型模式`；
  - **总量模式**：口径名 + ` · 总量模式`、精确值、当前会话/今日/全部累计三行、input 构成（mergeCache 时 `input {exact} · cache 命中 {pct}%` 一行 + output 一行；否则 常规 input / cache 读 / cache 新建 / output 四行）。cache pct = (cacheRead+cacheCreation)/input。
- 切球即换内容，移出（hoverIdx<0）即隐；卡文本在 hoverIdx 变化时重组（无每帧重建 TextLayout 的性能问题即可，v1 直接每帧 DrawText 也行）。

- [ ] **Step 1: 实现详情卡**

- [ ] **Step 2: 构建 + 手动验收**

Run: `cd /d/develop/OpenKnowledge/okmeter && cmd //c build.bat` → `39 cases, 0 failures`、零警告。
Run: `cmd //c build\\OkMeter.exe`，逐条记录：
1. 悬停模型球：卡出屏内侧，模型名/三行/最近调用相对时间正确（与 --scan 数据同源）；
2. 悬停中心球（全部累计绑定）：会话/今日/累计三行 + input/output 构成正确；mergeCache 开时 input 合并一行含 cache 百分比；
3. 切球内容即换，移出即隐；卡不遮挡邻球；
4. 收缩态不显示卡。

- [ ] **Step 3: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 悬停详情卡（模型/总量双模式）"
```

---

### Task 9: 目录监听、换边与右键菜单（换边/退出）

**Files:**
- Create: `okmeter/ui/watch.h`、`okmeter/ui/watch.cpp`（替换 Task 6 空壳）
- Modify: `okmeter/ui/app.cpp`（RDCW 接线、右键菜单、换边、config 保存）

**Interfaces:**
- Consumes: ReadDirectoryChangesW、`Config`（Task 3）
- Produces: 规格 §3.7 监听（RDCW + 轮询兜底）与 §3.1/§3.5 的基础右键菜单。

**骨架与行为规格：**

DirWatcher（ui/watch.*）：
- `bool start(const std::filesystem::path& dir)`：`CreateFileW(dir, FILE_LIST_DIRECTORY, FILE_SHARE_READ|WRITE|DELETE, nullptr, OPEN_EXISTING, FILE_FLAG_BACKUP_SEMANTICS|FILE_FLAG_OVERLAPPED)` → `ReadDirectoryChangesW(FILE_NOTIFY_CHANGE_LAST_WRITE|FILE_NOTIFY_CHANGE_FILE_NAME|FILE_NOTIFY_CHANGE_SIZE)` 子树递归，OVERLAPPED 绑 manual-reset event；`signaled()` = `WaitForSingleObject(event,0)==WAIT_OBJECT_0`；触发后重新投递。任一步失败 → `start` 返回 false（调用方回落纯轮询，符合规格"2s 轮询兜底"）。
- 接线：动画 timer 里每 500ms 检查 `signaled()` → 立即执行一次 poll（与 2s 轮询共用同一代码路径）；RDCW 只触发提前 poll，不替代轮询。

右键菜单（WM_RBUTTONUP）：
- `CreatePopupMenu` + 三项：`换边`（cfg.edge 左右互换 → `saveConfig(okmeterDir(), cfg)` → 重建窗口位置与几何）、`退出`（`DestroyWindow`）。
- `SetForegroundWindow(hwnd)` 后 `TrackPopupMenu(TPM_RETURNCMD|TPM_NONOTIFY)`，菜单选空无事发生。
- 换边后 `SetWindowPos` 到左缘镜像位（几何 layoutArc 的 edge 参数同步）。
- 退出前 `store.flush()` 保证游标落盘（析构路径统一）。

- [ ] **Step 1: 实现 watch + 菜单 + 换边**

- [ ] **Step 2: 构建 + 手动验收**

Run: `cd /d/develop/OpenKnowledge/okmeter && cmd //c build.bat` → `39 cases, 0 failures`、零警告。
Run: `cmd //c build\\OkMeter.exe`，逐条记录：
1. 后台跑一段 Kimi CLI 会话，数字在 1s 内跳动（RDCW 提前触发），且 2s 轮询兜底依然有效（可临时停 CLI 验证不报错）；
2. 右键 dock → 菜单两项；`换边` 后 dock 移到左缘镜像、球序与弧线正确；重启进程后仍在左缘（config.json 持久化）；
3. `退出` 干净退出；`%USERPROFILE%\.okryptos\okmeter\state.json` 时间戳更新、无 .tmp 残留；
4. 再次启动：游标生效（--scan 验证 +0 或仅新增）。

- [ ] **Step 3: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 目录变更监听、换边与右键菜单"
```

---

## 后续计划（不在本计划范围）

- **Plan 2b（高级材质与形态）**：WGC 背景捕获（`WDA_EXCLUDEFROMCAPTURE`）+ 毛玻璃/液态玻璃/沉浸光感材质渲染模块 + 胶囊量表/星环罗盘形态；复用 `Registry<T>` 做形态/材质注册表（规格 §3.6）。
- **Plan 3（设置背板）**：D2D/DWrite 背板式设置面板（v8 背板 + 缩略图选项卡），两段式保存；面板跟随当前材质。
- **Plan 4（集成与分发）**：托盘菜单项、build-dist.sh 的 MSVC 步骤、version.rc 接 sync-version、iss [Files]、/SUBSYSTEM:WINDOWS。

## Self-Review 记录

- 规格覆盖：§3.2 → Task 6/7；§3.3 → Task 1/4/5/7；§3.4 → Task 5/7/8；§3.7 监听 → Task 9；线程契约 → 全局约束 + Task 7 单线程 timer 模型。§3.5 设置面板归 Plan 3（本计划右键菜单只含换边/退出，无"设置…"项——避免假功能按钮）。
- 有意留白修正项以内联"注意"标注（fmt 的 cstring、config 的 minjson 数组访问器、binding 的废笔删除）——执行时必须照落地。
- 类型一致性：Binding/Scope 在 Task 4 定义、Task 7/8 消费一致；DockGeom 在 Task 5 定义、7/8 消费一致；D3DContext 接口 Task 6 定义、7 消费一致。
