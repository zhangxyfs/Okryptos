// adapters/qwen/adapter.h —— Qwen Code 采集：~/.qwen/projects/**/chats/*.jsonl
// 行内 usageMetadata 字段（Gemini 口径：prompt/cached/candidates/thoughts）
#pragma once
#include "../adapter.h"
#include <filesystem>

namespace okmeter {

class Store;

class QwenAdapter : public IAdapter {
public:
  QwenAdapter(std::filesystem::path qwenHome, Store* store);
  std::string id() const override { return "qwen-code"; }
  int poll(const EventSink& sink) override;

private:
  bool parseLine(std::string_view line, const std::string& fileStem,
                 const EventSink& sink);

  std::filesystem::path home_;
  Store* store_;
};

} // namespace okmeter
