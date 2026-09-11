// render/materials/glassfx.h —— 三材质共享内部助手：D2D effect GUID（SDK 头声明但
// 部分 lib 无导出定义，自建避免链接失败，与 dark.cpp 的 kClsidGaussianBlur 同策略）、
// 原型调色板、渐变画刷、环形几何（边缘环带）、过滤背景管线（模糊+提饱和+提亮）、
// 指针光源计算（liquid 镜面高光与 glow 统一光源同一套方向/距离衰减语义）。
#pragma once

#include "../material.h"
#include "../backdrop.h"
#include <algorithm>
#include <cmath>
#include <d2d1effects.h>
#include <initializer_list>
#include <vector>

namespace okmeter::render::glassfx {

// ── D2D effect CLSID（d2d1effects.h 仅声明）──
const GUID kClsidBlur = {0x1feb6d69, 0x2fe6, 0x4ac9,
                         {0x8c, 0x58, 0x1d, 0x7f, 0x93, 0xe7, 0xa6, 0xa5}};
const GUID kClsidSaturation = {0x5cb2d9cf, 0x327d, 0x459f,
                               {0xa0, 0xce, 0x40, 0xc0, 0xb2, 0x08, 0x6b, 0xf7}};
const GUID kClsidColorMatrix = {0x921f03d6, 0x641c, 0x47df,
                                {0x85, 0x2d, 0xb4, 0xbb, 0x61, 0x53, 0xae, 0x11}};
const GUID kClsidDisplacement = {0xedc48364, 0x0417, 0x4111,
                                 {0x94, 0x50, 0x43, 0x84, 0x5f, 0xa9, 0xf8, 0x90}};
const GUID kClsidCrop = {0xe23f7110, 0x0e9a, 0x4324,
                         {0xaf, 0x47, 0x6a, 0x2c, 0x0c, 0x46, 0xf3, 0x5b}};
const GUID kClsidAffine2D = {0x6aa97485, 0x6354, 0x4cfc,
                             {0x90, 0x8c, 0xe4, 0xa7, 0x4f, 0x62, 0xc9, 0x6c}};

// ── 原型调色板（oklch 近似 sRGB）──
constexpr UINT32 kAccent = 0x5FE0A8;  // 原型 accent 绿
inline D2D1_COLOR_F accentC(float a) { return D2D1::ColorF(kAccent, a); }
inline D2D1_COLOR_F ink(float a) { return D2D1::ColorF(0.93f, 0.94f, 0.96f, a); }
inline D2D1_COLOR_F desk(float a) { return D2D1::ColorF(0.13f, 0.14f, 0.17f, a); }
inline D2D1_COLOR_F deskDeep(float a) { return D2D1::ColorF(0.06f, 0.07f, 0.09f, a); }
inline D2D1_COLOR_F white(float a) { return D2D1::ColorF(1.0f, 1.0f, 1.0f, a); }

struct Stop { float pos; D2D1_COLOR_F color; };

inline Microsoft::WRL::ComPtr<ID2D1GradientStopCollection> stops(
    ID2D1DeviceContext* dc, std::initializer_list<Stop> list) {
  std::vector<D2D1_GRADIENT_STOP> gs;
  gs.reserve(list.size());
  for (const Stop& s : list) gs.push_back(D2D1::GradientStop(s.pos, s.color));
  Microsoft::WRL::ComPtr<ID2D1GradientStopCollection> coll;
  if (FAILED(dc->CreateGradientStopCollection(gs.data(), (UINT32)gs.size(), &coll)))
    return nullptr;
  return coll;
}

inline Microsoft::WRL::ComPtr<ID2D1RadialGradientBrush> radial(
    ID2D1DeviceContext* dc, std::initializer_list<Stop> list) {
  auto coll = stops(dc, list);
  if (!coll) return nullptr;
  Microsoft::WRL::ComPtr<ID2D1RadialGradientBrush> brush;
  if (FAILED(dc->CreateRadialGradientBrush(
          D2D1::RadialGradientBrushProperties(D2D1::Point2F(0, 0),
                                              D2D1::Point2F(0, 0), 1.0f, 1.0f),
          coll.Get(), &brush)))
    return nullptr;
  return brush;
}

inline Microsoft::WRL::ComPtr<ID2D1LinearGradientBrush> linear(
    ID2D1DeviceContext* dc, std::initializer_list<Stop> list) {
  auto coll = stops(dc, list);
  if (!coll) return nullptr;
  Microsoft::WRL::ComPtr<ID2D1LinearGradientBrush> brush;
  if (FAILED(dc->CreateLinearGradientBrush(
          D2D1::LinearGradientBrushProperties(D2D1::Point2F(0, 0),
                                              D2D1::Point2F(1, 1)),
          coll.Get(), &brush)))
    return nullptr;
  return brush;
}

// 圆域裁剪层：PushLayer(椭圆几何) → 画 → PopLayer
inline bool pushCircleClip(ID2D1DeviceContext* dc, D2D1_POINT_2F c, float r) {
  Microsoft::WRL::ComPtr<ID2D1Factory> factory;
  dc->GetFactory(&factory);
  Microsoft::WRL::ComPtr<ID2D1EllipseGeometry> clip;
  if (!factory || FAILED(factory->CreateEllipseGeometry(D2D1::Ellipse(c, r, r), &clip)))
    return false;
  const D2D1_RECT_F bounds =
      D2D1::RectF(c.x - r - 1.0f, c.y - r - 1.0f, c.x + r + 1.0f, c.y + r + 1.0f);
  dc->PushLayer(D2D1::LayerParameters1(bounds, clip.Get(),
                                       D2D1_ANTIALIAS_MODE_PER_PRIMITIVE),
                nullptr);
  return true;
}

// ── 项形状抽象：halfW > r 时胶囊（圆角矩形 半宽 halfW 半高 r 角半径 r），否则圆 ──
inline bool isPill(float halfW, float r) { return halfW > r + 0.01f; }

inline D2D1_ROUNDED_RECT pillRR(D2D1_POINT_2F c, float halfW, float r,
                                float grow = 0.0f) {
  return D2D1::RoundedRect(
      D2D1::RectF(c.x - halfW - grow, c.y - r - grow,
                  c.x + halfW + grow, c.y + r + grow),
      r + grow, r + grow);
}

// 形状填充（胶囊→圆角矩形，圆→椭圆）
inline void fillShape(ID2D1DeviceContext* dc, D2D1_POINT_2F c, float halfW, float r,
                      ID2D1Brush* brush) {
  if (isPill(halfW, r)) {
    const D2D1_ROUNDED_RECT rr = pillRR(c, halfW, r);
    dc->FillRoundedRectangle(&rr, brush);
  } else {
    const D2D1_ELLIPSE e = D2D1::Ellipse(c, r, r);
    dc->FillEllipse(&e, brush);
  }
}

// 形状描边（grow 外扩用于中心项光晕环）
inline void drawShape(ID2D1DeviceContext* dc, D2D1_POINT_2F c, float halfW, float r,
                      ID2D1Brush* brush, float strokeW = 1.0f, float grow = 0.0f) {
  if (isPill(halfW, r)) {
    const D2D1_ROUNDED_RECT rr = pillRR(c, halfW, r, grow);
    dc->DrawRoundedRectangle(&rr, brush, strokeW);
  } else {
    const D2D1_ELLIPSE e = D2D1::Ellipse(c, r + grow, r + grow);
    dc->DrawEllipse(&e, brush, strokeW);
  }
}

// 形状裁剪层（胶囊→圆角矩形几何，圆→pushCircleClip）
inline bool pushShapeClip(ID2D1DeviceContext* dc, D2D1_POINT_2F c, float halfW,
                          float r) {
  if (!isPill(halfW, r)) return pushCircleClip(dc, c, r);
  Microsoft::WRL::ComPtr<ID2D1Factory> factory;
  dc->GetFactory(&factory);
  Microsoft::WRL::ComPtr<ID2D1RoundedRectangleGeometry> clip;
  if (!factory ||
      FAILED(factory->CreateRoundedRectangleGeometry(pillRR(c, halfW, r), &clip)))
    return false;
  const D2D1_RECT_F bounds = D2D1::RectF(c.x - halfW - 1.0f, c.y - r - 1.0f,
                                         c.x + halfW + 1.0f, c.y + r + 1.0f);
  dc->PushLayer(D2D1::LayerParameters1(bounds, clip.Get(),
                                       D2D1_ANTIALIAS_MODE_PER_PRIMITIVE),
                nullptr);
  return true;
}

// 光晕/阴影的椭圆半径：胶囊横向按 halfW 外扩（椭圆圆角近似胶囊外发光）
inline float shapeRX(float halfW, float r, float grow) {
  return (isPill(halfW, r) ? halfW : r) + grow;
}

// 边缘环带几何（外圆 - 内圆，ALTERNATE 填充）：边缘光/镜面高光/色散的绘制域
inline Microsoft::WRL::ComPtr<ID2D1Geometry> ring(ID2D1DeviceContext* dc,
                                                  D2D1_POINT_2F c, float rOut,
                                                  float rIn) {
  Microsoft::WRL::ComPtr<ID2D1Factory> factory;
  dc->GetFactory(&factory);
  Microsoft::WRL::ComPtr<ID2D1PathGeometry> path;
  Microsoft::WRL::ComPtr<ID2D1GeometrySink> sink;
  if (!factory || FAILED(factory->CreatePathGeometry(&path)) ||
      FAILED(path->Open(&sink)))
    return nullptr;
  sink->SetFillMode(D2D1_FILL_MODE_ALTERNATE);
  auto ellipse = [&](float r) {
    sink->BeginFigure(D2D1::Point2F(c.x + r, c.y), D2D1_FIGURE_BEGIN_FILLED);
    D2D1_ARC_SEGMENT arc{};
    arc.point = D2D1::Point2F(c.x - r, c.y);
    arc.size = D2D1::SizeF(r, r);
    arc.sweepDirection = D2D1_SWEEP_DIRECTION_CLOCKWISE;
    sink->AddArc(&arc);
    arc.point = D2D1::Point2F(c.x + r, c.y);
    sink->AddArc(&arc);
    sink->EndFigure(D2D1_FIGURE_END_CLOSED);
  };
  ellipse(rOut);
  ellipse(rIn);
  if (FAILED(sink->Close())) return nullptr;
  return path;
}

// 过滤背景管线：整屏背景帧 → 高斯模糊 → 提饱和 →（可选）ColorMatrix 线性提亮。
// 设备代际由材质侧 ensure 把关；dirty/首帧时 refresh 重建输入位图。
// saturation 为 D2D 语义（0.5=原图，CSS saturate(150%) → 0.75）；
// brightness 为 CSS 线性倍数（1.0 时跳过该节点）。
class BackdropPipe {
public:
  void reset() {
    blur_.Reset();
    sat_.Reset();
    bright_.Reset();
    bg_.Reset();
  }

