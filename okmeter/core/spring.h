// core/spring.h —— 弹簧积分器（stiffness/damping 模型，非贝塞尔近似）
#pragma once

namespace okmeter {

struct Spring {
  double value = 0;
  double velocity = 0;
  double stiffness = 400;   // ωn=20 → 2% 整定约 0.23s，贴合 150~200ms 规格
  double damping = 40;      // 临界阻尼 = 2√stiffness

  void step(double dt, double target);   // dt 秒；内部钳上限防卡顿爆炸
  bool settled(double target, double eps = 1e-3) const;
  void snap(double v);
};

} // namespace okmeter
