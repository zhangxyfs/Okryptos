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
  applyHover(g, 2, 1.34, 10.0);
  CHECK(std::abs(g.items[2].scale - 1.34) < 1e-9);
  CHECK(g.items[1].dy < 0);   // 中心上方项被向上挤
  CHECK(g.items[3].dy > 0);   // 中心下方项被向下挤
  CHECK(std::abs(g.items[1].dy) > std::abs(g.items[0].dy));  // 近者挤得多
  applyHover(g, -1, 1.34, 10.0);   // 取消悬停
  CHECK(std::abs(g.items[2].scale - 1.0) < 1e-9);
  CHECK(g.items[1].dy == 0 && g.items[3].dy == 0);
}
