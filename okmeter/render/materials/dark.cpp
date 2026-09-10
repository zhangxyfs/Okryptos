// render/materials/dark.cpp —— 暗夜材质：玻璃球底（WGC 背景模糊 + desk-deep 74% 染色）
// + 顶部内高光 + 底部外阴影 + hairline/accent 描边 + accent 微光弧线。
// 背景捕获不可用（未启动/失败/affinity 降级）时退化为纯色 74% 透明底（无模糊）。
#include "../material.h"
#include "../backdrop.h"
#include <algorithm>
#include <d2d1effects.h>
#include <dxgi.h>

using Microsoft::WRL::ComPtr;

namespace okmeter::render {
namespace {

constexpr UINT32 kAccent = 0x5FE0A8;  // 原型 accent 绿

// CLSID_D2D1GaussianBlur 的公开 GUID（SDK 头声明但部分 lib 无导出定义，自建避免链接失败）
const GUID kClsidGaussianBlur = {0x1feb6d69, 0x2fe6, 0x4ac9,
                                 {0x8c, 0x58, 0x1d, 0x7f, 0x93, 0xe7, 0xa6, 0xa5}};

D2D1_COLOR_F kDeskDeep(float a) { return D2D1::ColorF(0.06f, 0.07f, 0.09f, a); }
D2D1_COLOR_F kHairline(float a) { return D2D1::ColorF(1.0f, 1.0f, 1.0f, a); }

class DarkMaterial final : public IMaterial {
public:
  std::string id() const override { return "dark"; }

  void onPointer(float x, float y) const override {
    px_ = x;
    py_ = y;
    hasPtr_ = true;
  }

  // 球体底：悬停外发光 → 底部外阴影 → 中心光晕环 → 玻璃底（模糊背景 + 74% 染色）
  // → 顶部内高光 → 1px 描边。画刷用 ctx.brush；渐变/模糊资源按设备代际缓存。
  void drawOrbBack(ID2D1DeviceContext* dc, const OrbStyleCtx& ctx) const override {
    if (!dc || !ctx.d3d || !ctx.brush || ctx.r <= 0) return;
    ensure(dc, ctx.d3d);
    const D2D1_POINT_2F c = ctx.center;
    const float r = ctx.r;
    const float dim = ctx.dimmed;

    // 悬停外发光（原型 .orb.hot box-shadow 0 0 22px accent 32%）
    if (ctx.isHot && hotGlow_) {
      hotGlow_->SetCenter(c);
      hotGlow_->SetRadiusX(r + 22.0f);
      hotGlow_->SetRadiusY(r + 22.0f);
      dc->FillEllipse(D2D1::Ellipse(c, r + 22.0f, r + 22.0f), hotGlow_.Get());
    }

    // 底部外阴影（柔和径向渐变，中心下移 5px 模拟下方投影）
    if (shadow_) {
      shadow_->SetCenter(D2D1::Point2F(c.x, c.y + 5.0f));
      shadow_->SetRadiusX(r + 9.0f);
      shadow_->SetRadiusY(r + 9.0f);
      dc->FillEllipse(D2D1::Ellipse(c, r + 9.0f, r + 9.0f), shadow_.Get());
    }

    // 中心项：半径+3 accent 10% 光晕环（原型 box-shadow 0 0 0 3px accent 10%）
    const D2D1_ELLIPSE ball = D2D1::Ellipse(c, r, r);
    if (ctx.isCenter) {
      ctx.brush->SetColor(D2D1::ColorF(kAccent, 0.10f * dim));
      dc->DrawEllipse(D2D1::Ellipse(c, r + 3.0f, r + 3.0f), ctx.brush, 6.0f);
    }

    // 玻璃底：圆域裁剪层 → 高斯模糊背景（屏幕对齐）→ desk-deep 74% 染色
    bool glassDrawn = false;
    if (ctx.backdrop && ctx.backdrop->ok() && !ctx.backdrop->degraded()) {
      refreshBackdrop(dc, ctx.backdrop);
      if (bgBmp_ && blur_) {
        ComPtr<ID2D1Factory> factory;
        dc->GetFactory(&factory);
        ComPtr<ID2D1EllipseGeometry> clip;
        if (factory &&
            SUCCEEDED(factory->CreateEllipseGeometry(ball, &clip)) && clip) {
          const D2D1_RECT_F bounds =
              D2D1::RectF(c.x - r - 1.0f, c.y - r - 1.0f, c.x + r + 1.0f, c.y + r + 1.0f);
          dc->PushLayer(D2D1::LayerParameters1(bounds, clip.Get(),
                                               D2D1_ANTIALIAS_MODE_PER_PRIMITIVE),
                        nullptr);
          dc->DrawImage(blur_.Get(), D2D1::Point2F(ctx.backdropDX, ctx.backdropDY),
                        D2D1_INTERPOLATION_MODE_LINEAR);
          ctx.brush->SetColor(kDeskDeep(0.74f * dim));
          dc->FillEllipse(&ball, ctx.brush);
          dc->PopLayer();
          glassDrawn = true;
        }
      }
    }
    if (!glassDrawn) {  // 退化：纯色 74% 透明底（无模糊）
      ctx.brush->SetColor(kDeskDeep(0.74f * dim));
      dc->FillEllipse(&ball, ctx.brush);
    }

    // 顶部内高光（原型 radial-gradient at 32% 26% ink 14% → transparent 62%；
    // 跟手光：渐变中心向指针方向微移，±5px 封顶）
    if (hl_) {
      float sx = 0, sy = 0;
      if (hasPtr_) {
        sx = std::clamp((px_ - c.x) * 0.15f, -5.0f, 5.0f);
        sy = std::clamp((py_ - c.y) * 0.15f, -5.0f, 5.0f);
      }
      hl_->SetCenter(D2D1::Point2F(c.x - 0.36f * r + sx, c.y - 0.48f * r + sy));
      hl_->SetRadiusX(1.3f * r);
      hl_->SetRadiusY(1.3f * r);
      dc->FillEllipse(&ball, hl_.Get());
    }

    // 1px 描边：悬停 accent 100% > 中心 accent 60% > hairline 白 13%
    if (ctx.isHot)
      ctx.brush->SetColor(D2D1::ColorF(kAccent, 1.0f * dim));
    else if (ctx.isCenter)
      ctx.brush->SetColor(D2D1::ColorF(kAccent, 0.60f * dim));
    else
      ctx.brush->SetColor(kHairline(0.13f * dim));
    dc->DrawEllipse(&ball, ctx.brush, 1.0f);
  }

