#include "app.h"
#include "../adapters/kimi/adapter.h"
#include "../core/fmt.h"
#include "../core/paths.h"
#include "../core/store.h"
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
constexpr int kCollapsedPx = 24;    // 收缩态露出宽度
constexpr int kCardZoneW = 268;     // 展开态卡区宽（卡 252 + 两侧边距）
constexpr UINT_PTR kTimerAnim = 1;    // 动画帧 16ms
constexpr UINT_PTR kTimerPoll = 2;    // 数据轮询 2000ms
constexpr UINT_PTR kTimerRel = 3;     // 相对时间/口径文本刷新 30s
constexpr UINT_PTR kTimerRetract = 4; // 离开迟滞 600ms（一次性）
constexpr UINT kMenuFlip = 1;         // 右键菜单：换边
constexpr UINT kMenuExit = 2;         // 右键菜单：退出
constexpr double kHoverScale = 1.34;
constexpr double kHoverPush = 10;
constexpr double kHoverHitY = 28;   // 悬停命中：按 y 最近项 < 28px

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

void DockApp::rebuildItems() {
  Aggregator& agg = store_->agg();
  bindings_ = resolveBindings(cfg_, agg);
  const int64_t now = nowMs();
  items_.assign(bindings_.size(), render::DockItem{});
  for (size_t i = 0; i < bindings_.size(); ++i) {
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
    items_[i].value = wide(fmtCompact(v));
  }
  rebuildCard();  // 数据/口径刷新后卡内容同源更新
}

// 悬停详情卡组装：hoverIdx<0 或越界 → 隐藏；模型/总量双模式（规格 §3.4）
void DockApp::rebuildCard() {
  card_ = render::DetailCard{};
  if (hoverIdx_ < 0 || hoverIdx_ >= (int)bindings_.size()) return;
  Aggregator& agg = store_->agg();
  const Binding& b = bindings_[(size_t)hoverIdx_];
  const int64_t now = nowMs();
  auto row = [&](const wchar_t* label, int64_t v) {
    card_.rows.emplace_back(label, wide(fmtExact(v)));
  };
  if (b.isModel) {
    const ModelStat* m = agg.model(b.modelId);
    if (!m) return;
    card_.title = wide(b.modelId);
    card_.big = wide(fmtExact(m->all.total()));
    row(L"今日", agg.modelToday(b.modelId, now).total());
    row(L"本周", agg.modelWeek(b.modelId, now).total());
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
      card_.rows.emplace_back(L"input",
          wide(fmtExact(input)) + L" · cache 命中 " + std::to_wstring(pct) + L"%");
      row(L"output", t.output);
    } else {
      row(L"常规 input", t.inputOther);
      row(L"cache 读", t.inputCacheRead);
      row(L"cache 新建", t.inputCacheCreation);
      row(L"output", t.output);
    }
  }
  card_.valid = true;
}

void DockApp::pollData() {
  Aggregator& agg = store_->agg();
  kimi_->poll([&](const UsageEvent& e) { agg.add(e); });
  store_->flush();
  rebuildItems();
  render();
}

void DockApp::rebuildLayout() {
  const int screenH = GetSystemMetrics(SM_CYSCREEN);
  const DockGeom g = layoutArc(cfg_.count, 30, 14, screenH, cfg_.edge);
  int h = (int)g.h;
  if (h < 120) h = 120;
  dockW_ = (int)g.w;
  winH_ = h;
  RECT work{};
  SystemParametersInfoW(SPI_GETWORKAREA, 0, &work, 0);
  winY_ = work.top + ((work.bottom - work.top) - h) / 2;
  SetWindowPos(hwnd_, nullptr, 0, winY_, dockW_ + (wide_ ? kCardZoneW : 0), h,
               SWP_NOMOVE | SWP_NOZORDER | SWP_NOACTIVATE);
  updatePosition();
}

void DockApp::updatePosition() {
  if (!hwnd_) return;
  const int screenW = GetSystemMetrics(SM_CXSCREEN);
  const double base = lerp((double)kCollapsedPx, (double)dockW_, emerge_.value);
  int x;
  if (cfg_.edge == "left")
    x = (int)std::lround(base - dockW_);  // 卡区在球区右侧，x 不变
  else
    x = screenW - (int)std::lround(base) - (wide_ ? kCardZoneW : 0);
  SetWindowPos(hwnd_, nullptr, x, winY_, 0, 0,
               SWP_NOSIZE | SWP_NOZORDER | SWP_NOACTIVATE);
}

