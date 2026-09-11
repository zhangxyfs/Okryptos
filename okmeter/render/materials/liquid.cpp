// render/materials/liquid.cpp —— 液态玻璃材质（对标 iOS 26）：
// 边缘环带折射（球域裁剪 → Crop 取球区背景 → DisplacementMap 预烘圆形法线位移图，
// 中心 58% 零位移、边缘沿法线外推，与原型 feDisplacementMap scale=14 同语义）
// → 微模糊 σ0.75≈blur(1.5px) + saturate(180%)→0.9 + brightness(1.12)
// + 上左亮/下右暗不均匀边缘光（环带几何 + 双向线性渐变）
// + 跟指针镜面高光（方向角 atan2 + 320px 距离衰减，原型 --sa/--si 同套计算）
// + 边缘 1px 红蓝色散 + 浮动阴影（0 14px 34px black 38%）。
// 背景捕获不可用/affinity 降级 → 整体退化毛玻璃（强模糊 + 白 12% + 提饱和）。
#include "glassfx.h"
#include <dxgi.h>

using Microsoft::WRL::ComPtr;

namespace okmeter::render {
namespace {

constexpr UINT kMapSize = 128;      // 预烘法线位移图边长
constexpr float kMapInner = 0.58f;  // 中心零位移半径（原型 INNER=0.58）
constexpr float kDispScale = 14.0f; // 位移幅度（原型 feDisplacementMap scale=14）
constexpr float kCropPad = 9.0f;    // 裁剪外扩 ≥ 最大位移 7px + 模糊渗边

// 预烘圆形法线位移图：中心 58% 中性灰（零位移），边缘环带沿径向平滑外推。
// R/G 通道编码 X/Y 位移（128=零），与原型 JS 超椭圆场同算法（圆球 P=2）。
ComPtr<ID2D1Bitmap> bakeNormalMap(ID2D1DeviceContext* dc) {
  std::vector<BYTE> px((size_t)kMapSize * kMapSize * 4);
  for (UINT y = 0; y < kMapSize; ++y)
    for (UINT x = 0; x < kMapSize; ++x) {
      const float nx = ((float)x / (kMapSize - 1)) * 2.0f - 1.0f;
      const float ny = ((float)y / (kMapSize - 1)) * 2.0f - 1.0f;
      const float dd = std::hypot(nx, ny);
      float band = (dd - kMapInner) / (1.0f - kMapInner);
      band = std::clamp(band, 0.0f, 1.0f);
      band = band * band * (3.0f - 2.0f * band);  // smoothstep
      const float len = dd > 1e-6f ? dd : 1.0f;
      BYTE* p = px.data() + ((size_t)y * kMapSize + x) * 4;
      p[2] = (BYTE)std::clamp(128.0f + (nx / len) * band * 127.0f, 0.0f, 255.0f);
      p[1] = (BYTE)std::clamp(128.0f + (ny / len) * band * 127.0f, 0.0f, 255.0f);
      p[0] = 128;
      p[3] = 255;
    }
  ComPtr<ID2D1Bitmap> bmp;
  const D2D1_BITMAP_PROPERTIES props = D2D1::BitmapProperties(
      D2D1::PixelFormat(DXGI_FORMAT_B8G8R8A8_UNORM, D2D1_ALPHA_MODE_PREMULTIPLIED));
  if (FAILED(dc->CreateBitmap(D2D1::SizeU(kMapSize, kMapSize), px.data(),
                              kMapSize * 4, &props, &bmp)))
    return nullptr;
  return bmp;
}

class LiquidMaterial final : public IMaterial {
public:
  std::string id() const override { return "liquid"; }

  void onPointer(float x, float y) const override {
    px_ = x;
    py_ = y;
    hasPtr_ = true;
  }
  void onPointerLeave() const override { hasPtr_ = false; }

