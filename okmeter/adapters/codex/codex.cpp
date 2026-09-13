#include "adapter.h"
#include "../../core/isotime.h"
#include "../../core/minjson.h"
#include "../../core/paths.h"
#include "../../core/store.h"
#include "../tail.h"
#include <fstream>

namespace okmeter {
namespace {

// 文本里最后一个 "model":"..." 出现处（turn_context 行；子串扫描比逐行 JSON 便宜，
// exec 输出行可能极大且不解析）
std::string lastModelIn(std::string_view text) {
  std::string found;
  const std::string_view pat = "\"model\":\"";
  size_t pos = 0;
  while ((pos = text.find(pat, pos)) != std::string_view::npos) {
    const size_t begin = pos + pat.size();
    const size_t end = text.find('"', begin);
    if (end == std::string_view::npos) break;
    if (end > begin) found = std::string(text.substr(begin, end - begin));
    pos = end;
  }
  return found;
}

} // namespace

CodexAdapter::CodexAdapter(std::filesystem::path codexHome, Store* store)
  : home_(std::move(codexHome)), store_(store) {}

int CodexAdapter::poll(const EventSink& sink) {
  int emitted = 0;
  for (const char* sub : {"sessions", "archived_sessions"}) {
    const auto root = home_ / sub;
    std::error_code ec;
    if (!std::filesystem::exists(root, ec)) continue;
    for (std::filesystem::recursive_directory_iterator it(root, ec), endIt;
         !ec && it != endIt; it.increment(ec)) {
      std::error_code ec2;
      if (!it->is_regular_file(ec2) || ec2 || it->path().extension() != ".jsonl")
        continue;
      tailFile(it->path(), sink, &emitted);
    }
  }
  return emitted;
}

void CodexAdapter::tailFile(const std::filesystem::path& f, const EventSink& sink,
                            int* emitted) {
  const std::string key = pathU8(f);
  std::string& model = models_[key];
  if (model.empty() && store_->cursor(key) > 0) {
    // 进程重启后恢复：游标之后的行才可能带 turn_context 模型，先扫头部 256KB 补回
    std::ifstream in(f, std::ios::binary);
    if (in) {
      std::string head(256 * 1024, '\0');
      in.read(head.data(), (std::streamsize)head.size());
      head.resize((size_t)in.gcount());
      model = lastModelIn(head);
    }
  }
  const std::string sessionId = pathU8(f.stem());
  tailJsonl(f, store_, [&](std::string_view line) {
    // token_count 行以外的行也可能更新模型（turn_context 换模型）
    if (line.find("\"model\":\"") != std::string_view::npos) {
      const size_t keep = line.size() < 4096 ? line.size() : 4096;
      if (std::string m = lastModelIn(line.substr(0, keep)); !m.empty()) model = m;
    }
    if (parseLine(line, sessionId, model, sink)) ++*emitted;
  });
}

bool CodexAdapter::parseLine(std::string_view line, const std::string& sessionId,
                             const std::string& model, const EventSink& sink) {
  if (line.find("\"token_count\"") == std::string_view::npos) return false;
  json::Value v;
  if (!json::parse(std::string(line), v)) return false;
  const json::Value* type = v.find("type");
  if (!type || type->str() != "event_msg") return false;
  const json::Value* payload = v.find("payload");
  if (!payload || !payload->isObject()) return false;
  const json::Value* ptype = payload->find("type");
  if (!ptype || ptype->str() != "token_count") return false;
  const json::Value* info = payload->find("info");
  if (!info || !info->isObject()) return false;          // info:null（限流播报行）跳过
  const json::Value* last = info->find("last_token_usage");
  if (!last || !last->isObject()) return false;

  UsageEvent e;
  e.model = "codex/" + (model.empty() ? "unknown" : model);
  e.sessionId = sessionId;
  e.agentId = "main";
  e.usageScope = "turn";
  // codex 口径：input_tokens 含 cached；total 已含 reasoning（out 不单加）
  const json::Value* inV = last->find("input_tokens");
  if (!inV) return false;
  const int64_t in = (int64_t)inV->num();
  const json::Value* cachedV = last->find("cached_input_tokens");
  const int64_t cached = cachedV ? (int64_t)cachedV->num() : 0;
  e.inputOther = in >= cached ? in - cached : 0;
  e.inputCacheRead = cached;
  if (const json::Value* x = last->find("output_tokens")) e.output = (int64_t)x->num();
  if (const json::Value* t = v.find("timestamp")) e.timeMs = isoToMs(t->str());
  sink(e);
  return true;
}

} // namespace okmeter
