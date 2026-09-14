#include "app.h"
#include "../adapters/claude/adapter.h"
#include "../adapters/codex/adapter.h"
#include "../adapters/dsh/adapter.h"
#include "../adapters/hanako/adapter.h"
#include "../adapters/kimi/adapter.h"
#include "../adapters/qwen/adapter.h"
#include "../adapters/reasonix/adapter.h"
#include "../adapters/workbuddy/adapter.h"
#include "../adapters/zcode/adapter.h"
#include "../core/fmt.h"
#include "../core/paths.h"
#include "../core/store.h"
#include "../render/catalog.h"
#include <chrono>
#include <cmath>
#include <string>
#include <wtsapi32.h>

// 动画流畅性诊断：帧间隔 + 分阶段耗时统计。默认仅 _DEBUG 构建启用；
// 临时诊断可在本行下方加 `#define OKM_ANIM_DIAG 1`（release 也生效）。
#if defined(_DEBUG) && !defined(OKM_ANIM_DIAG)
#define OKM_ANIM_DIAG 1
#endif
#if defined(OKM_ANIM_DIAG)
#include <algorithm>
#include <cstdio>
#include "../core/paths.h"
#endif

namespace okmeter {
namespace {

constexpr wchar_t kClassName[] = L"OkMeterDock";
constexpr int kCardZoneW = 268;     // 展开态卡区宽（卡 252 + 两侧边距）
constexpr UINT_PTR kTimerAnim = 1;    // 动画帧 16ms
constexpr UINT_PTR kTimerPoll = 2;    // 数据兜底轮询 2000ms（RDCW 健康时退避 30s）
constexpr UINT_PTR kTimerRel = 3;     // 相对时间/口径文本刷新 30s
constexpr UINT_PTR kTimerRetract = 4; // 离开迟滞 600ms（一次性）
constexpr UINT_PTR kTimerShot = 5;    // --shot 自检：启动 2.5s 后截图退出（一次性）
constexpr int kDragThreshold = 6;   // 拖拽阈值 px（阈值内视为按压/点击）
constexpr double kMenuAnimSec = 0.12;  // 菜单弹出动画 120ms（原型 .ctx cardin）
constexpr double kPanelAnimSec = 0.18; // 设置面板滑入动画 180ms（原型 panelin）
constexpr double kCardAnimSec = 0.14;  // 详情卡出现动画 140ms（原型 .detail cardin）

int64_t nowMs() {
  return std::chrono::duration_cast<std::chrono::milliseconds>(
      std::chrono::system_clock::now().time_since_epoch()).count();
}

// QPC 间隔（秒）：动画弹簧用真实 elapsed dt，timer 抖动不再变成动画抖动
LARGE_INTEGER qpcNow() { LARGE_INTEGER t; QueryPerformanceCounter(&t); return t; }

double qpcSeconds(LARGE_INTEGER prev, LARGE_INTEGER now) {
  static const double kFreq = [] {
    LARGE_INTEGER f;
    QueryPerformanceFrequency(&f);
    return (double)f.QuadPart;
  }();
  return (double)(now.QuadPart - prev.QuadPart) / kFreq;
}

std::wstring wide(const std::string& s) {
  if (s.empty()) return {};
  const int n = MultiByteToWideChar(CP_UTF8, 0, s.c_str(), (int)s.size(), nullptr, 0);
  std::wstring out((size_t)n, L'\0');
  MultiByteToWideChar(CP_UTF8, 0, s.c_str(), (int)s.size(), out.data(), n);
  return out;
}

// 短名：modelId 最后一段（/ 后）
std::string shortName(const std::string& modelId) {
  const size_t p = modelId.rfind('/');
  return p == std::string::npos ? modelId : modelId.substr(p + 1);
}

// 厂商段：modelId 第一段（/ 前；无 / 则全名）
std::string vendorOf(const std::string& modelId) {
  const size_t p = modelId.find('/');
  return p == std::string::npos ? modelId : modelId.substr(0, p);
}

const wchar_t* scopeLabel(Scope s) {
  switch (s) {
  case Scope::Session: return L"当前会话";
  case Scope::Today:   return L"今日";
  case Scope::Week:    return L"本周";
  case Scope::All:     return L"累计";
  }
  return L"累计";
}

double lerp(double a, double b, double t) { return a + (b - a) * t; }

// 原型 --ease-dock：cubic-bezier(.22,.8,.3,1)；对参数 t 牛顿迭代求 x 后的 y 值
double easeDock(double x) {
  if (x <= 0) return 0;
  if (x >= 1) return 1;
  double t = x;
  for (int i = 0; i < 5; ++i) {
    const double u = 1 - t;
    const double cx = 3*u*u*t*0.22 + 3*u*t*t*0.30 + t*t*t - x;
    const double dx = 3*u*u*0.22 + 6*u*t*(0.30 - 0.22) + 3*t*t*(1.0 - 0.30);
    if (std::abs(dx) < 1e-9) break;
    t -= cx / dx;
    t = t < 0 ? 0 : (t > 1 ? 1 : t);
  }
  const double u = 1 - t;
  return 3*u*u*t*0.8 + 3*u*t*t*1.0 + t*t*t;
}

#if defined(OKM_ANIM_DIAG)
// ── 动画诊断（临时）：动画激活期间逐帧记录 QPC 间隔与 step/挪窗/绘制/Present 耗时，
// settled 时把 min/p50/p95/max/方差 + 原始序列追加到 ~/.okryptos/okmeter/anim-diag.log。
struct AnimDiag {
  struct Frame { double dtMs, stepMs, swpMs, drawMs, presentMs; };
  std::vector<Frame> frames;
  LARGE_INTEGER freq{}, prevTick{}, tick{}, drawBegin{};
  bool active = false;
  bool expanding = false;
  double framesDrawMs_ = 0;
  AnimDiag() { QueryPerformanceFrequency(&freq); }
  double ms(LARGE_INTEGER a, LARGE_INTEGER b) const {
    return (double)(b.QuadPart - a.QuadPart) * 1000.0 / (double)freq.QuadPart;
  }
};
AnimDiag g_diag;

void diagFrameBegin(double target) {
  const LARGE_INTEGER now = qpcNow();
  if (!g_diag.active) {
    g_diag.active = true;
    g_diag.expanding = target > 0.5;
    g_diag.frames.clear();
    g_diag.prevTick = now;
  }
  g_diag.tick = now;
}

void diagFrameEnd(double stepMs, double swpMs, double drawMs, double presentMs) {
  g_diag.frames.push_back({g_diag.ms(g_diag.prevTick, g_diag.tick),
                           stepMs, swpMs, drawMs, presentMs});
  g_diag.prevTick = g_diag.tick;
}

void diagFlush() {
  g_diag.active = false;
  const auto& f = g_diag.frames;
  if (f.empty()) return;
  std::vector<double> dt;
  dt.reserve(f.size());
  double sum = 0, maxStep = 0, maxSwp = 0, maxDraw = 0, maxPresent = 0;
  for (const auto& r : f) {
    dt.push_back(r.dtMs);
    sum += r.dtMs;
    if (r.stepMs > maxStep) maxStep = r.stepMs;
    if (r.swpMs > maxSwp) maxSwp = r.swpMs;
    if (r.drawMs > maxDraw) maxDraw = r.drawMs;
    if (r.presentMs > maxPresent) maxPresent = r.presentMs;
  }
  std::sort(dt.begin(), dt.end());
  const double mean = sum / (double)f.size();
  double var = 0;
  for (double d : dt) var += (d - mean) * (d - mean);
  var /= (double)f.size();
  const double p50 = dt[dt.size() / 2];
  const double p95 = dt[(std::min)(dt.size() - 1, (size_t)std::ceil(dt.size() * 0.95) - 1)];
  const std::string path = (okmeterDir() / "anim-diag.log").string();
  FILE* fp = nullptr;
  if (fopen_s(&fp, path.c_str(), "a") == 0 && fp) {
    fprintf(fp, "[%s] frames=%zu dt min=%.2f p50=%.2f p95=%.2f max=%.2f mean=%.2f var=%.3f p95-p50=%.2f | max step=%.2f swp=%.2f draw=%.2f present=%.2f\n",
            g_diag.expanding ? "expand" : "collapse", f.size(),
            dt.front(), p50, p95, dt.back(), mean, var, p95 - p50,
            maxStep, maxSwp, maxDraw, maxPresent);
    fprintf(fp, "  raw dt:");
    for (double d : dt) fprintf(fp, " %.1f", d);
    fprintf(fp, "\n");
    fclose(fp);
  }
}
#endif

Sums scopeSums(const Aggregator& agg, Scope s, int64_t now) {
  switch (s) {
  case Scope::Session: return agg.session();
  case Scope::Today:   return agg.today(now);
  case Scope::Week:    return agg.week(now);
  case Scope::All:     return agg.all();
  }
  return agg.all();
}

} // namespace

DockApp::~DockApp() = default;

// 形态/材质：模块目录（render/catalog.h）按 id 创建，未知 id 回退目录首项（arc/dark）
void DockApp::createModules() {
  form_ = render::createForm(cfg_.form);
  if (!form_) form_ = render::createForm(render::kFormCatalog[0].id);
  material_ = render::createMaterial(cfg_.material);
  if (!material_) material_ = render::createMaterial(render::kMaterialCatalog[0].id);
}

void DockApp::rebuildItems() {
  Aggregator& agg = store_->agg();
  bindings_ = resolveBindings(cfg_, agg);
  const int64_t now = nowMs();
  std::vector<int64_t> raw(bindings_.size(), 0);
  int64_t maxV = 0;
  items_.assign(bindings_.size(), render::DockItem{});  for (size_t i = 0; i < bindings_.size(); ++i) {
    const Binding& b = bindings_[i];
    int64_t v = 0;
    if (b.isModel) {
      // v1 auto/模型位一律展示模型累计 all（与原型一致）
      if (const ModelStat* m = agg.model(b.modelId)) v = m->all.total();
      items_[i].label = wide(shortName(b.modelId));
    } else {
      v = scopeSums(agg, b.scope, now).total();
      items_[i].label = scopeLabel(b.scope);
    }
    raw[i] = v;
    if (v > maxV) maxV = v;
    items_[i].value = wide(fmtCompact(v));
    items_[i].raw = (double)v;
    // wave 波形流历史（最近 26 点）：记录**本周期增量**（累计值近乎直线，增量才
    // 是活动波形）；按绑定键持久保存（rebuild 重建 items_ 不丢）
    const std::string hkey = b.isModel
        ? "m:" + b.modelId
        : "s:" + std::to_string(static_cast<int>(b.scope));
    auto& hv = histByKey_[hkey];
    if (hv.empty()) hv.assign(25, 0.0);  // 启动即出全宽平线（wave 需 ≥2 点才绘制），
                                         // 零值在前随真实增量自然右侧滚出
    const auto prev = rawByKey_.find(hkey);
    const double delta = prev == rawByKey_.end() ? 0.0
        : (double)(v > prev->second ? v - prev->second : 0);
    rawByKey_[hkey] = v;
    hv.push_back(delta);
    if (hv.size() > 26) hv.erase(hv.begin());
    items_[i].hist = hv;
  }
  // 胶囊占比条：该项值/全部项最大值（原型 updateItem capsule 同款；全零 → 4% 地板）
  for (size_t i = 0; i < raw.size(); ++i)
    items_[i].ratio = maxV > 0 ? (double)raw[i] / (double)maxV : 0.04;
  rebuildCard();  // 数据/口径刷新后卡内容同源更新
}

// 悬停详情卡组装：hoverIdx<0 或越界 → 隐藏；模型/总量双模式（规格 §3.4）
void DockApp::rebuildCard() {
  const bool wasValid = card_.valid;
  card_ = render::DetailCard{};
  if (hoverIdx_ < 0 || hoverIdx_ >= (int)bindings_.size()) return;
  Aggregator& agg = store_->agg();
  const Binding& b = bindings_[(size_t)hoverIdx_];
  const int64_t now = nowMs();
  auto row = [&](const wchar_t* label, int64_t v) {
    card_.rows.emplace_back(label, wide(fmtExact(v)));  // 千分位原数（单位看着更费劲，用户裁决）
  };
  if (b.isModel) {
    const ModelStat* m = agg.model(b.modelId);
    if (!m) return;
    card_.title = wide(b.modelId);
    card_.big = wide(fmtExact(agg.modelToday(b.modelId, now).total()));  // 顶部=今日消耗
    row(L"本周", agg.modelWeek(b.modelId, now).total());
    row(L"本月", agg.modelMonth(b.modelId, now).total());
    row(L"累计", m->all.total());
    card_.foot = L"最近调用 " + wide(relTime(m->lastCallMs, now)) + L" · 模型模式";
  } else {
    const Sums t = scopeSums(agg, b.scope, now);
    card_.title = std::wstring(scopeLabel(b.scope)) + L" · 总量模式";
    card_.big = wide(fmtExact(t.total()));
    row(L"当前会话", agg.session().total());
    row(L"今日", agg.today(now).total());
    row(L"全部累计", agg.all().total());
    const int64_t input = t.inputOther + t.inputCacheRead + t.inputCacheCreation;
    if (cfg_.mergeCache) {
      // cache pct = (cacheRead+cacheCreation)/input×100，input=0 时 pct=0
      const long pct = input
          ? (long)std::lround(100.0 * (t.inputCacheRead + t.inputCacheCreation) / input)
          : 0;
      card_.rows.emplace_back(L"输入",
          wide(fmtExact(input)) + L" · cache 命中 " + std::to_wstring(pct) + L"%");
      row(L"输出", t.output);
    } else {
      row(L"常规输入", t.inputOther);
      row(L"cache 读", t.inputCacheRead);
      row(L"cache 新建", t.inputCacheCreation);
      row(L"输出", t.output);
    }
  }
  card_.valid = true;
  if (!wasValid) cardShownQpc_ = qpcNow();  // invalid→valid 出现沿（cardin 140ms）
}

void DockApp::pollData() {
  Aggregator& agg = store_->agg();
  int arrived = 0;
  for (auto& a : adapters_)
    arrived += a->poll([&](const UsageEvent& e) { agg.add(e); });
  lastPollMs_ = nowMs();
  if (arrived > 0) {
    store_->flush();            // 零新事件不落盘（state.json 无变化，省一次原子写）
    material_->onPulse();       // 数据到达 → 沉浸光感粒子迸散
  }
  rebuildItems();
  render();
}

// 窗口中心所在屏的工作区（规格 §3.2 多显示器按窗口中心所在屏）；
// 窗口未创建/查询失败回退主屏工作区
RECT DockApp::workArea() const {
  if (hwnd_) {
    RECT wr{};
    if (GetWindowRect(hwnd_, &wr)) {
      const POINT c{ (wr.left + wr.right) / 2, (wr.top + wr.bottom) / 2 };
      MONITORINFO mi{ sizeof(mi) };
      if (GetMonitorInfoW(MonitorFromPoint(c, MONITOR_DEFAULTTONEAREST), &mi))
        return mi.rcWork;
    }
  }
  RECT work{};
  SystemParametersInfoW(SPI_GETWORKAREA, 0, &work, 0);
  return work;
}

// 悬停/按压命中：按烘焙后坐标（让位 dy + 卡区偏移 dx）算 2D 归一化距离，
// ≤1 的最近项命中（胶囊按半宽/半高矩形归一，罗盘卫星与中心分离可点）
int DockApp::hitItem(const DockGeom& g, int mx, int my, float dx) const {
  int idx = -1;
  double best = 1.0;
  for (size_t i = 0; i < g.items.size(); ++i) {
    const ItemGeom& it = g.items[i];
    const double halfX = it.hw > 0 ? it.hw : it.r;
    const double bx = it.x + dx;
    const double by = it.y + it.dy;
    const double nx = (mx - bx) / (halfX + 8.0);
    const double ny = (my - by) / (it.r + 8.0);
    const double d = nx * nx + ny * ny;
    if (d < best) { best = d; idx = (int)i; }
  }
  return best <= 1.0 ? idx : -1;
}

void DockApp::rebuildLayout() {
  const RECT work = workArea();
  const int screenH = (int)(work.bottom - work.top);
  const int screenW = (int)(work.right - work.left);
  const DockGeom g = form_->layout(cfg_.count, screenW, screenH, cfg_.edge);
  int h = (int)g.h;
  if (h < 120) h = 120;
  dockW_ = (int)g.w;
  winH_ = h;
  winY_ = work.top + ((work.bottom - work.top) - h) / 2;
  applyWindowPos();
}

// 统一窗口矩形落窗：基础矩形（弹簧 e 露出宽 + 卡区 wide 268）与菜单/设置面板
// 屏幕矩形求并集（菜单与面板可同时打开——设置面板的指标级联下拉就是右键同款
// 菜单；同 swapchain 绘制，参照卡区扩展做法）。先算并集最终原点再放置菜单/面板
// （边并边放会让先放的浮层被后续原点移动带跑）；zoneDX_/zoneDY_ 记球区在窗口内
// 的偏移，命中与绘制共用
void DockApp::applyWindowPos() {
  if (!hwnd_ || dragging_) return;  // 拖拽中窗口位置由指针驱动，弹簧不插手
  const RECT work = workArea();
  // 收缩态露出左半 50%（原型 .dock.ready translate(50%) 同款；项另以 55% 透明度
  // 呈现——dim 已在各形态 drawItems 内 0.55+0.45e 处理）
  const double base = dockW_ * (0.5 + 0.5 * emerge_.value);
  int x, w = dockW_ + (wide_ ? kCardZoneW : 0);
  int y = winY_, h = winH_;
  if (cfg_.edge == "left")
    x = work.left + (int)std::lround(base - dockW_);  // 卡区在球区右侧，x 不变
  else
    x = work.right - (int)std::lround(base) - (wide_ ? kCardZoneW : 0);
  // 亮度采样区 = dock 基础矩形对应背景（自适应墨色用；勿用菜单/面板并集矩形——
  // 采样对象是条目背后的亮度）
  backdrop_.setLumaRegion(x, y, w, h);
  // 并集矩形（屏幕坐标）
  int ux = x, uy = y, ur = x + w, ub = y + h;
  if (menu_.open) {
    ux = (std::min)(ux, (int)menuScreen_.left);
    uy = (std::min)(uy, (int)menuScreen_.top);
    ur = (std::max)(ur, (int)menuScreen_.right);
    ub = (std::max)(ub, (int)menuScreen_.bottom);
  }
  // 设置面板打开：并集扩出面板区（dock 对侧屏缘，联合窗口横贯全屏；
  // 中部空白区点击穿透由 WM_NCHITTEST 兜底）
  if (settings_.open) {
    ux = (std::min)(ux, (int)panelScreen_.left);
    uy = (std::min)(uy, (int)panelScreen_.top);
    ur = (std::max)(ur, (int)panelScreen_.right);
    ub = (std::max)(ub, (int)panelScreen_.bottom);
  }
  zoneDX_ = (wide_ && cfg_.edge == "right" ? kCardZoneW : 0) + (x - ux);
  zoneDY_ = y - uy;
  if (menu_.open)
    menu_.place((float)(menuMainScreen_.left - ux), (float)(menuMainScreen_.top - uy));
  if (settings_.open)
    settings_.place((float)(panelScreen_.left - ux), (float)(panelScreen_.top - uy),
                    (float)(panelScreen_.bottom - panelScreen_.top));
  SetWindowPos(hwnd_, nullptr, ux, uy, ur - ux, ub - uy,
               SWP_NOZORDER | SWP_NOACTIVATE);  // 尺寸变化 → WM_SIZE → d3d.resize + render
  if (menu_.open && (menu_.parent1 >= 0 || menu_.parent2 >= 0))
    placeSubColumns();  // 扩窗后按新窗口原点重布子列（屏幕坐标不变）
}

// 设置面板打开期间的跨进程点击穿透：WM_NCHITTEST 的 HTTRANSPARENT 只对同线程
// 窗口有效（MSDN/Raymond Chen），跨进程必须 WS_EX_TRANSPARENT。但样式是整窗的，
// 面板/球区又要可交互 → 按指针位置动态开关：帧时钟每帧轮询 GetCursorPos
//（面板打开期帧时钟必在跑），指针进面板/球区即清样式，离开即置样式。
// 鸡生蛋问题不存在：穿透期间收不到 WM_MOUSEMOVE，但轮询不依赖消息。
void DockApp::syncClickThru() {
  if (!hwnd_) return;
  bool want = false;
  if (settings_.open) {
    POINT pt{};
    GetCursorPos(&pt);
    // 可命中区 = 面板矩形 ∪ 打开的级联映射菜单 ∪ 球区竖条（详情卡区仅展示不吞点击）
    RECT wr{};
    GetWindowRect(hwnd_, &wr);
    bool inPanel = settings_.contains(pt.x - (int)wr.left, pt.y - (int)wr.top);
    if (menu_.open && PtInRect(&menuScreen_, pt)) inPanel = true;
    const RECT work = workArea();
    const RECT ballZone{ cfg_.edge == "right" ? work.right - dockW_ : work.left,
                         winY_,
                         cfg_.edge == "right" ? work.right : work.left + dockW_,
                         winY_ + winH_ };
    want = !inPanel && !PtInRect(&ballZone, pt);
  }
  if (want == clickThru_) return;
  clickThru_ = want;
  LONG_PTR ex = GetWindowLongPtrW(hwnd_, GWL_EXSTYLE);
  if (want) ex |= WS_EX_TRANSPARENT;
  else ex &= ~(LONG_PTR)WS_EX_TRANSPARENT;
  SetWindowLongPtrW(hwnd_, GWL_EXSTYLE, ex);
  SetWindowPos(hwnd_, nullptr, 0, 0, 0, 0,
               SWP_NOMOVE | SWP_NOSIZE | SWP_NOZORDER | SWP_NOACTIVATE |
                   SWP_FRAMECHANGED);  // 强制重命中测试
}

// 展开/收缩切换窗口宽度（球区位置不动，卡区在屏内侧增减）
void DockApp::setWide(bool w) {
  if (wide_ == w || !hwnd_) return;
  wide_ = w;
  applyWindowPos();
}

void DockApp::setEmergeTarget(double t) {
  if (emergeTarget_ == t) return;
  emergeTarget_ = t;
  emerged_ = false;
  setWide(t > 0.5);
}

// 换边：edge 互换 → 持久化 config.json → 重建位置与几何（layoutArc edge 同步）
void DockApp::flipEdge() {
  cfg_.edge = cfg_.edge == "right" ? "left" : "right";
  saveConfig(okmeterDir(), cfg_);
  rebuildLayout();
  render();
}

// 退出：游标落盘后销毁窗口（与正常析构同一路径）
void DockApp::exitApp() {
  if (store_) store_->flush();
  DestroyWindow(hwnd_);
}

// 菜单内容组装：球上右键 = 标题"第 N 项 · 位置" + 映射组（当前值 ✓）+ 分隔 +
// 设置…（打开背板设置面板）+ 换边 + 退出；弧线/空白 = 仅后三项（规格 §3.3/§3.5）
void DockApp::buildMenuEntries(int slot) {
  menu_.entries.clear();
  if (slot >= 0 && slot < cfg_.count) {
    const int mid = (cfg_.count - 1) / 2;
    const int d = slot - mid;
    const std::wstring pos =
        d == 0 ? L"中心" : (d < 0 ? L"上 " + std::to_wstring(-d)
                                  : L"下 " + std::to_wstring(d));
    MenuEntry title;
    title.kind = MenuEntry::Title;
    title.label = L"第 " + std::to_wstring(slot + 1) + L" 项 · " + pos;
    menu_.entries.push_back(std::move(title));
    // 一级：总量 ▸ / 模型 ▸（二级展开；不再有"默认 · 按最近使用"项）
    MenuEntry total;
    total.kind = MenuEntry::Parent;
    total.label = L"总量";
    total.sub = 1;
    menu_.entries.push_back(std::move(total));
    MenuEntry models;
    models.kind = MenuEntry::Parent;
    models.label = L"模型";
    models.sub = 2;
    menu_.entries.push_back(std::move(models));
    MenuEntry sep;
    sep.kind = MenuEntry::Separator;
    menu_.entries.push_back(sep);
  }
  MenuEntry settings;
  settings.label = L"设置…";
  settings.action = 1;
  menu_.entries.push_back(std::move(settings));
  MenuEntry flip;
  flip.label = L"换边";
  flip.action = 2;
  menu_.entries.push_back(std::move(flip));
  MenuEntry quit;
  quit.label = L"退出";
  quit.action = 3;
  menu_.entries.push_back(std::move(quit));
}

// 当前映射值：设置面板的级联下拉取 draft（两段式保存），球区右键菜单取已生效 cfg
std::string DockApp::mappingOf(int slot) const {
  const std::vector<std::string>& m =
      menuForSettings_ ? settings_.draft.mapping : cfg_.mapping;
  return slot >= 0 && slot < (int)m.size() ? m[(size_t)slot] : "";
}

// 二级子列：subKind 1=总量四口径；2=模型厂商分组（按 modelId 首段聚合，按最近用排序）
void DockApp::openSub1(int parentIdx, int subKind) {
  menu_.parent1 = parentIdx;
  menu_.parent2 = -1;
  menu_.sub2.clear();
  menu_.sub1.clear();
  const std::string cur = mappingOf(menu_.slot);
  auto leaf = [&](const std::wstring& label, const std::string& v) {
    MenuEntry e;
    e.kind = MenuEntry::Item;
    e.label = label;
    e.value = v;
    e.tick = cur == v;
    menu_.sub1.entries.push_back(std::move(e));
  };
  if (subKind == 1) {
    leaf(L"当前会话", "total:session");
    leaf(L"今日用量", "total:today");
    leaf(L"本周用量", "total:week");
    leaf(L"全部累计", "total:all");
  } else {
    std::vector<std::string> vendors;
    for (const std::string& id : store_->agg().modelsByRecency()) {
      const std::string v = vendorOf(id);
      if (std::find(vendors.begin(), vendors.end(), v) == vendors.end())
        vendors.push_back(v);
    }
    for (const std::string& v : vendors) {
      MenuEntry e;
      e.kind = MenuEntry::Parent;
      e.label = wide(v);
      e.value = v;
      e.sub = 3;
      menu_.sub1.entries.push_back(std::move(e));
    }
  }
  menu_.layout(d3d_);
  placeSubColumns();
  applyWindowPos();
  render();
}

// 三级子列：某厂商下的具体模型
void DockApp::openSub2(int parentIdx, const std::string& vendor) {
  menu_.parent2 = parentIdx;
  menu_.sub2.clear();
  const std::string cur = mappingOf(menu_.slot);
  for (const std::string& id : store_->agg().modelsByRecency()) {
    if (vendorOf(id) != vendor) continue;
    MenuEntry e;
    e.kind = MenuEntry::Item;
    e.label = wide(shortName(id));
    e.value = "model:" + id;
    e.tick = cur == e.value;
    menu_.sub2.entries.push_back(std::move(e));
  }
  menu_.layout(d3d_);
  placeSubColumns();
  applyWindowPos();
  render();
}

// 子列定位：沿 menuDir_ 朝屏内逐级展开（球区菜单朝屏内，设置下拉朝面板内侧），
// 与父项行顶对齐
void DockApp::placeSubColumns() {
  const RECT work = workArea();
  RECT wr{};
  GetWindowRect(hwnd_, &wr);
  const int nx = (int)menu_.rect.left + (int)wr.left;   // 主列屏幕坐标
  const int ny = (int)menu_.rect.top + (int)wr.top;
  const bool towardLeft = menuDir_ < 0;
  if (menu_.parent1 >= 0 && menu_.parent1 < (int)menu_.entries.size() &&
      !menu_.sub1.entries.empty()) {
    const MenuEntry& p = menu_.entries[(size_t)menu_.parent1];
    int sx = towardLeft ? nx - menu_.sub1.width + 1 : nx + menu_.width - 1;
    if (sx < work.left + 4) sx = work.left + 4;
    if (sx + menu_.sub1.width > work.right - 4) sx = work.right - 4 - menu_.sub1.width;
    int sy = ny + (int)p.y0 - 5;
    if (sy + menu_.sub1.height > work.bottom - 4) sy = work.bottom - 4 - menu_.sub1.height;
    if (sy < work.top + 4) sy = work.top + 4;
    menu_.placeSub(menu_.sub1, (float)(sx - (int)wr.left), (float)(sy - (int)wr.top));
  }
  if (menu_.parent2 >= 0 && menu_.parent2 < (int)menu_.sub1.entries.size() &&
      !menu_.sub2.entries.empty()) {
    const MenuEntry& p = menu_.sub1.entries[(size_t)menu_.parent2];
    const int s1x = (int)menu_.sub1.rect.left + (int)wr.left;
    const int s1y = (int)menu_.sub1.rect.top + (int)wr.top;
    int sx = towardLeft ? s1x - menu_.sub2.width + 1 : s1x + menu_.sub1.width - 1;
    if (sx < work.left + 4) sx = work.left + 4;
    if (sx + menu_.sub2.width > work.right - 4) sx = work.right - 4 - menu_.sub2.width;
    int sy = s1y + (int)p.y0 - 5;
    if (sy + menu_.sub2.height > work.bottom - 4) sy = work.bottom - 4 - menu_.sub2.height;
    if (sy < work.top + 4) sy = work.top + 4;
    menu_.placeSub(menu_.sub2, (float)(sx - (int)wr.left), (float)(sy - (int)wr.top));
  }
  // 外点判定/扩窗矩形 = 全列并集（屏幕坐标）
  const D2D1_RECT_F b = menu_.bounds();
  menuScreen_ = RECT{ (int)b.left + (int)wr.left, (int)b.top + (int)wr.top,
                      (int)b.right + (int)wr.left, (int)b.bottom + (int)wr.top };
}

void DockApp::openMenu(int clientX, int clientY, int slot) {
  buildMenuEntries(slot);
  menu_.layout(d3d_);
  menuForSettings_ = false;
  settingsMenuOwner_ = -1;
  settings_.menuSlot = -1;
  menuDir_ = cfg_.edge == "right" ? -1 : 1;  // 朝屏内侧级联（右缘向左，左缘向右）
  menu_.subDir = menuDir_;                   // 箭头随展开方向（左 ◂ 右 ▸）
  POINT pt{ clientX, clientY };
  ClientToScreen(hwnd_, &pt);
  const RECT work = workArea();
  const int mw = menu_.width, mh = menu_.height;
  // 朝屏内侧展开（屏缘侧不出屏）：右缘向左开，点击点与菜单间留 4px 搭边
  int sx;
  if (cfg_.edge == "right") {
    sx = pt.x - mw + 4;
    if (sx < work.left + 8) sx = work.left + 8;
    if (sx + mw > work.right - 8) sx = work.right - 8 - mw;  // 屏缘侧不出屏
  } else {
    sx = pt.x - 4;
    if (sx + mw > work.right - 8) sx = work.right - 8 - mw;
    if (sx < work.left + 8) sx = work.left + 8;
  }
  int sy = pt.y - 8;
  if (sy + mh > work.bottom - 8) sy = work.bottom - 8 - mh;
  if (sy < work.top + 8) sy = work.top + 8;
  menuScreen_ = RECT{ sx, sy, sx + mw, sy + mh };
  menuMainScreen_ = menuScreen_;  // 主列定位基准（级联展开后并集变大，主列不动）
  menu_.slot = slot;
  menu_.open = true;
  menu_.hover = -1;
  menu_.parent1 = menu_.parent2 = -1;  // 新菜单从一级开始（上次级联状态清除）
  menu_.sub1.clear();
  menu_.sub2.clear();
  menuOpenQpc_ = qpcNow();
  // 沿检测基准：打开当帧若键已按下（如右键尚未松开）不误判为收起点击
  prevEsc_ = (GetAsyncKeyState(VK_ESCAPE) & 0x8000) != 0;
  prevLmb_ = (GetAsyncKeyState(VK_LBUTTON) & 0x8000) != 0;
  prevRmb_ = (GetAsyncKeyState(VK_RBUTTON) & 0x8000) != 0;
  KillTimer(hwnd_, kTimerRetract);
  setEmergeTarget(1);   // 菜单期间保持展开（原型 holdOpen）
  applyWindowPos();     // 菜单区纳入窗口（并集扩窗 → WM_SIZE → render）
  render();
}

void DockApp::closeMenu() {
  if (!menu_.open) return;
  menu_.open = false;
  menu_.hover = -1;
  menu_.parent1 = menu_.parent2 = -1;  // 级联子列一并收起
  menu_.sub1.clear();
  menu_.sub2.clear();
  menuForSettings_ = false;
  settingsMenuOwner_ = -1;
  settings_.menuSlot = -1;
  applyWindowPos();     // 窗口收回基础矩形
  POINT pt{};
  GetCursorPos(&pt);
  RECT wr{};
  GetWindowRect(hwnd_, &wr);
  if (!PtInRect(&wr, pt) && !cfg_.pinned) {  // 指针已在窗外：恢复 600ms 迟滞收回（保持显示除外）
    KillTimer(hwnd_, kTimerRetract);
    SetTimer(hwnd_, kTimerRetract, 600, nullptr);
  }
  render();
}

// 设置面板指标下拉：复用右键同一 GlassMenu 的侧向级联（默认 · 按最近使用 +
// 总量 + 模型 父项，勾选/级联/动画/沿检测全同款），主列锚定 gsel 行正下方
// （放不下翻上方）、与 gsel 对齐，子列朝屏内逐级展开，箭头方向 = 展开方向
// （向左 ◂ 向右 ▸）；叶项由 applyMenuMapping 改 draft（两段式保存，不即时落盘）

// 主列锚定：贴 owner gsel 行正下方（原型 drop 下展同款），水平与 gsel 对齐
//（朝屏内一侧生长），下方放不下翻到行上方
void DockApp::placeSettingsMenu() {
  RECT wr{};
  GetWindowRect(hwnd_, &wr);
  const D2D1_RECT_F gr = settings_.gselRect(settingsMenuOwner_);  // 窗口客户区坐标
  const RECT work = workArea();
  const int mw = menu_.width, mh = menu_.height;
  int sx = menuDir_ > 0 ? (int)wr.left + (int)gr.left
                        : (int)wr.left + (int)gr.right - mw;
  if (sx < work.left + 8) sx = work.left + 8;
  if (sx + mw > work.right - 8) sx = work.right - 8 - mw;  // 屏缘侧不出屏
  int sy = (int)wr.top + (int)gr.bottom + 4;  // 默认下展
  if (sy + mh > work.bottom - 8)
    sy = (int)wr.top + (int)gr.top - 4 - mh;  // 下方放不下 → 翻到行上方
  if (sy < work.top + 8) sy = work.top + 8;
  menuScreen_ = RECT{ sx, sy, sx + mw, sy + mh };
  menuMainScreen_ = menuScreen_;  // 主列定位基准（级联展开后并集变大，主列不动）
}

void DockApp::openSettingsMenu(int gselCtrl) {
  const int slot = settings_.ctrl(gselCtrl).a;
  menuForSettings_ = true;  // 先于 mappingOf 置位（勾选源取 draft）
  settingsMenuOwner_ = gselCtrl;
  settings_.menuSlot = slot;
  const std::string cur = mappingOf(slot);
  menu_.entries.clear();
  MenuEntry def;
  def.kind = MenuEntry::Item;
  def.label = L"默认 · 按最近使用";
  def.value = "auto";
  def.tick = cur.empty() || cur == "auto";
  menu_.entries.push_back(std::move(def));
  MenuEntry total;
  total.kind = MenuEntry::Parent;
  total.label = L"总量";
  total.sub = 1;
  menu_.entries.push_back(std::move(total));
  MenuEntry models;
  models.kind = MenuEntry::Parent;
  models.label = L"模型";
  models.sub = 2;
  menu_.entries.push_back(std::move(models));
  menu_.layout(d3d_);
  // 朝屏内级联：面板在左（dock 右缘）→ 向右展开；面板在右 → 向左（箭头随向）
  menuDir_ = cfg_.edge == "right" ? 1 : -1;
  menu_.subDir = menuDir_;
  menu_.slot = slot;
  menu_.open = true;
  menu_.hover = -1;
  menu_.parent1 = menu_.parent2 = -1;
  menu_.sub1.clear();
  menu_.sub2.clear();
  placeSettingsMenu();
  menuOpenQpc_ = qpcNow();
  // 沿检测基准：打开当帧若键已按下（如左键尚未松开）不误判为收起点击
  prevEsc_ = (GetAsyncKeyState(VK_ESCAPE) & 0x8000) != 0;
  prevLmb_ = (GetAsyncKeyState(VK_LBUTTON) & 0x8000) != 0;
  prevRmb_ = (GetAsyncKeyState(VK_RBUTTON) & 0x8000) != 0;
  applyWindowPos();   // 菜单区纳入窗口（与面板区并集扩窗 → WM_SIZE → render）
  syncClickThru();    // 菜单区纳入可命中（面板外的菜单区不再穿透）
  render();
}

// 激活：映射项 → cfg.mapping[slot] → saveConfig → rebuild 立即生效（设置面板的
// 级联下拉改 draft）；动作项分发。动作项先 closeMenu；映射叶项由 applyMenuMapping
// 收尾（它要先取 menuForSettings_ 再 closeMenu，不能在此提前收）
void DockApp::activateMenu(int idx) {
  const int col = idx / 1000, i = idx % 1000;
  if (col == 0) {
    if (i < 0 || i >= (int)menu_.entries.size()) return;
    const MenuEntry& e = menu_.entries[(size_t)i];
    if (e.kind == MenuEntry::Parent) { openSub1(i, e.sub); return; }  // 总量▸/模型▸
    const int action = e.action;
    const std::string v = e.value;
    if (action == 1) { closeMenu(); openSettings(); return; }
    if (action == 2) { closeMenu(); flipEdge(); return; }
    if (action == 3) { closeMenu(); exitApp(); return; }
    applyMenuMapping(v);
    return;
  }
  if (col == 1) {
    if (i < 0 || i >= (int)menu_.sub1.entries.size()) return;
    const MenuEntry& e = menu_.sub1.entries[(size_t)i];
    if (e.kind == MenuEntry::Parent) { openSub2(i, e.value); return; }  // 厂商▸
    applyMenuMapping(e.value);
    return;
  }
  if (col == 2) {
    if (i < 0 || i >= (int)menu_.sub2.entries.size()) return;
    applyMenuMapping(menu_.sub2.entries[(size_t)i].value);
    return;
  }
}

// 叶项映射落盘：mapping → saveConfig → rebuild 立即生效；设置面板的级联下拉只改
// draft（两段式保存，保存并生效时统一落盘）。
// v 必须按值传入：调用方的 e.value 是对 entries 向量的引用，closeMenu 会
// clear 子列向量使引用悬空（实测 v 变成 empty → 守卫早退 → 切换静默无效）
void DockApp::applyMenuMapping(const std::string v) {
  const int slot = menu_.slot;
  const bool forSettings = menuForSettings_;
  closeMenu();
  if (v.empty() || slot < 0) return;
  if (forSettings) {
    if (slot < (int)settings_.draft.mapping.size())
      settings_.draft.mapping[(size_t)slot] = v;  // 草稿：gsel 文本随下次 render 更新
    render();
    return;
  }
  if (slot >= cfg_.count) return;
  cfg_.mapping[(size_t)slot] = v;
  if (!saveConfig(okmeterDir(), cfg_)) {
    backdrop_.note(L"applyMenuMapping: saveConfig FAILED gle=%lu", GetLastError());
  }
  rebuildItems();
  render();
}

double DockApp::menuAnimT() const {
  if (!menu_.open || menuOpenQpc_.QuadPart == 0) return 1.0;
  const double t = qpcSeconds(menuOpenQpc_, qpcNow()) / kMenuAnimSec;
  return t >= 1.0 ? 1.0 : easeDock(t);
}

double DockApp::panelAnimT() const {
  if (!settings_.open || panelOpenQpc_.QuadPart == 0) return 1.0;
  const double t = qpcSeconds(panelOpenQpc_, qpcNow()) / kPanelAnimSec;
  return t >= 1.0 ? 1.0 : easeDock(t);
}

double DockApp::cardAnimT() const {
  if (!card_.valid || cardShownQpc_.QuadPart == 0) return 1.0;
  const double t = qpcSeconds(cardShownQpc_, qpcNow()) / kCardAnimSec;
  return t >= 1.0 ? 1.0 : easeDock(t);
}

// ── 按需渲染：静止定义 = 无任何随时间自变的视觉元素且无进行中动画 ──
// 4 材质 × 3 形态逐一核对：dark/frost/liquid 指针光/高光为即时态（无时间缓动，
// wantsTick 默认 false）；glow 粒子/柔光/气态漂移常驻（wantsTick 常真）→ 常帧；
// arc/capsule 无形态动画；罗盘收缩态 3.6°/s 旋转（e<0.999 时 wantsTick 真）→ 常帧，
// 展开静止。菜单/面板打开期需常帧：Escape/窗外点击沿检测在 animTick（NOACTIVATE
// 窗口收不到键盘/窗外点击消息）。拖拽为事件驱动（WM_MOUSEMOVE 内渲染），不计入。
bool DockApp::needsFrames() const {
  if (!emerged_) return true;                          // 弹簧未稳
  if (material_ && material_->wantsTick()) return true;  // glow 常驻动画
  if (form_ && form_->wantsTick()) return true;          // 罗盘收缩态旋转
  if (menu_.open) return true;                         // 弹出动画 + 沿检测
  if (settings_.open) return true;                     // 滑入动画 + Escape 沿检测
  if (card_.valid && cardAnimT() < 1.0) return true;     // cardin 140ms
  if (backdrop_.ok() && backdrop_.dirty()) return true;  // 新背景帧到达（切窗/壁纸变）→ 唤醒一帧
  return false;
}

void DockApp::startFrames() {
  if (framesOn_ || !hwnd_) return;
  framesOn_ = true;
  lastTickQpc_ = LARGE_INTEGER{};  // 停摆期墙钟不计入：重启首帧 dt 按 16ms，弹簧/罗盘不跳变
  if (animTimer_) {
    LARGE_INTEGER due{};
    due.QuadPart = -160000LL;  // 16ms（相对，100ns 单位）
    (void)SetWaitableTimerEx(animTimer_, &due, 0, nullptr, nullptr, nullptr, 0);
  } else {
    SetTimer(hwnd_, kTimerAnim, 16, nullptr);
  }
}

void DockApp::stopFrames() {
  if (!framesOn_) return;
  framesOn_ = false;
  if (animTimer_)
    (void)CancelWaitableTimer(animTimer_);  // manual-reset：取消并复位信号态，防空转
  else if (hwnd_)
    KillTimer(hwnd_, kTimerAnim);
}

void DockApp::syncFrames() {
  if (needsFrames()) startFrames(); else stopFrames();
}

// 打开设置面板：draft=cfg 副本 + 停靠 dock 对侧屏缘
//（原型 openSettings：顶 14 底 58 边距 14，classList left=edge==right）；
// 并集扩窗 + 保持展开（holdOpen）；面板皮肤跟随当前生效材质（草稿不即时换肤）
void DockApp::openSettings() {
  if (settings_.open) return;
  backdrop_.note(L"openSettings 入口（诊断消息触发或菜单）");
  closeMenu();
  settings_.begin(cfg_);
  settings_.layout(d3d_);
  const RECT work = workArea();
  const int h = (int)(work.bottom - work.top) - 14 - 58;
  const int x = cfg_.edge == "right" ? work.left + 14
                                     : work.right - 14 - kSettingsPanelW;
  panelScreen_ = RECT{ x, work.top + 14, x + kSettingsPanelW, work.top + 14 + h };
  panelOpenQpc_ = qpcNow();
  prevEsc_ = (GetAsyncKeyState(VK_ESCAPE) & 0x8000) != 0;  // 沿检测基准（同菜单）
  KillTimer(hwnd_, kTimerRetract);
  setEmergeTarget(1);   // 面板期间保持展开（原型 holdOpen）
  applyWindowPos();     // 面板区纳入窗口（并集扩窗 → WM_SIZE → render）
  syncClickThru();      // 初始穿透状态按当前指针位置定
  render();
}

// 关闭设置面板：apply=true 两段式保存（draft → normalize → saveConfig →
// createModules 重建形态/材质模块 → rebuildItems/rebuildLayout 全量生效）；
// false 丢弃草稿。面板关闭后窗口收回基础矩形，指针已在窗外则恢复 600ms 迟滞
void DockApp::closeSettings(bool apply) {
  if (!settings_.open) return;
  closeMenu();  // 指标级联下拉随面板一并收起
  if (apply) {
    cfg_ = settings_.draft;
    cfg_.normalize();
    saveConfig(okmeterDir(), cfg_);
    createModules();        // 形态/材质模块重建（材质换肤/形态切换同源）
    hoverIdx_ = -1;         // 球数可能变少，悬停下标作废
    pressIdx_ = -1;
    rebuildItems();         // bindings/文本/详情卡同源更新
  }
  settings_.open = false;
  if (clickThru_) syncClickThru();  // 恢复窗口可命中（关面板后不再需要穿透）
  rebuildLayout();  // edge/count 可能变化 → 几何重建（applyWindowPos 收回基础矩形）
  POINT pt{};
  GetCursorPos(&pt);
  RECT wr{};
  GetWindowRect(hwnd_, &wr);
  if (cfg_.pinned) {
    setEmergeTarget(1);  // 保持显示：关面板后仍常显展开
  } else if (!PtInRect(&wr, pt)) {  // 指针已在窗外：恢复 600ms 迟滞收回
    KillTimer(hwnd_, kTimerRetract);
    SetTimer(hwnd_, kTimerRetract, 600, nullptr);
  }
  render();
}

void DockApp::activateSettings(int idx) {
  const SettingsPanel::Ctrl::Kind kind = settings_.ctrl(idx).kind;
  if (kind == SettingsPanel::Ctrl::CloseX || kind == SettingsPanel::Ctrl::CancelBtn) {
    closeSettings(false);
    return;
  }
  if (kind == SettingsPanel::Ctrl::SaveBtn) {
    closeSettings(true);
    return;
  }
  const int r = settings_.click(d3d_, idx);
  if (r == 3) {
    openSettingsMenu(idx);  // 指标下拉钮 → 级联映射菜单（右键 GlassMenu 同款）
    return;
  }
  if (r == 2) {
    settings_.layout(d3d_);  // 球数变化 → 映射行重排
    applyWindowPos();        // layout 重建 ctrls_（头尾按钮矩形清零）→ 重新落窗填充
  }
  if (r >= 1) render();
}

// 背景捕获接线：失败仅降级标记（backdrop.log），不影响 dock 本体
void DockApp::startCapture() {
  const auto dxgi = d3d_.dxgiDevice();
  if (!dxgi) return;
  (void)backdrop_.start(hwnd_, dxgi.Get());
}

void DockApp::renderOnce() {
  if (!d3d_.begin()) return;
  if (d3d_.generation() != backdropGen_) {
    // 设备丢失重建（end() 内 init 重入）后：用新 DXGI 设备重启捕获
    backdrop_.note(L"render: 代际 %u→%u（重建 #%u lastErr=0x%08lX removed=0x%08lX）→ 重启捕获",
                   backdropGen_, d3d_.generation(), d3d_.rebuilds(),
                   (unsigned long)d3d_.lastRebuildErr(),
                   (unsigned long)d3d_.lastRemovedReason());
    backdropGen_ = d3d_.generation();
    startCapture();
  }
#if defined(OKM_ANIM_DIAG)
  if (g_diag.active) g_diag.drawBegin = qpcNow();
#endif
  d3d_.dc()->Clear(D2D1::ColorF(0, 0.0f));  // 全透明底
  const RECT work = workArea();
  const int screenH = (int)(work.bottom - work.top);
  const int screenW = (int)(work.right - work.left);
  DockGeom g = form_->layout(cfg_.count, screenW, screenH, cfg_.edge);
  applyHover(g, hoverIdx_, form_->hoverScale(), form_->hoverPush(),
               form_->hoverDimShrink());
  // 菜单向上扩窗时球区整体下移 zoneDY_（卡绘制共用同一 g，锚定随动）
  if (zoneDY_ != 0)
    for (ItemGeom& it : g.items) it.y += zoneDY_;
  // 球区窗口内偏移：右缘宽窗 +268（卡区靠左贴球区）+ 菜单区让位（zoneDX_）
  const float dx = (float)zoneDX_;
  // 背景纹理→窗口坐标平移：tex(0,0)=捕获屏左上角；窗口左上角=GetWindowRect
  RECT wr{};
  GetWindowRect(hwnd_, &wr);
  int monX = 0, monY = 0;
  backdrop_.capOrigin(monX, monY);
  scene_.draw(d3d_, *form_, *material_, &backdrop_, g, items_,
              (cfg_.count - 1) / 2, emerge_.value, cfg_.edge, dx,
              (float)(monX - wr.left), (float)(monY - wr.top),
              material_->id() == "glow" ? pressIdx_ : -1);  // 按压下沉仅沉浸光感
  // 菜单打开期间不画详情卡：右键时悬停卡与菜单级联列叠加层级太乱
  if (card_.valid && !menu_.open && hoverIdx_ >= 0 && hoverIdx_ < (int)g.items.size() &&
      emerge_.value > 0.5) {
    // 垂直夹取基准用当前真实客户区高度（联合窗口下 winH_ 只是球区高度，
    // 菜单/设置面板扩窗后窗口更高——错用 winH_ 会把卡 clamp 到顶部）
    RECT cr{};
    GetClientRect(hwnd_, &cr);
    const double clientH = (double)(cr.bottom - cr.top);
    // cardin 出现动画 140ms（透明度 + 向屏缘 6px 滑入，原型 .detail cardin 同款）
    const double ct = cardAnimT();
    if (ct < 1.0) {
      const float off =
          (float)((1.0 - ct) * 6.0) * (cfg_.edge == "right" ? 1.0f : -1.0f);
      ID2D1DeviceContext* dc = d3d_.dc();
      dc->SetTransform(D2D1::Matrix3x2F::Translation(off, 0.0f));
      const D2D1_LAYER_PARAMETERS lp = D2D1::LayerParameters(
          D2D1::InfiniteRect(), nullptr, D2D1_ANTIALIAS_MODE_PER_PRIMITIVE,
          D2D1::IdentityMatrix(), (float)ct);
      dc->PushLayer(&lp, nullptr);
      scene_.drawCard(d3d_, *material_, card_, cfg_.edge, g, dx, clientH,
                      hoverIdx_, form_->cardRadius(hoverIdx_, (cfg_.count - 1) / 2));
      dc->PopLayer();
      dc->SetTransform(D2D1::IdentityMatrix());
    } else {
      scene_.drawCard(d3d_, *material_, card_, cfg_.edge, g, dx, clientH,
                      hoverIdx_, form_->cardRadius(hoverIdx_, (cfg_.count - 1) / 2));
    }
  }
  // 背板设置面板：滑入动画 180ms（透明度 + 自 dock 侧 10px 滑入，原型 panelin 同款）；
  // 面板底由 material.drawCardBack 供皮（跟随当前生效材质，草稿不即时换肤）。
  // 面板先于菜单绘制：设置面板的指标级联下拉锚在面板区域内，菜单必须压面板顶层
  if (settings_.open) {
    const double t = panelAnimT();
    if (t < 1.0) {
      const float off =
          (float)((1.0 - t) * 10.0) * (cfg_.edge == "right" ? 1.0f : -1.0f);
      ID2D1DeviceContext* dc = d3d_.dc();
      dc->SetTransform(D2D1::Matrix3x2F::Translation(off, 0.0f));
      const D2D1_LAYER_PARAMETERS lp = D2D1::LayerParameters(
          D2D1::InfiniteRect(), nullptr, D2D1_ANTIALIAS_MODE_PER_PRIMITIVE,
          D2D1::IdentityMatrix(), (float)t);
      dc->PushLayer(&lp, nullptr);
      settings_.draw(d3d_, *material_);
      dc->PopLayer();
      dc->SetTransform(D2D1::IdentityMatrix());
    } else {
      settings_.draw(d3d_, *material_);
    }
  }
  // 自绘玻璃菜单（球区右键 / 设置面板级联下拉共用）：弹出动画 120ms（透明度 +
  // 向级联方向反向 6px 滑入，原型 cardin 同款）；无模态泵，动画帧照常驱动
  if (menu_.open) {
    const double t = menuAnimT();
    if (t < 1.0) {
      const float off =
          (float)((1.0 - t) * 6.0) * (menuDir_ < 0 ? 1.0f : -1.0f);
      ID2D1DeviceContext* dc = d3d_.dc();
      dc->SetTransform(D2D1::Matrix3x2F::Translation(off, 0.0f));
      const D2D1_LAYER_PARAMETERS lp = D2D1::LayerParameters(
          D2D1::InfiniteRect(), nullptr, D2D1_ANTIALIAS_MODE_PER_PRIMITIVE,
          D2D1::IdentityMatrix(), (float)t);
      dc->PushLayer(&lp, nullptr);
      menu_.draw(d3d_, *material_);
      dc->PopLayer();
      dc->SetTransform(D2D1::IdentityMatrix());
    } else {
      menu_.draw(d3d_, *material_);
    }
  }
#if defined(OKM_ANIM_DIAG)
  const LARGE_INTEGER drawEnd = qpcNow();
#endif
  const bool presented = d3d_.end();
#if defined(OKM_ANIM_DIAG)
  if (g_diag.active) {
    g_diag.framesDrawMs_ = g_diag.ms(g_diag.drawBegin, drawEnd);
  }
#endif
  (void)presented;  // 掉帧判定由外层 render() 经 rebuilds 计数完成
}

// 掉帧补呈：resize/设备重建丢帧后若无人补画，停摆期窗口滞留黑色（"设置开两次/
// 切程序黑边"根因）。有界重试 3 次，仍败则等下一事件帧。
void DockApp::render() {
  for (int attempt = 0; attempt < 3; ++attempt) {
    const unsigned before = d3d_.rebuilds();
    renderOnce();
    if (d3d_.rebuilds() == before) return;  // 无重建：要么已呈现要么硬失败，不重试
    if (!d3d_.ok()) return;                 // 重建失败，等下个事件
  }
}

// 动画帧 tick：HR 可等待定时器（主路径）或 16ms WM_TIMER（回退）驱动，按需启停
//（syncFrames；静止期帧时钟停摆，本函数不运行）。
// RDCW 目录监听检查 + 弹簧 step（真实 elapsed dt）+ 挪窗 + 重绘。
void DockApp::animTick() {
  // 真实 elapsed dt：每个 tick（含静止 tick）刷新采样点，弹簧按实际墙钟推进；
  // step 内部钳 0.05s 上限防卡顿爆炸。
  const LARGE_INTEGER qpc = qpcNow();
  const double dt = lastTickQpc_.QuadPart == 0
      ? 0.016 : qpcSeconds(lastTickQpc_, qpc);
  lastTickQpc_ = qpc;
  if (form_) form_->tick(dt, emerge_.value);  // 形态动画（罗盘收缩态旋转）
  // RDCW 目录监听：每 500ms 检查一次，触发则提前 poll（与 2s 轮询同路径）
  const int64_t now = nowMs();
  if (now - lastWatchMs_ >= 500) {
    lastWatchMs_ = now;
    bool hit = false;  // 每个 watcher 都要调 signaled（内含复位+重投），不能短路
    for (auto& w : watchers_) hit = w->signaled() || hit;
    if (hit) pollData();
  }
  // 菜单打开期间：Escape 收起；窗外点击收起（NOACTIVATE 窗口收不到 WM_KEYDOWN 与
  // 窗外点击，动画帧里 GetAsyncKeyState 沿检测兜底；窗内点击走消息处理同效收起）
  if (menu_.open) {
    const bool esc = (GetAsyncKeyState(VK_ESCAPE) & 0x8000) != 0;
    const bool lmb = (GetAsyncKeyState(VK_LBUTTON) & 0x8000) != 0;
    const bool rmb = (GetAsyncKeyState(VK_RBUTTON) & 0x8000) != 0;
    if (esc && !prevEsc_) closeMenu();
    else if ((lmb && !prevLmb_) || (rmb && !prevRmb_)) {
      POINT pt{};
      GetCursorPos(&pt);
      if (!PtInRect(&menuScreen_, pt)) closeMenu();
    }
    prevEsc_ = esc;
    prevLmb_ = lmb;
    prevRmb_ = rmb;
  }
  // 设置面板打开期间：Escape 丢弃草稿关闭（NOACTIVATE 窗口收不到 WM_KEYDOWN，
  // 动画帧里 GetAsyncKeyState 沿检测，同菜单通路）
  if (settings_.open) {
    const bool esc = (GetAsyncKeyState(VK_ESCAPE) & 0x8000) != 0;
    if (esc && !prevEsc_) closeSettings(false);
    prevEsc_ = esc;
    syncClickThru();  // 按指针位置动态穿透（帧时钟每帧轮询，不依赖鼠标消息）
  }
  if (emerged_) {
    // 弹簧静止后：材质仍有进行中的光效动画（粒子/柔光/光晕）或形态仍有持续
    // 动画（罗盘收缩态旋转）或菜单弹出动画未播完时持续重绘；面板滑入动画与
    // 详情卡 cardin 出现动画同此驱动
    if ((material_ && material_->wantsTick()) || (form_ && form_->wantsTick()) ||
        (menu_.open && menuAnimT() < 1.0) ||
        (settings_.open && panelAnimT() < 1.0) ||
        (card_.valid && cardAnimT() < 1.0) ||
        (backdrop_.ok() && backdrop_.dirty()))
      render();
    // 兜底清脏：本帧无任何材质消费背景帧（如收缩细条不采样）时，防 dirty 常置
    // 导致帧时钟空转；与 onFrame 写脏竞争的最坏代价是少渲染一帧背景
    if (backdrop_.dirty()) backdrop_.markClean();
    syncFrames();  // 全部静止 → 停帧时钟（菜单/面板打开期沿检测需要 → 保持）
    return;
  }
#if defined(OKM_ANIM_DIAG)
  diagFrameBegin(emergeTarget_);
  const LARGE_INTEGER q0 = qpcNow();
#endif
  emerge_.step(dt, emergeTarget_);
#if defined(OKM_ANIM_DIAG)
  const LARGE_INTEGER q1 = qpcNow();
#endif
  if (emerge_.settled(emergeTarget_)) {
    emerge_.snap(emergeTarget_);
    emerged_ = true;
  }
  updatePosition();
#if defined(OKM_ANIM_DIAG)
  const LARGE_INTEGER q2 = qpcNow();
#endif
  render();
#if defined(OKM_ANIM_DIAG)
  const LARGE_INTEGER q3 = qpcNow();
  diagFrameEnd(g_diag.ms(q0, q1), g_diag.ms(q1, q2),
               g_diag.framesDrawMs_, g_diag.ms(q2, q3) - g_diag.framesDrawMs_);
  if (emerged_) diagFlush();
#endif
  syncFrames();  // 弹簧落定且无余下动画 → 停帧时钟
}

LRESULT DockApp::onMessage(UINT msg, WPARAM wp, LPARAM lp) {
  const LRESULT r = dispatchMessage(msg, wp, lp);
  // 任何事件都可能翻转动画状态（弹簧目标/菜单/面板/卡片/材质切换）→ 按需启停帧时钟
  if (msg != WM_DESTROY) syncFrames();
  return r;
}

LRESULT DockApp::dispatchMessage(UINT msg, WPARAM wp, LPARAM lp) {
  switch (msg) {
  case WM_DESTROY:
    if (sessionNotif_) {
      WTSUnRegisterSessionNotification(hwnd_);
      sessionNotif_ = false;
    }
    backdrop_.stop();
    PostQuitMessage(0);
    return 0;
  case WM_PAINT:
    ValidateRect(hwnd_, nullptr);
    render();
    return 0;
  case WM_SIZE:
    d3d_.resize((int)LOWORD(lp), (int)HIWORD(lp));
    render();
    return 0;
  case WM_TIMER:
    switch (wp) {
    case kTimerAnim:
      animTick();  // 回退路径（HR 定时器不可用时）
      return 0;
    case kTimerPoll:
      // RDCW 健康：事件已即时 poll，2s tick 退避为 30s 兜底（防 RDCW 缓冲溢出丢事件；
      // 全量枚举 1095 个 wire.jsonl 实测 ~180ms/次，是静置 CPU 主源）。监听失效不退避。
      if (!watchActive_ || nowMs() - lastPollMs_ >= 30000) pollData();
      return 0;
    case kTimerRel:
      rebuildItems();  // 仅刷新文本缓存（口径随 now 变化）
      render();
      return 0;
    case kTimerRetract:
      KillTimer(hwnd_, kTimerRetract);
      if (!cfg_.pinned) setEmergeTarget(0);  // 保持显示：收回禁用
      return 0;
    case kTimerShot: {
      KillTimer(hwnd_, kTimerShot);
      if (shotCollapsed_) {
        // --shotcap 自检：静止在收缩终态（e=0，无悬停），露出条球帽+细边落盘
        hoverIdx_ = -1;
        emerge_.snap(0);
        emergeTarget_ = 0;
        emerged_ = false;
        updatePosition();
        render();
        render();   // resize 首帧可能 RECREATE_TARGET 被丢弃
        render();   // FLIP_SEQUENTIAL 双缓冲：saveFrame 读倒数第二帧
        (void)d3d_.saveFrame(shotPath_);
        DestroyWindow(hwnd_);
        return 0;
      }
      if (shotSettings_) {
        // --shotsettings 自检：强制展开 + 打开背板设置面板（滑入动画播完后落盘）
        hoverIdx_ = -1;
        emerge_.snap(1);
        emergeTarget_ = 1;
        emerged_ = true;
        setWide(true);
        updatePosition();
        openSettings();  // 内部并集扩窗 + render
        if (shotDropSlot_ >= 0 && shotDropSlot_ < 1000) {
          const int gi = settings_.gselCtrl(shotDropSlot_);
          if (gi >= 0) activateSettings(gi);  // 打开该槽位的指标级联映射菜单
        }
        if (shotDropSlot_ >= 1000) {
          // --shotcount 自检：走 activateSettings 真实路径点球数 chip（值为球数）
          const int ci = settings_.countChipCtrl(shotDropSlot_ - 1000);
          if (ci >= 0) activateSettings(ci);
        }
        // 跟手光落面板中部：玻璃面板高光/光感在截图里可见
        RECT wr0{};
        GetWindowRect(hwnd_, &wr0);
        material_->onPointer((float)(panelScreen_.left - wr0.left) + 130.0f,
                             (float)(panelScreen_.top - wr0.top) + 220.0f);
        render();
        render();   // 扩窗 resize 首帧可能 EndDraw RECREATE_TARGET 被丢弃
        Sleep(260); // 180ms 滑入动画播完（落盘帧取满透明度）
        render();
        render();   // FLIP_SEQUENTIAL 双缓冲：saveFrame 读 GetBuffer(0)=倒数第二帧
        (void)d3d_.saveFrame(shotPath_);
        DestroyWindow(hwnd_);
        return 0;
      }
      // 自检截图：直接静止在展开终态（悬停中心球），不等弹簧动画
      hoverIdx_ = (cfg_.count - 1) / 2;
      emerge_.snap(1);
      emergeTarget_ = 1;
      emerged_ = true;
      setWide(true);
      updatePosition();
      // 模拟指针在悬停球左上方：跟手光/镜面高光/光感汇聚在截图里可见
      {
        const RECT work = workArea();
        const int screenH = (int)(work.bottom - work.top);
        const int screenW = (int)(work.right - work.left);
        const DockGeom g = form_->layout(cfg_.count, screenW, screenH, cfg_.edge);
        if (hoverIdx_ >= 0 && hoverIdx_ < (int)g.items.size()) {
          const ItemGeom& it = g.items[(size_t)hoverIdx_];
          const float dx = (float)zoneDX_;  // setWide(true) 已在 applyWindowPos 里置位
          material_->onPointer((float)it.x + dx - 55.0f, (float)it.y - 70.0f);
          // glow 附加模拟按压：scale .9 下沉 + 扩散环起点帧（按压光晕自检）
          if (material_->id() == "glow") {
            pressIdx_ = hoverIdx_;
            material_->onPress((float)it.x + dx, (float)it.y);
          }
        }
      }
      rebuildCard();
      render();
      render();  // setWide 触发的 resize 可能让首帧 EndDraw 返回 RECREATE_TARGET
                 // 被丢弃（end 内重建设备），第二帧才落到新设备 back buffer
      Sleep(240);  // glow 按压环推进到中段（scale≈1.1，越出球缘可见）
      render();    // 第三帧：glow 柔光/粒子/按压环经 wantsTick 平滑到位后稳定
      render();    // 第四帧：FLIP_SEQUENTIAL 下 saveFrame 读倒数第二帧——
                   // cardin 140ms 已在 Sleep 内播完，末两帧须同为卡满透明度终态
      if (shotMenuSlot_ != -2) {
        // --shotmenu 自检：在目标球中心（-1=空白区）打开自绘菜单后落盘
        int cx = (int)(dockW_ * 0.5) + zoneDX_;
        int cy = winH_ / 2;
        if (shotMenuSlot_ >= 0) {
          const RECT work = workArea();
          const int screenH = (int)(work.bottom - work.top);
          const int screenW = (int)(work.right - work.left);
          const DockGeom g = form_->layout(cfg_.count, screenW, screenH, cfg_.edge);
          if (shotMenuSlot_ < (int)g.items.size()) {
            cx = (int)g.items[(size_t)shotMenuSlot_].x + zoneDX_;
            cy = (int)g.items[(size_t)shotMenuSlot_].y;
          }
        }
        openMenu(cx, cy, shotMenuSlot_);
        // 级联自检：shotDropSlot_ ≥1 时展开二级（1=总量 2=模型），==2 再展开首个厂商三级
        if (shotDropSlot_ >= 1 && menu_.slot >= 0) {
          for (int i = 0; i < (int)menu_.entries.size(); ++i)
            if (menu_.entries[(size_t)i].kind == MenuEntry::Parent &&
                menu_.entries[(size_t)i].sub == shotDropSlot_) {
              openSub1(i, shotDropSlot_);
              if (shotDropSlot_ == 2 && !menu_.sub1.entries.empty())
                openSub2(0, menu_.sub1.entries[0].value);
              break;
            }
        }
        render();  // openMenu 内 applyWindowPos 扩窗的 resize 首帧可能被丢弃
        Sleep(200);  // 弹出动画 120ms 播完（落盘帧取满透明度）
        render();
        render();  // FLIP_SEQUENTIAL 双缓冲：saveFrame 读 GetBuffer(0)=倒数第二帧，
                   // 末两帧须同为动画终态
      }
      (void)d3d_.saveFrame(shotPath_);
      DestroyWindow(hwnd_);
      return 0;
    }
    }
    return 0;
  case WM_MOUSEMOVE: {
    // 拖拽（规格 §3.2）：越阈值后窗口实时跟随指针；拖动中玻璃背景采样随窗移动
    if (dragArmed_ && (wp & MK_LBUTTON)) {
      POINT pt{ (int)(short)LOWORD(lp), (int)(short)HIWORD(lp) };
      ClientToScreen(hwnd_, &pt);
      if (!dragging_ &&
          std::abs(pt.x - dragStart_.x) + std::abs(pt.y - dragStart_.y) >
              kDragThreshold)
        dragging_ = true;
      if (dragging_) {
        SetWindowPos(hwnd_, nullptr, pt.x - dragGrab_.x, pt.y - dragGrab_.y, 0, 0,
                     SWP_NOSIZE | SWP_NOZORDER | SWP_NOACTIVATE);
        render();
        return 0;
      }
    }
    if (!trackingLeave_) {
      TRACKMOUSEEVENT tme{};
      tme.cbSize = sizeof(tme);
      tme.dwFlags = TME_LEAVE;
      tme.hwndTrack = hwnd_;
      TrackMouseEvent(&tme);
      trackingLeave_ = true;
      KillTimer(hwnd_, kTimerRetract);  // 进入取消迟滞收回
      setEmergeTarget(1);
    }
    material_->onPointer((float)(int)(short)LOWORD(lp),
                         (float)(int)(short)HIWORD(lp));  // 跟手光光源
    const int mx = (int)(short)LOWORD(lp), my = (int)(short)HIWORD(lp);
    const RECT work = workArea();
    const int screenH = (int)(work.bottom - work.top);
  const int screenW = (int)(work.right - work.left);
    DockGeom g = form_->layout(cfg_.count, screenW, screenH, cfg_.edge);
    applyHover(g, hoverIdx_, form_->hoverScale(), form_->hoverPush(),
               form_->hoverDimShrink());  // dy 参与命中
    const int idx = hitItem(g, mx, my - zoneDY_, (float)zoneDX_);
    if (idx != hoverIdx_) {
      hoverIdx_ = idx;
      rebuildCard();  // 切球即换卡内容
      render();
    }
    if (menu_.open) {
      const int mh = menu_.hit(mx, my);
      // hover 按列分发（-2/-1=列内不可点/菜单外 → 全清）
      int h0 = -1, h1 = -1, h2 = -1;
      if (mh >= 2000) h2 = mh - 2000;
      else if (mh >= 1000) h1 = mh - 1000;
      else if (mh >= 0) h0 = mh;
      if (h0 != menu_.hover || h1 != menu_.sub1.hover || h2 != menu_.sub2.hover) {
        menu_.hover = h0;
        menu_.sub1.hover = h1;
        menu_.sub2.hover = h2;
        render();
      }
      // 悬停父项即展开子列（Windows 菜单同款；悬停到别的父项自动切换）
      if (h0 >= 0 && h0 < (int)menu_.entries.size() &&
          menu_.entries[(size_t)h0].kind == MenuEntry::Parent &&
          menu_.parent1 != h0) {
        openSub1(h0, menu_.entries[(size_t)h0].sub);
      } else if (h1 >= 0 && h1 < (int)menu_.sub1.entries.size() &&
                 menu_.sub1.entries[(size_t)h1].kind == MenuEntry::Parent &&
                 menu_.parent2 != h1) {
        openSub2(h1, menu_.sub1.entries[(size_t)h1].value);
      }
    }
    if (settings_.open) {
      // 指针在级联下拉菜单上时清面板悬停（菜单压面板边缘，底下控件不高亮）
      const int sh =
          (menu_.open && menu_.contains(mx, my)) ? -1 : settings_.hit(mx, my);
      if (sh != settings_.hover) {
        settings_.hover = sh;
        render();
      }
    }
    return 0;
  }
  case WM_MOUSELEAVE:
    trackingLeave_ = false;
    material_->onPointerLeave();  // 光感熄灭/高光复位/粒子消散
    if (pressIdx_ != -1) { pressIdx_ = -1; render(); }
    if (settings_.open) {
      // 面板期间保持展开（原型 holdOpen）：不收球、不换卡，仅清面板悬停
      if (settings_.hover != -1) { settings_.hover = -1; render(); }
      return 0;
    }
    if (menu_.open) {
      // 菜单期间保持展开（原型 holdOpen）：不收球、不换卡，仅清菜单悬停
      if (menu_.hover != -1) { menu_.hover = -1; render(); }
      return 0;
    }
    if (hoverIdx_ != -1) {
      hoverIdx_ = -1;
      rebuildCard();  // 移出即隐
      render();
    }
    if (cfg_.pinned) return 0;  // 保持显示：不安排迟滞收回
    SetTimer(hwnd_, kTimerRetract, 600, nullptr);  // 600ms 迟滞后收回
    return 0;
  case WM_LBUTTONDOWN: {
    const int mx = (int)(short)LOWORD(lp), my = (int)(short)HIWORD(lp);
    // 菜单打开期间（球区右键菜单 / 设置面板指标级联下拉共用）：菜单内按下等
    // WM_LBUTTONUP 激活条目；菜单外按下收起——球区菜单吞掉该击（不触发按压/拖拽），
    // 设置下拉落在面板上的点击收起后继续生效（owner 下拉钮吞掉，防刚收即重开）
    if (menu_.open) {
      if (menu_.contains(mx, my)) return 0;
      const int owner = settingsMenuOwner_;  // closeMenu 会复位，先取
      closeMenu();
      if (!settings_.open) return 0;
      const int idx = settings_.hit(mx, my);
      if (idx < 0 || idx == owner) { render(); return 0; }
      activateSettings(idx);
      return 0;
    }
    // 设置面板打开期间：左键一律走面板命中（不按压/不起拖）
    if (settings_.open) {
      const int idx = settings_.hit(mx, my);
      if (idx >= 0) activateSettings(idx);
      return 0;
    }
    // 按压反馈（沉浸光感：scale .9 + 扩散环）与拖拽预备共存：
    // 阈值内视为按压/点击（按压光晕照常），越阈值进入拖拽（规格 §3.2）
    const RECT work = workArea();
    const int screenH = (int)(work.bottom - work.top);
  const int screenW = (int)(work.right - work.left);
    DockGeom g = form_->layout(cfg_.count, screenW, screenH, cfg_.edge);
    applyHover(g, hoverIdx_, form_->hoverScale(), form_->hoverPush(),
               form_->hoverDimShrink());
    const int idx = hitItem(g, (int)(short)LOWORD(lp),
                            (int)(short)HIWORD(lp) - zoneDY_, (float)zoneDX_);
    if (idx != -1) {
      pressIdx_ = idx;
      material_->onPress((float)(int)(short)LOWORD(lp),
                         (float)(int)(short)HIWORD(lp));
      render();
    }
    POINT pt{ (int)(short)LOWORD(lp), (int)(short)HIWORD(lp) };
    ClientToScreen(hwnd_, &pt);
    RECT wr{};
    GetWindowRect(hwnd_, &wr);
    dragStart_ = pt;
    dragGrab_.x = pt.x - wr.left;
    dragGrab_.y = pt.y - wr.top;
    dragArmed_ = true;
    dragging_ = false;
    SetCapture(hwnd_);
    return 0;
  }
  case WM_LBUTTONUP:
    // 菜单打开期间：松开落在可点条目上 → 激活（按下侧已在 WM_LBUTTONDOWN 吞掉/收起）
    if (menu_.open) {
      const int idx = menu_.hit((int)(short)LOWORD(lp), (int)(short)HIWORD(lp));
      if (idx >= 0) activateMenu(idx);
      return 0;
    }
    if (settings_.open) return 0;  // 面板期间：按下侧已吞/处理，松开无事
    if (dragArmed_) {
      // ReleaseCapture 会同步派发 WM_CAPTURECHANGED（其处理器复位拖拽状态），
      // 必须先取标志再释放
      const bool wasDragging = dragging_;
      dragArmed_ = false;
      dragging_ = false;
      ReleaseCapture();
      if (wasDragging) {
        // 松手落点判定（规格 §3.2）：窗口中心过半屏（所在屏工作区中线）即换边
        // + saveConfig；未过半屏/同侧松手则 rebuildLayout 吸附回原位
        RECT wr{};
        GetWindowRect(hwnd_, &wr);
        const POINT c{ (wr.left + wr.right) / 2, (wr.top + wr.bottom) / 2 };
        MONITORINFO mi{ sizeof(mi) };
        const BOOL miOk =
            GetMonitorInfoW(MonitorFromPoint(c, MONITOR_DEFAULTTONEAREST), &mi);
        if (miOk) {
          const std::string newEdge =
              c.x < (mi.rcWork.left + mi.rcWork.right) / 2 ? "left" : "right";
          if (newEdge != cfg_.edge) {
            cfg_.edge = newEdge;
            saveConfig(okmeterDir(), cfg_);
          }
        }
        rebuildLayout();
        render();
      }
    }
    if (pressIdx_ != -1) {
      pressIdx_ = -1;
      render();
    }
    return 0;
  case WM_RBUTTONUP: {
    // 设置面板打开期间吞掉右键（面板与菜单互斥，球上交互待面板关闭后恢复）
    if (settings_.open) return 0;
    // 自绘玻璃菜单（原生 TrackPopupMenu 已废除）：球上 = 映射组菜单，弧线/空白 =
    // 三项菜单；右键重复点击 = 原地重开（原生菜单同款行为）
    const int mx = (int)(short)LOWORD(lp), my = (int)(short)HIWORD(lp);
    const RECT work = workArea();
    const int screenH = (int)(work.bottom - work.top);
  const int screenW = (int)(work.right - work.left);
    DockGeom g = form_->layout(cfg_.count, screenW, screenH, cfg_.edge);
    applyHover(g, hoverIdx_, form_->hoverScale(), form_->hoverPush(),
               form_->hoverDimShrink());
    openMenu(mx, my, hitItem(g, mx, my - zoneDY_, (float)zoneDX_));
    return 0;
  }
  case WM_DISPLAYCHANGE:
    backdrop_.note(L"WM_DISPLAYCHANGE → 重建捕获");
    startCapture();   // 显示器拓扑/尺寸变化 → 重建捕获（HMONITOR/池尺寸）
    rebuildLayout();  // 屏高/DPI 变化 → 几何重建
    render();
    return 0;
  case WM_CAPTURECHANGED:
    dragArmed_ = false;  // 捕获被夺（菜单/系统等）→ 拖拽状态复位，防卡死
    dragging_ = false;
    return 0;
  case WM_NCHITTEST:
    // 设置面板打开时联合窗口横贯全屏：面板/级联菜单/dock 球区以外回 HTTRANSPARENT
    // 穿透，否则中部透明区挡住桌面点击（屏幕坐标 → 客户区判定）
    if (settings_.open) {
      POINT pt{ (int)(short)LOWORD(lp), (int)(short)HIWORD(lp) };
      if (menu_.open && PtInRect(&menuScreen_, pt))
        return DefWindowProcW(hwnd_, msg, wp, lp);  // 级联菜单区可命中
      ScreenToClient(hwnd_, &pt);
      const RECT dz{ zoneDX_, zoneDY_, zoneDX_ + dockW_, zoneDY_ + winH_ };
      if (!settings_.contains(pt.x, pt.y) && !PtInRect(&dz, pt))
        return HTTRANSPARENT;
    }
    return DefWindowProcW(hwnd_, msg, wp, lp);
  case WM_INPUT: {
    // 设置面板滚轮（NOACTIVATE 窗口收不到 WM_MOUSEWHEEL——滚轮消息发给焦点窗口；
    // run() 注册 RIDEV_INPUTSINK 原始输入后台收轮）：滚体区内容
    if (!settings_.open) return 0;
    RAWINPUT raw{};
    UINT size = sizeof(raw);
    if (GetRawInputData((HRAWINPUT)lp, RID_INPUT, &raw, &size,
                        sizeof(RAWINPUTHEADER)) != sizeof(raw))
      return 0;
    if (raw.header.dwType == RIM_TYPEMOUSE &&
        (raw.data.mouse.usButtonFlags & RI_MOUSE_WHEEL)) {
      POINT pt{};
      GetCursorPos(&pt);
      // 体区滚动即收级联下拉（锚定行随滚动移位，原型 closeDrop 同款）；
      // 收菜单会触发并集收窗，客户区坐标须在收窗后重取
      if (menuForSettings_ && !PtInRect(&menuScreen_, pt)) {
        closeMenu();
        GetCursorPos(&pt);
      }
      ScreenToClient(hwnd_, &pt);
      const int delta = (int)(short)raw.data.mouse.usButtonData;
      if (settings_.wheelAt(pt.x, pt.y, delta)) render();
    }
    return 0;
  }
  case WM_WTSSESSION_CHANGE:
    if (wp == WTS_SESSION_UNLOCK) {
      backdrop_.note(L"WTS_SESSION_UNLOCK → retry 重建捕获");
      backdrop_.retry();  // 锁屏/安全桌面期间捕获被中止 → 解锁重建
      render();
    }
    return 0;
  case WM_APP + 0x4C: {  // 在线诊断：转储当前 backbuffer + 布局状态到 okmeter 目录
    if (wp == 1) openSettings();        // 诊断便捷：wp=1 开设置面板（免菜单导航）
    else if (wp == 2) closeSettings(false);  // wp=2 丢弃关闭（配合复现"开两次黑边"）
    else if (wp == 3 || wp == 4) {      // wp=3/4：临时关/开截图排除（黑边取证用——
      using AffinityFn = BOOL(WINAPI*)(HWND, DWORD);  // 关掉后普通截图能拍到 dock 实际显示）
      const auto fn = reinterpret_cast<AffinityFn>(
          GetProcAddress(GetModuleHandleW(L"user32.dll"), "SetWindowDisplayAffinity"));
      if (fn) {
        const BOOL ok = fn(hwnd_, wp == 3 ? 0x0 : 0x11);  // WDA_NONE / WDA_EXCLUDEFROMCAPTURE
        backdrop_.note(L"诊断: affinity %s → %s", wp == 3 ? L"OFF" : L"ON",
                       ok ? L"ok" : L"FAIL");
      }
    }
    const std::wstring p = (okmeterDir() / L"dump-live.png").wstring();
    const bool okDump = d3d_.saveFrame(p);
    RECT wr2{};
    GetWindowRect(hwnd_, &wr2);
    backdrop_.note(
        L"DUMP ok=%d win=(%ld,%ld,%ld,%ld) zone=(%d,%d) wide=%d hover=%d emerge=%.2f "
        L"menu=%d settings=%d card=%d frames=%llu rebuilds=%u luma=%.2f",
        okDump ? 1 : 0, (long)wr2.left, (long)wr2.top, (long)wr2.right,
        (long)wr2.bottom, zoneDX_, zoneDY_, wide_ ? 1 : 0, hoverIdx_, emerge_.value,
        menu_.open ? 1 : 0, settings_.open ? 1 : 0, card_.valid ? 1 : 0,
        backdrop_.frameCount(), d3d_.rebuilds(), (double)backdrop_.luma());
    return 0;
  }
  case WM_DPICHANGED:
    rebuildLayout();
    render();
    return 0;
  default:
    return DefWindowProcW(hwnd_, msg, wp, lp);
  }
}

LRESULT CALLBACK DockApp::WndProc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp) {
  if (msg == WM_NCCREATE) {
    const auto* cs = reinterpret_cast<const CREATESTRUCTW*>(lp);
    SetWindowLongPtrW(hwnd, GWLP_USERDATA, (LONG_PTR)cs->lpCreateParams);
    return DefWindowProcW(hwnd, msg, wp, lp);
  }
  auto* self = reinterpret_cast<DockApp*>(GetWindowLongPtrW(hwnd, GWLP_USERDATA));
  if (!self) return DefWindowProcW(hwnd, msg, wp, lp);
  return self->onMessage(msg, wp, lp);
}

