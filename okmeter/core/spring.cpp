#include "spring.h"
#include <cmath>

namespace okmeter {

void Spring::step(double dt, double target) {
  if (dt <= 0) return;
  if (dt > 0.05) dt = 0.05;  // timer 卡顿防护：大步长切片由调用方继续推进
  const double force = -stiffness * (value - target) - damping * velocity;
  velocity += force * dt;
  value += velocity * dt;
}

bool Spring::settled(double target, double eps) const {
  return std::abs(value - target) < eps && std::abs(velocity) < eps;
}

void Spring::snap(double v) {
  value = v;
  velocity = 0;
}

} // namespace okmeter
