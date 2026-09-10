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

TEST(store_cursor_roundtrip) {
  auto d = tempDir("cursor");
  {
    Store s(d);
    s.setCursor("a/wire.jsonl", 1234);
    CHECK(s.flush());
  }
  {
    Store s(d);
    CHECK(s.load());
    CHECK_EQ(s.cursor("a/wire.jsonl"), 1234);
    CHECK_EQ(s.cursor("never/seen"), 0);
  }
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
}

TEST(store_agg_snapshot_roundtrip) {
  auto d = tempDir("agg");
  {
    Store s(d);
    UsageEvent e;
    e.model = "m/a"; e.sessionId = "s1";
    e.inputOther = 100; e.inputCacheRead = 50;
    e.timeMs = 1788865482021LL;
    s.agg().add(e);
    CHECK(s.flush());
  }
  {
    Store s(d);
    CHECK(s.load());
    CHECK_EQ(s.agg().all().total(), 150);
    CHECK_EQ(s.agg().modelsByRecency()[0], "m/a");
  }
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
}

TEST(store_corrupted_state_falls_back_fresh) {
  auto d = tempDir("broken");
  {
    std::ofstream f(d / "state.json", std::ios::binary);
    f << "{broken json";
  }
  Store s(d);
  CHECK(!s.load());                       // 损坏 → false，内部保持全新
  CHECK_EQ(s.cursor("x"), 0);
  CHECK_EQ(s.agg().all().total(), 0);
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
}

TEST(store_flush_is_atomic_no_tmp_left) {
  auto d = tempDir("atomic");
  Store s(d);
  s.setCursor("f", 1);
  CHECK(s.flush());
  std::error_code ec;
  CHECK(std::filesystem::exists(d / "state.json", ec));
  CHECK(!std::filesystem::exists(d / "state.json.tmp", ec));  // tmp 已被替换走
  std::filesystem::remove_all(d, ec);
}
