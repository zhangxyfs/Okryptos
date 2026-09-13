#include "adapter.h"
#include "../../core/minjson.h"
#include "../../core/paths.h"
#include "../../core/store.h"
#include "../tail.h"

namespace okmeter {

WorkbuddyAdapter::WorkbuddyAdapter(std::filesystem::path workbuddyHome, Store* store)
  : home_(std::move(workbuddyHome)), store_(store) {}

int WorkbuddyAdapter::poll(const EventSink& sink) {
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

bool WorkbuddyAdapter::parseLine(std::string_view line, const std::string& fileStem,
                                 const EventSink& sink) {
  if (line.find("\"inputTokens\"") == std::string_view::npos) return false;
  json::Value v;
  if (!json::parse(std::string(line), v)) return false;
  const json::Value* pd = v.find("providerData");
  if (!pd || !pd->isObject()) return false;
  const json::Value* usage = pd->find("usage");
  if (!usage || !usage->isObject()) return false;
  const json::Value* inV = usage->find("inputTokens");
  if (!inV) return false;

  UsageEvent e;
  e.model = "workbuddy/";
  if (const json::Value* m = pd->find("model"))
    e.model += m->str().empty() ? "unknown" : m->str();
  else
    e.model += "unknown";
  e.sessionId = fileStem;
  if (const json::Value* s = v.find("sessionId"))
    if (!s->str().empty()) e.sessionId = s->str();
  e.agentId = "main";
  e.usageScope = "turn";
  // workbuddy 口径：inputTokens 含 cached（total = input + output，cached 取自 details）
  const int64_t in = (int64_t)inV->num();
  int64_t cached = 0;
  if (const json::Value* det = usage->find("inputTokensDetails"))
    if (det->isArray() && !det->arr().empty())
      if (const json::Value* c = det->arr().front().find("cached_tokens"))
        cached = (int64_t)c->num();
  e.inputCacheRead = cached;
  e.inputOther = in >= cached ? in - cached : 0;
  if (const json::Value* x = usage->find("outputTokens"))
    e.output = (int64_t)x->num();  // reasoning 含在 outputTokens 内（total 恒等式）
  if (const json::Value* t = v.find("timestamp")) e.timeMs = (int64_t)t->num();
  if (e.total() == 0) return false;
  sink(e);
  return true;
}

} // namespace okmeter
