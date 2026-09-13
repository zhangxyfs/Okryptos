#include "../adapters/zcode/adapter.h"
#include "../core/isotime.h"
#include "../core/store.h"
#include "framework.h"
#include <fstream>
#include <process.h>

using namespace okmeter;

static std::filesystem::path zcodeTempDir(const char* name) {
  auto d = std::filesystem::temp_directory_path() /
           ("okmeter-test-" + std::string(name) + "-" + std::to_string(_getpid()));
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
  std::filesystem::create_directories(d);
  return d;
}

static void zcodeMkfile(const std::filesystem::path& p, const std::string& content) {
  std::filesystem::create_directories(p.parent_path());
  std::ofstream f(p, std::ios::binary | std::ios::trunc);
  f << content;
}

static const char* kZcodeGood =
  R"({"completedAt":"2026-08-27T06:52:41.275Z","requestId":"r1","sessionId":"sess_z","model":{"modelId":"GLM-5.3-Flash","providerId":"builtin:bigmodel"},"request":{},"response":{"finishReason":"stop","usage":{"inputTokens":224487,"outputTokens":1145,"totalTokens":225632,"cacheReadTokens":223872,"cacheWriteTokens":100}}})";

TEST(zcode_scan_and_mapping) {
  auto home = zcodeTempDir("zcode-home");
  auto stateDir = zcodeTempDir("zcode-state");
  const auto f1 = home / "cli" / "rollout" / "model-io-sess_z.jsonl";
  zcodeMkfile(f1, std::string(kZcodeGood) + "\n" +
                  R"({"completedAt":"2026-08-27T06:53:00.000Z","response":{"usage":{}}})" + "\n" +  // 空 usage 跳过
                  R"({"junk":true})" + "\n{bad\n");

  Store store(stateDir);
  ZcodeAdapter zcode(home, &store);
  Aggregator agg;
  std::string sessionId;
  int64_t ts = 0;
  int n = zcode.poll([&](const UsageEvent& e) {
    agg.add(e);
    sessionId = e.sessionId;
    ts = e.timeMs;
  });

  CHECK_EQ(n, 1);
  CHECK_EQ(agg.all().total(), 225632);  // 515+223872+100+1145
  const ModelStat* m = agg.model("zcode/GLM-5.3-Flash");
  CHECK(m != nullptr);
  CHECK_EQ(m->all.inputOther, 515);     // input 含 cache：224487-223872-100
  CHECK_EQ(m->all.inputCacheRead, 223872);
  CHECK_EQ(m->all.inputCacheCreation, 100);
  CHECK_EQ(m->all.output, 1145);
  CHECK(sessionId == "sess_z");
  CHECK_EQ(ts, isoToMs("2026-08-27T06:52:41.275Z"));

  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}

TEST(zcode_empty_home_no_crash) {
  auto home = zcodeTempDir("zcode-empty");
  auto stateDir = zcodeTempDir("zcode-empty-state");
  Store store(stateDir);
  ZcodeAdapter zcode(home, &store);
  CHECK_EQ(zcode.poll([](const UsageEvent&) {}), 0);
  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}
