// adapters/workbuddy/adapter.h —— WorkBuddy 采集：~/.workbuddy/projects/**/*.jsonl
// providerData.usage（camelCase，inputTokens 含 cached_tokens）；timestamp 为 epoch 毫秒
#pragma once
#include "../adapter.h"
#include <filesystem>

namespace okmeter {

class Store;

class WorkbuddyAdapter : public IAdapter {
public:
  WorkbuddyAdapter(std::filesystem::path workbuddyHome, Store* store);
  std::string id() const override { return "workbuddy"; }
  int poll(const EventSink& sink) override;

private:
  bool parseLine(std::string_view line, const std::string& fileStem,
                 const EventSink& sink);

  std::filesystem::path home_;
  Store* store_;
};

} // namespace okmeter
