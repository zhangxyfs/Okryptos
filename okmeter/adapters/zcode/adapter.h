// adapters/zcode/adapter.h —— ZCode 采集：~/.zcode/cli/rollout/model-io-*.jsonl
// 每行一次模型请求，response.usage 字段（camelCase，input 含 cacheRead）
#pragma once
#include "../adapter.h"
#include <filesystem>

namespace okmeter {

class Store;

class ZcodeAdapter : public IAdapter {
public:
  ZcodeAdapter(std::filesystem::path zcodeHome, Store* store);
  std::string id() const override { return "zcode"; }
  int poll(const EventSink& sink) override;

private:
  bool parseLine(std::string_view line, const std::string& fileStem,
                 const EventSink& sink);

  std::filesystem::path home_;
  Store* store_;
};

} // namespace okmeter
