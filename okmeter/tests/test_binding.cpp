#include "../core/binding.h"
#include "framework.h"
#include <ctime>

using namespace okmeter;

static UsageEvent ev(const char* model, int64_t tokens, int64_t t) {
  UsageEvent e;
  e.model = model; e.sessionId = "s1";
  e.inputOther = tokens; e.timeMs = t;
  return e;
}

static Aggregator threeModels() {
  Aggregator a;
  a.add(ev("m/old", 10, 1000));
  a.add(ev("m/mid", 20, 2000));
  a.add(ev("m/new", 30, 3000));   // 最近
  return a;
}

TEST(binding_auto_recency_alternation) {
  Config c;
  c.count = 5;
  c.normalize();
  auto agg = threeModels();
  auto b = resolveBindings(c, agg);
  CHECK_EQ(b.size(), (size_t)5);
  CHECK(b[2].isModel && b[2].modelId == "m/new");  // 中心 = 最近
  CHECK(b[1].isModel && b[1].modelId == "m/mid");  // 次近 → 中心上/左
  CHECK(b[3].isModel && b[3].modelId == "m/old");  // 再次 → 中心下/右
  CHECK(!b[0].isModel && b[0].scope == Scope::All);  // 模型不够 → 全部累计兜底
  CHECK(!b[4].isModel && b[4].scope == Scope::All);
}

TEST(binding_explicit_total_and_model) {
  Config c;
  c.count = 3;
  c.mapping = {"total:today", "model:m/keep", "auto"};
  c.normalize();
  auto agg = threeModels();
  auto b = resolveBindings(c, agg);
  CHECK(!b[0].isModel && b[0].scope == Scope::Today);
  CHECK(b[1].isModel && b[1].modelId == "m/keep");   // 显式模型保留（即使不在数据里）
  CHECK(b[2].isModel && b[2].modelId == "m/old");    // auto 位按槽位 rank 取 recency（n=3 slot2=rank2）
}

TEST(binding_invalid_mapping_falls_back_auto) {
  Config c;
  c.count = 3;
  c.mapping = {"garbage", "total:wrong", "auto"};
  c.normalize();
  auto agg = threeModels();
  auto b = resolveBindings(c, agg);
  CHECK(b[0].isModel && b[0].modelId == "m/mid");  // garbage → auto → 槽位 rank1
  CHECK(b[1].isModel && b[1].modelId == "m/new");  // total:wrong → auto → 中心 rank0
}

TEST(binding_empty_data_all_total) {
  Config c;
  c.count = 3;
  c.normalize();
  Aggregator empty;
  auto b = resolveBindings(c, empty);
  for (const auto& x : b) { CHECK(!x.isModel); CHECK(x.scope == Scope::All); }
}
