#include "geometry.h"
#include <algorithm>
#include <cmath>

namespace okmeter {

DockGeom layoutArc(int n, int radius, int gap, int screenW, int screenH,
                   const std::string& edge) {
  DockGeom g;
  if (n < 1) return g;
  const int mid = (n - 1) / 2;
  const double s = uiScale(screenH);  // 比例法：原型值 × 屏高/1080
  g.scale = s;
  if (isHorizEdge(edge)) {
    // 横向（原型 geom() arc 横排）：弧线朝屏心鼓（top 向下 / bottom 向上）
    const double step = mid > 0
        ? std::min(92.0 * s, (screenW * 0.6 / 2 - 58 * s) / mid)
        : 0.0;
    g.w = 2 * (mid * step + 58 * s);
    g.h = 150 * s;
    g.items.resize((size_t)n);
    for (int i = 0; i < n; ++i) {
      const double fr = mid == 0 ? 0.0 : (double)(i - mid) / mid;
      double y = (30 + 46 * (1 - fr * fr)) * s;
      if (edge == "bottom") y = g.h - y;
      g.items[(size_t)i].x = g.w / 2 + (i - mid) * step;
      g.items[(size_t)i].y = y;
      g.items[(size_t)i].r = radius * s;
    }
    (void)gap;
    return g;
  }
  const double step = mid > 0
      ? std::min(92.0 * s, (screenH * 0.8 / 2 - 58 * s) / mid)
      : 0.0;  // 原型同款屏高占比公式（参数同步缩放）
  g.w = 150 * s;
  g.h = 2 * (mid * step + 58 * s);
  g.items.resize((size_t)n);
  for (int i = 0; i < n; ++i) {
    const double fr = mid == 0 ? 0.0 : (double)(i - mid) / mid;
    double x = (120 - 46 * (1 - fr * fr)) * s;
    if (edge == "left") x = g.w - x;
    g.items[(size_t)i].x = x;
    g.items[(size_t)i].y = g.h / 2 + (i - mid) * step;
    g.items[(size_t)i].r = radius * s;
  }
  (void)gap;  // 弧线步长由屏高决定，gap 预留给直线形态（Plan 2b）
  return g;
}

DockGeom layoutCapsule(int n, int screenW, int screenH, const std::string& edge) {
  DockGeom g;
  if (n < 1) return g;
  const int mid = (n - 1) / 2;
  const double s = uiScale(screenH);
  g.scale = s;
  g.connector = false;     // 胶囊无项间连线（原型 capsule 隐藏 arcSvg）
  g.items.resize((size_t)n);
  if (isHorizEdge(edge)) {
    const double step = 186 * s;  // 原型横向 capsule：step 186
    g.w = 2 * (mid * step + 98 * s);
    g.h = 96 * s;
    for (int i = 0; i < n; ++i) {
      g.items[(size_t)i].x = g.w / 2 + (i - mid) * step;
      g.items[(size_t)i].y = g.h / 2;
      g.items[(size_t)i].r = 20 * s;   // 胶囊半高（同竖向）×比例
      g.items[(size_t)i].hw = 87 * s;  // 胶囊半宽（174/2）×比例
    }
    return g;
  }
  const double step = 56 * s;  // 原型 geom() capsule：step 56 ×比例
  g.w = 196 * s;               // 原型 W=196（胶囊 174 + 两侧各 11）×比例
  g.h = 2 * (mid * step + 48 * s);
  for (int i = 0; i < n; ++i) {
    g.items[(size_t)i].x = g.w / 2;  // 左右缘对称，edge 只影响 tuck 方向
    g.items[(size_t)i].y = g.h / 2 + (i - mid) * step;
    g.items[(size_t)i].r = 20 * s;   // 胶囊半高（原型 .cap 高 40：8+15+5+3+9）×比例
    g.items[(size_t)i].hw = 87 * s;  // 胶囊半宽（174/2）×比例
  }
  (void)screenW;  // 胶囊横向步长恒定 186，不吃屏宽
  (void)edge;
  return g;
}

DockGeom layoutMini(const std::vector<double>& widths, double scale) {
  DockGeom g;
  if (widths.empty()) return g;
  const double gap = 6 * scale, padX = 20 * scale;
  g.scale = scale;
  g.connector = false;
  g.h = 32 * scale;
  g.w = padX * 2;
  for (size_t i = 0; i < widths.size(); ++i)
    g.w += widths[i] + (i ? gap : 0);
  g.items.resize(widths.size());
  double x = padX;
  for (size_t i = 0; i < widths.size(); ++i) {
    g.items[i].x = x + widths[i] / 2;
    g.items[i].y = g.h / 2;
    g.items[i].hw = widths[i] / 2;
    g.items[i].r = 13 * scale;  // chipH/2（pill 半高）
    x += widths[i] + gap;
  }
  return g;
}

static double lerpD(double a, double b, double t) { return a + (b - a) * t; }

DockGeom morphGeom(const DockGeom& stage, const DockGeom& mini, double e,
                   bool anchorBottom) {
  if (e < 0) e = 0;
  if (e > 1) e = 1;
  DockGeom g;
  g.scale = stage.scale;
  g.connector = stage.connector;
  g.w = lerpD(mini.w, stage.w, e);
  g.h = lerpD(mini.h, stage.h, e);
  const size_t n = std::min(stage.items.size(), mini.items.size());
  g.items.resize(n);
  for (size_t i = 0; i < n; ++i) {
    const ItemGeom& a = mini.items[i], &b = stage.items[i];
    // 水平：两盒居中轴对齐（各自中心 − 各自半宽插值后回到新盒中心系）
    g.items[i].x = lerpD(a.x - mini.w / 2, b.x - stage.w / 2, e) + g.w / 2;
    g.items[i].y = anchorBottom
        ? g.h - lerpD(mini.h - a.y, stage.h - b.y, e)   // 底缘：盒底对齐
        : lerpD(a.y, b.y, e);                            // 顶缘：盒顶对齐
    g.items[i].r = lerpD(a.r, b.r, e);
    g.items[i].hw = lerpD(a.hw, b.hw, e);
    g.items[i].scale = b.scale;
  }
  return g;
}

DockGeom layoutCompass(int n, double rotDeg, int screenH) {
  DockGeom g;
  if (n < 1) return g;
  const int mid = (n - 1) / 2;
  const double s = uiScale(screenH);
  g.scale = s;
  g.w = 236 * s;  // 原型 geom() compass：W=236 H=252 R=84 ×比例
  g.h = 252 * s;
  g.connector = false;  // 罗盘轨道由形态自绘虚线圆环，不走项间连线
  g.items.resize((size_t)n);
  const double cx = g.w / 2, cy = g.h / 2, R = 84 * s;
  const double pi = 3.14159265358979323846;
  int k = 0;
  for (int i = 0; i < n; ++i) {
    if (i == mid) continue;
    const double a = (-90 + k * 360.0 / (n - 1) + rotDeg) * pi / 180;
    g.items[(size_t)i].x = cx + R * std::cos(a);
    g.items[(size_t)i].y = cy + R * std::sin(a);
    g.items[(size_t)i].r = 23 * s;  // 卫星 46px ×比例
    ++k;
  }
  g.items[(size_t)mid].x = cx;  // 中心罗盘 86px
  g.items[(size_t)mid].y = cy;
  g.items[(size_t)mid].r = 43 * s;  // 中心 86px ×比例
  return g;
}

void applyHover(DockGeom& g, int hoverIdx, double hoverScale, double push,
                double dimShrink, bool horizontal) {
  if (hoverIdx >= (int)g.items.size()) hoverIdx = -1;  // 越界正索引按复位处理
  for (size_t i = 0; i < g.items.size(); ++i) {
    ItemGeom& it = g.items[i];
    if (hoverIdx < 0 || (int)i == hoverIdx) {
      it.scale = hoverIdx < 0 ? 1.0 : hoverScale;
      it.dy = 0;
      it.dx = 0;
      continue;
    }
    const double d = std::abs((double)i - hoverIdx);
    const double amount = push * std::exp(-0.9 * (d - 1));  // 近多远少
    // 非悬停项回缩由形态决定（原型 .orb.dim scale(.9) 仅球体弧线；块状项 1.0 不回缩）
    it.scale = d >= 2.0 ? dimShrink : 1.0;
    if (horizontal) it.dx = ((int)i < hoverIdx ? -amount : amount);
    else            it.dy = ((int)i < hoverIdx ? -amount : amount);
  }
}

} // namespace okmeter