// 展开/收缩切换窗口宽度（球区位置不动，卡区在屏内侧增减）
void DockApp::setWide(bool w) {
  if (wide_ == w || !hwnd_) return;
  wide_ = w;
  const int screenW = GetSystemMetrics(SM_CXSCREEN);
  const double base = lerp((double)kCollapsedPx, (double)dockW_, emerge_.value);
  int x;
  if (cfg_.edge == "left")
    x = (int)std::lround(base - dockW_);
  else
    x = screenW - (int)std::lround(base) - (wide_ ? kCardZoneW : 0);
  SetWindowPos(hwnd_, nullptr, x, winY_, dockW_ + (wide_ ? kCardZoneW : 0), winH_,
               SWP_NOZORDER | SWP_NOACTIVATE);  // WM_SIZE → d3d.resize + render
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

// 背景捕获接线：失败仅降级标记（backdrop.log），不影响 dock 本体
void DockApp::startCapture() {
  const auto dxgi = d3d_.dxgiDevice();
  if (!dxgi) return;
  (void)backdrop_.start(hwnd_, dxgi.Get());
}

void DockApp::render() {
  if (!d3d_.begin()) return;
  if (d3d_.generation() != backdropGen_) {
    // 设备丢失重建（end() 内 init 重入）后：用新 DXGI 设备重启捕获
    backdropGen_ = d3d_.generation();
    startCapture();
  }
#if defined(OKM_ANIM_DIAG)
  if (g_diag.active) g_diag.drawBegin = qpcNow();
#endif
  d3d_.dc()->Clear(D2D1::ColorF(0, 0.0f));  // 全透明底
  const int screenH = GetSystemMetrics(SM_CYSCREEN);
  DockGeom g = layoutArc(cfg_.count, 30, 14, screenH, cfg_.edge);
  applyHover(g, hoverIdx_, kHoverScale, kHoverPush);
  // 宽窗右缘：球区整体右移 268（卡区靠左贴球区）；左缘球区在窗口左端不动
  const float dx = (wide_ && cfg_.edge == "right") ? (float)kCardZoneW : 0.0f;
  scene_.draw(d3d_, g, items_, (cfg_.count - 1) / 2, emerge_.value, cfg_.edge, dx);
  if (card_.valid && hoverIdx_ >= 0 && hoverIdx_ < (int)g.items.size() &&
      emerge_.value > 0.5) {
    const ItemGeom& it = g.items[(size_t)hoverIdx_];
    scene_.drawCard(d3d_, card_, cfg_.edge, g.w, (double)winH_, it.y + it.dy);
  }
#if defined(OKM_ANIM_DIAG)
  const LARGE_INTEGER drawEnd = qpcNow();
#endif
  d3d_.end();
#if defined(OKM_ANIM_DIAG)
  if (g_diag.active) {
    g_diag.framesDrawMs_ = g_diag.ms(g_diag.drawBegin, drawEnd);
  }
#endif
}

// 动画帧 tick：HR 可等待定时器（主路径）或 16ms WM_TIMER（回退）驱动。
// RDCW 目录监听检查 + 弹簧 step（真实 elapsed dt）+ 挪窗 + 重绘。
void DockApp::animTick() {
  // 真实 elapsed dt：每个 tick（含静止 tick）刷新采样点，弹簧按实际墙钟推进；
  // step 内部钳 0.05s 上限防卡顿爆炸。
  const LARGE_INTEGER qpc = qpcNow();
  const double dt = lastTickQpc_.QuadPart == 0
      ? 0.016 : qpcSeconds(lastTickQpc_, qpc);
  lastTickQpc_ = qpc;
  // RDCW 目录监听：每 500ms 检查一次，触发则提前 poll（与 2s 轮询同路径）
  const int64_t now = nowMs();
  if (now - lastWatchMs_ >= 500) {
    lastWatchMs_ = now;
    if (watch_.signaled()) pollData();
  }
  if (emerged_) return;  // 静止：不 step 不重绘不挪窗
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
}

LRESULT DockApp::onMessage(UINT msg, WPARAM wp, LPARAM lp) {
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
      pollData();
      return 0;
    case kTimerRel:
      rebuildItems();  // 仅刷新文本缓存（口径随 now 变化）
      render();
      return 0;
    case kTimerRetract:
      KillTimer(hwnd_, kTimerRetract);
      setEmergeTarget(0);
      return 0;
    }
    return 0;
  case WM_MOUSEMOVE: {
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
    const int y = (int)(short)HIWORD(lp);
    const int screenH = GetSystemMetrics(SM_CYSCREEN);
    const DockGeom g = layoutArc(cfg_.count, 30, 14, screenH, cfg_.edge);
    int idx = -1;
    double best = kHoverHitY;
    for (size_t i = 0; i < g.items.size(); ++i) {
      const double d = std::abs((double)y - g.items[i].y);
      if (d < best) { best = d; idx = (int)i; }
    }
    if (idx != hoverIdx_) {
      hoverIdx_ = idx;
      rebuildCard();  // 切球即换卡内容
      render();
    }
    return 0;
  }
  case WM_MOUSELEAVE:
    trackingLeave_ = false;
    if (hoverIdx_ != -1) {
      hoverIdx_ = -1;
      rebuildCard();  // 移出即隐
      render();
    }
    SetTimer(hwnd_, kTimerRetract, 600, nullptr);  // 600ms 迟滞后收回
    return 0;
  case WM_RBUTTONUP: {
    HMENU menu = CreatePopupMenu();
    if (!menu) return 0;
    AppendMenuW(menu, MF_STRING, kMenuFlip, L"换边");
    AppendMenuW(menu, MF_STRING, kMenuExit, L"退出");
    POINT pt{(int)(short)LOWORD(lp), (int)(short)HIWORD(lp)};
    ClientToScreen(hwnd_, &pt);
    SetForegroundWindow(hwnd_);  // 无此调用菜单不自动消失
    const UINT cmd = TrackPopupMenu(menu, TPM_RETURNCMD | TPM_NONOTIFY,
                                    pt.x, pt.y, 0, hwnd_, nullptr);
    DestroyMenu(menu);
    if (cmd == kMenuFlip) flipEdge();        // 选空（0）无事发生
    else if (cmd == kMenuExit) exitApp();
    return 0;
  }
  case WM_DISPLAYCHANGE:
    startCapture();   // 显示器拓扑/尺寸变化 → 重建捕获（HMONITOR/池尺寸）
    rebuildLayout();  // 屏高/DPI 变化 → 几何重建
    render();
    return 0;
  case WM_WTSSESSION_CHANGE:
    if (wp == WTS_SESSION_UNLOCK) {
      backdrop_.retry();  // 锁屏/安全桌面期间捕获被中止 → 解锁重建
      render();
    }
    return 0;
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

int DockApp::run(HINSTANCE inst) {
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
  kimi_ = std::make_unique<KimiAdapter>(kimiHome(), store_.get());
  loadConfig(okmeterDir(), cfg_);
  cfg_.normalize();

  // 目录变更监听：RDCW 提前触发 poll；失败静默回落纯 2s 轮询
  watch_.start(kimiHome() / "sessions");

  // 初始窗口：收缩态（e=0，右缘露出 24px）
  const int screenH = GetSystemMetrics(SM_CYSCREEN);
  const DockGeom g = layoutArc(cfg_.count, 30, 14, screenH, cfg_.edge);
  int h = (int)g.h;
  if (h < 120) h = 120;
  dockW_ = (int)g.w;
  winH_ = h;
  RECT work{};
  SystemParametersInfoW(SPI_GETWORKAREA, 0, &work, 0);
  winY_ = work.top + ((work.bottom - work.top) - h) / 2;
  const int screenW = GetSystemMetrics(SM_CXSCREEN);
  const int x = cfg_.edge == "left" ? kCollapsedPx - dockW_ : screenW - kCollapsedPx;

  hwnd_ = CreateWindowExW(WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE,
                          kClassName, L"OkMeter", WS_POPUP,
                          x, winY_, dockW_, h, nullptr, nullptr, inst, this);
  if (!hwnd_) return 1;
  if (!d3d_.init(hwnd_, dockW_, h)) {
    DestroyWindow(hwnd_);
    return 2;
  }

  // 背景捕获管线（WGC）：affinity 排除自身 + 实时抓屏；失败仅降级不影响 dock
  startCapture();
  backdropGen_ = d3d_.generation();
  // 会话解锁通知：锁屏/安全桌面中止捕获，解锁后重建
  sessionNotif_ = WTSRegisterSessionNotification(hwnd_, NOTIFY_FOR_THIS_SESSION) != FALSE;

  // 首轮数据：启动即有真实值（不等第一个 2s 轮询）
  pollData();

  ShowWindow(hwnd_, SW_SHOWNOACTIVATE);
  render();

  // 动画时钟：高分辨率可等待定时器（~0.5ms 粒度；实测 timeBeginPeriod(1) 对本
  // 进程无效——Win11 对非前台窗口压制计时器粒度请求，16ms WM_TIMER 仍量化到
  // 15.6ms 阶梯）。失败回退 16ms WM_TIMER。
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
  if (!animTimer_) SetTimer(hwnd_, kTimerAnim, 16, nullptr);
  SetTimer(hwnd_, kTimerPoll, 2000, nullptr);
  SetTimer(hwnd_, kTimerRel, 30000, nullptr);

  MSG msg;
  if (!animTimer_) {
    while (GetMessageW(&msg, nullptr, 0, 0) > 0) {
      TranslateMessage(&msg);
      DispatchMessageW(&msg);
    }
  } else {
    for (;;) {
      MsgWaitForMultipleObjectsEx(1, &animTimer_, INFINITE, QS_ALLINPUT,
                                  MWMO_INPUTAVAILABLE);
      if (WaitForSingleObject(animTimer_, 0) == WAIT_OBJECT_0) {
        // 先重整相位再处理帧：帧间隔恒定 16ms，与单帧耗时解耦
        SetWaitableTimerEx(animTimer_, &due, 0, nullptr, nullptr, nullptr, 0);
        animTick();
      }
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
