#include "../adapters/reasonix/adapter.h"
#include "../core/store.h"
#include "framework.h"
#include <fstream>
#include <process.h>

using namespace okmeter;

static std::filesystem::path rxTempDir(const char* name) {
  auto d = std::filesystem::temp_directory_path() /
           ("okmeter-test-" + std::string(name) + "-" + std::to_string(_getpid()));
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
  std::filesystem::create_directories(d);
  return d;
}

static const char* kRxGood =
  R"({"ts":1779600961491,"session":"code-OpenDelicacy","model":"deepseek-v4-pro","promptTokens":79280,"completionTokens":391,"cacheHitTokens":79104,"cacheMissTokens":176,"costUsd":0.0007})";
static const char* kRxNullSession =
  R"({"ts":1778587220716,"session":null,"model":"deepseek-v4-flash","promptTokens":17001,"completionTokens":71,"cacheHitTokens":5632,"cacheMissTokens":11369})";

TEST(reasonix_scan_and_mapping) {
  auto home = rxTempDir("rx-home");
  auto stateDir = rxTempDir("rx-state");
  const auto f1 = home / "usage.jsonl";
  {
    std::ofstream f(f1, std::ios::binary | std::ios::trunc);
    f << kRxGood << "\n" << kRxNullSession << "\n{bad\n";
  }

  Store store(stateDir);
  ReasonixAdapter rx(home, &store);
  Aggregator agg;
  int n = rx.poll([&](const UsageEvent& e) { agg.add(e); });

  CHECK_EQ(n, 2);
  const ModelStat* pro = agg.model("reasonix/deepseek-v4-pro");
  CHECK(pro != nullptr);
  CHECK_EQ(pro->all.total(), 79671);        // 176 + 79104 + 391
  CHECK_EQ(pro->all.inputOther, 176);       // cacheMiss
  CHECK_EQ(pro->all.inputCacheRead, 79104); // cacheHit
  CHECK_EQ(pro->all.output, 391);
  const ModelStat* flash = agg.model("reasonix/deepseek-v4-flash");
  CHECK(flash != nullptr);
  CHECK_EQ(flash->all.total(), 17072);      // 11369 + 5632 + 71（session:null 不丢）

  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}

TEST(reasonix_empty_home_no_crash) {
  auto home = rxTempDir("rx-empty");
  auto stateDir = rxTempDir("rx-empty-state");
  Store store(stateDir);
  ReasonixAdapter rx(home, &store);
  CHECK_EQ(rx.poll([](const UsageEvent&) {}), 0);
  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}
