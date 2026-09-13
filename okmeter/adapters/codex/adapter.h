// adapters/codex/adapter.h —— Codex CLI 采集：~/.codex/{sessions,archived_sessions}/**/*.jsonl
// event_msg/token_count 行的 payload.info.last_token_usage（每轮增量口径）
#pragma once
#include "../adapter.h"
#include <filesystem>
#include <map>
#include <string>

namespace okmeter {

class Store;

class CodexAdapter : public IAdapter {
public:
  CodexAdapter(std::filesystem::path codexHome, Store* store);
  std::string id() const override { return "codex"; }
  int poll(const EventSink& sink) override;

private:
  void tailFile(const std::filesystem::path& f, const EventSink& sink, int* emitted);
  bool parseLine(std::string_view line, const std::string& sessionId,
                 const std::string& model, const EventSink& sink);

  std::filesystem::path home_;
  Store* store_;
  std::map<std::string, std::string> models_;  // 文件 → 最近见到的模型（token_count 行不带 model）
};

} // namespace okmeter
