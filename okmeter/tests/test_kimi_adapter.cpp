#include "../adapters/kimi/adapter.h"
#include "../core/store.h"
#include "framework.h"
#include <fstream>
#include <process.h>

using namespace okmeter;

static std::filesystem::path tempDir(const char* name) {
  auto d = std::filesystem::temp_directory_path() /
           ("okmeter-test-" + std::string(name) + "-" + std::to_string(_getpid()));
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
  std::filesystem::create_directories(d);
  return d;
}

static void mkfile(const std::filesystem::path& p, const std::string& content) {
  std::filesystem::create_directories(p.parent_path());
  std::ofstream f(p, std::ios::binary | std::ios::trunc);
  f << content;
}

static const char* kGood1 =
  R"({"type":"usage.record","agentId":"main","model":"m/a","usage":{"inputOther":100,"output":10,"inputCacheRead":50,"inputCacheCreation":5},"usageScope":"turn","time":1788865482021})";
static const char* kGood2 =
  R"({"type":"usage.record","agentId":"main","model":"m/b","usage":{"inputOther":200,"output":20,"inputCacheRead":0,"inputCacheCreation":0},"usageScope":"turn","time":1788865483021})";

TEST(kimi_scan_increment_and_tolerance) {
  auto home = tempDir("kimi-home");
  auto stateDir = tempDir("kimi-state");
  const auto f1 = home / "sessions" / "wd_abc" / "session_1111" / "agents" / "main" / "wire.jsonl";
  const auto f2 = home / "sessions" / "wd_abc" / "session_1111" / "agents" / "agent-1" / "wire.jsonl";
  const auto f3 = home / "sessions" / "wd_def" / "session_2222" / "agents" / "main" / "wire.jsonl";
  mkfile(f1, std::string(kGood1) + "\n{bad json\n" +
             R"({"type":"other","x":1})" "\n" + kGood2 + "\n");
  // 简报夹具少一条完整 kGood1，与勘误预期（n=4 / 165*3+220 / m/a=495）矛盾；
  // 这里给 f2 补一条，使 3×kGood1 + 1×kGood2 成立
  mkfile(f2, std::string(kGood1) + "\n" + kGood1 + "\n");
  mkfile(f3, std::string(R"({"type":"usage.record","model":"m/c"})") + "\n" +  // 缺 usage → 跳过
             std::string(kGood1));  // 结尾无换行的半行 → 留给下一轮

  Store store(stateDir);
  KimiAdapter kimi(home, &store);
  Aggregator agg;
  std::string lastSession;
  int n = kimi.poll([&](const UsageEvent& e) { agg.add(e); lastSession = e.sessionId; });

  CHECK_EQ(n, 4);                      // f1 两条 + f2 两条 + f3 零条完整好行
  CHECK_EQ(agg.all().total(), 715);    // (100+10+50+5)*3 + (200+20)
  CHECK_EQ(agg.model("m/a")->all.total(), 495);
  CHECK_EQ(agg.model("m/b")->all.total(), 220);
  CHECK(lastSession == "session_1111"); // sessionId 从路径提取

  // 第二轮：补齐半行 + 追加一条新行 → 只增量产出 2 条
  {
    std::ofstream f(f3, std::ios::binary | std::ios::app);
    f << "\n" << kGood2 << "\n";
  }
  int n2 = kimi.poll([&](const UsageEvent& e) { agg.add(e); });
  CHECK_EQ(n2, 2);
  CHECK_EQ(agg.all().total(), 1100);   // 715 + 165 + 220

  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}

TEST(kimi_empty_home_no_crash) {
  auto home = tempDir("kimi-empty");
  auto stateDir = tempDir("kimi-empty-state");
  Store store(stateDir);
  KimiAdapter kimi(home, &store);
  int n = kimi.poll([](const UsageEvent&) {});
  CHECK_EQ(n, 0);
  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}
