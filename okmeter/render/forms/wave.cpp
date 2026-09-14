// render/forms/wave.cpp —— 波形流形态：196px 圆角块直线排布（step 86），
// 顶行左名右值，下方 26 点历史 sparkline（线 ink 55% 1.2px + 面积 ink 8%；
// 中心项 accent 线 + accent-soft 面积）。块底委托材质（胶囊分支）。
#include "../form.h"
#include "../material.h"
#include <algorithm>
#include <vector>

using Microsoft::WRL::ComPtr;

namespace okmeter::render {
namespace {

constexpr UINT32 kAccent = 0x5FE0A8;
constexpr double kStep = 86;     // 原型 geom() wave：step 86
constexpr double kW = 218;       // 原型 W=218
constexpr float kItemHalfW = 98.0f;   // 块半宽（196/2）
constexpr float kItemHalfH = 32.0f;   // 块半高（padding 8+顶行 ~15+间距 6+spark 24+padding 10）

class WaveForm final : public IForm {
public:
  std::string id() const override { return "wave"; }

  DockGeom layout(int n, int screenW, int screenH, const std::string& edge) const override {
    DockGeom g;
    if (n < 1) return g;
    const int mid = (n - 1) / 2;
    const double s = uiScale(screenH);  // 比例法：原型值 × 屏高/1080
    g.scale = s;
    g.connector = false;  // 原型 wave 隐藏 arcSvg
    g.items.resize((size_t)n);
    if (isHorizEdge(edge)) {
      // 原型横向 wave：step 206、盒高 136
      const double step = 206 * s;
      g.w = 2 * (mid * step + 109 * s);
      g.h = 136 * s;
      for (int i = 0; i < n; ++i) {
        g.items[(size_t)i].x = g.w / 2 + (i - mid) * step;
        g.items[(size_t)i].y = g.h / 2;
        g.items[(size_t)i].r = 32 * s;
        g.items[(size_t)i].hw = 98 * s;
      }
      return g;
    }
    g.w = kW * s;
    g.h = 2 * (mid * kStep * s + 68 * s);
    for (int i = 0; i < n; ++i) {
      g.items[(size_t)i].x = g.w / 2;
      g.items[(size_t)i].y = g.h / 2 + (i - mid) * kStep * s;
      g.items[(size_t)i].r = (float)(kItemHalfH * s);
      g.items[(size_t)i].hw = (float)(kItemHalfW * s);
    }
    (void)screenW;  // 横向步长恒定 206，不吃屏宽
    (void)edge;
    return g;
  }

  double hoverPush() const override { return 0; }
  double cardRadius(int, int) const override { return 20; }  // 原型 RADII wave

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
      osc.cornerR = 10.0f * (float)g.scale;  // 块状角半径 ×比例
      if (isHot)
        dc->SetTransform(D2D1::Matrix3x2F::Scale((float)it.scale, (float)it.scale, c));
      ctx.material->drawOrbBack(dc, osc);
      dc->SetTransform(D2D1::IdentityMatrix());

      if (i >= ctx.items->size()) continue;
      const DockItem& di = (*ctx.items)[i];
      const float u = (float)g.scale;  // uiScale：内部尺寸 = 原型值 × 比例
      const float l = c.x - (float)(it.hw * it.scale), rgt = c.x + (float)(it.hw * it.scale);
      // 顶行：左名右值（原型 .wv .top，padding 13）
      const bool halo = ctx.material && ctx.material->id() == "liquid";
      if (!di.label.empty()) {
        ctx.brush->SetColor(inkLight( 0.66f * dim));
        const D2D1_RECT_F tr = D2D1::RectF(l + 13.0f * u, c.y - 24.0f * u, rgt - 80.0f * u, c.y - 9.0f * u);
        drawTextTrimmed(*ctx.d3d, dc, ctx.brush, di.label, ctx.capNameFmt, tr,
                        halo ? (float)dim : 0.0f);
      }
      if (!di.value.empty()) {
        const D2D1_RECT_F tr = D2D1::RectF(l + 80.0f * u, c.y - 24.0f * u, rgt - 13.0f * u, c.y - 9.0f * u);
        if (halo) liquidHalo(dc, ctx.brush, di.value, ctx.capValFmt, tr, (float)dim);
        ctx.brush->SetColor(inkLight( 0.93f * dim));
        dc->DrawText(di.value.c_str(), (UINT32)di.value.size(), ctx.capValFmt,
                     &tr, ctx.brush);
      }
      // sparkline：26 点历史，170×24（原型 drawSpark 同款：线 + 下方面积）
      if (di.hist.size() >= 2) {
        const float sw = 170.0f * u, sh = 24.0f * u;
        const float sx = c.x - sw * 0.5f, sy = c.y - 3.0f * u;
        // 最新点恒定落在屏幕内侧：dock 左缘 → 最右（右→左流），
        // dock 右缘 → 最左（左→右流，x 镜像）
        const bool flip = ctx.edge == "right";
        double maxV = 1.0;
        for (double v : di.hist) if (v > maxV) maxV = v;
        const size_t cnt = di.hist.size();
        auto ptAt = [&](size_t k) {
          const double v = di.hist[k];
          const double t = (double)k / (double)(cnt - 1);
          const float fx = sx + (float)((flip ? 1.0 - t : t) * sw);
          const float fy = sy + sh - 1.5f - (float)(v / maxV * (sh - 4.0f));
          return D2D1::Point2F(fx, fy);
        };
        // 面积：折线 + 右下/左下闭合（ID2D1PathGeometry）
        ComPtr<ID2D1Factory> fac;
        dc->GetFactory(&fac);  // void 返回
        ComPtr<ID2D1PathGeometry> geo;
        ComPtr<ID2D1GeometrySink> sink;
        if (fac && SUCCEEDED(fac->CreatePathGeometry(&geo)) && geo &&
            SUCCEEDED(geo->Open(&sink)) && sink) {
          sink->BeginFigure(ptAt(0), D2D1_FIGURE_BEGIN_FILLED);
          for (size_t k = 1; k < cnt; ++k) sink->AddLine(ptAt(k));
          sink->AddLine(D2D1::Point2F(ptAt(cnt - 1).x, sy + sh));
          sink->AddLine(D2D1::Point2F(ptAt(0).x, sy + sh));
          sink->EndFigure(D2D1_FIGURE_END_CLOSED);
          sink->Close();
          ctx.brush->SetColor(osc.isCenter
              ? D2D1::ColorF(kAccent, 0.16f * dim) : inkLight( 0.08f * dim));
          dc->FillGeometry(geo.Get(), ctx.brush);
        }
        // 折线
        ctx.brush->SetColor(osc.isCenter
            ? D2D1::ColorF(kAccent, dim) : inkLight( 0.55f * dim));
        for (size_t k = 1; k < cnt; ++k)
          dc->DrawLine(ptAt(k - 1), ptAt(k), ctx.brush, 1.2f * u);
      }
    }
  }
};

} // namespace

std::unique_ptr<IForm> createWaveForm() {
  return std::unique_ptr<IForm>(new WaveForm());
}

} // namespace okmeter::render