  bool ensure(ID2D1DeviceContext* dc, float stddev, float saturation,
              float brightness = 1.0f) {
    if (!blur_) {
      if (FAILED(dc->CreateEffect(kClsidBlur, &blur_))) return false;
      (void)blur_->SetValue(D2D1_GAUSSIANBLUR_PROP_BORDER_MODE,
                            D2D1_BORDER_MODE_SOFT);
      (void)blur_->SetValue(D2D1_GAUSSIANBLUR_PROP_OPTIMIZATION,
                            D2D1_GAUSSIANBLUR_OPTIMIZATION_QUALITY);
    }
    if (!sat_ && FAILED(dc->CreateEffect(kClsidSaturation, &sat_))) return false;
    (void)blur_->SetValue(D2D1_GAUSSIANBLUR_PROP_STANDARD_DEVIATION, stddev);
    (void)sat_->SetValue(D2D1_SATURATION_PROP_SATURATION, saturation);
    // 先接通主链再挂可选亮度节点：CreateEffect(ColorMatrix) 失败时 bright_ 保持
    // null，输出落到已接线的 sat_（丢提亮不丢帧）；杜绝 sat_ 输入悬空导致
    // ready() 判真却 DrawImage WRONG_STATE 的回归（排障期实测 0x8899001E）
    sat_->SetInputEffect(0, blur_.Get());
    if (brightness != 1.0f && !bright_) {
      if (SUCCEEDED(dc->CreateEffect(kClsidColorMatrix, &bright_))) {
        D2D1_MATRIX_5X4_F m{};
        m._11 = brightness;
        m._22 = brightness;
        m._33 = brightness;
        m._44 = 1.0f;
        (void)bright_->SetValue(D2D1_COLORMATRIX_PROP_COLOR_MATRIX, m);
      }
    }
    if (brightness == 1.0f) bright_.Reset();
    if (bright_) bright_->SetInputEffect(0, sat_.Get());
    return true;
  }

