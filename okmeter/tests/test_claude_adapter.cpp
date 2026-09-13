#include "../adapters/claude/adapter.h"
#include "../core/isotime.h"
#include "../core/store.h"
#include "framework.h"
#include <fstream>
#include <process.h>

using namespace okmeter;

static std::filesystem::path claudeTempDir(const char* name) {
  auto d = std::filesystem::temp_directory_path() /
           ("okmeter-test-" + std::string(name) + "-" + std::to_string(_getpid()));
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
  std::filesystem::create_directories(d);
  return d;
}

static void claudeMkfile(const std::filesystem::path& p, const std::string& content) {
  std::filesystem::create_directories(p.parent_path());
  std::ofstream f(p, std::ios::binary | std::ios::trunc);
  f << content;
}

static const char* kClaudeGood =
  R"({"parentUuid":"u1","isSidechain":false,"sessionId":"sess-a","timestamp":"2026-08-12T10:44:13.573Z","message":{"id":"msg_1","role":"assistant","model":"claude-opus-4-1","content":[],"usage":{"input_tokens":288,"cache_creation_input_tokens":10,"cache_read_input_tokens":50,"output_tokens":52}}})";

TEST(claude_scan_and_mapping) {
  auto home = claudeTempDir("claude-home");
  auto stateDir = claudeTempDir("claude-state");
  const auto f1 = home / "projects" / "C--Users-x" / "sess-a.jsonl";
  claudeMkfile(f1, std::string(kClaudeGood) + "\n{bad json\n" +
                   R"({"type":"user","message":{"role":"user","content":"hi"}})" + "\n" +
                   // 零用量占位行（<synthetic>）必须跳过
                   R"({"sessionId":"sess-a","timestamp":"2026-08-12T10:45:00.000Z","message":{"role":"assistant","model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0}}})" + "\n");

  Store store(stateDir);
  ClaudeAdapter claude(home, &store);
  Aggregator agg;
  int64_t ts = 0;
  std::string sessionId;
  int n = claude.poll([&](const UsageEvent& e) {
    agg.add(e);
    ts = e.timeMs;
    sessionId = e.sessionId;
  });

  CHECK_EQ(n, 1);
  CHECK_EQ(agg.all().total(), 400);    // 288 + 10 + 50 + 52
  const ModelStat* m = agg.model("claude-code/claude-opus-4-1");
  CHECK(m != nullptr);
  CHECK_EQ(m->all.inputOther, 288);
  CHECK_EQ(m->all.inputCacheRead, 50);
  CHECK_EQ(m->all.inputCacheCreation, 10);
  CHECK_EQ(m->all.output, 52);
  CHECK(sessionId == "sess-a");
  CHECK_EQ(ts, isoToMs("2026-08-12T10:44:13.573Z"));
  CHECK(ts > 0);

  // 增量：追加一条只多产 1 条
  {
    std::ofstream f(f1, std::ios::binary | std::ios::app);
    f << kClaudeGood << "\n";
  }
  int n2 = claude.poll([&](const UsageEvent& e) { agg.add(e); });
  CHECK_EQ(n2, 1);
  CHECK_EQ(agg.all().total(), 800);

  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}

TEST(claude_empty_home_no_crash) {
  auto home = claudeTempDir("claude-empty");
  auto stateDir = claudeTempDir("claude-empty-state");
  Store store(stateDir);
  ClaudeAdapter claude(home, &store);
  CHECK_EQ(claude.poll([](const UsageEvent&) {}), 0);
  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}