  void drawOrbBack(ID2D1DeviceContext* dc, const OrbStyleCtx& ctx) const override {
    if (!dc || !ctx.d3d || !ctx.brush || ctx.r <= 0) return;
    ensure(dc, ctx.d3d);
    const D2D1_POINT_2F c = ctx.center;
    const float r = ctx.r;
    const float dim = ctx.dimmed;
    const bool fullGlass =
        ctx.backdrop && ctx.backdrop->ok() && !ctx.backdrop->degraded();

    // 悬停外发光（原型 .orb.hot）
    if (ctx.isHot && hotGlow_) {
      hotGlow_->SetCenter(c);
      hotGlow_->SetRadiusX(r + 22.0f);
      hotGlow_->SetRadiusY(r + 22.0f);
      dc->FillEllipse(D2D1::Ellipse(c, r + 22.0f, r + 22.0f), hotGlow_.Get());
    }

    // 浮动阴影（原型 0 14px 34px black 38%：中心下移 14，大而柔）
    if (floatShadow_) {
      floatShadow_->SetCenter(D2D1::Point2F(c.x, c.y + 14.0f));
      floatShadow_->SetRadiusX(r + 17.0f);
      floatShadow_->SetRadiusY(r + 17.0f);
      dc->FillEllipse(D2D1::Ellipse(c, r + 17.0f, r + 17.0f), floatShadow_.Get());
    }

    // 中心项光晕环（原型 .orb.center）
    const D2D1_ELLIPSE ball = D2D1::Ellipse(c, r, r);
    if (ctx.isCenter) {
      ctx.brush->SetColor(glassfx::accentC(0.10f * dim));
      dc->DrawEllipse(D2D1::Ellipse(c, r + 3.0f, r + 3.0f), ctx.brush, 6.0f);
    }

    // 玻璃底：全折射路径 / 退化毛玻璃 / 纯色兜底
    if (fullGlass) {
      refreshBackdrop(dc, ctx.backdrop);
      if (refractReady() && glassfx::pushCircleClip(dc, c, r)) {
        // 球区背景（纹理坐标）→ 位移折射 → 微模糊 → 提饱和 → 提亮 → 平移回原点
        const float l = c.x - r - kCropPad - ctx.backdropDX;
        const float t = c.y - r - kCropPad - ctx.backdropDY;
        (void)crop_->SetValue(D2D1_CROP_PROP_RECT,
                              D2D1::Vector4F(l, t, c.x + r + kCropPad - ctx.backdropDX,
                                             c.y + r + kCropPad - ctx.backdropDY));
        (void)move_->SetValue(D2D1_2DAFFINETRANSFORM_PROP_TRANSFORM_MATRIX,
                              D2D1::Matrix3x2F::Translation(-l, -t));
        dc->DrawImage(move_.Get(),
                      D2D1::Point2F(c.x - r - kCropPad, c.y - r - kCropPad),
                      D2D1_INTERPOLATION_MODE_LINEAR);
        // 环境色染色：desk-deep 9% + 左上 ink 15% 径向（原型 .orb background）
        ctx.brush->SetColor(glassfx::deskDeep(0.09f * dim));
        dc->FillEllipse(&ball, ctx.brush);
        if (tintHl_) {
          tintHl_->SetCenter(D2D1::Point2F(c.x - 0.4f * r, c.y - 0.6f * r));
          tintHl_->SetRadiusX(1.1f * r);
          tintHl_->SetRadiusY(1.1f * r);
          dc->FillEllipse(&ball, tintHl_.Get());
        }
        dc->PopLayer();
      }
    } else {
      drawFrostFallback(dc, ctx);
    }

    // 不均匀边缘光（全玻璃与退化毛玻璃都画）：上/左亮、下/右暗，
    // 环带几何直接填双向线性渐变（原型 inset 四向 box-shadow 组合）
    if (edgeBright_ && edgeDark_) {
      if (ComPtr<ID2D1Geometry> band = glassfx::ring(dc, c, r + 0.5f, r - 2.0f)) {
        edgeBright_->SetStartPoint(D2D1::Point2F(c.x - r, c.y - r));
        edgeBright_->SetEndPoint(D2D1::Point2F(c.x + 0.5f * r, c.y + 0.5f * r));
        dc->FillGeometry(band.Get(), edgeBright_.Get());
        edgeDark_->SetStartPoint(D2D1::Point2F(c.x + r, c.y + r));
        edgeDark_->SetEndPoint(D2D1::Point2F(c.x - 0.4f * r, c.y - 0.4f * r));
        dc->FillGeometry(band.Get(), edgeDark_.Get());
      }
    }

    // 跟指针镜面高光：边缘环带（58%~100% r）楔形指向光源，layer 不透明度 =
    // 距离衰减强度（原型 --si = max(0, 1 - dist/320)）
    const glassfx::PtrLight L = glassfx::ptrLight(px_, py_, hasPtr_, c);
    if (fullGlass && L.k > 0.01f && specular_) {
      if (ComPtr<ID2D1Geometry> band = glassfx::ring(dc, c, r, r * 0.58f)) {
        specular_->SetCenter(
            D2D1::Point2F(c.x + L.ux * r * 0.79f, c.y + L.uy * r * 0.79f));
        specular_->SetRadiusX(r * 0.9f);
        specular_->SetRadiusY(r * 0.9f);
        const D2D1_RECT_F bounds =
            D2D1::RectF(c.x - r - 1.0f, c.y - r - 1.0f, c.x + r + 1.0f, c.y + r + 1.0f);
        dc->PushLayer(D2D1::LayerParameters1(bounds, band.Get(),
                                             D2D1_ANTIALIAS_MODE_PER_PRIMITIVE,
                                             D2D1::Matrix3x2F::Identity(),
                                             L.k * dim),
                      nullptr);
        dc->FillEllipse(&ball, specular_.Get());
        dc->PopLayer();
      }
    }

    // 边缘 1px 色散：红/蓝 1px 描边横向错位 ±0.6px
    ctx.brush->SetColor(D2D1::ColorF(1.0f, 0.30f, 0.25f, 0.20f * dim));
    dc->DrawEllipse(D2D1::Ellipse(D2D1::Point2F(c.x - 0.6f, c.y), r - 0.5f, r - 0.5f),
                    ctx.brush, 1.0f);
    ctx.brush->SetColor(D2D1::ColorF(0.30f, 0.55f, 1.0f, 0.20f * dim));
    dc->DrawEllipse(D2D1::Ellipse(D2D1::Point2F(c.x + 0.6f, c.y), r - 0.5f, r - 0.5f),
                    ctx.brush, 1.0f);

    // 1px 描边：悬停 accent > 中心 accent 60% > ink 16%（原型 liquid border）
    if (ctx.isHot)
      ctx.brush->SetColor(glassfx::accentC(1.0f * dim));
    else if (ctx.isCenter)
      ctx.brush->SetColor(glassfx::accentC(0.60f * dim));
    else
      ctx.brush->SetColor(glassfx::ink(0.16f * dim));
    dc->DrawEllipse(&ball, ctx.brush, 1.0f);
  }

