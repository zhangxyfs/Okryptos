#include "../adapters/workbuddy/adapter.h"
#include "../core/store.h"
#include "framework.h"
#include <fstream>
#include <process.h>

using namespace okmeter;

static std::filesystem::path wbTempDir(const char* name) {
  auto d = std::filesystem::temp_directory_path() /
           ("okmeter-test-" + std::string(name) + "-" + std::to_string(_getpid()));
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
  std::filesystem::create_directories(d);
  return d;
}

static void wbMkfile(const std::filesystem::path& p, const std::string& content) {
  std::filesystem::create_directories(p.parent_path());
  std::ofstream f(p, std::ios::binary | std::ios::trunc);
  f << content;
}

static const char* kWbGood =
  R"({"id":"m1","timestamp":1788879523708,"type":"function_call","sessionId":"sess_wb","providerData":{"model":"hy4-preview","usage":{"requests":1,"inputTokens":33920,"outputTokens":81,"totalTokens":34001,"inputTokensDetails":[{"cached_tokens":12736}],"outputTokensDetails":[{"reasoning_tokens":7}]}},"message":{"usage":{"input_tokens":33920,"output_tokens":81,"total_tokens":34001,"cache_read_input_tokens":12736}}})";

TEST(workbuddy_scan_and_mapping) {
  auto home = wbTempDir("wb-home");
  auto stateDir = wbTempDir("wb-state");
  const auto f1 = home / "projects" / "proj-x" / "sess_wb.jsonl";
  wbMkfile(f1, std::string(kWbGood) + "\n" +
               R"({"type":"text","message":{"content":"hi"}})" + "\n{bad\n");

  Store store(stateDir);
  WorkbuddyAdapter wb(home, &store);
  Aggregator agg;
  std::string sessionId;
  int64_t ts = 0;
  int n = wb.poll([&](const UsageEvent& e) {
    agg.add(e);
    sessionId = e.sessionId;
    ts = e.timeMs;
  });

  CHECK_EQ(n, 1);
  CHECK_EQ(agg.all().total(), 34001);   // 21184 + 12736 + 81
  const ModelStat* m = agg.model("workbuddy/hy4-preview");
  CHECK(m != nullptr);
  CHECK_EQ(m->all.inputOther, 21184);   // 33920-12736
  CHECK_EQ(m->all.inputCacheRead, 12736);
  CHECK_EQ(m->all.output, 81);
  CHECK(sessionId == "sess_wb");
  CHECK_EQ(ts, 1788879523708LL);        // timestamp 直接是 epoch 毫秒

  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}

TEST(workbuddy_empty_home_no_crash) {
  auto home = wbTempDir("wb-empty");
  auto stateDir = wbTempDir("wb-empty-state");
  Store store(stateDir);
  WorkbuddyAdapter wb(home, &store);
  CHECK_EQ(wb.poll([](const UsageEvent&) {}), 0);
  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}
