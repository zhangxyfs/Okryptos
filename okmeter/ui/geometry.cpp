#include "geometry.h"
#include <algorithm>
#include <cmath>

namespace okmeter {

DockGeom layoutArc(int n, int radius, int gap, int screenH, const std::string& edge) {
  DockGeom g;
  if (n < 1) return g;
  const int mid = (n - 1) / 2;
  const double step = mid > 0
      ? std::min(92.0, (screenH * 0.8 / 2 - 58) / mid)
      : 0.0;
  g.w = 150;
  g.h = 2 * (mid * step + 58);
  g.items.resize((size_t)n);
  for (int i = 0; i < n; ++i) {
    const double fr = mid == 0 ? 0.0 : (double)(i - mid) / mid;
    double x = 120 - 46 * (1 - fr * fr);
    if (edge == "left") x = g.w - x;
    g.items[(size_t)i].x = x;
    g.items[(size_t)i].y = g.h / 2 + (i - mid) * step;
    g.items[(size_t)i].r = radius;
  }
  (void)gap;  // 弧线步长由屏高决定，gap 预留给直线形态（Plan 2b）
  return g;
}

void applyHover(DockGeom& g, int hoverIdx, double hoverScale, double push) {
  if (hoverIdx >= (int)g.items.size()) hoverIdx = -1;  // 越界正索引按复位处理
  for (size_t i = 0; i < g.items.size(); ++i) {
    ItemGeom& it = g.items[i];
    if (hoverIdx < 0 || (int)i == hoverIdx) {
      it.scale = hoverIdx < 0 ? 1.0 : hoverScale;
      it.dy = 0;
      continue;
    }
    const double d = std::abs((double)i - hoverIdx);
    const double amount = push * std::exp(-0.9 * (d - 1));  // 近多远少
    it.scale = 1.0;
    it.dy = ((int)i < hoverIdx ? -amount : amount);
  }
}

} // namespace okmeter