  // 详情卡底：90% 深玻璃 + 1px hairline（v1 卡不做 backdrop blur）
  void drawCardBack(ID2D1DeviceContext* dc, const D2D1_RECT_F& rect,
                    float radius) const override {
    if (!dc) return;
    ComPtr<ID2D1SolidColorBrush> brush;
    if (FAILED(dc->CreateSolidColorBrush(D2D1::ColorF(0, 0), &brush))) return;
    const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(rect, radius, radius);
    brush->SetColor(kDeskDeep(0.90f));
    dc->FillRoundedRectangle(&rr, brush.Get());
    brush->SetColor(kHairline(0.13f));
    dc->DrawRoundedRectangle(&rr, brush.Get(), 1.0f);
  }

  // 弧线描边：hairline 白 13% 底 + accent 30% 微光叠层（原型 .dock-arc .glow 同款）。
  // g.items 已是烘焙后的最终位置（tuck/让位由场景并入）。
  void drawArcStroke(ID2D1DeviceContext* dc, const DockGeom& g) const override {
    if (!dc || g.items.size() < 2) return;
    ComPtr<ID2D1SolidColorBrush> brush;
    if (FAILED(dc->CreateSolidColorBrush(D2D1::ColorF(0, 0), &brush))) return;
    for (int pass = 0; pass < 2; ++pass) {
      brush->SetColor(pass == 0 ? kHairline(0.13f)
                                : D2D1::ColorF(kAccent, 0.30f));
      for (size_t i = 0; i + 1 < g.items.size(); ++i) {
        const D2D1_POINT_2F a{ (float)g.items[i].x, (float)g.items[i].y };
        const D2D1_POINT_2F b{ (float)g.items[i + 1].x, (float)g.items[i + 1].y };
        dc->DrawLine(a, b, brush.Get(), 1.0f);
      }
    }
  }

private:
  // 设备代际变化（D3DContext 重建）时重建全部缓存资源
  void ensure(ID2D1DeviceContext* dc, const D3DContext* d3d) const {
    if (dc == seenDc_ && gen_ == d3d->generation()) return;
    seenDc_ = dc;
    gen_ = d3d->generation();
    blur_.Reset();
    bgBmp_.Reset();
    hl_.Reset();
    shadow_.Reset();
    hotGlow_.Reset();

    if (SUCCEEDED(dc->CreateEffect(kClsidGaussianBlur, &blur_))) {
      (void)blur_->SetValue(D2D1_GAUSSIANBLUR_PROP_STANDARD_DEVIATION, 6.0f);
      (void)blur_->SetValue(D2D1_GAUSSIANBLUR_PROP_BORDER_MODE,
                            D2D1_BORDER_MODE_SOFT);  // 屏缘采样钳制，防透明晕染
      (void)blur_->SetValue(D2D1_GAUSSIANBLUR_PROP_OPTIMIZATION,
                            D2D1_GAUSSIANBLUR_OPTIMIZATION_QUALITY);
    } else {
      blur_.Reset();
    }

    hl_ = makeRadial(dc, {{0.0f, kHairline(0.14f)},
                          {0.62f, kHairline(0.0f)},
                          {1.0f, kHairline(0.0f)}});
    shadow_ = makeRadial(dc, {{0.0f, D2D1::ColorF(0, 0, 0, 0.30f)},
                              {0.60f, D2D1::ColorF(0, 0, 0, 0.12f)},
                              {1.0f, D2D1::ColorF(0, 0, 0, 0.0f)}});
    hotGlow_ = makeRadial(dc, {{0.0f, D2D1::ColorF(kAccent, 0.32f)},
                               {0.50f, D2D1::ColorF(kAccent, 0.16f)},
                               {1.0f, D2D1::ColorF(kAccent, 0.0f)}});
  }

