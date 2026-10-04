#include "../ui/chartcolors.h"
#include "framework.h"
#include <cmath>
#include <vector>

using namespace okmeter::chartcolors;

static const char* kKnown[] = {"月之暗面", "OpenAI", "DeepSeek", "通义", "混元",
                               "智谱", "Anthropic", "Google", "豆包"};

TEST(chartcolors_curated_pairwise_distinguishable) {
  std::vector<Hsl> hs;
  for (const char* n : kKnown) {
    Hsl c;
    CHECK(curated(n, c));
    hs.push_back(c);
  }
  CHECK_EQ(hs.size(), (size_t)9);
  for (size_t i = 0; i < hs.size(); ++i)
    for (size_t j = i + 1; j < hs.size(); ++j)
      CHECK(hueDist(hs[i].h, hs[j].h) >= 25.0f);  // 两两 ΔHue≥25°（拍板 9）
  Hsl miss;
  CHECK(!curated("不知名小厂", miss));  // 未命中 → 走黄金角
}

TEST(chartcolors_golden_stable_and_avoids_curated) {
  const std::vector<std::string> names = {"acme", "bCorp", "zzz-labs", "月之暗面"};
  const auto a = providerColors(names);
  const auto b = providerColors(names);
  CHECK_EQ(a.size(), names.size());
  int overflow = 0;
  for (size_t i = 0; i < a.size(); ++i) {
    CHECK(std::fabs(a[i].h - b[i].h) < 0.001f);  // 同序同色（会话内稳定）
    CHECK(std::fabs(a[i].s - b[i].s) < 0.001f);
    Hsl cur;
    if (curated(names[i], cur)) {
      CHECK(std::fabs(a[i].h - cur.h) < 0.001f);  // 策展命中用固定色
      continue;
    }
    ++overflow;
    CHECK(std::fabs(a[i].s - 65.0f) < 0.001f);    // 溢出 S/L 固定
    CHECK(std::fabs(a[i].l - 55.0f) < 0.001f);
  }
  CHECK_EQ(overflow, 3);
  // 前两家溢出：空间尚足，必须与全部策展色避让 ΔHue≥25°，且两两可辨
  for (size_t i = 0; i < 2; ++i) {
    for (const char* n : kKnown) {
      Hsl c;
      curated(n, c);
      CHECK(hueDist(a[i].h, c.h) >= 25.0f);
    }
  }
  CHECK(hueDist(a[0].h, a[1].h) >= 25.0f);
}

TEST(chartcolors_golden_dense_table_terminates) {
  // 回归：策展表 9 色 ×±25° 覆盖超 360°，大量溢出提供商时严格避让数学上无解，
  // 必须退化取色距最大者（曾为死循环：黄金角 144 步回绕后无限重试）
  std::vector<std::string> many;
  for (int i = 0; i < 12; ++i) many.push_back("overflow-" + std::to_string(i));
  const auto cols = providerColors(many);
  CHECK_EQ(cols.size(), many.size());
  const auto again = providerColors(many);  // 稠密退化路径同样稳定
  for (size_t i = 0; i < cols.size(); ++i)
    CHECK(std::fabs(cols[i].h - again[i].h) < 0.001f);
}

TEST(chartcolors_drilled_family_rotation) {
  // 家族契约（用户裁决：模型随提供商同色相——"都是紫色，只是不同的紫"）：
  // 色相以族心对称展开、全族跨度 ≤36°（±18° 封顶）；明度单调斜坡（跨度 ≤30）
  const Hsl base{258.0f, 82.0f, 66.0f};
  const Hsl m0 = drilled(base, 0, 3);
  const Hsl m1 = drilled(base, 1, 3);
  const Hsl m2 = drilled(base, 2, 3);
  CHECK(std::fabs(m1.h - base.h) < 0.001f);       // 中间序 = 基色
  CHECK(m0.h < base.h);                           // 首序 -18°
  CHECK(m2.h > base.h);                           // 末序 +18°
  CHECK(std::fabs(m0.h - m2.h + 36.0f) < 0.001f);
  CHECK(m0.l < m1.l && m1.l < m2.l);              // 明度单调斜坡（族内可辨）
  CHECK(std::fabs(m0.l - 51.0f) < 0.001f);        // 66 - 15
  CHECK(std::fabs(m1.l - 66.0f) < 0.001f);
  CHECK(std::fabs(m2.l - 81.0f) < 0.001f);        // 66 + 15
  const Hsl single = drilled(base, 0, 1);         // 单模型 = 基色
  CHECK(std::fabs(single.h - base.h) < 0.001f);
  CHECK(std::fabs(single.l - base.l) < 0.001f);
}

TEST(chartcolors_drilled_many_models_stay_in_family) {
  // 模型很多时：色相步长均摊（跨度仍 ≤36°），不漂出提供商家族色；
  // 相邻模型色相与明度双重渐变，保证逐对可辨
  const Hsl base{258.0f, 82.0f, 66.0f};
  const int n = 10;
  Hsl prev = drilled(base, 0, n);
  CHECK(std::fabs(prev.h - (base.h - 18.0f)) < 0.001f);   // 跨度封顶 ±18°
  CHECK(std::fabs(prev.l - 51.0f) < 0.001f);              // 明度斜坡起点
  for (int i = 1; i < n; ++i) {
    const Hsl c = drilled(base, i, n);
    CHECK(c.h > prev.h);                                   // 色相单调（步长 4°）
    CHECK(c.l > prev.l);                                   // 明度单调（步长 10/3°）
    CHECK(hueDist(c.h, base.h) <= 18.001f);                // 不出族
    prev = c;
  }
  CHECK(std::fabs(prev.h - (base.h + 18.0f)) < 0.001f);
  CHECK(std::fabs(prev.l - 81.0f) < 0.001f);
}

TEST(chartcolors_to_rgb_sanity) {
  const Rgb red = toRgb(Hsl{0.0f, 100.0f, 50.0f});
  CHECK(std::fabs(red.r - 1.0f) < 0.001f);
  CHECK(std::fabs(red.g) < 0.001f);
  CHECK(std::fabs(red.b) < 0.001f);
  const Rgb gray = toRgb(Hsl{200.0f, 0.0f, 50.0f});  // S=0 → 灰
  CHECK(std::fabs(gray.r - 0.5f) < 0.001f);
  CHECK(std::fabs(gray.r - gray.g) < 0.001f);
  CHECK(std::fabs(gray.g - gray.b) < 0.001f);
  const Rgb wrap = toRgb(Hsl{360.0f, 100.0f, 50.0f});  // 色相回绕
  CHECK(std::fabs(wrap.r - 1.0f) < 0.001f);
}
