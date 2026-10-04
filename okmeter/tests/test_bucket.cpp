#include "../core/bucket.h"
#include "../core/minjson.h"
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

// ── msgs 轮数 ──────────────────────────────────────────────

TEST(msgs_add_counts_each_event) {
  const int64_t now = msOf(2026, 9, 16, 12, 0);
  Aggregator a;
  a.add(ev("m/a", "s1", 100, 0, 0, 0, now - 3600000));
  a.add(ev("m/a", "s1", 50, 0, 0, 0, now));
  CHECK_EQ(a.all().msgs, 2);
  CHECK_EQ(a.today(now).msgs, 2);
  CHECK_EQ(a.session().msgs, 2);
  const ModelStat* m = a.model("m/a");
  CHECK_EQ(m->all.msgs, 2);
  CHECK_EQ(m->byDay.begin()->second.msgs, 2);
  CHECK_EQ(m->bySession.begin()->second.msgs, 2);
  // total() 语义不变：仍只算 token，不含 msgs
  CHECK_EQ(a.all().total(), 150);
}

TEST(msgs_plus_accumulates) {
  Sums x = Sums::of(ev("m/a", "s1", 10, 0, 0, 0, msOf(2026, 9, 16, 10, 0)));
  Sums y = Sums::of(ev("m/a", "s1", 20, 0, 0, 0, msOf(2026, 9, 16, 11, 0)));
  CHECK_EQ(x.msgs, 1);
  x.plus(y);
  CHECK_EQ(x.msgs, 2);
  CHECK_EQ(x.total(), 30);
}

TEST(msgs_json_roundtrip) {
  const int64_t now = msOf(2026, 9, 16, 12, 0);
  Aggregator a;
  a.add(ev("m/a", "s1", 100, 0, 0, 0, now - 3600000));
  a.add(ev("m/a", "s1", 50, 0, 0, 0, now));
  Aggregator b;
  CHECK(b.fromJson(a.toJson()));
  CHECK_EQ(b.all().msgs, 2);
  CHECK_EQ(b.today(now).msgs, 2);
  CHECK_EQ(b.session().msgs, 2);
  CHECK_EQ(b.model("m/a")->all.msgs, 2);
  CHECK_EQ(b.model("m/a")->byDay.begin()->second.msgs, 2);
}

TEST(msgs_json_missing_defaults_zero) {
  json::Value v;
  CHECK(json::parse("{\"all\":{\"io\":5,\"o\":7},\"days\":{\"20260910\":{\"io\":3}}}", v));
  Aggregator a;
  CHECK(a.fromJson(v));
  CHECK_EQ(a.all().total(), 12);      // token 字段照常读
  CHECK_EQ(a.all().msgs, 0);          // 旧快照无 msgs → 0
  CHECK_EQ(a.days().begin()->second.msgs, 0);
}

// ── 分桶 ──────────────────────────────────────────────────
// 2026-09-16 是周三；当周周一 = 2026-09-14，上周一 = 2026-09-07

TEST(bucket_day_window_zero_gaps_not_compressed) {
  const int64_t now = msOf(2026, 9, 16, 12, 0);
  Aggregator a;
  a.add(ev("m/a", "s1", 10, 0, 0, 0, msOf(2026, 9, 13, 10, 0)));
  a.add(ev("m/a", "s1", 40, 0, 0, 0, msOf(2026, 9, 16, 9, 0)));
  auto bs = bucketSeries(a, BucketRange::Day, 7, 0, now);
  CHECK_EQ(bs.size(), (size_t)7);
  CHECK_EQ(bs[0].key, 20260910);          // 最旧在前
  CHECK_EQ(bs[6].key, 20260916);          // 含当前桶
  for (size_t i = 1; i < bs.size(); ++i)  // 键连续不压缩
    CHECK(bs[i].key > bs[i - 1].key);
  CHECK_EQ(bs[3].key, 20260913);
  CHECK_EQ(bs[3].total.total(), 10);
  CHECK_EQ(bs[3].total.msgs, 1);
  CHECK_EQ(bs[6].total.total(), 40);
  CHECK_EQ(bs[0].total.total(), 0);       // 零值桶照常产出
  CHECK_EQ(bs[6].beginKey, 20260916);
  CHECK_EQ(bs[6].endKey, 20260916);
}