  struct Stop { float pos; D2D1_COLOR_F color; };
  static ComPtr<ID2D1RadialGradientBrush> makeRadial(
      ID2D1DeviceContext* dc, std::initializer_list<Stop> stops) {
    ComPtr<ID2D1GradientStopCollection> coll;
    std::vector<D2D1_GRADIENT_STOP> gs;
    gs.reserve(stops.size());
    for (const Stop& s : stops) gs.push_back(D2D1::GradientStop(s.pos, s.color));
    if (FAILED(dc->CreateGradientStopCollection(gs.data(), (UINT32)gs.size(), &coll)))
      return nullptr;
    ComPtr<ID2D1RadialGradientBrush> brush;
    if (FAILED(dc->CreateRadialGradientBrush(
            D2D1::RadialGradientBrushProperties(D2D1::Point2F(0, 0),
                                                D2D1::Point2F(0, 0), 1.0f, 1.0f),
            coll.Get(), &brush)))
      return nullptr;
    return brush;
  }

  // 背景帧纹理 → ID2D1Bitmap1（dirty/首帧/设备重建时重建），并挂到模糊 effect 输入
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
    if (blur_) blur_->SetInput(0, bgBmp_.Get());
    backdrop->markClean();
  }

  // 设备相关缓存（const 接口下的内部状态；代际由 ensure 把关）
  mutable const ID2D1DeviceContext* seenDc_ = nullptr;
  mutable unsigned gen_ = 0;
  mutable ComPtr<ID2D1Effect> blur_;                  // 高斯模糊（输入=背景位图）
  mutable ComPtr<ID2D1Bitmap1> bgBmp_;                // 最新背景帧位图
  mutable ComPtr<ID2D1RadialGradientBrush> hl_;       // 顶部内高光
  mutable ComPtr<ID2D1RadialGradientBrush> shadow_;   // 底部外阴影
  mutable ComPtr<ID2D1RadialGradientBrush> hotGlow_;  // 悬停 accent 外发光
  mutable float px_ = 0, py_ = 0;                     // 跟手光光源位置
  mutable bool hasPtr_ = false;
};

} // namespace

void registerDarkMaterial(Registry<IMaterial>& reg) {
  reg.add("dark", [] { return std::unique_ptr<IMaterial>(new DarkMaterial()); });
}

} // namespace okmeter::render
