#include "../core/spring.h"
#include "framework.h"
#include <cmath>

using namespace okmeter;

TEST(spring_converges_to_target) {
  Spring s;                       // 默认刚度/阻尼：150~200ms 等效时长
  for (int i = 0; i < 240; ++i)   // 2s @ 1/120
    s.step(1.0 / 120, 1.0);
  CHECK(s.settled(1.0));
  CHECK(std::abs(s.value - 1.0) < 0.01);
}

TEST(spring_zero_dt_noop) {
  Spring s;
  s.step(0, 1.0);
  CHECK(s.value == 0 && s.velocity == 0);
}

TEST(spring_huge_dt_does_not_blow_up) {
  Spring s;
  s.step(100.0, 1.0);             // timer 卡顿：内部钳 dt
  CHECK(std::isfinite(s.value) && std::isfinite(s.velocity));
  for (int i = 0; i < 240; ++i)
    s.step(1.0 / 120, 1.0);
  CHECK(s.settled(1.0));
}

TEST(spring_snap) {
  Spring s;
  s.snap(0.5);
  CHECK(s.value == 0.5 && s.velocity == 0);
}