  // 背景帧 → ID2D1Bitmap1（dirty/首帧重建）并挂到模糊输入
  void refresh(ID2D1DeviceContext* dc, BackdropCapture* backdrop) {
    if (bg_ && !backdrop->dirty()) return;
    Microsoft::WRL::ComPtr<ID3D11Texture2D> tex;
    if (!backdrop->acquire(&tex) || !tex) return;
    Microsoft::WRL::ComPtr<IDXGISurface> surface;
    if (FAILED(tex.As(&surface)) || !surface) return;
    const D2D1_BITMAP_PROPERTIES1 props = D2D1::BitmapProperties1(
        D2D1_BITMAP_OPTIONS_NONE,
        D2D1::PixelFormat(DXGI_FORMAT_B8G8R8A8_UNORM,
                          D2D1_ALPHA_MODE_PREMULTIPLIED));
    Microsoft::WRL::ComPtr<ID2D1Bitmap1> bmp;
    if (FAILED(dc->CreateBitmapFromDxgiSurface(surface.Get(), &props, &bmp)) || !bmp)
      return;
    bg_ = bmp;
    if (blur_) blur_->SetInput(0, bg_.Get());
    backdrop->markClean();
  }

  ID2D1Effect* output() const { return bright_ ? bright_.Get() : sat_.Get(); }
  bool ready() const { return bg_ && blur_ && sat_; }

private:
  Microsoft::WRL::ComPtr<ID2D1Effect> blur_;
  Microsoft::WRL::ComPtr<ID2D1Effect> sat_;
  Microsoft::WRL::ComPtr<ID2D1Effect> bright_;
  Microsoft::WRL::ComPtr<ID2D1Bitmap1> bg_;
};

// 指针光源：方向单位向量 + 强度（原型 --si = max(0, 1 - dist/320)）。
// liquid 镜面高光与 glow 统一光源共用；无指针时 k=0。
struct PtrLight {
  float ux = 0, uy = -1;  // 球心 → 指针 单位方向
  float k = 0;            // 强度 [0,1]，320px 线性衰减
};

inline PtrLight ptrLight(float px, float py, bool hasPtr, D2D1_POINT_2F c) {
  PtrLight L;
  if (!hasPtr) return L;
  const float dx = px - c.x, dy = py - c.y;
  const float d = std::hypot(dx, dy);
  L.k = std::clamp(1.0f - d / 320.0f, 0.0f, 1.0f);  // windows.h max 宏冲突，用 clamp
  if (d > 0.001f) {
    L.ux = dx / d;
    L.uy = dy / d;
  }
  return L;
}

} // namespace okmeter::render::glassfx
