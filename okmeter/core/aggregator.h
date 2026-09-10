// core/aggregator.h —— 聚合统计：all/today/week/session × 全局/模型
#pragma once
#include "event.h"
#include "minjson.h"
#include <map>
#include <vector>

namespace okmeter {

struct Sums {
  int64_t inputOther = 0, inputCacheRead = 0, inputCacheCreation = 0, output = 0;
  int64_t total() const { return inputOther + inputCacheRead + inputCacheCreation + output; }
  void plus(const Sums& o) {
    inputOther += o.inputOther; inputCacheRead += o.inputCacheRead;
    inputCacheCreation += o.inputCacheCreation; output += o.output;
  }
  static Sums of(const UsageEvent& e) {
    Sums s;
    s.inputOther = e.inputOther; s.inputCacheRead = e.inputCacheRead;
    s.inputCacheCreation = e.inputCacheCreation; s.output = e.output;
    return s;
  }
};

struct ModelStat {
  Sums all;
  std::map<int, Sums> byDay;              // dayKey(yyyymmdd) → 当日
  std::map<std::string, Sums> bySession;  // sessionId → 该会话
  int64_t lastCallMs = 0;
};

// 线程契约：本类非线程安全。运行时约定——采集 poll 与渲染读都在 UI 线程
// 由 timer 驱动串行进行（Plan 2 接线时遵守）；跨线程使用必须自行加锁。
class Aggregator {
public:
  void add(const UsageEvent& e);

  // 全局口径
  Sums all() const { return allAll_; }
  Sums today(int64_t nowMs) const;
  Sums week(int64_t nowMs) const;
  Sums session() const;  // 最近活跃 session 的累计

  // 模型口径
  std::vector<std::string> modelsByRecency() const;
  const ModelStat* model(const std::string& id) const {
    auto it = models_.find(id);
    return it == models_.end() ? nullptr : &it->second;
  }
  Sums modelToday(const std::string& id, int64_t nowMs) const;
  Sums modelWeek(const std::string& id, int64_t nowMs) const;
  Sums modelSession(const std::string& id) const;

  // 序列化（state.json）
  json::Value toJson() const;
  bool fromJson(const json::Value& v);

  static int dayKey(int64_t ms);            // 本地时区 yyyymmdd
  static int weekStartKey(int64_t ms);      // 本周一的 dayKey

private:
  Sums allAll_;
  std::map<int, Sums> dayAll_;
  std::map<std::string, Sums> sessAll_;
  std::map<std::string, ModelStat> models_;
  std::string hotSession_;
  int64_t hotSessionMs_ = -1;
};

} // namespace okmeter
