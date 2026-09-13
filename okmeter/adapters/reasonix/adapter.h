// adapters/reasonix/adapter.h —— Reasonix 采集：<home>/usage.jsonl 单文件账本
// （promptTokens = cacheHit + cacheMiss；ts 为 epoch 毫秒；home 可能有两处：
// 新版 %APPDATA%/reasonix 与旧版 ~/.reasonix，都扫）
#pragma once
#include "../adapter.h"
#include <filesystem>
#include <vector>

namespace okmeter {

class Store;

class ReasonixAdapter : public IAdapter {
public:
  ReasonixAdapter(std::filesystem::path reasonixHome, Store* store);
  std::string id() const override { return "reasonix"; }
  int poll(const EventSink& sink) override;

private:
  bool parseLine(std::string_view line, const EventSink& sink);

  std::vector<std::filesystem::path> files_;  // 去重后的 usage.jsonl 候选
  Store* store_;
};

} // namespace okmeter
