// ui/geometry.h —— 弧线布局与悬停几何（纯函数，渲染无关）
#pragma once
#include <string>
#include <vector>

namespace okmeter {

// 原型基准画布（okmeter-dock-prototype.html 的设计分辨率 1920×1080）。
// 一切几何尺寸 = 原型值 × uiScale（比例法，不用魔法像素；屏高为主——垂直 dock
// 的纵向密度决定观感）。钳制防极端屏
constexpr double kRefScreenH = 1080.0;
inline double uiScale(int screenH) {
  const double s = screenH / kRefScreenH;
  return s < 0.7 ? 0.7 : (s > 1.6 ? 1.6 : s);
}

// 横向吸附边（屏顶/状态栏上方）：dock 横排，宽 > 高
inline bool isHorizEdge(const std::string& edge) {
  return edge == "top" || edge == "bottom";
}

struct ItemGeom {
  double x = 0, y = 0;     // 项中心（dock 盒内坐标）
  double r = 0;            // 半径（胶囊为半高）
  double hw = 0;           // 水平半宽（>r 时项为胶囊；0=圆形项）
  double scale = 1;        // 悬停放大
  double dy = 0;           // 悬停让位纵向偏移（+ 向下）
  double dx = 0;           // 悬停让位横向偏移（+ 向右；横向 dock 用）
};

struct DockGeom {
  double w = 0, h = 0;
  double scale = 1.0;      // uiScale（绘制层所有尺寸/偏移/字号乘它）
  bool connector = true;   // 项间连线（弧线形态）；胶囊/罗盘无连线（罗盘自绘虚线轨道）
  std::vector<ItemGeom> items;
};

// 球体弧线：中心项最靠屏内，两侧弓形回退；edge=left 时 x 镜像；
// 横向边（top/bottom）横排，弧线朝屏心鼓（top 向下 / bottom 向上，垂直镜像）
DockGeom layoutArc(int n, int radius, int gap, int screenW, int screenH,
                   const std::string& edge);

// 胶囊量表：174px 胶囊直线排布（step 56，左右缘对称布局；edge 仅影响 tuck 烘焙方向）；
// 横向边横排（step 186，盒高 96）
DockGeom layoutCapsule(int n, int screenW, int screenH, const std::string& edge);

// 星环罗盘：中心 86px 罗盘（r=43）+ 卫星 46px（r=23）沿 R=84 圆周均布；
// rotDeg 为收缩态缓慢旋转的当前角度（展开态调用方传 0/静止值）
DockGeom layoutCompass(int n, double rotDeg, int screenH);

// mini 条布局（横向收缩态）：chip 行，gap 6 / 横 padding 20 / 盒高 32（全 ×scale）
DockGeom layoutMini(const std::vector<double>& widths, double scale);

// 展开↔收缩几何插值：e=0 完全 mini，e=1 完全 stage；anchorBottom=true 底缘锚定
//（盒底对齐，用于 bottom 边），false 顶缘锚定
DockGeom morphGeom(const DockGeom& stage, const DockGeom& mini, double e,
                   bool anchorBottom);

// 悬停强化：hoverIdx 项放大，其余项沿排布方向让位（近多远少，指数衰减）；
// hoverIdx<0 复位全部；horizontal=true 时让位走 dx（横向 dock）
void applyHover(DockGeom& g, int hoverIdx, double hoverScale, double push,
                double dimShrink = 1.0, bool horizontal = false);

} // namespace okmeter
