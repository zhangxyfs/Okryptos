#include "app.h"
#include "../adapters/kimi/adapter.h"
#include "../core/fmt.h"
#include "../core/paths.h"
#include "../core/store.h"
#include <chrono>
#include <cmath>
#include <string>

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

void DockApp::render() {
  if (!d3d_.begin()) return;
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
  d3d_.end();
}

LRESULT DockApp::onMessage(UINT msg, WPARAM wp, LPARAM lp) {
  switch (msg) {
  case WM_DESTROY:
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
    case kTimerAnim: {
      // RDCW 目录监听：每 500ms 检查一次，触发则提前 poll（与 2s 轮询同路径）
      const int64_t now = nowMs();
      if (now - lastWatchMs_ >= 500) {
        lastWatchMs_ = now;
        if (watch_.signaled()) pollData();
      }
      if (emerged_) return 0;  // 静止：不 step 不重绘不挪窗
      emerge_.step(0.016, emergeTarget_);
      if (emerge_.settled(emergeTarget_)) {
        emerge_.snap(emergeTarget_);
        emerged_ = true;
      }
      updatePosition();
      render();
      return 0;
    }
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
  case WM_DPICHANGED:
    rebuildLayout();  // 屏高/DPI 变化 → 几何重建
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

  // 首轮数据：启动即有真实值（不等第一个 2s 轮询）
  pollData();

  ShowWindow(hwnd_, SW_SHOWNOACTIVATE);
  render();
  SetTimer(hwnd_, kTimerAnim, 16, nullptr);
  SetTimer(hwnd_, kTimerPoll, 2000, nullptr);
  SetTimer(hwnd_, kTimerRel, 30000, nullptr);

  MSG msg;
  while (GetMessageW(&msg, nullptr, 0, 0) > 0) {
    TranslateMessage(&msg);
    DispatchMessageW(&msg);
  }
  KillTimer(hwnd_, kTimerAnim);
  KillTimer(hwnd_, kTimerPoll);
  KillTimer(hwnd_, kTimerRel);
  KillTimer(hwnd_, kTimerRetract);
  return (int)msg.wParam;
}

} // namespace okmeter