TEST(bucket_week_monday_boundary) {
  const int64_t now = msOf(2026, 9, 16, 12, 0);
  Aggregator a;
  a.add(ev("m/a", "s1", 10, 0, 0, 0, msOf(2026, 9, 6, 23, 59)));  // 周日 → 上周
  a.add(ev("m/a", "s1", 20, 0, 0, 0, msOf(2026, 9, 7, 0, 0)));    // 周一 → 本周
  auto bs = bucketSeries(a, BucketRange::Week, 4, 0, now);
  CHECK_EQ(bs.size(), (size_t)4);
  CHECK_EQ(bs[3].key, 20260914);          // 当前周（周一键）
  CHECK_EQ(bs[3].beginKey, 20260914);
  CHECK_EQ(bs[3].endKey, 20260920);
  CHECK_EQ(bs[2].key, 20260907);
  CHECK_EQ(bs[2].total.total(), 20);      // 周一那笔
  CHECK_EQ(bs[1].key, 20260831);
  CHECK_EQ(bs[1].total.total(), 10);      // 周日深夜归上一周
  CHECK_EQ(bs[0].total.total(), 0);
}

TEST(bucket_month_year_boundaries) {
  const int64_t now = msOf(2026, 3, 15, 12, 0);
  Aggregator a;
  a.add(ev("m/a", "s1", 10, 0, 0, 0, msOf(2025, 12, 31, 23, 59)));  // 去年 12 月
  a.add(ev("m/a", "s1", 20, 0, 0, 0, msOf(2026, 1, 1, 0, 0)));      // 今年 1 月
  a.add(ev("m/a", "s1", 40, 0, 0, 0, msOf(2026, 2, 10, 10, 0)));    // 今年 2 月
  auto ms = bucketSeries(a, BucketRange::Month, 3, 0, now);
  CHECK_EQ(ms.size(), (size_t)3);
  CHECK_EQ(ms[0].key, 202601);
  CHECK_EQ(ms[0].total.total(), 20);
  CHECK_EQ(ms[1].key, 202602);
  CHECK_EQ(ms[1].total.total(), 40);
  CHECK_EQ(ms[1].beginKey, 20260201);
  CHECK_EQ(ms[1].endKey, 20260228);       // 2026 非闰年
  CHECK_EQ(ms[2].key, 202603);
  CHECK_EQ(ms[2].total.total(), 0);
  auto ys = bucketSeries(a, BucketRange::Year, 2, 0, now);
  CHECK_EQ(ys.size(), (size_t)2);
  CHECK_EQ(ys[0].key, 2025);
  CHECK_EQ(ys[0].total.total(), 10);      // 跨年归桶正确
  CHECK_EQ(ys[0].beginKey, 20250101);
  CHECK_EQ(ys[0].endKey, 20251231);
  CHECK_EQ(ys[1].key, 2026);
  CHECK_EQ(ys[1].total.total(), 60);
}

TEST(bucket_offset_shifts_window_to_past) {
  const int64_t now = msOf(2026, 9, 16, 12, 0);
  Aggregator a;
  a.add(ev("m/a", "s1", 10, 0, 0, 0, msOf(2026, 9, 13, 10, 0)));
  auto bs = bucketSeries(a, BucketRange::Day, 3, 2, now);
  CHECK_EQ(bs.size(), (size_t)3);
  CHECK_EQ(bs[0].key, 20260912);          // 窗口整体向过去挪 2 桶（最新桶 = 今天-2）
  CHECK_EQ(bs[2].key, 20260914);
  CHECK_EQ(bs[1].total.total(), 10);      // 09-13 那笔落在中间桶
  auto ws = bucketSeries(a, BucketRange::Week, 2, 1, now);
  CHECK_EQ(ws[1].key, 20260907);          // 周窗口挪 1 周
}

