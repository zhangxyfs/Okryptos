// adapters/hanako/adapter.h —— OpenHanako 采集：~/.hanako/logs/*.log 调试日志里
// 的 model_usage {json} 行（lib/llm/usage-observer.js 落盘；时间戳拆自文件名
// 日期 + 行首本地墙钟）
#pragma once
#include "../adapter.h"
#include <filesystem>

namespace okmeter {

class Store;

class HanakoAdapter : public IAdapter {
public:
  HanakoAdapter(std::filesystem::path hanakoHome, Store* store);
  std::string id() const override { return "hanako"; }
  int poll(const EventSink& sink) override;

private:
  bool parseLine(std::string_view line, const std::string& fileStem,
                 const EventSink& sink);

  std::filesystem::path home_;
  Store* store_;
};

} // namespace okmeter
