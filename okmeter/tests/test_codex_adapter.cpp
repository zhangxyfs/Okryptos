#include "../adapters/codex/adapter.h"
#include "../core/isotime.h"
#include "../core/store.h"
#include "framework.h"
#include <fstream>
#include <process.h>

using namespace okmeter;

static std::filesystem::path codexTempDir(const char* name) {
  auto d = std::filesystem::temp_directory_path() /
           ("okmeter-test-" + std::string(name) + "-" + std::to_string(_getpid()));
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
  std::filesystem::create_directories(d);
  return d;
}

static void codexMkfile(const std::filesystem::path& p, const std::string& content) {
  std::filesystem::create_directories(p.parent_path());
  std::ofstream f(p, std::ios::binary | std::ios::trunc);
  f << content;
}

static const char* kTurnCtx =
  R"({"timestamp":"2026-03-11T23:30:00.000Z","type":"turn_context","payload":{"model":"gpt-5-codex"}})";
static const char* kTokenCount =
  R"({"timestamp":"2026-03-11T23:30:38.331Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":34616,"cached_input_tokens":21504,"output_tokens":1337,"reasoning_output_tokens":802,"total_tokens":35953},"last_token_usage":{"input_tokens":17432,"cached_input_tokens":17152,"output_tokens":515,"reasoning_output_tokens":213,"total_tokens":17947},"model_context_window":258400},"rate_limits":null}})";
static const char* kTokenCountNullInfo =
  R"({"timestamp":"2026-03-11T23:31:00.000Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"plan_type":"free"}}})";

TEST(codex_scan_and_mapping) {
  auto home = codexTempDir("codex-home");
  auto stateDir = codexTempDir("codex-state");
  const auto f1 = home / "sessions" / "2026" / "03" / "11" / "rollout-x.jsonl";
  codexMkfile(f1, std::string(kTurnCtx) + "\n" + kTokenCount + "\n" +
                  kTokenCountNullInfo + "\n{bad json\n");

  Store store(stateDir);
  CodexAdapter codex(home, &store);
  Aggregator agg;
  std::string sessionId;
  int n = codex.poll([&](const UsageEvent& e) { agg.add(e); sessionId = e.sessionId; });

  CHECK_EQ(n, 1);                      // null-info 行跳过
  CHECK_EQ(agg.all().total(), 17947);  // (17432-17152) + 17152 + 515
  const ModelStat* m = agg.model("codex/gpt-5-codex");
  CHECK(m != nullptr);
  CHECK_EQ(m->all.inputOther, 280);    // input 含 cached：17432-17152
  CHECK_EQ(m->all.inputCacheRead, 17152);
  CHECK_EQ(m->all.output, 515);        // reasoning 不单加
  CHECK(sessionId == "rollout-x");

  // 进程重启模拟：新建适配器（models_ 清空），游标已持久化 → 头部扫描补回模型
  CodexAdapter codex2(home, &store);
  {
    std::ofstream f(f1, std::ios::binary | std::ios::app);
    f << kTokenCount << "\n";
  }
  int n2 = codex2.poll([&](const UsageEvent& e) { agg.add(e); });
  CHECK_EQ(n2, 1);
  CHECK_EQ(agg.model("codex/gpt-5-codex")->all.total(), 17947 * 2);

  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}

TEST(codex_empty_home_no_crash) {
  auto home = codexTempDir("codex-empty");
  auto stateDir = codexTempDir("codex-empty-state");
  Store store(stateDir);
  CodexAdapter codex(home, &store);
  CHECK_EQ(codex.poll([](const UsageEvent&) {}), 0);
  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}
