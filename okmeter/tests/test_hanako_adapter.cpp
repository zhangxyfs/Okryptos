#include "../adapters/hanako/adapter.h"
#include "../core/isotime.h"
#include "../core/store.h"
#include "framework.h"
#include <fstream>
#include <process.h>

using namespace okmeter;

static std::filesystem::path hkTempDir(const char* name) {
  auto d = std::filesystem::temp_directory_path() /
           ("okmeter-test-" + std::string(name) + "-" + std::to_string(_getpid()));
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
  std::filesystem::create_directories(d);
  return d;
}

// Anthropic 系（input 不含 cache）：total = in+out+cr+cw
static const char* kHkAnthropic =
  R"([06:52:41.275] [INFO] [llm] llm-usage model_usage {"source":"sess_x","api":"anthropic","provider":"bigmodel","modelId":"glm-5.3","inputTokens":615,"outputTokens":1145,"cacheReadTokens":223872,"cacheWriteTokens":100,"totalTokens":225732})";
// OpenAI 系（input 含 cached）：total = in+out
static const char* kHkOpenAI =
  R"([06:53:00.100] [INFO] [llm] llm-usage model_usage {"source":"sess_x","api":"openai","provider":"deepseek","modelId":"deepseek-v4","inputTokens":33920,"outputTokens":81,"cacheReadTokens":12736,"cacheWriteTokens":0,"totalTokens":34001})";

TEST(hanako_scan_and_mapping) {
  auto home = hkTempDir("hk-home");
  auto stateDir = hkTempDir("hk-state");
  const auto f1 = home / "logs" / "2026-09-13_06-50-00.log";
  std::filesystem::create_directories(f1.parent_path());
  {
    std::ofstream f(f1, std::ios::binary | std::ios::trunc);
    f << "[06:50:01.000] [INFO] [boot] server started\n"
      << kHkAnthropic << "\n" << kHkOpenAI << "\nnot json at all\n";
  }

  Store store(stateDir);
  HanakoAdapter hk(home, &store);
  Aggregator agg;
  int64_t ts = 0;
  int n = hk.poll([&](const UsageEvent& e) { agg.add(e); ts = e.timeMs; });

  CHECK_EQ(n, 2);
  const ModelStat* glm = agg.model("hanako/glm-5.3");
  CHECK(glm != nullptr);
  CHECK_EQ(glm->all.total(), 225732);       // 615 + 223872 + 100 + 1145
  CHECK_EQ(glm->all.inputOther, 615);       // 不含 cache：原值
  const ModelStat* ds = agg.model("hanako/deepseek-v4");
  CHECK(ds != nullptr);
  CHECK_EQ(ds->all.total(), 34001);         // 21184 + 12736 + 81
  CHECK_EQ(ds->all.inputOther, 21184);      // 含 cached：33920-12736 拆开
  CHECK(ts > 0);                            // 文件名日期+行首墙钟合成

  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}

TEST(hanako_empty_home_no_crash) {
  auto home = hkTempDir("hk-empty");
  auto stateDir = hkTempDir("hk-empty-state");
  Store store(stateDir);
  HanakoAdapter hk(home, &store);
  CHECK_EQ(hk.poll([](const UsageEvent&) {}), 0);
  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}
