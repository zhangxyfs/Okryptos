// core/binding.h —— 指标映射解析：位置 → 总量模式/模型模式
#pragma once
#include "aggregator.h"
#include "config.h"
#include <string>
#include <vector>

namespace okmeter {

enum class Scope { Session, Today, Week, All };

struct Binding {
  bool isModel = false;       // false=总量模式，true=模型模式
  Scope scope = Scope::All;   // 总量模式有效
  std::string modelId;        // 模型模式有效
};

std::vector<Binding> resolveBindings(const Config& cfg, const Aggregator& agg);

} // namespace okmeter
