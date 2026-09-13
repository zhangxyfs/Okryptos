#include "../adapters/dsh/adapter.h"
#include "../core/store.h"
#include "framework.h"
#include <fstream>
#include <process.h>

using namespace okmeter;

static std::filesystem::path dshTempDir(const char* name) {
  auto d = std::filesystem::temp_directory_path() /
           ("okmeter-test-" + std::string(name) + "-" + std::to_string(_getpid()));
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
  std::filesystem::create_directories(d);
  return d;
}

static void dshWrite(const std::filesystem::path& home, const std::string& projcache) {
  std::filesystem::create_directories(home / "storages");
  { std::ofstream f(home / "storages" / "session_projcache.json",
                    std::ios::binary | std::ios::trunc);
    f << projcache; }
  { std::ofstream f(home / "settings.yaml", std::ios::binary | std::ios::trunc);
    f << "agent-default-model:\n  provider: kimi-coding\n  model: k3-256k\n"; }
}

static std::string kProjcache = R"({
  "unit": {"name": "session_projcache", "version": 3},
  "global": null,
  "tables": {"sessions": {
    "session-aaa": {
      "identity": {"createdAt": 1786631479556},
      "rows": {
        "tokenUsage": {"ver": 1, "val": {"totals": {"uncachedInputTokens": 9306,
          "outputTokens": 121, "cacheReadTokens": 500, "cacheWriteTokens": 40}}},
        "sessionListMetadata": {"ver": 1, "val": {"lastPromptAt": 1786632069779}}
      }
    },
    "session-bbb": {
      "identity": {"createdAt": 1786631479556},
      "rows": {"title": {"ver": 1, "val": "no usage yet"}}
    }
  }}
})";

TEST(dsh_delta_and_mapping) {
  auto home = dshTempDir("dsh-home");
  auto stateDir = dshTempDir("dsh-state");
  dshWrite(home, kProjcache);

  Store store(stateDir);
  DshAdapter dsh(home, &store);
  Aggregator agg;
  int64_t ts = 0;
  std::string sid;
  int n = dsh.poll([&](const UsageEvent& e) { agg.add(e); ts = e.timeMs; sid = e.sessionId; });

  CHECK_EQ(n, 1);                       // session-bbb 无 tokenUsage 跳过
  CHECK_EQ(agg.all().total(), 9967);    // 9306+121+500+40
  const ModelStat* m = agg.model("dsh/k3-256k");  // settings.yaml 默认模型
  CHECK(m != nullptr);
  CHECK_EQ(m->all.inputOther, 9306);
  CHECK_EQ(m->all.inputCacheRead, 500);
  CHECK_EQ(m->all.inputCacheCreation, 40);
  CHECK_EQ(m->all.output, 121);
  CHECK(sid == "session-aaa");
  CHECK_EQ(ts, 1786632069779LL);

  // 累计值前进 → 只发差量；另一会话新增 → 全量
  std::string grown = kProjcache;
  const std::string from = "\"uncachedInputTokens\": 9306";
  grown.replace(grown.find(from), from.size(), "\"uncachedInputTokens\": 10306");
  dshWrite(home, grown);
  int n2 = dsh.poll([&](const UsageEvent& e) { agg.add(e); });
  CHECK_EQ(n2, 1);
  CHECK_EQ(agg.all().total(), 9967 + 1000);

  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}

TEST(dsh_empty_home_no_crash) {
  auto home = dshTempDir("dsh-empty");
  auto stateDir = dshTempDir("dsh-empty-state");
  Store store(stateDir);
  DshAdapter dsh(home, &store);
  CHECK_EQ(dsh.poll([](const UsageEvent&) {}), 0);
  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}
