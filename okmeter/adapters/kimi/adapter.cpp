#include "adapter.h"
#include "../../core/minjson.h"
#include "../../core/paths.h"
#include "../../core/store.h"
#include <fstream>

namespace okmeter {
namespace {

std::string sessionIdOf(const std::filesystem::path& p) {
  for (const auto& c : p) {
    std::string s = pathU8(c);
    if (s.rfind("session_", 0) == 0) return s;
  }
  return "";
}

} // namespace

KimiAdapter::KimiAdapter(std::filesystem::path kimiHome, Store* store)
  : home_(std::move(kimiHome)), store_(store) {}

int KimiAdapter::poll(const EventSink& sink) {
  int emitted = 0;
  const auto root = home_ / "sessions";
  std::error_code ec;
  if (!std::filesystem::exists(root, ec)) return 0;
  for (std::filesystem::recursive_directory_iterator it(root, ec), endIt;
       !ec && it != endIt; it.increment(ec)) {
    std::error_code ec2;
    if (it->is_regular_file(ec2) && !ec2 && it->path().filename() == "wire.jsonl")
      emitted += tailFile(it->path(), sink);
  }
  return emitted;
}

int KimiAdapter::tailFile(const std::filesystem::path& f, const EventSink& sink) {
  const std::string key = pathU8(f);
  int64_t off = store_->cursor(key);
  std::error_code ec;
  const auto size = (int64_t)std::filesystem::file_size(f, ec);
  if (ec) return 0;
  if (size < off) off = 0;        // 文件被截断/轮换：从头读
  if (size == off) return 0;

  std::ifstream in(f, std::ios::binary);
  if (!in) return 0;
  in.seekg(off);
  std::string chunk((std::istreambuf_iterator<char>(in)), std::istreambuf_iterator<char>());

  // 游标只推进到最后一个完整行；结尾半行留给下一轮
  const size_t lastNl = chunk.rfind('\n');
  if (lastNl == std::string::npos) return 0;
  const std::string_view work(chunk.data(), lastNl + 1);
  const int64_t newOff = off + (int64_t)work.size();

  const std::string sessionId = sessionIdOf(f);
  int emitted = 0;
  size_t pos = 0;
  while (pos < work.size()) {
    const size_t nl = work.find('\n', pos);
    if (parseLine(work.substr(pos, nl - pos), sessionId, sink)) ++emitted;
    pos = nl + 1;
  }
  store_->setCursor(key, newOff);
  return emitted;
}

bool KimiAdapter::parseLine(std::string_view line, const std::string& sessionId,
                            const EventSink& sink) {
  json::Value v;
  if (!json::parse(std::string(line), v)) return false;        // 坏行跳过
  const json::Value* type = v.find("type");
  if (!type || type->str() != "usage.record") return false;    // 未知 type 跳过
  const json::Value* usage = v.find("usage");
  if (!usage || !usage->isObject()) return false;              // 缺 usage 跳过
  const json::Value* model = v.find("model");
  if (!model || model->str().empty()) return false;

  UsageEvent e;
  e.model = model->str();
  if (const json::Value* a = v.find("agentId")) e.agentId = a->str();
  if (const json::Value* s = v.find("usageScope")) e.usageScope = s->str();
  e.sessionId = sessionId;
  if (const json::Value* x = usage->find("inputOther")) e.inputOther = (int64_t)x->num();
  if (const json::Value* x = usage->find("inputCacheRead")) e.inputCacheRead = (int64_t)x->num();
  if (const json::Value* x = usage->find("inputCacheCreation")) e.inputCacheCreation = (int64_t)x->num();
  if (const json::Value* x = usage->find("output")) e.output = (int64_t)x->num();
  if (const json::Value* t = v.find("time")) e.timeMs = (int64_t)t->num();
  sink(e);
  return true;
}

} // namespace okmeter
