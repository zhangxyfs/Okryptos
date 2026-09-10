// core/store.h —— 游标 + 聚合快照持久化（state.json，原子写）
#pragma once
#include "aggregator.h"
#include <filesystem>
#include <map>
#include <string>

namespace okmeter {

// 线程契约：本类非线程安全。运行时约定——采集 poll 与渲染读都在 UI 线程
// 由 timer 驱动串行进行（Plan 2 接线时遵守）；跨线程使用必须自行加锁。
class Store {
public:
  explicit Store(std::filesystem::path dir) : dir_(std::move(dir)) {}

  int64_t cursor(const std::string& file) const {
    auto it = cursors_.find(file);
    return it == cursors_.end() ? 0 : it->second;
  }
  void setCursor(const std::string& file, int64_t off) { cursors_[file] = off; }

  Aggregator& agg() { return agg_; }

  bool load();   // 读 state.json；缺失/损坏 → false，内部状态保持全新
  bool flush();  // 原子写：state.json.tmp → MoveFileExW(REPLACE_EXISTING)

private:
  std::filesystem::path dir_;
  std::map<std::string, int64_t> cursors_;
  Aggregator agg_;
};

} // namespace okmeter