  // 详情卡底：82% 深玻璃 + 白 6% 提亮 + ink 17% 描边（v1 卡不做 backdrop blur）
  void drawCardBack(ID2D1DeviceContext* dc, const D2D1_RECT_F& rect,
                    float radius) const override {
    if (!dc) return;
    ComPtr<ID2D1SolidColorBrush> brush;
    if (FAILED(dc->CreateSolidColorBrush(D2D1::ColorF(0, 0), &brush))) return;
    const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(rect, radius, radius);
    brush->SetColor(glassfx::deskDeep(0.82f));
    dc->FillRoundedRectangle(&rr, brush.Get());
    brush->SetColor(glassfx::ink(0.06f));
    dc->FillRoundedRectangle(&rr, brush.Get());
    brush->SetColor(glassfx::ink(0.17f));
    dc->DrawRoundedRectangle(&rr, brush.Get(), 1.0f);
  }

  // 弧线描边：ink 22% 双pass（原型 liquid 下 path stroke ink 22%）
  void drawArcStroke(ID2D1DeviceContext* dc, const DockGeom& g) const override {
    if (!dc || g.items.size() < 2) return;
    ComPtr<ID2D1SolidColorBrush> brush;
    if (FAILED(dc->CreateSolidColorBrush(D2D1::ColorF(0, 0), &brush))) return;
    brush->SetColor(glassfx::ink(0.22f));
    for (int pass = 0; pass < 2; ++pass)
      for (size_t i = 0; i + 1 < g.items.size(); ++i) {
        const D2D1_POINT_2F a{ (float)g.items[i].x, (float)g.items[i].y };
        const D2D1_POINT_2F b{ (float)g.items[i + 1].x, (float)g.items[i + 1].y };
        dc->DrawLine(a, b, brush.Get(), 1.0f);
      }
  }

private:
  // 退化毛玻璃：强模糊 σ11 + 提饱和 0.75 + 白 12% + 顶部内高光（frost 同款；
  // 捕获完全不可用时纯色兜底）
  void drawFrostFallback(ID2D1DeviceContext* dc, const OrbStyleCtx& ctx) const {
    const D2D1_POINT_2F c = ctx.center;
    const float r = ctx.r;
    const float dim = ctx.dimmed;
    const D2D1_ELLIPSE ball = D2D1::Ellipse(c, r, r);
    bool drawn = false;
    if (ctx.backdrop && ctx.backdrop->ok()) {
      frostPipe_.refresh(dc, ctx.backdrop);
      if (frostPipe_.ready() && glassfx::pushCircleClip(dc, c, r)) {
        dc->DrawImage(frostPipe_.output(),
                      D2D1::Point2F(ctx.backdropDX, ctx.backdropDY),
                      D2D1_INTERPOLATION_MODE_LINEAR);
        ctx.brush->SetColor(glassfx::ink(0.12f * dim));
        dc->FillEllipse(&ball, ctx.brush);
        dc->PopLayer();
        drawn = true;
      }
    }
    if (!drawn) {
      ctx.brush->SetColor(glassfx::deskDeep(0.55f * dim));
      dc->FillEllipse(&ball, ctx.brush);
      ctx.brush->SetColor(glassfx::ink(0.12f * dim));
      dc->FillEllipse(&ball, ctx.brush);
    }
    if (hlTop_) {
      hlTop_->SetCenter(D2D1::Point2F(c.x - 0.36f * r, c.y - 0.48f * r));
      hlTop_->SetRadiusX(1.3f * r);
      hlTop_->SetRadiusY(1.3f * r);
      dc->FillEllipse(&ball, hlTop_.Get());
    }
  }

