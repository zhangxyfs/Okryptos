// render/materials/frost.cpp —— 毛玻璃材质：乳白磨砂球底（WGC 背景强模糊 σ11≈CSS
// blur(22px) + 提饱和 saturate(150%)→D2D 0.75 + 白 12% 染色）+ 顶部内高光
//（ink 16%）+ ink 26% 描边（悬停/中心 accent 优先，原型同款规则）。
// 背景捕获不可用（未启动/失败/affinity 降级）时退化为 desk-deep 55% + 白 12% 纯色底。
#include "glassfx.h"
#include <dxgi.h>

using Microsoft::WRL::ComPtr;

namespace okmeter::render {
namespace {

class FrostMaterial final : public IMaterial {
public:
  std::string id() const override { return "frost"; }
  float backdropLuma() const override { return lastLuma_; }

  void onPointer(float x, float y) const override {
    px_ = x;
    py_ = y;
    hasPtr_ = true;
  }
  void onPointerLeave() const override { hasPtr_ = false; }

  // 球体底：悬停外发光 → 中心光晕环 → 磨砂玻璃底（强模糊 + 提饱和 + 白 12%）
  // → 顶部内高光 → 1px 描边。原型 frost 无外阴影（box-shadow 仅 inset 高光）。
  // halfW>r 时项为胶囊：填充/裁剪/描边走圆角矩形，光晕/高光按椭圆横向外扩。
  void drawOrbBack(ID2D1DeviceContext* dc, const OrbStyleCtx& ctx) const override {
    if (!dc || !ctx.d3d || !ctx.brush || ctx.r <= 0) return;
    ensure(dc, ctx.d3d);
    const D2D1_POINT_2F c = ctx.center;
    const float r = ctx.r;
    const float hw = ctx.halfW > 0 ? ctx.halfW : ctx.r;
    const float dim = ctx.dimmed;

    // 悬停外发光（原型 .orb.hot box-shadow 0 0 22px accent 32%）
    if (ctx.isHot && hotGlow_ && !glassfx::isPill(hw, r)) {  // 椭圆光晕仅圆项（原型 .orb.hot 专属；块状 hot 仅描边）
      hotGlow_->SetCenter(c);
      hotGlow_->SetRadiusX(glassfx::shapeRX(hw, r, 22.0f));
      hotGlow_->SetRadiusY(r + 22.0f);
      dc->FillEllipse(
          D2D1::Ellipse(c, glassfx::shapeRX(hw, r, 22.0f), r + 22.0f), hotGlow_.Get());
    }

    // 中心项：半径+3 accent 10% 光晕环（原型 .orb.center box-shadow）
    if (ctx.isCenter && !glassfx::isPill(hw, r)) {  // 中心光晕环仅圆项（原型 .orb.center 专属）
      ctx.brush->SetColor(glassfx::accentC(0.10f * dim));
      glassfx::drawShape(dc, c, hw, r, ctx.brush, 6.0f, 3.0f, ctx.cornerR);
    }

    // 磨砂玻璃底：形状域裁剪层 → 模糊+提饱和背景（屏幕对齐）→ 白 12% 染色
    bool glassDrawn = false;
    if (ctx.backdrop && ctx.backdrop->ok() && !ctx.backdrop->degraded()) {
      pipe_.refresh(dc, ctx.backdrop);
      lastLuma_ = ctx.backdrop->luma();
      if (pipe_.ready() && glassfx::pushShapeClip(dc, c, hw, r, ctx.cornerR)) {
        dc->DrawImage(pipe_.output(), D2D1::Point2F(ctx.backdropDX, ctx.backdropDY),
                      D2D1_INTERPOLATION_MODE_LINEAR);
        ctx.brush->SetColor(glassfx::ink(0.12f * dim));
        glassfx::fillShape(dc, c, hw, r, ctx.brush, ctx.cornerR);
        dc->PopLayer();
        glassDrawn = true;
      }
    }
    if (!glassDrawn) {  // 退化：desk-deep 55% + 白 12% 纯色底（无模糊）
      ctx.brush->SetColor(glassfx::deskDeep(0.55f * dim));
      glassfx::fillShape(dc, c, hw, r, ctx.brush, ctx.cornerR);
      ctx.brush->SetColor(glassfx::ink(0.12f * dim));
      glassfx::fillShape(dc, c, hw, r, ctx.brush, ctx.cornerR);
    }

    // 顶部内高光（原型 inset 0 1px 0 ink 16%；跟手光中心向指针微移 ±5px）
    if (hl_) {
      float sx = 0, sy = 0;
      if (hasPtr_) {
        sx = std::clamp((px_ - c.x) * 0.15f, -5.0f, 5.0f);
        sy = std::clamp((py_ - c.y) * 0.15f, -5.0f, 5.0f);
      }
      hl_->SetCenter(D2D1::Point2F(c.x - 0.36f * r + sx, c.y - 0.48f * r + sy));
      hl_->SetRadiusX(1.3f * hw);
      hl_->SetRadiusY(1.3f * r);
      glassfx::fillShape(dc, c, hw, r, hl_.Get(), ctx.cornerR);
    }

    // 1px 描边：悬停 accent 100% > 中心 accent 60% > ink 26%（原型 frost border）
    if (ctx.isHot && !glassfx::isPill(hw, r))  // 块状项悬停不要 accent 描边（用户裁决：悬停=他项降暗）
      ctx.brush->SetColor(glassfx::accentC(1.0f * dim));
    else if (ctx.isCenter)
      ctx.brush->SetColor(glassfx::accentC(0.60f * dim));
    else
      ctx.brush->SetColor(glassfx::ink(0.26f * dim));
    glassfx::drawShape(dc, c, hw, r, ctx.brush, 1.0f, 0.0f, ctx.cornerR);
  }

