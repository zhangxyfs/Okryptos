// render/forms/capsule.cpp —— 胶囊量表形态：174px 胶囊直线排布（step 56），
// 左模型名右紧凑值，底部 3px 占比条（值/最大值，中心项 accent）。
// 胶囊底（圆角矩形玻璃/描边/光晕）委托给材质（OrbStyleCtx.halfW > r 走胶囊分支）。
#include "../form.h"
#include "../material.h"

namespace okmeter::render {
namespace {

constexpr double kCapHalfW = 87;   // 胶囊半宽（174px）
constexpr double kCapHalfH = 20;   // 胶囊半高（原型 .cap 高 40：8+15+5+3+9）

class CapsuleForm final : public IForm {
public:
  std::string id() const override { return "capsule"; }

  DockGeom layout(int n, int screenH, const std::string& edge) const override {
    // 尺寸全部由 layoutCapsule 按 uiScale(screenH) 缩放（比例法）
    return layoutCapsule(n, screenH, edge);
  }

  double hoverPush() const override { return 0; }      // 胶囊间距已大，推挤邻项只会抖
  double cardRadius(int, int) const override { return 22; }  // 原型 RADII capsule

  void drawItems(ID2D1DeviceContext* dc, const DrawContext& ctx) const override {
    if (!dc || !ctx.geom || !ctx.items || !ctx.material || !ctx.brush ||
        !ctx.capNameFmt || !ctx.capValFmt) return;
    const DockGeom& g = *ctx.geom;
    const size_t n = g.items.size();
    const double e = ctx.e < 0 ? 0 : ctx.e > 1 ? 1 : ctx.e;
    const float dimBase = (float)(0.55 + 0.45 * e);  // 收缩态 55%（原型同款）
    bool anyHot = false;  // 有悬停项时非悬停项降暗（原型 .dim）
    for (const ItemGeom& it : g.items) anyHot = anyHot || it.scale > 1.001;

    for (size_t i = 0; i < n; ++i) {
      const ItemGeom& it = g.items[i];
      const D2D1_POINT_2F c{ (float)it.x, (float)it.y };
      const bool isHot = it.scale > 1.001;
      const float dim = dimBase * ((anyHot && !isHot) ? (float)hoverDimOpacity() : 1.0f);  // 原型 .dim 降暗

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
        const float u = (float)g.scale;  // uiScale：内部尺寸 = 原型值 × 比例
        const float hw = (float)(it.hw * it.scale), hh = (float)(it.r * it.scale);
        const float l = c.x - hw, t = c.y - hh;
        const float rgt = c.x + hw;
        // 顶行：左名 右值，padding 左右 13、上 8（原型 .cap .top）×比例
        if (!di.label.empty()) {
          ctx.brush->SetColor(inkLight( 0.66f * dim));
          const D2D1_RECT_F tr = D2D1::RectF(l + 13.0f * u, t + 8.0f * u,
                                             rgt - 80.0f * u, t + 24.0f * u);
          drawTextTrimmed(*ctx.d3d, dc, ctx.brush, di.label, ctx.capNameFmt, tr);
        }
        if (!di.value.empty()) {
          ctx.brush->SetColor(inkLight( 0.93f * dim));
          const D2D1_RECT_F tr = D2D1::RectF(l + 80.0f * u, t + 8.0f * u,
                                             rgt - 13.0f * u, t + 24.0f * u);
          dc->DrawText(di.value.c_str(), (UINT32)di.value.size(), ctx.capValFmt,
                       &tr, ctx.brush);
        }
        // 底部 3px 占比条：轨道 ink 10%，填充 ink 52%（中心项 accent），
        // 宽 max(4%, ratio)（原型 .cap .bar 同款）×比例
        const float bl = l + 13.0f * u, br = rgt - 13.0f * u;
        const float by = t + 2.0f * hh - 12.0f * u;  // 底 padding 9 + 条高 3
        ctx.brush->SetColor(inkLight( 0.10f * dim));  // 轨道：亮底深色
        dc->FillRoundedRectangle(D2D1::RoundedRect(D2D1::RectF(bl, by, br, by + 3.0f * u),
                                                   1.5f * u, 1.5f * u),
                                 ctx.brush);
        double ratio = di.ratio;
        if (ratio < 0.04) ratio = 0.04;
        if (ratio > 1.0) ratio = 1.0;
        const float fw = (br - bl) * (float)ratio;
        if ((int)i == ctx.mid)
          ctx.brush->SetColor(D2D1::ColorF(0x5FE0A8, dim));  // 原型 accent 绿
        else
          ctx.brush->SetColor(inkLight( 0.52f * dim));  // 填充：亮底深色
        dc->FillRoundedRectangle(
            D2D1::RoundedRect(D2D1::RectF(bl, by, bl + fw, by + 3.0f * u),
                              1.5f * u, 1.5f * u),
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
