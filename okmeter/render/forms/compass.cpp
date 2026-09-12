// render/forms/compass.cpp —— 星环罗盘形态：中心 86px 罗盘（r=43）+ 卫星 46px
// 沿 R=84 圆周均布；收缩态缓慢旋转（3.6°/s × (1-e)，展开态静止）；虚线圆环轨道。
// 旋转角度由 tick 推进、layout 并入卫星位置（tuck 烘焙在旋转之后，收缩态卫星沿
// 露出条上下滑动出入）；球底/描边委托材质。
#include "../form.h"
#include "../material.h"

namespace okmeter::render {
namespace {

class CompassForm final : public IForm {
public:
  std::string id() const override { return "compass"; }

  DockGeom layout(int n, int screenH, const std::string& edge) const override {
    (void)screenH;
    (void)edge;  // 环形布局左右缘对称，edge 只影响 tuck 烘焙方向
    // 展开态（e≈1）不旋转，位置回到基准角（悬停命中/卡锚定与绘制一致）
    return layoutCompass(n, rot_ * (1.0 - lastE_), screenH);
  }

  // 收缩态缓慢旋转（原型 spinLoop：rot += dt_ms * 0.0036° = 3.6°/s；展开静止）
  void tick(double dt, double e) override {
    lastE_ = e;
    if (e < 0.999) {
      rot_ += dt * 3.6 * (1.0 - e);
      if (rot_ >= 360.0) rot_ -= 360.0;
    }
  }

  // 收缩态旋转需持续重绘（展开静止后停帧）
  bool wantsTick() const override { return lastE_ < 0.999; }

  double hoverPush() const override { return 0; }      // 环形排布不推挤邻项
  // 原型 RADII：罗盘中心 43 / 卫星 23
  double cardRadius(int idx, int mid) const override { return idx == mid ? 43 : 23; }

