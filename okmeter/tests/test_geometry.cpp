#include "../ui/geometry.h"
#include "framework.h"
#include <cmath>

using namespace okmeter;

TEST(geom_arc_right_edge_symmetry) {
  auto g = layoutArc(3, 30, 14, 1920, 900, "right");
  CHECK_EQ(g.items.size(), (size_t)3);
  CHECK(g.w > 0 && g.h > 0);
  CHECK(g.items[0].y < g.items[1].y && g.items[1].y < g.items[2].y);
  CHECK(std::abs(g.items[0].y + g.items[2].y - 2 * g.items[1].y) < 1e-9); // 中心对称
  CHECK(g.items[1].x < g.items[0].x);   // 中心球最靠屏内（弧线弓形）
}

TEST(geom_arc_left_edge_mirror) {
  auto r = layoutArc(3, 30, 14, 1920, 900, "right");
  auto l = layoutArc(3, 30, 14, 1920, 900, "left");
  for (size_t i = 0; i < 3; ++i) {
    CHECK(std::abs(r.items[i].x + l.items[i].x - r.w) < 1e-9);  // 镜像
    CHECK(r.items[i].y == l.items[i].y);
  }
}

TEST(geom_single_item) {
  auto g = layoutArc(1, 30, 14, 1920, 900, "right");
  CHECK_EQ(g.items.size(), (size_t)1);
  CHECK(std::isfinite(g.items[0].x) && std::isfinite(g.items[0].y));
}

TEST(geom_hover_enlarge_and_squeeze) {
  auto g = layoutArc(5, 30, 14, 1920, 900, "right");
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
  auto g1 = layoutCapsule(3, 1920, 1080, "right");
  auto g2 = layoutCapsule(3, 1920, 2160, "right");  // 钳 1.6
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

TEST(geom_arc_horizontal_top_bottom_mirror) {
  auto t = layoutArc(3, 30, 14, 1920, 1080, "top");
  auto b = layoutArc(3, 30, 14, 1920, 1080, "bottom");
  CHECK(t.w > t.h);                        // 横向：宽 > 高
  CHECK(t.items[0].x < t.items[1].x && t.items[1].x < t.items[2].x);  // 横排
  CHECK(t.items[1].y > t.items[0].y);      // top：中心项向下鼓（朝屏心）
  for (size_t i = 0; i < 3; ++i) {
    CHECK(std::abs(t.items[i].y + b.items[i].y - t.h) < 1e-9);  // bottom = 垂直镜像
    CHECK(t.items[i].x == b.items[i].x);
  }
}

TEST(geom_wave_horizontal_row) {
  auto c = layoutCapsule(3, 1920, 1080, "top");
  CHECK(c.w > c.h);
  CHECK(std::abs(c.items[0].y - c.h / 2) < 1e-9);   // 全部垂直居中
  CHECK(std::abs(c.items[1].x - c.w / 2) < 1e-9);   // 中心项水平居中
  CHECK(std::abs(c.items[1].x - c.items[0].x - 186.0) < 1e-6);  // step 186 ×scale(=1)
}

TEST(geom_layout_mini_accumulates) {
  std::vector<double> w{80, 100, 80};   // scale=1：gap6 padX20 chipH26 盒32
  auto g = layoutMini(w, 1.0);
  CHECK(std::abs(g.w - (40 + 80 + 6 + 100 + 6 + 80)) < 1e-9);  // 312
  CHECK(std::abs(g.h - 32) < 1e-9);
  CHECK(std::abs(g.items[0].x - (20 + 40)) < 1e-9);
  CHECK(std::abs(g.items[1].x - (20 + 80 + 6 + 50)) < 1e-9);
  CHECK(std::abs(g.items[0].y - 16) < 1e-9);
  CHECK(std::abs(g.items[1].hw - 50) < 1e-9);
  CHECK(std::abs(g.items[0].r - 13) < 1e-9);
}

TEST(geom_morph_endpoints_and_anchor) {
  auto stage = layoutCapsule(3, 1920, 1080, "top");
  std::vector<double> w{80, 100, 80};
  auto mini = layoutMini(w, 1.0);
  stage.items[0].dx = 12;   // 悬停让位：展开全额、收缩归零，morph 全程线性
  CHECK(std::abs(morphGeom(stage, mini, 0.5, false).items[0].dx - 6.0) < 1e-9);
  auto g0 = morphGeom(stage, mini, 0.0, false);
  auto g1 = morphGeom(stage, mini, 1.0, false);
  CHECK(std::abs(g0.w - mini.w) < 1e-9 && std::abs(g0.h - mini.h) < 1e-9);
  CHECK(std::abs(g1.w - stage.w) < 1e-9 && std::abs(g1.h - stage.h) < 1e-9);
  for (size_t i = 0; i < 3; ++i) {
    CHECK(std::abs(g0.items[i].x - mini.items[i].x) < 1e-9);   // e=0 完全 mini 坐标
    CHECK(std::abs(g1.items[i].x - stage.items[i].x) < 1e-9);  // e=1 完全 stage 坐标
    CHECK(std::abs(g1.items[i].y - stage.items[i].y) < 1e-9);
  }
  auto gb = morphGeom(stage, mini, 0.0, true);   // 底缘锚定：盒底对齐
  CHECK(std::abs(gb.items[0].y - mini.items[0].y) < 1e-9);
  auto gb5 = morphGeom(stage, mini, 0.5, true);
  // 底对齐：各项到底边距离 = mini/stage 到底边距离的插值
  const double dMini = mini.h - mini.items[0].y, dStage = stage.h - stage.items[0].y;
  CHECK(std::abs((gb5.h - gb5.items[0].y) - (dMini + dStage) / 2) < 1e-9);
}

TEST(geom_apply_hover_horizontal_uses_dx) {
  auto g = layoutCapsule(3, 1920, 1080, "top");
  applyHover(g, 1, 1.0, 10.0, 1.0, true);
  CHECK(g.items[0].dx < 0 && g.items[2].dx > 0);   // 横向让位
  CHECK(g.items[0].dy == 0 && g.items[2].dy == 0);
  applyHover(g, -1, 1.0, 10.0, 1.0, true);
  CHECK(g.items[0].dx == 0 && g.items[2].dx == 0);
}