TEST(bucket_provider_merge_and_order) {
  const int64_t now = msOf(2026, 9, 16, 12, 0);
  Aggregator a;
  a.add(ev("ns1/kimi-a", "s1", 10, 0, 0, 0, now));
  a.add(ev("ns2/k2-b", "s1", 30, 0, 0, 0, now));   // 同提供商（月之暗面）归并
  a.add(ev("ns/gpt-4", "s1", 5, 0, 0, 0, now));
  auto bs = bucketSeries(a, BucketRange::Day, 1, 0, now);
  CHECK_EQ(bs.size(), (size_t)1);
  CHECK_EQ(bs[0].total.total(), 45);
  CHECK_EQ(bs[0].total.msgs, 3);
  CHECK_EQ(bs[0].segments.size(), (size_t)2);
  CHECK(bs[0].segments[0].id == "月之暗面");      // 量大者在前
  CHECK(bs[0].segments[0].label == "月之暗面");
  CHECK_EQ(bs[0].segments[0].sums.total(), 40);   // 两模型合一
  CHECK_EQ(bs[0].segments[0].sums.msgs, 2);
  CHECK(bs[0].segments[1].id == "OpenAI");
  CHECK_EQ(bs[0].segments[1].sums.total(), 5);
}

TEST(bucket_drill_splits_models_of_one_provider) {
  const int64_t now = msOf(2026, 9, 16, 12, 0);
  Aggregator a;
  a.add(ev("ns1/kimi-a", "s1", 10, 0, 0, 0, now));
  a.add(ev("ns2/k2-b", "s1", 30, 0, 0, 0, now));
  a.add(ev("ns/gpt-4", "s1", 5, 0, 0, 0, now));
  auto bs = bucketSeries(a, BucketRange::Day, 1, 0, now, "月之暗面");
  CHECK_EQ(bs[0].total.total(), 40);        // 下钻：total = 该提供商合计
  CHECK_EQ(bs[0].segments.size(), (size_t)2);
  CHECK(bs[0].segments[0].id == "ns2/k2-b");      // 全名
  CHECK(bs[0].segments[0].label == "k2-b");       // 短名
  CHECK_EQ(bs[0].segments[0].sums.total(), 30);
  CHECK(bs[0].segments[1].id == "ns1/kimi-a");
  CHECK(bs[0].segments[1].label == "kimi-a");
  auto none = bucketSeries(a, BucketRange::Day, 1, 0, now, "不存在的厂");
  CHECK_EQ(none[0].total.total(), 0);
  CHECK_EQ(none[0].segments.size(), (size_t)0);
}

TEST(bucket_drill_merges_case_variants) {
  const int64_t now = msOf(2026, 9, 16, 12, 0);
  Aggregator a;
  // GLM-5.3 与 glm-5.3 是同一模型：下钻拆分按小写规范化归并（providerOf 本
  // 就小写判前缀，两家都归 智谱）；显示名 = 最近使用的原始大小写
  a.add(ev("zcode/glm-5.3", "s1", 10, 0, 0, 0, now - 3600000));
  a.add(ev("zcode/GLM-5.3", "s1", 30, 0, 0, 0, now));  // 最近使用 = 大写
  auto bs = bucketSeries(a, BucketRange::Day, 1, 0, now, "智谱");
  CHECK_EQ(bs[0].segments.size(), (size_t)1);   // 只出现一次
  CHECK_EQ(bs[0].segments[0].sums.total(), 40); // 两变体合计
  CHECK(bs[0].segments[0].label == "GLM-5.3");  // 最近原始大小写
  CHECK(bs[0].segments[0].id == "zcode/glm-5.3");  // 归并键 = 小写全 id
  CHECK_EQ(bs[0].total.total(), 40);
}

TEST(bucket_drill_suffix_dup_short_names) {
  const int64_t now = msOf(2026, 9, 16, 12, 0);
  Aggregator a;
  // 归并后仍同短名（不同命名空间）：kimi-code/k3 vs claude-code/k3 → 同提供商
  //（月之暗面），标签加" · 命名空间"后缀（级联菜单既有规则）
  a.add(ev("kimi-code/k3", "s1", 10, 0, 0, 0, now - 3600000));
  a.add(ev("claude-code/k3", "s1", 30, 0, 0, 0, now));
  auto bs = bucketSeries(a, BucketRange::Day, 1, 0, now, "月之暗面");
  CHECK_EQ(bs[0].segments.size(), (size_t)2);
  CHECK(bs[0].segments[0].label == "k3 · claude-code");  // 量大者在前
  CHECK(bs[0].segments[1].label == "k3 · kimi-code");
  // 不同短名不加后缀
  a.add(ev("kimi-code/k3-256k", "s1", 50, 0, 0, 0, now));
  auto bs2 = bucketSeries(a, BucketRange::Day, 1, 0, now, "月之暗面");
  for (const auto& s : bs2[0].segments)
    if (s.id == "kimi-code/k3-256k") CHECK(s.label == "k3-256k");
}

