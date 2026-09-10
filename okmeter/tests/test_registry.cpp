#include "../core/registry.h"
#include "../adapters/adapter.h"
#include "../core/aggregator.h"
#include "framework.h"

using namespace okmeter;

namespace {

// 伪造适配器：不读盘，直接吐两条合成事件——验证"新增适配器零改动框架"
class FakeAdapter : public IAdapter {
public:
  std::string id() const override { return "fake-agent"; }
  int poll(const EventSink& sink) override {
    UsageEvent a;
    a.model = "fake/m1"; a.sessionId = "fs1";
    a.inputOther = 100; a.timeMs = 1000;
    sink(a);
    UsageEvent b;
    b.model = "fake/m2"; b.sessionId = "fs1";
    b.output = 200; b.timeMs = 2000;
    sink(b);
    return 2;
  }
};

} // namespace

TEST(registry_contract_new_adapter_needs_no_framework_change) {
  Registry<IAdapter> reg;
  reg.add("fake-agent", [] { return std::make_unique<FakeAdapter>(); });

  auto a = reg.create("fake-agent");
  CHECK(a != nullptr);
  CHECK(a->id() == "fake-agent");

  Aggregator agg;
  int n = a->poll([&](const UsageEvent& e) { agg.add(e); });
  CHECK_EQ(n, 2);
  CHECK_EQ(agg.all().total(), 300);
  CHECK_EQ(agg.modelsByRecency()[0], "fake/m2");

  CHECK(reg.create("nonexistent") == nullptr);
  auto names = reg.names();
  CHECK_EQ(names.size(), (size_t)1);
  CHECK(names[0] == "fake-agent");
}