int DockApp::run(HINSTANCE inst, const std::wstring& shotPath, int shotMenuSlot,
                 bool shotSettings, int shotDropSlot, bool shotCollapsed) {
  shotPath_ = shotPath;
  shotMenuSlot_ = shotMenuSlot;
  shotSettings_ = shotSettings;
  shotDropSlot_ = shotDropSlot;
  shotCollapsed_ = shotCollapsed;
  // DPI 感知（失败忽略，按系统缩放继续）
  SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);

  WNDCLASSEXW wc{};
  wc.cbSize = sizeof(wc);
  wc.lpfnWndProc = &DockApp::WndProc;
  wc.hInstance = inst;
  wc.hCursor = LoadCursorW(nullptr, MAKEINTRESOURCEW(32512));  // IDC_ARROW
  wc.lpszClassName = kClassName;
  if (!RegisterClassExW(&wc)) return 1;

  // 数据接线（单线程：全部对象随 UI 线程生灭）
  store_ = std::make_unique<Store>(okmeterDir());
  store_->load();
  adapters_.push_back(std::make_unique<KimiAdapter>(kimiHome(), store_.get()));
  adapters_.push_back(std::make_unique<ClaudeAdapter>(claudeHome(), store_.get()));
  adapters_.push_back(std::make_unique<CodexAdapter>(codexHome(), store_.get()));
  adapters_.push_back(std::make_unique<QwenAdapter>(qwenHome(), store_.get()));
  adapters_.push_back(std::make_unique<ZcodeAdapter>(zcodeHome(), store_.get()));
  adapters_.push_back(std::make_unique<WorkbuddyAdapter>(workbuddyHome(), store_.get()));
  adapters_.push_back(std::make_unique<ReasonixAdapter>(reasonixHome(), store_.get()));
  adapters_.push_back(std::make_unique<HanakoAdapter>(hanakoHome(), store_.get()));
  adapters_.push_back(std::make_unique<DshAdapter>(dshHome(), store_.get()));
  loadConfig(okmeterDir(), cfg_);
  cfg_.normalize();
  createModules();  // 形态/材质注册表创建（布局与渲染都经 form_）

  // 目录变更监听：每个已存在的 agent 数据根一个 RDCW，触发即时 poll；
  // 全部失败/目录不存在则 2s 轮询维持原行为（不退避）
  for (const std::filesystem::path& root : { kimiHome() / "sessions",
                                             claudeHome() / "projects",
                                             codexHome(),
                                             qwenHome() / "projects",
                                             zcodeHome() / "cli" / "rollout",
                                             workbuddyHome() / "projects",
                                             reasonixHome(),
                                             userProfile() / ".reasonix",
                                             hanakoHome() / "logs",
                                             dshHome() / "storages" }) {
    std::error_code ec;
    if (!std::filesystem::exists(root, ec)) continue;
    auto w = std::make_unique<DirWatcher>();
    if (w->start(root)) watchers_.push_back(std::move(w));
  }
  watchActive_ = !watchers_.empty();

  // 初始窗口：收缩态（e=0，右缘露出 24px）；起始落主屏工作区（hwnd 未创建，
  // workArea() 回退 SPI_GETWORKAREA），多屏位置由后续拖拽/重建接管
  const RECT work = workArea();
  const int screenH = (int)(work.bottom - work.top);
  const int screenW = (int)(work.right - work.left);
  const DockGeom g = form_->layout(cfg_.count, screenW, screenH, cfg_.edge);
  int h = (int)g.h;
  if (h < 120) h = 120;
  dockW_ = (int)g.w;
  winH_ = h;
  winY_ = work.top + ((work.bottom - work.top) - h) / 2;
  const int x = cfg_.edge == "left" ? work.left + dockW_ / 2 - dockW_
                                    : work.right - dockW_ / 2;  // 收缩态露出左半

  hwnd_ = CreateWindowExW(WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE |
                              WS_EX_LAYERED,
                          kClassName, L"OkMeter", WS_POPUP,
                          x, winY_, dockW_, h, nullptr, nullptr, inst, this);
  if (!hwnd_) return 1;
  if (cfg_.pinned) setEmergeTarget(1);  // 保持显示：启动即常显展开（弹簧滑出）
  if (!d3d_.init(hwnd_, dockW_, h)) {
    DestroyWindow(hwnd_);
    return 2;
  }

  // 背景捕获管线（WGC）：affinity 排除自身 + 实时抓屏；失败仅降级不影响 dock
  backdrop_.note(L"init: 首次启动捕获");
  startCapture();
  backdropGen_ = d3d_.generation();
  // 会话解锁通知：锁屏/安全桌面中止捕获，解锁后重建
  sessionNotif_ = WTSRegisterSessionNotification(hwnd_, NOTIFY_FOR_THIS_SESSION) != FALSE;
  // 设置面板滚轮通路：NOACTIVATE 窗口收不到 WM_MOUSEWHEEL（滚轮消息发给焦点窗口），
  // 注册原始输入（RIDEV_INPUTSINK）后台收鼠标滚轮 → WM_INPUT 里滚体区/浮层
  {
    RAWINPUTDEVICE rid{};
    rid.usUsagePage = 0x01;  // HID_USAGE_PAGE_GENERIC
    rid.usUsage = 0x02;      // HID_USAGE_GENERIC_MOUSE
    rid.dwFlags = RIDEV_INPUTSINK;
    rid.hwndTarget = hwnd_;
    (void)RegisterRawInputDevices(&rid, 1, sizeof(rid));
  }

  // 首轮数据：启动即有真实值（不等第一个 2s 轮询）
  pollData();

  ShowWindow(hwnd_, SW_SHOWNOACTIVATE);
  applyWindowPos();  // 落窗 + 设亮度采样区（init 直建窗口不经 applyWindowPos，缺这步
                     // 采样区恒为空 → 退化为全屏采样，亮底自适应永不触发）
  render();

  // 动画时钟：高分辨率可等待定时器（~0.5ms 粒度；实测 timeBeginPeriod(1) 对本
  // 进程无效——Win11 对非前台窗口压制计时器粒度请求，16ms WM_TIMER 仍量化到
  // 15.6ms 阶梯）。失败回退 16ms WM_TIMER。按需启停（syncFrames）：先武装一次
  // 验证可用性即取消，随后由 syncFrames 按 needsFrames 决定是否真正起帧。
  animTimer_ = CreateWaitableTimerExW(nullptr, nullptr,
      CREATE_WAITABLE_TIMER_MANUAL_RESET | CREATE_WAITABLE_TIMER_HIGH_RESOLUTION,
      TIMER_MODIFY_STATE | SYNCHRONIZE);
  LARGE_INTEGER due{};
  due.QuadPart = -160000LL;  // 16ms（相对，100ns 单位）
  if (animTimer_ &&
      !SetWaitableTimerEx(animTimer_, &due, 0, nullptr, nullptr, nullptr, 0)) {
    CloseHandle(animTimer_);
    animTimer_ = nullptr;
  }
  if (animTimer_) (void)CancelWaitableTimer(animTimer_);
  syncFrames();  // 起始按需：罗盘收缩态/glow 常驻动画立即起帧，静态组合停摆
  SetTimer(hwnd_, kTimerPoll, 2000, nullptr);
  SetTimer(hwnd_, kTimerRel, 30000, nullptr);
  if (!shotPath_.empty()) SetTimer(hwnd_, kTimerShot, 2500, nullptr);  // 等 WGC 帧流稳定

  MSG msg;
  if (!animTimer_) {
    while (GetMessageW(&msg, nullptr, 0, 0) > 0) {
      TranslateMessage(&msg);
      DispatchMessageW(&msg);
    }
  } else {
    // 等待集 = 动画定时器 + 各 RDCW 完成事件：帧时钟停摆期目录变更仍能即时唤醒 poll
    HANDLE waitHandles[12];
    DWORD nWait = 0;
    waitHandles[nWait++] = animTimer_;
    for (auto& w : watchers_)
      if (w->eventHandle() && nWait < 12) waitHandles[nWait++] = w->eventHandle();
    for (;;) {
      MsgWaitForMultipleObjectsEx(nWait, waitHandles, INFINITE, QS_ALLINPUT,
                                  MWMO_INPUTAVAILABLE);
      if (WaitForSingleObject(animTimer_, 0) == WAIT_OBJECT_0) {
        if (framesOn_) {
          // 先重整相位再处理帧：帧间隔恒定 16ms，与单帧耗时解耦
          SetWaitableTimerEx(animTimer_, &due, 0, nullptr, nullptr, nullptr, 0);
          animTick();  // 内部 syncFrames 停帧时 CancelWaitableTimer 撤销本次重整
        } else {
          (void)CancelWaitableTimer(animTimer_);  // 防御：stopFrames 已取消，不应到达
        }
      }
      bool watchHit = false;  // 每个 watcher 都要调 signaled（内含复位+重投）
      for (auto& w : watchers_) watchHit = w->signaled() || watchHit;
      if (watchHit) pollData();  // RDCW 触发提前 poll
      bool quit = false;
      while (PeekMessageW(&msg, nullptr, 0, 0, PM_REMOVE)) {
        if (msg.message == WM_QUIT) { quit = true; break; }
        TranslateMessage(&msg);
        DispatchMessageW(&msg);
      }
      if (quit) break;
    }
    CloseHandle(animTimer_);
    animTimer_ = nullptr;
  }
  KillTimer(hwnd_, kTimerAnim);
  KillTimer(hwnd_, kTimerPoll);
  KillTimer(hwnd_, kTimerRel);
  KillTimer(hwnd_, kTimerRetract);
  return (int)msg.wParam;
}

} // namespace okmeter