  void drawItems(ID2D1DeviceContext* dc, const DrawContext& ctx) const override {
    if (!dc || !ctx.geom || !ctx.items || !ctx.material || !ctx.brush ||
        !ctx.hubFmt || !ctx.satFmt || !ctx.labelFmt) return;
    const DockGeom& g = *ctx.geom;
    const size_t n = g.items.size();
    if (ctx.mid < 0 || (size_t)ctx.mid >= n) return;
    const double e = ctx.e < 0 ? 0 : ctx.e > 1 ? 1 : ctx.e;
    const float dimBase = (float)(0.55 + 0.45 * e);  // 收缩态 55%（原型同款）
    bool anyHot = false;  // 有悬停项时非悬停项降暗（原型 .dim）
    for (const ItemGeom& it : g.items) anyHot = anyHot || it.scale > 1.001;

    // 虚线圆环轨道（原型 arcSvg circle r=84 stroke hairline dasharray 2 4；
    // 轨道跟随罗盘中心烘焙位置，收缩态不透明度随 e 衰减 .35+.65e）
    const ItemGeom& hub = g.items[(size_t)ctx.mid];
    if (ensureDash(dc)) {
      ctx.brush->SetColor(
          inkOn(ctx.material ? ctx.material->backdropLuma() : 0.0f,
                                   0.13f * (float)(0.35 + 0.65 * e)));
      const float R84 = 84.0f * (float)g.scale;  // 轨道半径 ×比例
      dc->DrawEllipse(D2D1::Ellipse(D2D1::Point2F((float)hub.x, (float)hub.y),
                                    R84, R84),
                      ctx.brush, 1.0f * (float)g.scale, dash_.Get());
    }

    for (size_t i = 0; i < n; ++i) {
      const ItemGeom& it = g.items[i];
      const D2D1_POINT_2F c{ (float)it.x, (float)it.y };
      const bool isHub = (int)i == ctx.mid;
      const bool isHot = it.scale > 1.001;
      const float dim = dimBase * ((anyHot && !isHot) ? (float)hoverDimOpacity() : 1.0f);  // 原型 .dim 降暗

      // 球底（材质）：r 已并入悬停放大，背景采样与屏幕对齐不被放大
      OrbStyleCtx osc{};
      osc.d3d = ctx.d3d;
      osc.backdrop = ctx.backdrop;
      osc.brush = ctx.brush;
      osc.center = c;
      osc.r = (float)(it.r * it.scale);
      osc.isCenter = isHub;  // 中心罗盘：accent 描边 + 光晕环（原型 .hub border/box-shadow）
      osc.isHot = isHot;
      osc.dimmed = dim;
      osc.backdropDX = ctx.backdropDX;
      osc.backdropDY = ctx.backdropDY;
      ctx.material->drawOrbBack(dc, osc);

      // 球内双行文本：悬停项整体放大（与原型 transform: scale 同款）；
      // 卫星文字不随轨道旋转（原型 sat-rot 反向旋转同款语义）
      if (isHot)
        dc->SetTransform(D2D1::Matrix3x2F::Scale((float)it.scale, (float)it.scale, c));
      if (i < ctx.items->size()) {
        const DockItem& di = (*ctx.items)[i];
        const float r = (float)it.r;
        const float u = (float)g.scale;  // uiScale：内部偏移 ×比例
        const float luma = ctx.material ? ctx.material->backdropLuma() : 0.0f;
        if (isHub) {
          if (!di.value.empty()) {
            ctx.brush->SetColor(inkOn(luma, 0.93f * dim));
            const D2D1_RECT_F tr = D2D1::RectF(c.x - r, c.y - 18.0f * u, c.x + r, c.y + 2.0f * u);
            dc->DrawText(di.value.c_str(), (UINT32)di.value.size(), ctx.hubFmt,
                         &tr, ctx.brush);
          }
          if (!di.label.empty()) {
            ctx.brush->SetColor(inkOn(luma, 0.66f * dim));
            const D2D1_RECT_F tr = D2D1::RectF(c.x - r, c.y + 3.0f * u, c.x + r, c.y + 16.0f * u);
            dc->DrawText(di.label.c_str(), (UINT32)di.label.size(), ctx.labelFmt,
                         &tr, ctx.brush);
          }
        } else {
          if (!di.value.empty()) {
            ctx.brush->SetColor(inkOn(luma, 0.93f * dim));
            const D2D1_RECT_F tr = D2D1::RectF(c.x - r, c.y - 12.0f * u, c.x + r, c.y + 1.0f * u);
            dc->DrawText(di.value.c_str(), (UINT32)di.value.size(), ctx.satFmt,
                         &tr, ctx.brush);
          }
          if (!di.label.empty()) {
            ctx.brush->SetColor(inkOn(luma, 0.66f * dim));
            const D2D1_RECT_F tr = D2D1::RectF(c.x - r, c.y + 1.0f * u, c.x + r, c.y + 12.0f * u);
            dc->DrawText(di.label.c_str(), (UINT32)di.label.size(), ctx.labelFmt,
                         &tr, ctx.brush);
          }
        }
      }
      if (isHot) dc->SetTransform(D2D1::Matrix3x2F::Identity());
    }
  }

private:
  // 虚线轨道描边样式（设备无关资源，随首个 dc 创建一次）
  bool ensureDash(ID2D1DeviceContext* dc) const {
    if (dash_) return true;
    Microsoft::WRL::ComPtr<ID2D1Factory> factory;
    dc->GetFactory(&factory);
    if (!factory) return false;
    const float dashes[] = { 2.0f, 4.0f };
    D2D1_STROKE_STYLE_PROPERTIES props{};
    props.dashStyle = D2D1_DASH_STYLE_CUSTOM;
    return SUCCEEDED(factory->CreateStrokeStyle(&props, dashes, 2, &dash_));
  }

  mutable Microsoft::WRL::ComPtr<ID2D1StrokeStyle> dash_;
  mutable double rot_ = 0;     // 累积旋转角（deg）
  mutable double lastE_ = 0;   // 最近 tick 的展开进度（初始收缩态）
};

} // namespace

std::unique_ptr<IForm> createCompassForm() {
  return std::unique_ptr<IForm>(new CompassForm());
}

} // namespace okmeter::render