  bool refractReady() const { return bgBmp_ && crop_ && disp_ && mapBmp_ && move_; }

  void ensure(ID2D1DeviceContext* dc, const D3DContext* d3d) const {
    if (dc == seenDc_ && gen_ == d3d->generation()) return;
    seenDc_ = dc;
    gen_ = d3d->generation();
    crop_.Reset();
    disp_.Reset();
    blurSm_.Reset();
    sat_.Reset();
    bright_.Reset();
    move_.Reset();
    mapBmp_.Reset();
    bgBmp_.Reset();
    frostPipe_.reset();
    hotGlow_.Reset();
    floatShadow_.Reset();
    tintHl_.Reset();
    hlTop_.Reset();
    edgeBright_.Reset();
    edgeDark_.Reset();
    specular_.Reset();

    // 折射链：Crop → DisplacementMap(scale 14) → Blur σ0.75 → Saturation 0.9
    // → ColorMatrix 1.12 提亮 → Affine2D 平移回原点
    const bool okChain =
        SUCCEEDED(dc->CreateEffect(glassfx::kClsidCrop, &crop_)) &&
        SUCCEEDED(dc->CreateEffect(glassfx::kClsidDisplacement, &disp_)) &&
        SUCCEEDED(dc->CreateEffect(glassfx::kClsidBlur, &blurSm_)) &&
        SUCCEEDED(dc->CreateEffect(glassfx::kClsidSaturation, &sat_)) &&
        SUCCEEDED(dc->CreateEffect(glassfx::kClsidColorMatrix, &bright_)) &&
        SUCCEEDED(dc->CreateEffect(glassfx::kClsidAffine2D, &move_));
    if (okChain) {
      (void)disp_->SetValue(D2D1_DISPLACEMENTMAP_PROP_SCALE, kDispScale);
      (void)blurSm_->SetValue(D2D1_GAUSSIANBLUR_PROP_STANDARD_DEVIATION, 0.75f);
      (void)blurSm_->SetValue(D2D1_GAUSSIANBLUR_PROP_BORDER_MODE,
                              D2D1_BORDER_MODE_SOFT);
      (void)sat_->SetValue(D2D1_SATURATION_PROP_SATURATION, 0.9f);
      D2D1_MATRIX_5X4_F m{};
      m._11 = 1.12f;
      m._22 = 1.12f;
      m._33 = 1.12f;
      m._44 = 1.0f;
      (void)bright_->SetValue(D2D1_COLORMATRIX_PROP_COLOR_MATRIX, m);
      disp_->SetInputEffect(0, crop_.Get());
      blurSm_->SetInputEffect(0, disp_.Get());
      sat_->SetInputEffect(0, blurSm_.Get());
      bright_->SetInputEffect(0, sat_.Get());
      move_->SetInputEffect(0, bright_.Get());
      mapBmp_ = bakeNormalMap(dc);
      if (mapBmp_) disp_->SetInput(1, mapBmp_.Get());
    } else {
      crop_.Reset();  // refractReady() 判空 → 自动退化毛玻璃
    }

    (void)frostPipe_.ensure(dc, 11.0f, 0.75f);
    hotGlow_ = glassfx::radial(dc, {{0.0f, glassfx::accentC(0.32f)},
                                    {0.50f, glassfx::accentC(0.16f)},
                                    {1.0f, glassfx::accentC(0.0f)}});
    floatShadow_ = glassfx::radial(dc, {{0.0f, D2D1::ColorF(0, 0, 0, 0.38f)},
                                        {0.55f, D2D1::ColorF(0, 0, 0, 0.15f)},
                                        {1.0f, D2D1::ColorF(0, 0, 0, 0.0f)}});
    tintHl_ = glassfx::radial(dc, {{0.0f, glassfx::ink(0.15f)},
                                   {0.55f, glassfx::ink(0.0f)},
                                   {1.0f, glassfx::ink(0.0f)}});
    hlTop_ = glassfx::radial(dc, {{0.0f, glassfx::ink(0.16f)},
                                  {0.62f, glassfx::ink(0.0f)},
                                  {1.0f, glassfx::ink(0.0f)}});
    edgeBright_ = glassfx::linear(dc, {{0.0f, glassfx::white(0.55f)},
                                       {0.55f, glassfx::white(0.14f)},
                                       {1.0f, glassfx::white(0.0f)}});
    edgeDark_ = glassfx::linear(dc, {{0.0f, glassfx::deskDeep(0.50f)},
                                     {0.55f, glassfx::deskDeep(0.16f)},
                                     {1.0f, glassfx::deskDeep(0.0f)}});
    specular_ = glassfx::radial(dc, {{0.0f, glassfx::white(0.55f)},
                                     {0.55f, glassfx::white(0.0f)},
                                     {1.0f, glassfx::white(0.0f)}});
  }

