// ui/geometry.h —— 弧线布局与悬停几何（纯函数，渲染无关）
#pragma once
#include <string>
#include <vector>

namespace okmeter {

// 收缩态几何共享常量：app 窗口露出宽度 / 弧线球帽露出宽度（约半球被屏缘裁掉）
constexpr int kCollapsedPx = 24;     // 收缩态窗口露出宽度
constexpr int kCollapsedCapPx = 12;  // 收缩态球帽露出宽度

struct ItemGeom {
  double x = 0, y = 0;     // 项中心（dock 盒内坐标）
  double r = 0;            // 半径（胶囊为半高）
  double hw = 0;           // 水平半宽（>r 时项为胶囊；0=圆形项）
  double scale = 1;        // 悬停放大
  double dy = 0;           // 悬停让位纵向偏移（+ 向下）
};

struct DockGeom {
  double w = 0, h = 0;
  bool connector = true;   // 项间连线（弧线形态）；胶囊/罗盘无连线（罗盘自绘虚线轨道）
  std::vector<ItemGeom> items;
};

// 球体弧线：中心项最靠屏内，两侧弓形回退；edge=left 时 x 镜像
DockGeom layoutArc(int n, int radius, int gap, int screenH, const std::string& edge);

// 胶囊量表：174px 胶囊直线排布（step 56，左右缘对称布局；edge 仅影响 tuck 烘焙方向）
DockGeom layoutCapsule(int n, const std::string& edge);

// 星环罗盘：中心 86px 罗盘（r=43）+ 卫星 46px（r=23）沿 R=84 圆周均布；
// rotDeg 为收缩态缓慢旋转的当前角度（展开态调用方传 0/静止值）
DockGeom layoutCompass(int n, double rotDeg);

// 悬停强化：hoverIdx 项放大，其余项沿排布方向让位（近多远少，指数衰减）；
// hoverIdx<0 复位全部
void applyHover(DockGeom& g, int hoverIdx, double hoverScale, double push);

} // namespace okmeter
