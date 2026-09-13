// adapters/claude/adapter.h —— Claude Code 采集：~/.claude/projects/**/<session>.jsonl
// 行内 message.usage 字段（input/cache_read/cache_create/output）
#pragma once
#include "../adapter.h"
#include <filesystem>

namespace okmeter {

class Store;

class ClaudeAdapter : public IAdapter {
public:
  ClaudeAdapter(std::filesystem::path claudeHome, Store* store);
  std::string id() const override { return "claude-code"; }
  int poll(const EventSink& sink) override;

private:
  bool parseLine(std::string_view line, const std::string& fileStem,
                 const EventSink& sink);

  std::filesystem::path home_;
  Store* store_;
};

} // namespace okmeter
