#include "../core/aggregator.h"
#include "framework.h"
#include <ctime>

using namespace okmeter;

static int64_t msOf(int y, int mo, int d, int h, int mi) {
  std::tm t{};
  t.tm_year = y - 1900; t.tm_mon = mo - 1; t.tm_mday = d; t.tm_hour = h; t.tm_min = mi;
  return (int64_t)std::mktime(&t) * 1000;
}

static UsageEvent ev(const char* model, const char* sess, int64_t io,
                     int64_t icr, int64_t icc, int64_t out, int64_t t) {
  UsageEvent e;
  e.model = model; e.sessionId = sess;
  e.inputOther = io; e.inputCacheRead = icr; e.inputCacheCreation = icc; e.output = out;
  e.timeMs = t;
  return e;
}

TEST(agg_all_total_and_breakdown) {
  Aggregator a;
  a.add(ev("m/a", "s1", 100, 200, 10, 5, msOf(2026, 9, 8, 10, 0)));
  a.add(ev("m/b", "s1", 50, 0, 0, 5, msOf(2026, 9, 9, 11, 0)));
  CHECK_EQ(a.all().total(), 370);
  CHECK_EQ(a.all().inputOther, 150);
  CHECK_EQ(a.all().inputCacheRead, 200);
  CHECK_EQ(a.all().output, 10);
}

TEST(agg_today_week_boundaries) {
  const int64_t now = msOf(2026, 9, 9, 12, 0);  // 周三中午，避开 DST 边缘
  Aggregator a;
  a.add(ev("m/a", "s1", 10, 0, 0, 0, now - 3600000));            // 今天 1 小时前
  a.add(ev("m/a", "s1", 20, 0, 0, 0, now - 86400000));           // 昨天此时
  a.add(ev("m/a", "s1", 40, 0, 0, 0, now - 7 * 86400000));       // 上周此时
  CHECK_EQ(a.today(now).total(), 10);
  CHECK_EQ(a.week(now).total(), 30);   // 今天 + 昨天（同周）
  CHECK_EQ(a.all().total(), 70);
}

TEST(agg_models_grouping_and_recency) {
  const int64_t now = msOf(2026, 9, 9, 12, 0);
  Aggregator a;
  a.add(ev("m/old", "s1", 1, 0, 0, 0, now - 7200000));
  a.add(ev("m/new", "s1", 2, 0, 0, 0, now - 3600000));
  auto ranked = a.modelsByRecency();
  CHECK_EQ(ranked.size(), (size_t)2);
  CHECK(ranked[0] == "m/new");
  CHECK_EQ(a.model("m/new")->lastCallMs, now - 3600000);
  CHECK_EQ(a.modelToday("m/old", now).total(), 1);
}

TEST(agg_session_is_latest_active) {
  const int64_t now = msOf(2026, 9, 9, 12, 0);
  Aggregator a;
  a.add(ev("m/a", "s1", 10, 0, 0, 0, now - 7200000));
  a.add(ev("m/b", "s2", 20, 0, 0, 0, now - 3600000));   // s2 更晚 → 当前会话
  CHECK_EQ(a.session().total(), 20);
  CHECK_EQ(a.modelSession("m/a").total(), 0);           // m/a 在 s2 无量
  CHECK_EQ(a.modelSession("m/b").total(), 20);
}

TEST(agg_json_roundtrip) {
  const int64_t now = msOf(2026, 9, 9, 12, 0);
  Aggregator a;
  a.add(ev("m/a", "s1", 100, 200, 10, 5, now - 3600000));
  a.add(ev("m/b", "s2", 50, 0, 0, 5, now));
  Aggregator b;
  CHECK(b.fromJson(a.toJson()));
  CHECK_EQ(b.all().total(), 370);
  CHECK_EQ(b.today(now).total(), 370);
  CHECK_EQ(b.session().total(), 55);
  CHECK_EQ(b.modelsByRecency()[0], "m/b");
}

TEST(agg_fromjson_tolerates_missing_keys) {
  Aggregator a;
  a.add(ev("m/a", "s1", 100, 0, 0, 0, msOf(2026, 9, 9, 11, 0)));
  json::Value full = a.toJson();

  Aggregator b;
  CHECK(b.fromJson(full));            // 完整快照正常

  json::Value partial;                 // 只剩 models 节的残缺快照
  partial.v = json::Object{};
  partial.obj();                       // 确保是对象
  json::Value modelsOnly;
  modelsOnly.v = json::Object{};
  // 构造 {"models": {...}} 的残缺对象
  std::get<json::Object>(modelsOnly.v) = full.find("models")->obj();
  std::get<json::Object>(partial.v)["models"] = modelsOnly;
  Aggregator c;
  CHECK(c.fromJson(partial));          // 不崩、缺节保持默认
  CHECK_EQ(c.all().total(), 0);        // all 缺键 → 0
  CHECK_EQ(c.model("m/a")->all.total(), 100);

  Aggregator d;
  CHECK(!d.fromJson(json::Value{}));   // 非对象 → false
}
