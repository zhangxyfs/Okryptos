#include "adapter.h"
#include "../../core/isotime.h"
#include "../../core/minjson.h"
#include "../../core/paths.h"
#include "../../core/store.h"
#include "../tail.h"

namespace okmeter {

QwenAdapter::QwenAdapter(std::filesystem::path qwenHome, Store* store)
  : home_(std::move(qwenHome)), store_(store) {}

int QwenAdapter::poll(const EventSink& sink) {
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

bool QwenAdapter::parseLine(std::string_view line, const std::string& fileStem,
                            const EventSink& sink) {
  if (line.find("\"usageMetadata\"") == std::string_view::npos) return false;
  json::Value v;
  if (!json::parse(std::string(line), v)) return false;
  const json::Value* usage = v.find("usageMetadata");
  if (!usage || !usage->isObject()) return false;

  UsageEvent e;
  e.model = "qwen-code/";
  if (const json::Value* m = v.find("model"))
    e.model += m->str().empty() ? "unknown" : m->str();
  else
    e.model += "unknown";
  e.sessionId = fileStem;
  if (const json::Value* s = v.find("sessionId"))
    if (!s->str().empty()) e.sessionId = s->str();
  e.agentId = "main";
  e.usageScope = "turn";
  if (const json::Value* x = usage->find("promptTokenCount"))
    e.inputOther = (int64_t)x->num();
  if (const json::Value* x = usage->find("cachedContentTokenCount"))
    e.inputCacheRead = (int64_t)x->num();
  int64_t out = 0;
  if (const json::Value* x = usage->find("candidatesTokenCount"))
    out += (int64_t)x->num();
  if (const json::Value* x = usage->find("thoughtsTokenCount"))
    out += (int64_t)x->num();
  e.output = out;
  if (const json::Value* t = v.find("timestamp")) e.timeMs = isoToMs(t->str());
  sink(e);
  return true;
}

} // namespace okmeter