// ── 热力图 ────────────────────────────────────────────────

TEST(heatmap_day_cells_window_and_thresholds) {
  const int64_t now = msOf(2026, 9, 16, 12, 0);   // 周三
  Aggregator a;
  a.add(ev("m/a", "s1", 10, 0, 0, 0, msOf(2026, 9, 8, 10, 0)));
  a.add(ev("m/a", "s1", 20, 0, 0, 0, msOf(2026, 9, 10, 10, 0)));
  a.add(ev("m/a", "s1", 30, 0, 0, 0, msOf(2026, 9, 12, 10, 0)));
  a.add(ev("m/a", "s1", 40, 0, 0, 0, msOf(2026, 9, 16, 10, 0)));
  Heatmap h = heatmap(a, HeatGrain::Day, 2, now);
  CHECK_EQ(h.cells.size(), (size_t)10);           // 上周一 09-07 ~ 今天，逐日连续
  CHECK_EQ(h.cells[0].dayKey, 20260907);
  CHECK_EQ(h.cells[9].dayKey, 20260916);
  CHECK_EQ(h.cells[0].sums.total(), 0);           // 窗口内零值格照常产出
  CHECK_EQ(h.cells[1].sums.total(), 10);
  // 非零格 {10,20,30,40} 最近秩分位
  CHECK_EQ(h.p50, 20);
  CHECK_EQ(h.p75, 30);
  CHECK_EQ(h.p90, 40);
}

TEST(heatmap_full_span_zero_fills_before_data) {
  // 用户裁决（2026-09-17 截图）：固定 52 周全跨度——数据不足一窗不再左对齐截短，
  // 数据开始前的日子画空档格（GitHub 同款），月份轴得以标满全跨度不留右侧空白
  const int64_t now = msOf(2026, 9, 16, 12, 0);
  Aggregator a;
  a.add(ev("m/a", "s1", 10, 0, 0, 0, msOf(2026, 9, 14, 10, 0)));  // 本周一
  Heatmap h = heatmap(a, HeatGrain::Day, 52, now);
  CHECK_EQ(h.cells[0].dayKey, 20250922);   // 起点 = 本周往回 51 周的周一
  CHECK_EQ(h.cells.size(), (size_t)360);   // 起点 ~ 今天逐日连续
  CHECK_EQ(h.cells[356].dayKey, 20260913); // 数据开始前一天 = 空档格
  CHECK_EQ(h.cells[356].sums.total(), 0);
  CHECK_EQ(h.cells[357].dayKey, 20260914);
  CHECK_EQ(h.cells[357].sums.total(), 10); // 有数据日照常着色
  Heatmap empty = heatmap(Aggregator(), HeatGrain::Day, 4, now);
  CHECK_EQ(empty.cells.size(), (size_t)24);  // 全跨度 4 周（8/24~9/16）
  CHECK_EQ(empty.p50, 0);                    // 无非零格 → 阈值全 0
  CHECK_EQ(empty.p90, 0);
}

TEST(heatmap_week_grain_sums) {
  const int64_t now = msOf(2026, 9, 16, 12, 0);
  Aggregator a;
  a.add(ev("m/a", "s1", 7, 0, 0, 0, msOf(2026, 8, 24, 10, 0)));   // 窗口首周周一
  a.add(ev("m/a", "s1", 10, 0, 0, 0, msOf(2026, 9, 7, 10, 0)));   // 上周一
  a.add(ev("m/a", "s1", 20, 0, 0, 0, msOf(2026, 9, 13, 10, 0)));  // 上周日
  a.add(ev("m/a", "s1", 40, 0, 0, 0, msOf(2026, 9, 15, 10, 0)));  // 本周二
  Heatmap h = heatmap(a, HeatGrain::Week, 4, now);
  CHECK_EQ(h.cells.size(), (size_t)4);     // 每周一格
  CHECK_EQ(h.cells[0].dayKey, 20260824);
  CHECK_EQ(h.cells[0].sums.total(), 7);
  CHECK_EQ(h.cells[1].sums.total(), 0);    // 零值周照常产出
  CHECK_EQ(h.cells[2].dayKey, 20260907);
  CHECK_EQ(h.cells[2].sums.total(), 30);   // 上周合计
  CHECK_EQ(h.cells[3].dayKey, 20260914);
  CHECK_EQ(h.cells[3].sums.total(), 40);
}