  // 详情卡底：88% 深玻璃 + 白 10% 提亮 + ink 28% 描边（v1 卡不做 backdrop blur）
  void drawCardBack(ID2D1DeviceContext* dc, const D2D1_RECT_F& rect,
                    float radius) const override {
    if (!dc) return;
    ComPtr<ID2D1SolidColorBrush> brush;
    if (FAILED(dc->CreateSolidColorBrush(D2D1::ColorF(0, 0), &brush))) return;
    const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(rect, radius, radius);
    brush->SetColor(glassfx::deskDeep(0.88f));
    dc->FillRoundedRectangle(&rr, brush.Get());
    brush->SetColor(glassfx::ink(0.10f));
    dc->FillRoundedRectangle(&rr, brush.Get());
    brush->SetColor(glassfx::ink(0.28f));
    dc->DrawRoundedRectangle(&rr, brush.Get(), 1.0f);
  }

  // 弧线描边：ink 24% 双pass（原型 frost 下 base/glow 两 path 同为 ink 24%）；
  // connector=false（胶囊/罗盘）不画
  void drawArcStroke(ID2D1DeviceContext* dc, const DockGeom& g,
                     const std::string& edge) const override {
    (void)edge;
    if (!dc || !g.connector || g.items.size() < 2) return;
    ComPtr<ID2D1SolidColorBrush> brush;
    if (FAILED(dc->CreateSolidColorBrush(D2D1::ColorF(0, 0), &brush))) return;
    brush->SetColor(glassfx::ink(0.24f));
    for (int pass = 0; pass < 2; ++pass)
      for (size_t i = 0; i + 1 < g.items.size(); ++i) {
        const D2D1_POINT_2F a{ (float)g.items[i].x, (float)g.items[i].y };
        const D2D1_POINT_2F b{ (float)g.items[i + 1].x, (float)g.items[i + 1].y };
        dc->DrawLine(a, b, brush.Get(), 1.0f);
      }
  }

private:
  void ensure(ID2D1DeviceContext* dc, const D3DContext* d3d) const {
    if (dc == seenDc_ && gen_ == d3d->generation()) return;
    seenDc_ = dc;
    gen_ = d3d->generation();
    pipe_.reset();
    hl_.Reset();
    hotGlow_.Reset();
    // σ11 ≈ CSS blur(22px)；CSS saturate(150%) → D2D 0.75
    (void)pipe_.ensure(dc, 11.0f, 0.75f);
    hl_ = glassfx::radial(dc, {{0.0f, glassfx::ink(0.16f)},
                               {0.62f, glassfx::ink(0.0f)},
                               {1.0f, glassfx::ink(0.0f)}});
    hotGlow_ = glassfx::radial(dc, {{0.0f, glassfx::accentC(0.32f)},
                                    {0.50f, glassfx::accentC(0.16f)},
                                    {1.0f, glassfx::accentC(0.0f)}});
  }

  mutable const ID2D1DeviceContext* seenDc_ = nullptr;
  mutable unsigned gen_ = 0;
  mutable glassfx::BackdropPipe pipe_;            // 模糊+提饱和背景管线
  mutable float lastLuma_ = 0.0f;                  // 最近背景帧亮度（自适应墨色）
  mutable ComPtr<ID2D1RadialGradientBrush> hl_;   // 顶部内高光
  mutable ComPtr<ID2D1RadialGradientBrush> hotGlow_;
  mutable float px_ = 0, py_ = 0;
  mutable bool hasPtr_ = false;
};

} // namespace

std::unique_ptr<IMaterial> createFrostMaterial() {
  return std::unique_ptr<IMaterial>(new FrostMaterial());
}

} // namespace okmeter::render
