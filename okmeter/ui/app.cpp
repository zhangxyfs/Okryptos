#include "app.h"
#include "geometry.h"
#include <cwchar>

using Microsoft::WRL::ComPtr;

namespace okmeter {
namespace {

constexpr wchar_t kClassName[] = L"OkMeterDock";
constexpr int kDockWidth = 150;
constexpr UINT_PTR kRepaintTimer = 1;

} // namespace

void DockApp::render() {
  if (!d3d_.begin()) return;
  ID2D1DeviceContext* dc = d3d_.dc();
  dc->Clear(D2D1::ColorF(0, 0.0f));  // 全透明底

  RECT rc{};
  GetClientRect(hwnd_, &rc);
  const float w = (float)(rc.right - rc.left);
  const float h = (float)(rc.bottom - rc.top);

  // accent 绿实心圆（原型 accent 色）
  ComPtr<ID2D1SolidColorBrush> brush;
  if (SUCCEEDED(dc->CreateSolidColorBrush(D2D1::ColorF(0x5FE0A8), &brush))) {
    const D2D1_ELLIPSE e = D2D1::Ellipse(D2D1::Point2F(w / 2, h / 3), 30.0f, 30.0f);
    dc->FillEllipse(&e, brush.Get());
  }

  // Consolas 白文本（验证 DWrite 文本管线）
  ComPtr<IDWriteTextFormat> fmt;
  if (brush && SUCCEEDED(d3d_.dwrite()->CreateTextFormat(
          L"Consolas", nullptr, DWRITE_FONT_WEIGHT_NORMAL, DWRITE_FONT_STYLE_NORMAL,
          DWRITE_FONT_STRETCH_NORMAL, 12.0f, L"", &fmt))) {
    fmt->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_CENTER);
    brush->SetColor(D2D1::ColorF(D2D1::ColorF::White));
    const wchar_t* text = L"OkMeter dock";
    const D2D1_RECT_F tr = D2D1::RectF(0.0f, h * 2.0f / 3.0f, w, h);
    dc->DrawText(text, (UINT32)std::wcslen(text), fmt.Get(), &tr, brush.Get());
  }

  d3d_.end();
}

LRESULT CALLBACK DockApp::WndProc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp) {
  if (msg == WM_NCCREATE) {
    const auto* cs = reinterpret_cast<const CREATESTRUCTW*>(lp);
    SetWindowLongPtrW(hwnd, GWLP_USERDATA, (LONG_PTR)cs->lpCreateParams);
    return DefWindowProcW(hwnd, msg, wp, lp);
  }
  auto* self = reinterpret_cast<DockApp*>(GetWindowLongPtrW(hwnd, GWLP_USERDATA));
  if (!self) return DefWindowProcW(hwnd, msg, wp, lp);
  switch (msg) {
  case WM_DESTROY:
    PostQuitMessage(0);
    return 0;
  case WM_PAINT:
    ValidateRect(hwnd, nullptr);
    self->render();
    return 0;
  case WM_TIMER:
    self->render();  // 周期重绘；Task 7 数据刷新挂这里
    return 0;
  case WM_SIZE:
    self->d3d_.resize((int)LOWORD(lp), (int)HIWORD(lp));
    self->render();
    return 0;
  default:
    return DefWindowProcW(hwnd, msg, wp, lp);
  }
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

  // 窗口矩形：右缘、宽 150、高按 layoutArc(3)，垂直居中于工作区
  RECT work{};
  SystemParametersInfoW(SPI_GETWORKAREA, 0, &work, 0);
  const DockGeom g = layoutArc(3, 24, 8, work.bottom - work.top, "right");
  const int w = kDockWidth;
  int h = (int)g.h;
  if (h < 120) h = 120;
  const int x = work.right - w;
  const int y = work.top + ((work.bottom - work.top) - h) / 2;

  hwnd_ = CreateWindowExW(WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE,
                          kClassName, L"OkMeter", WS_POPUP,
                          x, y, w, h, nullptr, nullptr, inst, this);
  if (!hwnd_) return 1;
  if (!d3d_.init(hwnd_, w, h)) {
    DestroyWindow(hwnd_);
    return 2;
  }

  ShowWindow(hwnd_, SW_SHOWNOACTIVATE);
  render();
  SetTimer(hwnd_, kRepaintTimer, 1000, nullptr);

  MSG msg;
  while (GetMessageW(&msg, nullptr, 0, 0) > 0) {
    TranslateMessage(&msg);
    DispatchMessageW(&msg);
  }
  KillTimer(hwnd_, kRepaintTimer);
  return (int)msg.wParam;
}

} // namespace okmeter
