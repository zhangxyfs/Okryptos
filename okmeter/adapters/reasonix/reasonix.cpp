#include "adapter.h"
#include "../../core/minjson.h"
#include "../../core/paths.h"
#include "../../core/store.h"
#include "../tail.h"

namespace okmeter {

ReasonixAdapter::ReasonixAdapter(std::filesystem::path reasonixHome, Store* store)
  : store_(store) {
  files_.push_back(reasonixHome / "usage.jsonl");
  // 旧版 home（~/.reasonix）仅在传入的是默认 home 时补扫——显式 home（测试/隔离
  // 环境）不应意外吞掉真实账本；同一游标账本按路径区分，两候选不冲突
  const auto up = userProfile();
  const auto defNew = up / "AppData" / "Roaming" / "reasonix";
  const auto defOld = up / ".reasonix";
  if (pathU8(reasonixHome) == pathU8(defNew) || pathU8(reasonixHome) == pathU8(defOld)) {
    for (const auto& def : { defNew, defOld }) {
      const auto f = def / "usage.jsonl";
      if (pathU8(f) != pathU8(files_.front())) files_.push_back(f);
    }
  }
}

int ReasonixAdapter::poll(const EventSink& sink) {
  int emitted = 0;
  for (const auto& f : files_) {
    std::error_code ec;
    if (!std::filesystem::exists(f, ec)) continue;
    tailJsonl(f, store_, [&](std::string_view line) {
      if (parseLine(line, sink)) ++emitted;
    });
  }
  return emitted;
}

bool ReasonixAdapter::parseLine(std::string_view line, const EventSink& sink) {
  if (line.find("\"promptTokens\"") == std::string_view::npos) return false;
  json::Value v;
  if (!json::parse(std::string(line), v)) return false;

  UsageEvent e;
  e.model = "reasonix/";
  if (const json::Value* m = v.find("model"))
    e.model += m->str().empty() ? "unknown" : m->str();
  else
    e.model += "unknown";
  e.sessionId = "usage";
  if (const json::Value* s = v.find("session"))
    if (!s->str().empty()) e.sessionId = s->str();  // session 可为 null → str() 空
  e.agentId = "main";
  e.usageScope = "turn";
  // reasonix 口径：promptTokens = cacheHit + cacheMiss
  if (const json::Value* x = v.find("cacheMissTokens")) e.inputOther = (int64_t)x->num();
  if (const json::Value* x = v.find("cacheHitTokens")) e.inputCacheRead = (int64_t)x->num();
  if (const json::Value* x = v.find("completionTokens")) e.output = (int64_t)x->num();
  if (const json::Value* t = v.find("ts")) e.timeMs = (int64_t)t->num();
  if (e.total() == 0) return false;
  sink(e);
  return true;
}

} // namespace okmeter
