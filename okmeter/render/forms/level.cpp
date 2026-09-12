// render/forms/level.cpp —— 电平柱形态：184px 圆角块直线排布（step 68），
// 顶行左名右值，下方 12 段电平条按占比点亮（值/最大值，中心项 accent）。
// 块底（圆角矩形玻璃/描边/光晕）委托材质（OrbStyleCtx.halfW > r 走胶囊分支）。
#include "../form.h"
#include "../material.h"
#include <algorithm>
#include <cmath>

namespace okmeter::render {
namespace {

constexpr UINT32 kAccent = 0x5FE0A8;
constexpr double kStep = 68;     // 原型 geom() level：step 68
constexpr double kW = 206;       // 原型 W=206
constexpr float kItemHalfW = 92.0f;   // 块半宽（184/2）
constexpr float kItemHalfH = 25.0f;   // 块半高（padding 8+顶行 ~15+间距 6+段 10+padding 10）

class LevelForm final : public IForm {
public:
  std::string id() const override { return "level"; }

  DockGeom layout(int n, int screenH, const std::string& edge) const override {
    DockGeom g;
    if (n < 1) return g;
    const int mid = (n - 1) / 2;
    const double s = uiScale(screenH);  // 比例法：原型值 × 屏高/1080
    g.scale = s;
    g.w = kW * s;
    g.h = 2 * (mid * kStep * s + 58 * s);
    g.connector = false;  // 原型 level 隐藏 arcSvg
    g.items.resize((size_t)n);
    for (int i = 0; i < n; ++i) {
      g.items[(size_t)i].x = g.w / 2;
      g.items[(size_t)i].y = g.h / 2 + (i - mid) * kStep * s;
      g.items[(size_t)i].r = (float)(kItemHalfH * s);
      g.items[(size_t)i].hw = (float)(kItemHalfW * s);
    }
    (void)edge;
    return g;
  }

  double hoverScale() const override { return 1.07; }  // 原型 .lvl.hot scale(1.07)
  double hoverPush() const override { return 0; }      // 块间距已大，推挤只会抖
  double cardRadius(int, int) const override { return 20; }  // 原型 RADII level

  void drawItems(ID2D1DeviceContext* dc, const DrawContext& ctx) const override {
    if (!dc || !ctx.geom || !ctx.items || !ctx.material || !ctx.brush ||
        !ctx.capNameFmt || !ctx.capValFmt) return;
    const DockGeom& g = *ctx.geom;
    const size_t n = g.items.size();
    const double e = ctx.e < 0 ? 0 : ctx.e > 1 ? 1 : ctx.e;
    const float dim = (float)(0.55 + 0.45 * e);  // 收缩态 55%（原型同款）
    const float luma = ctx.material->backdropLuma();

    for (size_t i = 0; i < n; ++i) {
      const ItemGeom& it = g.items[i];
      const D2D1_POINT_2F c{ (float)it.x, (float)it.y };
      const bool isHot = it.scale > 1.001;

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
      osc.cornerR = 10.0f * (float)g.scale;  // 块状角半径（原型 border-radius）×比例
      if (isHot)
        dc->SetTransform(D2D1::Matrix3x2F::Scale((float)it.scale, (float)it.scale, c));
      ctx.material->drawOrbBack(dc, osc);
      dc->SetTransform(D2D1::IdentityMatrix());

      if (i < ctx.items->size()) {
        const DockItem& di = (*ctx.items)[i];
        const float u = (float)g.scale;  // uiScale：内部尺寸 = 原型值 × 比例
        const float l = c.x - (float)(it.hw * it.scale), rgt = c.x + (float)(it.hw * it.scale);
        // 顶行：左名（10px）右值（11px mono），padding 13（原型 .lvl .top）×比例
        if (!di.label.empty()) {
          ctx.brush->SetColor(inkOn(luma, 0.66f * dim));
          const D2D1_RECT_F tr = D2D1::RectF(l + 13.0f * u, c.y - 17.0f * u,
                                             rgt - 80.0f * u, c.y - 2.0f * u);
          drawTextTrimmed(*ctx.d3d, dc, ctx.brush, di.label, ctx.capNameFmt, tr);
        }
        if (!di.value.empty()) {
          ctx.brush->SetColor(inkOn(luma, 0.93f * dim));
          const D2D1_RECT_F tr = D2D1::RectF(l + 80.0f * u, c.y - 17.0f * u,
                                             rgt - 13.0f * u, c.y - 2.0f * u);
          dc->DrawText(di.value.c_str(), (UINT32)di.value.size(), ctx.capValFmt,
                       &tr, ctx.brush);
        }
        // 12 段电平条（原型 .lvl .segs：gap 2、高 10；点亮 = ink 55%，中心 accent）×比例
        {
          const float segGap = 2.0f * u, segH = 10.0f * u;
          const float total = ((float)(it.hw * it.scale) - 13.0f * u) * 2.0f;
          const float segW = (total - segGap * 11.0f) / 12.0f;
          int lit = (int)std::lround((di.ratio < 0 ? 0 : di.ratio > 1 ? 1 : di.ratio) * 12.0);
          if (lit < 1) lit = 1;
          const float y0 = c.y + 4.0f * u;
          for (int s = 0; s < 12; ++s) {
            const float x0 = l + 13.0f * u + s * (segW + segGap);
            const D2D1_RECT_F sr = D2D1::RectF(x0, y0, x0 + segW, y0 + segH);
            if (s < lit)
              ctx.brush->SetColor(osc.isCenter
                  ? D2D1::ColorF(kAccent, dim) : inkOn(luma, 0.55f * dim));
            else
              ctx.brush->SetColor(inkOn(luma, 0.10f * dim));
            dc->FillRoundedRectangle(D2D1::RoundedRect(sr, 1.0f, 1.0f), ctx.brush);
          }
        }
      }
    }
  }
};

} // namespace

std::unique_ptr<IForm> createLevelForm() {
  return std::unique_ptr<IForm>(new LevelForm());
}

} // namespace okmeter::render