TEST(heatmap_week_cumulative_resets_on_monday) {
  const int64_t now = msOf(2026, 9, 16, 12, 0);
  Aggregator a;
  a.add(ev("m/a", "s1", 10, 0, 0, 0, msOf(2026, 9, 7, 10, 0)));   // 上周一
  a.add(ev("m/a", "s1", 30, 0, 0, 0, msOf(2026, 9, 9, 10, 0)));   // 上周三
  a.add(ev("m/a", "s1", 5, 0, 0, 0, msOf(2026, 9, 14, 10, 0)));   // 本周一
  Heatmap h = heatmap(a, HeatGrain::WeekCumulative, 2, now);
  CHECK_EQ(h.cells[0].dayKey, 20260907);
  CHECK_EQ(h.cells[0].sums.total(), 10);   // 周一 = 当日
  CHECK_EQ(h.cells[1].sums.total(), 10);   // 周二累计不变
  CHECK_EQ(h.cells[2].sums.total(), 40);   // 周三累计 10+30
  CHECK_EQ(h.cells[7].dayKey, 20260914);
  CHECK_EQ(h.cells[7].sums.total(), 5);    // 新周一清零重计
  CHECK_EQ(h.cells[9].sums.total(), 5);
}

// ── 累计摘要 ──────────────────────────────────────────────

TEST(summary_six_fields) {
  const int64_t now = msOf(2026, 9, 16, 12, 0);
  Aggregator a;
  a.add(ev("m/a", "s1", 100, 0, 0, 0, msOf(2026, 9, 10, 10, 0)));
  a.add(ev("m/a", "s1", 300, 0, 0, 0, msOf(2026, 9, 15, 10, 0)));
  a.add(ev("m/b", "s1", 50, 0, 0, 0, msOf(2026, 9, 15, 11, 0)));  // 同日两轮
  a.add(ev("m/a", "s1", 50, 0, 0, 0, msOf(2026, 9, 16, 9, 0)));
  TotalsSummary t = totalsSummary(a, now);
  CHECK_EQ(t.totalTokens, 500);
  CHECK_EQ(t.totalMsgs, 4);
  CHECK_EQ(t.activeDays, 3);
  CHECK_EQ(t.avgTokensPerActiveDay, 166);   // 500/3 整除截断
  CHECK_EQ(t.peakDayKey, 20260915);         // 350 > 100 > 50
  CHECK_EQ(t.peakDayTokens, 350);
  CHECK_EQ(t.currentStreak, 2);             // 今天 + 昨天，前天断档
}

TEST(summary_streak_gap_and_empty_today) {
  Aggregator a;
  a.add(ev("m/a", "s1", 10, 0, 0, 0, msOf(2026, 9, 14, 10, 0)));
  a.add(ev("m/a", "s1", 10, 0, 0, 0, msOf(2026, 9, 15, 10, 0)));
  a.add(ev("m/a", "s1", 10, 0, 0, 0, msOf(2026, 9, 10, 10, 0)));  // 更早前孤立一天
  TotalsSummary t = totalsSummary(a, msOf(2026, 9, 16, 12, 0));
  CHECK_EQ(t.currentStreak, 0);             // 当天无量 → 0
  TotalsSummary y = totalsSummary(a, msOf(2026, 9, 15, 12, 0));
  CHECK_EQ(y.currentStreak, 2);             // 09-15/09-14 连续，09-13 断档
  TotalsSummary none = totalsSummary(Aggregator(), msOf(2026, 9, 16, 12, 0));
  CHECK_EQ(none.activeDays, 0);
  CHECK_EQ(none.avgTokensPerActiveDay, 0);  // 0 除防
  CHECK_EQ(none.peakDayKey, 0);
}
