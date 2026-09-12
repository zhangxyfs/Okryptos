#include "../ui/geometry.h"
#include "framework.h"
#include <cmath>

using namespace okmeter;

TEST(geom_arc_right_edge_symmetry) {
  auto g = layoutArc(3, 30, 14, 900, "right");
  CHECK_EQ(g.items.size(), (size_t)3);
  CHECK(g.w > 0 && g.h > 0);
  CHECK(g.items[0].y < g.items[1].y && g.items[1].y < g.items[2].y);
  CHECK(std::abs(g.items[0].y + g.items[2].y - 2 * g.items[1].y) < 1e-9); // 中心对称
  CHECK(g.items[1].x < g.items[0].x);   // 中心球最靠屏内（弧线弓形）
}

TEST(geom_arc_left_edge_mirror) {
  auto r = layoutArc(3, 30, 14, 900, "right");
  auto l = layoutArc(3, 30, 14, 900, "left");
  for (size_t i = 0; i < 3; ++i) {
    CHECK(std::abs(r.items[i].x + l.items[i].x - r.w) < 1e-9);  // 镜像
    CHECK(r.items[i].y == l.items[i].y);
  }
}

TEST(geom_single_item) {
  auto g = layoutArc(1, 30, 14, 900, "right");
  CHECK_EQ(g.items.size(), (size_t)1);
  CHECK(std::isfinite(g.items[0].x) && std::isfinite(g.items[0].y));
}

TEST(geom_hover_enlarge_and_squeeze) {
  auto g = layoutArc(5, 30, 14, 900, "right");
  applyHover(g, 2, 1.34, 10.0, 1.0);   // 用户裁决：悬停他项不回缩
  CHECK(std::abs(g.items[2].scale - 1.34) < 1e-9);
  CHECK(g.items[1].dy < 0);   // 中心上方项被向上挤
  CHECK(g.items[3].dy > 0);   // 中心下方项被向下挤
  CHECK(std::abs(g.items[1].dy) > std::abs(g.items[0].dy));  // 近者挤得多
  CHECK(std::abs(g.items[1].scale - 1.0) < 1e-9);   // 相邻项只让位不回缩
  CHECK(std::abs(g.items[0].scale - 1.0) < 1e-9);  // 远端项不回缩（用户裁决）
  CHECK(std::abs(g.items[4].scale - 1.0) < 1e-9);
  applyHover(g, 2, 1.34, 10.0, 1.0);   // 恢复悬停态，验证越界索引的复位语义
  applyHover(g, 99, 1.34, 10.0, 1.0);   // 越界正索引 → 复位
  CHECK(std::abs(g.items[2].scale - 1.0) < 1e-9);
  CHECK(g.items[0].dy == 0 && g.items[4].dy == 0);
  applyHover(g, -1, 1.34, 10.0, 1.0);   // 取消悬停
  CHECK(std::abs(g.items[2].scale - 1.0) < 1e-9);
  CHECK(g.items[1].dy == 0 && g.items[3].dy == 0);
}

TEST(geom_uiscale_ratio) {
  // 比例法：基准画布 1080 → 比例 1（用户实机）；钳制防极端
  CHECK(std::abs(uiScale(1080) - 1.0) < 1e-9);
  CHECK(std::abs(uiScale(2160) - 1.6) < 1e-9);  // 4K 钳 1.6
  CHECK(std::abs(uiScale(540) - 0.7) < 1e-9);   // 小屏钳 0.7
  CHECK(std::abs(uiScale(900) - 900.0 / 1080.0) < 1e-9);
}

TEST(geom_capsule_scales_with_screen) {
  auto g1 = layoutCapsule(3, 1080, "right");
  auto g2 = layoutCapsule(3, 2160, "right");  // 钳 1.6
  CHECK(std::abs(g1.w - 196.0) < 1e-6);            // 基准分辨率下与原型一致
  CHECK(std::abs(g2.w - 196.0 * 1.6) < 1e-6);      // 4K 按比例放大
  CHECK(std::abs(g2.items[1].hw - 87.0 * 1.6) < 1e-6);
  CHECK(std::abs(g2.items[1].r - 20.0 * 1.6) < 1e-6);
  CHECK(std::abs(g2.scale - 1.6) < 1e-9);
  // 间距同比：g2 step = g1 step × 1.6
  const double step1 = g1.items[1].y - g1.items[0].y;
  const double step2 = g2.items[1].y - g2.items[0].y;
  CHECK(std::abs(step2 - step1 * 1.6) < 1e-6);
}
