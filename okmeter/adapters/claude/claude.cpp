#include "adapter.h"
#include "../../core/isotime.h"
#include "../../core/minjson.h"
#include "../../core/paths.h"
#include "../../core/store.h"
#include "../tail.h"

namespace okmeter {

ClaudeAdapter::ClaudeAdapter(std::filesystem::path claudeHome, Store* store)
  : home_(std::move(claudeHome)), store_(store) {}

int ClaudeAdapter::poll(const EventSink& sink) {
  int emitted = 0;
  const auto root = home_ / "projects";
  std::error_code ec;
  if (!std::filesystem::exists(root, ec)) return 0;
  for (std::filesystem::recursive_directory_iterator it(root, ec), endIt;
       !ec && it != endIt; it.increment(ec)) {
    std::error_code ec2;
    if (!it->is_regular_file(ec2) || ec2 || it->path().extension() != ".jsonl")
      continue;
    const std::string stem = pathU8(it->path().stem());
    tailJsonl(it->path(), store_, [&](std::string_view line) {
      if (parseLine(line, stem, sink)) ++emitted;
    });
  }
  return emitted;
}

bool ClaudeAdapter::parseLine(std::string_view line, const std::string& fileStem,
                              const EventSink& sink) {
  if (line.find("\"usage\"") == std::string_view::npos) return false;  // 廉价闸门
  json::Value v;
  if (!json::parse(std::string(line), v)) return false;
  const json::Value* msg = v.find("message");
  if (!msg || !msg->isObject()) return false;
  const json::Value* usage = msg->find("usage");
  if (!usage || !usage->isObject()) return false;
  const json::Value* model = msg->find("model");
  if (!model || model->str().empty()) return false;

  UsageEvent e;
  e.model = "claude-code/" + model->str();
  e.sessionId = fileStem;
  if (const json::Value* s = v.find("sessionId"))
    if (!s->str().empty()) e.sessionId = s->str();
  if (const json::Value* sc = v.find("isSidechain"))
    e.agentId = sc->boolean() ? "sub" : "main";
  e.usageScope = "turn";
  // Anthropic 口径：input_tokens 不含 cache_read/cache_creation，直接映射
  if (const json::Value* x = usage->find("input_tokens"))
    e.inputOther = (int64_t)x->num();
  if (const json::Value* x = usage->find("cache_read_input_tokens"))
    e.inputCacheRead = (int64_t)x->num();
  if (const json::Value* x = usage->find("cache_creation_input_tokens"))
    e.inputCacheCreation = (int64_t)x->num();
  if (const json::Value* x = usage->find("output_tokens"))
    e.output = (int64_t)x->num();
  if (const json::Value* t = v.find("timestamp")) e.timeMs = isoToMs(t->str());
  if (e.total() == 0) return false;  // <synthetic> 占位消息等零用量行是噪声
  sink(e);
  return true;
}

} // namespace okmeter
