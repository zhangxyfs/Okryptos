#include "adapter.h"
#include "../../core/isotime.h"
#include "../../core/minjson.h"
#include "../../core/paths.h"
#include "../../core/store.h"
#include "../tail.h"

namespace okmeter {

ZcodeAdapter::ZcodeAdapter(std::filesystem::path zcodeHome, Store* store)
  : home_(std::move(zcodeHome)), store_(store) {}

int ZcodeAdapter::poll(const EventSink& sink) {
  int emitted = 0;
  const auto root = home_ / "cli" / "rollout";
  std::error_code ec;
  if (!std::filesystem::exists(root, ec)) return 0;
  for (std::filesystem::directory_iterator it(root, ec), endIt;
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

bool ZcodeAdapter::parseLine(std::string_view line, const std::string& fileStem,
                             const EventSink& sink) {
  if (line.find("\"inputTokens\"") == std::string_view::npos) return false;
  json::Value v;
  if (!json::parse(std::string(line), v)) return false;
  const json::Value* resp = v.find("response");
  if (!resp || !resp->isObject()) return false;
  const json::Value* usage = resp->find("usage");
  if (!usage || !usage->isObject()) return false;

  UsageEvent e;
  e.model = "zcode/";
  const json::Value* model = v.find("model");
  const json::Value* modelId = model ? model->find("modelId") : nullptr;
  e.model += (modelId && !modelId->str().empty()) ? modelId->str() : "unknown";
  e.sessionId = fileStem;
  if (const json::Value* s = v.find("sessionId"))
    if (!s->str().empty()) e.sessionId = s->str();
  e.agentId = "main";
  e.usageScope = "turn";
  // zcode 口径：inputTokens 含 cacheRead/cacheWrite（total = input + output）
  const json::Value* inV = usage->find("inputTokens");
  if (!inV) return false;
  const int64_t in = (int64_t)inV->num();
  const json::Value* crV = usage->find("cacheReadTokens");
  const int64_t cacheRead = crV ? (int64_t)crV->num() : 0;
  const json::Value* cwV = usage->find("cacheWriteTokens");
  const int64_t cacheWrite = cwV ? (int64_t)cwV->num() : 0;
  e.inputCacheRead = cacheRead;
  e.inputCacheCreation = cacheWrite;
  const int64_t rest = in - cacheRead - cacheWrite;
  e.inputOther = rest > 0 ? rest : 0;
  if (const json::Value* x = usage->find("outputTokens"))
    e.output = (int64_t)x->num();
  if (const json::Value* t = v.find("completedAt")) e.timeMs = isoToMs(t->str());
  if (e.total() == 0) return false;  // 失败请求的 usage:{} 空行
  sink(e);
  return true;
}

} // namespace okmeter
