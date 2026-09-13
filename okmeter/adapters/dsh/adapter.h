// adapters/dsh/adapter.h —— DeepSeek Harness 采集：~/.dsh/storages/session_projcache.json
// 的 per-session tokenUsage 累计值（会话 jsonl 是 zstd 帧流读不了，投影缓存是唯一
// 明文口）；每次 poll 全量读文件，与 Store 里上次值做差，增量作为事件发出
#pragma once
#include "../adapter.h"
#include <filesystem>
#include <string>

namespace okmeter {

class Store;

class DshAdapter : public IAdapter {
public:
  DshAdapter(std::filesystem::path dshHome, Store* store);
  std::string id() const override { return "dsh"; }
  int poll(const EventSink& sink) override;

private:
  std::string defaultModel() const;  // settings.yaml 的 agent-default-model.model

  std::filesystem::path home_;
  Store* store_;
  int64_t lastMtimeMs_ = -1;  // 文件未变直接跳过（projcache 每次全量重写）
};

} // namespace okmeter
