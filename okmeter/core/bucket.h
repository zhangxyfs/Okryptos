// core/bucket.h —— 用量总览数据层：日/周/月/年分桶、热力日历、累计摘要。
// 纯数据模块（无 D3D/UI 依赖），全部从 Aggregator 的 dayAll_ + ModelStat::byDay
// 现算，口径与 modelWeek/modelMonth 一致（周一起界、月键 yyyymm）。
#pragma once
#include "aggregator.h"
#include <string>
#include <vector>

namespace okmeter {

enum class BucketRange { Day, Week, Month, Year };

// 堆叠分段：提供商层 id==label==提供商名（providerOf 归并）；
// 下钻层 id=模型全名、label=短名（最后一段）
struct Segment {
  std::string id;
  std::string label;
  Sums sums;
};

struct Bucket {
  int key = 0;       // 日=当日 yyyymmdd；周=当周周一 yyyymmdd；月=yyyymm；年=yyyy
  int beginKey = 0;  // 桶起始日 dayKey
  int endKey = 0;    // 桶结束日 dayKey（含）
  Sums total;        // 全局口径；下钻时 = 该提供商合计
  std::vector<Segment> segments;  // 本桶有量的段，按 total() 降序、id 升序兜底
};

// 最近 count 桶（含当前桶，最旧在前），offset>0 整体向过去平移 offset 桶。
// 未观测/零量桶照常产出，序列连续不压缩。drillProvider 非空 = 下钻：拆分维度
// 改为该提供商的模型，total 变为该提供商合计（图随下钻重自适应）。
std::vector<Bucket> bucketSeries(const Aggregator& agg, BucketRange range,
                                 int count, int offset, int64_t nowMs,
                                 const std::string& drillProvider = "");

enum class HeatGrain { Day, Week, WeekCumulative };

struct HeatCell {
  int dayKey = 0;  // 格日期；Week 粒度 = 当周周一
  Sums sums;       // Week=该周合计；WeekCumulative=周一截至当日的当周累计
};

struct Heatmap {
  std::vector<HeatCell> cells;        // 最旧在前，止于当天（不出未来格）
  int64_t p50 = 0, p75 = 0, p90 = 0;  // 非零格 total() 分位阈值（最近秩），无非零格全 0
};

// 最近 weeks 个日历周（周一起界，含本周）。数据起点晚于窗口起点时左端对齐到
// 数据所在周，左侧不留空列。
Heatmap heatmap(const Aggregator& agg, HeatGrain grain, int weeks, int64_t nowMs);

struct TotalsSummary {
  int64_t totalTokens = 0;
  int64_t totalMsgs = 0;
  int activeDays = 0;                // total()>0 或 msgs>0 的天数
  int64_t avgTokensPerActiveDay = 0; // 总量/活跃天数，0 活跃天 → 0
  int peakDayKey = 0;                // 量最大的 dayKey（并列取最早）
  int64_t peakDayTokens = 0;
  int currentStreak = 0;             // 从当天往前连续有量天数（当天无量 → 0）
};

TotalsSummary totalsSummary(const Aggregator& agg, int64_t nowMs);

} // namespace okmeter
