// render/forms/arc.cpp —— 弧线形态：球体弧线布局 + 球内双行文本。
// 球底/描边委托给材质；本形态只管项位置、悬停放大与文本。
#include "../form.h"
#include "../material.h"

namespace okmeter::render {
namespace {

class ArcForm final : public IForm {
public:
  std::string id() const override { return "arc"; }
  double hoverDimShrink() const override { return 1.0; }   // 用户裁决：悬停他项不回缩（原型 .orb.dim scale(.9) 不采用）
  double hoverDimOpacity() const override { return 0.6; }  // 原型 .orb.dim opacity .6

  // 球体弧线：半径 30、中心项最靠屏内（布局细节见 ui/geometry.cpp）
  DockGeom layout(int n, int screenW, int screenH, const std::string& edge) const override {
    return layoutArc(n, 30, 14, screenW, screenH, edge);
  }

  void drawItems(ID2D1DeviceContext* dc, const DrawContext& ctx) const override {
    if (!dc || !ctx.geom || !ctx.items || !ctx.material || !ctx.brush ||
        !ctx.valueFmt || !ctx.labelFmt) return;
    const DockGeom& g = *ctx.geom;
    const size_t n = g.items.size();
    const double e = ctx.e < 0 ? 0 : ctx.e > 1 ? 1 : ctx.e;
    const bool hz = ctx.mini != nullptr;  // 横向：mini⇄stage morph 绘制
    const float dimBase = hz ? 1.0f : (float)(0.55 + 0.45 * e);  // 横向全额（无 55% 降暗）
    const float fe = hz ? (float)e : 1.0f;  // 形态内容渐隐（chip 文字反向渐显）
    bool anyHot = false;  // 有悬停项时非悬停项降暗（原型 .dim）
    for (const ItemGeom& it : g.items) anyHot = anyHot || it.scale > 1.001;

    for (size_t i = 0; i < n; ++i) {
      const ItemGeom& it = g.items[i];
      const D2D1_POINT_2F c{ (float)it.x, (float)it.y };
      const bool isHot = it.scale > 1.001;
      const float dim = dimBase * ((anyHot && !isHot) ? (float)hoverDimOpacity() : 1.0f);  // 原型 .dim 降暗
      const float fade = dim * fe;  // 形态内容墨色（横向随 e 渐隐）

      // 球底（材质）：r 已并入悬停放大，背景采样与屏幕对齐不被放大
      OrbStyleCtx osc{};
      osc.d3d = ctx.d3d;
      osc.backdrop = ctx.backdrop;
      osc.brush = ctx.brush;
      osc.center = c;
      osc.r = (float)(it.r * it.scale);
      osc.halfW = (float)(it.hw * it.scale);  // morph 后 hw 渐增 → 材质胶囊分支自动接管
      osc.isCenter = (int)i == ctx.mid;
      osc.isHot = isHot;
      osc.dimmed = dim;
      osc.backdropDX = ctx.backdropDX;
      osc.backdropDY = ctx.backdropDY;
      if (hz)  // pill→球角插值（圆形项材质忽略 cornerR，直接传插值即可）
        osc.cornerR = (float)((*ctx.mini).items[i].r +
                              (it.r - (*ctx.mini).items[i].r) * e);
      ctx.material->drawOrbBack(dc, osc);

      // 球内双行文本：悬停项整体放大（含文本，与原型 transform: scale 同款）
      if (isHot)
        dc->SetTransform(D2D1::Matrix3x2F::Scale((float)it.scale, (float)it.scale, c));
      if (i < ctx.items->size()) {
        const DockItem& di = (*ctx.items)[i];
        const float r = (float)it.r;
        const float u = (float)g.scale;  // uiScale：内部偏移 ×比例
        const bool halo = ctx.material && ctx.material->id() == "liquid";
        if (!di.value.empty()) {
          const D2D1_RECT_F tr = D2D1::RectF(c.x - r, c.y - 16.0f * u, c.x + r, c.y + 1.0f * u);
          if (halo) liquidHalo(dc, ctx.brush, di.value, ctx.valueFmt, tr, (float)fade);
          ctx.brush->SetColor(inkLight( 0.93f * fade));
          dc->DrawText(di.value.c_str(), (UINT32)di.value.size(), ctx.valueFmt,
                       &tr, ctx.brush);
        }
        if (!di.label.empty()) {
          const D2D1_RECT_F tr = D2D1::RectF(c.x - r, c.y + 2.0f * u, c.x + r, c.y + 15.0f * u);
          if (halo) liquidHalo(dc, ctx.brush, di.label, ctx.labelFmt, tr, (float)fade);
          ctx.brush->SetColor(inkLight( 0.66f * fade));
          dc->DrawText(di.label.c_str(), (UINT32)di.label.size(), ctx.labelFmt,
                       &tr, ctx.brush);
        }
      }
      if (isHot) dc->SetTransform(D2D1::Matrix3x2F::Identity());
      if (hz) drawChipText(ctx, dc, i, c.x, c.y, (float)(1.0 - e));
    }
  }
};

} // namespace

std::unique_ptr<IForm> createArcForm() {
  return std::unique_ptr<IForm>(new ArcForm());
}

} // namespace okmeter::render