  // 背景帧纹理 → ID2D1Bitmap1（dirty/首帧/设备重建时重建），挂到折射链首节点
  void refreshBackdrop(ID2D1DeviceContext* dc, BackdropCapture* backdrop) const {
    if (bgBmp_ && !backdrop->dirty()) return;
    ComPtr<ID3D11Texture2D> tex;
    if (!backdrop->acquire(&tex) || !tex) return;
    ComPtr<IDXGISurface> surface;
    if (FAILED(tex.As(&surface)) || !surface) return;
    const D2D1_BITMAP_PROPERTIES1 props = D2D1::BitmapProperties1(
        D2D1_BITMAP_OPTIONS_NONE,
        D2D1::PixelFormat(DXGI_FORMAT_B8G8R8A8_UNORM,
                          D2D1_ALPHA_MODE_PREMULTIPLIED));
    ComPtr<ID2D1Bitmap1> bmp;
    if (FAILED(dc->CreateBitmapFromDxgiSurface(surface.Get(), &props, &bmp)) || !bmp)
      return;
    bgBmp_ = bmp;
    if (crop_) crop_->SetInput(0, bgBmp_.Get());
    backdrop->markClean();
  }

  mutable const ID2D1DeviceContext* seenDc_ = nullptr;
  mutable unsigned gen_ = 0;
  // 折射链资源
  mutable ComPtr<ID2D1Effect> crop_, disp_, blurSm_, sat_, bright_, move_;
  mutable ComPtr<ID2D1Bitmap> mapBmp_;    // 预烘法线位移图
  mutable ComPtr<ID2D1Bitmap1> bgBmp_;    // 最新背景帧
  mutable glassfx::BackdropPipe frostPipe_;  // 退化毛玻璃管线
  mutable ComPtr<ID2D1RadialGradientBrush> hotGlow_, floatShadow_, tintHl_;
  mutable ComPtr<ID2D1RadialGradientBrush> hlTop_, specular_;
  mutable ComPtr<ID2D1LinearGradientBrush> edgeBright_, edgeDark_;
  mutable float px_ = 0, py_ = 0;
  mutable bool hasPtr_ = false;
};

} // namespace

void registerLiquidMaterial(Registry<IMaterial>& reg) {
  reg.add("liquid",
          [] { return std::unique_ptr<IMaterial>(new LiquidMaterial()); });
}

} // namespace okmeter::render
