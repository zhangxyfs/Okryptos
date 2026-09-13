#include "adapter.h"
#include "../../core/minjson.h"
#include "../../core/paths.h"
#include "../../core/store.h"
#include <fstream>

namespace okmeter {

DshAdapter::DshAdapter(std::filesystem::path dshHome, Store* store)
  : home_(std::move(dshHome)), store_(store) {}

std::string DshAdapter::defaultModel() const {
  // 极简 yaml 读取：agent-default-model: 段内的第一个 model: 行
  std::ifstream in(home_ / "settings.yaml", std::ios::binary);
  if (!in) return "unknown";
  std::string line;
  bool inSection = false;
  while (std::getline(in, line)) {
    if (line.rfind("agent-default-model:", 0) == 0) { inSection = true; continue; }
    if (inSection) {
      if (!line.empty() && line[0] != ' ' && line[0] != '\t') break;  // 段结束
      const auto pos = line.find("model:");
      if (pos != std::string::npos) {
        std::string m = line.substr(pos + 6);
        const auto b = m.find_first_not_of(" \t\"'");
        const auto e = m.find_last_not_of(" \t\"'\r");
        if (b != std::string::npos && e >= b) return m.substr(b, e - b + 1);
      }
    }
  }
  return "unknown";
}

int DshAdapter::poll(const EventSink& sink) {
  const auto f = home_ / "storages" / "session_projcache.json";
  std::error_code ec;
  const auto mtime = std::filesystem::last_write_time(f, ec);
  if (ec) return 0;
  const int64_t mtimeMs =
    std::chrono::duration_cast<std::chrono::milliseconds>(mtime.time_since_epoch()).count();
  if (mtimeMs == lastMtimeMs_) return 0;

  std::ifstream in(f, std::ios::binary);
  if (!in) return 0;
  const std::string text((std::istreambuf_iterator<char>(in)),
                         std::istreambuf_iterator<char>());
  json::Value v;
  if (!json::parse(text, v)) return 0;
  lastMtimeMs_ = mtimeMs;

  const json::Value* tables = v.find("tables");
  const json::Value* sessions = tables ? tables->find("sessions") : nullptr;
  if (!sessions || !sessions->isObject()) return 0;
  const std::string model = "dsh/" + defaultModel();

  int emitted = 0;
  for (const auto& [sid, sv] : sessions->obj()) {
    const json::Value* rows = sv.find("rows");
    const json::Value* tu = rows ? rows->find("tokenUsage") : nullptr;
    const json::Value* val = tu ? tu->find("val") : nullptr;
    const json::Value* totals = val ? val->find("totals") : nullptr;
    if (!totals || !totals->isObject()) continue;

    // 时间：最近 prompt 时刻（epoch ms），退化会话创建时刻，再退化 0
    int64_t timeMs = 0;
    if (const json::Value* slm = rows->find("sessionListMetadata"))
      if (const json::Value* slv = slm->find("val"))
        if (const json::Value* t = slv->find("lastPromptAt"))
          timeMs = (int64_t)t->num();
    if (timeMs == 0)
      if (const json::Value* idn = sv.find("identity"))
        if (const json::Value* t = idn->find("createdAt"))
          timeMs = (int64_t)t->num();

    static const char* kKeys[4] = { "uncachedInputTokens", "outputTokens",
                                    "cacheReadTokens", "cacheWriteTokens" };
    int64_t delta[4] = { 0, 0, 0, 0 };
    bool any = false;
    for (int i = 0; i < 4; ++i) {
      const json::Value* x = totals->find(kKeys[i]);
      const int64_t cur = x ? (int64_t)x->num() : 0;
      const std::string ck = "dsh:totals:" + sid + ":" + kKeys[i];
      const int64_t prev = store_->cursor(ck);
      delta[i] = cur >= prev ? cur - prev : cur;  // 变小=重置，按全量补
      if (delta[i] > 0) any = true;
      if (cur != prev) store_->setCursor(ck, cur);
    }
    if (!any) continue;

    UsageEvent e;
    e.model = model;
    e.sessionId = sid;
    e.agentId = "main";
    e.usageScope = "turn";
    e.inputOther = delta[0];
    e.output = delta[1];
    e.inputCacheRead = delta[2];
    e.inputCacheCreation = delta[3];
    e.timeMs = timeMs;
    sink(e);
    ++emitted;
  }
  return emitted;
}

} // namespace okmeter
