// render/forms/capsule.cpp —— 胶囊量表形态：174px 胶囊直线排布（step 56），
// 左模型名右紧凑值，底部 3px 占比条（值/最大值，中心项 accent）。
// 胶囊底（圆角矩形玻璃/描边/光晕）委托给材质（OrbStyleCtx.halfW > r 走胶囊分支）。
#include "../form.h"
#include "../material.h"

namespace okmeter::render {
namespace {

constexpr double kCapHalfW = 87;   // 胶囊半宽（174px）
constexpr double kCapHalfH = 19;   // 胶囊半高

class CapsuleForm final : public IForm {
public:
  std::string id() const override { return "capsule"; }

  DockGeom layout(int n, int screenH, const std::string& edge) const override {
    (void)screenH;  // 直线排布固定 step，与屏高无关（原型 geom() capsule 同款）
    return layoutCapsule(n, edge);
  }

  double hoverScale() const override { return 1.08; }  // 原型 .cap.hot scale(1.08)
  double cardRadius(int, int) const override { return 22; }  // 原型 RADII capsule

  void drawItems(ID2D1DeviceContext* dc, const DrawContext& ctx) const override {
    if (!dc || !ctx.geom || !ctx.items || !ctx.material || !ctx.brush ||
        !ctx.capNameFmt || !ctx.capValFmt) return;
    const DockGeom& g = *ctx.geom;
    const size_t n = g.items.size();
    const double e = ctx.e < 0 ? 0 : ctx.e > 1 ? 1 : ctx.e;
    const float dim = (float)(0.55 + 0.45 * e);  // 收缩态 55%（原型同款）

    for (size_t i = 0; i < n; ++i) {
      const ItemGeom& it = g.items[i];
      const D2D1_POINT_2F c{ (float)it.x, (float)it.y };
      const bool isHot = it.scale > 1.001;

      // 胶囊底（材质）：hw/r 已并入悬停放大，背景采样与屏幕对齐不被放大
      OrbStyleCtx osc{};
      osc.d3d = ctx.d3d;
      osc.backdrop = ctx.backdrop;
      osc.brush = ctx.brush;
      osc.center = c;
      osc.r = (float)(it.r * it.scale);
      osc.halfW = (float)(it.hw * it.scale);
      osc.isCenter = (int)i == ctx.mid;
      osc.isHot = isHot;
      osc.dimmed = dim;
      osc.backdropDX = ctx.backdropDX;
      osc.backdropDY = ctx.backdropDY;
      ctx.material->drawOrbBack(dc, osc);

      // 胶囊内容：悬停项整体放大（含文本，与原型 transform: scale 同款）
      if (isHot)
        dc->SetTransform(D2D1::Matrix3x2F::Scale((float)it.scale, (float)it.scale, c));
      if (i < ctx.items->size()) {
        const DockItem& di = (*ctx.items)[i];
        const float l = c.x - (float)kCapHalfW, t = c.y - (float)kCapHalfH;
        const float rgt = c.x + (float)kCapHalfW;
        // 顶行：左名（白 66%）右值（白 93%），padding 左右 13、上 8（原型 .cap .top）
        if (!di.label.empty()) {
          ctx.brush->SetColor(D2D1::ColorF(1.0f, 1.0f, 1.0f, 0.66f * dim));
          const D2D1_RECT_F tr = D2D1::RectF(l + 13.0f, t + 5.0f, rgt - 80.0f, t + 21.0f);
          dc->DrawText(di.label.c_str(), (UINT32)di.label.size(), ctx.capNameFmt,
                       &tr, ctx.brush,
                       D2D1_DRAW_TEXT_OPTIONS_NONE,
                       DWRITE_MEASURING_MODE_NATURAL);
        }
        if (!di.value.empty()) {
          ctx.brush->SetColor(D2D1::ColorF(1.0f, 1.0f, 1.0f, 0.93f * dim));
          const D2D1_RECT_F tr = D2D1::RectF(l + 80.0f, t + 5.0f, rgt - 13.0f, t + 21.0f);
          dc->DrawText(di.value.c_str(), (UINT32)di.value.size(), ctx.capValFmt,
                       &tr, ctx.brush);
        }
        // 底部 3px 占比条：轨道 ink 10%，填充 ink 52%（中心项 accent），
        // 宽 max(4%, ratio)（原型 .cap .bar 同款）
        const float bl = l + 13.0f, br = rgt - 13.0f;
        const float by = t + 2.0f * (float)kCapHalfH - 12.0f;  // 底 padding 9 + 条高 3
        ctx.brush->SetColor(D2D1::ColorF(0.93f, 0.94f, 0.96f, 0.10f * dim));
        dc->FillRoundedRectangle(D2D1::RoundedRect(D2D1::RectF(bl, by, br, by + 3.0f),
                                                   1.5f, 1.5f),
                                 ctx.brush);
        double ratio = di.ratio;
        if (ratio < 0.04) ratio = 0.04;
        if (ratio > 1.0) ratio = 1.0;
        const float fw = (br - bl) * (float)ratio;
        if ((int)i == ctx.mid)
          ctx.brush->SetColor(D2D1::ColorF(0x5FE0A8, dim));  // 原型 accent 绿
        else
          ctx.brush->SetColor(D2D1::ColorF(0.93f, 0.94f, 0.96f, 0.52f * dim));
        dc->FillRoundedRectangle(
            D2D1::RoundedRect(D2D1::RectF(bl, by, bl + fw, by + 3.0f), 1.5f, 1.5f),
            ctx.brush);
      }
      if (isHot) dc->SetTransform(D2D1::Matrix3x2F::Identity());
    }
  }
};

} // namespace

std::unique_ptr<IForm> createCapsuleForm() {
  return std::unique_ptr<IForm>(new CapsuleForm());
}

} // namespace okmeter::render
