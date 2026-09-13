#include "../adapters/qwen/adapter.h"
#include "../core/isotime.h"
#include "../core/store.h"
#include "framework.h"
#include <fstream>
#include <process.h>

using namespace okmeter;

static std::filesystem::path qwenTempDir(const char* name) {
  auto d = std::filesystem::temp_directory_path() /
           ("okmeter-test-" + std::string(name) + "-" + std::to_string(_getpid()));
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
  std::filesystem::create_directories(d);
  return d;
}

static void qwenMkfile(const std::filesystem::path& p, const std::string& content) {
  std::filesystem::create_directories(p.parent_path());
  std::ofstream f(p, std::ios::binary | std::ios::trunc);
  f << content;
}

static const char* kQwenGood =
  R"({"uuid":"u1","sessionId":"sess-q","timestamp":"2026-06-14T00:05:27.879Z","type":"assistant","model":"qwen3-coder-plus","message":{"role":"assistant"},"usageMetadata":{"promptTokenCount":19311,"candidatesTokenCount":22,"thoughtsTokenCount":16,"totalTokenCount":19333,"cachedContentTokenCount":500}})";

TEST(qwen_scan_and_mapping) {
  auto home = qwenTempDir("qwen-home");
  auto stateDir = qwenTempDir("qwen-state");
  const auto f1 = home / "projects" / "proj-x" / "chats" / "sess-q.jsonl";
  qwenMkfile(f1, std::string(kQwenGood) + "\n" +
                 R"({"type":"user","message":{"content":"hi"}})" + "\n{bad\n");

  Store store(stateDir);
  QwenAdapter qwen(home, &store);
  Aggregator agg;
  std::string sessionId;
  int n = qwen.poll([&](const UsageEvent& e) { agg.add(e); sessionId = e.sessionId; });

  CHECK_EQ(n, 1);
  CHECK_EQ(agg.all().total(), 19849);  // 19311 + 500 + 22 + 16
  const ModelStat* m = agg.model("qwen-code/qwen3-coder-plus");
  CHECK(m != nullptr);
  CHECK_EQ(m->all.inputOther, 19311);
  CHECK_EQ(m->all.inputCacheRead, 500);
  CHECK_EQ(m->all.output, 38);         // candidates + thoughts
  CHECK(sessionId == "sess-q");

  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}

TEST(qwen_empty_home_no_crash) {
  auto home = qwenTempDir("qwen-empty");
  auto stateDir = qwenTempDir("qwen-empty-state");
  Store store(stateDir);
  QwenAdapter qwen(home, &store);
  CHECK_EQ(qwen.poll([](const UsageEvent&) {}), 0);
  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}
