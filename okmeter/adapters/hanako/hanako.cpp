#define _CRT_SECURE_NO_WARNINGS  // sscanf 解析固定格式时间戳（MSVC C4996 降噪）
#include "adapter.h"
#include "../../core/isotime.h"
#include "../../core/minjson.h"
#include "../../core/paths.h"
#include "../../core/store.h"
#include "../tail.h"
#include <cstdio>

namespace okmeter {

HanakoAdapter::HanakoAdapter(std::filesystem::path hanakoHome, Store* store)
  : home_(std::move(hanakoHome)), store_(store) {}

int HanakoAdapter::poll(const EventSink& sink) {
  int emitted = 0;
  const auto root = home_ / "logs";
  std::error_code ec;
  if (!std::filesystem::exists(root, ec)) return 0;
  for (std::filesystem::directory_iterator it(root, ec), endIt;
       !ec && it != endIt; it.increment(ec)) {
    std::error_code ec2;
    if (!it->is_regular_file(ec2) || ec2 || it->path().extension() != ".log")
      continue;
    const std::string stem = pathU8(it->path().stem());
    tailJsonl(it->path(), store_, [&](std::string_view line) {
      if (parseLine(line, stem, sink)) ++emitted;
    });
  }
  return emitted;
}

bool HanakoAdapter::parseLine(std::string_view line, const std::string& fileStem,
                              const EventSink& sink) {
  // 行形：[HH:MM:SS.mmm] [LEVEL] [MODULE] llm-usage 前缀 + "model_usage {json}"
  const size_t marker = line.find("model_usage {");
  if (marker == std::string_view::npos) return false;
  const size_t brace = line.find('{', marker);
  if (brace == std::string_view::npos) return false;
  json::Value v;
  if (!json::parse(std::string(line.substr(brace)), v)) return false;
  const json::Value* inV = v.find("inputTokens");
  if (!inV) return false;

  UsageEvent e;
  e.model = "hanako/";
  std::string modelId;
  if (const json::Value* m = v.find("modelId")) modelId = m->str();
  if (modelId.empty())
    if (const json::Value* p = v.find("provider")) modelId = p->str();
  e.model += modelId.empty() ? "unknown" : modelId;
  e.sessionId = fileStem;
  if (const json::Value* s = v.find("source"))
    if (!s->str().empty()) e.sessionId = s->str();
  e.agentId = "main";
  e.usageScope = "turn";

  // usage-observer 归一化为 Pi SDK 词汇；totalTokens 恒等式判 cache 是否含在 input：
  // total == in+out → OpenAI 系（含 cached，需拆）；否则 Anthropic/Pi 系（不含）
  const int64_t in = (int64_t)inV->num();
  const json::Value* outV = v.find("outputTokens");
  const int64_t out = outV ? (int64_t)outV->num() : 0;
  const json::Value* crV = v.find("cacheReadTokens");
  const int64_t cacheRead = crV ? (int64_t)crV->num() : 0;
  const json::Value* cwV = v.find("cacheWriteTokens");
  const int64_t cacheWrite = cwV ? (int64_t)cwV->num() : 0;
  const json::Value* totV = v.find("totalTokens");
  const int64_t total = totV ? (int64_t)totV->num() : 0;
  e.output = out;
  e.inputCacheRead = cacheRead;
  e.inputCacheCreation = cacheWrite;
  if (total > 0 && total == in + out) {
    const int64_t rest = in - cacheRead - cacheWrite;
    e.inputOther = rest > 0 ? rest : 0;
  } else {
    e.inputOther = in;
  }

  // 时间：文件名 YYYY-MM-DD_HH-MM-SS（本地） + 行首 [HH:MM:SS.mmm]（本地）
  int y = 0, mo = 0, d = 0;
  int h = 0, mi = 0, s = 0, ms = 0;
  if (std::sscanf(fileStem.c_str(), "%d-%d-%d", &y, &mo, &d) == 3 &&
      line.size() > 2 && line[0] == '[' &&
      std::sscanf(std::string(line.substr(1, 12)).c_str(), "%d:%d:%d.%d",
                  &h, &mi, &s, &ms) >= 3)
    e.timeMs = localToMs(y, mo, d, h, mi, s, ms);
  if (e.total() == 0) return false;
  sink(e);
  return true;
}

} // namespace okmeter
