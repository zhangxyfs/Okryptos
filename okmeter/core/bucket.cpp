#include "bucket.h"
#include "provider.h"
#include <algorithm>
#include <cmath>
#include <ctime>
#include <map>

namespace okmeter {
namespace {

int64_t dayKeyToMs(int key) {  // 正午，避开 DST 切换边缘（同 aggregator.cpp 口径）
  std::tm t{};
  t.tm_year = key / 10000 - 1900;
  t.tm_mon = (key / 100) % 100 - 1;
  t.tm_mday = key % 100;
  t.tm_hour = 12;
  return (int64_t)std::mktime(&t) * 1000;
}

int addDays(int key, int n) {
  return Aggregator::dayKey(dayKeyToMs(key) + (int64_t)n * 86400000);
}

int stepMonths(int yyyymm01, int n) {  // n<0 向过去；mktime 规范化越界 tm_mon
  std::tm t{};
  t.tm_year = yyyymm01 / 10000 - 1900;
  t.tm_mon = (yyyymm01 / 100) % 100 - 1 + n;
  t.tm_mday = 1;
  t.tm_hour = 12;
  return Aggregator::dayKey((int64_t)std::mktime(&t) * 1000);
}

int stepYears(int yyyy0101, int n) {
  std::tm t{};
  t.tm_year = yyyy0101 / 10000 - 1900 + n;
  t.tm_mon = 0;
  t.tm_mday = 1;
  t.tm_hour = 12;
  return Aggregator::dayKey((int64_t)std::mktime(&t) * 1000);
}

// 当前桶（含 nowMs 的那个）的起始日
int currentBegin(BucketRange range, int64_t nowMs) {
  const int dk = Aggregator::dayKey(nowMs);
  switch (range) {
    case BucketRange::Day: return dk;
    case BucketRange::Week: return Aggregator::weekStartKey(nowMs);
    case BucketRange::Month: return (dk / 100) * 100 + 1;
    case BucketRange::Year: return (dk / 10000) * 10000 + 101;
  }
  return dk;
}

int stepBack(BucketRange range, int beginKey, int n) {
  switch (range) {
    case BucketRange::Day: return addDays(beginKey, -n);
    case BucketRange::Week: return addDays(beginKey, -7 * n);
    case BucketRange::Month: return stepMonths(beginKey, -n);
    case BucketRange::Year: return stepYears(beginKey, -n);
  }
  return beginKey;
}

int endOf(BucketRange range, int beginKey) {
  switch (range) {
    case BucketRange::Day: return beginKey;
    case BucketRange::Week: return addDays(beginKey, 6);
    case BucketRange::Month: return addDays(stepMonths(beginKey, 1), -1);
    case BucketRange::Year: return addDays(stepYears(beginKey, 1), -1);
  }
  return beginKey;
}

int keyOf(BucketRange range, int beginKey) {
  switch (range) {
    case BucketRange::Day:
    case BucketRange::Week: return beginKey;
    case BucketRange::Month: return beginKey / 100;
    case BucketRange::Year: return beginKey / 10000;
  }
  return beginKey;
}

int bucketKeyOfDay(BucketRange range, int dayKey) {
  return range == BucketRange::Week
             ? Aggregator::weekStartKey(dayKeyToMs(dayKey))
             : keyOf(range, dayKey);
}

std::string shortName(const std::string& modelId) {
  const size_t slash = modelId.rfind('/');
  return slash == std::string::npos ? modelId : modelId.substr(slash + 1);
}

// 命名空间段（/ 前；无 / 则空）。agent 工具名（kimi-code/claude-code…）
std::string vendorOf(const std::string& modelId) {
  const size_t slash = modelId.rfind('/');
  return slash == std::string::npos ? std::string() : modelId.substr(0, slash);
}

std::string lowerId(std::string s) {  // 大小写不敏感归并键（GLM-5.3 ≡ glm-5.3）
  for (auto& c : s)
    if (c >= 'A' && c <= 'Z') c = (char)(c - 'A' + 'a');
  return s;
}

bool hasVolume(const Sums& s) { return s.total() > 0 || s.msgs > 0; }

int64_t percentile(std::vector<int64_t> sorted, double p) {  // 最近秩
  const size_t n = sorted.size();
  size_t rank = (size_t)std::ceil(p / 100.0 * (double)n);
  if (rank < 1) rank = 1;
  if (rank > n) rank = n;
  return sorted[rank - 1];
}

void fillPercentiles(Heatmap& h) {
  std::vector<int64_t> nonzero;
  for (const HeatCell& c : h.cells)
    if (c.sums.total() > 0) nonzero.push_back(c.sums.total());
  if (nonzero.empty()) return;
  std::sort(nonzero.begin(), nonzero.end());
  h.p50 = percentile(nonzero, 50.0);
  h.p75 = percentile(nonzero, 75.0);
  h.p90 = percentile(nonzero, 90.0);
}

} // namespace

std::vector<Bucket> bucketSeries(const Aggregator& agg, BucketRange range,
                                 int count, int offset, int64_t nowMs,
                                 const std::string& drillProvider) {
  std::vector<Bucket> out;
  if (count <= 0) return out;
  if (offset < 0) offset = 0;
  const int cur = currentBegin(range, nowMs);
  std::map<int, size_t> idxOf;  // 桶键 → out 下标
  for (int j = 0; j < count; ++j) {
    const int begin = stepBack(range, cur, offset + count - 1 - j);
    Bucket b;
    b.key = keyOf(range, begin);
    b.beginKey = begin;
    b.endKey = endOf(range, begin);
    idxOf[b.key] = out.size();
    out.push_back(std::move(b));
  }

  const bool drill = !drillProvider.empty();
  std::vector<std::map<std::string, Sums>> segMaps(out.size());
  std::map<std::string, std::string> bestCasing;  // lower(id) → 最近使用的原始 id
                                                   //（modelsByRecency 降序，首个即最近）

  if (!drill) {  // 全局口径：total 直接取 dayAll_
    for (const auto& [day, s] : agg.days()) {
      auto it = idxOf.find(bucketKeyOfDay(range, day));
      if (it != idxOf.end()) out[it->second].total.plus(s);
    }
  }

  for (const std::string& id : agg.modelsByRecency()) {
    const std::string provider = providerOf(id);
    if (drill && provider != drillProvider) continue;
    const std::string segId = drill ? lowerId(id) : provider;  // 下钻按小写归并
    if (drill && bestCasing.find(segId) == bestCasing.end()) bestCasing[segId] = id;
    const ModelStat* m = agg.model(id);
    if (!m) continue;
    for (const auto& [day, s] : m->byDay) {
      auto it = idxOf.find(bucketKeyOfDay(range, day));
      if (it == idxOf.end()) continue;
      segMaps[it->second][segId].plus(s);
      if (drill) out[it->second].total.plus(s);  // 下钻：total = 该提供商合计
    }
  }

  // 下钻重名带后缀：归并后仍同短名的（kimi-code/k3 vs claude-code/k3）
  // 加" · 命名空间"（级联菜单既有规则）
  std::map<std::string, int> shortNameCount;
  if (drill)
    for (const auto& [lower, best] : bestCasing)
      ++shortNameCount[lowerId(shortName(best))];

  for (size_t i = 0; i < out.size(); ++i) {
    for (const auto& [segId, s] : segMaps[i]) {
      if (!hasVolume(s)) continue;
      Segment seg;
      seg.id = segId;
      if (drill) {
        const std::string& best = bestCasing[segId];
        seg.label = shortName(best);
        if (shortNameCount[lowerId(shortName(best))] > 1)
          seg.label += " · " + vendorOf(best);
      } else {
        seg.label = segId;
      }
      seg.sums = s;
      out[i].segments.push_back(std::move(seg));
    }
    std::sort(out[i].segments.begin(), out[i].segments.end(),
              [](const Segment& a, const Segment& b) {
                if (a.sums.total() != b.sums.total())
                  return a.sums.total() > b.sums.total();
                return a.id < b.id;
              });
  }
  return out;
}

Heatmap heatmap(const Aggregator& agg, HeatGrain grain, int weeks, int64_t nowMs) {
  Heatmap h;
  if (weeks <= 0) return h;
  const int today = Aggregator::dayKey(nowMs);
  // 固定全跨度（用户裁决 2026-09-17 截图：GitHub 同款满宽）：数据不足一窗不再
  // 左对齐截短，数据开始前的日子画空档格——月份轴得以标满全跨度不留右侧空白
  const int start = addDays(Aggregator::weekStartKey(nowMs), -7 * (weeks - 1));

  if (grain == HeatGrain::Week) {
    for (int mon = start; mon <= today; mon = addDays(mon, 7)) {
      HeatCell c;
      c.dayKey = mon;
      for (int d = 0; d < 7; ++d) {
        auto it = agg.days().find(addDays(mon, d));
        if (it != agg.days().end()) c.sums.plus(it->second);
      }
      h.cells.push_back(std::move(c));
    }
  } else {
    Sums run;      // WeekCumulative 的周内滚动累计
    int runWeek = 0;
    for (int d = start; d <= today; d = addDays(d, 1)) {
      HeatCell c;
      c.dayKey = d;
      auto it = agg.days().find(d);
      const Sums day = it == agg.days().end() ? Sums{} : it->second;
      if (grain == HeatGrain::WeekCumulative) {
        const int wk = Aggregator::weekStartKey(dayKeyToMs(d));
        if (wk != runWeek) { runWeek = wk; run = Sums{}; }
        run.plus(day);
        c.sums = run;
      } else {
        c.sums = day;
      }
      h.cells.push_back(std::move(c));
    }
  }
  fillPercentiles(h);
  return h;
}

TotalsSummary totalsSummary(const Aggregator& agg, int64_t nowMs) {
  TotalsSummary t;
  for (const auto& [day, s] : agg.days()) {
    t.totalTokens += s.total();
    t.totalMsgs += s.msgs;
    if (!hasVolume(s)) continue;
    ++t.activeDays;
    if (s.total() > t.peakDayTokens) {  // std::map 升序，严格 > → 并列取最早
      t.peakDayTokens = s.total();
      t.peakDayKey = day;
    }
  }
  if (t.activeDays > 0) t.avgTokensPerActiveDay = t.totalTokens / t.activeDays;
  for (int d = Aggregator::dayKey(nowMs);; d = addDays(d, -1)) {
    auto it = agg.days().find(d);
    if (it == agg.days().end() || !hasVolume(it->second)) break;
    ++t.currentStreak;
  }
  return t;
}

} // namespace okmeter
