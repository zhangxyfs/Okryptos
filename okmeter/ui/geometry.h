// ui/geometry.h —— 弧线布局与悬停几何（纯函数，渲染无关）
#pragma once
#include <string>
#include <vector>

namespace okmeter {

struct ItemGeom {
  double x = 0, y = 0;     // 项中心（dock 盒内坐标）
  double r = 0;            // 半径
  double scale = 1;        // 悬停放大
  double dy = 0;           // 悬停让位纵向偏移（+ 向下）
};

struct DockGeom {
  double w = 0, h = 0;
  std::vector<ItemGeom> items;
};

// 球体弧线：中心项最靠屏内，两侧弓形回退；edge=left 时 x 镜像
DockGeom layoutArc(int n, int radius, int gap, int screenH, const std::string& edge);

// 悬停强化：hoverIdx 项放大，其余项沿排布方向让位（近多远少，指数衰减）；
// hoverIdx<0 复位全部
void applyHover(DockGeom& g, int hoverIdx, double hoverScale, double push);

} // namespace okmeter
